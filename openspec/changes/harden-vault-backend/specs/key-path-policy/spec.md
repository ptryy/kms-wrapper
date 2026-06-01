## ADDED Requirements

### Requirement: Plugin enforces key-path format independently of the gateway
The Vault secrets plugin SHALL validate the `{project}/{chain}/{username}` key-name format at every write path (`create`, `import`, and any future `replace`) and at every list path. Validation SHALL use the same regex used by the REST gateway and CLI. Names that do not match SHALL be rejected with `logical.ErrInvalidRequest` (HTTP 400). The plugin SHALL NOT assume that the caller (gateway, CLI, or another Vault client) has pre-validated the input.

#### Scenario: Plugin rejects invalid name on create
- **WHEN** a Vault client calls `vault write kms/keys/Bad-Name create` (uppercase, fewer than 3 segments)
- **THEN** the plugin returns HTTP 400 with an error containing the validator's message ("key path segments must match [a-z0-9_-]" or "must have format {project}/{chain}/{username}")

#### Scenario: Plugin rejects path traversal segments
- **WHEN** a Vault client calls `vault write kms/keys/proj-a/evm/../alice create`
- **THEN** the plugin returns HTTP 400 and the key is NOT created

#### Scenario: Plugin accepts valid name
- **WHEN** a Vault client calls `vault write kms/keys/proj-a/evm/alice create`
- **THEN** the plugin creates the key (subject to Vault policy and existing idempotency rules) and returns HTTP 200

#### Scenario: List validates prefix
- **WHEN** a Vault client calls `vault list kms/keys/Proj A/`
- **THEN** the plugin returns HTTP 400 — the malformed prefix is rejected before any storage read

---

### Requirement: Vault policy install is part of bootstrap
The local-dev bootstrap (`vault/init.sh`) SHALL install `policy-project.hcl` via `vault policy write` and issue a scoped, renewable token via `vault token create -policy=<name>`. The issued token SHALL be written to `.env` as `KMS_VAULT_TOKEN`. The policy path globs SHALL match the live plugin mount (`kms/keys/+/*`, `kms/sign/+/*`).

#### Scenario: Bootstrap installs policy
- **WHEN** `make dev-up` runs `vault/init.sh` against a fresh Vault dev container
- **THEN** `vault policy list` includes the project policy and `vault token create -policy=<name>` succeeds without errors

#### Scenario: Bootstrap issues a non-root token to the gateway
- **WHEN** `make dev-up` completes successfully
- **THEN** `.env` contains `KMS_VAULT_TOKEN=<non-root-token>` and the value is NOT the literal string `root`

#### Scenario: Policy globs match the plugin mount
- **WHEN** a token bound to the project policy attempts `vault write kms/keys/proj-a/evm/alice create`
- **THEN** the write succeeds, and the same token attempting `vault write kms/keys/proj-b/evm/alice create` fails with HTTP 403
