package kmsplugin

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hashicorp/vault/sdk/logical"
)

func testBackend(t *testing.T) (*backend, logical.Storage) {
	t.Helper()
	config := logical.TestBackendConfig()
	config.StorageView = &logical.InmemStorage{}
	b := newBackend()
	if err := b.Setup(context.Background(), config); err != nil {
		t.Fatalf("backend setup: %v", err)
	}
	return b, config.StorageView
}

func writeKey(t *testing.T, b *backend, storage logical.Storage, name string) *logical.Response {
	t.Helper()
	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.CreateOperation,
		Path:      "keys/" + name,
		Storage:   storage,
	})
	if err != nil {
		t.Fatalf("create key %q: %v", name, err)
	}
	if resp == nil || resp.IsError() {
		t.Fatalf("create key %q returned error response: %v", name, resp)
	}
	return resp
}

func TestCreateReadDeleteRoundTrip(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	const name = "proj/evm/alice"

	created := writeKey(t, b, storage, name)
	if _, ok := created.Data["compressed_pub_key"].(string); !ok {
		t.Fatalf("create response missing compressed_pub_key: %v", created.Data)
	}
	addr, _ := created.Data["evm_address"].(string)
	if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
		t.Fatalf("create response evm_address invalid: %q", addr)
	}
	if src, _ := created.Data["source"].(string); src != "generated" {
		t.Fatalf("expected source=generated, got %q", src)
	}

	// Read returns same fields, no private key.
	read, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ReadOperation,
		Path:      "keys/" + name,
		Storage:   storage,
	})
	if err != nil || read == nil || read.IsError() {
		t.Fatalf("read key: err=%v resp=%v", err, read)
	}
	if _, leaked := read.Data["private_key"]; leaked {
		t.Fatalf("read response leaked private_key")
	}
	if read.Data["evm_address"] != created.Data["evm_address"] {
		t.Fatalf("evm_address differs across create/read: %v vs %v", created.Data["evm_address"], read.Data["evm_address"])
	}

	// Idempotent create returns the same key.
	again := writeKey(t, b, storage, name)
	if again.Data["evm_address"] != created.Data["evm_address"] {
		t.Fatalf("idempotent create generated a new key: %v vs %v", created.Data["evm_address"], again.Data["evm_address"])
	}

	// Delete.
	del, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.DeleteOperation,
		Path:      "keys/" + name,
		Storage:   storage,
	})
	if err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if del != nil && del.IsError() {
		t.Fatalf("delete returned error: %v", del)
	}

	// Read after delete -> nil response (not found).
	gone, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ReadOperation,
		Path:      "keys/" + name,
		Storage:   storage,
	})
	if err != nil {
		t.Fatalf("post-delete read: %v", err)
	}
	if gone != nil {
		t.Fatalf("expected nil response after delete, got %v", gone.Data)
	}
}

func TestListKeys(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	for _, name := range []string{"proj-a/evm/alice", "proj-a/evm/bob", "proj-b/cosmos/carol"} {
		writeKey(t, b, storage, name)
	}

	// List under "proj-a/evm/" -> alice, bob.
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ListOperation,
		Path:      "keys/proj-a/evm/",
		Storage:   storage,
	})
	if err != nil || resp == nil {
		t.Fatalf("list keys: err=%v resp=%v", err, resp)
	}
	got, _ := resp.Data["keys"].([]string)
	if len(got) != 2 {
		t.Fatalf("expected 2 keys under proj-a/evm/, got %v", got)
	}
}

func TestSignRoundTrip(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	const name = "proj/evm/alice"
	created := writeKey(t, b, storage, name)
	compressedB64, _ := created.Data["compressed_pub_key"].(string)
	compressed, err := base64.StdEncoding.DecodeString(compressedB64)
	if err != nil || len(compressed) != 33 {
		t.Fatalf("decode compressed pubkey: %v len=%d", err, len(compressed))
	}

	digest := crypto.Keccak256([]byte("hello world"))
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "sign/" + name,
		Storage:   storage,
		Data: map[string]interface{}{
			"input": hex.EncodeToString(digest),
		},
	})
	if err != nil || resp == nil || resp.IsError() {
		t.Fatalf("sign: err=%v resp=%v", err, resp)
	}
	rHex, _ := resp.Data["r"].(string)
	sHex, _ := resp.Data["s"].(string)
	rBytes, err := hex.DecodeString(rHex)
	if err != nil || len(rBytes) != 32 {
		t.Fatalf("decode r: %v len=%d", err, len(rBytes))
	}
	sBytes, err := hex.DecodeString(sHex)
	if err != nil || len(sBytes) != 32 {
		t.Fatalf("decode s: %v len=%d", err, len(sBytes))
	}

	// Recover pubkey from (r,s,v) and assert it matches the stored compressed pubkey.
	sig := make([]byte, 65)
	copy(sig[0:32], rBytes)
	copy(sig[32:64], sBytes)
	matched := false
	for v := byte(0); v < 2; v++ {
		sig[64] = v
		recovered, err := crypto.SigToPub(digest, sig)
		if err != nil {
			continue
		}
		recoveredCompressed := crypto.CompressPubkey(recovered)
		if hex.EncodeToString(recoveredCompressed) == hex.EncodeToString(compressed) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("signature does not recover to stored public key")
	}
}

func TestSignRejectsBadInput(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	const name = "proj/evm/alice"
	writeKey(t, b, storage, name)

	cases := []struct {
		desc string
		data map[string]interface{}
	}{
		{"missing input", map[string]interface{}{}},
		{"non-hex input", map[string]interface{}{"input": "not-hex"}},
		{"short input", map[string]interface{}{"input": hex.EncodeToString([]byte{0x01, 0x02})}},
		{"long input", map[string]interface{}{"input": hex.EncodeToString(make([]byte, 64))}},
	}
	for _, tc := range cases {
		resp, err := b.HandleRequest(ctx, &logical.Request{
			Operation: logical.UpdateOperation,
			Path:      "sign/" + name,
			Storage:   storage,
			Data:      tc.data,
		})
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tc.desc, err)
		}
		if resp == nil || !resp.IsError() {
			t.Fatalf("%s: expected error response, got %v", tc.desc, resp)
		}
	}
}

func TestSignUnknownKey(t *testing.T) {
	b, storage := testBackend(t)
	digest := crypto.Keccak256([]byte("x"))
	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "sign/does/not/exist",
		Storage:   storage,
		Data:      map[string]interface{}{"input": hex.EncodeToString(digest)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || !resp.IsError() {
		t.Fatalf("expected error response for unknown key, got %v", resp)
	}
}
