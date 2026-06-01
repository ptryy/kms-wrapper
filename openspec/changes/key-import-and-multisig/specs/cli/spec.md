## ADDED Requirements

### Requirement: `kms-wrapper keys import` — import EVM private key
The CLI SHALL provide `kms-wrapper keys import --path <key-path> --chain evm --private-key <hex>` to import an existing EVM raw private key into Vault via the `kms-vault-plugin`'s direct import endpoint. On success, the CLI SHALL print the key path, derived EVM address, and `source: imported` confirmation, and exit 0. On any error (validation, Vault, plugin) the CLI SHALL exit with a non-zero code and print a descriptive message to stderr — no "partial success" output to stdout.

#### Scenario: Successful EVM key import
- **WHEN** `kms-wrapper keys import --path proj/evm/alice --chain evm --private-key <64-hex>` is run with Vault reachable and valid policy
- **THEN** the CLI prints the key path, EVM address (EIP-55), and exits 0

#### Scenario: Invalid private key
- **WHEN** the provided hex string is not exactly 64 characters
- **THEN** the CLI prints "error: EVM private key must be 64 hex characters (32 bytes)" to stderr and exits 1

#### Scenario: Vault version too old
- **WHEN** the Vault instance is older than the `kms-vault-plugin` SDK floor (Vault 1.17+)
- **THEN** the CLI prints "error: Vault 1.17+ required for kms-vault-plugin" to stderr and exits 1

#### Scenario: Key already exists at path
- **WHEN** a key already exists at the given path (the plugin returns HTTP 409 mapped to `types.ErrKeyExists` per `harden-vault-backend`'s typed-error pattern)
- **THEN** the CLI prints "error: key already exists at path <path> — delete it first or choose a different path" to stderr and exits 1

#### Scenario: Signer/plugin error exits non-zero with no stdout output
- **WHEN** the import call fails for any reason (Vault unreachable, plugin error, policy denial)
- **THEN** the CLI exits with a non-zero code, prints the error to stderr (wrapped as `"keys import: <reason>"`), and prints NOTHING to stdout — the err-shadowing pattern fixed in `polish-api-correctness` SHALL NOT be reintroduced here

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
The CLI SHALL provide `kms-wrapper sign cosmos partial --path <key-path> --mode <DIRECT|AMINO_JSON> --sign-doc <base64> --signer-index <N> --multisig-pubkeys <base64,...> --threshold <M>` to produce a partial Cosmos multisig signature. The implementation SHALL use a single outer-scope `err` variable for the signing-call result (NOT shadowed inside a `case` block) — this is the err-shadowing pattern fixed by `polish-api-correctness` task 1.1 and SHALL NOT be reintroduced here.

#### Scenario: Successful partial DIRECT sign
- **WHEN** valid arguments are provided and the gateway returns a partial signature
- **THEN** the CLI prints the base64-encoded SignatureV2 and public key (base64 compressed) to stdout and exits 0

#### Scenario: Missing required flag
- **WHEN** any of `--path`, `--mode`, `--sign-doc`, `--signer-index`, `--multisig-pubkeys`, or `--threshold` is omitted
- **THEN** the CLI prints "required flag missing: <flag>" to stderr and exits 1

#### Scenario: Signer error exits non-zero with no stdout output
- **WHEN** the partial-sign call fails for any reason (Vault error, mismatched signer_index, invalid sign_doc)
- **THEN** the CLI exits with a non-zero code, prints the error to stderr (wrapped as `"sign cosmos partial: <reason>"`), and prints NOTHING to stdout

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
- **THEN** the CLI prints "required flag missing: <flag>" to stderr and exits 1

#### Scenario: Signer error exits non-zero with no stdout output
- **WHEN** the Safe-sign call fails for any reason (Vault error, key not found, invalid hash)
- **THEN** the CLI exits with a non-zero code, prints the error to stderr (wrapped as `"sign evm safe: <reason>"`), and prints NOTHING to stdout
