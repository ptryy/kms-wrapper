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

type keyStoreMock struct {
	createKey    func(ctx context.Context, path string, chains []string) error
	getPublicKey func(ctx context.Context, path string) ([]byte, error)
	getKeyChains func(ctx context.Context, path string) ([]string, error)
	listKeys     func(ctx context.Context, prefix string) ([]string, error)
}

func (k keyStoreMock) CreateKey(ctx context.Context, path string, chains []string) error {
	if k.createKey == nil {
		return errors.New("CreateKey not stubbed")
	}
	return k.createKey(ctx, path, chains)
}
func (k keyStoreMock) GetPublicKey(ctx context.Context, path string) ([]byte, error) {
	if k.getPublicKey == nil {
		return nil, errors.New("GetPublicKey not stubbed")
	}
	return k.getPublicKey(ctx, path)
}
func (k keyStoreMock) GetKeyChains(ctx context.Context, path string) ([]string, error) {
	if k.getKeyChains == nil {
		return nil, errors.New("GetKeyChains not stubbed")
	}
	return k.getKeyChains(ctx, path)
}
func (k keyStoreMock) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	if k.listKeys == nil {
		return nil, errors.New("ListKeys not stubbed")
	}
	return k.listKeys(ctx, prefix)
}

type swaggerDoc struct {
	OpenAPI string                 `json:"openapi"`
	Paths   map[string]swaggerPath `json:"paths"`
	Servers []swaggerServer        `json:"servers"`
}

type swaggerServer struct {
	URL string `json:"url"`
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
	return newGatewayHandlerWithKeys(keyStoreMock{}, opts...)
}

func newGatewayHandlerWithKeys(ks KeyStore, opts ...func(*config.Config)) http.Handler {
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	// Existing tests pre-date the swagger_auth default flip; leave swagger
	// publicly reachable so they continue to exercise the unauthenticated
	// UI surface. Tests for the auth-on path override this explicitly.
	cfg.Gateway.SwaggerAuth = false
	for _, opt := range opts {
		opt(&cfg)
	}
	return New(cfg, healthMock{}, ks, evmMock{}, cosmosMock{}).Handler()
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

	body := []byte(`{"type":"personal_message","key_path":"proj/prod/alice","personal_message":"0x6869"}`)
	rr = doRequest(h, http.MethodPost, "/sign/evm", body, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("evm code=%d body=%s", rr.Code, rr.Body.String())
	}

	body = []byte(`{"key_path":"proj/staging/alice","sign_mode":"DIRECT","sign_doc":"` + base64.StdEncoding.EncodeToString([]byte("doc")) + `"}`)
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

func TestSwaggerRootServesUIWithoutRedirect(t *testing.T) {
	h := newGatewayHandler()
	rr := doRequest(h, http.MethodGet, "/swagger/", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected /swagger/ to return 200, got code=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content-type, got %q", ct)
	}
	if loc := rr.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no Location header on /swagger/, got %q", loc)
	}
}

func TestUnsupportedMethodReturnsJSON405(t *testing.T) {
	h := newGatewayHandler()
	rr := doRequest(h, http.MethodDelete, "/keys", nil, true)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE /keys, got code=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json content-type, got %q", ct)
	}
	if got, want := rr.Body.String(), "{\"error\":\"method not allowed\"}\n"; got != want {
		t.Fatalf("unexpected 405 body: got %q want %q", got, want)
	}
}

func TestSwaggerSpecRoutesAndSecurity(t *testing.T) {
	h := newGatewayHandler()
	doc := loadSwaggerDoc(t, h, false)

	if !strings.HasPrefix(doc.OpenAPI, "3.0") {
		t.Fatalf("expected openapi 3.0.x, got %q", doc.OpenAPI)
	}

	for _, path := range []string{"/v1/health", "/v1/sign/evm", "/v1/sign/cosmos"} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %s in swagger doc", path)
		}
	}
	for path := range doc.Paths {
		if strings.HasPrefix(path, "/swagger/") {
			t.Fatalf("swagger path should not be documented, found %s", path)
		}
	}

	evmPost := doc.Paths["/v1/sign/evm"].Post
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

	healthGet := doc.Paths["/v1/health"].Get
	if healthGet == nil || healthGet.Security == nil {
		t.Fatal("expected /v1/health to declare explicit empty security")
	}
	if len(*healthGet.Security) != 0 {
		t.Fatalf("expected /v1/health security to be empty, got %#v", *healthGet.Security)
	}

	if !requiresBearer(evmPost) {
		t.Fatalf("expected /v1/sign/evm to require BearerAuth, got %#v", evmPost.Security)
	}
	if !requiresBearer(doc.Paths["/v1/sign/cosmos"].Post) {
		t.Fatalf("expected /v1/sign/cosmos to require BearerAuth, got %#v", doc.Paths["/v1/sign/cosmos"].Post.Security)
	}
}

func TestSwaggerDocUsesRequestOrigin(t *testing.T) {
	h := newGatewayHandler(func(cfg *config.Config) {
		cfg.Gateway.Addr = "127.0.0.1:3010"
	})
	rr := doRequest(h, http.MethodGet, "http://127.0.0.1:3010/swagger/doc.json", nil, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("swagger doc code=%d body=%s", rr.Code, rr.Body.String())
	}

	var doc swaggerDoc
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal swagger doc: %v", err)
	}
	if len(doc.Servers) == 0 {
		t.Fatalf("expected at least one server entry, got %#v", doc.Servers)
	}
	if got, want := doc.Servers[0].URL, "http://127.0.0.1:3010/"; got != want {
		t.Fatalf("unexpected server url: got %q want %q", got, want)
	}
	if doc.Servers[0].URL == "http://localhost:8080/" {
		t.Fatalf("swagger server url should not be fixed localhost:8080")
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

	body := []byte(`{"type":"personal_message","key_path":"proj/prod/alice","personal_message":"0x6869"}`)
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
