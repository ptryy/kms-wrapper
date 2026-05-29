## ADDED Requirements

### Requirement: Key import endpoint
The gateway SHALL expose `POST /keys/import` authenticated via the standard bearer token. The request SHALL accept `key_path`, `chain` (`evm` or `cosmos`), and either `private_key` (hex, EVM) or `mnemonic` + `derivation_path` (Cosmos). On success, the response SHALL include the derived address and metadata confirmation. The endpoint SHALL never log or return the provided private key or mnemonic.

#### Scenario: Import EVM key
- **WHEN** `POST /keys/import` is called with `{"key_path": "proj/evm/ops", "chain": "evm", "private_key": "0x<64-hex>"}`
- **THEN** the gateway returns HTTP 200 with `{"key_path": "proj/evm/ops", "evm_address": "0x...", "source": "imported"}`

#### Scenario: Import Cosmos mnemonic key
- **WHEN** `POST /keys/import` is called with `{"key_path": "proj/mantra/ops", "chain": "cosmos", "mnemonic": "<words>", "derivation_path": "m/44'/118'/0'/0/0", "hrp": "mantra"}`
- **THEN** the gateway returns HTTP 200 with `{"key_path": "proj/mantra/ops", "cosmos_address": "mantra1...", "source": "imported"}`

#### Scenario: Missing required fields
- **WHEN** `key_path` or `chain` is absent, or `chain=evm` with no `private_key`, or `chain=cosmos` with no `mnemonic`
- **THEN** the gateway returns HTTP 400: `{"error": "<field> is required"}`

#### Scenario: Unsupported chain value
- **WHEN** `chain` is not `evm` or `cosmos`
- **THEN** the gateway returns HTTP 400: `{"error": "unsupported chain: <value> — must be evm or cosmos"}`

#### Scenario: Key already exists
- **WHEN** the target key path already has a Vault Transit key
- **THEN** the gateway returns HTTP 409: `{"error": "key already exists at path <path>"}`

#### Scenario: Key material never in response body
- **WHEN** any import request completes (success or failure)
- **THEN** the response body contains no private key hex, mnemonic words, or raw key bytes

---

### Requirement: Partial Cosmos multisig sign endpoint
The gateway SHALL expose `POST /sign/cosmos/partial` authenticated via bearer token. The request SHALL include `key_path`, `sign_mode`, `sign_doc`, `signer_index`, `multisig_pubkeys`, and `threshold`. The response SHALL include the partial `SignatureV2` and the gateway's compressed public key.

#### Scenario: Successful partial sign
- **WHEN** `POST /sign/cosmos/partial` is called with all required fields and the gateway key exists
- **THEN** the gateway returns HTTP 200 with `{"signature": "<base64>", "pub_key": "<base64>", "signer_index": N}`

#### Scenario: signer_index mismatch
- **WHEN** the public key at `signer_index` in `multisig_pubkeys` does not match the gateway's key for `key_path`
- **THEN** the gateway returns HTTP 400: `{"error": "key at signer_index does not match gateway key for path <path>"}`

#### Scenario: Vault signing error
- **WHEN** Vault returns an error during partial signing
- **THEN** the gateway returns HTTP 500: `{"error": "<vault-error>"}` without exposing key material

---

### Requirement: EVM Safe sign endpoint
The gateway SHALL expose `POST /sign/evm/safe` authenticated via bearer token. The request SHALL include `key_path` and `safe_tx_hash` (32-byte hex). The response SHALL include the 65-byte signature hex and the signer's EVM address.

#### Scenario: Successful Safe sign
- **WHEN** `POST /sign/evm/safe` is called with `{"key_path": "...", "safe_tx_hash": "0x<64-hex>"}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "0x<130-hex>", "signer_address": "0x<EIP-55-address>"}`

#### Scenario: Invalid hash
- **WHEN** `safe_tx_hash` is not exactly 32 bytes
- **THEN** the gateway returns HTTP 400: `{"error": "safe_tx_hash must be exactly 32 bytes"}`

#### Scenario: Audit log emitted
- **WHEN** `POST /sign/evm/safe` is called (success or failure)
- **THEN** a structured log entry is written with `key_path`, `signer_address`, `safe_tx_hash`, and timestamp — never with signature bytes or key material
