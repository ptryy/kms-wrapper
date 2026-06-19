package vault

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ryan-truong/kms-wrapper/pkg/types"
)

// mockPlugin emulates the kms-vault-plugin HTTP surface (kms/keys/<path>,
// kms/sign/<path>) with a single secp256k1 key generated at test startup.
type mockPlugin struct {
	priv       []byte
	compressed []byte
	uncomp     []byte
	created    bool
	chains     []string
}

func newMockPlugin(t *testing.T) *mockPlugin {
	t.Helper()
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return &mockPlugin{
		priv:       crypto.FromECDSA(priv),
		compressed: crypto.CompressPubkey(&priv.PublicKey),
		uncomp:     crypto.FromECDSAPub(&priv.PublicKey),
	}
}

func TestClientMockPlugin(t *testing.T) {
	mp := newMockPlugin(t)
	const keyPath = "proj/prod/alice"
	const vaultKeyPath = "/v1/kms/keys/" + keyPath
	const vaultSignPath = "/v1/kms/sign/" + keyPath

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sys/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"initialized": true})
		case vaultKeyPath:
			switch r.Method {
			case http.MethodPost, http.MethodPut:
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				extract := func(v any) []string {
					switch got := v.(type) {
					case string:
						if got == "" {
							return nil
						}
						return strings.Split(got, ",")
					case []any:
						out := make([]string, 0, len(got))
						for _, chain := range got {
							if s, ok := chain.(string); ok {
								out = append(out, s)
							}
						}
						return out
					default:
						return nil
					}
				}
				if got := extract(body["add_chains"]); len(got) > 0 {
					added := got
					seen := map[string]bool{}
					for _, chain := range append(mp.chains, added...) {
						seen[chain] = true
					}
					mp.chains = mp.chains[:0]
					for chain := range seen {
						mp.chains = append(mp.chains, chain)
					}
					sort.Strings(mp.chains)
				} else if got := extract(body["chains"]); len(got) > 0 {
					mp.chains = mp.chains[:0]
					mp.chains = append(mp.chains, got...)
					mp.created = true
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
					"compressed_pub_key": base64.StdEncoding.EncodeToString(mp.compressed),
					"evm_address":        "0x0000000000000000000000000000000000000000",
					"source":             "generated",
					"created_at":         "2026-01-01T00:00:00Z",
					"chains":             mp.chains,
				}})
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
					"compressed_pub_key": base64.StdEncoding.EncodeToString(mp.compressed),
					"evm_address":        "0x0000000000000000000000000000000000000000",
					"source":             "generated",
					"created_at":         "2026-01-01T00:00:00Z",
					"chains":             mp.chains,
				}})
			default:
				http.NotFound(w, r)
			}
		case vaultSignPath:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["chain"] != "evm" {
				http.Error(w, "missing chain", http.StatusBadRequest)
				return
			}
			digest, _ := hex.DecodeString(body["input"])
			priv, _ := crypto.ToECDSA(mp.priv)
			sig, err := crypto.Sign(digest, priv)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"r": hex.EncodeToString(sig[0:32]),
				"s": hex.EncodeToString(sig[32:64]),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c, err := NewClient(ts.URL, TokenAuthProvider{TokenValue: "root"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.CreateKey(ctx, keyPath, []string{"evm", "cosmos"}); err != nil || !mp.created {
		t.Fatalf("CreateKey err=%v created=%v", err, mp.created)
	}
	if got, want := len(mp.chains), 2; got != want {
		t.Fatalf("CreateKey chains len=%d, want %d", got, want)
	}

	got, err := c.GetPublicKey(ctx, keyPath)
	if err != nil {
		t.Fatalf("GetPublicKey err=%v", err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(mp.uncomp) {
		t.Fatalf("GetPublicKey returned %x, want %x", got, mp.uncomp)
	}

	chains, err := c.GetKeyChains(ctx, keyPath)
	if err != nil {
		t.Fatalf("GetKeyChains err=%v", err)
	}
	if len(chains) != 2 || chains[0] != "evm" || chains[1] != "cosmos" {
		t.Fatalf("GetKeyChains returned %v", chains)
	}

	updated, err := c.UpdateKeyChains(ctx, keyPath, []string{"evm"})
	if err != nil {
		t.Fatalf("UpdateKeyChains err=%v", err)
	}
	if len(updated) != 2 || updated[0] != "cosmos" || updated[1] != "evm" {
		t.Fatalf("UpdateKeyChains returned %v", updated)
	}

	digest := crypto.Keccak256([]byte("msg"))
	r, s, err := c.Sign(ctx, keyPath, digest, "evm")
	if err != nil || r.Sign() == 0 || s.Sign() == 0 {
		t.Fatalf("Sign r=%v s=%v err=%v", r, s, err)
	}
	// Sanity check: r,s must recover to the stored public key for one of v in {0,1}.
	sig := make([]byte, 65)
	r.FillBytes(sig[0:32])
	s.FillBytes(sig[32:64])
	matched := false
	for v := byte(0); v < 2 && !matched; v++ {
		sig[64] = v
		recovered, err := crypto.SigToPub(digest, sig)
		if err == nil && hex.EncodeToString(crypto.FromECDSAPub(recovered)) == hex.EncodeToString(mp.uncomp) {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("signature does not recover to mock pubkey")
	}
}

func TestSignRequiresHashLength(t *testing.T) {
	c := &Client{}
	if _, _, err := c.Sign(context.Background(), "proj/prod/alice", []byte{1}, "evm"); err == nil || err.Error() != "payload must be 32 bytes (pre-hashed)" {
		t.Fatalf("unexpected err %v", err)
	}
}

// A missing key must be recorded as not_found, not ok: the read succeeds at the
// HTTP layer but the method returns ErrNotFound on a nil secret.
func TestRecordsNotFoundOnMissingKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sys/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"initialized": true})
			return
		}
		// Empty-body 404 so the Vault client returns a nil secret (key missing).
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c, err := NewClient(ts.URL, TokenAuthProvider{TokenValue: "root"})
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	c.SetVaultCallObserver(func(op, status string, _ float64) {
		statuses = append(statuses, op+":"+status)
	})

	if _, err := c.GetKeyChains(context.Background(), "proj/prod/ghost"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if want := "read:not_found"; len(statuses) != 1 || statuses[0] != want {
		t.Fatalf("recorded %v, want [%s]", statuses, want)
	}
}
