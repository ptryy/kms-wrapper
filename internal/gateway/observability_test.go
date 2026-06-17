package gateway

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/ryan-truong/kms-wrapper/internal/config"
)

func newProbesHandler(vh HealthChecker) http.Handler {
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	cfg.Gateway.SwaggerAuth = false
	srv, err := NewOrFail(cfg, vh, keyStoreMock{}, evmMock{}, cosmosMock{})
	if err != nil {
		panic(err)
	}
	return srv.Handler()
}

func TestLivezAlwaysAlive(t *testing.T) {
	h := newProbesHandler(healthMock{})
	rr := doRequest(h, http.MethodGet, "/livez", nil, false)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"alive"`) {
		t.Fatalf("livez code=%d body=%s", rr.Code, rr.Body.String())
	}
}

type unreachableVault struct{}

func (unreachableVault) Health() error { return errors.New("connection refused") }

type readyVault struct{ last time.Time }

func (r readyVault) Health() error             { return nil }
func (r readyVault) LastLookupSelf() time.Time { return r.last }

func TestReadyzVaultDownReturns503(t *testing.T) {
	h := newProbesHandler(unreachableVault{})
	rr := doRequest(h, http.MethodGet, "/readyz", nil, false)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "vault_unreachable") {
		t.Fatalf("expected vault_unreachable reason, got %s", rr.Body.String())
	}
}

func TestReadyzStaleLookupReturns503(t *testing.T) {
	h := newProbesHandler(readyVault{last: time.Now().Add(-time.Hour)})
	rr := doRequest(h, http.MethodGet, "/readyz", nil, false)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "token_invalid") {
		t.Fatalf("expected token_invalid reason, got %s", rr.Body.String())
	}
}

func TestReadyzFreshLookupReturns200(t *testing.T) {
	h := newProbesHandler(readyVault{last: time.Now()})
	rr := doRequest(h, http.MethodGet, "/readyz", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHealthAliasIncludesDeprecationHeaders(t *testing.T) {
	h := newProbesHandler(healthMock{})
	rr := doRequest(h, http.MethodGet, "/health", nil, false)
	if rr.Header().Get("Deprecation") != "true" {
		t.Fatalf("expected Deprecation:true on /health, got %q", rr.Header().Get("Deprecation"))
	}
	if rr.Header().Get("Sunset") == "" {
		t.Fatalf("expected Sunset header on /health")
	}
}

func TestMetricsEndpointExposesCollectors(t *testing.T) {
	// Pre-warm every collector. Vec families with no observations are not
	// rendered by promhttp (no `# HELP` line, no rows), so observing once
	// each is the canonical way to surface the family on the scrape.
	kmsHTTPRequestsTotal.WithLabelValues("/test", "GET", "200").Inc()
	kmsHTTPRequestDuration.WithLabelValues("/test", "GET").Observe(0.001)
	kmsSigningDuration.WithLabelValues("evm").Observe(0.001)
	kmsRateLimitRejectionsTotal.WithLabelValues("/test").Inc()
	kmsVaultCallsTotal.WithLabelValues("read", "ok").Inc()
	kmsVaultCallDuration.WithLabelValues("read").Observe(0.001)
	kmsTokenRenewalFailuresTotal.Inc()
	kmsPanicsTotal.WithLabelValues("/test").Inc()

	h := newProbesHandler(healthMock{})
	rr := doRequest(h, http.MethodGet, "/metrics", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	wantNames := []string{
		"kms_http_requests_total",
		"kms_http_request_duration_seconds",
		"kms_signing_duration_seconds",
		"kms_rate_limit_rejections_total",
		"kms_vault_calls_total",
		"kms_vault_call_duration_seconds",
		"kms_token_renewal_failures_total",
		"kms_panics_total",
	}
	for _, name := range wantNames {
		if !strings.Contains(body, name) {
			t.Errorf("missing collector name %q in /metrics body", name)
		}
	}
}

func TestRequestMetricIncrementsOnSign(t *testing.T) {
	before := testutil.ToFloat64(kmsHTTPRequestsTotal.WithLabelValues("/v1/sign/evm", "POST", "200"))
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/v1/sign/evm",
		[]byte(`{"type":"personal_message","key_path":"proj/prod/alice","personal_message":"0x6869"}`), true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	after := testutil.ToFloat64(kmsHTTPRequestsTotal.WithLabelValues("/v1/sign/evm", "POST", "200"))
	if after-before < 1 {
		t.Fatalf("expected kms_http_requests_total to increment, before=%f after=%f", before, after)
	}
}

func TestRequestIDPreservedAndEchoed(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	req.Header.Set("X-Request-ID", "my-trace-id-123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if got := rr.Header().Get("X-Request-ID"); got != "my-trace-id-123" {
		t.Fatalf("expected echoed ID, got %q", got)
	}
}

func TestRequestIDGeneratedIfMissing(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	got := rr.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("expected generated request ID")
	}
	if !requestIDPattern.MatchString(got) {
		t.Fatalf("generated ID %q fails pattern", got)
	}
}

