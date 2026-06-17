## ADDED Requirements

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
