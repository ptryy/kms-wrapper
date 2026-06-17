# update-key-path-scheme Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the middle key-path segment from `{chain}` to `{environment}` (free-form), updating the validator's error messages and every example/string literal, with no signing-behavior change.

**Architecture:** `internal/keypath/keypath.go` is the single behavioral source of truth (two error messages + doc comments). All other edits are mechanical string/example/doc updates that fan out from that rename. Generated `docs/` is regenerated, not hand-edited.

**Tech Stack:** Go 1.25+, `swaggo/swag` v2 (`make swagger`), Make targets, HashiCorp Vault dev container.

## Global Constraints

- Segment regex stays exactly `^[a-z0-9_-]+$`; the 3-segment rule and empty-segment check are unchanged. (verbatim from design "Behavioral delta")
- New validator format string is exactly: `key path must have format {project}/{environment}/{username}`
- No reserved-chain list or warning log is added or removed in code — it never existed in `keypath.go`. (design "Removed")
- Clean break: no compatibility shim, no dual-shape acceptance. (design "Migration")
- Use plugin-reality terms (`kms/keys/...`), never Vault Transit terms (`transit/keys/...`). (CONSTITUTION §7)
- `make swagger-check` must be clean (committed `docs/` matches regen) before completion. (design "Testing")
- Environment example labels to standardize on: `prod` (primary), with `staging`/`dev` where a second example is useful.

---

### Task 1: Rename validator format and add dedicated coverage

**Files:**
- Create: `internal/keypath/keypath_test.go`
- Modify: `internal/keypath/keypath.go` (lines 1, 15-16, 20, 26-28, 36)
- Modify (caller tests asserting the old message): `internal/plugin/path_keys_test.go`, `internal/plugin/path_sign_test.go`, `internal/vault/path_test.go`

**Interfaces:**
- Consumes: nothing (leaf package).
- Produces: `keypath.Validate(path string) error` and `keypath.ValidateListPrefix(s string) error` — unchanged signatures; only the returned error *message* for the 3-segment-count case changes to `key path must have format {project}/{environment}/{username}`.

- [ ] **Step 1: Write the failing test**

Create `internal/keypath/keypath_test.go`:

```go
package keypath

import (
	"strings"
	"testing"
)

func TestValidate_ValidEnvironmentPath(t *testing.T) {
	if err := Validate("proj-a/prod/alice"); err != nil {
		t.Fatalf("expected valid path, got error: %v", err)
	}
}

func TestValidate_WrongSegmentCount_MentionsEnvironment(t *testing.T) {
	err := Validate("proj/prod")
	if err == nil {
		t.Fatal("expected error for 2-segment path")
	}
	if !strings.Contains(err.Error(), "{project}/{environment}/{username}") {
		t.Fatalf("error must reference {environment} format, got: %q", err.Error())
	}
	if strings.Contains(err.Error(), "{chain}") {
		t.Fatalf("error must not reference {chain}, got: %q", err.Error())
	}
}

func TestValidateListPrefix_WrongCount_MentionsEnvironment(t *testing.T) {
	err := ValidateListPrefix("a/b/c/d")
	if err == nil {
		t.Fatal("expected error for 4-segment prefix")
	}
	if !strings.Contains(err.Error(), "{project}/{environment}/{username}") {
		t.Fatalf("error must reference {environment} format, got: %q", err.Error())
	}
}

func TestValidate_FreeFormMiddleSegment_NoReservedList(t *testing.T) {
	// Any [a-z0-9_-] middle segment is valid; there is no reserved-chain list.
	for _, p := range []string{"proj/staging/bob", "proj/mychain/bob", "proj/dev/bob"} {
		if err := Validate(p); err != nil {
			t.Fatalf("expected %q valid, got: %v", p, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/keypath/ -run TestValidate -v`
Expected: FAIL — `TestValidate_WrongSegmentCount_MentionsEnvironment` and `TestValidateListPrefix_WrongCount_MentionsEnvironment` fail because the message still says `{chain}`.

