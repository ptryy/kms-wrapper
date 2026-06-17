# add-key-chain-capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit, persisted, plugin-enforced per-key `chains` capability tag (`["evm"|"cosmos"]`), set at create, enforced with HTTP 403 at every sign boundary, expandable via `PATCH /keys/{path}` — hardened for resilience (TTL'd cache + re-validate-on-deny, fail-safe lookups, per-key locked updates, resilient list, denial metrics).

**Architecture:** Bottom-up. The plugin (`internal/plugin`) is the authoritative trust boundary; the gateway (`internal/gateway`) is the fast-fail layer. A closed-set `Chain` enum in `pkg/types` is the single source of truth for membership. Threading: gateway fast-fail check → signer → `vault.Sign(..., chain)` → plugin re-checks.

**Tech Stack:** Go 1.25+, HashiCorp Vault plugin SDK (`framework`, `logical`), `swaggo/swag` v2, Prometheus client.

## Global Constraints

- Closed set is exactly `{"evm", "cosmos"}`. Unknown members rejected everywhere with the verbatim message `chains is required and must be a non-empty subset of [evm, cosmos]` (create) / `add_chains is required and must be a non-empty subset of [evm, cosmos]` (PATCH).
- Canonical form: lowercase, deduped, sorted. All comparisons are equality/`slices.Contains` over canonical lists.
- Sign-mismatch error body verbatim: `key <path> not authorized for <chain> signing (allowed chains: [<sorted-list>])`.
- Expand-only: any shrink/replace → HTTP 400 `only add_chains is supported`.
- Response shape (D6): `evm_address` present iff `evm` in tag; `cosmos_address` present iff `cosmos` in tag; `public_key_hex` and `chains` always present.
- Legacy entries with no persisted `Chains` → treated as `[]` → fail closed (every sign 403).
- Depends on `update-key-path-scheme` already landed — use `{project}/{environment}/{username}` paths (`payment/prod/alice`) in all examples/fixtures.
- `chain` lookup transient error at sign time → HTTP 503, signer NOT invoked (fail-safe).
- Use plugin-reality terms (`kms/...`), never Transit.

---

### Task 1: `Chain` type and `ParseChains` helper

**Files:**
- Modify: `pkg/types/types.go`
- Test: `pkg/types/chains_test.go` (new)

**Interfaces:**
- Produces: `type Chain string`; `const ChainEVM Chain = "evm"`, `ChainCosmos Chain = "cosmos"`; `func ParseChains(in []string) ([]Chain, error)` — lowercases, dedupes, sorts, validates closed-set membership, rejects empty; `func ChainsContain(chains []Chain, c Chain) bool`.

- [ ] **Step 1: Write the failing test**

Create `pkg/types/chains_test.go`:

```go
package types

import (
	"reflect"
	"testing"
)

func TestParseChains_Canonicalizes(t *testing.T) {
	got, err := ParseChains([]string{"cosmos", "EVM", "cosmos"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Chain{ChainCosmos, ChainEVM} // lowercased, deduped, sorted
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseChains_RejectsEmpty(t *testing.T) {
	if _, err := ParseChains(nil); err == nil {
		t.Fatal("expected error for empty chains")
	}
	if _, err := ParseChains([]string{}); err == nil {
		t.Fatal("expected error for empty slice")
	}
}

func TestParseChains_RejectsUnknown(t *testing.T) {
	if _, err := ParseChains([]string{"evm", "solana"}); err == nil {
		t.Fatal("expected error for unknown chain")
	}
}

func TestChainsContain(t *testing.T) {
	cs := []Chain{ChainEVM}
	if !ChainsContain(cs, ChainEVM) {
		t.Fatal("expected evm present")
	}
	if ChainsContain(cs, ChainCosmos) {
		t.Fatal("expected cosmos absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/types/ -run 'ParseChains|ChainsContain' -v`
Expected: FAIL — undefined `ParseChains`/`Chain`/`ChainEVM`.

- [ ] **Step 3: Implement the type and helper**

Add to `pkg/types/types.go`:

