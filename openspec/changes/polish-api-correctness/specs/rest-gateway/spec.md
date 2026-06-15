## MODIFIED Requirements

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

## ADDED Requirements

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
