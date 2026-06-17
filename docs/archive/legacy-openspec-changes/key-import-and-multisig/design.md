## Context

The existing KMS gateway generates all signing keys inside the `kms-vault-plugin` and never exports them. Teams migrating from externally-managed wallets (raw EVM private keys, Cosmos mnemonics) have no supported import path. Additionally, multi-party signing (ops + user co-signing) is completely unsupported — operators must resort to unsafe key duplication or off-system tooling.

The `kms-vault-plugin` (introduced in `multi-chain-kms-gateway`) exposes a `POST kms/keys/<path>/import` endpoint that accepts a raw 32-byte secp256k1 private key over TLS. The plugin validates the key, derives its address and compressed public key, and stores it in Vault's encrypted logical storage. This is the foundation for the import feature.

> **Why not Transit wrapping (D7 — superseded)**: The original design relied on Vault Transit's RSA-OAEP wrapping import API (Vault 1.11+). That design is invalidated because Vault OSS Transit does not support secp256k1. The custom plugin accepts raw key bytes directly over TLS — providing equivalent security without the RSA wrapping ceremony. The private key is in memory only for the duration of the HTTPS request; Vault's storage layer encrypts it at rest immediately.

Constraints carried forward from the existing design:
- Keys never leave Vault after import.
- Single binary deployment (`kms-wrapper`).
- Internal-network-first; no change to listen defaults.
- Multi-tenant key path convention unchanged: `{project}/{chain}/{username}`.
- Vault version requirement: 1.17+ (plugin requires Vault 1.17 plugin SDK).

## Goals / Non-Goals

**Goals:**
- Define the key import flow (EVM raw key + Cosmos mnemonic-derived key) using the plugin's direct import endpoint.
- Specify BIP39/BIP44 derivation for Cosmos: mnemonic → private key → plugin import. Mnemonic not stored.
- Define the REST endpoint and CLI subcommand for key import.
- Specify Cosmos native multisig partial-sign: gateway returns its `SignatureV2`; caller owns signer set assembly.
- Specify EVM Gnosis Safe partial-sign: accept pre-computed `safeTxHash` (EIP-712 32-byte digest), return 65-byte signature.
- Extend Vault policy documentation for import capabilities (`kms/keys/<path>/import`).

**Non-Goals:**
- MPC / Threshold Signature Scheme (TSS) — deferred to a future change.
- Full Cosmos multisig account creation or on-chain registration — caller's responsibility.
- Gnosis Safe transaction construction (to, value, data, nonce, etc.) — caller pre-computes `safeTxHash`.
- Key export (private key retrieval from Vault) — explicitly prohibited.
- Multi-region Vault replication or key rotation automation.

## Decisions

### D7: Plugin direct import protocol (supersedes Transit wrapping)

**Decision**: Use the `kms-vault-plugin`'s `POST kms/keys/<path>/import` endpoint. The gateway or CLI sends the raw 32-byte secp256k1 private key (hex-encoded) in the request body over TLS. The plugin validates the input (32 bytes, valid secp256k1 scalar), derives the EVM address and compressed public key, and writes a `KeyEntry` with `source: imported` to Vault's encrypted logical storage. No RSA wrapping ceremony required.

**Rationale**: The original D7 (Transit RSA-OAEP wrapping) was designed specifically because Vault Transit could not accept raw key bytes — the wrapping ceremony was the only way in. The custom plugin has no such restriction: it accepts raw bytes, stores them immediately under Vault's AES-256-GCM encryption. The security model is equivalent — the key travels over TLS (encrypted in transit) and is encrypted at rest by Vault's seal key. This is the same model used by AWS KMS, GCP KMS, and Azure Key Vault for key import.

**Private key memory window**: Key material (hex string or derived bytes) is held in the gateway/CLI process memory only for the duration of the HTTPS request (typically < 100ms). It is never written to disk, never logged, and zeroed (via `defer` in the CLI) after the request completes.

