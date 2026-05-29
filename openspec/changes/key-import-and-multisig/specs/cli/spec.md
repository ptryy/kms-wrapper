## ADDED Requirements

### Requirement: `kms-wrapper keys import` — import EVM private key
The CLI SHALL provide `kms-wrapper keys import --path <key-path> --chain evm --private-key <hex>` to import an existing EVM raw private key into Vault Transit via the wrapping flow. On success, the CLI SHALL print the key path, derived EVM address, and `source: imported` confirmation.

#### Scenario: Successful EVM key import
- **WHEN** `kms-wrapper keys import --path proj/evm/alice --chain evm --private-key <64-hex>` is run with Vault reachable and valid policy
- **THEN** the CLI prints the key path, EVM address (EIP-55), and exits 0

#### Scenario: Invalid private key
- **WHEN** the provided hex string is not exactly 64 characters
- **THEN** the CLI prints "error: EVM private key must be 64 hex characters (32 bytes)" and exits 1

#### Scenario: Vault version too old
- **WHEN** the Vault instance is < 1.11 (wrapping_key endpoint returns 404)
- **THEN** the CLI prints "error: Vault 1.11+ required for key import" and exits 1

#### Scenario: Key already exists at path
- **WHEN** a key already exists at the given path in Vault
- **THEN** the CLI prints "error: key already exists at path <path> — delete it first or choose a different path" and exits 1

---

### Requirement: `kms-wrapper keys import` — import Cosmos mnemonic-derived key
The CLI SHALL provide `kms-wrapper keys import --path <key-path> --chain cosmos --mnemonic "<words>" --derivation-path <m/44'/118'/0'/0/0> [--hrp <bech32-prefix>]` to derive a secp256k1 private key from a BIP39 mnemonic and import it into Vault Transit. The CLI SHALL print the derived Cosmos bech32 address for operator confirmation before importing (in interactive mode).

#### Scenario: Successful Cosmos mnemonic import — interactive confirmation
- **WHEN** the CLI derives the address and the operator types `y` at the confirmation prompt
- **THEN** the import proceeds, CLI prints the key path, Cosmos bech32 address, and exits 0

#### Scenario: Operator rejects address at confirmation
- **WHEN** the CLI derives the address and the operator types `n` at the confirmation prompt
- **THEN** the import is cancelled, no Vault write is made, CLI prints "import cancelled" and exits 0

#### Scenario: Non-interactive import with `--yes`
- **WHEN** `--yes` flag is provided
- **THEN** the import proceeds without a confirmation prompt and exits 0

#### Scenario: Invalid mnemonic
- **WHEN** the mnemonic contains invalid BIP39 words or wrong word count
- **THEN** the CLI prints "error: invalid BIP39 mnemonic: <reason>" and exits 1

#### Scenario: Default derivation path
- **WHEN** `--derivation-path` is omitted
- **THEN** the CLI uses `m/44'/118'/0'/0/0` as the default and logs the path used

---

### Requirement: `kms-wrapper sign cosmos partial` — partial multisig signing
The CLI SHALL provide `kms-wrapper sign cosmos partial --path <key-path> --mode <DIRECT|AMINO_JSON> --sign-doc <base64> --signer-index <N> --multisig-pubkeys <base64,...> --threshold <M>` to produce a partial Cosmos multisig signature.

#### Scenario: Successful partial DIRECT sign
- **WHEN** valid arguments are provided and the gateway returns a partial signature
- **THEN** the CLI prints the base64-encoded SignatureV2 and public key (base64 compressed) to stdout and exits 0

#### Scenario: Missing required flag
- **WHEN** any of `--path`, `--mode`, `--sign-doc`, `--signer-index`, `--multisig-pubkeys`, or `--threshold` is omitted
- **THEN** the CLI prints "required flag missing: <flag>" and exits 1

---

### Requirement: `kms-wrapper sign evm safe` — sign Gnosis Safe transaction
The CLI SHALL provide `kms-wrapper sign evm safe --path <key-path> --safe-tx-hash <0x...>` to sign a pre-computed Gnosis Safe transaction hash and print the 65-byte signature hex to stdout.

#### Scenario: Successful Safe sign
- **WHEN** a valid 32-byte hex `safe_tx_hash` and key path are provided
- **THEN** the CLI prints the 65-byte signature (0x-prefixed hex) and the signer address to stdout and exits 0

#### Scenario: Invalid safe_tx_hash length
- **WHEN** the provided hash is not 32 bytes
- **THEN** the CLI prints "error: safe_tx_hash must be exactly 32 bytes (64 hex characters)" and exits 1

#### Scenario: Missing required flag
- **WHEN** `--path` or `--safe-tx-hash` is omitted
- **THEN** the CLI prints "required flag missing: <flag>" and exits 1
