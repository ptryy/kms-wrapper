package kmsplugin

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hashicorp/vault/sdk/logical"
)

func TestSignValidatesPath(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	digest := crypto.Keccak256([]byte("payload"))

	cases := []struct {
		name   string
		key    string
		errSub string
	}{
		{"uppercase", "Proj/prod/alice", "segments must match"},
		{"two-segments", "proj/prod", "format {project}/{environment}/{username}"},
		{"empty-segment", "proj//alice", "segments must not be empty"},
		{"dotdot", "proj/prod/..", "segments must match"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := b.HandleRequest(ctx, &logical.Request{
				Operation: logical.UpdateOperation,
				Path:      "sign/" + tc.key,
				Storage:   storage,
				Data:      map[string]interface{}{"input": hex.EncodeToString(digest)},
			})
			if !errors.Is(err, logical.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got err=%v resp=%v", err, resp)
			}
			if resp == nil || !resp.IsError() {
				t.Fatalf("expected error response, got %v", resp)
			}
			if msg := resp.Error().Error(); !strings.Contains(msg, tc.errSub) {
				t.Fatalf("expected error %q to contain %q", msg, tc.errSub)
			}
		})
	}
}

func TestSign_RequiresChainField(t *testing.T) {
	b, storage := testBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice")

	resp, err := signHash(t, b, storage, "proj-a/prod/alice", "")
	if !errors.Is(err, logical.ErrInvalidRequest) {
		t.Fatalf("want invalid request, got err=%v resp=%v", err, resp)
	}
	if resp == nil || !resp.IsError() || !strings.Contains(resp.Error().Error(), "chain is required") {
		t.Fatalf("want chain-required error, got err=%v resp=%v", err, resp)
	}
}

func TestSign_AllowedChainSucceeds(t *testing.T) {
	b, storage := testBackend(t)
	writeKeyWithChains(t, b, storage, "proj-a/prod/alice", "evm,cosmos")

	resp, err := signHash(t, b, storage, "proj-a/prod/alice", "cosmos")
	if err != nil || resp == nil || resp.IsError() {
		t.Fatalf("expected sign success, got err=%v resp=%v", err, resp)
	}
}

func TestSign_DisallowedChainDenied(t *testing.T) {
	b, storage := testBackend(t)
	writeKeyWithChains(t, b, storage, "proj-a/prod/alice", "evm")

	resp, err := signHash(t, b, storage, "proj-a/prod/alice", "cosmos")
	if !errors.Is(err, logical.ErrPermissionDenied) {
		t.Fatalf("want permission denied, got err=%v resp=%v", err, resp)
	}
	if resp == nil || !resp.IsError() || !strings.Contains(resp.Error().Error(), "not authorized for cosmos signing (allowed chains: [evm])") {
		t.Fatalf("want chain-denied permission error, got err=%v resp=%v", err, resp)
	}
}

func TestSign_LegacyEntryFailsClosed(t *testing.T) {
	b, storage := testBackend(t)
	writeRawKeyEntryWithoutChains(t, storage, "proj-a/prod/legacy")

	resp, err := signHash(t, b, storage, "proj-a/prod/legacy", "evm")
	if !errors.Is(err, logical.ErrPermissionDenied) {
		t.Fatalf("want permission denied, got err=%v resp=%v", err, resp)
	}
	if resp == nil || !resp.IsError() || !strings.Contains(resp.Error().Error(), "allowed chains: []") {
		t.Fatalf("want fail-closed denial, got err=%v resp=%v", err, resp)
	}
}

func signHash(t *testing.T, b *backend, storage logical.Storage, name, chain string) (*logical.Response, error) {
	t.Helper()
	digest := crypto.Keccak256([]byte("payload"))
	data := map[string]interface{}{
		"input": hex.EncodeToString(digest),
	}
	if chain != "" {
		data["chain"] = chain
	}
	return b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "sign/" + name,
		Storage:   storage,
		Data:      data,
	})
}

func writeKeyWithChains(t *testing.T, b *backend, storage logical.Storage, name, chains string) *logical.Response {
	t.Helper()
	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.CreateOperation,
		Path:      "keys/" + name,
		Storage:   storage,
		Data: map[string]interface{}{
			"chains": chains,
		},
	})
	if err != nil {
		t.Fatalf("create key %q: %v", name, err)
	}
	if resp == nil || resp.IsError() {
		t.Fatalf("create key %q returned error response: %v", name, resp)
	}
	return resp
}

func writeRawKeyEntryWithoutChains(t *testing.T, storage logical.Storage, name string) {
	t.Helper()
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate legacy key: %v", err)
	}
	legacy := &KeyEntry{
		PrivateKey:       crypto.FromECDSA(priv),
		CompressedPubKey: crypto.CompressPubkey(&priv.PublicKey),
		EVMAddress:       crypto.PubkeyToAddress(priv.PublicKey).Hex(),
		Source:           "generated",
		CreatedAt:        time.Now().UTC(),
	}
	entry, err := logical.StorageEntryJSON("keys/"+name, legacy)
	if err != nil {
		t.Fatalf("legacy storage entry: %v", err)
	}
	if err := storage.Put(context.Background(), entry); err != nil {
		t.Fatalf("store legacy key: %v", err)
	}
}
