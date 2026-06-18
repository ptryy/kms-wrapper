package gateway

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/ryan-truong/kms-wrapper/internal/config"
	cosmossigner "github.com/ryan-truong/kms-wrapper/internal/signer/cosmos"
	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

func newKeyPair(t *testing.T) (pubBytes []byte, evmAddr, cosmosAddr string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubBytes = crypto.FromECDSAPub(&key.PublicKey)
	evmAddr = crypto.PubkeyToAddress(key.PublicKey).Hex()
	cosmosAddr, err = cosmossigner.DeriveCosmosAddress(pubBytes, "cosmos")
	if err != nil {
		t.Fatalf("derive cosmos addr: %v", err)
	}
	return
}

func decodeJSON(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("unmarshal %q: %v", string(body), err)
	}
}

func TestCreateKeyHappyPath(t *testing.T) {
	pub, evmAddr, _ := newKeyPair(t)
	created := false
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) {
			if !created {
				return nil, fmt.Errorf("%w: key not found", apptypes.ErrNotFound)
			}
			return pub, nil
		},
		createKey: func(_ context.Context, _ string, _ []string) error {
			created = true
			return nil
		},
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"proj-a/prod/alice","chains":["evm"]}`), true)
	if rr.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp apptypes.KeyCreateResponse
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if resp.Path != "proj-a/prod/alice" {
		t.Fatalf("path=%q", resp.Path)
	}
	if resp.PublicKeyHex != hex.EncodeToString(pub) {
		t.Fatalf("public_key_hex mismatch")
	}
	if resp.EVMAddress != evmAddr {
		t.Fatalf("evm_address got=%s want=%s", resp.EVMAddress, evmAddr)
	}
	if resp.CosmosAddress != "" {
		t.Fatalf("expected no cosmos address, got %q", resp.CosmosAddress)
	}
	if got, want := resp.Chains, []apptypes.Chain{apptypes.ChainEVM}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("chains got=%#v want=%#v", got, want)
	}
	if resp.AlreadyExisted {
		t.Fatalf("expected already_existed=false on first create")
	}
}

func TestCreateKeyIdempotent(t *testing.T) {
	pub, _, _ := newKeyPair(t)
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) { return pub, nil },
		createKey:    func(_ context.Context, _ string, _ []string) error { return nil },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"proj-a/prod/alice","chains":["evm","cosmos"]}`), true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp apptypes.KeyCreateResponse
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if !resp.AlreadyExisted {
		t.Fatalf("expected already_existed=true when key pre-existed")
	}
	if resp.PublicKeyHex != hex.EncodeToString(pub) {
		t.Fatalf("public_key_hex mismatch on idempotent re-create")
	}
	if got, want := resp.Chains, []apptypes.Chain{apptypes.ChainCosmos, apptypes.ChainEVM}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("chains got=%#v want=%#v", got, want)
	}
}

func TestCreateKeyMissingPath(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{}`), true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "{\"error\":\"path is required\"}\n" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCreateKeyInvalidPath(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"Bad Path"}`), true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var err apptypes.ErrorResponse
	decodeJSON(t, rr.Body.Bytes(), &err)
	if err.Error == "" {
		t.Fatalf("empty error message")
	}
}

func TestCreateKeyMissingChains(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"proj-a/prod/alice"}`), true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if got, want := rr.Body.String(), "{\"error\":\"chains is required and must be a non-empty subset of [evm, cosmos]\"}\n"; got != want {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCreateKeyUnknownChains(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"proj-a/prod/alice","chains":["evm","solana"]}`), true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if got, want := rr.Body.String(), "{\"error\":\"chains is required and must be a non-empty subset of [evm, cosmos]\"}\n"; got != want {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCreateKeyMalformedJSON(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":`), true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "{\"error\":\"invalid JSON\"}\n" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCreateKeyPermissionDenied(t *testing.T) {
	wrapped := fmt.Errorf("%w: vault said no", apptypes.ErrPermission)
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) {
			return nil, fmt.Errorf("%w: key not found", apptypes.ErrNotFound)
		},
		createKey: func(_ context.Context, _ string, _ []string) error { return wrapped },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"proj-a/prod/alice","chains":["evm"]}`), true)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "{\"error\":\"permission denied\"}\n" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCreateKeyGenericVaultError(t *testing.T) {
	boom := errors.New("vault: kaboom with secret token=tok-AAAA")
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) {
			return nil, fmt.Errorf("%w: key not found", apptypes.ErrNotFound)
		},
		createKey: func(_ context.Context, _ string, _ []string) error { return boom },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"proj-a/prod/alice","chains":["evm"]}`), true)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "{\"error\":\"vault error\"}\n" {
		t.Fatalf("body=%s", rr.Body.String())
	}
	if string(rr.Body.Bytes()) == boom.Error() {
		t.Fatalf("raw vault error leaked into response")
	}
}