**Error cases**:
- Invalid hex or wrong byte length → HTTP 400 `"private key must be 64 hex characters (32 bytes)"`
- Not a valid secp256k1 scalar → HTTP 400 `"invalid secp256k1 private key"`
- Key path already exists → HTTP 409 `"key already exists at path <path>"`

---

### D8: Cosmos mnemonic import — derive-then-import

**Decision**: For Cosmos import, the CLI accepts `--mnemonic <BIP39 words>` and `--derivation-path <BIP44 path>` (default `m/44'/118'/0'/0/0` for Cosmos Hub and MANTRA). The CLI derives the 32-byte secp256k1 private key in-memory using BIP39 entropy + BIP44 HMAC-SHA512 chain. The derived key is then sent to the plugin via the D7 flow. The mnemonic is not stored anywhere.

**Change from original D8**: The final step is now `POST kms/keys/<path>/import` (plugin) instead of the Transit RSA-OAEP wrapping flow. The derivation logic (`DeriveCosmosPrvKey`) in the gateway/CLI is unchanged.

**Rationale**: Storing the raw mnemonic in Vault KV would allow anyone with KV read access to derive all child keys. Derive-then-import scopes the secret to a single derived key per import call — consistent with the principle of least privilege.

**Alternatives considered**: Store mnemonic in KV + HD wallet derivation on-demand — rejected because it widens the mnemonic's blast radius and requires KV read at sign time.

---

### D9: Import metadata tag — `imported` vs. `generated`

**Decision**: The `kms-vault-plugin` stores `source` (`"imported"` or `"generated"`) and `imported_at` (RFC3339 timestamp) directly in the `KeyEntry` struct alongside the key material in Vault's logical storage. No separate KV mount is required. The `GET kms/keys/<path>` response includes these fields. Vault-generated keys have `source: generated`; imported keys have `source: imported` and a non-null `imported_at`.

**Change from original D9**: Metadata is now plugin-native — stored in the same logical storage entry as the key, not in a separate `secret/kms-metadata/` KV path. This eliminates the separate KV mount requirement and the `MetadataKVMount` config field. The `GET kms/keys/<path>` endpoint is the single source of truth for key provenance.

**Rationale**: Co-locating metadata with the key entry removes a race condition (key created but metadata write fails) and eliminates the KV mount policy surface. Audit requirements are still met — `source` and `imported_at` are returned on every key read and included in structured log events.

---

### D10: Cosmos multisig partial-sign — gateway-only perspective

**Decision**: The gateway signs with its own managed plugin key and returns a single `SignatureV2` (protobuf-encoded, base64). It does NOT assemble the full `MultiSignature` — the caller provides all signer public keys and threshold in the request, uses them for tx building on their side, and submits the gateway's partial sig alongside their own.

**Rationale**: The gateway's security model is to sign with its keys and return signatures — not to coordinate between parties. Keeping partial-sign stateless avoids the complexity of session management, retry logic, and TTL enforcement. The caller (client app) is the natural coordinator.

