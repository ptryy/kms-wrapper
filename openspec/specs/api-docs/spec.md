## Purpose
Define the OpenAPI 3.0 specification generation, CI enforcement, and documentation accuracy requirements for the gateway API surface.
## Requirements
### Requirement: OpenAPI 3.0 specification is generated from handler annotations
The project SHALL produce an OpenAPI 3.0 specification describing all public gateway endpoints (`/health`, `/sign/evm`, `/sign/cosmos`, `/swagger/*`). The spec SHALL be generated from `swaggo/swag` annotations placed adjacent to the handler functions in `internal/gateway/gateway.go` and the request/response types in `pkg/types/types.go`. The generated artifacts SHALL be checked into the repository under `docs/`.

#### Scenario: Generated docs exist in the repo
- **WHEN** the repository is cloned at any commit on `main`
- **THEN** `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml` exist and are valid OpenAPI 3.0 documents

#### Scenario: Regeneration command
- **WHEN** a developer runs `make swagger`
- **THEN** `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml` are regenerated from the current handler annotations

#### Scenario: Build does not require swag
- **WHEN** a developer runs `go build ./...` on a clean checkout without the `swag` CLI installed
- **THEN** the build succeeds because `docs/docs.go` is committed and self-contained

---

### Requirement: CI enforces docs are in sync with annotations
The CI pipeline SHALL run a `swagger-check` step that regenerates the docs and fails the build if the generated artifacts differ from the checked-in versions.

#### Scenario: Annotations and committed docs match
- **WHEN** `make swagger-check` runs in CI against a PR that has regenerated `docs/` after editing annotations
- **THEN** the step exits 0

#### Scenario: Annotations changed but docs not regenerated
- **WHEN** `make swagger-check` runs against a PR that edited handler annotations without running `make swagger`
- **THEN** the step exits non-zero with an error message instructing the developer to run `make swagger`

---

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

### Requirement: Spec describes Cosmos sign-mode enum
The OpenAPI 3.0 spec for `POST /sign/cosmos` SHALL declare `sign_mode` as a string enum constrained to `DIRECT` and `AMINO_JSON`, and SHALL document that `sign_doc` is base64-encoded protobuf when `sign_mode=DIRECT` and a raw JSON string when `sign_mode=AMINO_JSON`.

#### Scenario: sign_mode enum
- **WHEN** consumers inspect the `CosmosSignRequest` schema
- **THEN** `sign_mode` is declared as `type: string, enum: [DIRECT, AMINO_JSON]`

#### Scenario: sign_doc encoding documented
- **WHEN** consumers read the `sign_doc` field description in the spec
- **THEN** the description explains the two encodings keyed by `sign_mode`

---

### Requirement: Spec documents bearer-token security scheme
The OpenAPI 3.0 spec SHALL declare an HTTP bearer security scheme named `BearerAuth` and apply it as the default security requirement on `/sign/evm` and `/sign/cosmos`. The `/health` operation SHALL be marked as not requiring this scheme.

#### Scenario: Security scheme defined
- **WHEN** consumers inspect `components.securitySchemes` in the spec
- **THEN** a `BearerAuth` entry exists with `type: http, scheme: bearer`

#### Scenario: Signing endpoints require auth
- **WHEN** consumers inspect the `/sign/evm` and `/sign/cosmos` operations
- **THEN** each declares `security: [{BearerAuth: []}]`

#### Scenario: Health endpoint is public
- **WHEN** consumers inspect the `/health` operation
- **THEN** it declares `security: []` (no requirement)

---

### Requirement: Spec documents error response shape
The OpenAPI 3.0 spec SHALL describe a shared `ErrorResponse` schema (object with a single `error` string field) and reference it from the 400, 401, 429, and 500 responses of every operation that can produce those statuses.

#### Scenario: ErrorResponse schema present
- **WHEN** consumers inspect `components.schemas.ErrorResponse`
- **THEN** the schema is an object with a required `error: string` property

#### Scenario: 401 references ErrorResponse
- **WHEN** consumers inspect the 401 response of `POST /sign/evm`
- **THEN** the response body schema references `#/components/schemas/ErrorResponse`

### Requirement: Spec describes the EVM sign response by variant
The OpenAPI 3.0 document SHALL describe the EVM sign response as a `oneOf` between a raw-tx response (`{signed_tx, signature_parts}`) and a digest/message response (`{signature}`). The spec SHALL NOT include a top-level `signature` field typed as free-form `object` or empty schema `{}`.

#### Scenario: Raw-tx response includes signature_parts
- **WHEN** a client inspects `components.schemas.EVMSignRawTxResponse.properties`
- **THEN** there is a `signed_tx: {type: string}` field and a `signature_parts: {type: object, properties: {r: {type: string}, s: {type: string}, v: {type: integer}}}` field — and NO `signature` field

#### Scenario: Personal/EIP-712 response uses typed string
- **WHEN** a client inspects `components.schemas.EVMSignPersonalResponse.properties.signature`
- **THEN** the schema is `{type: string, pattern: "^0x[0-9a-fA-F]{130}$"}` (65-byte hex) — typed, not `{}`

---

### Requirement: Spec advertises `/v1/`-prefixed paths
The OpenAPI 3.0 document `paths` object SHALL key every public route under `/v1/` (e.g. `/v1/sign/evm`, `/v1/sign/cosmos`, `/v1/keys`, `/v1/keys/info`, `/v1/health`). The bare (un-prefixed) routes SHALL NOT appear as separate entries in `paths`; the dual-mount at runtime is for backwards compatibility only.

#### Scenario: Versioned paths advertised
- **WHEN** a client retrieves `GET /swagger/doc.json`
- **THEN** every operation key in `paths` starts with `/v1/`; no bare `/sign/`, `/keys`, or `/health` entries exist in the `paths` object

#### Scenario: Spec is self-consistent for codegen
- **WHEN** a tool such as `openapi-generator` consumes the spec
- **THEN** the generated client targets `/v1/...` paths and the `EVMSignRequest` discriminator drives a typed sealed-class-style hierarchy

