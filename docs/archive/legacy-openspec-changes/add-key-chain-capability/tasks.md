## 1. Types and shared validation
- [ ] 1.1 Add a closed-set `Chain` type (string enum: `"evm"`, `"cosmos"`) to `pkg/types/types.go` with a `ParseChains([]string) ([]Chain, error)` helper that canonicalizes (lowercase, dedupe, sort) and validates membership.
- [ ] 1.2 Extend `KeyCreateRequest` with `Chains []Chain` (`json:"chains" binding:"required"` + swagger `enums:"evm,cosmos"` per element).
- [ ] 1.3 Extend `KeyCreateResponse`, `KeyInfo`, and the per-entry `KeyListEntry` with `Chains []Chain`. Make `EVMAddress` and `CosmosAddress` `omitempty`.
- [ ] 1.4 Add a new `KeyUpdateChainsRequest{ AddChains []Chain }` type.
- [ ] 1.5 Unit-test the `ParseChains` helper: lowercase/dedupe/sort, reject empty, reject unknown values, reject non-string elements.

## 2. Plugin storage
- [ ] 2.1 Add `Chains []string` to the plugin's `KeyEntry` struct in `internal/plugin/backend.go`. Update the JSON serialization round-trip if it is field-based, or document the implicit shape if `encoding/json` already handles it.
- [ ] 2.2 In `internal/plugin/path_keys.go:createKey`, read `data.Get("chains")`, canonicalize via the shared helper, and reject empty/invalid with `logical.ErrInvalidRequest` ("chains is required and must be a non-empty subset of [evm, cosmos]").
- [ ] 2.3 Idempotent re-create: if the key already exists, the existing `Chains` SHALL be preserved verbatim; the request `chains` SHALL match (set equality) or the plugin SHALL return `logical.ErrInvalidRequest` ("chains mismatch on idempotent create"). Document this in the field description.
- [ ] 2.4 Add `internal/plugin/path_keys_update.go` exposing an `update-chains` operation. Validates `add_chains` membership, computes new = union(existing, add_chains), writes only if new differs from existing. Returns the canonical new list. Reject any payload field other than `add_chains` (no `chains` or `remove_chains` allowed).
- [ ] 2.5 Unit tests in `internal/plugin/path_keys_update_test.go`: idempotent add, add new chain, reject unknown chain, reject empty add_chains, reject `remove_chains`, reject `chains` field.

## 3. Plugin sign-time enforcement
- [ ] 3.1 In `internal/plugin/path_sign.go`, the sign handler SHALL accept a new required `chain` parameter (string, `evm`|`cosmos`), look up the `KeyEntry`, and reject with `logical.ErrPermission` if `chain ∉ KeyEntry.Chains`. The error message SHALL be exactly: `"key <path> not authorized for <chain> signing (allowed chains: [<sorted-list>])"`.
- [ ] 3.2 Unit tests in `internal/plugin/path_sign_test.go`: sign with allowed chain succeeds, sign with disallowed chain returns 403-mapped error, sign on a key created before the change (no `Chains` field) is treated as `[]` → always denied (so legacy data fails closed).

## 4. Vault client
- [ ] 4.1 Extend `internal/vault/client.go::CreateKey` signature with `chains []string` parameter; pass through to the plugin write call.
- [ ] 4.2 Add `internal/vault/client.go::UpdateKeyChains(ctx, path, addChains []string) ([]string, error)` that POSTs to the plugin's `update-chains` operation and returns the new canonical list.
- [ ] 4.3 Extend `internal/vault/client.go::Sign` (or whichever method is the sign entrypoint) to pass `chain` through to the plugin sign call.
- [ ] 4.4 Update typed error mapping tests in `internal/vault/client_typed_test.go` so the chain-403 case is asserted (HTTP 403 from plugin → `types.ErrPermission` with the chain-mismatch message preserved).

## 5. Key info derivation
- [ ] 5.1 In `internal/keyinfo/keyinfo.go`, change the derivation function to accept `chains []Chain` and return a struct where `EVMAddress` is computed only if `evm` in chains and `CosmosAddress` only if `cosmos` in chains. `PublicKeyHex` is always computed.
- [ ] 5.2 Update `internal/keyinfo/keyinfo_test.go` to cover the three cases: `[evm]`, `[cosmos]`, `[evm, cosmos]`.

