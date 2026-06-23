# golangci-lint CI Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the CI `test` job's golangci-lint step pass by upgrading the linter to a v2 release built with a Go toolchain ≥ the project target, then resolving all 37 lint findings that the upgraded linter surfaces.

**Architecture:** Two parts. (1) Code/config fixes — resolve every finding `golangci-lint v2.12.2` reports against the current tree (errcheck, gosec, ineffassign, noctx, staticcheck). (2) CI fix — bump `golangci/golangci-lint-action@v6 → @v8` and `version: v1.63.4 → v2.12.2` so the action understands the existing `version: "2"` config and runs a binary built with go1.26 (≥ the go1.25.9 target). Code is fixed *before* the CI bump so the workflow is green the first time it runs.

**Tech Stack:** Go 1.25.9 (target, per `go.mod`), golangci-lint v2.12.2, GitHub Actions.

## Global Constraints

- **Go target:** `go 1.25.9` (from `go.mod`). golangci-lint refuses to run when its build Go version < target; v2.12.2 is built with go1.26.2 and satisfies this.
- **Linter version:** `golangci-lint v2.12.2`. Config schema is **v2** (`.golangci.yaml` already has `version: "2"`).
- **Enabled linters:** v2 defaults (`errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`) **plus** the explicit `enable:` list (`govet`, `ineffassign`, `staticcheck`, `gosec`, `noctx`). This is why `errcheck` fires even though it is not in the explicit list.
- **gosec suppression format:** `//nolint:gosec // <justification>` placed on the flagged line (or the flagged import). NOTE: golangci-lint **ignores** gosec's native `//nosec` directive — its own `//nolint:gosec` mechanism is the one that works. Every suppression MUST carry a justification after the ` // ` separator.
- **No behavior changes.** All fixes are either error-handling (`_, _ = …`), dead-code removal, idiomatic rewrites, or justified suppressions. Do not alter runtime behavior.
- **Local verification command:** `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...` — the `--max-*=0` flags are REQUIRED. golangci-lint defaults to `max-same-issues: 3`, which hides identical messages (e.g. 14 `noctx` violations show as 3). Always verify with the caps disabled.
- Local toolchain confirmed: `go1.26.4`, `golangci-lint 2.12.2`.

---

## Finding Inventory (37 total, caps disabled)

| Linter | Count | Location(s) | Task |
|---|---|---|---|
| errcheck | 11 | `cmd/kms-wrapper/root.go` (61, 113, 129, 216, 308, 379, 426, 431, 449, 454, 460) | Task 1 |
| gosec G406/G507 | 2 | `internal/signer/cosmos/cosmos.go` (15, 71) | Task 2 |
| gosec G306/G304 | 5 | `cmd/swagger-postprocess/main.go` (35, 39, 50, 337, 360) | Task 3 |
| ineffassign + staticcheck QF1008 | 2 | `internal/gateway/gateway.go` (329, 76) | Task 4 |
| staticcheck S1030/SA4006 ×3 | 3 | `keys_test.go:199`, `security_test.go:71`, `amino_canonical_test.go:54` | Task 5 |
| noctx | 14 | `gateway_test.go` (1), `observability_test.go` (4), `security_test.go` (9) | Task 6 |
| — | — | CI workflow bump | Task 7 |

---

## Task 1: errcheck — handle `fmt.Fprint*` returns in `root.go`

**Files:**
- Modify: `cmd/kms-wrapper/root.go` (lines 61, 113, 129, 216, 308, 379, 426, 431, 449, 454, 460)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: nothing other tasks depend on.

All 11 are unchecked writes to a CLI writer (`warnOut`, `cmd.OutOrStdout()`). The idiomatic fix for intentionally-ignored `fmt.Fprint*` returns is to assign to the blank identifier: `_, _ = fmt.Fprintln(...)`. This is explicit, behavior-preserving, and satisfies errcheck without a config exclusion.

- [ ] **Step 1: Confirm the findings exist (red)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./cmd/kms-wrapper/... 2>&1 | grep errcheck`
Expected: 11 lines, each `... Error return value of `fmt.Fprint(ln|f)` is not checked (errcheck)`.

- [ ] **Step 2: Apply the fixes**

Edit each of the following lines in `cmd/kms-wrapper/root.go`, prefixing the call with `_, _ = `. Exact before → after:

Line 61:
```go
			fmt.Fprintln(warnOut, msg)
