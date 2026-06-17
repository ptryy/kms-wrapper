## 1. Post-processing pass
- [ ] 1.1 In `cmd/swagger-postprocess/main.go`, add a function `renameSchemaPrefix(spec, oldPrefix, newPrefix)` that walks `spec["components"]["schemas"]` and replaces top-level keys.
- [ ] 1.2 Extend the same function to walk every `$ref` value (including inside `oneOf`, `allOf`, `anyOf`, `discriminator.mapping`, request bodies, response schemas, and nested property schemas) and rewrite `#/components/schemas/<oldPrefix>` → `#/components/schemas/<newPrefix>`.
- [ ] 1.3 Call `renameSchemaPrefix(spec, "github_com_ryan-truong_kms-wrapper_pkg_types", "kms-wrapper_pkg_types")` from `normalizeSpec`, before `injectEVMDiscriminator`.
- [ ] 1.4 Update `injectEVMDiscriminator` mapping values from `#/components/schemas/types.EVMSignRawTxRequest` etc. to `#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest` etc., matching the renamed schema keys (mapping values are `$ref` strings and MUST keep the `#/components/schemas/` prefix).

## 2. Tests
- [ ] 2.1 Extend `cmd/swagger-postprocess/main_test.go` with a fixture that exercises a schema key under the old prefix and a `$ref` to it; assert both are rewritten to the new prefix after `normalizeSpec`.
- [ ] 2.2 Add a unit test that confirms `injectEVMDiscriminator` produces mapping values pointing at the renamed schemas.
- [ ] 2.3 `go test ./cmd/swagger-postprocess/...` passes.

## 3. Regenerate docs
- [ ] 3.1 Run `make swagger`.
- [ ] 3.2 Confirm `docs/swagger.json` contains zero occurrences of `github_com_ryan-truong_kms-wrapper_pkg_types` (`! grep -q github_com_ryan-truong_kms-wrapper_pkg_types docs/swagger.json`).
- [ ] 3.3 Confirm `docs/swagger.json` discriminator mapping entries resolve (every `mapping` value matches an existing key under `components.schemas`).
- [ ] 3.4 Run `make swagger-check` to confirm regeneration is clean.

## 4. Spec delta
- [ ] 4.1 Confirm `openspec/changes/refactor-swagger-schema-names/specs/api-docs/spec.md` MODIFIES the `Spec describes the EVM payload union with oneOf` requirement so the `Discriminator drives codegen` scenario references the new prefix, and ADDS a `Schema component names use a stable short prefix` requirement.
- [ ] 4.2 Run `openspec validate refactor-swagger-schema-names --strict --no-interactive`.

## 5. Verification
- [ ] 5.1 `go test ./...` green (mainly to confirm `pkg/types` and gateway handlers were not touched accidentally).
- [ ] 5.2 Open `docs/swagger.json` in Swagger Editor or `openapi-generator validate` to confirm the spec is still valid OpenAPI 3.0.3.
