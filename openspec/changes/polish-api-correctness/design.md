## Context

The eight issues bundled here cluster naturally:

- **Silent failures (high-severity correctness):** CLI cosmos shadowing, swallowed gateway handler errors. These bugs produce confidently-wrong output and are the kind of defect that surfaces on-chain weeks later.
- **AMINO canonicalisation (high-severity correctness):** Go's `json.Marshal` is not Cosmos canonical JSON. The fix is to use cosmos-sdk's own `sdk.SortJSON` (which produces the exact form the chain re-derives during signature verification).
- **API surface polish (medium):** versioning, discriminator, pagination, status codes, RFC compliance. Each one is small; together they materially improve the integrator experience and OpenAPI codegen output.

None of these require new infrastructure or new dependencies beyond the canonical-JSON helper that cosmos-sdk already exposes (the SDK is already a direct module dependency in `go.mod`; this repo uses Go modules and does not vendor). The cost of skipping the polish items is incremental drift between the spec and the implementation.

## Goals / Non-Goals

**Goals:**
- No code path in CLI or REST handler silently returns "success" on a failed operation.
- AMINO-mode signatures cover bytes that exactly equal the bytes the chain re-derives from the same input.
- Routes are addressable via `/v1/` for forward-compatibility; the OpenAPI spec advertises the versioned paths.
- OpenAPI consumers can codegen typed clients for the EVM sign request and `SignResponse` without manual fixup.
- `GET /keys` cannot return an unbounded response or starve the rate budget for a large tenant.

**Non-Goals:**
- A full `/v2` API. `/v1/` is just an addressable prefix; no semantic difference.
- Removing the bare `/sign/*` and `/keys` routes. They remain for one full minor-version cycle for backward compatibility.
- Reworking the Cosmos `SignDoc` representation; only the canonicalisation step changes.
- New CLI subcommands.

## Decisions

### D1 — Outer-scope error in CLI cosmos sign

**Decision:** Restructure `cmd/kms-wrapper/root.go:225-236` to declare `var err error` *outside* the `switch sign_mode { case "DIRECT": ... }` block. Decode operations use a differently-named local (`doc, decErr := base64...`). `SignDirect`/`SignAmino` write into the outer `err`. Post-switch, `if err != nil { return fmt.Errorf("sign cosmos: %w", err) }`.

**Rationale:** This is the smallest fix; preserves the existing case-statement structure. Adding the test (a smoke test asserting non-empty `sig`/`pub` on success and non-zero exit on simulated failure) prevents regression. Linting with `errorlint` and `revive` would have caught this — that's a separate concern in `.golangci.yaml` (out of scope here; flagged as a nit elsewhere).

### D2 — `sdk.SortJSON` for AMINO canonicalisation

**Decision:** Replace the body of `SignAmino` (currently `json.Decode` with `UseNumber` → `json.Marshal`) with:

```go
sorted, err := sdk.SortJSON(rawSignDocBytes)
if err != nil { return ..., fmt.Errorf("canonicalise amino sign doc: %w", err) }
// hash sorted bytes with SHA-256, then sign
```

