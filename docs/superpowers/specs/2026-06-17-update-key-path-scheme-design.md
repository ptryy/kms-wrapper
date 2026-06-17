# Design: update-key-path-scheme

**Date:** 2026-06-17
**Status:** Approved (migrated from legacy OpenSpec change `update-key-path-scheme`)
**Governing doc:** [`/CONSTITUTION.md`](../../../CONSTITUTION.md)
**Breaking:** Yes — clean break (no compatibility shim)

## Goal

Rename the middle key-path segment from `{chain}` to `{environment}`, make it free-form, and drop
the never-enforced reserved-chain convention. This is a terminology + validation-message change.
**No signing behavior changes.**

## Why

The path `{project}/{chain}/{username}` (e.g. `payment/evm/bob`) implies a key is bound to a chain.
In reality every key is one `secp256k1` keypair usable by both the EVM and Cosmos signers — nothing
enforces a chain-to-path binding. Operators have been confused by `evm`-named paths that sign Cosmos
transactions. Renaming to `{environment}` (e.g. `payment/prod/bob`) removes the false promise and
accurately describes what the segment scopes: a project-internal namespace (env/tier/deployment).

This also unblocks `add-key-chain-capability`, which introduces *real* per-key chain enforcement via
an explicit `chains` tag (the honest replacement for the path-implied binding).

## Scope

### Behavioral delta (the only logic change)
- `internal/keypath/keypath.go`:
  - `Validate` error message → `"key path must have format {project}/{environment}/{username}"`.
  - `ValidateListPrefix` error message → same new format string.
  - Package + function doc comments → `{project}/{environment}/{username}`.
  - **Unchanged:** the `^[a-z0-9_-]+$` per-segment regex, the 3-segment rule, empty-segment check.

### Removed (spec/doc only — no code exists to delete)
- The "Chain identifier conventions" reserved table (`evm`, `eth`, `mantra`, `cosmos`, `osmosis`)
  and the "unknown chain identifier" warning log were **documentation-only** in the OpenSpec spec;
  they were never implemented in `keypath.go`. Removal is a spec/README cleanup. Confirmed
  "none in-tree" by the legacy proposal and by reading `keypath.go`.

### Mechanical ripple (string hygiene, no logic)
- `pkg/types/types.go` — `example:"proj-a/evm/alice"` struct tags → `proj-a/prod/alice`.
- `internal/gateway/*.go` — swagger `@Param`/`@Description` examples and string literals.
- `internal/plugin/backend.go`, `internal/vault/client.go` — doc/help strings naming the format.
- Test literals: `internal/gateway/{gateway,keys,security,polish,observability}_test.go`,
  `internal/plugin/{backend,path_keys,path_sign}_test.go`,
  `internal/vault/{client,client_typed,path}_test.go`, `internal/keyinfo/keyinfo_test.go`,
  `internal/signer/cosmos/amino_canonical_test.go`, `internal/signer/evm/evm_test.go`.
- Operator surfaces: `vault/init.sh`, `README.md`, `.github/copilot-instructions.md`.
- Regenerated docs: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` via `make swagger`.

## Migration

Clean break. Old-shape keys (`{project}/{chain}/{username}`) become unreachable and must be
recreated at the new shape. Local dev is in-memory Vault (keys lost on `make dev-down`), so impact
is local-only. Production callers update Vault policies and recreate keys at the new paths.

## Error handling

Unchanged. Same validation error categories (missing segment / empty segment / bad characters);
only the format string embedded in the "missing segment" message changes.

## Testing

- Update scenario string literals to use `prod`/`staging`/`dev` environment names.
- `keypath` unit tests assert the new error-message wording.
- `go test ./...` and `make lint` green.
- `make swagger && make swagger-check` clean (regen matches committed docs).
- E2E: create `payment/prod/alice` via REST and via CLI; sign one EVM personal-message request and
  one Cosmos AMINO_JSON request against the same path.

## Risk

Low. The only true behavior change is the error-message wording. Everything else is string/doc
hygiene plus generated-doc regeneration, caught by `swagger-check` and the existing test suite.

## Constitution alignment

- Uses plugin-reality terms (`kms/keys/...`), not Transit — satisfies `CONSTITUTION.md §7`.
- Explicit BREAKING flag + migration notes — satisfies the "no breaking changes without migration
  notes" constraint (§6).
- No change to the security boundary, digesting model, or idempotent lifecycle (§4).