```
→
```go
			_, _ = fmt.Fprintln(warnOut, msg)
```

Line 113:
```go
		fmt.Fprintln(warnOut, "warn: running with weak vault token (KMS_DEV=true)")
```
→
```go
		_, _ = fmt.Fprintln(warnOut, "warn: running with weak vault token (KMS_DEV=true)")
```

Line 129:
```go
		fmt.Fprintln(warnOut, "warn: running with weak gateway token (KMS_DEV=true)")
```
→
```go
		_, _ = fmt.Fprintln(warnOut, "warn: running with weak gateway token (KMS_DEV=true)")
```

Line 216:
```go
		fmt.Fprintln(warnOut, "warn: running with unauthenticated swagger on non-loopback (KMS_DEV=true)")
```
→
```go
		_, _ = fmt.Fprintln(warnOut, "warn: running with unauthenticated swagger on non-loopback (KMS_DEV=true)")
```

Line 308:
```go
				fmt.Fprintln(cmd.OutOrStdout(), k)
```
→
```go
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), k)
```

Line 379:
```go
			fmt.Fprintf(cmd.OutOrStdout(), "0x%x\n", out)
```
→
```go
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "0x%x\n", out)
```

Lines 426–429 (multi-line call — prefix the first line only):
```go
			fmt.Fprintf(cmd.OutOrStdout(), "signature: %s\npub_key: %s\n",
```
→
```go
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "signature: %s\npub_key: %s\n",
```

Line 431:
```go
				fmt.Fprintf(cmd.OutOrStdout(), "cosmos_address: %s\n", addr)
```
→
```go
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cosmos_address: %s\n", addr)
```

Line 449:
```go
				fmt.Fprintf(cmd.OutOrStdout(), "Config: INVALID (%s)\n", err)
```
→
```go
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Config: INVALID (%s)\n", err)
```

Line 454:
```go
				fmt.Fprintf(cmd.OutOrStdout(), "Vault: UNREACHABLE (%s)\n", st.cfg.Vault.Addr)
```
→
```go
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Vault: UNREACHABLE (%s)\n", st.cfg.Vault.Addr)
```

Line 460:
```go
				fmt.Fprintf(cmd.OutOrStdout(), "Vault: OK (%s)\n", st.cfg.Vault.Addr)
```
→
```go
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Vault: OK (%s)\n", st.cfg.Vault.Addr)
```

- [ ] **Step 3: Verify the findings are gone and the package builds (green)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./cmd/kms-wrapper/... 2>&1 | grep errcheck`
Expected: no output.
Run: `go build ./cmd/kms-wrapper/...`
Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
git add cmd/kms-wrapper/root.go
git commit -m "fix(cli): check fmt.Fprint return values (errcheck)"
```

---

## Task 2: gosec — justify protocol-required ripemd160 in `cosmos.go`

**Files:**
- Modify: `internal/signer/cosmos/cosmos.go` (lines 15, 71)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing.

`ripemd160` is mandated by the Cosmos/Bitcoin address scheme (`bech32(ripemd160(sha256(pubkey)))`). It is not a discretionary weak-hash choice — replacing it would produce wrong addresses. Suppress the findings inline with justifications (per the decision: inline, scoped, not a global config exclusion). Use `//nolint:gosec` (golangci-lint ignores gosec's native `//nosec`).

**NOTE (discovered during execution):** the import line also trips staticcheck `SA1019` (`ripemd160` is deprecated) — the scoped gosec-only `grep` in Step 1/3 does not surface it, but the whole-tree gate in Task 7 does. The import directive therefore must be `//nolint:gosec,staticcheck`.

- [ ] **Step 1: Confirm the findings exist (red)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/signer/cosmos/... 2>&1 | grep -E 'G406|G507|SA1019'`
Expected: lines for `G507: Blocklisted import golang.org/x/crypto/ripemd160`, `G406: Use of deprecated weak cryptographic primitive`, and `SA1019: ... ripemd160 ... is deprecated`.

- [ ] **Step 2: Add the suppressions**

Line 15 (the import) — before:
```go
	"golang.org/x/crypto/ripemd160"
```
after:
```go
	"golang.org/x/crypto/ripemd160" //nolint:gosec,staticcheck // ripemd160 is mandated by the Cosmos address scheme (bech32 of RIPEMD160(SHA256(pubkey))); it is protocol-required, not a discretionary hash choice
```

Line 71 — before:
```go
	h := ripemd160.New()
```
after:
```go
	h := ripemd160.New() //nolint:gosec // ripemd160 is mandated by the Cosmos address scheme; required for correct address derivation
```

- [ ] **Step 3: Verify the findings are gone (green)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/signer/cosmos/... 2>&1 | grep -E 'G406|G507|SA1019'`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/signer/cosmos/cosmos.go
git commit -m "fix(cosmos): annotate protocol-required ripemd160 (gosec G406/G507)"
```

---

## Task 3: gosec — tighten perms + justify path reads in `swagger-postprocess`

**Files:**
- Modify: `cmd/swagger-postprocess/main.go` (lines 35, 39, 50, 337, 360)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing.

This is a build-time codegen tool, not a server. Per the decision: tighten generated-doc perms `0o644 → 0o600` (resolves G306 cleanly) and add scoped `//nolint:gosec` on the two `os.ReadFile(path)` calls, since `path` is a tool input (CLI/codegen argument), not untrusted user input. NOTE: golangci-lint ignores gosec's native `//nosec`; use `//nolint:gosec // <reason>`.

- [ ] **Step 1: Confirm the findings exist (red)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./cmd/swagger-postprocess/... 2>&1 | grep -E 'G306|G304'`
Expected: five lines — G306 at 35, 39, 360 and G304 at 50, 337.

- [ ] **Step 2: Tighten the three WriteFile perms (G306)**

Line 35 — before:
```go
	if err := os.WriteFile("docs/swagger.json", jsonBytes, 0o644); err != nil {
```
after:
```go
	if err := os.WriteFile("docs/swagger.json", jsonBytes, 0o600); err != nil {
```

Line 39 — before:
```go
	if err := os.WriteFile("docs/swagger.yaml", jsonBytes, 0o644); err != nil {
```
after:
```go
	if err := os.WriteFile("docs/swagger.yaml", jsonBytes, 0o600); err != nil {
```

Line 360 — before:
```go
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
```
after:
```go
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
```

- [ ] **Step 3: Justify the two ReadFile path reads (G304)**

Line 50 — before:
```go
	b, err := os.ReadFile(path)
```
after:
```go
	b, err := os.ReadFile(path) //nolint:gosec // path is a build-time codegen input (this is a generator tool, not a server), not untrusted user input
```

Line 337 — before:
```go
	content, err := os.ReadFile(path)
```
after:
```go
	content, err := os.ReadFile(path) //nolint:gosec // path is a build-time codegen input (this is a generator tool, not a server), not untrusted user input
```

- [ ] **Step 4: Verify gosec is clean and codegen still works (green)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./cmd/swagger-postprocess/... 2>&1 | grep -E 'G306|G304'`
Expected: no output.
Run: `make swagger-check`
Expected: exits 0 (regenerated docs match committed docs; the `0o600` change only affects file mode, not content).

- [ ] **Step 5: Commit**

```bash
git add cmd/swagger-postprocess/main.go
git commit -m "fix(swagger-postprocess): tighten doc file perms and annotate path reads (gosec G306/G304)"
```

---

## Task 4: ineffassign + staticcheck QF1008 in `gateway.go` (production code)

**Files:**
- Modify: `internal/gateway/gateway.go` (lines 76, 327–330)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing.

Two production-code findings:
- **ineffassign (line 329):** in `instrumentRoutes`, the local `status` string is computed (line 327), checked for empty (line 328), reassigned to `"unknown"` (line 329) — and then never read. The metric labels use `intToStatusLabel(sw.status)` (line 331), not `status`. The entire `status` string block is dead. Remove lines 327–330.
- **staticcheck QF1008 (line 76):** `w.ResponseWriter.Header()` redundantly names the embedded field. `methodNotAllowedRewriter` embeds `http.ResponseWriter`, so `w.Header()` resolves identically.

- [ ] **Step 1: Confirm the findings exist (red)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/gateway/... 2>&1 | grep -E 'ineffassign|QF1008'`
Expected: `gateway.go:329 ... ineffectual assignment to status (ineffassign)` and `gateway.go:76 ... QF1008: could remove embedded field "ResponseWriter" from selector (staticcheck)`.

- [ ] **Step 2: Remove the dead `status` block (lines 327–330)**

Before:
```go
		path := r.URL.Path
		method := r.Method
		status := http.StatusText(sw.status)
		if status == "" {
			status = "unknown"
		}
		kmsHTTPRequestsTotal.WithLabelValues(path, method, intToStatusLabel(sw.status)).Inc()
```
After:
```go
		path := r.URL.Path
		method := r.Method
		kmsHTTPRequestsTotal.WithLabelValues(path, method, intToStatusLabel(sw.status)).Inc()
```

- [ ] **Step 3: Simplify the embedded-field selector (line 76)**

Before:
```go
		h := w.ResponseWriter.Header()
```
After:
```go
		h := w.Header()
```

- [ ] **Step 4: Verify findings gone, package builds, tests pass (green)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/gateway/... 2>&1 | grep -E 'ineffassign|QF1008'`
Expected: no output.
Run: `go test ./internal/gateway/...`
Expected: `ok` (no regression in the 405-rewriter or metrics behavior).

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/gateway.go
git commit -m "fix(gateway): drop dead status local and redundant embedded selector (ineffassign, staticcheck QF1008)"
```

---

## Task 5: staticcheck S1030 + SA4006 in test files

**Files:**
- Modify: `internal/gateway/keys_test.go` (line 199)
- Modify: `internal/gateway/security_test.go` (lines 71–75, 88)
- Modify: `internal/signer/cosmos/amino_canonical_test.go` (lines 5, 52–57)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing. (Task 6 also edits `security_test.go`; do Task 5 first to avoid line-number drift, or re-locate by content.)

Three findings:
- **S1030 (`keys_test.go:199`):** `string(rr.Body.Bytes())` → `rr.Body.String()`.
- **SA4006 (`security_test.go:71`):** `h` is assigned a throwaway handler at line 71 that is never read before being overwritten at line 88 (`h = srv.Handler()`). The override func it passes (HealthRateLimit/Burst = 5) is redundant — the same values are set on the real `cfg` at lines 80–81. Remove the dead assignment (lines 71–75) and make line 88 the declaration (`:=`).
- **SA4006 (`amino_canonical_test.go:54`):** `d := sha256.Sum256(got)` then `if len(d) != 32`. `len` of a fixed-size `[32]byte` array is a compile-time constant, so `d`'s value is never actually read and the check is a tautology. Remove the dead sanity block (lines 52–57) and the now-unused `crypto/sha256` import (line 5).

- [ ] **Step 1: Confirm the findings exist (red)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/... 2>&1 | grep -E 'S1030|SA4006'`
Expected: three lines — S1030 at `keys_test.go:199`, SA4006 at `security_test.go:71`, SA4006 at `amino_canonical_test.go:54`.

- [ ] **Step 2: Fix S1030 in `keys_test.go` (line 199)**

Before:
```go
	if string(rr.Body.Bytes()) == boom.Error() {
```
After:
```go
	if rr.Body.String() == boom.Error() {
```

- [ ] **Step 3: Fix SA4006 in `security_test.go` — remove dead `h` assignment**

Remove lines 71–75 (the throwaway assignment and its trailing blank line). Before:
```go
	var healthCalls atomic.Int64
	h := newGatewayHandlerWithKeys(keyStoreMock{}, func(cfg *config.Config) {
		cfg.Gateway.HealthRateLimit = 5
		cfg.Gateway.HealthRateBurst = 5
	})

	// Wrap the healthMock so we can count Vault round-trips. We rebuild the
```
After:
```go
	var healthCalls atomic.Int64

	// Wrap the healthMock so we can count Vault round-trips. We rebuild the
```

Then change line 88 from assignment to declaration. Before:
```go
	h = srv.Handler()
```
After:
```go
	h := srv.Handler()
```

- [ ] **Step 4: Fix SA4006 in `amino_canonical_test.go` — remove dead sanity block + import**

Remove lines 52–57 (blank line through the closing brace). Before:
```go
	if string(got) != string(want) {
		t.Fatalf("canonical mismatch:\n got=%s\nwant=%s", got, want)
	}

	// Sanity: the digest of the canonical bytes is what we'd hand to Vault.
	d := sha256.Sum256(got)
	if len(d) != 32 {
		t.Fatalf("digest length wrong")
	}
}
```
After:
```go
	if string(got) != string(want) {
		t.Fatalf("canonical mismatch:\n got=%s\nwant=%s", got, want)
	}
}
```

Then remove the now-unused import at line 5. Before:
```go
	"crypto/sha256"
```
After: (delete the line entirely)

- [ ] **Step 5: Verify findings gone, tests compile and pass (green)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/... 2>&1 | grep -E 'S1030|SA4006'`
Expected: no output.
Run: `go test ./internal/gateway/... ./internal/signer/cosmos/...`
Expected: `ok` for both packages (no compile error from the removed import / `h` redeclaration).

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/keys_test.go internal/gateway/security_test.go internal/signer/cosmos/amino_canonical_test.go
git commit -m "test: resolve staticcheck S1030/SA4006 (dead assignments, Body.String)"
```

---

## Task 6: noctx — use `httptest.NewRequestWithContext` across test files

**Files:**
- Modify: `internal/gateway/gateway_test.go` (1 site: line 134)
- Modify: `internal/gateway/observability_test.go` (4 sites: lines 142, 153, 167, 205)
- Modify: `internal/gateway/security_test.go` (9 sites: lines 26, 92, 125, 143, 163, 190, 199, 214, 256; **add `"context"` import**)

**Interfaces:**
- Consumes: `security_test.go` edits from Task 5 (do Task 5 first; line numbers below are pre-Task-5 — re-locate `httptest.NewRequest(` by content, not line number).
- Produces: nothing.

noctx flags every `httptest.NewRequest(...)` call (14 total — the CI showed only 3 because of `max-same-issues: 3`). The fix is mechanical: replace `httptest.NewRequest(` with `httptest.NewRequestWithContext(context.Background(), ` at every call site. `context.Background()` satisfies noctx and is safe in tests. `gateway_test.go` and `observability_test.go` already import `"context"`; `security_test.go` does not and must have it added.

- [ ] **Step 1: Confirm the findings exist (red)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/gateway/... 2>&1 | grep -c noctx`
Expected: `14`.

- [ ] **Step 2: Add the `context` import to `security_test.go`**

`security_test.go` does not import `context`. Add `"context"` to its import block (alphabetically first in the stdlib group). For example, if the block opens:
```go
import (
	"bytes"
```
make it:
```go
import (
	"bytes"
	"context"
```
(Place it so the import block stays sorted; `goimports`/`gofmt` in Step 4 will normalize ordering if slightly off.)

- [ ] **Step 3: Replace every `httptest.NewRequest(` call site**

In each of the three files, replace each occurrence of `httptest.NewRequest(` with `httptest.NewRequestWithContext(context.Background(), `. This is a global replace-all within each file. The argument lists are preserved verbatim — only the constructor name and the leading `context.Background(),` argument are added.

Representative before → after (`gateway_test.go:134`, inside the `doRequest` helper which has no `*testing.T` in scope, hence `context.Background()`):
```go
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
```
→
```go
	req := httptest.NewRequestWithContext(context.Background(), method, path, bytes.NewReader(body))
```

Representative before → after (`observability_test.go:142`):
```go
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
```
→
```go
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/livez", nil)
```

Apply the same transformation to all 14 sites:
- `gateway_test.go`: 1 site (134)
- `observability_test.go`: 4 sites (142, 153, 167, 205)
- `security_test.go`: 9 sites (26, 92, 125, 143, 163, 190, 199, 214, 256)

- [ ] **Step 4: Format imports**

Run: `gofmt -w internal/gateway/gateway_test.go internal/gateway/observability_test.go internal/gateway/security_test.go`
Expected: no output (files reformatted in place; normalizes the added import ordering).

- [ ] **Step 5: Verify noctx is clean and tests pass (green)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./internal/gateway/... 2>&1 | grep -c noctx`
Expected: `0`.
Run: `go test ./internal/gateway/...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/gateway_test.go internal/gateway/observability_test.go internal/gateway/security_test.go
git commit -m "test(gateway): use httptest.NewRequestWithContext (noctx)"
```

---

## Task 7: Bump CI to golangci-lint v2 + full-tree verification

**Files:**
- Modify: `.github/workflows/ci.yml` (lines 21–23)

**Interfaces:**
- Consumes: all prior tasks (the tree must be lint-clean before flipping CI on).
- Produces: nothing.

The original failure was a version mismatch: `golangci/golangci-lint-action@v6` installs golangci-lint **v1**, which cannot parse the `version: "2"` config and is built with go1.23 (< the go1.25.9 target). Action **v8** installs golangci-lint **v2** and supports the v2 config schema; pinning `v2.12.2` (built with go1.26.2) clears the toolchain-version gate.

- [ ] **Step 1: Verify the WHOLE tree is lint-clean (red→green gate for the entire change)**

Run: `golangci-lint run --max-same-issues=0 --max-issues-per-linter=0 ./...`
Expected: exits 0, prints `0 issues` (or no output). If any issue remains, fix it under the owning task before proceeding — do NOT bump CI against a dirty tree.

- [ ] **Step 2: Update the workflow lint step**

Before (`.github/workflows/ci.yml` lines 21–23):
```yaml
      - uses: golangci/golangci-lint-action@v6
        with:
          version: v1.63.4
```
After:
```yaml
      - uses: golangci/golangci-lint-action@v8
        with:
          version: v2.12.2
```

- [ ] **Step 3: Sanity-check the workflow YAML and run the local lint target**

Run: `make lint`
Expected: exits 0 (this is the same `golangci-lint run ./...` the CI invokes, minus the cap flags; with a clean tree it passes regardless of caps).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: bump golangci-lint-action to v8 and golangci-lint to v2.12.2"
```

- [ ] **Step 5: Trigger CI and confirm the `test` job's lint step passes**

The `ci.yml` trigger is `workflow_dispatch` only. After pushing the branch:
```bash
gh workflow run CI --ref <branch> --repo ryantruong-mantra/kms-wrapper
```
Then watch the run:
```bash
gh run watch --repo ryantruong-mantra/kms-wrapper
```
Expected: the `Run golangci/golangci-lint-action@v8` step succeeds (no "can't load config" / version error, no lint findings).

---

## Self-Review

**1. Spec coverage:** Every linter from the uncapped run maps to a task — errcheck→T1, gosec G406/G507→T2, gosec G306/G304→T3, ineffassign+QF1008→T4, S1030+SA4006→T5, noctx→T6, CI version mismatch→T7. The two root causes from the CI log (toolchain-version gate + v2 config on a v1 binary) are both fixed in T7. 37/37 findings covered.

**2. Placeholder scan:** Every code step shows exact before/after. No TBD/TODO/"handle edge cases". Verification steps give exact commands and expected output.

**3. Type/consistency:** `_, _ = fmt.Fprint*` matches `(int, error)` return. `httptest.NewRequestWithContext(context.Background(), …)` matches the stdlib signature `(ctx, method, target, body)`. `w.Header()` resolves through the embedded `http.ResponseWriter`. The `--max-same-issues=0 --max-issues-per-linter=0` flags are used consistently in every verification step (critical — defaults hide ~16 findings). Task 5 and Task 6 both touch `security_test.go`; the ordering note (do T5 before T6, re-locate by content) prevents line-drift errors.

**Decisions applied (from clarification):** action@v8 + v2.12.2; inline `#nosec` for ripemd160; tighten perms `0o600` + scoped `#nosec G304` for swagger-postprocess; fix test code (not exclude) for noctx/staticcheck.