func TestRequestIDMalformedReplaced(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	// Inject characters that fail the [A-Za-z0-9._-]{1,128} pattern.
	req.Header.Set("X-Request-ID", "bad id with <html>")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	got := rr.Header().Get("X-Request-ID")
	if got == "bad id with <html>" {
		t.Fatal("malformed ID should have been replaced")
	}
	if !requestIDPattern.MatchString(got) {
		t.Fatalf("replacement ID %q fails pattern", got)
	}
}

func TestRequestIDInSlogContext(t *testing.T) {
	var buf bytes.Buffer
	handler := NewRequestIDLogHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(prev)

	ctx := context.WithValue(context.Background(), requestIDKey{}, "test-id-XYZ")
	slog.InfoContext(ctx, "hello")
	if !strings.Contains(buf.String(), "request_id=test-id-XYZ") {
		t.Fatalf("expected request_id in slog output, got: %s", buf.String())
	}
}

// panickyHandler always panics; used for the recover middleware test.
type panickyHandler struct{}

func (panickyHandler) ServeHTTP(http.ResponseWriter, *http.Request) {
	panic("boom for test")
}

func TestPanicRecoveryReturns500WithRequestID(t *testing.T) {
	before := testutil.ToFloat64(kmsPanicsTotal.WithLabelValues("/panic-test"))
	wrapped := requestID(recoverPanic(panickyHandler{}))
	req := httptest.NewRequest(http.MethodGet, "/panic-test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "internal server error") {
		t.Fatalf("expected canonical 500 body, got %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "request_id") {
		t.Fatalf("expected request_id in body, got %s", rr.Body.String())
	}
	after := testutil.ToFloat64(kmsPanicsTotal.WithLabelValues("/panic-test"))
	if after-before < 1 {
		t.Fatalf("kms_panics_total did not increment: before=%f after=%f", before, after)
	}
}

func TestRateLimitRejectionLogsInfoAndCounts(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	h := newGatewayHandlerWithKeys(keyStoreMock{}, func(cfg *config.Config) {
		cfg.Gateway.RateLimit = 1
		cfg.Gateway.RateBurst = 1
	})
	body := []byte(`{"type":"personal_message","key_path":"proj/prod/alice","personal_message":"0x6869"}`)
	doRequest(h, http.MethodPost, "/v1/sign/evm", body, true) // consume burst
	rr := doRequest(h, http.MethodPost, "/v1/sign/evm", body, true)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	logged := buf.String()
	if !strings.Contains(logged, "rate limit exceeded") {
		t.Fatalf("expected info log, got: %s", logged)
	}
	if strings.Contains(logged, "level=WARN") {
		t.Fatalf("rate-limit rejection must not log at WARN, got: %s", logged)
	}
}