**What the request includes**: `key_path`, `sign_mode` (DIRECT or AMINO_JSON), `sign_doc` (base64), `signer_index` (position of this key in the multisig account's pubkey list — needed to construct `SignatureV2` correctly).

---

### D11: EVM Gnosis Safe sign — `safeTxHash` only

**Decision**: `POST /sign/evm/safe` accepts a pre-computed 32-byte `safe_tx_hash` (hex). The gateway treats it identically to EIP-712 digest signing (D existing). Response is a 65-byte Ethereum-compatible `signature` hex string. No Safe ABI awareness in the gateway.

**Rationale**: The safeTxHash is already a domain-specific EIP-712 digest — signing it is cryptographically identical to signing any other EIP-712 digest. A dedicated endpoint is justified for discoverability and to enforce that the input is a Safe hash (named field, explicit 32-byte validation), not to add Safe-specific logic.

**Alternatives considered**: Reuse existing `POST /sign/evm` with `eip712_digest` field and document the Safe use case — rejected because it's not obvious to callers that this endpoint can be used for Safe signing, and there's no field for `safe_address` context in audit logs.

---

### D12: New REST routes

| Method | Primary Path | Bare Alias (deprecated) | Auth | Purpose |
|--------|--------------|------------------------|------|---------|
| POST | `/v1/keys/import` | `/keys/import` | Bearer | Import EVM or Cosmos-derived key via plugin direct import |
| POST | `/v1/sign/cosmos/partial` | `/sign/cosmos/partial` | Bearer | Return partial Cosmos multisig signature |
| POST | `/v1/sign/evm/safe` | `/sign/evm/safe` | Bearer | Sign Gnosis Safe safeTxHash |

All routes are dual-mounted per the `polish-api-correctness` change: the `/v1/...` form is primary and advertised in OpenAPI; the bare-path form responds identically but emits `Deprecation: true` and `Sunset` headers. All existing pre-`/v1/` routes are unchanged in behaviour.

> **Note on `/keys/import` auth (resolved)**: authorization for import is enforced at the Vault layer via the scoped policy installed by `harden-vault-backend` (`vault/init.sh`). A token whose policy lacks `create` on `kms/keys/+/import` cannot perform imports regardless of the bearer token presented at the gateway. A separate `KMS_IMPORT_TOKEN` is NOT introduced — see Open Question #2 (resolved).

---

### D13: New CLI subcommands

| Command | Flags | Purpose |
|---------|-------|---------|
| `kms-wrapper keys import` | `--path`, `--chain` (evm\|cosmos), `--private-key` OR `--mnemonic`, `--derivation-path` | Import key via plugin direct-import endpoint |
| `kms-wrapper sign cosmos partial` | `--path`, `--hrp`, `--mode`, `--sign-doc`, `--signer-index` | Partial Cosmos multisig sign |
| `kms-wrapper sign evm safe` | `--path`, `--safe-tx-hash` | Sign Gnosis Safe safeTxHash |

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| Imported keys were previously exposed (e.g. lived in plaintext config) | Document that import does not retroactively make a key safe if it was already compromised. Recommend rotating on-chain accounts after import. |
| BIP39 mnemonic exposure in shell history | Document: use `--mnemonic` with `$(cat -)` or env var injection, not inline. Add warning in CLI output. |
| Raw private key transmitted over TLS | Key travels over TLS (AES-128/256-GCM with forward secrecy); Vault stores it encrypted at rest immediately. Equivalent security model to AWS KMS / GCP KMS import. Any pre-existing exposure of the key is outside the gateway's threat model — same as before. |
| Cosmos BIP44 derivation path mistake (wrong account index) | CLI prints the derived address before confirming import. Operator can abort if address is wrong. |
| Partial-sign returns a signature the caller misuses (wrong signer index) | `signer_index` is validated against the number of provided public keys in the request. Error returned for out-of-range index. |
| `safeTxHash` passed incorrectly (e.g. full tx data instead of hash) | 32-byte enforcement at the REST layer catches all non-digest inputs. |
| Plugin `import` endpoint callable by any bearer token holder | Recommend separate elevated `KMS_IMPORT_TOKEN` for `/keys/import` in production (see Open Questions). |

## Open Questions

1. **Derivation path defaults**: Should MANTRA chain use `m/44'/118'/0'/0/0` (Cosmos generic) or a MANTRA-specific coin type? Confirm with chain team before implementation.
2. **Import endpoint auth**: ~~Should `POST /keys/import` require an elevated bearer token (separate `KMS_IMPORT_TOKEN`) vs. the standard gateway token?~~ **Resolved**: deferred to the Vault policy boundary. The `harden-vault-backend` change installs a scoped policy via `vault/init.sh` and refuses to start with a root token; tokens whose policy lacks `create` on `kms/keys/+/import` cannot perform imports even if they hold the gateway bearer. This provides a cleaner authz boundary than a second bearer secret to rotate. Operators wanting two-person review on imports SHOULD use a per-operator Vault token issued with a stricter, time-bound policy.
3. **Metadata KV mount**: ~~Use `secret/` KV v2 mount or a dedicated `kms-metadata/` mount?~~ **Resolved by D9**: metadata is now stored plugin-native alongside the key. No separate KV mount required.
