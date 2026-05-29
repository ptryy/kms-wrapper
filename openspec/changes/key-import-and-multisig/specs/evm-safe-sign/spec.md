## ADDED Requirements

### Requirement: Sign Gnosis Safe transaction hash (safeTxHash)
The system SHALL accept a pre-computed 32-byte Gnosis Safe transaction hash (`safeTxHash`, an EIP-712 digest) in hex format, sign it via the Vault Transit backend using the specified key path, and return a 65-byte Ethereum-compatible signature `[r(32) || s(32) || v(1)]`. The gateway SHALL NOT construct or validate Safe transaction parameters (to, value, data, nonce, etc.) — the caller is fully responsible for computing the correct `safeTxHash`.

#### Scenario: Successful Safe transaction sign
- **WHEN** `POST /sign/evm/safe` is called with `{"key_path": "proj/evm/ops-signer", "safe_tx_hash": "0x<64-hex-chars>"}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "0x<130-hex-chars>", "signer_address": "<EIP-55-checksummed-address>"}`

#### Scenario: Invalid safe_tx_hash length
- **WHEN** `safe_tx_hash` is not exactly 32 bytes (64 hex characters, optionally prefixed with `0x`)
- **THEN** the gateway returns HTTP 400: `{"error": "safe_tx_hash must be exactly 32 bytes (64 hex characters)"}`

#### Scenario: safe_tx_hash is not valid hex
- **WHEN** `safe_tx_hash` contains non-hex characters
- **THEN** the gateway returns HTTP 400: `{"error": "safe_tx_hash must be a hex-encoded value"}`

#### Scenario: key_path not found in Vault
- **WHEN** the specified `key_path` does not exist in Vault Transit
- **THEN** the gateway returns HTTP 404: `{"error": "key not found: <path>"}`

#### Scenario: Missing required fields
- **WHEN** `POST /sign/evm/safe` is called without `key_path` or `safe_tx_hash`
- **THEN** the gateway returns HTTP 400: `{"error": "<field> is required"}`

---

### Requirement: Audit log entry for Safe signing
The system SHALL emit a structured log entry for every `POST /sign/evm/safe` request at `INFO` level, including: `key_path`, `signer_address`, `safe_tx_hash` (hex), and the request timestamp. The log entry SHALL NOT include the private key or signature bytes.

#### Scenario: Successful sign produces audit log
- **WHEN** `POST /sign/evm/safe` completes successfully
- **THEN** a structured log line is emitted: `{"level":"info","msg":"evm-safe-sign","key_path":"...","signer_address":"0x...","safe_tx_hash":"0x...","ts":"<RFC3339>"}`

#### Scenario: Failed sign also produces audit log
- **WHEN** `POST /sign/evm/safe` returns an error (key not found, Vault error, invalid input)
- **THEN** a structured log line is emitted at `WARN` or `ERROR` level with `"error": "<reason>"` and the same fields (excluding signature)
