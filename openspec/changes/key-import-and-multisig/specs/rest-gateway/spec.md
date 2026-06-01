## ADDED Requirements

### Requirement: Key import endpoint
The gateway SHALL expose `POST /v1/keys/import` authenticated via the standard bearer token. (The bare path `/keys/import` SHALL be served as a deprecated alias per the dual-mount requirement established in `polish-api-correctness`.) The request SHALL accept `key_path`, `chain` (`evm` or `cosmos`), and either `private_key` (hex, EVM) or `mnemonic` + `derivation_path` (Cosmos). On first-time successful import the response SHALL be HTTP 201; the body SHALL include the derived address and `source: imported`. The endpoint SHALL never log or return the provided private key or mnemonic.

#### Scenario: Import EVM key returns 201
- **WHEN** `POST /v1/keys/import` is called with `{"key_path": "proj/evm/ops", "chain": "evm", "private_key": "0x<64-hex>"}` and that key path does not yet exist
- **THEN** the gateway returns HTTP 201 with `{"key_path": "proj/evm/ops", "evm_address": "0x...", "source": "imported", "imported_at": "<RFC3339>"}`

#### Scenario: Import Cosmos mnemonic key returns 201
- **WHEN** `POST /v1/keys/import` is called with `{"key_path": "proj/mantra/ops", "chain": "cosmos", "mnemonic": "<words>", "derivation_path": "m/44'/118'/0'/0/0", "hrp": "mantra"}`
- **THEN** the gateway returns HTTP 201 with `{"key_path": "proj/mantra/ops", "cosmos_address": "mantra1...", "source": "imported", "imported_at": "<RFC3339>"}`

#### Scenario: Missing required fields
- **WHEN** `key_path` or `chain` is absent, or `chain=evm` with no `private_key`, or `chain=cosmos` with no `mnemonic`
- **THEN** the gateway returns HTTP 400: `{"error": "<field> is required"}`

#### Scenario: Unsupported chain value
- **WHEN** `chain` is not `evm` or `cosmos`
- **THEN** the gateway returns HTTP 400: `{"error": "unsupported chain: <value> — must be evm or cosmos"}`

#### Scenario: Key already exists — mapped via typed Vault error
- **WHEN** the target key path already has a key (the plugin's import handler returns `logical.CodedError(409, ...)`)
- **THEN** the Vault client maps the typed `*vaultapi.ResponseError` with `StatusCode == 409` to the `types.ErrKeyExists` sentinel (per `harden-vault-backend`'s typed-error pattern), and the gateway returns HTTP 409 with body `{"error": "key already exists at path <path>"}` — the gateway SHALL NOT substring-match the Vault error message text to derive this 409

#### Scenario: Plugin rejects malformed key_path
- **WHEN** the `key_path` fails the `{project}/{chain}/{username}` regex at the plugin layer (per `harden-vault-backend`'s plugin-side validation on the import write path)
- **THEN** the gateway returns HTTP 400 with the plugin's validation message; no Vault storage write is performed

#### Scenario: Key material never in response body
- **WHEN** any import request completes (success or failure)
- **THEN** the response body contains no private key hex, mnemonic words, or raw key bytes

---

### Requirement: Partial Cosmos multisig sign endpoint
The gateway SHALL expose `POST /v1/sign/cosmos/partial` authenticated via bearer token. (The bare path `/sign/cosmos/partial` SHALL be served as a deprecated alias per the dual-mount requirement.) The request SHALL include `key_path`, `sign_mode`, `sign_doc`, `signer_index`, `multisig_pubkeys`, and `threshold`. The response SHALL include the partial `SignatureV2` and the gateway's compressed public key. When `sign_mode` is `AMINO_JSON`, canonicalisation SHALL use `sdk.SortJSON` per the `cosmos-signer` capability as updated by `polish-api-correctness`.

#### Scenario: Successful partial sign
- **WHEN** `POST /v1/sign/cosmos/partial` is called with all required fields and the gateway key exists
- **THEN** the gateway returns HTTP 200 with `{"signature": "<base64>", "pub_key": "<base64>", "signer_index": N}`

#### Scenario: signer_index mismatch
- **WHEN** the public key at `signer_index` in `multisig_pubkeys` does not match the gateway's key for `key_path`
- **THEN** the gateway returns HTTP 400: `{"error": "key at signer_index does not match gateway key for path <path>"}`

#### Scenario: Vault signing error surfaced via typed mapping
- **WHEN** Vault returns an error during partial signing
- **THEN** the gateway maps the error via `harden-vault-backend`'s typed `errors.As` pattern (403 → 403, 404 → 404, other → 500) and returns the appropriate status with body `{"error": "<sanitised-message>"}` — never exposing key material or raw Vault tokens

---

### Requirement: EVM Safe sign endpoint
The gateway SHALL expose `POST /v1/sign/evm/safe` authenticated via bearer token. (The bare path `/sign/evm/safe` SHALL be served as a deprecated alias per the dual-mount requirement.) The request SHALL include `key_path` and `safe_tx_hash` (32-byte hex). The response SHALL include the 65-byte signature hex and the signer's EVM address.

#### Scenario: Successful Safe sign
- **WHEN** `POST /v1/sign/evm/safe` is called with `{"key_path": "...", "safe_tx_hash": "0x<64-hex>"}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "0x<130-hex>", "signer_address": "0x<EIP-55-address>"}`

#### Scenario: Invalid hash
- **WHEN** `safe_tx_hash` is not exactly 32 bytes
- **THEN** the gateway returns HTTP 400: `{"error": "safe_tx_hash must be exactly 32 bytes"}`

#### Scenario: Audit log entry includes request_id
- **WHEN** `POST /v1/sign/evm/safe` is called (success or failure)
- **THEN** a structured log entry is written at `info` (success) or `warn`/`error` (failure) level with fields `key_path`, `signer_address`, `safe_tx_hash`, `request_id` (from the propagation middleware in `add-observability-and-ops`), and timestamp — never with signature bytes or key material
