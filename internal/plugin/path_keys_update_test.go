package kmsplugin

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/vault/sdk/logical"
)

func updateChains(t *testing.T, b *backend, storage logical.Storage, name string, data map[string]interface{}) *logical.Response {
	t.Helper()
	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "keys/" + name,
		Storage:   storage,
		Data:      data,
	})
	if err != nil {
		t.Fatalf("update chains %q: %v", name, err)
	}
	if resp == nil || resp.IsError() {
		t.Fatalf("update chains %q returned error response: %v", name, resp)
	}
	return resp
}

func updateChainsErr(t *testing.T, b *backend, storage logical.Storage, name string, data map[string]interface{}) (*logical.Response, error) {
	t.Helper()
	return b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "keys/" + name,
		Storage:   storage,
		Data:      data,
	})
}

func readEntry(t *testing.T, storage logical.Storage, name string) *KeyEntry {
	t.Helper()
	raw, err := storage.Get(context.Background(), "keys/"+name)
	if err != nil {
		t.Fatalf("load key %q: %v", name, err)
	}
	if raw == nil {
		t.Fatalf("expected key %q to exist", name)
	}
	var entry KeyEntry
	if err := raw.DecodeJSON(&entry); err != nil {
		t.Fatalf("decode key %q: %v", name, err)
	}
	return &entry
}

func TestUpdateChainsExpands(t *testing.T) {
	b, storage := testBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice")

	resp := updateChains(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
		"add_chains": "cosmos",
	})
	got, ok := resp.Data["chains"].([]string)
	if !ok {
		t.Fatalf("chains response type = %T, want []string", resp.Data["chains"])
	}
	if want := []string{"cosmos", "evm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chains = %v, want %v", got, want)
	}
	if resp.Data["path"] != "proj-a/prod/alice" {
		t.Fatalf("path = %v, want proj-a/prod/alice", resp.Data["path"])
	}
	if stored := readEntry(t, storage, "proj-a/prod/alice"); !reflect.DeepEqual(stored.Chains, []string{"cosmos", "evm"}) {
		t.Fatalf("stored chains = %v, want [cosmos evm]", stored.Chains)
	}
}

func TestUpdateChainsIdempotentNoop(t *testing.T) {
	b, storage := testBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice")

	resp := updateChains(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
		"add_chains": "evm",
	})
	got, ok := resp.Data["chains"].([]string)
	if !ok {
		t.Fatalf("chains response type = %T, want []string", resp.Data["chains"])
	}
	if want := []string{"evm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chains = %v, want %v", got, want)
	}
	if resp.Data["path"] != "proj-a/prod/alice" {
		t.Fatalf("path = %v, want proj-a/prod/alice", resp.Data["path"])
	}
	if stored := readEntry(t, storage, "proj-a/prod/alice"); !reflect.DeepEqual(stored.Chains, []string{"evm"}) {
		t.Fatalf("stored chains = %v, want [evm]", stored.Chains)
	}
}

func TestUpdateChainsRejectsRemoveAndReplace(t *testing.T) {
	b, storage := testBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice")

	cases := []struct {
		body   map[string]interface{}
		errSub string
	}{
		{body: map[string]interface{}{"remove_chains": "evm"}, errSub: "only add_chains is supported"},
		{body: map[string]interface{}{"chains": "cosmos"}, errSub: "chains mismatch on idempotent create"},
		{body: map[string]interface{}{"add_chains": "cosmos", "remove_chains": "evm"}, errSub: "only add_chains is supported"},
	}
	for _, tc := range cases {
		resp, err := updateChainsErr(t, b, storage, "proj-a/prod/alice", tc.body)
		if !errors.Is(err, logical.ErrInvalidRequest) {
			t.Fatalf("want ErrInvalidRequest for %v, got err=%v resp=%v", tc.body, err, resp)
		}
		if resp == nil || !resp.IsError() {
			t.Fatalf("expected error response for %v, got %v", tc.body, resp)
		}
		if msg := resp.Error().Error(); !strings.Contains(msg, tc.errSub) {
			t.Fatalf("expected error containing %q for %v, got %q", tc.errSub, tc.body, msg)
		}
	}
}

func TestUpdateChainsConcurrentNoLostUpdate(t *testing.T) {
	b, storage := testBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice")

	var wg sync.WaitGroup
	for _, add := range []string{"cosmos", "evm"} {
		wg.Add(1)
		go func(add string) {
			defer wg.Done()
			_, _ = updateChainsErr(t, b, storage, "proj-a/prod/alice", map[string]interface{}{
				"add_chains": add,
			})
		}(add)
	}
	wg.Wait()

	if stored := readEntry(t, storage, "proj-a/prod/alice"); !reflect.DeepEqual(stored.Chains, []string{"cosmos", "evm"}) {
		t.Fatalf("lost update: chains = %v", stored.Chains)
	}
}

func TestUpdateChainsWorksForKeyNamedChains(t *testing.T) {
	b, storage := testBackend(t)
	writeKey(t, b, storage, "proj-a/prod/chains")

	resp := updateChains(t, b, storage, "proj-a/prod/chains", map[string]interface{}{
		"add_chains": "cosmos",
	})
	got, ok := resp.Data["chains"].([]string)
	if !ok {
		t.Fatalf("chains response type = %T, want []string", resp.Data["chains"])
	}
	if want := []string{"cosmos", "evm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chains = %v, want %v", got, want)
	}
	if resp.Data["path"] != "proj-a/prod/chains" {
		t.Fatalf("path = %v, want proj-a/prod/chains", resp.Data["path"])
	}
	if stored := readEntry(t, storage, "proj-a/prod/chains"); !reflect.DeepEqual(stored.Chains, []string{"cosmos", "evm"}) {
		t.Fatalf("stored chains = %v, want [cosmos evm]", stored.Chains)
	}
}