- [ ] **Step 3: Update `keypath.go` messages and doc comments**

In `internal/keypath/keypath.go`:
- Line 1 doc comment: `// Package keypath holds the canonical {project}/{environment}/{username} validator`
- Lines 15-16 doc comment: `// Validate checks a fully-qualified key path of the form` / `// {project}/{environment}/{username}.`
- Line 20 (in `Validate`): return message → `errors.New("key path must have format {project}/{environment}/{username}")`
- Lines 26-28 doc comment for `ValidateListPrefix`: replace `"<project>/<chain>"` with `"<project>/<environment>"`
- Line 36 (in `ValidateListPrefix`): return message → `errors.New("key path must have format {project}/{environment}/{username}")`

Leave `segmentRE`, the 3-segment check, and `validateSegments` untouched.

- [ ] **Step 4: Run keypath tests to verify they pass**

Run: `go test ./internal/keypath/ -v`
Expected: PASS (all 4 tests).

- [ ] **Step 5: Fix caller tests that assert the old format string**

These three test files assert the literal `{project}/{chain}/{username}` and will now fail. Update each assertion's expected substring to `{project}/{environment}/{username}`:
- `internal/plugin/path_keys_test.go`
- `internal/plugin/path_sign_test.go`
- `internal/vault/path_test.go`

Run: `go test ./internal/plugin/ ./internal/vault/ -run 'Format|Path|Validate' -v` and confirm the format-message assertions pass. (Example-path literals in these files are handled in Task 3 — only touch the `{chain}`→`{environment}` *format-string* assertion here.)

- [ ] **Step 6: Commit**

```bash
git add internal/keypath/keypath.go internal/keypath/keypath_test.go \
  internal/plugin/path_keys_test.go internal/plugin/path_sign_test.go internal/vault/path_test.go
git commit -m "feat(keypath): rename {chain} segment to {environment} in validator"
```

---

### Task 2: Update non-test source examples and doc strings

**Files:**
- Modify: `pkg/types/types.go` (4 occurrences — `example:` struct tags)
- Modify: `internal/gateway/gateway.go` (1 example path + 1 `{project}/{chain}/{username}` annotation string)
- Modify: `internal/signer/cosmos/cosmos.go` (1 occurrence — doc comment/example)

**Interfaces:**
- Consumes: `keypath.Validate` semantics from Task 1 (free-form middle segment).
- Produces: updated swagger `example:` values that downstream Task 5 (`make swagger`) reads.

- [ ] **Step 1: Replace example paths in `pkg/types/types.go`**

Replace every `example:"...evm/..."` / `...cosmos/...` struct tag so the middle segment is an environment label, e.g. `example:"proj-a/evm/alice"` → `example:"proj-a/prod/alice"`. Keep the `proj-a` / username parts; only the middle segment changes.

- [ ] **Step 2: Replace example + format string in `internal/gateway/gateway.go`**

- Change the chain-shaped `@Param`/`@Description` example path (e.g. `proj-a/evm/alice`) to `proj-a/prod/alice`.
- Change the `{project}/{chain}/{username}` annotation string to `{project}/{environment}/{username}`.

- [ ] **Step 3: Replace example in `internal/signer/cosmos/cosmos.go`**

Change the single chain-shaped example path in the doc comment to an environment-based one (e.g. `proj-a/mantra/alice` → `proj-a/prod/alice`).

- [ ] **Step 4: Verify build and existing tests still compile/pass**

Run: `go build ./... && go test ./pkg/... ./internal/gateway/ ./internal/signer/cosmos/`
Expected: build OK; tests PASS (these edits are comments/struct tags — no logic).

- [ ] **Step 5: Commit**

```bash
git add pkg/types/types.go internal/gateway/gateway.go internal/signer/cosmos/cosmos.go
git commit -m "docs(types,gateway,cosmos): use {environment} example paths"
```

---

### Task 3: Update test example-path literals

