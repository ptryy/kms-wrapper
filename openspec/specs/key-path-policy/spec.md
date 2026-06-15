## Purpose
Define key-path validation rules, chain conventions, uniqueness, and tenant policy guidance.
## Requirements
### Requirement: Key path format validation
The system SHALL validate key paths against the pattern `{project}/{chain}/{username}` where each segment is a non-empty string containing only `[a-z0-9_-]` characters. The full path used in Vault Transit API calls SHALL be prefixed with `transit/keys/`.

#### Scenario: Valid key path
- **WHEN** the key path `"proj-a/evm/alice"` is validated
- **THEN** validation passes and the Vault Transit path is `transit/keys/proj-a/evm/alice`

#### Scenario: Invalid characters in segment
- **WHEN** the key path contains uppercase letters, spaces, or special characters (e.g. `"Proj A/EVM/Alice"`)
- **THEN** validation returns an error: "key path segments must match [a-z0-9_-]"

#### Scenario: Missing segment
- **WHEN** the key path has fewer than 3 `/`-separated segments (e.g. `"proj/evm"`)
- **THEN** validation returns an error: "key path must have format {project}/{chain}/{username}"

#### Scenario: Empty segment
- **WHEN** any segment in the path is empty (e.g. `"proj//alice"`)
- **THEN** validation returns an error: "key path segments must not be empty"

---

### Requirement: Chain identifier conventions
The system SHALL document and enforce the following reserved `{chain}` segment values for well-known chains. Unknown chain values SHALL be allowed (pass-through) with a warning log.

| Chain segment | Description |
|---------------|-------------|
| `evm`         | Generic EVM (use for multi-chain EVM) |
| `eth`         | Ethereum mainnet |
| `mantra`      | MANTRA chain (Cosmos SDK) |
| `cosmos`      | Cosmos Hub |
| `osmosis`     | Osmosis |

#### Scenario: Known chain segment
- **WHEN** a key path with `chain=mantra` is validated
- **THEN** validation passes without warnings

#### Scenario: Unknown chain segment
- **WHEN** a key path with `chain=mychain` (not in the reserved list) is validated
- **THEN** validation passes but a warning is logged: "unknown chain identifier: mychain"

---

### Requirement: Key path uniqueness per identity
Each combination of `{project}/{chain}/{username}` SHALL map to exactly one Vault Transit key. The system SHALL not allow creating a second key at an existing path (enforced by idempotent creation semantics from the Vault backend).

#### Scenario: Idempotent creation preserves key
- **WHEN** `keys create` is called twice with the same path
- **THEN** the same key material is returned both times (no new key generated)

---

### Requirement: Vault policy path prefix for multi-tenancy
The system SHALL document the recommended Vault policy pattern for scoping access per project. A policy for `proj-a` SHALL grant capabilities only on paths matching `transit/keys/proj-a/*` and `transit/sign/proj-a/*`.

#### Scenario: Policy prefix isolation
- **WHEN** a Vault token with a policy scoped to `transit/*/proj-a/*` is used
- **THEN** it cannot access keys under `transit/keys/proj-b/*` (enforced by Vault, validated in integration tests)

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

