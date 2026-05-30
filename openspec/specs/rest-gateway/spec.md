## Purpose
Define the authenticated HTTP gateway endpoints, request contracts, and error behavior.

## Requirements

### Requirement: Bearer token authentication middleware
The REST gateway SHALL require all non-health requests to include `Authorization: Bearer <token>` matching the value of `KMS_GATEWAY_TOKEN`. Requests without a valid token SHALL be rejected with HTTP 401.

#### Scenario: Valid token
- **WHEN** a request includes `Authorization: Bearer <correct-token>`
- **THEN** the request is forwarded to the handler

#### Scenario: Missing token
- **WHEN** a request includes no `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

#### Scenario: Wrong token
- **WHEN** a request includes an incorrect bearer token
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

---

### Requirement: Health endpoint
The gateway SHALL expose `GET /health` without authentication. The response SHALL include Vault connectivity status.

#### Scenario: Healthy
- **WHEN** Vault is reachable and the token is valid
- **THEN** `GET /health` returns HTTP 200 with `{"status": "ok", "vault": "reachable"}`

#### Scenario: Vault unreachable
- **WHEN** Vault cannot be reached
- **THEN** `GET /health` returns HTTP 503 with `{"status": "degraded", "vault": "unreachable"}`

---

### Requirement: Sign EVM transaction endpoint
The gateway SHALL expose `POST /sign/evm` accepting a JSON body with `key_path`, `chain_id`, and one of `raw_tx` (hex RLP) or `personal_message` (hex bytes) or `eip712_digest` (hex 32 bytes).

#### Scenario: Sign raw EVM transaction
- **WHEN** `POST /sign/evm` is called with `{"key_path": "...", "chain_id": 1, "raw_tx": "0x..."}`
- **THEN** the gateway returns HTTP 200 with `{"signed_tx": "0x...", "signature": {"r": "...", "s": "...", "v": N}}`

#### Scenario: Sign personal message
- **WHEN** `POST /sign/evm` is called with `{"key_path": "...", "personal_message": "0x..."}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "0x<65-byte-hex>"}`

#### Scenario: Missing required fields
- **WHEN** `POST /sign/evm` is called without `key_path` or without any payload field
- **THEN** the gateway returns HTTP 400 with `{"error": "<field> is required"}`

#### Scenario: Vault signing error
- **WHEN** Vault returns an error (e.g. key not found, policy denied)
- **THEN** the gateway returns HTTP 500 with `{"error": "<vault-error-message>"}` — never exposing the Vault token or key material

---

### Requirement: Sign Cosmos transaction endpoint
The gateway SHALL expose `POST /sign/cosmos` accepting a JSON body with `key_path`, `hrp`, `sign_mode`, and `sign_doc` (base64-encoded protobuf or amino JSON string).

#### Scenario: Sign DIRECT mode
- **WHEN** `POST /sign/cosmos` is called with `{"key_path": "...", "hrp": "mantra", "sign_mode": "DIRECT", "sign_doc": "<base64>"}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "<base64>", "pub_key": "<base64-compressed-pubkey>"}`

#### Scenario: Sign AMINO mode
- **WHEN** `POST /sign/cosmos` is called with `{"sign_mode": "AMINO_JSON", "sign_doc": "<amino-json-string>"}`
- **THEN** the gateway returns HTTP 200 with the amino-compatible signature

#### Scenario: Unknown sign mode
- **WHEN** `sign_mode` is not one of `DIRECT` or `AMINO_JSON`
- **THEN** the gateway returns HTTP 400 with `{"error": "unsupported sign_mode"}`

---

### Requirement: Structured error responses
All error responses from the gateway SHALL be JSON objects with at minimum an `"error"` string field. The gateway SHALL never include stack traces, Vault tokens, or key material in responses.

#### Scenario: Error response format
- **WHEN** any handler returns an error
- **THEN** the response body is `{"error": "<human-readable message>"}` with an appropriate HTTP status code (400, 401, 404, 500)

---

### Requirement: Listen on configurable address
The gateway SHALL bind to a configurable host:port via `KMS_GATEWAY_ADDR` env var or `gateway.addr` config field. Default SHALL be `127.0.0.1:8080`.

#### Scenario: Default bind address
- **WHEN** no address is configured
- **THEN** the gateway listens on `127.0.0.1:8080`

#### Scenario: Custom bind address
- **WHEN** `KMS_GATEWAY_ADDR=0.0.0.0:9090` is set
- **THEN** the gateway listens on `0.0.0.0:9090`

---

### Requirement: Swagger UI and OpenAPI spec endpoints
When `gateway.swagger_enabled` is true (the default), the REST gateway SHALL expose two routes:

- `GET /swagger/index.html` — interactive Swagger UI served by `swaggo/http-swagger`.
- `GET /swagger/doc.json` — the raw OpenAPI 3.0 specification document.

The UI route SHALL also serve any sibling static assets (`/swagger/swagger-ui*.js`, `/swagger/swagger-ui.css`, etc.) required by the bundled Swagger UI distribution.

#### Scenario: UI is reachable in default config
- **WHEN** the gateway starts with default config and a client requests `GET /swagger/index.html`
- **THEN** the gateway responds with HTTP 200 and an HTML document containing the Swagger UI

#### Scenario: Spec endpoint serves OpenAPI 3.0
- **WHEN** a client requests `GET /swagger/doc.json`
- **THEN** the gateway responds with HTTP 200, `Content-Type: application/json`, and a body whose top-level `openapi` field starts with `3.0`

#### Scenario: Swagger disabled
- **WHEN** `gateway.swagger_enabled=false` is set and a client requests `GET /swagger/index.html`
- **THEN** the gateway responds with HTTP 404

---

### Requirement: Swagger surface respects optional bearer auth
When `gateway.swagger_auth` is true, all `/swagger/*` routes SHALL be wrapped by the same bearer-token middleware that protects `/sign/evm` and `/sign/cosmos`. When `gateway.swagger_auth` is false (the default), the `/swagger/*` routes SHALL be publicly reachable.

#### Scenario: Default public access
- **WHEN** `gateway.swagger_auth=false` and a client requests `GET /swagger/index.html` without an `Authorization` header
- **THEN** the gateway responds with HTTP 200

#### Scenario: Auth gate enabled and token missing
- **WHEN** `gateway.swagger_auth=true` and a client requests `GET /swagger/doc.json` without an `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

#### Scenario: Auth gate enabled with valid token
- **WHEN** `gateway.swagger_auth=true` and a client requests `GET /swagger/doc.json` with `Authorization: Bearer <correct-token>`
- **THEN** the gateway responds with HTTP 200 and the spec document

---

### Requirement: Swagger routes are exempt from rate limiting
The `/swagger/*` routes SHALL NOT count against the per-process rate limiter that protects the signing endpoints. Doc-surface traffic SHALL never starve signing requests, and signing-bound bursts SHALL never make the docs unreachable.

#### Scenario: Docs available under signing load
- **WHEN** the signing rate limiter is exhausted (subsequent `/sign/*` calls would receive HTTP 429)
- **THEN** `GET /swagger/index.html` and `GET /swagger/doc.json` still respond with HTTP 200

---

### Requirement: Swagger routes do not appear in spec discovery when disabled
The `/swagger/*` operations SHALL NOT be advertised in the OpenAPI spec document. The spec describes the signing/health API surface only.

#### Scenario: Spec omits swagger routes
- **WHEN** a client retrieves `GET /swagger/doc.json`
- **THEN** the returned spec's `paths` object contains entries for `/health`, `/sign/evm`, and `/sign/cosmos`, and no entry for `/swagger/index.html` or `/swagger/doc.json`

---

### Requirement: Swagger OpenAPI server URL reflects active gateway origin
When a client retrieves `GET /swagger/doc.json`, the gateway SHALL serve an OpenAPI document whose server URL targets the same origin the client is using for the gateway, rather than a fixed default localhost port.

#### Scenario: Custom gateway port is reflected
- **WHEN** the gateway is running on `127.0.0.1:3010` and a client opens Swagger UI from that origin
- **THEN** operations executed from Swagger UI target `http://127.0.0.1:3010` (not `http://localhost:8080`)

#### Scenario: Default port remains valid
- **WHEN** the gateway is running on its default local address
- **THEN** Swagger UI operations still target the running gateway origin and continue to work without manual edits

---

### Requirement: Create key endpoint
The REST gateway SHALL expose `POST /keys` accepting a JSON body `{"path": "<key-path>"}` where `<key-path>` matches the format defined by the `key-path-policy` capability (`{project}/{chain}/{username}`). The handler SHALL create a secp256k1 Transit key at that path via the existing `vault.Client.CreateKey` primitive, then derive and return the public key (hex), Ethereum address (EIP-55), and Cosmos bech32 address (default HRP `cosmos`). The operation SHALL be idempotent: a second call with the same path SHALL return the existing key material and SHALL set `already_existed: true` in the response body.

#### Scenario: Create new key
- **WHEN** an authorized client POSTs `{"path": "proj-a/evm/alice"}` to `/keys` and that path does not yet exist
- **THEN** the gateway responds with HTTP 200, `Content-Type: application/json`, and body `{"path": "proj-a/evm/alice", "public_key_hex": "<hex>", "evm_address": "0x<EIP-55>", "cosmos_address": "cosmos1<bech32>", "already_existed": false}`

#### Scenario: Re-create existing key is idempotent
- **WHEN** an authorized client POSTs `{"path": "proj-a/evm/alice"}` to `/keys` and that path already exists
- **THEN** the gateway responds with HTTP 200 and the same `path`, `public_key_hex`, `evm_address`, and `cosmos_address` as the original create, with `already_existed: true`

#### Scenario: Missing path field
- **WHEN** an authorized client POSTs `{}` (or any body without a non-empty `path`) to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "path is required"}`

#### Scenario: Invalid key path format
- **WHEN** an authorized client POSTs `{"path": "Proj A/EVM/Alice"}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body whose `error` field contains the validation message from `ValidateKeyPath` (e.g. `"key path segments must match [a-z0-9_-]"`)

#### Scenario: Malformed JSON body
- **WHEN** an authorized client POSTs a body that is not valid JSON to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "invalid JSON"}`

#### Scenario: Vault permission denied
- **WHEN** Vault rejects the create call with a permission-denied error
- **THEN** the gateway responds with HTTP 403 and body `{"error": "permission denied"}` — the raw Vault error message and the Vault token SHALL NOT appear in the response body

#### Scenario: Vault unreachable or other Vault failure
- **WHEN** any other Vault error occurs during create
- **THEN** the gateway responds with HTTP 500 and body `{"error": "vault error"}` — the full error SHALL be logged server-side via `slog.ErrorContext` including the key path

---

### Requirement: Show key endpoint
The REST gateway SHALL expose `GET /keys/info?path=<key-path>` returning the public key (hex), Ethereum address (EIP-55), and Cosmos bech32 address for the given path. When the key does not exist, the gateway SHALL respond with HTTP 404. The `path` query parameter is required and SHALL be validated against the `key-path-policy` format before any Vault call.

#### Scenario: Show existing key
- **WHEN** an authorized client GETs `/keys/info?path=proj-a/evm/alice` and that key exists in Vault
- **THEN** the gateway responds with HTTP 200, `Content-Type: application/json`, and body `{"path": "proj-a/evm/alice", "public_key_hex": "<hex>", "evm_address": "0x<EIP-55>", "cosmos_address": "cosmos1<bech32>"}`

#### Scenario: Key not found
- **WHEN** an authorized client GETs `/keys/info?path=proj-a/evm/ghost` and no such key exists in Vault
- **THEN** the gateway responds with HTTP 404 and body `{"error": "key not found: proj-a/evm/ghost"}`

#### Scenario: Missing path query parameter
- **WHEN** an authorized client GETs `/keys/info` (no `path` query)
- **THEN** the gateway responds with HTTP 400 and body `{"error": "path is required"}`

#### Scenario: Invalid key path format
- **WHEN** an authorized client GETs `/keys/info?path=BadPath`
- **THEN** the gateway responds with HTTP 400 and body whose `error` field contains the validation message from `ValidateKeyPath`

#### Scenario: Vault permission denied on show
- **WHEN** Vault rejects the read with a permission-denied error
- **THEN** the gateway responds with HTTP 403 and body `{"error": "permission denied"}`

---

### Requirement: List keys endpoint
The REST gateway SHALL expose `GET /keys?prefix=<prefix>` returning a JSON object with a `keys` array (the bare names returned by the underlying Vault Transit plugin's LIST operation under that prefix) and an integer `count`. The `prefix` query parameter is optional; when empty or omitted, the gateway SHALL list keys at the top of the Transit mount. The response SHALL NOT enrich each entry with derived addresses — callers SHALL call `GET /keys/info` to fetch full info for a specific name.

#### Scenario: List with prefix returns names
- **WHEN** an authorized client GETs `/keys?prefix=proj-a/` and `vault.Client.ListKeys` returns `["evm/alice", "cosmos/bob"]`
- **THEN** the gateway responds with HTTP 200, `Content-Type: application/json`, and body `{"keys": ["evm/alice", "cosmos/bob"], "count": 2}`

#### Scenario: List with no prefix
- **WHEN** an authorized client GETs `/keys` (no query)
- **THEN** the gateway calls `vault.Client.ListKeys(ctx, "")` and returns its result in the same shape

#### Scenario: List with no matches
- **WHEN** an authorized client GETs `/keys?prefix=empty-tenant/` and the underlying LIST returns no keys
- **THEN** the gateway responds with HTTP 200 and body `{"keys": [], "count": 0}`

#### Scenario: Vault permission denied on list
- **WHEN** Vault rejects the LIST with a permission-denied error
- **THEN** the gateway responds with HTTP 403 and body `{"error": "permission denied"}`

#### Scenario: Vault unreachable on list
- **WHEN** any other Vault error occurs during list
- **THEN** the gateway responds with HTTP 500 and body `{"error": "vault error"}` — the full error SHALL be logged server-side

---

### Requirement: Key endpoints require bearer authentication
The three `/keys` routes (`POST /keys`, `GET /keys`, `GET /keys/info`) SHALL be wrapped by the same bearer-token middleware that protects `/sign/evm` and `/sign/cosmos`. Requests without `Authorization: Bearer <KMS_GATEWAY_TOKEN>` SHALL be rejected with HTTP 401 before reaching the handler.

#### Scenario: Missing token on create
- **WHEN** a client POSTs to `/keys` with no `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

#### Scenario: Wrong token on show
- **WHEN** a client GETs `/keys/info?path=proj-a/evm/alice` with an incorrect bearer token
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

#### Scenario: Missing token on list
- **WHEN** a client GETs `/keys?prefix=proj-a/` with no `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

---

### Requirement: Key endpoints share the signing rate limiter
The three `/keys` routes SHALL be subject to the same per-process rate limiter as `/sign/evm` and `/sign/cosmos` (configured via `gateway.rate_limit` and `gateway.rate_burst`). When the limiter is exhausted, key requests SHALL receive HTTP 429 with body `{"error": "rate limit exceeded"}`.

#### Scenario: Key request rate-limited
- **WHEN** the gateway's rate limiter has been exhausted by prior `/sign/*` or `/keys` requests
- **THEN** the next `GET /keys/info` or `POST /keys` or `GET /keys` request receives HTTP 429 with body `{"error": "rate limit exceeded"}` and the handler SHALL NOT execute

#### Scenario: Signing request also affected by key bursts
- **WHEN** a flood of `/keys` requests exhausts the limiter
- **THEN** subsequent `/sign/*` requests within the same window are rate-limited (HTTP 429), confirming the shared budget

---

### Requirement: Key endpoints appear in the OpenAPI 3.0 spec
The generated OpenAPI 3.0 document served at `GET /swagger/doc.json` (when `gateway.swagger_enabled=true`) SHALL include operations for `POST /keys`, `GET /keys`, and `GET /keys/info`, each tagged `keys` and requiring `BearerAuth`. The request and response schemas SHALL accurately describe the JSON shapes defined by the create/show/list requirements above.

#### Scenario: Spec advertises the new operations
- **WHEN** a client retrieves `GET /swagger/doc.json`
- **THEN** the spec's `paths` object contains entries for `/keys` (with `get` and `post`) and `/keys/info` (with `get`), each declaring `security: [{"BearerAuth": []}]` and a `200` response referencing `KeyInfo` / `KeyCreateResponse` / `KeyListResponse` schemas

#### Scenario: Create response schema is documented
- **WHEN** a client inspects the spec at `paths./keys.post.responses.200.content.application/json.schema`
- **THEN** the schema describes `path`, `public_key_hex`, `evm_address`, `cosmos_address`, and `already_existed` fields with their JSON types