**Files (chain-shaped example paths, counts from grep):**
- Modify: `internal/gateway/keys_test.go` (14), `internal/gateway/polish_test.go` (7), `internal/gateway/gateway_test.go` (3), `internal/gateway/observability_test.go` (2), `internal/gateway/security_test.go` (1)
- Modify: `internal/signer/evm/evm_test.go` (5), `internal/signer/cosmos/cosmos_test.go` (4), `internal/signer/cosmos/amino_canonical_test.go` (2)
- Modify: `internal/keyinfo/keyinfo_test.go` (5)
- Modify: `internal/plugin/backend_test.go` (4), `internal/plugin/path_keys_test.go`, `internal/plugin/path_sign_test.go` (example-path literals only — format-string assertions already done in Task 1)
- Modify: `internal/vault/client_test.go` (2), `internal/vault/client_typed_test.go` (3), `internal/vault/path_test.go` (example-path literals only)

**Interfaces:**
- Consumes: validator from Task 1 (these tests build key paths the validator must accept).
- Produces: nothing downstream — test-only.

- [ ] **Step 1: Rewrite chain-shaped middle segments to environment labels**

In each file above, replace the middle segment of every example key path with an environment label. Mapping convention:
- `.../evm/...`  → `.../prod/...`
- `.../eth/...`  → `.../prod/...`
- `.../cosmos/...` → `.../prod/...`
- `.../mantra/...` → `.../prod/...`
- `.../osmosis/...` → `.../staging/...`

Use `staging`/`dev` instead of a second `prod` where a test deliberately uses two *distinct* paths, so paths that were distinct stay distinct (e.g. a test using `proj-a/evm/alice` and `proj-a/cosmos/alice` to mean two keys must become `proj-a/prod/alice` and `proj-a/staging/alice`, NOT two identical paths). Read each test's intent before collapsing.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: PASS. If a test fails because two formerly-distinct paths collapsed to one, give them distinct environment labels.

- [ ] **Step 3: Commit**

```bash
git add internal/
git commit -m "test: migrate example key paths to {environment} segment"
```

---

### Task 4: Update operator-facing surfaces

**Files:**
- Modify: `README.md` (key-path section, worked examples, plugin-contract examples)
- Modify: `vault/init.sh` (any hard-coded example paths in policy templates/bootstrap fixtures)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing — documentation/bootstrap only.

- [ ] **Step 1: Update `README.md`**

- "Key paths" section: change `{project}/{chain}/{username}` → `{project}/{environment}/{username}`.
- Remove the "Reserved chain identifiers are `evm`, `eth`, `mantra`, `cosmos`, and `osmosis`; unknown values are allowed with a warning." sentence (the convention no longer exists). Replace with: "The `{environment}` segment is free-form (`[a-z0-9_-]`), e.g. `prod`, `staging`, `dev`."
- Update every worked example path (`proj-a/evm/alice`, `proj-a/mantra/alice`, etc.) to environment form (`proj-a/prod/alice`).

- [ ] **Step 2: Update `vault/init.sh`**

Replace any chain-shaped example key path in policy templates or bootstrap fixtures with an environment-based one. Policy globs that are already project-scoped (`kms/keys/<project>/*`) need no change — only literal `.../evm/...`-style examples.

- [ ] **Step 3: Verify dev bootstrap still parses**

Run: `bash -n vault/init.sh`
Expected: no syntax errors. (Full `make dev-up` is exercised in Task 6.)

- [ ] **Step 4: Commit**

```bash
git add README.md vault/init.sh
git commit -m "docs(readme,bootstrap): {environment} key paths, drop reserved-chain convention"
```

---

### Task 5: Regenerate OpenAPI docs

**Files:**
- Modify (generated): `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`

**Interfaces:**
- Consumes: the updated `example:` tags + annotations from Task 2.
- Produces: regenerated committed docs that `swagger-check` validates.

- [ ] **Step 1: Regenerate**

