## 1. Types and shared derivation helper

- [ ] 1.1 Add `KeyCreateRequest`, `KeyCreateResponse` (embeds `KeyInfo` + `AlreadyExisted bool`), and `KeyListResponse` (`Keys []string`, `Count int`) structs to `pkg/types/types.go` with `json` and `binding`/`example` tags consistent with the existing structs in that file.
- [ ] 1.2 Add a `KeyInfoFor(ctx, c KeyStore, path, hrp string) (apptypes.KeyInfo, error)` helper in a new `internal/keyinfo` package; it SHALL call `c.GetPublicKey`, then `evm.DeriveEVMAddress`, then `cosmos.DeriveCosmosAddress(..., hrp)` (defaulting empty `hrp` to `"cosmos"`), wrapping `types.ErrNotFound` so callers can `errors.Is`-check.
- [ ] 1.3 Refactor `cmd/kms-wrapper/root.go:printKeyInfo` to call `keyinfo.For(..., "cosmos")` so CLI and gateway share the same derivation path. Verify CLI tests still pass: `go test ./cmd/...`.
- [ ] 1.4 Add `internal/keyinfo/keyinfo_test.go` with at least one fixed-input case (a known compressed pubkey → expected EVM EIP-55 + Cosmos bech32 addresses) and a not-found case using a fake `KeyStore`.

## 2. Gateway plumbing

- [ ] 2.1 Define a `KeyStore` interface in `internal/gateway/gateway.go` with `CreateKey(ctx, path) error`, `GetPublicKey(ctx, path) ([]byte, error)`, `ListKeys(ctx, prefix) ([]string, error)` — matching `*vault.Client`'s signatures so it satisfies the interface implicitly.
- [ ] 2.2 Add a `keys KeyStore` field to `gateway.Server` and a corresponding parameter to `gateway.New(cfg, vault HealthChecker, keys KeyStore, evm EVMSigner, cosmos CosmosSigner)`. Keep `HealthChecker` for `/health` (no signature break beyond the new parameter).
- [ ] 2.3 Update `cmd/kms-wrapper/root.go:90` (`serveCmd`) to pass the existing `*vault.Client` for both the health-checker and key-store parameters.
- [ ] 2.4 Update all existing call sites that construct `gateway.New(...)` (notably `internal/gateway/gateway_test.go`) to pass a fake or nil `KeyStore` to keep current tests compiling.

## 3. Handlers

- [ ] 3.1 Implement `func (s *Server) createKey(w http.ResponseWriter, r *http.Request)`:
  - Decode JSON body into `KeyCreateRequest` (max body 1<<20). On decode failure → 400 `"invalid JSON"`.
  - Validate `Path` non-empty (400 `"path is required"`) and pass to `vault.ValidateKeyPath` (400 with validation message on failure).
  - Probe existence: call `s.keys.GetPublicKey(ctx, path)`; on `types.ErrNotFound` set `alreadyExisted = false`, otherwise (no error) set `alreadyExisted = true`. Surface non-not-found errors via the error-mapping rules in 3.4.
  - Call `s.keys.CreateKey(ctx, path)`. On `types.ErrPermission` → 403; other errors → 500.
  - Call `keyinfo.For(ctx, s.keys, path, "cosmos")` to fetch full info; map errors the same way.
  - Write 200 with `KeyCreateResponse{KeyInfo: info, AlreadyExisted: alreadyExisted}`.
- [ ] 3.2 Implement `func (s *Server) showKey(w http.ResponseWriter, r *http.Request)`:
  - Read `path` from `r.URL.Query().Get("path")`. Validate non-empty (400 `"path is required"`) and `vault.ValidateKeyPath` (400 with validation message).
  - Call `keyinfo.For(ctx, s.keys, path, "cosmos")`. On `types.ErrNotFound` → 404 `"key not found: <path>"`. On `types.ErrPermission` → 403. Other → 500.
  - Write 200 with the resulting `apptypes.KeyInfo`.
- [ ] 3.3 Implement `func (s *Server) listKeys(w http.ResponseWriter, r *http.Request)`:
  - Read `prefix` from `r.URL.Query().Get("prefix")` (empty allowed).
  - Call `s.keys.ListKeys(ctx, prefix)`. On `types.ErrPermission` → 403; other errors → 500.
  - Normalise nil result to an empty slice so the JSON output is `[]` not `null`.
  - Write 200 with `KeyListResponse{Keys: ks, Count: len(ks)}`.
- [ ] 3.4 Add a shared `(s *Server) writeVaultErr(w, r, err, keyPath)` helper that classifies via `errors.Is` and writes 403/404/500 with redacted messages, logging the full error via `slog.ErrorContext`. Reuse from all three handlers.

## 4. Route registration

- [ ] 4.1 In `gateway.routes`, register the three routes through `requestLogger → rateLimit → auth`, matching the existing `/sign/*` chain:
  ```go
  mux.Handle("POST /keys",     s.rateLimit(s.auth(http.HandlerFunc(s.createKey))))
  mux.Handle("GET /keys/info", s.rateLimit(s.auth(http.HandlerFunc(s.showKey))))
  mux.Handle("GET /keys",      s.rateLimit(s.auth(http.HandlerFunc(s.listKeys))))
  ```
