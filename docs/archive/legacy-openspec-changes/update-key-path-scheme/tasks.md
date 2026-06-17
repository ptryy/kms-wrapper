## 1. Validator and shared types
- [ ] 1.1 Update `internal/keypath/keypath.go` error messages to reference `{project}/{environment}/{username}` (replace both string literals returned from `Validate` and `ValidateListPrefix`).
- [ ] 1.2 Update doc comment on `keypath.Validate` to use the new format.
- [ ] 1.3 Update `pkg/types/types.go` swagger `example:"..."` struct tags from `proj-a/evm/alice` to `proj-a/prod/alice` on every type that carries one.

## 2. Gateway annotations and examples
- [ ] 2.1 Update swagger handler annotations in `internal/gateway/gateway.go` (`@Param` examples, `@Description` text) to use environment-based examples.
- [ ] 2.2 Search-and-replace remaining string literals (`proj/evm/alice`, `proj-a/evm/alice`, `proj/cosmos/alice`, etc.) in `internal/gateway/*.go` non-test sources.

## 3. Plugin and Vault client
- [ ] 3.1 Update `internal/plugin/backend.go` help text and any HelpSynopsis strings that name the format.
- [ ] 3.2 Update `internal/vault/client.go` doc comments that name the format.

## 4. Tests
- [ ] 4.1 Update string literals in `internal/gateway/{gateway_test,keys_test,security_test,polish_test,observability_test}.go` to use new paths.
- [ ] 4.2 Update `internal/plugin/{backend_test,path_keys_test,path_sign_test}.go`.
- [ ] 4.3 Update `internal/vault/{client_test,client_typed_test,path_test}.go`.
- [ ] 4.4 Update `internal/keyinfo/keyinfo_test.go`.
- [ ] 4.5 Update `internal/signer/cosmos/amino_canonical_test.go` and `internal/signer/evm/evm_test.go`.
- [ ] 4.6 Run `go test ./...` and resolve failures.

## 5. Bootstrap and operator surfaces
- [ ] 5.1 Update `vault/init.sh` — any hard-coded example paths in policy templates or bootstrap fixtures.
- [ ] 5.2 Update `README.md` worked examples.
- [ ] 5.3 Update `.github/copilot-instructions.md` examples.
- [ ] 5.4 Update `.claude/settings.local.json` if any allowlist entries pin chain-shaped paths.

## 6. Generated docs
- [ ] 6.1 Run `make swagger` to regenerate `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`.
- [ ] 6.2 Run `make swagger-check` to confirm regeneration is clean.

## 7. Spec deltas (applied at archive time)
- [ ] 7.1 Confirm `openspec/changes/update-key-path-scheme/specs/key-path-policy/spec.md` reflects MODIFIED + REMOVED requirements per the proposal.
- [ ] 7.2 Run `openspec validate update-key-path-scheme --strict --no-interactive` and resolve any reported issues.

## 8. Verification
- [ ] 8.1 `make scrub-env && make dev-down && make dev-up` succeeds against the new validator.
- [ ] 8.2 End-to-end manual check: create `payment/prod/alice` via REST and via CLI, sign one EVM personal-message request and one Cosmos AMINO_JSON request against the same key path.
- [ ] 8.3 `make lint` and `go test ./...` both green.
