# Change: Per-key chain capability tag

## Why
With `update-key-path-scheme` removing the chain hint from the key path, the system no longer carries any signal of which chain a key is *meant* to sign for — every key is a secp256k1 keypair that both `/sign/evm` and `/sign/cosmos` can use. Some operators want a key restricted to a single chain (so a misrouted client cannot accidentally sign on the wrong chain), and some want one key to deliberately serve both. Today there is no way to express that intent.

This change introduces an explicit, per-key `chains` capability tag, set at create time, persisted by the plugin, and enforced at every sign endpoint. The tag is documentation **and** enforcement: a key tagged `["evm"]` will refuse a `/sign/cosmos` request with HTTP 403, in parallel at both the gateway and the plugin trust boundaries.

## What Changes
- **NEW** `POST /keys` request field `chains: ["evm" | "cosmos", ...]`. The field is **required** and must be a non-empty subset of `["evm", "cosmos"]`. Missing or empty → HTTP 400 (`"chains is required and must be a non-empty subset of [evm, cosmos]"`).
- **NEW** plugin storage: `KeyEntry` gains a `Chains []string` field, persisted alongside the secp256k1 key material on first create. The plugin SHALL canonicalize the list (lowercase, dedupe, sort) before storage.
- **NEW** enforcement: `/sign/evm`, `/sign/cosmos` (gateway) and the underlying plugin sign path SHALL load the key's `chains` tag and reject requests where the endpoint's chain is not in the tag, with HTTP 403 and body:
  ```json
  { "error": "key <path> not authorized for <chain> signing (allowed chains: [<list>])" }
  ```
  A structured `slog` warn log is emitted carrying `key_path`, `attempted_chain`, `allowed_chains`.
- **NEW** `PATCH /keys/{path}` endpoint accepting `{ "add_chains": ["cosmos", ...] }`. **Expand-only**: any request that would remove a chain (or any other field shape) SHALL be rejected with HTTP 400. Idempotent — adding a chain already present is a no-op and returns 200.
- **MODIFIED** `POST /keys` response and `GET /keys/info` response shape:
  - The response carries a new top-level `chains: [...]` field.
  - `evm_address` is included **only** when `chains` contains `"evm"`.
  - `cosmos_address` is included **only** when `chains` contains `"cosmos"`.
  - `public_key_hex` is always included (it is curve-level, not chain-level).
- **MODIFIED** `GET /keys` (list) responses to include `chains` per entry so callers can filter without an extra `/keys/info` round-trip.
- **MODIFIED** OpenAPI 3.0 spec (regenerated): new `chains` enum, new field on `KeyCreateRequest` / `KeyCreateResponse` / `KeyInfo`, new `PATCH /keys/{path}` operation, response examples updated.

## Impact
- Affected specs:
  - `key-path-policy` — ADD requirement for plugin chains-tag persistence and canonicalization.
  - `rest-gateway` — MODIFY `Create key endpoint`, `Show key endpoint`, `List keys endpoint`, `Sign EVM transaction endpoint`, `Sign Cosmos transaction endpoint`; ADD `Update key chains endpoint`.
  - `api-docs` — implicitly updated via `make swagger`; no spec delta needed because the existing `api-docs` requirements are structural ("the spec is generated from annotations") and the annotation surface is the change.
- Affected code:
  - `pkg/types/types.go` — new `Chain` enum type, `Chains []Chain` on `KeyCreateRequest`/`KeyCreateResponse`/`KeyInfo`/`KeyListEntry`, new `KeyUpdateChainsRequest`.
  - `internal/plugin/backend.go` — `KeyEntry.Chains []string`, canonicalization helper, serialization/deserialization round-trip.
  - `internal/plugin/path_keys.go` — accept `chains` on create, persist; reject create with empty/invalid `chains` (HTTP 400 from the plugin).
  - `internal/plugin/path_sign.go` — load `KeyEntry`, check `attempted_chain ∈ Chains`, reject with `logical.ErrPermission` mapped to HTTP 403 if not allowed.
  - **NEW** `internal/plugin/path_keys_update.go` — handle expand-only `update-chains` operation.
  - `internal/vault/client.go` — new method `UpdateKeyChains(ctx, path, addChains []string) error`; existing `CreateKey` grows a `chains []string` parameter.
  - `internal/gateway/gateway.go` — wire chains through create/show/list, add 403 enforcement before signer dispatch, add `PATCH /keys/{path}` handler.
  - `internal/keyinfo/keyinfo.go` — derive only the addresses corresponding to the key's chains.
  - `cmd/kms-wrapper/...` — CLI `keys create --chains evm,cosmos` flag; CLI `keys update-chains --path ... --add cosmos`.
- Test impact: every `/keys` test in `internal/gateway/*_test.go`, every plugin test in `internal/plugin/*_test.go`, plus new tests for 403 enforcement and PATCH endpoint.
- **Depends on**: `update-key-path-scheme` (must be applied first; both proposals MODIFY the same `Create key endpoint` requirement in `rest-gateway` and depend on the new path shape in examples).
