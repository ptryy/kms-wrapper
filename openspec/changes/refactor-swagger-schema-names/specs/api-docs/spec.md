## ADDED Requirements
### Requirement: Schema component names use a stable short prefix
The OpenAPI 3.0 document SHALL key every schema under `components.schemas` with the prefix `kms-wrapper_pkg_types.` followed by the Go type name (e.g. `kms-wrapper_pkg_types.KeyInfo`, `kms-wrapper_pkg_types.EVMSignRawTxRequest`). The generated artifacts SHALL NOT contain any schema key prefixed with `github_com_ryan-truong_kms-wrapper_pkg_types.`. The `cmd/swagger-postprocess` tool SHALL enforce this by rewriting both schema keys and every `$ref` value in a deterministic pass.

#### Scenario: Generated spec uses the short prefix
- **WHEN** a developer inspects `docs/swagger.json` after running `make swagger`
- **THEN** every key under `components.schemas` begins with `kms-wrapper_pkg_types.` and no key begins with `github_com_ryan-truong_kms-wrapper_pkg_types.`

#### Scenario: Refs are rewritten consistently with keys
- **WHEN** a developer inspects any `$ref` value in `docs/swagger.json` (including those nested inside `oneOf`, `allOf`, request bodies, response schemas, and `discriminator.mapping`)
- **THEN** every `$ref` pointing at a `pkg/types` schema resolves to a key that exists under `components.schemas` (no dangling references)

#### Scenario: Repo-path prefix never reappears
- **WHEN** `make swagger-check` runs against a PR
- **THEN** the check fails if `docs/swagger.json` or `docs/docs.go` contains the substring `github_com_ryan-truong_kms-wrapper_pkg_types`

## MODIFIED Requirements
### Requirement: Spec describes the EVM payload union with `oneOf`
The OpenAPI 3.0 document SHALL describe the EVM sign request as a `oneOf` between three payload variants (raw transaction, personal message, and EIP-712 digest), modeled inline at `paths./sign/evm.post.requestBody.content."application/json".schema`. The `oneOf` SHALL include an explicit `discriminator` block keyed on a required string property `type` with `mapping` entries for `raw_tx`, `personal_message`, and `eip712_digest`. The `mapping` values SHALL reference schemas under the short prefix `#/components/schemas/kms-wrapper_pkg_types.` and SHALL resolve to existing schema keys.

#### Scenario: Raw-tx variant present
- **WHEN** a client inspects the spec at `paths./sign/evm.post.requestBody.content."application/json".schema.oneOf`
- **THEN** one variant `$ref`s `#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest`, whose `properties.raw_tx` field is of type `string`

#### Scenario: EIP-712 digest length constraint
- **WHEN** the EIP-712 digest variant schema is inspected
- **THEN** `properties.eip712_digest` has `pattern` enforcing a 32-byte hex string (`^0x[0-9a-fA-F]{64}$` or equivalent)

#### Scenario: Personal-message variant present
- **WHEN** the personal-message variant schema is inspected
- **THEN** `properties.personal_message` is a `string` with `format: hex`

#### Scenario: Discriminator drives codegen
- **WHEN** a client inspects the inline EVM sign request schema at `paths./sign/evm.post.requestBody.content."application/json".schema`
- **THEN** the schema has `discriminator: { propertyName: "type", mapping: { raw_tx: "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest", personal_message: "#/components/schemas/kms-wrapper_pkg_types.EVMSignPersonalMessageRequest", eip712_digest: "#/components/schemas/kms-wrapper_pkg_types.EVMSignEIP712Request" } }` and every mapping value resolves to an existing schema key

#### Scenario: Type field is required on every variant
- **WHEN** any variant schema is inspected
- **THEN** `required` contains the string `"type"` AND the variant's payload field