Run: `make swagger`
Expected: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` rewritten; the `{project}/{chain}/{username}` strings and chain-shaped examples in `docs/docs.go` are replaced by `{environment}` forms.

- [ ] **Step 2: Confirm regeneration is clean and complete**

Run: `make swagger-check`
Expected: exits 0 (committed docs match a fresh regen).
Then: `grep -rn '{chain}\|/evm/\|/cosmos/\|/mantra/' docs/` → expect no remaining chain-shaped key-path examples (matches inside unrelated words are fine; verify each hit is not a key-path example).

- [ ] **Step 3: Commit**

```bash
git add docs/docs.go docs/swagger.json docs/swagger.yaml
git commit -m "docs(swagger): regenerate with {environment} key-path examples"
```

---

### Task 6: End-to-end verification

**Files:** none (verification only).

**Interfaces:**
- Consumes: all prior tasks.
- Produces: a passing verification record.

- [ ] **Step 1: Lint and full test suite**

Run: `make lint && go test ./...`
Expected: both green.

- [ ] **Step 2: Fresh dev stack against the new validator**

Run: `make scrub-env && make dev-down && make dev-up`
Expected: stack comes up; `vault/init.sh` installs the project policy and writes a non-root `KMS_VAULT_TOKEN` to `.env`.

- [ ] **Step 3: Create + sign against a new-shape path (REST and CLI)**

```bash
export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root KMS_GATEWAY_TOKEN=dev
# CLI create
go run ./cmd/kms-wrapper keys create --path payment/prod/alice
# REST create (start gateway in another shell: go run ./cmd/kms-wrapper serve)
curl -fsS -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"path":"payment/prod/alice"}' http://127.0.0.1:8080/keys
```
Expected: both succeed; second create reports `already_existed: true`.

- [ ] **Step 4: Sign one EVM personal-message and one Cosmos AMINO_JSON request**

```bash
curl -fsS -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" -H 'Content-Type: application/json' \
  -d '{"type":"personal_message","key_path":"payment/prod/alice","personal_message":"0x68656c6c6f"}' \
  http://127.0.0.1:8080/sign/evm
# Cosmos AMINO_JSON (raw JSON sign-doc):
go run ./cmd/kms-wrapper sign cosmos --path payment/prod/alice --hrp mantra --mode AMINO_JSON \
  --sign-doc '{"account_number":"1","chain_id":"mantra-1","fee":{"amount":[],"gas":"200000"},"memo":"","msgs":[],"sequence":"0"}'
```
Expected: EVM returns `{"signature":"0x...130 hex"}`; Cosmos returns base64 `signature` + `pub_key`. Confirms the renamed path signs on both chains (the exact behavior the rename clarifies).

- [ ] **Step 5: Confirm old-shape path is rejected as expected (clean break)**

```bash
go run ./cmd/kms-wrapper keys show --path payment/prod/alice   # works
# Old chain-shaped path is just a different (nonexistent) key now — not special-cased:
curl -s -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" \
  "http://127.0.0.1:8080/keys/info?path=payment/evm/alice"
```
Expected: the old path resolves to a 404 (no such key) — there is no migration/alias, confirming the clean break.

- [ ] **Step 6: Record verification result**

Note in the PR/commit description: lint+tests green, swagger-check clean, dev stack up, REST+CLI create and dual-chain sign succeed against `payment/prod/alice`.

---

## Self-Review

- **Spec coverage:** Behavioral delta → Task 1. Removed reserved-chain (spec/README only) → Task 1 (code already absent) + Task 4 (README sentence). Mechanical ripple: types/gateway/cosmos source → Task 2; tests → Task 3; README/init.sh → Task 4; generated docs → Task 5. Migration (clean break) → verified in Task 6 Step 5. Testing → Task 6. All design sections map to a task.
- **Placeholder scan:** No TBD/TODO; every code step shows exact strings/commands.
- **Type consistency:** `keypath.Validate` / `keypath.ValidateListPrefix` signatures unchanged across all tasks; new format string `{project}/{environment}/{username}` used identically in Task 1 code, Task 1 tests, Task 2 gateway annotation, and Task 5 regen check.
