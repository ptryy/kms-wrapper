package vault

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestClientMockVault(t *testing.T) {
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub := crypto.FromECDSAPub(&priv.PublicKey)
	var created bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sys/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"initialized": true})
			return
		}
		if r.URL.Path == "/v1/transit/keys/proj/evm/alice" {
			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				created = true
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"keys": map[string]any{"1": map[string]any{"public_key": hex.EncodeToString(pub)}}}})
			return
		}
		if r.URL.Path == "/v1/transit/sign/proj/evm/alice" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			hash, _ := base64.StdEncoding.DecodeString(body["input"])
			rs, ss, _ := ecdsa.Sign(rand.Reader, priv, hash)
			der, _ := asn1.Marshal(struct{ R, S *big.Int }{rs, ss})
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"signature": "vault:v1:" + base64.StdEncoding.EncodeToString(der)}})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	c, err := NewClient(ts.URL, TokenAuthProvider{TokenValue: "root"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.CreateKey(ctx, "proj/evm/alice"); err != nil || !created {
		t.Fatalf("CreateKey err=%v created=%v", err, created)
	}
	got, err := c.GetPublicKey(ctx, "proj/evm/alice")
	if err != nil || hex.EncodeToString(got) != hex.EncodeToString(pub) {
		t.Fatalf("GetPublicKey got %x err %v", got, err)
	}
	r, s, err := c.Sign(ctx, "proj/evm/alice", crypto.Keccak256([]byte("msg")))
	if err != nil || r.Sign() == 0 || s.Sign() == 0 {
		t.Fatalf("Sign r=%v s=%v err=%v", r, s, err)
	}
}

func TestSignRequiresHashLength(t *testing.T) {
	c := &Client{}
	if _, _, err := c.Sign(context.Background(), "proj/evm/alice", []byte{1}); err == nil || err.Error() != "payload must be 32 bytes (pre-hashed)" {
		t.Fatalf("unexpected err %v", err)
	}
}