## 6. Gateway: create / show / list
- [ ] 6.1 In `internal/gateway/gateway.go::handleCreateKey`, parse and validate `chains` from the request body; reject empty/missing with HTTP 400 (`"chains is required and must be a non-empty subset of [evm, cosmos]"`). Pass `chains` through to `vault.CreateKey`.
- [ ] 6.2 Build the response from `keyinfo.Derive(pubkey, chains)` so `evm_address` and `cosmos_address` are conditionally present. Always include the `chains` field in the response.
- [ ] 6.3 Update the show endpoint (`/keys/info`) to load the key's `chains` from the plugin and emit the same conditional shape.
- [ ] 6.4 Update the list endpoint (`/keys`) so each `KeyListEntry` includes `chains`; the underlying list operation may need to read each `KeyEntry` for the tag (an extra storage read per entry — accept the cost; if it becomes a problem the future optimization is to include `chains` in the list metadata).

## 7. Gateway: sign enforcement
- [ ] 7.1 In `internal/gateway/gateway.go::handleSignEVM`, before calling the signer, fetch the key's `chains` (via `vault.GetKeyChains(ctx, path)` — add this method if not present) and reject with HTTP 403 if `"evm" ∉ chains`. Body: `{"error": "key <path> not authorized for evm signing (allowed chains: [<list>])"}`. `slog.WarnContext` with `key_path`, `attempted_chain="evm"`, `allowed_chains`.
- [ ] 7.2 Mirror in `handleSignCosmos` with `attempted_chain="cosmos"`.
- [ ] 7.3 Cache the `chains` lookup in the same per-process cache that already memoizes public keys (see `internal/vault/client.go` `GetPublicKey` cache). Invalidate on PATCH success.
- [ ] 7.4 Tests in `internal/gateway/security_test.go` (or a new `chain_capability_test.go`): EVM sign on `[evm]` key → 200; EVM sign on `[cosmos]` key → 403 with the exact error body; Cosmos sign on `[evm]` key → 403; both chains on a `[evm, cosmos]` key → 200.

## 8. Gateway: update-chains endpoint
- [ ] 8.1 Add `PATCH /keys/{path}` (and `/v1/keys/{path}`) handler. Validate `add_chains` is non-empty, all members are in the closed set, no other fields present.
- [ ] 8.2 Call `vault.UpdateKeyChains`. Return HTTP 200 with `{"path": "<p>", "chains": [<new canonical list>]}`.
- [ ] 8.3 Reject `{"chains": [...]}` or `{"remove_chains": [...]}` payloads with HTTP 400 ("only add_chains is supported").
- [ ] 8.4 Wrap in the same bearer-token middleware and rate limiter as the other `/keys` routes.
- [ ] 8.5 Invalidate the per-process `chains` cache on success.
- [ ] 8.6 Tests: expand `[evm]` → `[evm, cosmos]` returns the new list; expanding to a chain already present is a no-op (200, no change); unknown chain → 400; missing field → 400; remove attempt → 400; unauthorized → 401.

## 9. CLI
- [ ] 9.1 `kms-wrapper keys create` grows `--chains evm,cosmos` (required). Help text spells out the closed set.
- [ ] 9.2 New `kms-wrapper keys update-chains --path <p> --add evm,cosmos` subcommand.
- [ ] 9.3 CLI tests / integration tests under `cmd/kms-wrapper/...` cover both new flags.

## 10. Generated docs
- [ ] 10.1 Update swagger annotations on the new and modified handlers in `internal/gateway/gateway.go`.
- [ ] 10.2 Run `make swagger` to regenerate `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`.
- [ ] 10.3 `make swagger-check` passes.

## 11. Tests across the rest of the repo
- [ ] 11.1 Update every `POST /keys` test fixture in `internal/gateway/*_test.go` to include `chains: ["evm", "cosmos"]` (the easy path) or a chain-specific value if the test is about that.
- [ ] 11.2 Update plugin fixture tests similarly.
- [ ] 11.3 `go test ./...` green.

## 12. Spec deltas
- [ ] 12.1 Confirm `openspec/changes/add-key-chain-capability/specs/key-path-policy/spec.md` ADDs the chains-persistence requirement.
- [ ] 12.2 Confirm `openspec/changes/add-key-chain-capability/specs/rest-gateway/spec.md` MODIFIES Create / Show / List / Sign-EVM / Sign-Cosmos requirements and ADDs the Update endpoint.
- [ ] 12.3 Run `openspec validate add-key-chain-capability --strict --no-interactive`.

## 13. Verification
- [ ] 13.1 End-to-end manual: create `payment/prod/alice` with `chains=[evm]`, sign EVM (200), sign Cosmos (403 with correct body and log), PATCH add cosmos, sign Cosmos (200).
- [ ] 13.2 `make lint` and `go test ./...` green.
- [ ] 13.3 Confirm `docs/swagger.json` shows the closed-set enum and the PATCH operation.
