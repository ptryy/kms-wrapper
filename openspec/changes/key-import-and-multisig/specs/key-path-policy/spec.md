## ADDED Requirements

### Requirement: Import metadata tagging (plugin-native)
After any key operation (import or generation), the `kms-vault-plugin` SHALL persist `source` (`imported` or `generated`) and a timestamp (`imported_at` for imports, `created_at` for generations) directly in the plugin's `KeyEntry` struct alongside the key material in Vault's encrypted logical storage. The `GET kms/keys/<path>` plugin endpoint SHALL return these fields. No separate KV mount is involved (per design D9).

#### Scenario: Imported key metadata
- **WHEN** a key is successfully imported via the plugin's `POST kms/keys/<path>/import` flow
- **THEN** the plugin's stored `KeyEntry` for that path has `Source: "imported"` and a non-null `ImportedAt` field; `GET kms/keys/<path>` returns these fields in the response body

#### Scenario: Generated key metadata
- **WHEN** a key is created via `keys create` (plugin generates the key inside Vault)
- **THEN** the plugin's stored `KeyEntry` for that path has `Source: "generated"` and `CreatedAt: <RFC3339>`; `ImportedAt` is null

#### Scenario: Metadata persists with the key
- **WHEN** the key entry is read after process restart or plugin reload
- **THEN** the `source`, `created_at`, and `imported_at` fields are still present (they live in the same encrypted storage entry as the key bytes — no separate write to fail)

---

### Requirement: Vault policy capability for key import
The scoped Vault policy installed by `vault/init.sh` (per the `harden-vault-backend` change) SHALL include `create` capability on the import path glob `kms/keys/+/import`. The existing per-project policy pattern SHALL be extended with this path; tokens without the capability SHALL fail the import operation at the Vault layer (HTTP 403).

#### Scenario: Extended policy for `proj-a` import
- **WHEN** the scoped token issued by `vault/init.sh` includes the `proj-a` policy with `path "kms/keys/proj-a/+/import" { capabilities = ["create"] }`
- **THEN** that token can import keys under `kms/keys/proj-a/*` AND cannot import under `kms/keys/proj-b/*`

#### Scenario: Existing signing-only token rejected at import
- **WHEN** a token has only the signing capability (`create` on `kms/sign/proj-a/*`) but lacks `create` on `kms/keys/proj-a/+/import`
- **THEN** an import request returns HTTP 403; the typed-error mapping (per `harden-vault-backend`) classifies this as `types.ErrPermission`

#### Scenario: Plugin-side path validation applies to imports
- **WHEN** an import is requested with a malformed `key_path` (e.g. uppercase, fewer than 3 segments, `..` traversal)
- **THEN** the plugin rejects with HTTP 400 BEFORE any storage write — per the plugin-side validator covered in `harden-vault-backend`'s `key-path-policy` delta
