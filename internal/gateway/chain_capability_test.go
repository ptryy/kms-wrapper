package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/ryan-truong/kms-wrapper/internal/config"
	apptypes "github.com/ryan-truong/kms-wrapper/pkg/types"
)

type patchKeyChainsResponse struct {
	Path   string   `json:"path"`
	Chains []string `json:"chains"`
}

type chainStore struct {
	mu     sync.RWMutex
	chains map[string][]string
	errs   map[string]error
}

func newChainStore() *chainStore {
	return &chainStore{
		chains: map[string][]string{},
		errs:   map[string]error{},
	}
}

func (s *chainStore) SetChains(path string, chains []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chains[path] = append([]string(nil), chains...)
	delete(s.errs, path)
}

func (s *chainStore) SetError(path string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs[path] = err
}

func (s *chainStore) CreateKey(context.Context, string, []string) error {
	return errors.New("CreateKey not expected")
}

func (s *chainStore) UpdateKeyChains(_ context.Context, path string, addChains []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.errs[path]; err != nil {
		return nil, err
	}
	current := append([]string(nil), s.chains[path]...)
	parsed, err := apptypes.ParseChains(append(current, addChains...))
	if err != nil {
		return nil, err
	}
	updated := make([]string, len(parsed))
	for i, chain := range parsed {
		updated[i] = string(chain)
	}
	s.chains[path] = append([]string(nil), updated...)
	return updated, nil
}

func (s *chainStore) GetPublicKey(context.Context, string) ([]byte, error) {
	return nil, errors.New("GetPublicKey not expected")
}

func (s *chainStore) GetKeyChains(_ context.Context, path string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.errs[path]; err != nil {
		return nil, err
	}
	chains := s.chains[path]
	return append([]string(nil), chains...), nil
}

func (s *chainStore) ListKeys(context.Context, string) ([]string, error) {
	return nil, errors.New("ListKeys not expected")
}

func newChainAuthHandler(t *testing.T, store *chainStore, opts ...func(*config.Config)) http.Handler {
	t.Helper()
	return newChainAuthServer(t, store, opts...).Handler()
}

func newChainAuthServer(t *testing.T, store *chainStore, opts ...func(*config.Config)) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	cfg.Gateway.SwaggerAuth = false
	cfg.Gateway.ChainsCacheTTL = 30 * time.Second
	for _, opt := range opts {
		opt(&cfg)
	}
	return New(cfg, healthMock{}, store, evmMock{}, cosmosMock{})
}

func TestSignEVM_OnCosmosOnlyKey_Returns403(t *testing.T) {
	store := newChainStore()
	store.SetChains("payment/prod/alice", []string{"cosmos"})
	h := newChainAuthHandler(t, store)

	before := testutil.ToFloat64(kmsChainAuthzDenialsTotal.WithLabelValues("evm"))
	rr := doRequest(h, http.MethodPost, "/sign/evm", []byte(`{"type":"personal_message","key_path":"payment/prod/alice","personal_message":"0x6869"}`), true)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	want := `{"error":"key payment/prod/alice not authorized for evm signing (allowed chains: [cosmos])"}` + "\n"
	if got := rr.Body.String(); got != want {
		t.Fatalf("body=%q want=%q", got, want)
	}
	after := testutil.ToFloat64(kmsChainAuthzDenialsTotal.WithLabelValues("evm"))
	if after-before < 1 {
		t.Fatalf("expected denial metric to increment, before=%f after=%f", before, after)
	}
}

func TestSignCosmos_OnEvmOnlyKey_Returns403(t *testing.T) {
	store := newChainStore()
	store.SetChains("payment/prod/alice", []string{"evm"})
	h := newChainAuthHandler(t, store)

	rr := doRequest(h, http.MethodPost, "/sign/cosmos", []byte(`{"key_path":"payment/prod/alice","hrp":"mantra","sign_mode":"DIRECT","sign_doc":"`+base64.StdEncoding.EncodeToString([]byte("doc"))+`"}`), true)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	want := `{"error":"key payment/prod/alice not authorized for cosmos signing (allowed chains: [evm])"}` + "\n"
	if got := rr.Body.String(); got != want {
		t.Fatalf("body=%q want=%q", got, want)
	}
}

func TestSign_ChainsLookupTransientError_Returns503(t *testing.T) {
	store := newChainStore()
	store.SetError("payment/prod/alice", errors.New("vault timeout"))
	h := newChainAuthHandler(t, store)

	rr := doRequest(h, http.MethodPost, "/sign/evm", []byte(`{"type":"personal_message","key_path":"payment/prod/alice","personal_message":"0x6869"}`), true)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	if got, want := rr.Body.String(), "{\"error\":\"chain authorization unavailable\"}\n"; got != want {
		t.Fatalf("body=%q want=%q", got, want)
	}
}

