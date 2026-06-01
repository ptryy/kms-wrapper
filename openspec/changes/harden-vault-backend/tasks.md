## 1. Shared key-path validator usable from the plugin

- [ ] 1.1 Verify whether `internal/plugin` importing `internal/vault` introduces a cycle. If yes, lift `ValidateKeyPath` (and its regex constants) into a new leaf package `internal/keypath/` and re-export from `internal/vault/path.go` for backward compat.
- [ ] 1.2 In `internal/plugin/path_keys.go`, call the validator at the top of `handleCreateKey` and `handleListKeys`; on validation error return `logical.ErrInvalidRequest` wrapping the validator message.
- [ ] 1.3 In `internal/plugin/path_sign.go`, call the validator on `nameStr` before any storage read; same error mapping.
- [ ] 1.4 Add table-driven tests in `internal/plugin/path_keys_test.go` and `path_sign_test.go` covering: uppercase, fewer-than-3 segments, empty segment, `..`, valid happy path.

## 2. Typed Vault error mapping

- [ ] 2.1 Replace the body of `mapVaultErr` in `internal/vault/client.go` with `errors.As` against `*vaultapi.ResponseError` switching on `StatusCode` (403 → `ErrPermission`, 404 → `ErrNotFound`, 400 → `ErrBadRequest` wrapping `rerr.Errors`).
- [ ] 2.2 If `types.ErrBadRequest` does not exist, add it next to `ErrPermission`/`ErrNotFound` in `pkg/types/errors.go`. Update gateway error mapping to return HTTP 400 for it.
- [ ] 2.3 Delete the legacy substring matcher and the `strings.Contains` helpers it relied on.
- [ ] 2.4 Add `client_test.go` cases that use `httptest.NewServer` to return 403 / 404 / 400 with bodies that do NOT contain the legacy substrings; assert the typed sentinel is returned.

## 3. Observable, TTL-adaptive token renewal

- [ ] 3.1 In `internal/vault/client.go:StartRenewal`, wrap the initial `LookupSelf` in a backoff loop (1, 2, 4, 8, 16, 30s, then 30s steady) that exits on success or `ctx.Done()`.
- [ ] 3.2 Compute tick interval from `info.Data["ttl"]` as `max(30*time.Second, ttl/3)` and reset the `time.Ticker` after each successful `LookupSelf` (re-read TTL because renewals can change it).
- [ ] 3.3 Replace `_, _ = c.api.Auth().Token().RenewSelf(0)` with capturing and logging the error at `warn`; log success at `debug` with new TTL.
- [ ] 3.4 Add a test using a stubbed Vault server that returns an error on the first `RenewSelf` and asserts a `warn` log line is emitted (capture via `slog.NewTextHandler` to a buffer).

## 4. Public-key cache

- [ ] 4.1 Add `pubkeyCache sync.Map` field to `vault.Client`. Initialise in `NewClient`.
- [ ] 4.2 In `GetPublicKey(ctx, path)`, check the cache first; on miss, fetch from the plugin and `Store` the result before returning.
- [ ] 4.3 Add a test that calls `GetPublicKey` twice for the same path against an `httptest` server with a request counter; assert exactly one underlying HTTP call.
- [ ] 4.4 Remove the redundant `GetPublicKey` call in `internal/signer/evm/evm.go:signWithRecovery` if it is no longer needed once the cache makes the cost trivial — or keep it but ensure both calls hit the cache.

## 5. Non-root token guard at startup

- [ ] 5.1 In `cmd/kms-wrapper/root.go`, after `cfg, err := cliState.load(...)` returns, add a guard: if `cfg.Vault.Token` ∈ `{"", "root", "dev", "dev-token", "change-me"}` and `os.Getenv("KMS_DEV") != "true"`, exit with `"refusing to start with weak vault token; set KMS_DEV=true for local dev"`.
- [ ] 5.2 When `KMS_DEV=true` and the token is weak, emit `slog.Warn("running with weak vault token (KMS_DEV=true)")` once at startup.
- [ ] 5.3 Apply the same guard logic to `serveCmd`, `keysCmd`, `signCmd`, `healthCmd` (any subcommand that constructs a `vault.NewClient`).
- [ ] 5.4 Tests: `cmd/kms-wrapper/root_test.go` (or a new file) asserts startup failure when token=`root` and `KMS_DEV` unset, and startup success when `KMS_DEV=true`.

## 6. Bootstrap policy install + scoped-token issuance

- [ ] 6.1 Audit `policy-project.hcl` path globs: replace any `transit/*` references with `kms/keys/+/*` and `kms/sign/+/*` to match the live plugin mount.
- [ ] 6.2 Extend `vault/init.sh` after the plugin-mount block: `vault policy write kms-project policy-project.hcl`.
- [ ] 6.3 Add `vault token create -policy=kms-project -ttl=24h -renewable=true -format=json` and parse the returned token via `jq -r .auth.client_token`.
- [ ] 6.4 Write the issued token to `.env` as `KMS_VAULT_TOKEN=<token>` (replace any existing line). Ensure the script fails (`set -e`) if any of the above steps fail.
- [ ] 6.5 In `KMS_DEV=true` mode (or when `init.sh` runs in a CI/local container), keep installing the policy but skip the token issuance — leave `VAULT_TOKEN=root` in `.env` for the dev workflow. Document this branch in `README.md` and `vault/init.sh` comments.
- [ ] 6.6 Add a smoke test in `make dev-up` that asserts the scoped token can `kms/keys/proj-a/evm/x` create AND cannot `kms/keys/proj-b/evm/x` create (e.g. a short bash check at the end of `init.sh`).

## 7. Verification and archive

- [ ] 7.1 `go test ./...` passes locally and in CI.
- [ ] 7.2 `make dev-up` succeeds end-to-end on a clean machine; manual `curl /sign/evm` against the gateway works using the issued scoped token, not root.
- [ ] 7.3 `openspec validate harden-vault-backend --strict` passes.
- [ ] 7.4 Run `openspec archive-change harden-vault-backend` (or `/openspec-archive-change`) once implementation is complete to merge the delta specs into `openspec/specs/vault-backend/spec.md` and `openspec/specs/key-path-policy/spec.md`.
