## Context

The `kms-wrapper` CLI has full key-lifecycle commands (`keys create | show | list`) that talk to the Vault Transit plugin via `internal/vault.Client`. The REST gateway, by contrast, only exposes signing and health endpoints. Today, an integration test or a sibling service that needs to provision a new signing key has two awkward options: shell into a host and exec the CLI, or call the Vault Transit plugin directly (which means re-implementing path validation, response decoding, and address derivation in every consumer).

We want a small, additive surface on the REST gateway that mirrors the CLI's key-management behavior exactly, sharing the same `vault.Client` primitives so the two paths never drift. The design choices below all come from explicit user input during proposal — see the inline references.

**Current state (load-bearing files):**

- `cmd/kms-wrapper/root.go:95-152` — `keysCmd` defines `create`, `show`, `list`. `printKeyInfo` (`root.go:154-172`) is the address-derivation helper.
- `internal/vault/client.go` — `CreateKey` (line 113), `GetPublicKey` (line 124), `ListKeys` (line 188). All take a logical key path (`{project}/{chain}/{username}`) validated by `ValidateKeyPath` / `ToVaultPath` in `internal/vault/path.go`.
- `internal/signer/evm.DeriveEVMAddress` + `internal/signer/cosmos.DeriveCosmosAddress` — derivation helpers, already used by `printKeyInfo`.
- `internal/gateway/gateway.go` — auth + rate-limit middleware (`auth`, `rateLimit`) and the existing `signEVM` / `signCosmos` handlers. Swagger annotations live next to handlers.
- `pkg/types/types.go` — request/response structs with swaggo tags. `KeyInfo` already exists (line 61).

**Stakeholders:** gateway operators, integration-test authors, and any client service that needs programmatic key provisioning.

**Constraints:**

- No new Vault plugin endpoints or permissions. Use the existing `kms-vault-plugin` LIST/READ/WRITE surface.
- Auth must reuse `KMS_GATEWAY_TOKEN` and `auth` middleware. No new tokens or auth schemes.
- Same rate-limit budget as `/sign/*` (per explicit user choice).
- No breaking changes to existing endpoints, CLI flags, or config keys.
- Errors must never leak Vault tokens, raw Vault error strings that include host/IP information, or key material. Stay consistent with the existing `apptypes.ErrorResponse` shape.
- Generated docs (`docs/swagger.json`, `docs/swagger.yaml`, `docs/docs.go`) must stay in sync — CI's `make swagger-check` will fail otherwise.

## Goals / Non-Goals

**Goals:**

- Expose three HTTP endpoints (`POST /keys`, `GET /keys/info?path=`, `GET /keys?prefix=`) that produce byte-identical results to `kms-wrapper keys create|show|list` for the same inputs.
- Reuse `vault.Client.CreateKey/GetPublicKey/ListKeys` and the existing EVM/Cosmos derivation helpers — one code path per operation, called by both CLI and gateway.
- Document the new endpoints in the OpenAPI 3.0 spec via swaggo annotations; regenerate `docs/` and commit, so CI's `swagger-check` passes.
- Test coverage in `gateway_test.go` for: happy path, missing/invalid input, auth failures (missing & wrong token), `404` on unknown key path, idempotent re-create, list with empty result, and shared rate-limit interaction with `/sign/*`.

**Non-Goals:**

- **No `delete`, `rotate`, or `update` endpoints.** Out of scope; the user asked specifically for create/list/show.
- **No CLI behavior change.** CLI output format stays as-is (JSON `KeyInfo` for create/show, line-per-name for list).
- **No new config knobs.** No `keys_rate_limit`, no `keys_enabled`, no `KMS_GATEWAY_KEYS_*` env vars. Same auth and limiter as `/sign/*`.
- **No pagination on list.** The Vault plugin's LIST is currently flat; mirroring CLI behavior.
- **No new derivation paths or HRPs.** Default Cosmos HRP stays `cosmos` (matches `printKeyInfo`). If callers need a different HRP, that's a follow-up.
- **No bulk endpoints.** No `POST /keys/batch`, no multi-key create. One key per request.

## Decisions

### Decision: REST-y mixed verbs, key path in body for create, in query for read

**Choice:**

- `POST /keys` with JSON body `{"path": "proj-a/evm/alice"}` for create.
- `GET /keys/info?path=proj-a/evm/alice` for show.
- `GET /keys?prefix=proj-a/` for list (`prefix` optional; empty → list top-level).