func TestCreateKeyUnauthorized(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodPost, "/keys", []byte(`{"path":"proj-a/prod/alice","chains":["evm"]}`), false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestShowKeyHappyPath(t *testing.T) {
	pub, evmAddr, _ := newKeyPair(t)
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) { return pub, nil },
		getKeyChains: func(_ context.Context, _ string) ([]string, error) { return []string{"evm"}, nil },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys/info?path=proj-a/prod/alice", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var info apptypes.KeyInfo
	decodeJSON(t, rr.Body.Bytes(), &info)
	if info.EVMAddress != evmAddr || info.CosmosAddress != "" {
		t.Fatalf("addresses mismatch: %#v", info)
	}
	if got, want := info.Chains, []apptypes.Chain{apptypes.ChainEVM}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("chains got=%#v want=%#v", got, want)
	}
}

func TestShowKeyNotFound(t *testing.T) {
	ks := keyStoreMock{
		getKeyChains: func(_ context.Context, _ string) ([]string, error) { return []string{"evm"}, nil },
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) {
			return nil, fmt.Errorf("%w: key not found", apptypes.ErrNotFound)
		},
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys/info?path=proj-a/prod/ghost", nil, true)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "{\"error\":\"key not found: proj-a/prod/ghost\"}\n" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestShowKeyMissingPath(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodGet, "/keys/info", nil, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "{\"error\":\"path is required\"}\n" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestShowKeyInvalidPath(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodGet, "/keys/info?path=BadPath", nil, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestShowKeyPermissionDenied(t *testing.T) {
	wrapped := fmt.Errorf("%w: vault said no", apptypes.ErrPermission)
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) { return nil, wrapped },
		getKeyChains: func(_ context.Context, _ string) ([]string, error) { return nil, nil },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys/info?path=proj-a/prod/alice", nil, true)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestShowKeyUnauthorized(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodGet, "/keys/info?path=proj-a/prod/alice", nil, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", rr.Code)
	}
}

func TestListKeysHappyPath(t *testing.T) {
	ks := keyStoreMock{
		listKeys: func(_ context.Context, prefix string) ([]string, error) {
			if prefix != "proj-a/" {
				t.Fatalf("unexpected prefix=%q", prefix)
			}
			return []string{"evm/alice", "cosmos/bob"}, nil
		},
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys?prefix=proj-a/", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp apptypes.KeyListResponse
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if resp.Count != 2 || len(resp.Keys) != 2 || resp.Keys[0].Path != "evm/alice" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestListKeysEmptyPrefix(t *testing.T) {
	called := false
	ks := keyStoreMock{
		listKeys: func(_ context.Context, prefix string) ([]string, error) {
			called = true
			if prefix != "" {
				t.Fatalf("expected empty prefix, got %q", prefix)
			}
			return []string{"k1"}, nil
		},
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatal("listKeys was not invoked")
	}
}

func TestListKeysEmptyResultIsNotNull(t *testing.T) {
	ks := keyStoreMock{
		listKeys: func(_ context.Context, _ string) ([]string, error) { return nil, nil },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys?prefix=empty/", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if body != "{\"keys\":[],\"count\":0,\"next_cursor\":\"\"}\n" {
		t.Fatalf("expected empty array, got %s", body)
	}
}

func TestListKeysPermissionDenied(t *testing.T) {
	ks := keyStoreMock{
		listKeys: func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("%w: vault denied", apptypes.ErrPermission)
		},
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys", nil, true)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestListKeysGenericError(t *testing.T) {
	ks := keyStoreMock{
		listKeys: func(_ context.Context, _ string) ([]string, error) { return nil, errors.New("boom") },
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys", nil, true)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestListKeysUnauthorized(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	rr := doRequest(h, http.MethodGet, "/keys", nil, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", rr.Code)
	}
}

func TestKeysShareRateLimitWithSign(t *testing.T) {
	pub, _, _ := newKeyPair(t)
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) { return pub, nil },
		getKeyChains: func(_ context.Context, _ string) ([]string, error) { return []string{"evm"}, nil },
		listKeys:     func(_ context.Context, _ string) ([]string, error) { return []string{}, nil },
	}
	h := newGatewayHandlerWithKeys(ks, func(cfg *config.Config) {
		cfg.Gateway.RateLimit = 1
		cfg.Gateway.RateBurst = 1
	})

	// First call (info) consumes the single available token.
	rr := doRequest(h, http.MethodGet, "/keys/info?path=proj-a/prod/alice", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("first /keys/info code=%d body=%s", rr.Code, rr.Body.String())
	}
	// Subsequent /sign/* should be rate-limited (shared budget).
	body := []byte(`{"type":"personal_message","key_path":"proj/prod/alice","personal_message":"0x6869"}`)
	rr = doRequest(h, http.MethodPost, "/sign/evm", body, true)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected /sign/evm to be rate-limited via shared budget, got code=%d body=%s", rr.Code, rr.Body.String())
	}
	// And another /keys call should also fail.
	rr = doRequest(h, http.MethodGet, "/keys", nil, true)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected /keys to be rate-limited, got code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSwaggerSpecAdvertisesKeys(t *testing.T) {
	h := newGatewayHandlerWithKeys(keyStoreMock{})
	doc := loadSwaggerDoc(t, h, false)

	for _, p := range []string{"/v1/keys", "/v1/keys/info"} {
		if _, ok := doc.Paths[p]; !ok {
			t.Fatalf("missing path %s in swagger doc", p)
		}
	}
	keysPath := doc.Paths["/v1/keys"]
	if keysPath.Post == nil || keysPath.Get == nil {
		t.Fatalf("/v1/keys must expose both POST and GET, got %#v", keysPath)
	}
	if doc.Paths["/v1/keys/info"].Get == nil {
		t.Fatalf("/v1/keys/info must expose GET")
	}
	if !requiresBearer(keysPath.Post) || !requiresBearer(keysPath.Get) || !requiresBearer(doc.Paths["/v1/keys/info"].Get) {
		t.Fatalf("expected all /v1/keys operations to require BearerAuth")
	}
}

// TestKeysInfoRoutesToShow guards against mux pattern precedence regressions:
// `GET /keys/info?path=...` must hit showKey, not listKeys.
func TestKeysInfoRoutesToShow(t *testing.T) {
	listCalled := false
	pub, _, _ := newKeyPair(t)
	ks := keyStoreMock{
		getPublicKey: func(_ context.Context, _ string) ([]byte, error) { return pub, nil },
		getKeyChains: func(_ context.Context, _ string) ([]string, error) { return []string{"evm"}, nil },
		listKeys: func(_ context.Context, _ string) ([]string, error) {
			listCalled = true
			return nil, nil
		},
	}
	h := newGatewayHandlerWithKeys(ks)
	rr := doRequest(h, http.MethodGet, "/keys/info?path=proj-a/prod/alice", nil, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if listCalled {
		t.Fatalf("/keys/info incorrectly dispatched to listKeys")
	}
	var info apptypes.KeyInfo
	decodeJSON(t, rr.Body.Bytes(), &info)
	if info.PublicKeyHex == "" {
		t.Fatalf("expected populated KeyInfo, got %#v", info)
	}
}
