# Change: Shorten OpenAPI schema component names

## Why
Today's generated OpenAPI spec keys every component schema under the fully-qualified Go import path: `github_com_ryan-truong_kms-wrapper_pkg_types.KeyInfo`, `github_com_ryan-truong_kms-wrapper_pkg_types.EVMSignRawTxRequest`, and so on (45 occurrences across `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`). This makes:
- the spec hard to read for humans (every schema name is dominated by the repo path);
- generated client code in downstream consumers carry leaky type names tied to our exact GitHub org+repo;
- the existing EVM discriminator mapping at `cmd/swagger-postprocess/main.go:117-119` already **broken** — it references `types.EVMSignRawTxRequest` etc., but those keys do not exist in the generated spec under that name, so codegen does not produce the typed sealed-class-style hierarchy that `api-docs` spec requires.

Renaming the prefix decouples the public schema names from our repo path and unbreaks the discriminator in the same pass.

## What Changes
- Schema component names in `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml` change prefix from `github_com_ryan-truong_kms-wrapper_pkg_types.` to `kms-wrapper_pkg_types.`. Examples:
  - `github_com_ryan-truong_kms-wrapper_pkg_types.KeyInfo` → `kms-wrapper_pkg_types.KeyInfo`
  - `github_com_ryan-truong_kms-wrapper_pkg_types.EVMSignRawTxRequest` → `kms-wrapper_pkg_types.EVMSignRawTxRequest`
- The rename is implemented in `cmd/swagger-postprocess/main.go` as a deterministic post-processing pass that rewrites both schema keys under `components.schemas.*` and every `$ref` to `#/components/schemas/github_com_ryan-truong_kms-wrapper_pkg_types.X`.
- The EVM discriminator mapping in `injectEVMDiscriminator` is updated to reference the new prefix (`kms-wrapper_pkg_types.EVMSignRawTxRequest`, etc.), fixing the existing breakage.
- The `api-docs` spec is updated:
  - One existing scenario (`Discriminator drives codegen`) MODIFIED to reference the new prefixed names.
  - One new requirement ADDED that pins the prefix in the OpenAPI spec so future swag output drift is caught by code review.
- No runtime behavior change. No `pkg/types/types.go` Go-side rename. No new dependency. Downstream consumers regenerate their clients against the new names.

## Impact
- Affected specs: `api-docs` (scenario MODIFIED + new requirement ADDED).
- Affected code:
  - `cmd/swagger-postprocess/main.go` — add schema-rename pass; update discriminator mapping prefix.
  - `cmd/swagger-postprocess/main_test.go` — add coverage for the rename and the corrected discriminator.
  - `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` — regenerated via `make swagger`.
- No impact on `pkg/types/types.go`, no impact on handler signatures, no impact on Vault, no impact on the CLI.
- Downstream client consumers of `docs/swagger.json` regenerate their typed clients; the regenerated symbols carry a shorter prefix.
