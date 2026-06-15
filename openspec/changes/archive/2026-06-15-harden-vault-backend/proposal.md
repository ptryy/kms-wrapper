## Why

A deep review surfaced four critical/high-severity defects in the Vault-backed signing path that defeat tenant isolation, hide token-expiry failures, and rely on brittle string matching for error classification — plus one medium-severity correctness/perf issue where per-sign public-key fetches double Vault round-trips. Left as-is, a compromise of the gateway token grants cross-tenant key access and a token-renewal failure surfaces only as opaque 500s on `/sign`.

## What Changes

- **Plugin-side key-name validation (defense in depth):** `internal/plugin/path_keys.go` and `path_sign.go` SHALL apply the same `{project}/{chain}/{username}` validator as the gateway. The plugin no longer trusts the gateway as the only enforcer.
- **Non-root Vault token for the gateway:** `vault/init.sh` installs `policy-project.hcl` and issues a scoped token; the gateway SHALL refuse to start when `Vault.Token` is `root` (or empty) unless `KMS_DEV=true` is set.
- **Typed Vault error mapping:** `internal/vault/client.go` `mapVaultErr` SHALL switch on `*vaultapi.ResponseError.StatusCode` via `errors.As`. The substring matcher (`"permission denied"`, `"403"`, `"not found"`, `"404"`) is removed.
- **Observable token renewal:** `StartRenewal` SHALL log every `LookupSelf`/`RenewSelf` failure at `warn`, key the tick off `info.Data["ttl"]/3` instead of a hard-coded 5 minutes, and retry the initial lookup with backoff so a transient Vault outage at startup does not silently disable renewal.
- **Public-key cache:** the vault client SHALL cache the immutable public key per key path (a `sync.Map`) so EVM and Cosmos signing each make one Vault call per sign instead of two.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `vault-backend`: adds typed error mapping, observable renewal, public-key caching, and a non-root-token startup guard.
- `key-path-policy`: adds a plugin-side enforcement requirement so name validation is not gateway-only.

## Impact

- `internal/plugin/path_keys.go`, `internal/plugin/path_sign.go`: import and call the shared path validator.
- `internal/vault/client.go`: rewrite `mapVaultErr`, instrument `StartRenewal`, add pubkey cache.
- `cmd/kms-wrapper/root.go`: refuse to start with `Vault.Token == "root"` outside dev mode.
- `vault/init.sh`: install `policy-project.hcl`, issue a scoped token, write it to `.env`.
- `policy-project.hcl`: confirm path globs match plugin mount (`kms/`) — current file references stale `transit/`.
- No REST or CLI request/response shape changes; no breaking changes for callers.
- New test surface in `internal/plugin/*_test.go` (bad names) and `internal/vault/client_test.go` (typed-error mapping, renewal logging).
