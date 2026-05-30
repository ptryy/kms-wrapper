## ADDED Requirements

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
