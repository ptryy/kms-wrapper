package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/ryan-truong/kms-wrapper/docs"
	"github.com/ryan-truong/kms-wrapper/internal/config"
)

type healthMock struct{ err error }

func (h healthMock) Health() error { return h.err }

type evmMock struct{}

func (evmMock) SignRawTx(_ context.Context, _ string, _ *big.Int, _ []byte) ([]byte, error) {
	return nil, errors.New("mock raw unsupported")
}
func (evmMock) SignPersonalMessage(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return bytes.Repeat([]byte{1}, 65), nil
}
func (evmMock) SignEIP712Digest(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return bytes.Repeat([]byte{2}, 65), nil
}

type cosmosMock struct{}

func (cosmosMock) SignDirect(_ context.Context, _ string, _ []byte) ([]byte, []byte, error) {
	return []byte("sig"), bytes.Repeat([]byte{3}, 33), nil
}
func (cosmosMock) SignAmino(_ context.Context, _ string, _ []byte) ([]byte, []byte, error) {
	return []byte("sig"), bytes.Repeat([]byte{3}, 33), nil
}

type swaggerDoc struct {
	OpenAPI string                 `json:"openapi"`
	Paths   map[string]swaggerPath `json:"paths"`
}

type swaggerPath struct {
	Get  *swaggerOperation `json:"get,omitempty"`
	Post *swaggerOperation `json:"post,omitempty"`
}

type swaggerOperation struct {
	Security    *[]map[string][]string `json:"security,omitempty"`
	RequestBody *swaggerRequestBody    `json:"requestBody,omitempty"`
}

type swaggerRequestBody struct {
	Content map[string]swaggerMediaType `json:"content"`
}

type swaggerMediaType struct {
	Schema swaggerSchema `json:"schema"`
}

type swaggerSchema struct {
	OneOf []json.RawMessage `json:"oneOf,omitempty"`
}

func newGatewayHandler(opts ...func(*config.Config)) http.Handler {
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	for _, opt := range opts {
		opt(&cfg)
	}
	return New(cfg, healthMock{}, evmMock{}, cosmosMock{}).Handler()
}

