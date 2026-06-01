## ADDED Requirements

### Requirement: Plugin direct import endpoint
The `kms-vault-plugin` SHALL expose a `POST kms/keys/<path>/import` endpoint that accepts a raw 32-byte secp256k1 private key (hex-encoded in `private_key_hex`), validates it, derives the EVM address and compressed public key, and writes a `KeyEntry{Source: "imported", ImportedAt: now}` to Vault's encrypted logical storage. (The original Vault Transit RSA-OAEP wrapping flow is superseded by design decision D7 — Transit does not support secp256k1.)

#### Scenario: Successful import via plugin endpoint
- **WHEN** an authorized caller invokes `POST kms/keys/proj-a/evm/alice/import` with `{"private_key_hex": "<64-hex>"}` and that key path does not yet exist
- **THEN** the plugin validates the scalar, derives the EVM address and compressed public key, writes the `KeyEntry` to logical storage with `Source: "imported"` and `ImportedAt: <RFC3339>`, and returns HTTP 200 with the key info (no key material echoed back)

#### Scenario: Key path already exists
- **WHEN** the target key path already has a stored `KeyEntry`
- **THEN** the plugin returns `logical.CodedError(409, "key already exists at <path>")` — HTTP 409. Import is NOT idempotent (unlike `create`). The Vault client's typed-error mapping (per `harden-vault-backend`) translates this to `types.ErrKeyExists`.

#### Scenario: Malformed key_path rejected before storage read
- **WHEN** the `key_path` fails the `{project}/{chain}/{username}` validator (uppercase, fewer than 3 segments, `..`, etc.)
- **THEN** the plugin returns HTTP 400 with the validator message and performs NO storage read or write — per `harden-vault-backend`'s plugin-side validation requirement

---

### Requirement: Import EVM raw private key
The system SHALL accept a hex-encoded 32-byte EVM private key, validate it is a valid secp256k1 scalar (`0 < k < n`), and submit it to the plugin's direct-import endpoint over the TLS-protected Vault API. The private key bytes SHALL exist in process memory only for the duration of the HTTPS request, SHALL be zeroed via `defer` immediately after the request returns, and SHALL never be written to logs or disk.

#### Scenario: Successful EVM key import
- **WHEN** a valid 64-hex-character (32-byte) private key is provided with a valid key path
- **THEN** the key is imported into the plugin, the system returns the derived EVM address (EIP-55 checksummed) and `source: imported` confirmation, and the private key bytes are zeroed from memory before the function returns

#### Scenario: Invalid private key format
- **WHEN** the provided private key is not exactly 64 hex characters (with optional `0x` prefix)
- **THEN** the system returns an error before contacting Vault: "EVM private key must be 64 hex characters (32 bytes)"

#### Scenario: Private key not on secp256k1 curve
- **WHEN** the provided 32-byte value is zero, equal to the curve order, or greater than the curve order
- **THEN** the system returns an error: "provided key is not a valid secp256k1 private key"

#### Scenario: Import denied by Vault policy
- **WHEN** the Vault token lacks `create` capability on `kms/keys/<path>/import`
- **THEN** the typed-error mapping (per `harden-vault-backend`) classifies the 403 as `types.ErrPermission`; the gateway/CLI surfaces a 403 with path context

---

### Requirement: Import Cosmos mnemonic-derived private key
The system SHALL accept a BIP39 mnemonic (12 or 24 words) and a BIP44 derivation path, derive the secp256k1 private key in-memory, and submit it to the plugin's direct-import endpoint. The mnemonic and intermediate seed bytes SHALL be zeroed from memory after derivation. The derived address SHALL be printed before import confirmation so the operator can verify correctness (in interactive CLI mode).

#### Scenario: Successful Cosmos key import
- **WHEN** a valid BIP39 mnemonic and derivation path (e.g. `m/44'/118'/0'/0/0`) are provided with a valid key path
- **THEN** the system derives the private key, imports it into the plugin, returns the Cosmos bech32 address (using the HRP from the key path's chain segment or `--hrp` flag), and zeroes mnemonic and derived key from memory

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

### Requirement: Key provenance is read via plugin (no separate metadata store)
After a successful import, key provenance (`source: imported`, `imported_at: <RFC3339>`) SHALL be readable via the plugin's `GET kms/keys/<path>` endpoint — populated by the plugin from its own stored `KeyEntry`. (No Vault KV mount is involved; the previous design that wrote a separate `secret/kms-metadata/<path>` entry is superseded by D9.)

#### Scenario: Imported key shows source on read
- **WHEN** `GET kms/keys/<path>` is called for a previously-imported key
- **THEN** the response includes `source: "imported"`, `imported_at: <RFC3339>`, and `created_at: null`

#### Scenario: Generated key shows source on read
- **WHEN** `GET kms/keys/<path>` is called for a previously-generated key
- **THEN** the response includes `source: "generated"`, `created_at: <RFC3339>`, and `imported_at: null`

#### Scenario: `kms-wrapper keys show` surfaces provenance
- **WHEN** `kms-wrapper keys show --path <path>` is run for an imported key
- **THEN** the output includes `source: imported` and `imported_at: <timestamp>` alongside the public key and derived addresses