```go
// Chain is a closed-set signing-capability identifier persisted on a key.
type Chain string

const (
	ChainEVM    Chain = "evm"
	ChainCosmos Chain = "cosmos"
)

// errChainsSubset is the verbatim message used at create-time validation.
const errChainsSubset = "chains is required and must be a non-empty subset of [evm, cosmos]"

// ParseChains lowercases, dedupes, sorts, and validates closed-set membership.
// It rejects an empty result and any unknown member.
func ParseChains(in []string) ([]Chain, error) {
	seen := map[Chain]bool{}
	for _, raw := range in {
		c := Chain(strings.ToLower(strings.TrimSpace(raw)))
		switch c {
		case ChainEVM, ChainCosmos:
			seen[c] = true
		default:
			return nil, errors.New(errChainsSubset)
		}
	}
	if len(seen) == 0 {
		return nil, errors.New(errChainsSubset)
	}
	out := make([]Chain, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// ChainsContain reports whether c is in chains.
func ChainsContain(chains []Chain, c Chain) bool {
	for _, x := range chains {
		if x == c {
			return true
		}
	}
	return false
}
```

Ensure `strings`, `sort`, `errors` are imported.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/types/ -run 'ParseChains|ChainsContain' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/types/types.go pkg/types/chains_test.go
git commit -m "feat(types): add closed-set Chain enum and ParseChains helper"
```

---

### Task 2: Add `Chains` to request/response types

**Files:**
- Modify: `pkg/types/types.go` (`KeyInfo:81`, `KeyCreateRequest:88`, `KeyCreateResponse:92`, `KeyListResponse:97`)
- Test: covered by build + downstream tests.

**Interfaces:**
- Produces: `KeyCreateRequest.Chains []Chain` (`json:"chains"`); `KeyCreateResponse`/`KeyInfo` gain `Chains []Chain` (`json:"chains"`) and `EVMAddress`/`CosmosAddress` gain `,omitempty`; new `KeyListEntry{ Path string; Chains []Chain }` used by `KeyListResponse.Keys`; new `KeyUpdateChainsRequest{ AddChains []Chain }`.

- [ ] **Step 1: Edit the types**

In `pkg/types/types.go`:
- `KeyCreateRequest`: add `Chains []Chain `+"`json:\"chains\" example:\"evm,cosmos\"`".
- `KeyCreateResponse` and `KeyInfo`: add `Chains []Chain `+"`json:\"chains\"`"; change EVM/Cosmos address tags to include `,omitempty` (e.g. `json:"evm_address,omitempty"`, `json:"cosmos_address,omitempty"`).
- Replace the list element type so `KeyListResponse.Keys []KeyListEntry` where:
  ```go
  type KeyListEntry struct {
      Path   string  `json:"path"`
      Chains []Chain `json:"chains"` // null = tag read failed (see resilient list)
  }
  ```