Additionally, before passing to `SortJSON`, the function SHALL detect duplicate JSON keys in the raw bytes (Go's stdlib does not flag duplicates; walk the raw bytes with a streaming `json.Decoder` in token mode, tracking seen keys per object scope, or use a recursive uniqueness check over the parsed token stream). On duplicate detection, `SignAmino` SHALL return a typed error (e.g. `fmt.Errorf("duplicate key in amino sign doc: %s", key)`); the REST gateway handler SHALL map that error to HTTP 400 — the signer itself does not emit HTTP status codes.

**Rationale:** `sdk.SortJSON` is the exact code cosmos-sdk uses to re-derive sign bytes during signature verification. Using it eliminates the entire class of "Go-canonical vs Cosmos-canonical" mismatch. Duplicate-key rejection prevents the "last-wins ambiguity" footgun (the same input bytes can deserialise into two different documents under different parsers).

**Alternative considered — keep `json.Marshal` but add a fixup pass:** rejected; the failure modes (`Coin.amount` integer-vs-string, escaping rules) are too many to enumerate.

**Alternative considered — use `legacy.Cdc.MarshalJSON`:** equivalent for our use case but pulls in more cosmos-sdk surface. `sdk.SortJSON` is the minimal correct primitive.

### D3 — Propagate swallowed handler errors

**Decision:** `internal/gateway/gateway.go:359`: replace `_ = tx.UnmarshalBinary(out)` with `if err := tx.UnmarshalBinary(out); err != nil { writeError(w, http.StatusInternalServerError, "decode signed tx: " + err.Error()); return }`. Same shape for `:452` `DeriveCosmosAddressFromCompressed`. Both add a corresponding test with a fixture that triggers the error branch.

**Rationale:** A 500 with a specific error message is the correct behaviour; the current behaviour (success status with zero-valued fields) is a silent integrity bug. Tests prevent reintroduction.

### D4 — `/v1/` prefix via route-mount loop, dual-mount for one cycle

**Decision:** In `Server.routes()`, factor route registration into a slice of `{method, pattern, handler}` and register each twice — once at the bare path, once with `/v1` prefixed. The OpenAPI spec generation (`@Router` annotations) emits the `/v1/` form as primary. Document the bare forms as "legacy aliases — to be removed in the next minor version."

**Rationale:** Dual-mount is cheap (a 4-line change in `routes`); the deprecation window gives external integrators time to update without breakage. Setting the precedent of `/v1/` now is much cheaper than retrofitting later.

**Alternative considered — only `/v1/` (breaking change):** rejected; even though there are few external callers today, the cost of a known-incoming break is higher than a one-cycle parallel mount.

### D5 — Typed `Signature` field

**Decision:** Replace `Signature any` in `pkg/types/types.go:53-59` with:
- For personal-message / eip712: a `string` (`0x<65-byte-hex>`) at top level.
- For raw-tx: leave as the structured `signature_parts: {r,s,v}` field already in the response.

Define two distinct response structs (`SignEVMRawTxResponse`, `SignEVMPersonalResponse`) and have the handler dispatch on request variant. OpenAPI emits two response variants under `oneOf` keyed by which request variant was used.

**Rationale:** `Signature any` was the lazy solution and produces an opaque `{}` in OpenAPI. Splitting the response is the way every other multi-variant signing API does it.

### D6 — EVM sign-request `oneOf` discriminator

**Decision:** Add a required `type: "raw_tx"|"personal_message"|"eip712_digest"` field to the EVM sign request. The handler validates the discriminator and dispatches to the matching path. The OpenAPI `oneOf` uses `discriminator: { propertyName: "type", mapping: { ... } }`.

The dispatch helper `countNonEmpty` is replaced with a `switch req.Type { case "raw_tx": ... }`.

**Rationale:** Explicit discriminators are the OpenAPI-recommended pattern for codegen. The current "infer from which field is set" approach works for humans writing curl, but fails for typed clients and tooling.

### D7 — Cursor pagination on `GET /keys`

**Decision:** Add `?limit=` (default 100, max 1000) and `?cursor=` query params. The cursor is opaque base64 (encoded `{prefix, offset}` or wraps the Vault list pagination token if/when the plugin exposes one). For now (single Vault LIST → no native pagination), implement client-side pagination: fetch the full list, slice `[offset:offset+limit]`, return `next_cursor` if more exists. Document this as a temporary implementation that can be replaced when the plugin gains a native paginated list.

**Rationale:** Caps response size today; gives future-us a backwards-compatible upgrade path.

### D8 — 405 `Allow` header preserved

**Decision:** In `internal/gateway/gateway.go:methodNotAllowedRewriter`, copy `Allow` from the inner `ResponseWriter`'s `Header()` map *before* clearing `Content-Length`. The header survives into the rewritten JSON response.

**Rationale:** RFC compliance for cheap. The test fixture already exists; just assert the header.

### D9 — 201 on first POST /keys

**Decision:** In the create handler, after the gateway determines `already_existed` from the Vault response, set `w.WriteHeader(http.StatusCreated)` when `already_existed=false`, otherwise `http.StatusOK`. The response body is unchanged.

**Rationale:** Convention. HTTP-aware reverse proxies (e.g. metrics collectors) bucket 201 separately from 200.

## Risks / Trade-offs

- **D2 may change AMINO signatures for inputs that were not strictly canonical.** This is intentional — the goal is correctness. The previous signatures would have been verified-but-misleading or rejected-but-silently-recoverable. Operators who had built test fixtures against the buggy output must regenerate them.
- **D4 dual-mount doubles the entry in OpenAPI paths.** Mitigation: emit only the `/v1/` form in the spec; document the bare aliases as deprecated. The bare routes still exist at runtime but are not advertised.
- **D5 / D6 are wire-level changes** — but for a service with ~zero external integrators today, the cost is small. Worth doing now while it's cheap.
- **D7 cursor pagination is client-side initially** — under very large tenants the gateway still fetches the whole list internally. Acceptable as a first step; documented as a known limitation.

## Migration Plan

1. Land D1 (CLI cosmos shadow) — pure bug fix, no surface change.
2. Land D2 (AMINO canonicalisation) — behavioural fix; document explicitly in the changelog.
3. Land D3 (swallowed errors) — pure bug fix.
4. Land D8, D9 (Allow header, 201) — small standalone.
5. Land D5, D6 (typed Signature, discriminator) together — they share `pkg/types/types.go`.
6. Land D4 (/v1/ prefix) — last, since it touches every route registration and the OpenAPI annotation set.
7. Land D7 (pagination) — independent of the others.

Rollback: each item is independently revertable. D4 is reverted by deleting the dual-mount block.

## Open Questions

- Should we delete the bare `/sign/*` and `/keys` routes immediately rather than deprecate? **Recommended answer:** no, keep them for the next minor-version cycle. Add `Deprecation` + `Sunset` HTTP headers on the bare routes per RFC 8594.
- Should `/v1/health` be added? **Recommended answer:** yes, for consistency, even though health endpoints are conventionally unversioned. Cheap.