func TestSign_PatchExpand_ThenSignSucceeds(t *testing.T) {
	store := newChainStore()
	store.SetChains("payment/prod/alice", []string{"evm"})
	h := newChainAuthHandler(t, store)

	rr := doRequest(h, http.MethodPost, "/sign/cosmos", []byte(`{"key_path":"payment/prod/alice","hrp":"mantra","sign_mode":"DIRECT","sign_doc":"`+base64.StdEncoding.EncodeToString([]byte("doc"))+`"}`), true)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("first request should deny, code=%d body=%s", rr.Code, rr.Body.String())
	}

	store.SetChains("payment/prod/alice", []string{"cosmos", "evm"})
	rr = doRequest(h, http.MethodPost, "/sign/cosmos", []byte(`{"key_path":"payment/prod/alice","hrp":"mantra","sign_mode":"DIRECT","sign_doc":"`+base64.StdEncoding.EncodeToString([]byte("doc"))+`"}`), true)
	if rr.Code != http.StatusOK {
		t.Fatalf("second request should succeed, code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPatchKeyChainsExpandOnly(t *testing.T) {
	store := newChainStore()
	store.SetChains("payment/prod/alice", []string{"evm"})
	h := newChainAuthHandler(t, store)

	t.Run("expands", func(t *testing.T) {
		rr := doRequest(h, http.MethodPatch, "/keys/payment/prod/alice", []byte(`{"add_chains":["cosmos"]}`), true)
		if rr.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
		var resp patchKeyChainsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Path != "payment/prod/alice" {
			t.Fatalf("path=%q", resp.Path)
		}
		if want := []string{"cosmos", "evm"}; len(resp.Chains) != len(want) || resp.Chains[0] != want[0] || resp.Chains[1] != want[1] {
			t.Fatalf("chains=%v want=%v", resp.Chains, want)
		}
	})

	t.Run("rejects_remove_chains", func(t *testing.T) {
		rr := doRequest(h, http.MethodPatch, "/keys/payment/prod/alice", []byte(`{"remove_chains":["evm"]}`), true)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
		if got, want := rr.Body.String(), "{\"error\":\"only add_chains is supported\"}\n"; got != want {
			t.Fatalf("body=%q want=%q", got, want)
		}
	})

	t.Run("rejects_chains_replace", func(t *testing.T) {
		rr := doRequest(h, http.MethodPatch, "/keys/payment/prod/alice", []byte(`{"chains":["evm"]}`), true)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
		if got, want := rr.Body.String(), "{\"error\":\"only add_chains is supported\"}\n"; got != want {
			t.Fatalf("body=%q want=%q", got, want)
		}
	})

	t.Run("rejects_empty_add_chains", func(t *testing.T) {
		rr := doRequest(h, http.MethodPatch, "/keys/payment/prod/alice", []byte(`{"add_chains":[]}`), true)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
		if got, want := rr.Body.String(), "{\"error\":\"chains is required and must be a non-empty subset of [evm, cosmos]\"}\n"; got != want {
			t.Fatalf("body=%q want=%q", got, want)
		}
	})

	t.Run("rejects_unknown_chain", func(t *testing.T) {
		rr := doRequest(h, http.MethodPatch, "/keys/payment/prod/alice", []byte(`{"add_chains":["solana"]}`), true)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
		if got, want := rr.Body.String(), "{\"error\":\"chains is required and must be a non-empty subset of [evm, cosmos]\"}\n"; got != want {
			t.Fatalf("body=%q want=%q", got, want)
		}
	})
}

func TestPatchKeyChainsUnauthorized(t *testing.T) {
	store := newChainStore()
	store.SetChains("payment/prod/alice", []string{"evm"})
	h := newChainAuthHandler(t, store)

	rr := doRequest(h, http.MethodPatch, "/keys/payment/prod/alice", []byte(`{"add_chains":["cosmos"]}`), false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPatchKeyChainsInvalidatesCache(t *testing.T) {
	store := newChainStore()
	store.SetChains("payment/prod/alice", []string{"evm"})
	server := newChainAuthServer(t, store)
	h := server.Handler()

	rr := doRequest(h, http.MethodPost, "/sign/evm", []byte(`{"type":"personal_message","key_path":"payment/prod/alice","personal_message":"0x6869"}`), true)
	if rr.Code != http.StatusOK {
		t.Fatalf("prime cache code=%d body=%s", rr.Code, rr.Body.String())
	}

	if _, _, ok := server.chains.get("payment/prod/alice", time.Now()); !ok {
		t.Fatal("expected cache entry to be primed")
	}

	rr = doRequest(h, http.MethodPatch, "/keys/payment/prod/alice", []byte(`{"add_chains":["cosmos"]}`), true)
	if rr.Code != http.StatusOK {
		t.Fatalf("patch code=%d body=%s", rr.Code, rr.Body.String())
	}

	if _, _, ok := server.chains.get("payment/prod/alice", time.Now()); ok {
		t.Fatal("expected cache entry to be invalidated")
	}
}