- Add:
  ```go
  type KeyUpdateChainsRequest struct {
      AddChains []Chain `json:"add_chains" example:"cosmos"`
  }
  ```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/...`
Expected: builds (downstream call sites break — fixed in later tasks; that's expected and those packages are not built here).

- [ ] **Step 3: Commit**

```bash
git add pkg/types/types.go
git commit -m "feat(types): thread Chains through key request/response shapes"
```

---

### Task 3: Plugin persists canonical `chains` at create

**Files:**
- Modify: `internal/plugin/backend.go` (`KeyEntry:24`), `internal/plugin/path_keys.go`
- Test: `internal/plugin/path_keys_test.go`

**Interfaces:**
- Consumes: `types.ParseChains`.
- Produces: `KeyEntry.Chains []string` persisted canonical; create reads `chains` field, rejects empty/unknown with `logical.ErrInvalidRequest`; idempotent re-create requires set-equal `chains` or returns `logical.ErrInvalidRequest("chains mismatch on idempotent create")`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/plugin/path_keys_test.go` (follow the file's existing harness for building a `*backend` + `logical.Storage`):

```go
func TestCreate_PersistsCanonicalChains(t *testing.T) {
	b, storage := newTestBackend(t)
	resp := writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "cosmos,EVM,cosmos"})
	got := resp.Data["chains"].([]string)
	if !reflect.DeepEqual(got, []string{"cosmos", "evm"}) {
		t.Fatalf("chains = %v, want [cosmos evm]", got)
	}
}

func TestCreate_EmptyChainsRejected(t *testing.T) {
	b, storage := newTestBackend(t)
	_, err := writeKeyErr(b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": ""})
	if err == nil || !strings.Contains(err.Error(), "non-empty subset of [evm, cosmos]") {
		t.Fatalf("want subset error, got %v", err)
	}
}

func TestCreate_MismatchedIdempotentRejected(t *testing.T) {
	b, storage := newTestBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "evm"})
	_, err := writeKeyErr(b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "cosmos"})
	if err == nil || !strings.Contains(err.Error(), "chains mismatch on idempotent create") {
		t.Fatalf("want mismatch error, got %v", err)
	}
}
```

(Use/extend whatever helpers the file already has; `newTestBackend`, `writeKey`, `writeKeyErr` are the local conventions — if names differ, match the existing ones.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/plugin/ -run 'TestCreate_(Persists|Empty|Mismatched)' -v`
Expected: FAIL — `chains` not read/persisted.

- [ ] **Step 3: Implement**

- In `backend.go`, add to `KeyEntry`: `Chains []string `+"`json:\"chains\"`".
- Register a `chains` field on the keys `framework.Path` (TypeCommaStringSlice or TypeString parsed via `ParseChains`).
- In `path_keys.go` create logic: read `chains`, call `types.ParseChains`; on error return `logical.ErrInvalidRequest` with the message. On idempotent re-create (entry exists), compare canonical request chains to stored `Chains` with set-equality; mismatch → `logical.ErrInvalidRequest("chains mismatch on idempotent create")`; match → preserve stored. Persist canonical strings. Echo `chains` in the response `Data`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/plugin/ -run 'TestCreate_(Persists|Empty|Mismatched)' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/backend.go internal/plugin/path_keys.go internal/plugin/path_keys_test.go
git commit -m "feat(plugin): persist canonical chains tag at key create"
```

---

### Task 4: Plugin enforces `chain` at sign time (fail-closed legacy)

**Files:**
- Modify: `internal/plugin/path_sign.go` (`handleSign:37`)
- Test: `internal/plugin/path_sign_test.go`

**Interfaces:**
- Produces: sign accepts a required `chain` field (`evm`|`cosmos`); loads `KeyEntry`; if `chain ∉ Chains` returns `logical.ErrPermission` with `key <path> not authorized for <chain> signing (allowed chains: [<sorted>])`; entry with empty/missing `Chains` denies all.

- [ ] **Step 1: Write the failing tests**

Add to `internal/plugin/path_sign_test.go`:

```go
func TestSign_AllowedChainSucceeds(t *testing.T) {
	b, storage := newTestBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "evm,cosmos"})
	if _, err := signHash(b, storage, "proj-a/prod/alice", "cosmos"); err != nil {
		t.Fatalf("expected sign success, got %v", err)
	}
}

func TestSign_DisallowedChainDenied(t *testing.T) {
	b, storage := newTestBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "evm"})
	_, err := signHash(b, storage, "proj-a/prod/alice", "cosmos")
	if err == nil || !strings.Contains(err.Error(), "not authorized for cosmos signing (allowed chains: [evm])") {
		t.Fatalf("want chain-denied permission error, got %v", err)
	}
}

func TestSign_LegacyEntryFailsClosed(t *testing.T) {
	b, storage := newTestBackend(t)
	writeRawKeyEntryWithoutChains(t, storage, "proj-a/prod/legacy") // helper writes a KeyEntry with no Chains
	_, err := signHash(b, storage, "proj-a/prod/legacy", "evm")
	if err == nil || !strings.Contains(err.Error(), "allowed chains: []") {
		t.Fatalf("want fail-closed denial, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/plugin/ -run 'TestSign_' -v`
Expected: FAIL — `chain` not enforced.

- [ ] **Step 3: Implement**

In `handleSign`: register required `chain` field; after loading the `KeyEntry`, if `chain` not in `entry.Chains` (treat nil as empty), return `logical.ErrPermission` wrapping the formatted message: `fmt.Errorf("key %s not authorized for %s signing (allowed chains: [%s])", name, chain, strings.Join(entry.Chains, " "))`. Use the canonical sorted `entry.Chains`. Only proceed to sign when authorized.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/plugin/ -run 'TestSign_' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/path_sign.go internal/plugin/path_sign_test.go
git commit -m "feat(plugin): enforce chains tag at sign time, legacy fails closed"
```

---

### Task 5: Plugin `update-chains` (expand-only, per-key locked) — R-C

**Files:**
- Create: `internal/plugin/path_keys_update.go`, `internal/plugin/path_keys_update_test.go`
- Modify: `internal/plugin/backend.go` (register the path; ensure a per-key lock exists)

**Interfaces:**
- Produces: an `update-chains` write operation on `kms/keys/<name>` (sub-path or distinct field op per the plugin's routing) accepting only `add_chains`; computes `union(existing, add_chains)`, canonical; writes under per-key lock only if changed; returns `{path, chains}`. Rejects `chains`/`remove_chains`/any other field → `logical.ErrInvalidRequest("only add_chains is supported")`.

- [ ] **Step 1: Write the failing tests**

Create `internal/plugin/path_keys_update_test.go`:

```go
func TestUpdateChains_Expands(t *testing.T) {
	b, storage := newTestBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "evm"})
	resp := updateChains(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"add_chains": "cosmos"})
	if got := resp.Data["chains"].([]string); !reflect.DeepEqual(got, []string{"cosmos", "evm"}) {
		t.Fatalf("chains = %v, want [cosmos evm]", got)
	}
}

