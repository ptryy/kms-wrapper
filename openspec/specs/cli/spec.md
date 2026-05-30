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

### Requirement: CLI config file is optional with warning fallback
The CLI SHALL treat a missing config file path as non-fatal. When the file referenced by `--config` does not exist, the CLI SHALL print a warning and continue using environment variables and defaults.

#### Scenario: Missing default config file with valid env vars
- **WHEN** `~/.kms-wrapper/config.yaml` does not exist and required runtime env vars are set
- **THEN** `kms-wrapper` commands continue startup, print a warning about missing config, and do not fail with `read config` error

#### Scenario: Missing explicit config path with valid env vars
- **WHEN** `kms-wrapper <cmd> --config /missing/path.yaml` is run and required runtime env vars are set
- **THEN** startup continues with warning + env/default fallback and command execution proceeds

#### Scenario: Missing config file and missing required runtime config
- **WHEN** config file is missing and required values are not provided via env/defaults
- **THEN** startup fails with descriptive runtime validation errors (for example, `vault addr is required`, `vault token is required`, or `gateway token is required`)

#### Scenario: Config file exists but is malformed
- **WHEN** the config file exists but cannot be parsed as valid YAML
- **THEN** startup fails with a `read config` error and exits non-zero

---

### Requirement: `kms-wrapper health` distinguishes config failures from Vault reachability
The `health` command SHALL separate configuration/runtime validation errors from Vault connectivity failures.

#### Scenario: Config/runtime validation failure
- **WHEN** required runtime config is missing after applying fallback resolution
- **THEN** `kms-wrapper health` exits non-zero with a config/runtime validation error and SHALL NOT print `Vault: UNREACHABLE ()`

#### Scenario: Vault connectivity failure
- **WHEN** runtime config is valid but Vault is unreachable
- **THEN** `kms-wrapper health` prints `Vault: UNREACHABLE (<address>)` and exits non-zero

---

### Requirement: Gateway config supports Swagger toggles via env and YAML
The CLI's config loader SHALL recognize two new optional fields under `gateway`:

- `swagger_enabled` (bool, default `true`) — bound to env var `KMS_GATEWAY_SWAGGER_ENABLED`.
- `swagger_auth` (bool, default `false`) — bound to env var `KMS_GATEWAY_SWAGGER_AUTH`.

Both SHALL be readable from a YAML config file, an env var, or fall back to the documented defaults, using the same precedence rules as the existing `gateway.*` fields.

#### Scenario: Defaults applied when unspecified
- **WHEN** neither `gateway.swagger_enabled` nor `gateway.swagger_auth` appears in the config file or env
- **THEN** the loaded config has `swagger_enabled=true` and `swagger_auth=false`

#### Scenario: Env var overrides YAML
- **WHEN** the config YAML sets `gateway.swagger_auth: false` and `KMS_GATEWAY_SWAGGER_AUTH=true` is set in the environment
- **THEN** the loaded config has `swagger_auth=true`

#### Scenario: YAML disables swagger
- **WHEN** `gateway.swagger_enabled: false` appears in the config file and no env override is set
- **THEN** the loaded config has `swagger_enabled=false` and `kms-wrapper serve` starts without registering `/swagger/*` routes

#### Scenario: Invalid boolean value
- **WHEN** `KMS_GATEWAY_SWAGGER_ENABLED=notabool` is set
- **THEN** `kms-wrapper serve` exits non-zero with a config parse error

