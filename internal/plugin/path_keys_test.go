package kmsplugin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hashicorp/vault/sdk/logical"
)

func writeKeyWithData(t *testing.T, b *backend, storage logical.Storage, name string, data map[string]interface{}) (*logical.Response, error) {
	t.Helper()
	return b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.CreateOperation,
		Path:      "keys/" + name,
		Storage:   storage,
		Data:      data,
	})
}

func writeKeyWithUpdate(t *testing.T, b *backend, storage logical.Storage, name string, data map[string]interface{}) (*logical.Response, error) {
	t.Helper()
	return b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "keys/" + name,
		Storage:   storage,
		Data:      data,
	})
}

func TestCreateKeyPersistsCanonicalChains(t *testing.T) {
	b, storage := testBackend(t)

	resp, err := writeKeyWithData(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
		"chains": "cosmos,EVM,cosmos",
	})
	if err != nil || resp == nil || resp.IsError() {
		t.Fatalf("create err=%v resp=%v", err, resp)
	}

	got, ok := resp.Data["chains"].([]string)
	if !ok {
		t.Fatalf("chains response type = %T, want []string", resp.Data["chains"])
	}
	want := []string{"cosmos", "evm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chains = %v, want %v", got, want)
	}
}

func TestCreateKeyRejectsInvalidChains(t *testing.T) {
	b, storage := testBackend(t)
	cases := []struct {
		name   string
		chains string
	}{
		{name: "empty", chains: ""},
		{name: "unknown", chains: "evm,solana"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := writeKeyWithData(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
				"chains": tc.chains,
			})
			if !errors.Is(err, logical.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got err=%v resp=%v", err, resp)
			}
			if resp == nil || !resp.IsError() {
				t.Fatalf("expected error response, got %v", resp)
			}
			if msg := resp.Error().Error(); !strings.Contains(msg, "chains is required and must be a non-empty subset of [evm, cosmos]") {
				t.Fatalf("expected subset error, got %q", msg)
			}
		})
	}
}

func TestCreateKeyRejectsMismatchedIdempotentChains(t *testing.T) {
	b, storage := testBackend(t)

	created, err := writeKeyWithData(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
		"chains": "evm",
	})
	if err != nil || created == nil || created.IsError() {
		t.Fatalf("initial create err=%v resp=%v", err, created)
	}

	resp, err := writeKeyWithData(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
		"chains": "cosmos",
	})
	if !errors.Is(err, logical.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got err=%v resp=%v", err, resp)
	}
	if resp == nil || !resp.IsError() {
		t.Fatalf("expected error response, got %v", resp)
	}
	if msg := resp.Error().Error(); !strings.Contains(msg, "chains mismatch on idempotent create") {
		t.Fatalf("expected mismatch error, got %q", msg)
	}
}

func TestCreateKeyBackfillsLegacyEmptyChains(t *testing.T) {
	b, storage := testBackend(t)

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
	entry, err := logical.StorageEntryJSON("keys/proj-a/prod/alice", legacy)
	if err != nil {
		t.Fatalf("legacy storage entry: %v", err)
	}
	if err := storage.Put(context.Background(), entry); err != nil {
		t.Fatalf("store legacy key: %v", err)
	}

	resp, err := writeKeyWithData(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
		"chains": "cosmos,EVM,cosmos",
	})
	if err != nil || resp == nil || resp.IsError() {
		t.Fatalf("backfill err=%v resp=%v", err, resp)
	}

	got, ok := resp.Data["chains"].([]string)
	if !ok {
		t.Fatalf("chains response type = %T, want []string", resp.Data["chains"])
	}
	want := []string{"cosmos", "evm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chains = %v, want %v", got, want)
	}
	if resp.Data["evm_address"] != legacy.EVMAddress {
		t.Fatalf("evm_address changed during backfill: %v vs %v", resp.Data["evm_address"], legacy.EVMAddress)
	}

	stored, err := storage.Get(context.Background(), "keys/proj-a/prod/alice")
	if err != nil {
		t.Fatalf("load backfilled key: %v", err)
	}
	var gotEntry KeyEntry
	if err := stored.DecodeJSON(&gotEntry); err != nil {
		t.Fatalf("decode backfilled key: %v", err)
	}
	if !reflect.DeepEqual(gotEntry.Chains, want) {
		t.Fatalf("stored chains = %v, want %v", gotEntry.Chains, want)
	}
}

func TestCreateKeyValidatesPath(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		key    string
		errSub string
	}{
		{"uppercase", "Proj-A/prod/alice", "segments must match"},
		{"two-segments", "proj-a/prod", "format {project}/{environment}/{username}"},
		{"empty-segment", "proj-a//alice", "segments must not be empty"},
		{"dotdot", "proj-a/prod/..", "segments must match"},
		{"four-segments", "proj-a/prod/alice/extra", "format {project}/{environment}/{username}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := b.HandleRequest(ctx, &logical.Request{
				Operation: logical.CreateOperation,
				Path:      "keys/" + tc.key,
				Storage:   storage,
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

	// Happy path still works.
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.CreateOperation,
		Path:      "keys/proj-a/prod/alice",
		Storage:   storage,
		Data:      map[string]interface{}{"chains": "evm"},
	})
	if err != nil || resp == nil || resp.IsError() {
		t.Fatalf("happy create err=%v resp=%v", err, resp)
	}

}

func TestCreateKeyViaUpdateOperationAcceptsChains(t *testing.T) {
	b, storage := testBackend(t)
	resp, err := writeKeyWithUpdate(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
		"chains": "evm",
	})
	if err != nil || resp == nil || resp.IsError() {
		t.Fatalf("update create err=%v resp=%v", err, resp)
	}
	got, ok := resp.Data["chains"].([]string)
	if !ok {
		t.Fatalf("chains response type = %T, want []string", resp.Data["chains"])
	}
	want := []string{"evm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chains = %v, want %v", got, want)
	}
}

func TestListKeysValidatesPrefix(t *testing.T) {
	b, storage := testBackend(t)
	ctx := context.Background()

	// Malformed prefix rejected.
	resp, err := b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ListOperation,
		Path:      "keys/Proj A/",
		Storage:   storage,
	})
	if !errors.Is(err, logical.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got err=%v resp=%v", err, resp)
	}
	if resp == nil || !resp.IsError() {
		t.Fatalf("expected error response, got %v", resp)
	}

	// Empty prefix lists everything.
	resp, err = b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ListOperation,
		Path:      "keys/",
		Storage:   storage,
	})
	if err != nil {
		t.Fatalf("list err=%v", err)
	}
	if resp != nil && resp.IsError() {
		t.Fatalf("unexpected error: %v", resp)
	}

	// Valid 2-segment prefix accepted.
	resp, err = b.HandleRequest(ctx, &logical.Request{
		Operation: logical.ListOperation,
		Path:      "keys/proj-a/prod/",
		Storage:   storage,
	})
	if err != nil {
		t.Fatalf("valid 2-seg prefix err=%v", err)
	}
	if resp != nil && resp.IsError() {
		t.Fatalf("unexpected error: %v", resp)
	}
}