**Why:** Idiomatic HTTP semantics — create is a non-idempotent mutation by URL (the resource collection is `/keys`), reads are GETs and cacheable. The `/keys/info` sub-path for show avoids a path-parameter conflict with the list endpoint while keeping slashes inside the `path` query string un-encoded for human readability in browser/Swagger try-it-out. Pure-REST `GET /keys/{path}` was rejected because key paths contain three slashes (`{project}/{chain}/{username}`), forcing `%2F` encoding that's painful in Swagger UI and curl one-liners. RPC-style `POST /keys/create | /keys/show | /keys/list` was rejected as needlessly un-RESTful for endpoints where standard verbs fit cleanly.

**Trade-off:** The `GET /keys/info` sub-path is slightly unusual (REST purists might expect `GET /keys/{id}`). Acceptable cost for keeping key paths un-encoded in the URL.

### Decision: Create is idempotent — always `200 OK` with `already_existed` flag

**Choice:** `POST /keys` always returns `200 OK` with a `KeyCreateResponse`:

```json
{
  "path": "proj-a/evm/alice",
  "public_key_hex": "04...",
  "evm_address": "0xAb12...",
  "cosmos_address": "cosmos1...",
  "already_existed": false
}
```

When the same path is posted again, the response is identical except `already_existed: true` (and the key material is unchanged — Vault Transit's idempotent create returns the existing key).

**Why:** Matches the CLI's documented contract (`cli` spec: "Key already exists → exits 0 (idempotent)"). Callers don't have to branch on status codes to detect "already there"; they get the same `KeyInfo` either way and an explicit boolean if they care to log it. `201/200` split was rejected because it forces every caller to write a 2xx-tolerant switch when the underlying semantics are uniform. `201/409` was rejected outright — it breaks CLI parity and turns a routine repeat-call into an error-handling path.

**Trade-off:** "Always 200" loses the HTTP-level signal that something was newly created. Mitigated by the `already_existed` field, which is also more informative than `201` alone (a `201` doesn't tell you whether the key material was reused).

**Detecting `already_existed`:** Vault Transit's plugin returns the same response shape for a fresh-create and an existing-read, with no `created_at` or `version` flag we can inspect on a `WriteWithContext`. Implementation will read first (`GetPublicKey`), set `already_existed = true` if it returns a key, then call `CreateKey` (which is idempotent at the plugin layer); race-safe because if two creators race, the second sees `already_existed = true` even though it technically lost the race — semantically equivalent outcome.

### Decision: List returns bare names, mirror CLI output

**Choice:** `GET /keys?prefix=<p>` returns

```json
{
  "keys": ["evm/alice", "cosmos/bob"],
  "count": 2
}
```

Names are returned as Vault's plugin LIST emits them (the suffix under the prefix), not the full canonical path.

**Why:** One Vault round-trip per request regardless of result size. Enriching each entry with derived EVM/Cosmos addresses would mean N+1 Vault reads + 2N derivations per request — slow on large prefixes and a foot-gun if a tenant has thousands of keys. Callers who need full info per key can iterate with `GET /keys/info?path=<full-path>`. Matches the CLI, which prints one name per line.

**Trade-off:** Two round-trips for a UI that wants to render a key picker with addresses. Acceptable — that's a downstream concern and a future enriched-list endpoint can ship later without breaking this one.

### Decision: Shared bearer auth + shared rate limiter

**Choice:** Mount the three routes through the same `requestLogger → rateLimit → auth` chain that protects `/sign/*`:

```go
mux.Handle("POST /keys",       s.rateLimit(s.auth(http.HandlerFunc(s.createKey))))
mux.Handle("GET /keys/info",   s.rateLimit(s.auth(http.HandlerFunc(s.showKey))))
mux.Handle("GET /keys",        s.rateLimit(s.auth(http.HandlerFunc(s.listKeys))))
```

The bearer token is `KMS_GATEWAY_TOKEN`; the limiter is the existing `rate.NewLimiter(cfg.Gateway.RateLimit, cfg.Gateway.RateBurst)`.

**Why:** Single token surface, single rate budget. Operational consistency: every authenticated endpoint behaves the same way. Adding a second limiter or a second token would expand the config blast-radius without a concrete pain point to justify it.

**Trade-off:** A burst of `/keys` calls can starve `/sign/*` and vice versa. In practice the gateway is internal and traffic is low; if this becomes a real problem we can split the limiter as a follow-up. The user explicitly chose the shared posture.

### Decision: Reuse one code path; extract `deriveKeyInfo` if duplication crosses the threshold

**Choice:** The gateway's `createKey` and `showKey` handlers must produce the same `KeyInfo` shape as the CLI's `printKeyInfo`. We achieve that by calling the same three primitives in the same order:

```go
pub, err := s.vaultClient.GetPublicKey(ctx, path)
evmAddr, _ := evm.DeriveEVMAddress(pub)
cosmosAddr, _ := cosmos.DeriveCosmosAddress(pub, "cosmos")
```

If the resulting code in `gateway.go` becomes more than a few lines duplicated with `cmd/kms-wrapper/root.go:154-172`, extract a small helper. Two reasonable homes:

1. **`internal/vault.KeyInfoFor(ctx, path string, hrp string) (KeyInfo, error)`** — semantically wrong (Vault client shouldn't know about EVM/Cosmos derivation), but a single line for callers.
2. **Unexported `gateway.deriveKeyInfo(ctx, c *vault.Client, path string)` reused by CLI via re-export** — clean separation but cross-package coupling between cmd and internal/gateway.

**Preferred:** Add a small helper in a new `internal/keyinfo` package: `keyinfo.For(ctx, c VaultClient, path, hrp string) (apptypes.KeyInfo, error)`. Both CLI and gateway depend on `internal/keyinfo`, no circular imports, derivation logic lives in one place. Tasks call this out as optional; the minimum bar is that the two call sites invoke the same three primitives in the same order.

**Why this matters:** If CLI and gateway diverge on, e.g., EIP-55 casing or default HRP, downstream consumers will silently get different addresses for the same key — exactly the kind of drift that costs hours during incidents.

**Trade-off:** A third internal package is one more thing to know about. Acceptable.

### Decision: Vault client lives on the gateway `Server` struct

**Choice:** Add a new field to `gateway.Server`:

```go
type Server struct {
    ...
    vaultClient *vault.Client // for key management; existing `vault HealthChecker` stays for /health
}
```

`gateway.New` gets an extra parameter `vaultClient *vault.Client`. `cmd/kms-wrapper/root.go:90` (the `serve` command) updates its call to pass `c` (already constructed) twice — once as `HealthChecker` for backward compat, once as the typed `vault.Client` for key management.

**Why:** The existing `HealthChecker` interface deliberately exposes only `Health()` to keep the gateway test-double surface small. Key management needs `CreateKey | GetPublicKey | ListKeys`, which is too much to add to `HealthChecker` without losing the interface's purpose. A separate typed dependency is honest about what the gateway actually uses.

**Alternative considered:** Define a new `KeyStore` interface in `internal/gateway/gateway.go` with the three methods and wire `*vault.Client` to satisfy it. **Preferred** for test ergonomics — `gateway_test.go` can use a fake `KeyStore` instead of standing up a full Vault. Tasks call this out as the implementation form.

```go
type KeyStore interface {
    CreateKey(ctx context.Context, path string) error
    GetPublicKey(ctx context.Context, path string) ([]byte, error)
    ListKeys(ctx context.Context, prefix string) ([]string, error)
}
```

**Trade-off:** A fifth dependency on `Server`. Trivial.

### Decision: Error mapping mirrors today's `/sign/*` discipline

**Choice:**

| Condition | HTTP status | Body |
|---|---|---|
| Body not JSON / malformed | 400 | `{"error": "invalid JSON"}` |
| `path` missing or empty (create / show) | 400 | `{"error": "path is required"}` |
| `path` fails `ValidateKeyPath` | 400 | `{"error": "<validation message>"}` (e.g. `"key path segments must match [a-z0-9_-]"`) |
| Key not found on `GET /keys/info` | 404 | `{"error": "key not found: <path>"}` |
| Vault permission denied | 403 | `{"error": "permission denied"}` |
| Other Vault failure | 500 | `{"error": "vault error"}` (full error logged at `slog.Error` server-side) |
| Auth failure | 401 | `{"error": "unauthorized"}` (existing middleware) |
| Over rate limit | 429 | `{"error": "rate limit exceeded"}` (existing middleware) |

`vault.mapVaultErr` (in `internal/vault/client.go`) already classifies errors into `types.ErrNotFound | types.ErrPermission`. Handlers `errors.Is`-check those and map to 404 / 403 respectively; everything else is 500 with a redacted body. Same redaction discipline as `signEVM`: log the full error with `slog.ErrorContext`, return `"vault error"` (or `"signing failed"`-equivalent) to the client.

**Why:** Consistency with `/sign/*` is more valuable than per-endpoint cleverness. Vault error strings can include host, network, or token-hash fragments — sanitize at the boundary.

### Decision: Swagger annotation strategy

**Choice:** Annotate the three new handlers in `internal/gateway/gateway.go` with swaggo declarative comments (same style as `signEVM` / `signCosmos`). Add three new struct types to `pkg/types/types.go` for the request/response shapes:

```go
type KeyCreateRequest struct {
    Path string `json:"path" binding:"required" example:"proj-a/evm/alice"`
}

type KeyCreateResponse struct {
    apptypes.KeyInfo
    AlreadyExisted bool `json:"already_existed" example:"false"`
}

type KeyListResponse struct {
    Keys  []string `json:"keys" example:"evm/alice,cosmos/bob"`
    Count int      `json:"count" example:"2"`
}
```

Annotations declare `@Tags keys`, `@Security BearerAuth`, and the response codes. Regenerate via `make swagger`; commit `docs/`.

**Why:** Identical pattern to `add-swagger-docs`. Keeps the spec next to the code that implements it; `make swagger-check` in CI catches drift.

**Trade-off:** Slightly more boilerplate per handler. Worth it for the generated contract.

## Risks / Trade-offs

- **[Risk] CLI and gateway drift on address derivation.** If `printKeyInfo` and `gateway.showKey` ever stop calling the same helpers in the same order, the same key can render with different EVM checksums or Cosmos addresses across the two surfaces. → **Mitigation:** Extract `internal/keyinfo.For(...)` (decision above) and call it from both CLI and gateway. Add a `keyinfo_test.go` that pins one fixed compressed-pubkey input to known EVM + Cosmos outputs.
- **[Risk] `already_existed` flag race.** The "read-then-create" idempotency detection (decision above) is racy: two concurrent creators both see "not present", both call `CreateKey`, both get `already_existed: false`. → **Mitigation:** Document that `already_existed` is best-effort, not a synchronization primitive. The underlying Vault behavior (idempotent create) remains correct — only the flag is approximate. If callers need strict first-writer semantics, that's a separate change.
- **[Risk] Slow list on large prefixes.** Even bare-name list returns every key under a prefix in one shot. A tenant with thousands of keys gets a large response. → **Mitigation:** Vault Transit plugin's LIST is already bounded by Vault's response size limits. If we see operational pain, add `?limit=&cursor=` in a follow-up — not blocking for v1.
- **[Risk] Rate-limit cross-talk between `/keys` and `/sign`.** A misbehaving operator script flooding `/keys` can starve `/sign/*`. → **Mitigation:** Accept for v1 (explicit user choice). Document the shared-budget posture in `README.md`. If it bites, split the limiter behind a config flag.
- **[Risk] Swagger drift on PR.** Adding three handlers without regenerating `docs/` would make `make swagger-check` fail in CI but allow the binary to build locally. → **Mitigation:** Tasks 5.x explicitly require regenerating and committing the docs as part of the change; the existing `swagger-check` CI step is the safety net.
- **[Trade-off] Three new handlers, ~150 lines plus annotations and tests.** Modest gateway surface growth. Acceptable for closing the lifecycle gap.

## Migration Plan

Fully additive — no existing API contracts change.

1. Land code + annotations + regenerated docs in a single PR; CI's `swagger-check` enforces consistency.
2. No deployment-time config changes required. Helm values and env templates are unchanged.
3. Rollback: revert the binary. No schema, vault, or persistent-data changes to undo.
4. Inform consumers (signer-team Slack channel) once shipped, pointing at `/swagger/index.html` for the contract.

## Open Questions

- **Default Cosmos HRP exposure.** Should `GET /keys/info` accept an optional `?hrp=` query parameter to render the Cosmos address under a different bech32 prefix (e.g. `cosmos`, `osmosis`, or any other reserved chain HRP)? CLI today hardcodes `cosmos`. Leaning yes (`?hrp=` query, default `cosmos`) — adds zero complexity and is consistent with `kms-wrapper sign cosmos --hrp ...`. Confirm during implementation; if added, document in the `rest-gateway` spec.
- **Error code for vault permission denied.** Today `signEVM` returns 500 for all Vault failures including permission. Should `/keys/*` distinguish 403? Leaning yes for `/keys/*` (better operator UX), but that creates an asymmetry with `/sign/*`. If we add it here, consider following up on `/sign/*` for consistency.
- **Should `already_existed` be a header instead of a body field?** A custom header (`X-Key-Already-Existed: true`) keeps the success body shape pure. Body field is more discoverable; sticking with body for now.