- [ ] 4.2 Confirm route ordering: `GET /keys/info` is registered before `GET /keys` if Go's `net/http` ServeMux pattern semantics require it for this version (Go 1.22+ pattern routing should disambiguate by literal segment; verify with a unit test that hits `/keys/info?path=...` and asserts the show handler, not the list handler, runs).

## 5. Swagger annotations and regenerated docs

- [ ] 5.1 Annotate the three handlers with swaggo comments: `@Summary`, `@Tags keys`, `@Accept json` (create only), `@Produce json`, `@Param`s, `@Success 200 {object} apptypes.{KeyCreateResponse|KeyInfo|KeyListResponse}`, `@Failure 400/401/403/404/429/500 {object} apptypes.ErrorResponse`, `@Security BearerAuth`, `@Router`.
- [ ] 5.2 Add struct-level annotations on `KeyCreateRequest`, `KeyCreateResponse`, `KeyListResponse` (examples, field types). `KeyCreateResponse` SHALL document the `already_existed` boolean and embed `KeyInfo` so swaggo emits a flat schema.
- [ ] 5.3 Run `make swagger` to regenerate `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`. Commit the regenerated files.
- [ ] 5.4 Run `make swagger-check` locally to confirm zero diff after regeneration.

## 6. Tests

- [ ] 6.1 In `internal/gateway/gateway_test.go`, add a fake `KeyStore` implementation with stubbable `CreateKey`/`GetPublicKey`/`ListKeys` behavior (per-test injection or table-driven).
- [ ] 6.2 Add tests for `POST /keys`:
  - 6.2.1 Happy path: new key → 200 with `already_existed: false` and populated `evm_address`, `cosmos_address`.
  - 6.2.2 Idempotency: same path twice → both 200, second response has `already_existed: true` and identical `public_key_hex`.
  - 6.2.3 Missing/empty `path` → 400 `"path is required"`.
  - 6.2.4 Invalid path format (e.g. `"Bad Path"`) → 400 with validation message.
  - 6.2.5 Malformed JSON body → 400 `"invalid JSON"`.
  - 6.2.6 Fake returns `types.ErrPermission` from `CreateKey` → 403 `"permission denied"`.
  - 6.2.7 Fake returns a generic Vault error → 500 `"vault error"` (and assert the error is not echoed in the body).
  - 6.2.8 Missing bearer token → 401 (auth middleware).
- [ ] 6.3 Add tests for `GET /keys/info?path=...`:
  - 6.3.1 Existing key → 200 with full `KeyInfo`.
  - 6.3.2 Not found (fake returns `types.ErrNotFound`) → 404 `"key not found: <path>"`.
  - 6.3.3 Missing `?path=` → 400 `"path is required"`.
  - 6.3.4 Invalid path format → 400 with validation message.
  - 6.3.5 Permission denied → 403.
  - 6.3.6 Missing bearer token → 401.
- [ ] 6.4 Add tests for `GET /keys`:
  - 6.4.1 Fake returns `["evm/alice", "cosmos/bob"]` → 200 with `{"keys": [...], "count": 2}`.
  - 6.4.2 Empty prefix → fake called with `""`, returns 200.
  - 6.4.3 Empty result (nil from fake) → 200 with `{"keys": [], "count": 0}` (assert no `null`).
  - 6.4.4 Permission denied → 403.
  - 6.4.5 Generic error → 500.
  - 6.4.6 Missing bearer token → 401.
- [ ] 6.5 Add a rate-limit test: construct a gateway with very low `RateLimit`/`RateBurst`, exhaust via `/sign/*` calls, then assert the next `/keys` call returns 429 — and vice-versa, confirming the shared budget per the `rest-gateway` shared-rate-limiter requirement.
- [ ] 6.6 Add a swagger-spec presence test: build the server with `swagger_enabled=true`, fetch `/swagger/doc.json`, unmarshal as a generic map, and assert `paths` contains `/keys` (with `get` and `post`) and `/keys/info` (with `get`). Assert each operation has `security: [{"BearerAuth": []}]`.
- [ ] 6.7 Add a route-precedence test confirming `GET /keys/info?path=p` routes to `showKey` (not `listKeys`).

## 7. Documentation

- [ ] 7.1 Update `README.md` "API documentation" section with a short table listing the new `/keys` endpoints, their bearer-auth requirement, and a curl example for each (create, show, list).
- [ ] 7.2 Note in `README.md` that the new endpoints share the `/sign/*` bearer token and rate-limit budget — pointing operators at `gateway.rate_limit` / `gateway.rate_burst` for tuning.

## 8. Validation

- [ ] 8.1 Run `go build ./...` to confirm clean compile.
- [ ] 8.2 Run `make test` (or `go test ./...`) and confirm all packages pass.
- [ ] 8.3 Run `make swagger-check` to confirm no doc drift.
- [ ] 8.4 Manual smoke: `kms-wrapper serve` against a local Vault dev server with `kms-vault-plugin`; `curl -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" -X POST -d '{"path":"smoke/evm/alice"}' http://127.0.0.1:8080/keys` returns expected JSON; second call returns `already_existed: true`; `GET /keys/info?path=smoke/evm/alice` returns the same `KeyInfo`; `GET /keys?prefix=smoke/` lists `["evm/alice"]`.
- [ ] 8.5 Run `openspec validate add-keys-rest-endpoints --strict` and confirm it passes.
