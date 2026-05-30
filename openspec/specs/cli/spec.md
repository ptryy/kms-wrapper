## Purpose
Define the kms-wrapper CLI commands, global flags, and operational behaviors.

## Requirements

### Requirement: Root command and global flags
The CLI SHALL be invoked as `kms-wrapper` with a `--config` flag (default `~/.kms-wrapper/config.yaml`) and a `--log-level` flag (default `info`). Unrecognised commands SHALL print usage and exit non-zero.

#### Scenario: Help output
- **WHEN** `kms-wrapper --help` is run
- **THEN** the CLI prints a usage summary listing all subcommands and exits 0

#### Scenario: Unknown subcommand
- **WHEN** `kms-wrapper unknowncmd` is run
- **THEN** the CLI prints "unknown command" and exits 1

---

### Requirement: `kms-wrapper serve` — start the REST gateway
The CLI SHALL provide a `serve` subcommand that starts the REST gateway. It SHALL block until interrupted (SIGINT/SIGTERM), logging startup parameters on launch.

#### Scenario: Successful start
- **WHEN** `kms-wrapper serve` is run with valid config and Vault reachable
- **THEN** the gateway starts, logs the listen address, and blocks until signal received

#### Scenario: Graceful shutdown
- **WHEN** SIGINT is received while the gateway is running
- **THEN** the gateway stops accepting new requests, finishes in-flight requests (up to 30s), and exits 0

---

### Requirement: `kms-wrapper keys create` — create a Vault Transit key
The CLI SHALL provide `kms-wrapper keys create --path <key-path>` to create a new secp256k1 Transit key. On success it SHALL print the key path and the derived Ethereum address and Cosmos address (with default HRP `cosmos`).

#### Scenario: Key created successfully
- **WHEN** `kms-wrapper keys create --path transit/keys/proj/evm/alice` is run
- **THEN** the CLI prints the key path, Ethereum address, and Cosmos bech32 address, and exits 0

#### Scenario: Key already exists
- **WHEN** the key path already exists in Vault
- **THEN** the CLI prints a warning "key already exists" and exits 0 (idempotent)

---

### Requirement: `kms-wrapper keys show` — display key information
The CLI SHALL provide `kms-wrapper keys show --path <key-path>` to display the public key (hex), Ethereum address, and Cosmos address for a given key path.

#### Scenario: Show existing key
- **WHEN** `kms-wrapper keys show --path <existing-path>` is run
- **THEN** the CLI prints public key (hex), Ethereum address (EIP-55), and Cosmos address

#### Scenario: Key not found
- **WHEN** the key path does not exist
- **THEN** the CLI prints "key not found: <path>" and exits 1

---

### Requirement: `kms-wrapper sign evm` — sign an EVM transaction from CLI
The CLI SHALL provide `kms-wrapper sign evm --path <key-path> --chain-id <N> --raw-tx <hex>` to sign a raw EVM transaction and print the signed hex to stdout.

#### Scenario: Successful EVM sign
- **WHEN** valid arguments are provided
- **THEN** the CLI prints the signed transaction hex to stdout and exits 0

#### Scenario: Missing required flag
- **WHEN** `--path` or `--raw-tx` is omitted
- **THEN** the CLI prints "required flag missing: <flag>" and exits 1

---

### Requirement: `kms-wrapper sign cosmos` — sign a Cosmos transaction from CLI
The CLI SHALL provide `kms-wrapper sign cosmos --path <key-path> --hrp <hrp> --mode <DIRECT|AMINO_JSON> --sign-doc <base64>` to sign a Cosmos transaction and print the base64 signature and public key to stdout.

#### Scenario: Successful Cosmos sign
- **WHEN** valid arguments and a valid sign doc are provided
- **THEN** the CLI prints signature (base64) and compressed public key (base64) to stdout and exits 0

---

### Requirement: `kms-wrapper health` — check Vault connectivity
The CLI SHALL provide `kms-wrapper health` to check Vault reachability and token validity. Output SHALL be human-readable, with exit code 0 for healthy and 1 for any failure.

#### Scenario: Healthy
- **WHEN** Vault is reachable and the token is valid
- **THEN** the CLI prints "Vault: OK (<address>)" and exits 0

#### Scenario: Vault unreachable
- **WHEN** Vault cannot be reached
- **THEN** the CLI prints "Vault: UNREACHABLE (<address>)" and exits 1