func doRequest(h http.Handler, method, path string, body []byte, auth bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if auth {
		req.Header.Set("Authorization", "Bearer secret")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func loadSwaggerDoc(t *testing.T, h http.Handler, auth bool) swaggerDoc {
	t.Helper()
	rr := doRequest(h, http.MethodGet, "/swagger/doc.json", nil, auth)
	if rr.Code != http.StatusOK {
		t.Fatalf("swagger doc code=%d body=%s", rr.Code, rr.Body.String())
	}
	var doc swaggerDoc
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal swagger doc: %v", err)
	}
	return doc
}

func requiresBearer(op *swaggerOperation) bool {
	if op == nil || op.Security == nil {
		return false
	}
	for _, security := range *op.Security {
		if scopes, ok := security["BearerAuth"]; ok && len(scopes) == 0 {
			return true
		}
	}
	return false
}

func TestGatewayAuthHealthAndSign(t *testing.T) {
	h := newGatewayHandler()

	rr := doRequest(h, http.MethodGet, "/health", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("health code %d", rr.Code)
	}

	rr = doRequest(h, http.MethodPost, "/sign/evm", nil, false)
	if rr.Code != http.StatusUnauthorized || rr.Body.String() != "{\"error\":\"unauthorized\"}\n" {
		t.Fatalf("unauth code=%d body=%s", rr.Code, rr.Body.String())
	}

	body := []byte(`{"key_path":"proj/evm/alice","personal_message":"0x6869"}`)
	rr = doRequest(h, http.MethodPost, "/sign/evm", body, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("evm code=%d body=%s", rr.Code, rr.Body.String())
	}

	body = []byte(`{"key_path":"proj/cosmos/alice","sign_mode":"DIRECT","sign_doc":"` + base64.StdEncoding.EncodeToString([]byte("doc")) + `"}`)
	rr = doRequest(h, http.MethodPost, "/sign/cosmos", body, true)
	var resp map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if rr.Code != http.StatusOK || resp["signature"] == "" || resp["pub_key"] == "" {
		t.Fatalf("cosmos code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSwaggerUIEnabledByDefault(t *testing.T) {
	h := newGatewayHandler()
	rr := doRequest(h, http.MethodGet, "/swagger/index.html", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("swagger ui code=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<title>Swagger UI</title>") && !strings.Contains(body, "swagger-ui-bundle") {
		t.Fatalf("swagger ui body missing expected marker: %s", body)
	}
}

func TestSwaggerSpecRoutesAndSecurity(t *testing.T) {
	h := newGatewayHandler()
	doc := loadSwaggerDoc(t, h, false)

	if !strings.HasPrefix(doc.OpenAPI, "3.0") {
		t.Fatalf("expected openapi 3.0.x, got %q", doc.OpenAPI)
	}

	for _, path := range []string{"/health", "/sign/evm", "/sign/cosmos"} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %s in swagger doc", path)
		}
	}
	for path := range doc.Paths {
		if strings.HasPrefix(path, "/swagger/") {
			t.Fatalf("swagger path should not be documented, found %s", path)
		}
	}

	evmPost := doc.Paths["/sign/evm"].Post
	if evmPost == nil || evmPost.RequestBody == nil {
		t.Fatalf("missing /sign/evm requestBody in swagger doc")
	}
	jsonSchema, ok := evmPost.RequestBody.Content["application/json"]
	if !ok {
		t.Fatal("missing application/json schema for /sign/evm")
	}
	if len(jsonSchema.Schema.OneOf) != 3 {
		t.Fatalf("expected /sign/evm oneOf length 3, got %d", len(jsonSchema.Schema.OneOf))
	}

	healthGet := doc.Paths["/health"].Get
	if healthGet == nil || healthGet.Security == nil {
		t.Fatal("expected /health to declare explicit empty security")
	}
	if len(*healthGet.Security) != 0 {
		t.Fatalf("expected /health security to be empty, got %#v", *healthGet.Security)
	}

	if !requiresBearer(evmPost) {
		t.Fatalf("expected /sign/evm to require BearerAuth, got %#v", evmPost.Security)
	}
	if !requiresBearer(doc.Paths["/sign/cosmos"].Post) {
		t.Fatalf("expected /sign/cosmos to require BearerAuth, got %#v", doc.Paths["/sign/cosmos"].Post.Security)
	}
}

func TestSwaggerDisabledReturns404(t *testing.T) {
	h := newGatewayHandler(func(cfg *config.Config) {
		cfg.Gateway.SwaggerEnabled = false
	})
	rr := doRequest(h, http.MethodGet, "/swagger/index.html", nil, false)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected swagger 404 when disabled, got code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSwaggerAuthToggle(t *testing.T) {
	h := newGatewayHandler(func(cfg *config.Config) {
		cfg.Gateway.SwaggerAuth = true
	})

	rr := doRequest(h, http.MethodGet, "/swagger/doc.json", nil, false)
	if rr.Code != http.StatusUnauthorized || rr.Body.String() != "{\"error\":\"unauthorized\"}\n" {
		t.Fatalf("expected unauthorized without token, got code=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodGet, "/swagger/doc.json", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected swagger doc with auth token, got code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSwaggerRoutesAreNotRateLimited(t *testing.T) {
	h := newGatewayHandler(func(cfg *config.Config) {
		cfg.Gateway.RateLimit = 1
		cfg.Gateway.RateBurst = 1
	})

	body := []byte(`{"key_path":"proj/evm/alice","personal_message":"0x6869"}`)
	rr := doRequest(h, http.MethodPost, "/sign/evm", body, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected first sign request to succeed, got code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = doRequest(h, http.MethodPost, "/sign/evm", body, true)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected sign endpoint to be rate-limited, got code=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodGet, "/swagger/index.html", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected swagger UI to bypass rate limiter, got code=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = doRequest(h, http.MethodGet, "/swagger/doc.json", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected swagger doc to bypass rate limiter, got code=%d body=%s", rr.Code, rr.Body.String())
	}
}
