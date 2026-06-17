# Change: Replace `{chain}` segment with `{environment}` in key paths

## Why
Today's key path `{project}/{chain}/{username}` (e.g. `payment/evm/bob`) implies the key is bound to a specific chain. In reality every key is a single secp256k1 keypair stored in Vault and is usable by **both** the EVM signer and the Cosmos signer — nothing in the gateway, the plugin, or the Vault policy enforces a chain-to-path binding. Operators have already been confused by paths that contain `evm` yet successfully sign Cosmos transactions. Renaming the middle segment to `{environment}` (e.g. `payment/prod/bob`) eliminates the false promise of chain isolation and accurately reflects what the segment actually scopes: a project-internal namespace (env, tier, deployment).

## What Changes
- **BREAKING**: Key path format changes from `{project}/{chain}/{username}` to `{project}/{environment}/{username}`. All existing keys under the old shape are unreachable until recreated.
- **BREAKING**: The reserved chain identifier conventions table (`evm`, `eth`, `mantra`, `cosmos`, `osmosis`) is removed. The `{environment}` segment is free-form; only the `[a-z0-9_-]` regex and 3-segment count are enforced.
- **BREAKING**: The "unknown chain identifier" warning log is removed.
- Validator error message is updated: `"key path must have format {project}/{environment}/{username}"`.
- All example paths in specs, scenarios, struct tags (`example:"..."`), CLI help, tests, and `README.md` are updated to use environment names (`prod`, `staging`, `dev`) instead of chain names.
- No dual-mode or backwards-compat shim. Existing dev/local Vault keys must be recreated at the new paths.

## Impact
- Affected specs:
  - `key-path-policy` — primary (validator format, chain conventions removed, plugin enforcement examples).
  - `vault-backend` — example string in cache-test scenario only (no behavior change).
  - `rest-gateway` — example strings in `/keys` and `/keys/info` scenarios only (no behavior change).
  - `cli` — example string in `keys create` scenario only (no behavior change).
- Affected code:
  - `internal/keypath/keypath.go` — error message update.
  - `pkg/types/types.go` — `example:"proj-a/evm/alice"` struct tags → `example:"proj-a/prod/alice"`.
  - `internal/gateway/gateway.go` — swagger annotation examples in handler doc comments.
  - All test files in `internal/gateway/`, `internal/plugin/`, `internal/vault/`, `internal/keyinfo/`, `internal/signer/cosmos/`, `internal/signer/evm/` — string literals.
  - `vault/init.sh` — any hard-coded example paths in policy or bootstrap fixtures.
  - `README.md`, `.github/copilot-instructions.md` — documentation examples.
  - `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` — regenerated via `make swagger`.
- Migration: clean break. Local dev environments scrub and recreate keys via `make scrub-env && make dev-down && make dev-up`. Production callers update their Vault policies and recreate keys at the new paths.