func TestUpdateChains_IdempotentNoop(t *testing.T) {
	b, storage := newTestBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "cosmos,evm"})
	resp := updateChains(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"add_chains": "evm"})
	if got := resp.Data["chains"].([]string); !reflect.DeepEqual(got, []string{"cosmos", "evm"}) {
		t.Fatalf("chains = %v, want unchanged", got)
	}
}

func TestUpdateChains_RejectsRemoveAndReplace(t *testing.T) {
	b, storage := newTestBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "evm"})
	for _, body := range []map[string]interface{}{{"remove_chains": "evm"}, {"chains": "cosmos"}} {
		if _, err := updateChainsErr(b, storage, "proj-a/prod/alice", body); err == nil ||
			!strings.Contains(err.Error(), "only add_chains is supported") {
			t.Fatalf("want only-add error for %v, got %v", body, err)
		}
	}
}

func TestUpdateChains_ConcurrentNoLostUpdate(t *testing.T) {
	b, storage := newTestBackend(t)
	writeKey(t, b, storage, "proj-a/prod/alice", map[string]interface{}{"chains": "evm"})
	var wg sync.WaitGroup
	for _, c := range []string{"cosmos", "evm"} {
		wg.Add(1)
		go func(add string) { defer wg.Done(); _, _ = updateChainsErr(b, storage, "proj-a/prod/alice", map[string]interface{}{"add_chains": add}) }(c)
	}
	wg.Wait()
	entry := readEntry(t, storage, "proj-a/prod/alice")
	if !reflect.DeepEqual(entry.Chains, []string{"cosmos", "evm"}) {
		t.Fatalf("lost update: chains = %v", entry.Chains)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/plugin/ -run 'TestUpdateChains' -v`
Expected: FAIL — operation not defined.

- [ ] **Step 3: Implement with per-key lock**

In `path_keys_update.go`: register the operation; reject any field other than `add_chains`; `ParseChains(add_chains)`; acquire the backend's per-key lock for `name` (use `locksutil`/`b.lock(name)` — if the backend has no lock map yet, add a `sync.Map` of `*sync.Mutex` keyed by name in `backend.go` and a `b.keyLock(name)` helper); read entry, union+canonicalize, write only if changed, release lock; return `{path, chains}`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/plugin/ -run 'TestUpdateChains' -race -v`
Expected: PASS (including `-race` on the concurrent test).

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/path_keys_update.go internal/plugin/path_keys_update_test.go internal/plugin/backend.go
git commit -m "feat(plugin): expand-only update-chains under per-key lock"
```

---

### Task 6: Vault client — thread chains/chain, add lookups

**Files:**
- Modify: `internal/vault/client.go` (`CreateKey:244`, `Sign:308`, add `GetKeyChains`, `UpdateKeyChains`)
- Test: `internal/vault/client_test.go`, `internal/vault/client_typed_test.go`

**Interfaces:**
- Produces:
  - `CreateKey(ctx, path string, chains []string) error`
  - `Sign(ctx, path string, hash []byte, chain string) (r, s *big.Int, err error)`
  - `GetKeyChains(ctx, path string) ([]string, error)` — reads the persisted tag
  - `UpdateKeyChains(ctx, path string, addChains []string) ([]string, error)` — returns new canonical list
- 403 from plugin (chain mismatch) maps to `types.ErrPermission` with the message preserved (existing typed mapping).

- [ ] **Step 1: Write/adjust the failing tests**

In `client_typed_test.go` add a case asserting an HTTP 403 chain-mismatch response from the (mocked) plugin maps to `errors.Is(err, types.ErrPermission)` and the message contains `not authorized for cosmos signing`. In `client_test.go`, update existing `CreateKey`/`Sign` call sites to the new signatures and add a `GetKeyChains` round-trip test against the test double.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/vault/ -v`
Expected: FAIL — signatures/methods missing.

- [ ] **Step 3: Implement**

- `CreateKey` writes `chains` in the plugin payload.
- `Sign` includes `chain` in the sign payload.
- `GetKeyChains` reads `kms/keys/<path>` and returns the `chains` field (`[]string`); not-found → `types.ErrNotFound`.
- `UpdateKeyChains` issues the `update-chains` write with `add_chains` and returns the response `chains`.
- Confirm the existing `*vaultapi.ResponseError` → `types.ErrPermission` mapping covers the 403 (no substring matching).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/vault/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/client.go internal/vault/client_test.go internal/vault/client_typed_test.go
git commit -m "feat(vault): thread chains/chain and add GetKeyChains/UpdateKeyChains"
```

---

### Task 7: Signers pass their chain to `vault.Sign`

**Files:**
- Modify: `internal/signer/evm/evm.go` (`signWithRecovery:73-74`), `internal/signer/cosmos/cosmos.go` (`:189`)
- Test: `internal/signer/evm/evm_test.go`, `internal/signer/cosmos/cosmos_test.go`

**Interfaces:**
- Consumes: `vault.Sign(ctx, path, hash, chain)`.
- Produces: EVM signer always passes `chain="evm"`; Cosmos signer always passes `chain="cosmos"`.

- [ ] **Step 1: Update the `Vault` interface + call sites and their test doubles**

Change each signer's local `Vault` interface `Sign` method to `Sign(ctx, path string, hash []byte, chain string) (r, s *big.Int, err error)`. EVM `signWithRecovery` calls `s.vault.Sign(ctx, keyPath, hash, "evm")`; Cosmos calls `s.vault.Sign(ctx, keyPath, hash[:], "cosmos")`. Update the in-test mock `Vault` implementations to the new signature and assert the chain argument.

- [ ] **Step 2: Run the signer tests**

Run: `go test ./internal/signer/... -v`
Expected: PASS (mocks accept and assert the chain).

- [ ] **Step 3: Commit**

```bash
git add internal/signer/
git commit -m "feat(signer): pass chain identifier to vault.Sign"
```

---

### Task 8: keyinfo derives only enabled-chain addresses

**Files:**
- Modify: `internal/keyinfo/keyinfo.go` (`For:25`)
- Test: `internal/keyinfo/keyinfo_test.go`

**Interfaces:**
- Produces: `For(ctx, store, path, hrp string, chains []types.Chain) (types.KeyInfo, error)` — `PublicKeyHex` always set; `EVMAddress` set iff `evm` in chains; `CosmosAddress` set iff `cosmos` in chains; result `KeyInfo.Chains = chains`.

- [ ] **Step 1: Write the failing tests**

Add three cases to `keyinfo_test.go`: `[evm]` → `CosmosAddress == ""`, `EVMAddress != ""`, `Chains == [evm]`; `[cosmos]` → mirror; `[evm,cosmos]` → both set.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/keyinfo/ -v`
Expected: FAIL — `For` signature lacks chains.

- [ ] **Step 3: Implement**

Add the `chains []types.Chain` param; guard each address derivation with `types.ChainsContain`; set `KeyInfo.Chains`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/keyinfo/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/keyinfo/keyinfo.go internal/keyinfo/keyinfo_test.go
git commit -m "feat(keyinfo): derive only the enabled chains' addresses"
```

---

### Task 9: Gateway create / show — chains in + conditional shape

**Files:**
- Modify: `internal/gateway/gateway.go` (`createKey:689`, `showKey:744`), `KeyStore` interface (in gateway.go)
- Test: `internal/gateway/keys_test.go`

**Interfaces:**
- Consumes: `keys.CreateKey(ctx, path, chains)`, `keys.GetKeyChains(ctx, path)`, `keyinfo.For(..., chains)`.
- Produces: `createKey` validates `chains` (400 on missing/empty/unknown), passes through, responds with conditional addresses + `chains` + `already_existed`; `showKey` loads chains then conditional shape.

- [ ] **Step 1: Write the failing tests**

In `keys_test.go`: create with `chains:["evm"]` → 201, body has `evm_address`, no `cosmos_address`, `chains:["evm"]`; create missing `chains` → 400 with the subset message; create `chains:["evm","solana"]` → 400; show an `[evm]` key → no `cosmos_address`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gateway/ -run 'TestCreate|TestShow' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Extend `KeyStore` interface with `CreateKey(ctx, path string, chains []string) error` and `GetKeyChains(ctx, path string) ([]string, error)`. In `createKey`: decode `chains`, `types.ParseChains`, 400 on error (verbatim message). Build response via `keyinfo.For(..., chains)`. In `showKey`: `GetKeyChains` then `keyinfo.For(..., chains)`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/gateway/ -run 'TestCreate|TestShow' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go internal/gateway/keys_test.go
git commit -m "feat(gateway): require chains on create, conditional address shape"
```

---

### Task 10: Gateway sign enforcement + R-A/R-B/R-E

**Files:**
- Create: `internal/gateway/chains_cache.go` (TTL'd cache + helper), `internal/gateway/chain_capability_test.go`
- Modify: `internal/gateway/gateway.go` (`signEVM:505`, `signCosmos:622`), `internal/gateway/metrics.go`, `internal/config` (new `gateway.chains_cache_ttl`)

**Interfaces:**
- Produces: `(s *Server) authorizeChain(ctx, path string, attempted types.Chain) (allowed []types.Chain, status int, err error)` — returns the cached tag on allow; on would-deny does one authoritative `GetKeyChains` re-fetch (R-A); on transient lookup error returns `status=503` (R-B); increments `kms_chain_authz_denials_total{chain}` on deny (R-E).

- [ ] **Step 1: Write the failing tests**

Create `chain_capability_test.go`:

```go
func TestSignEVM_OnCosmosOnlyKey_Returns403(t *testing.T) {
	srv := newTestServer(t, withKeyChains("payment/prod/alice", []string{"cosmos"}))
	resp := postJSON(t, srv, "/sign/evm", `{"type":"personal_message","key_path":"payment/prod/alice","personal_message":"0x6869"}`)
	assertStatus(t, resp, 403)
	assertBody(t, resp, `key payment/prod/alice not authorized for evm signing (allowed chains: [cosmos])`)
}

func TestSignCosmos_OnEvmOnlyKey_Returns403(t *testing.T) {
	srv := newTestServer(t, withKeyChains("payment/prod/alice", []string{"evm"}))
	resp := postJSON(t, srv, "/sign/cosmos", `{"key_path":"payment/prod/alice","hrp":"mantra","sign_mode":"DIRECT","sign_doc":"`+validDirectDoc+`"}`)
	assertStatus(t, resp, 403)
}

func TestSign_ChainsLookupTransientError_Returns503(t *testing.T) {
	srv := newTestServer(t, withKeyChainsError("payment/prod/alice", errors.New("vault timeout")))
	resp := postJSON(t, srv, "/sign/evm", `{"type":"personal_message","key_path":"payment/prod/alice","personal_message":"0x6869"}`)
	assertStatus(t, resp, 503)
	assertBody(t, resp, "chain authorization unavailable")
}

func TestSign_PatchExpand_ThenSignSucceeds(t *testing.T) {
	srv := newTestServer(t, withKeyChains("payment/prod/alice", []string{"evm"}))
	// warm the cache with a deny
	_ = postJSON(t, srv, "/sign/cosmos", cosmosBody("payment/prod/alice"))
	srv.store.SetChains("payment/prod/alice", []string{"cosmos", "evm"}) // simulate PATCH on another replica
	resp := postJSON(t, srv, "/sign/cosmos", cosmosBody("payment/prod/alice"))
	assertStatus(t, resp, 200) // re-validate-on-deny picks up the new tag
}
```

(Use the gateway test suite's existing server/store doubles; the `with*`/`postJSON`/`assert*` helpers mirror existing patterns — match real names.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gateway/ -run 'TestSign(EVM|Cosmos)_On|TestSign_(ChainsLookup|PatchExpand)' -v`
Expected: FAIL.

- [ ] **Step 3: Implement the cache + helper**

`chains_cache.go`: a `map[string]struct{chains []types.Chain; at time.Time}` guarded by `sync.RWMutex`, TTL from `cfg.Gateway.ChainsCacheTTL` (default 30s). `authorizeChain`:
1. read cache; if fresh and contains `attempted` → return allowed.
2. if fresh and does NOT contain `attempted` → **re-fetch** `keys.GetKeyChains` (R-A). On success, refresh cache, re-evaluate.
3. if cache miss/stale → fetch `keys.GetKeyChains`, store.
4. lookup transient error (not `ErrNotFound`) → return `status=503` (R-B).
5. still not allowed → `kms_chain_authz_denials_total{chain=attempted}.Inc()` (R-E), return deny.

In `signEVM`/`signCosmos`, after decoding the body and before dispatch, call `authorizeChain(ctx, key_path, "evm"/"cosmos")`; on deny write 403 + the verbatim body + `slog.WarnContext(key_path, attempted_chain, allowed_chains)`; on 503 write `{"error":"chain authorization unavailable"}`.

Add the metric in `metrics.go`. Add `ChainsCacheTTL time.Duration` to gateway config (env `KMS_GATEWAY_CHAINS_CACHE_TTL`, default 30s).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/gateway/ -run 'TestSign(EVM|Cosmos)_On|TestSign_(ChainsLookup|PatchExpand)' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/chains_cache.go internal/gateway/chain_capability_test.go internal/gateway/gateway.go internal/gateway/metrics.go internal/config/
git commit -m "feat(gateway): chain authz with TTL cache, re-validate-on-deny, fail-safe, denial metric"
```

---

### Task 11: Gateway `PATCH /keys/{path}` + cache invalidation

**Files:**
- Modify: `internal/gateway/gateway.go` (`appRoutes:241`, new `updateKeyChains` handler)
- Test: `internal/gateway/chain_capability_test.go`

**Interfaces:**
- Consumes: `keys.UpdateKeyChains(ctx, path, addChains)`.
- Produces: `PATCH /keys/{path}` + `/v1/keys/{path}`; validates only `add_chains` (else 400 `only add_chains is supported`); 400 on empty/unknown; 200 `{path, chains}`; invalidates the chains cache for the path.

- [ ] **Step 1: Write the failing tests**

Add: expand `[evm]`+`{add_chains:["cosmos"]}` → 200 `chains:["cosmos","evm"]`; `{remove_chains:[...]}` → 400 `only add_chains is supported`; `{chains:[...]}` → 400; `{add_chains:[]}` → 400 subset message; `{add_chains:["solana"]}` → 400; no auth header → 401.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gateway/ -run 'TestPatch' -v`
Expected: FAIL — route absent.

- [ ] **Step 3: Implement**

Add a `updateKeyChains` handler: decode into a strict map; reject any key other than `add_chains`; `ParseChains`; call `keys.UpdateKeyChains`; `chainsCache.invalidate(path)`; respond `{path, chains}`. Register `{http.MethodPatch, "/keys/{path}", s.rateLimit(s.auth(http.HandlerFunc(s.updateKeyChains)))}` in `appRoutes` (path param uses Go 1.22 mux `{path...}` wildcard since key paths contain slashes — use `/keys/{path...}` and `r.PathValue("path")`).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/gateway/ -run 'TestPatch' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go internal/gateway/chain_capability_test.go
git commit -m "feat(gateway): expand-only PATCH /keys/{path} with cache invalidation"
```

---

### Task 12: Resilient list endpoint — R-D

**Files:**
- Modify: `internal/gateway/gateway.go` (`listKeys:774`)
- Test: `internal/gateway/keys_test.go`

**Interfaces:**
- Produces: each list entry carries `chains`; per-entry tag reads run with bounded concurrency (8) + per-entry timeout; a failed entry read yields `"chains": null` for that entry only.

- [ ] **Step 1: Write the failing tests**

Add: list of 3 keys with mixed tags → each entry's `chains` matches; list where one entry's `GetKeyChains` errors → that entry has `chains: null`, the other entries are intact, HTTP 200 overall.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gateway/ -run 'TestList' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

After the existing list/paginate, fan out `GetKeyChains` per leaf entry through a semaphore (`make(chan struct{}, 8)`) with a per-entry `context.WithTimeout`. Collect into `KeyListEntry`; on error set `Chains = nil` (serializes to JSON `null`). Preserve order and pagination.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/gateway/ -run 'TestList' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go internal/gateway/keys_test.go
git commit -m "feat(gateway): resilient list with bounded per-entry chains reads"
```

---

### Task 13: CLI flags

**Files:**
- Modify: `cmd/kms-wrapper/` (keys create command; new update-chains command)
- Test: `cmd/kms-wrapper/*_test.go`

**Interfaces:**
- Produces: `keys create --path <p> --chains evm,cosmos` (required, comma-separated); `keys update-chains --path <p> --add evm,cosmos`.

- [ ] **Step 1: Write the failing tests**

Add CLI tests: `keys create` without `--chains` exits non-zero with the subset message; with `--chains evm` succeeds and prints the chains + evm address (no cosmos address); `keys update-chains --add cosmos` prints the new canonical list.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/kms-wrapper/ -v`
Expected: FAIL — flags/subcommand absent.

- [ ] **Step 3: Implement**

Add a required `--chains` string flag (split on `,`, pass to `vault.CreateKey`). Add `keys update-chains` subcommand with `--path` + `--add` (split on `,`, call `vault.UpdateKeyChains`, print result). Help text names the closed set. Preserve the non-zero-on-error pattern (no empty-success prints).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./cmd/kms-wrapper/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/kms-wrapper/
git commit -m "feat(cli): --chains on keys create and keys update-chains subcommand"
```

---

### Task 14: Update remaining fixtures + regenerate docs

**Files:**
- Modify: all remaining `internal/**/*_test.go` that `POST /keys` or create keys without `chains`
- Modify (generated): `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`; swagger annotations on changed handlers in `gateway.go`

**Interfaces:** none (test/doc only).

- [ ] **Step 1: Update create fixtures**

Run `go test ./... 2>&1 | head -60` to find every test still calling the old `CreateKey`/create-without-chains. Add `chains: ["evm","cosmos"]` (or a chain-specific value where the test is about capability). Update mock `KeyStore`/`Vault` doubles to the new method signatures.

- [ ] **Step 2: Add/adjust swagger annotations**

On `createKey`, `showKey`, `listKeys`, `updateKeyChains`: annotate `chains` (enum `evm,cosmos`), the conditional addresses, and the new `PATCH` operation. Then:

Run: `make swagger && make swagger-check`
Expected: regen clean; spec shows the closed-set enum and the `PATCH /v1/keys/{path}` operation.

- [ ] **Step 3: Full suite**

Run: `go test ./... && make lint`
Expected: green.

- [ ] **Step 4: Commit**

```bash
git add internal/ docs/
git commit -m "test+docs: chains fixtures and regenerated OpenAPI"
```

---

### Task 15: End-to-end verification

**Files:** none.

- [ ] **Step 1: Fresh stack**

Run: `make scrub-env && make dev-down && make dev-up`
Expected: stack up.

- [ ] **Step 2: Capability lifecycle**

```bash
export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root KMS_GATEWAY_TOKEN=dev
go run ./cmd/kms-wrapper keys create --path payment/prod/alice --chains evm
# EVM sign → 200
curl -fsS -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"type":"personal_message","key_path":"payment/prod/alice","personal_message":"0x6869"}' \
  http://127.0.0.1:8080/sign/evm
# Cosmos sign → 403 with allowed chains [evm]
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"key_path":"payment/prod/alice","hrp":"mantra","sign_mode":"AMINO_JSON","sign_doc":"{}"}' \
  http://127.0.0.1:8080/sign/cosmos   # expect 403
# Expand
curl -fsS -X PATCH -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"add_chains":["cosmos"]}' http://127.0.0.1:8080/keys/payment/prod/alice
# Cosmos sign now → 200
```
Expected: EVM 200; Cosmos 403 then 200 after PATCH; PATCH returns `chains:["cosmos","evm"]`.

- [ ] **Step 3: Confirm metric + swagger**

```bash
curl -fsS http://127.0.0.1:8080/metrics | grep kms_chain_authz_denials_total   # >=1 after the 403
curl -fsS http://127.0.0.1:8080/swagger/doc.json | grep -o 'add_chains'         # present
```
Expected: denial counter present and incremented; `add_chains`/PATCH op in spec.

- [ ] **Step 4: Record verification result** in the PR description (lint+tests green, swagger-check clean, lifecycle verified, denial metric increments).

---

## Self-Review

- **Spec coverage:** D1 create-required → Task 3/9; D2 403 body+log → Task 4/10; D3 dual enforce → Task 4 (plugin) + Task 10 (gateway); D4 canonical closed set → Task 1/3; D5 expand-only PATCH → Task 5/11; D6 conditional shape → Task 2/8/9; D7 CLI → Task 13; legacy-fails-closed → Task 4. Resilience: R-A/R-B/R-E → Task 10; R-C → Task 5; R-D → Task 12. Docs/fixtures → Task 14. Verify → Task 15. All map.
- **Placeholder scan:** none — every code step has concrete strings/signatures/commands. Test-helper names flagged as "match existing conventions" where the gateway/plugin suites already define them.
- **Type consistency:** `ParseChains`/`ChainsContain`/`Chain`/`ChainEVM`/`ChainCosmos` (Task 1) reused identically in Tasks 3,9,10,13. `Sign(ctx,path,hash,chain)` defined in Task 6, consumed in Task 7. `GetKeyChains`/`UpdateKeyChains` defined in Task 6, consumed in Tasks 9–12. `keyinfo.For(...,chains)` defined Task 8, consumed Task 9. `authorizeChain` defined Task 10, consumed Tasks 10–11.
- **Dependency note:** assumes `update-key-path-scheme` merged (all example paths use `payment/prod/alice`).
