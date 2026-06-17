## Purpose
Define the authenticated HTTP gateway endpoints, request contracts, and error behavior.
## Requirements
### Requirement: Bearer token authentication middleware
The REST gateway SHALL require all non-health, non-livez, non-readyz, non-metrics requests to include `Authorization: Bearer <token>` matching the value of `KMS_GATEWAY_TOKEN`. The unauthenticated set is: `/health`, `/livez`, `/readyz`, and `/metrics` (the probe and observability endpoints introduced by `add-observability-and-ops`). The comparison SHALL be performed in constant time over fixed-length HMAC-SHA256 digests of the supplied and configured tokens (using a server-side nonce generated at startup as the HMAC key), so the comparison does not short-circuit on unequal input lengths. Requests without a valid token SHALL be rejected with HTTP 401 and a single log line indicating the failure reason (`missing`, `bad-format`, or `mismatch`). The supplied token SHALL NOT appear in any log entry.

#### Scenario: Valid token
- **WHEN** a request includes `Authorization: Bearer <correct-token>`
- **THEN** the request is forwarded to the handler and no auth log line is emitted

#### Scenario: Missing token
- **WHEN** a request includes no `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}` and logs `unauthorized request reason=missing` at `warn`

#### Scenario: Wrong token
- **WHEN** a request includes an incorrect bearer token
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}` and logs `unauthorized request reason=mismatch` at `warn`; the supplied token is NOT in the log

#### Scenario: Malformed header
- **WHEN** the `Authorization` header is present but does not start with `Bearer ` (e.g. `Basic ...`)
- **THEN** the gateway responds with HTTP 401 and logs `unauthorized request reason=bad-format` at `warn`

#### Scenario: Length-leak resistance
- **WHEN** an attacker probes with bearer tokens of varying lengths against an endpoint that returns 401
- **THEN** the response time distribution does not correlate with the supplied token length (HMAC digests are fixed-length so the comparison runs the same number of bytes regardless of input)

---

### Requirement: Health endpoint
The gateway SHALL expose `GET /health` without authentication. The response SHALL include Vault connectivity status. The endpoint SHALL be subject to a dedicated slow-path rate limiter (default 10 rps, burst 5, keyed on remote IP) and its result SHALL be cached for 1 second to absorb micro-bursts. When the slow-path limiter is exhausted, `/health` SHALL return HTTP 429 with body `{"error": "rate limit exceeded"}`.

> **Superseded by `add-observability-and-ops`:** The response shape below (`ok`/`degraded`) is replaced by the `/readyz` shape (`ready`/`not_ready`) when `add-observability-and-ops` is applied. `/health` becomes an alias for `/readyz`. The rate-limit and caching requirements in this spec still apply.

#### Scenario: Healthy
- **WHEN** Vault is reachable and the token is valid
- **THEN** `GET /health` returns HTTP 200 with `{"status": "ready"}` (once `add-observability-and-ops` is applied; `{"status": "ok", "vault": "reachable"}` before)

#### Scenario: Vault unreachable
- **WHEN** Vault cannot be reached
- **THEN** `GET /health` returns HTTP 503 with `{"status": "not_ready", "reason": "vault_unreachable"}` (once `add-observability-and-ops` is applied; `{"status": "degraded", "vault": "unreachable"}` before)

#### Scenario: Health response is cached for 1 second
- **WHEN** `GET /health` is called twice within 1 second from any client(s)
- **THEN** at most one Vault round-trip is performed; the second response reuses the first response's status and body

#### Scenario: Health rate-limited under burst
- **WHEN** a single remote IP issues 30 `GET /health` requests in 1 second
- **THEN** at most ~15 (rate × 1s + burst) succeed; the rest receive HTTP 429 with body `{"error": "rate limit exceeded"}`

---

### Requirement: Sign EVM transaction endpoint
The gateway SHALL expose `POST /sign/evm` (and equivalently `POST /v1/sign/evm`) accepting a JSON body with `key_path`, an explicit `type` discriminator field whose value is one of `raw_tx`, `personal_message`, `eip712_digest`, and the payload field matching the discriminator: `raw_tx` (hex RLP), `personal_message` (hex bytes), or `eip712_digest` (hex 32 bytes). `chain_id` SHALL be required when `type=raw_tx` (used to scope the secp256k1 signature to a specific EVM chain) and SHALL be optional/ignored for `personal_message` and `eip712_digest` (which do not bind to a chain at the signature layer). The handler SHALL dispatch on `type`; payload fields not matching the discriminator SHALL be ignored. The response SHALL be one of two typed shapes based on the request variant:
- `raw_tx` → `{"signed_tx": "0x...", "signature_parts": {"r": "...", "s": "...", "v": N}}` — the `signature_parts` field IS the structured signature; there SHALL NOT be a free-form `signature` field of type `any`.
- `personal_message` or `eip712_digest` → `{"signature": "0x<65-byte-hex>"}` — `signature` is a typed string.

When the gateway cannot decode the signed transaction bytes (e.g. `tx.UnmarshalBinary` fails on the `raw_tx` path), the handler SHALL respond with HTTP 500 and body `{"error": "decode signed tx: <message>"}` and SHALL NOT return a partially-populated response.

#### Scenario: Sign raw EVM transaction
- **WHEN** `POST /sign/evm` is called with `{"type": "raw_tx", "key_path": "...", "chain_id": 1, "raw_tx": "0x..."}`
- **THEN** the gateway returns HTTP 200 with `{"signed_tx": "0x...", "signature_parts": {"r": "...", "s": "...", "v": N}}` and NO field named `signature`

#### Scenario: Sign personal message
- **WHEN** `POST /sign/evm` is called with `{"type": "personal_message", "key_path": "...", "personal_message": "0x..."}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "0x<65-byte-hex>"}` where `signature` is typed `string`

#### Scenario: Missing discriminator
- **WHEN** `POST /sign/evm` is called without a `type` field
- **THEN** the gateway returns HTTP 400 with `{"error": "type is required and must be one of raw_tx|personal_message|eip712_digest"}`

#### Scenario: Discriminator/payload mismatch
- **WHEN** `POST /sign/evm` is called with `{"type": "raw_tx", "personal_message": "0x...", "key_path": "..."}` (raw_tx field absent)
- **THEN** the gateway returns HTTP 400 with `{"error": "raw_tx is required when type=raw_tx"}`

#### Scenario: UnmarshalBinary failure surfaces as 500
- **WHEN** the signed-tx bytes returned by the signer fail to `UnmarshalBinary`
- **THEN** the gateway returns HTTP 500 and body `{"error": "decode signed tx: <message>"}` — no zero-valued `signature_parts` is returned

#### Scenario: Vault signing error
- **WHEN** Vault returns an error (e.g. key not found, policy denied)
- **THEN** the gateway returns HTTP 500 with `{"error": "<vault-error-message>"}` — never exposing the Vault token or key material

---

### Requirement: Sign Cosmos transaction endpoint
The gateway SHALL expose `POST /sign/cosmos` (and equivalently `POST /v1/sign/cosmos`) accepting a JSON body with `key_path`, `hrp`, `sign_mode`, and `sign_doc` (base64-encoded protobuf or amino JSON string). When the gateway cannot derive the Cosmos bech32 address from the public key (`DeriveCosmosAddressFromCompressed` returns an error), the handler SHALL respond with HTTP 500 and body `{"error": "derive cosmos address: <message>"}` and SHALL NOT return a partially-populated response.

#### Scenario: Sign DIRECT mode
- **WHEN** `POST /sign/cosmos` is called with `{"key_path": "...", "hrp": "mantra", "sign_mode": "DIRECT", "sign_doc": "<base64>"}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "<base64>", "pub_key": "<base64-compressed-pubkey>", "cosmos_address": "mantra1..."}`

#### Scenario: Sign AMINO mode
- **WHEN** `POST /sign/cosmos` is called with `{"sign_mode": "AMINO_JSON", "sign_doc": "<amino-json-string>"}`
- **THEN** the gateway returns HTTP 200 with the amino-compatible signature; the signed bytes are the Cosmos-canonical form of the input (see `cosmos-signer` capability)

#### Scenario: Unknown sign mode
- **WHEN** `sign_mode` is not one of `DIRECT` or `AMINO_JSON`
- **THEN** the gateway returns HTTP 400 with `{"error": "unsupported sign_mode"}`

#### Scenario: Address derivation failure surfaces as 500
- **WHEN** `DeriveCosmosAddressFromCompressed` fails on the compressed pubkey returned by Vault
- **THEN** the gateway returns HTTP 500 and body `{"error": "derive cosmos address: <message>"}` and `cosmos_address` is NOT zero-valued in any returned response

---

### Requirement: Structured error responses
All error responses from the gateway SHALL be JSON objects with at minimum an `"error"` string field. This includes responses generated by the router itself (405 Method Not Allowed). The gateway SHALL never include stack traces, Vault tokens, or key material in responses. On HTTP 405 responses the gateway SHALL preserve the `Allow` response header set by the underlying mux (RFC 7231 §6.5.5).

#### Scenario: Error response format
- **WHEN** any handler returns an error
- **THEN** the response body is `{"error": "<human-readable message>"}` with an appropriate HTTP status code (400, 401, 404, 405, 500) and `Content-Type: application/json`

#### Scenario: Unsupported method
- **WHEN** a request uses a method not registered for that path (e.g. `DELETE /keys`)
- **THEN** the gateway responds with HTTP 405 and body `{"error": "method not allowed"}` with `Content-Type: application/json` AND an `Allow` header listing the supported methods for that path (e.g. `Allow: GET, POST`)

#### Scenario: Allow header lists exact set
- **WHEN** a client sends `DELETE /keys` (which supports `GET` and `POST`)
- **THEN** the response includes `Allow: GET, POST` (or any order — both `GET, POST` and `POST, GET` are acceptable per RFC)

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
When `gateway.swagger_auth` is true (the default), all `/swagger/*` routes SHALL be wrapped by the same bearer-token middleware that protects `/sign/evm` and `/sign/cosmos`. When `gateway.swagger_auth` is false, the `/swagger/*` routes SHALL be publicly reachable. The gateway SHALL refuse to start when `gateway.swagger_auth=false` and the configured listen address is not a loopback address (`127.0.0.0/8` or `::1`), unless the environment variable `KMS_DEV` is set to `true`.

#### Scenario: Default auth gate is on
- **WHEN** `gateway.swagger_auth` is not explicitly set and a client requests `GET /swagger/index.html` without an `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

#### Scenario: Auth gate enabled with valid token
- **WHEN** `gateway.swagger_auth=true` and a client requests `GET /swagger/doc.json` with `Authorization: Bearer <correct-token>`
- **THEN** the gateway responds with HTTP 200 and the spec document

#### Scenario: Loopback bind permits public swagger
- **WHEN** `gateway.swagger_auth=false` and `gateway.addr=127.0.0.1:8080`
- **THEN** the gateway starts normally and `GET /swagger/index.html` is publicly reachable

#### Scenario: Non-loopback bind refuses public swagger
- **WHEN** `gateway.swagger_auth=false` and `gateway.addr=0.0.0.0:8080` and `KMS_DEV` is not set
- **THEN** startup fails with `"refusing to expose unauthenticated swagger on non-loopback address; set KMS_DEV=true for local dev"`

#### Scenario: Non-loopback bind with KMS_DEV escape
- **WHEN** `gateway.swagger_auth=false` and `gateway.addr=0.0.0.0:8080` and `KMS_DEV=true`
- **THEN** startup proceeds and a `warn` log is emitted: `"running with unauthenticated swagger on non-loopback (KMS_DEV=true)"`

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

### Requirement: Swagger root path serves UI without redirect
`GET /swagger/` SHALL return HTTP 200 with the Swagger UI body directly. The gateway SHALL NOT issue a redirect (301, 302, 307, or 308) to `/swagger/index.html`. This ensures the UI is reachable in environments where reverse proxies or CSP policies strip or block `Location` headers, and avoids permanently-cached redirect entries in clients.

#### Scenario: Root swagger path returns UI directly
- **WHEN** a client sends `GET /swagger/` to the gateway
- **THEN** the gateway responds with HTTP 200 and an HTML body containing the Swagger UI, without issuing any redirect response

#### Scenario: Index path still works
- **WHEN** a client sends `GET /swagger/index.html` to the gateway
- **THEN** the gateway responds with HTTP 200 and the same Swagger UI HTML body

#### Scenario: Auth gate applies to root path too
- **WHEN** `gateway.swagger_auth=true` and a client sends `GET /swagger/` without an `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`, not a redirect to the login page

---

### Requirement: Create key endpoint
The REST gateway SHALL expose `POST /keys` (and equivalently `POST /v1/keys`) accepting a JSON body `{"path": "<key-path>"}` where `<key-path>` matches the format defined by the `key-path-policy` capability. The handler SHALL create a secp256k1 key at that path, then derive and return the public key (hex), Ethereum address (EIP-55), and Cosmos bech32 address. The operation SHALL be idempotent. On first create the response status SHALL be HTTP 201; on subsequent idempotent re-create the response status SHALL be HTTP 200. The response body in both cases SHALL include `"already_existed": <bool>` matching the status (`false` for 201, `true` for 200).

#### Scenario: First create returns 201
- **WHEN** an authorized client POSTs `{"path": "proj-a/evm/alice"}` to `/keys` and that path does not yet exist
- **THEN** the gateway responds with HTTP 201, `Content-Type: application/json`, and body `{"path": "proj-a/evm/alice", "public_key_hex": "<hex>", "evm_address": "0x<EIP-55>", "cosmos_address": "cosmos1<bech32>", "already_existed": false}`

#### Scenario: Idempotent re-create returns 200
- **WHEN** an authorized client POSTs `{"path": "proj-a/evm/alice"}` to `/keys` and that path already exists
- **THEN** the gateway responds with HTTP 200 and the same `path`, `public_key_hex`, `evm_address`, and `cosmos_address` as the original create, with `already_existed: true`

#### Scenario: Missing path field
- **WHEN** an authorized client POSTs `{}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "path is required"}`

#### Scenario: Invalid key path format
- **WHEN** an authorized client POSTs `{"path": "Proj A/EVM/Alice"}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body whose `error` field contains the validation message from `ValidateKeyPath`

#### Scenario: Malformed JSON body
- **WHEN** an authorized client POSTs a body that is not valid JSON to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "invalid JSON"}`

#### Scenario: Vault permission denied
- **WHEN** Vault rejects the create call with a permission-denied error
- **THEN** the gateway responds with HTTP 403 and body `{"error": "permission denied"}`

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
The REST gateway SHALL expose `GET /keys` (and equivalently `GET /v1/keys`) accepting `?prefix=<prefix>`, `?limit=<n>` (default 100, max 1000), and `?cursor=<opaque>` query parameters. The response SHALL be `{"keys": [...], "count": <n>, "next_cursor": "<opaque>" | ""}`. When more results exist than fit in `limit`, `next_cursor` SHALL be non-empty; clients pass it back via `?cursor=` to fetch the next page. When no more results exist, `next_cursor` SHALL be the empty string.

#### Scenario: List with limit returns up to limit entries
- **WHEN** an authorized client GETs `/keys?prefix=proj-a/&limit=2` and the underlying list has 5 entries
- **THEN** the gateway responds with HTTP 200, `"count": 2`, and `next_cursor` is non-empty

#### Scenario: Cursor pagination drives next page
- **WHEN** the client follows up with `GET /keys?prefix=proj-a/&limit=2&cursor=<previous-next-cursor>`
- **THEN** the gateway responds with the next 2 entries (or fewer if fewer remain); `next_cursor` continues to be non-empty until the last page

#### Scenario: Empty `next_cursor` on final page
- **WHEN** the page returned contains the last of the available entries
- **THEN** `next_cursor` is the empty string

#### Scenario: Invalid limit
- **WHEN** `?limit=99999` is passed
- **THEN** the gateway clamps to the maximum (1000) and returns the clamped page (per the implementation choice documented in `tasks.md` 6.1; values below 1 SHALL also be clamped to the default of 100)

#### Scenario: Tampered cursor
- **WHEN** `?cursor=not-a-valid-cursor` is passed
- **THEN** the gateway returns HTTP 400 with `{"error": "invalid cursor"}`

#### Scenario: List with no prefix
- **WHEN** an authorized client GETs `/keys` (no query)
- **THEN** the gateway calls the underlying list with empty prefix and returns the first page (`limit` default 100)

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

---

### Requirement: Key list traversal semantics
`GET /keys` proxies the Vault LIST API, which returns one level of the key hierarchy at a time. Intermediate path segments are returned as strings ending with `/` (e.g. `"proj-a/"`). Leaf key names (e.g. `"alice"`) have no trailing slash. Callers MUST iterate by appending each intermediate prefix to the `?prefix=` query parameter to traverse to leaf level.

#### Scenario: Root list
- **WHEN** `GET /keys` is called without a prefix
- **THEN** the response contains only top-level prefixes (e.g. `["proj-a/", "proj-b/"]`), not fully-qualified key paths

#### Scenario: Prefix drill-down
- **WHEN** `GET /keys?prefix=proj-a/evm/` is called
- **THEN** the response contains only the leaf key names under that prefix (e.g. `["alice", "bob"]`)

#### Scenario: Empty prefix subtree
- **WHEN** `GET /keys?prefix=nonexistent/` is called and no keys exist under that prefix
- **THEN** the gateway returns HTTP 200 with `{"keys": [], "count": 0}`

---

### Requirement: Key deletion not exposed
The gateway SHALL NOT expose a DELETE endpoint for the keys resource. Key deletion is intentionally withheld to preserve Vault's cryptographic audit log and key version history.

#### Scenario: Attempt to delete a key
- **WHEN** a request is sent with method `DELETE` to `/keys` or `/keys/info`
- **THEN** the gateway returns HTTP 405 `{"error": "method not allowed"}` — no key is deleted

### Requirement: Per-principal rate limiting for signing and key endpoints
The gateway SHALL apply rate limiting per principal, where the principal key is `HMAC-SHA256(server_nonce, bearer_token) || ip`. A principal SHALL have its own `golang.org/x/time/rate.Limiter` instance using the configured `gateway.rate_limit` and `gateway.rate_burst` values. Principal entries SHALL be evicted from the map after 5 minutes of idle time. The map SHALL be capped at 10,000 entries; when full, the least-recently-used entry is evicted. The principal key value SHALL NOT appear in logs in cleartext; only its fingerprint may be logged.

#### Scenario: One principal does not starve another
- **WHEN** principal A exhausts its rate budget on `/sign/evm`
- **THEN** principal B with a different token continues to receive `2xx` on its own `/sign/evm` calls until B's own budget is exhausted

#### Scenario: Same token from two IPs has separate budgets
- **WHEN** the same bearer token is used from two different remote IPs
- **THEN** the two callers have independent rate budgets (the principal key concatenates the IP)

#### Scenario: Idle entries are evicted
- **WHEN** a principal has not issued a request for 5 minutes
- **THEN** its limiter entry is removed from the map; the next request from that principal allocates a fresh limiter at full burst

#### Scenario: Map capacity is bounded
- **WHEN** 10,000 distinct principals have active limiters and a 10,001st arrives
- **THEN** the least-recently-used entry is evicted before the new one is inserted

---

### Requirement: Trusted-proxy gate on forwarded headers
The gateway SHALL honour `X-Forwarded-Proto` and `X-Forwarded-Host` only when the immediate peer's IP matches one of the CIDR entries in `gateway.trusted_proxies`. When the peer is untrusted, the gateway SHALL derive scheme from `r.TLS != nil` and host from `gateway.public_url` if configured, otherwise from `r.Host`. The OpenAPI `servers[].url` exposed at `GET /swagger/doc.json` SHALL be computed via this same resolver.

#### Scenario: Default config does not trust forwarded headers
- **WHEN** `gateway.trusted_proxies` is empty (default) and a client sends `Host: attacker.example` and `X-Forwarded-Proto: https`
- **THEN** the OpenAPI document served back uses `gateway.public_url` (if set) or `r.Host` as the host, and scheme `http` (because `r.TLS` is nil for a plaintext listener) — NOT `https://attacker.example`

#### Scenario: Trusted proxy is honoured
- **WHEN** `gateway.trusted_proxies=["10.0.0.0/8"]`, the peer IP is `10.0.0.5`, and headers say `X-Forwarded-Proto: https`, `X-Forwarded-Host: api.example.com`
- **THEN** the OpenAPI document uses `https://api.example.com`

#### Scenario: Public URL override
- **WHEN** `gateway.public_url=https://kms.example.com` is set and `gateway.trusted_proxies` is empty
- **THEN** OpenAPI documents target `https://kms.example.com` regardless of `Host` headers from clients

---

### Requirement: Weak-token startup guard
The gateway SHALL refuse to start unconditionally (no `KMS_DEV` bypass) when `gateway.token` is empty. The gateway SHALL also refuse to start when `gateway.token` matches a known-weak placeholder (`change-me`, `dev`, `dev-token`, `password`) unless `KMS_DEV=true` is set. The equivalent rule applies to `vault.token` (with `root` added to the weak list — covered by the `vault-backend` capability).

#### Scenario: Default placeholder refused
- **WHEN** `gateway.token=change-me` and `KMS_DEV` is not set
- **THEN** startup fails with `"refusing to start with weak gateway token; set KMS_DEV=true for local dev"`

#### Scenario: Empty token refused
- **WHEN** `gateway.token` is empty and `KMS_DEV` is not set
- **THEN** startup fails with `"gateway token is required"`

#### Scenario: KMS_DEV bypass with warn
- **WHEN** `gateway.token=dev-token` and `KMS_DEV=true`
- **THEN** startup proceeds and a `warn` log is emitted: `"running with weak gateway token (KMS_DEV=true)"`

### Requirement: Routes are dual-mounted at `/v1/` prefix
Every public gateway route SHALL be registered at its bare path AND at the same path prefixed with `/v1/`. Both forms SHALL behave identically. The OpenAPI document SHALL advertise the `/v1/` form as primary; the bare form SHALL be marked deprecated in the spec via the `deprecated: true` flag and a `Sunset` HTTP header on responses, per RFC 8594.

#### Scenario: Both forms resolve identically
- **WHEN** an authorized client calls `POST /sign/evm` and `POST /v1/sign/evm` with the same body
- **THEN** the two responses have the same status and body; headers are identical except: `Deprecation` and `Sunset` appear only on the bare-path response, and timestamp-valued headers (e.g. `Date`) may differ by wall-clock skew

#### Scenario: OpenAPI spec uses `/v1/` paths
- **WHEN** a client retrieves `GET /swagger/doc.json`
- **THEN** the `paths` object keys are `/v1/sign/evm`, `/v1/sign/cosmos`, `/v1/keys`, `/v1/keys/info`, `/v1/health`, etc. — the bare paths are NOT in the `paths` object

#### Scenario: Deprecation headers on bare paths
- **WHEN** an authorized client calls `POST /sign/evm` (bare form)
- **THEN** the response includes `Deprecation: true` and `Sunset: <RFC-1123-date>` headers; the date is at least 90 days in the future

### Requirement: Liveness and readiness endpoints
The gateway SHALL expose `GET /livez` and `GET /readyz` as separate endpoints. `/livez` SHALL return HTTP 200 with body `{"status": "alive"}` whenever the process is running, with no external dependency checks. `/readyz` SHALL return HTTP 200 with body `{"status": "ready"}` when both Vault is reachable AND the most recent `LookupSelf` succeeded within the last 30 seconds; otherwise HTTP 503 with body `{"status": "not_ready", "reason": "<vault_unreachable|token_invalid|token_lookup_stale>"}`. Both endpoints SHALL be unauthenticated. The existing `GET /health` route SHALL remain as an alias for `/readyz` for one minor-version cycle, with `Deprecation: true` and `Sunset` response headers (per the `/v1/` deprecation pattern).

#### Scenario: Liveness always alive
- **WHEN** the process is up and serving HTTP, regardless of Vault state
- **THEN** `GET /livez` returns HTTP 200 with `{"status": "alive"}`

#### Scenario: Readiness ready
- **WHEN** Vault is reachable AND `LookupSelf` succeeded within the last 30 seconds
- **THEN** `GET /readyz` returns HTTP 200 with `{"status": "ready"}`

#### Scenario: Readiness not ready — Vault unreachable
- **WHEN** Vault is unreachable
- **THEN** `GET /readyz` returns HTTP 503 with `{"status": "not_ready", "reason": "vault_unreachable"}`

#### Scenario: Readiness not ready — token expired
- **WHEN** Vault is reachable but the gateway's token can no longer `LookupSelf` (token expired or revoked)
- **THEN** `GET /readyz` returns HTTP 503 with `{"status": "not_ready", "reason": "token_invalid"}`

#### Scenario: Legacy `/health` continues
- **WHEN** a client GETs `/health`
- **THEN** the response is identical to `/readyz` AND includes `Deprecation: true` and `Sunset: <RFC1123-date≥90-days-out>` headers

---

### Requirement: Prometheus metrics endpoint
The gateway SHALL expose `GET /metrics` returning Prometheus exposition format. The endpoint SHALL be unauthenticated and SHALL be subject to a per-IP slow-path rate limiter (default 10 rps, burst 5). The endpoint SHALL be served from the bare path `/metrics` (NOT under `/v1/`). The following metrics SHALL be registered:

- `kms_http_requests_total{path,method,status}` (counter)
- `kms_http_request_duration_seconds{path,method}` (histogram)
- `kms_signing_duration_seconds{chain}` (histogram) — `chain` is `"evm"` or `"cosmos"`
- `kms_rate_limit_rejections_total{path}` (counter)
- `kms_vault_calls_total{op,status}` (counter)
- `kms_vault_call_duration_seconds{op}` (histogram)
- `kms_token_renewal_failures_total` (counter)
- `kms_panics_total{path}` (counter)

The `path` label SHALL use the matched route pattern, not the raw URL, to bound cardinality.

#### Scenario: Metrics endpoint reachable
- **WHEN** a client GETs `/metrics`
- **THEN** the response is HTTP 200 with `Content-Type: text/plain; version=0.0.4` (Prometheus exposition format) and the body includes the listed metric names

#### Scenario: Request metric is incremented
- **WHEN** an authorized client successfully POSTs to `/v1/sign/evm`
- **THEN** `kms_http_requests_total{path="/v1/sign/evm",method="POST",status="200"}` increments by 1 AND `kms_http_request_duration_seconds_bucket{path="/v1/sign/evm",method="POST",...}` increments

#### Scenario: Path label uses matched route, not raw URL
- **WHEN** a request hits `/v1/keys/info?path=proj-a/evm/alice`
- **THEN** the `path` label on `kms_http_requests_total` is `"/v1/keys/info"` (the matched route), not the full URL with query parameters

#### Scenario: Rate-limit rejection metered
- **WHEN** a request is rejected with HTTP 429 by the per-principal limiter
- **THEN** `kms_rate_limit_rejections_total{path="<matched-pattern>"}` increments by 1

#### Scenario: Token renewal failure metered
- **WHEN** the Vault client's renewal goroutine receives a non-nil error from `RenewSelf` or `LookupSelf`
- **THEN** `kms_token_renewal_failures_total` increments by 1

---

### Requirement: Request-ID propagation
The gateway SHALL accept an inbound `X-Request-ID` header (validated against pattern `^[A-Za-z0-9._-]{1,128}$`) or generate a UUIDv4 if absent or invalid. The canonical request ID SHALL be echoed back via the `X-Request-ID` response header, stored in `r.Context()`, and emitted as a `request_id=<id>` field on every `slog.*Context` log line for that request.

#### Scenario: Inbound request ID is preserved
- **WHEN** a request includes `X-Request-ID: my-trace-id-123`
- **THEN** the response includes `X-Request-ID: my-trace-id-123` AND every log line for that request includes `request_id=my-trace-id-123`

#### Scenario: Missing request ID is generated
- **WHEN** a request has no `X-Request-ID` header
- **THEN** the gateway generates a UUIDv4, returns it in the `X-Request-ID` response header, and uses it in logs

#### Scenario: Malformed request ID is replaced
- **WHEN** a request includes `X-Request-ID: ` containing 1000 characters or invalid characters (`<`, `>`, spaces)
- **THEN** the gateway generates a fresh UUIDv4 and uses that as the canonical ID (the inbound malformed value is discarded and not logged as-is)

---

### Requirement: Panic-recovery middleware
The gateway SHALL wrap every route with a top-level recovery middleware that catches any panic, logs it at `error` with the request ID and stack trace, increments `kms_panics_total`, and returns HTTP 500 with body `{"error": "internal server error", "request_id": "<id>"}`. A panic in any handler SHALL NOT crash the gateway process or terminate the listener.

#### Scenario: Handler panic returns 500
- **WHEN** a handler invokes a `panic("unexpected nil")`
- **THEN** the client receives HTTP 500 with body `{"error": "internal server error", "request_id": "<the-request's-id>"}` AND the process keeps running

#### Scenario: Panic stack trace logged
- **WHEN** a handler panics
- **THEN** a single `error`-level log line is emitted with fields `panic=<value>`, `stack=<...>`, `request_id=<...>`

#### Scenario: Panic counter increments
- **WHEN** a handler panics
- **THEN** `kms_panics_total{path="<matched-pattern>"}` increments by 1

---

### Requirement: Rate-limit rejections are logged at info and counted
When the per-principal rate limiter (or the `/health`/`/metrics` slow-path limiter) rejects a request, the gateway SHALL emit a single `info`-level log line with fields `reason=rate_limited`, `path=<matched-pattern>`, and the request ID. The gateway SHALL increment `kms_rate_limit_rejections_total{path=...}`. The rejection SHALL NOT be logged at `warn` (rate limiting is expected behaviour under load).

#### Scenario: 429 logged at info
- **WHEN** the gateway returns HTTP 429 due to rate-limit exhaustion
- **THEN** a single log line is emitted at `info` level with `reason=rate_limited`

#### Scenario: 429 not logged at warn
- **WHEN** the gateway returns HTTP 429
- **THEN** NO `warn`-level log line is emitted for this rejection (only the `info` line)

---

### Requirement: Startup logs the Swagger UI URL
When `gateway.swagger_enabled=true` and the listener starts successfully, the gateway SHALL emit one `info`-level log line containing the externally-reachable Swagger UI URL, derived using the same trusted-proxy resolver used to compute the OpenAPI `servers[].url`.

#### Scenario: UI URL logged once at startup
- **WHEN** the gateway starts with `swagger_enabled=true` and `gateway.public_url=https://kms.example.com`
- **THEN** a single log line is emitted: `info ... swagger UI url=https://kms.example.com/swagger/index.html`

#### Scenario: URL respects loopback bind
- **WHEN** the gateway starts on `127.0.0.1:8080` with no `public_url` set
- **THEN** the logged URL is `http://127.0.0.1:8080/swagger/index.html`

#### Scenario: No log when swagger disabled
- **WHEN** `swagger_enabled=false`
- **THEN** no Swagger UI URL log line is emitted at startup

---

### Requirement: Cross-change integration — probe endpoints bypass auth middleware
The gateway SHALL exempt `/livez`, `/readyz`, `/health`, and `/metrics` from the bearer-token authentication middleware introduced by `harden-gateway-security`. The auth middleware MUST NOT intercept requests to these probe and observability endpoints regardless of whether an `Authorization` header is present. These scenarios verify the combined behaviour of `harden-gateway-security` (auth middleware) and this change (probe endpoints); both changes must be applied for these scenarios to be testable.

#### Scenario: /livez is reachable without a token
- **WHEN** `harden-gateway-security` auth middleware is active AND a client sends `GET /livez` with no `Authorization` header
- **THEN** the gateway returns HTTP 200 with `{"status": "alive"}` — the auth middleware MUST NOT intercept probe endpoints

#### Scenario: /readyz is reachable without a token
- **WHEN** `harden-gateway-security` auth middleware is active AND a client sends `GET /readyz` with no `Authorization` header
- **THEN** the gateway returns HTTP 200 or 503 (depending on Vault state) — the auth middleware MUST NOT intercept probe endpoints

#### Scenario: /health alias returns the /readyz shape after both changes land
- **WHEN** both `harden-gateway-security` and `add-observability-and-ops` are applied AND a client sends `GET /health`
- **THEN** the response body is `{"status": "ready"}` (HTTP 200) or `{"status": "not_ready", "reason": "..."}` (HTTP 503) — the legacy `{"status": "ok", "vault": "reachable"}` shape from `harden-gateway-security` alone is superseded; `Deprecation: true` and `Sunset` headers are present

