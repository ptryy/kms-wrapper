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
