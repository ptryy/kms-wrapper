## ADDED Requirements

### Requirement: Import metadata tagging
After any key operation (import or generation), the system SHALL write a KV v2 metadata record at `<kv-mount>/kms-metadata/<path>` with fields: `source` (`imported` or `generated`), `chain` (chain segment from the key path), `created_at` or `imported_at` (RFC3339 timestamp). The KV mount SHALL be configurable via `KMS_METADATA_KV_MOUNT` env var (default: `secret`).

#### Scenario: Imported key metadata
- **WHEN** a key is successfully imported via the wrapping flow
- **THEN** the KV entry at `secret/kms-metadata/<path>` contains `source: imported` and `imported_at: <RFC3339>`

#### Scenario: Generated key metadata
- **WHEN** a key is created via `keys create` (Vault-generated)
- **THEN** the KV entry at `secret/kms-metadata/<path>` contains `source: generated` and `created_at: <RFC3339>`

#### Scenario: Configurable KV mount
- **WHEN** `KMS_METADATA_KV_MOUNT=kms-metadata` is set
- **THEN** the system writes metadata to `kms-metadata/kms-metadata/<path>` instead of `secret/kms-metadata/<path>`

#### Scenario: Metadata KV failure is non-fatal
- **WHEN** the KV write fails (e.g. policy missing for `secret/kms-metadata/*`)
- **THEN** the primary key operation (import or create) is considered successful, and a `WARN` log is emitted: "could not write key metadata: <reason>"

---

### Requirement: Vault policy documentation for import operations
The system SHALL document the additional Vault policy capabilities required for key import: `read` on `transit/wrapping_key` and `create`/`update` on `transit/import/<path>`. The existing per-project policy pattern SHALL be extended with these paths.

#### Scenario: Extended policy for proj-a import
- **WHEN** a Vault token with the updated `proj-a` policy is used
- **THEN** it can read `transit/wrapping_key` and import keys under `transit/import/proj-a/*` but NOT under `transit/import/proj-b/*`

#### Scenario: Existing signing policy unchanged
- **WHEN** a Vault token has only the original signing policy (no import capabilities)
- **THEN** it can still sign via `transit/sign/proj-a/*` but cannot call `transit/wrapping_key` or `transit/import/*`
