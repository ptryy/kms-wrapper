## MODIFIED Requirements

### Requirement: Spec describes the EVM payload union with `oneOf`
The OpenAPI 3.0 document SHALL describe the EVM sign request as a `oneOf` between three payload variants (raw transaction, personal message, and EIP-712 digest). The `oneOf` SHALL include an explicit `discriminator` block keyed on a required string property `type` with `mapping` entries for `raw_tx`, `personal_message`, and `eip712_digest`.

#### Scenario: Raw-tx variant present
- **WHEN** a client inspects the spec at `components.schemas.EVMSignRequest.oneOf`
- **THEN** one variant has a `properties.raw_tx` field of type `string`

#### Scenario: EIP-712 digest length constraint
- **WHEN** the EIP-712 digest variant schema is inspected
- **THEN** `properties.eip712_digest` has `pattern` enforcing a 32-byte hex string (`^0x[0-9a-fA-F]{64}$` or equivalent)

#### Scenario: Personal-message variant present
- **WHEN** the personal-message variant schema is inspected
- **THEN** `properties.personal_message` is a `string` with `format: hex`

#### Scenario: Discriminator drives codegen
- **WHEN** a client inspects `components.schemas.EVMSignRequest`
- **THEN** the schema has `discriminator: { propertyName: "type", mapping: { raw_tx: "#/components/schemas/EVMSignRawTxRequest", personal_message: "#/components/schemas/EVMSignPersonalRequest", eip712_digest: "#/components/schemas/EVMSignEIP712Request" } }`

#### Scenario: Type field is required on every variant
- **WHEN** any variant schema is inspected
- **THEN** `required` contains the string `"type"` AND the variant's payload field

---

### Requirement: Spec describes the EVM sign response by variant
The OpenAPI 3.0 document SHALL describe the EVM sign response as a `oneOf` between a raw-tx response (`{signed_tx, signature_parts}`) and a digest/message response (`{signature}`). The spec SHALL NOT include a top-level `signature` field typed as free-form `object` or empty schema `{}`.

#### Scenario: Raw-tx response includes signature_parts
- **WHEN** a client inspects `components.schemas.EVMSignRawTxResponse.properties`
- **THEN** there is a `signed_tx: {type: string}` field and a `signature_parts: {type: object, properties: {r: {type: string}, s: {type: string}, v: {type: integer}}}` field — and NO `signature` field

#### Scenario: Personal/EIP-712 response uses typed string
- **WHEN** a client inspects `components.schemas.EVMSignPersonalResponse.properties.signature`
- **THEN** the schema is `{type: string, pattern: "^0x[0-9a-fA-F]{130}$"}` (65-byte hex) — typed, not `{}`

---

## ADDED Requirements

### Requirement: Spec advertises `/v1/`-prefixed paths
The OpenAPI 3.0 document `paths` object SHALL key every public route under `/v1/` (e.g. `/v1/sign/evm`, `/v1/sign/cosmos`, `/v1/keys`, `/v1/keys/info`, `/v1/health`). The bare (un-prefixed) routes SHALL NOT appear as separate entries in `paths`; the dual-mount at runtime is for backwards compatibility only.

#### Scenario: Versioned paths advertised
- **WHEN** a client retrieves `GET /swagger/doc.json`
- **THEN** every operation key in `paths` starts with `/v1/`; no bare `/sign/`, `/keys`, or `/health` entries exist in the `paths` object

#### Scenario: Spec is self-consistent for codegen
- **WHEN** a tool such as `openapi-generator` consumes the spec
- **THEN** the generated client targets `/v1/...` paths and the `EVMSignRequest` discriminator drives a typed sealed-class-style hierarchy
