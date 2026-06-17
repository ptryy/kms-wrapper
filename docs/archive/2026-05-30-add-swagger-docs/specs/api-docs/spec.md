## ADDED Requirements

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
The OpenAPI 3.0 spec for `POST /sign/evm` SHALL describe its request body as `oneOf` over three discriminated variants: a raw-transaction variant (requires `key_path`, `chain_id`, `raw_tx`), a personal-message variant (requires `key_path`, `personal_message`), and an EIP-712 variant (requires `key_path`, `eip712_digest`).

#### Scenario: Raw-tx variant present
- **WHEN** consumers inspect the spec at `/swagger/doc.json`
- **THEN** the `POST /sign/evm` request body lists a schema requiring `key_path`, `chain_id` (exclusive minimum 0), and `raw_tx` (hex string)

#### Scenario: EIP-712 digest length constraint
- **WHEN** consumers inspect the EIP-712 variant schema
- **THEN** `eip712_digest` carries a `pattern` constraint matching exactly 32 hex bytes (with optional `0x` prefix)

#### Scenario: Personal-message variant present
- **WHEN** consumers inspect the spec
- **THEN** a variant requires only `key_path` and `personal_message` (hex string) and explicitly does not require `chain_id`

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
