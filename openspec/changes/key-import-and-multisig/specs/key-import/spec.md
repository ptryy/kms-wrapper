## ADDED Requirements

### Requirement: Fetch Vault Transit wrapping key
The system SHALL retrieve Vault's ephemeral RSA-4096 public wrapping key from `transit/wrapping_key` before any import operation. This wrapping key is used to encrypt the raw private key material before submission to Vault.

#### Scenario: Successful wrapping key retrieval
- **WHEN** the Vault token has `read` capability on `transit/wrapping_key`
- **THEN** the system returns the PEM-encoded RSA-4096 public key without error

#### Scenario: Vault version too old (< 1.11)
- **WHEN** the `transit/wrapping_key` endpoint returns HTTP 404
- **THEN** the system returns a descriptive error: "Vault 1.11+ required for key import — transit/wrapping_key not found"

#### Scenario: Insufficient Vault policy for wrapping key
- **WHEN** the Vault token lacks `read` capability on `transit/wrapping_key`
- **THEN** the system returns a permission error: "vault token cannot read transit/wrapping_key — update policy to include: path \"transit/wrapping_key\" { capabilities = [\"read\"] }"

---

### Requirement: Import EVM raw private key via Transit wrapping
The system SHALL accept a hex-encoded 32-byte EVM private key, wrap it with the Vault ephemeral RSA-OAEP wrapping key (SHA-256), and submit the wrapped ciphertext to `transit/import/<path>` with `type=ecdsa-p256k1`. The private key SHALL exist in process memory only for the duration of the wrapping operation and SHALL be zeroed immediately after wrapping.

#### Scenario: Successful EVM key import
- **WHEN** a valid 64-hex-character (32-byte) private key is provided with a valid key path
- **THEN** the key is imported into Vault Transit, the system returns the derived EVM address (EIP-55 checksummed) and `source: imported` confirmation, and the private key bytes are zeroed from memory

#### Scenario: Invalid private key format
- **WHEN** the provided private key is not exactly 64 hex characters
- **THEN** the system returns an error before contacting Vault: "EVM private key must be 64 hex characters (32 bytes)"

#### Scenario: Private key not on secp256k1 curve
- **WHEN** the provided 32-byte value is not a valid secp256k1 scalar (> curve order)
- **THEN** the system returns an error: "provided key is not a valid secp256k1 private key"

#### Scenario: Key path already exists in Vault
- **WHEN** the target key path already has a Transit key
- **THEN** the system returns an error: "key already exists at path <path> — delete it first or choose a different path" (import is NOT idempotent, unlike key creation)

#### Scenario: Import denied by Vault policy
- **WHEN** the Vault token lacks `create` or `update` capability on `transit/import/<path>`
- **THEN** the system surfaces the Vault permission error with path context

---

### Requirement: Import Cosmos mnemonic-derived private key
The system SHALL accept a BIP39 mnemonic (12 or 24 words) and a BIP44 derivation path, derive the secp256k1 private key in-memory, wrap it with the Vault wrapping key, and import it into Vault Transit. The mnemonic SHALL be zeroed from memory after derivation. The derived address SHALL be printed before import confirmation so the operator can verify correctness.

#### Scenario: Successful Cosmos key import
- **WHEN** a valid BIP39 mnemonic and derivation path (e.g. `m/44'/118'/0'/0/0`) are provided with a valid key path
- **THEN** the system derives the private key, wraps and imports it into Vault Transit, returns the Cosmos bech32 address (using the HRP from the key path's chain segment or `--hrp` flag), and zeroes mnemonic and derived key from memory

#### Scenario: Invalid mnemonic (wrong word count or invalid words)
- **WHEN** the provided mnemonic has an invalid word count or contains words not in the BIP39 wordlist
- **THEN** the system returns an error before derivation: "invalid BIP39 mnemonic: <reason>"

#### Scenario: Invalid derivation path
- **WHEN** the derivation path does not conform to BIP44 format (e.g. `m/44'/118'/0'/0/0`)
- **THEN** the system returns an error: "invalid BIP44 derivation path: <path>"

#### Scenario: Operator abort after address preview
- **WHEN** the CLI prints the derived Cosmos address and the operator does not confirm (interactive mode)
- **THEN** the import is cancelled without contacting Vault, and all key material is zeroed

#### Scenario: Non-interactive mode (piped or `--yes` flag)
- **WHEN** `--yes` flag is provided or stdin is not a TTY
- **THEN** the import proceeds without address confirmation prompt

---

### Requirement: Write import metadata to Vault KV
After a successful Transit import, the system SHALL write a KV v2 metadata entry at `<kv-mount>/kms-metadata/<path>` with fields: `source` (`imported`), `chain` (from key path), `imported_at` (RFC3339 timestamp). Generated-key metadata SHALL write `source: generated` at key creation time (new to this change).

#### Scenario: Metadata written on import
- **WHEN** an EVM or Cosmos key is successfully imported
- **THEN** a KV entry at `secret/kms-metadata/<path>` is written with `source: imported` and `imported_at: <timestamp>`

#### Scenario: Metadata KV write fails (non-fatal)
- **WHEN** the KV write fails (e.g. insufficient policy on `secret/kms-metadata/*`)
- **THEN** the import is considered successful (Transit key exists), but a warning is logged: "could not write import metadata: <reason>"

#### Scenario: Metadata readable via `keys show`
- **WHEN** `kms-wrapper keys show --path <path>` is run for an imported key
- **THEN** the output includes `source: imported` and `imported_at: <timestamp>` alongside the public key and derived addresses
