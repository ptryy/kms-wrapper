package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ryan-truong/kms-wrapper/internal/config"
	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

// erroringCosmos returns the supplied err from both sign methods.
type erroringCosmos struct{ err error }

func (e erroringCosmos) SignDirect(_ context.Context, _ string, _ []byte) ([]byte, []byte, error) {
	return nil, nil, e.err
}
func (e erroringCosmos) SignAmino(_ context.Context, _ string, _ []byte) ([]byte, []byte, error) {
	return nil, nil, e.err
}

func gatewayWithCosmos(t *testing.T, cs CosmosSigner) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	cfg.Gateway.SwaggerAuth = false
	srv, err := NewOrFail(cfg, healthMock{}, keyStoreMock{}, evmMock{}, cs)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func TestSignCosmosDuplicateKeysReturns400(t *testing.T) {
	dupErr := fmt.Errorf("duplicate key in amino sign doc: a: %w", apptypes.ErrBadRequest)
	h := gatewayWithCosmos(t, erroringCosmos{err: dupErr})
	body := []byte(`{"key_path":"proj/cosmos/alice","sign_mode":"AMINO_JSON","sign_doc":"{}"}`)
	rr := doRequest(h, http.MethodPost, "/v1/sign/cosmos", body, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "duplicate key") {
		t.Fatalf("expected duplicate-key error in body, got %s", rr.Body.String())
	}
}

func TestSignCosmosGenericSignerErrorReturns500(t *testing.T) {
	h := gatewayWithCosmos(t, erroringCosmos{err: errors.New("boom")})
	body := []byte(`{"key_path":"proj/cosmos/alice","sign_mode":"AMINO_JSON","sign_doc":"{}"}`)
	rr := doRequest(h, http.MethodPost, "/v1/sign/cosmos", body, true)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRoutesDualMounted(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	bare := doRequest(h, http.MethodPost, "/sign/evm",
		[]byte(`{"type":"personal_message","key_path":"proj/evm/alice","personal_message":"0x6869"}`), true)
	v1 := doRequest(h, http.MethodPost, "/v1/sign/evm",
		[]byte(`{"type":"personal_message","key_path":"proj/evm/alice","personal_message":"0x6869"}`), true)
	if bare.Code != http.StatusOK || v1.Code != http.StatusOK {
		t.Fatalf("bare=%d v1=%d bare-body=%s v1-body=%s", bare.Code, v1.Code, bare.Body.String(), v1.Body.String())
	}
	if bare.Body.String() != v1.Body.String() {
		t.Fatalf("dual-mount bodies diverge: bare=%s v1=%s", bare.Body.String(), v1.Body.String())
	}
	if bare.Header().Get("Deprecation") != "true" {
		t.Fatalf("expected Deprecation:true on bare path, got %q", bare.Header().Get("Deprecation"))
	}
	if bare.Header().Get("Sunset") == "" {
		t.Fatalf("expected Sunset header on bare path")
	}
	if v1.Header().Get("Deprecation") != "" {
		t.Fatalf("v1 path should not include Deprecation header, got %q", v1.Header().Get("Deprecation"))
	}
}

func TestEVMDiscriminatorMissing(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/v1/sign/evm",
		[]byte(`{"key_path":"proj/evm/alice","personal_message":"0x6869"}`), true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "type is required") {
		t.Fatalf("expected 'type is required' error, got %s", rr.Body.String())
	}
}

func TestEVMDiscriminatorMismatch(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/v1/sign/evm",
		[]byte(`{"type":"raw_tx","key_path":"proj/evm/alice","personal_message":"0x6869"}`), true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "raw_tx is required when type=raw_tx") {
		t.Fatalf("expected raw_tx-required error, got %s", rr.Body.String())
	}
}

func TestListKeysPagination(t *testing.T) {
	ks := keyStoreMock{
		listKeys: func(_ context.Context, _ string) ([]string, error) {
			return []string{"a", "b", "c", "d", "e"}, nil
		},
	}
	h := newGatewayHandlerWithKeys(ks)

	rr := doRequest(h, http.MethodGet, "/v1/keys?prefix=&limit=2", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("page 1 code=%d body=%s", rr.Code, rr.Body.String())
	}
	var page1 apptypes.KeyListResponse
	decodeJSON(t, rr.Body.Bytes(), &page1)
	if len(page1.Keys) != 2 || page1.NextCursor == "" {
		t.Fatalf("page 1: %#v", page1)
	}

	rr = doRequest(h, http.MethodGet, "/v1/keys?prefix=&limit=2&cursor="+page1.NextCursor, nil, true)
	var page2 apptypes.KeyListResponse
	decodeJSON(t, rr.Body.Bytes(), &page2)
	if len(page2.Keys) != 2 || page2.NextCursor == "" {
		t.Fatalf("page 2: %#v", page2)
	}

	rr = doRequest(h, http.MethodGet, "/v1/keys?prefix=&limit=2&cursor="+page2.NextCursor, nil, true)
	var page3 apptypes.KeyListResponse
	decodeJSON(t, rr.Body.Bytes(), &page3)
	if len(page3.Keys) != 1 || page3.NextCursor != "" {
		t.Fatalf("page 3 (last): %#v", page3)
	}
}

func TestListKeysInvalidCursor(t *testing.T) {
	ks := keyStoreMock{
		listKeys: func(_ context.Context, _ string) ([]string, error) { return nil, nil },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/v1/keys?cursor=!!!not-base64!!!", nil, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid cursor") {
		t.Fatalf("expected 'invalid cursor', got %s", rr.Body.String())
	}
}

func TestListKeysLimitClamp(t *testing.T) {
	ks := keyStoreMock{
		listKeys: func(_ context.Context, _ string) ([]string, error) {
			out := make([]string, 1500)
			for i := range out {
				out[i] = fmt.Sprintf("k%04d", i)
			}
			return out, nil
		},
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/v1/keys?limit=99999", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp apptypes.KeyListResponse
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if resp.Count != 1000 {
		t.Fatalf("expected limit clamped to 1000, got count=%d", resp.Count)
	}
}

func TestMethodNotAllowedIncludesAllowHeader(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodDelete, "/v1/keys", nil, true)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	allow := rr.Header().Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Fatalf("expected Allow header to list GET and POST, got %q", allow)
	}
	if rr.Body.String() != "{\"error\":\"method not allowed\"}\n" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestKeysIdempotentReturns200(t *testing.T) {
	pub, _, _ := newKeyPair(t)
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) { return pub, nil },
		createKey:    func(_ context.Context, _ string) error { return nil },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodPost, "/v1/keys", []byte(`{"path":"proj-a/evm/alice"}`), true)
	if rr.Code != http.StatusOK {
		t.Fatalf("idempotent re-create should be 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp apptypes.KeyCreateResponse
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if !resp.AlreadyExisted {
		t.Fatalf("expected already_existed=true")
	}
}
