## Context

The existing KMS gateway generates all signing keys inside Vault Transit and never exports them. Teams migrating from externally-managed wallets (raw EVM private keys, Cosmos mnemonics) have no supported import path. Additionally, multi-party signing (ops + user co-signing) is completely unsupported — operators must resort to unsafe key duplication or off-system tooling.

Vault 1.11 introduced the Transit key import API: callers can wrap raw key bytes with Vault's ephemeral RSA-OAEP wrapping key and submit the ciphertext. The key then lives in Vault Transit with the same security guarantees as a Vault-generated key. This is the foundation for the import feature.

Constraints carried forward from the existing design:
- Keys never leave Vault after import.
- Single binary deployment (`kms-wrapper`).
- Internal-network-first; no change to listen defaults.
- Multi-tenant key path convention unchanged: `{project}/{chain}/{username}`.
- Vault 1.11+ required (bumped from 1.10).

## Goals / Non-Goals

**Goals:**
- Define the key import flow (EVM raw key + Cosmos mnemonic-derived key) using Vault Transit wrapping import.
- Specify BIP39/BIP44 derivation for Cosmos: mnemonic → private key → Transit import. Mnemonic not stored.
- Define the REST endpoint and CLI subcommand for key import.
- Specify Cosmos native multisig partial-sign: gateway returns its `SignatureV2`; caller owns signer set assembly.
- Specify EVM Gnosis Safe partial-sign: accept pre-computed `safeTxHash` (EIP-712 32-byte digest), return 65-byte signature.
- Extend Vault policy documentation for import capabilities (`transit/wrapping_key`, `transit/import/<path>`).

**Non-Goals:**
- MPC / Threshold Signature Scheme (TSS) — deferred to a future change.
- Full Cosmos multisig account creation or on-chain registration — caller's responsibility.
- Gnosis Safe transaction construction (to, value, data, nonce, etc.) — caller pre-computes `safeTxHash`.
- Key export (private key retrieval from Vault) — explicitly prohibited.
- Multi-region Vault replication or key rotation automation.

## Decisions

### D7: Transit wrapping import protocol

**Decision**: Use Vault's `transit/wrapping_key` + `transit/import/<path>` API. The CLI fetches Vault's ephemeral RSA-4096 wrapping key, wraps the raw private key bytes using RSA-OAEP (SHA-256), and POSTs the ciphertext to `transit/import/<path>` with `type=ecdsa-p256k1` and `allow_plaintext_backup=false`.

**Rationale**: This is the only OSS Vault mechanism that lets an externally-generated key enter Transit with full server-side key protection. The alternative (storing raw bytes in KV) was rejected because KV secrets are readable by anyone with the policy — Transit signing never exposes the key regardless of policy.

**Private key memory window**: Key material (hex string or mnemonic) is held in process memory only for the duration of the wrapping operation (typically < 1 second). It is never written to disk, never logged, and zeroed (via `defer`) after wrapping.

---

### D8: Cosmos mnemonic import — derive-then-import

**Decision**: For Cosmos import, the CLI accepts `--mnemonic <BIP39 words>` and `--derivation-path <BIP44 path>` (default `m/44'/118'/0'/0/0` for Cosmos Hub; `m/44'/118'/0'/0/0` also works for MANTRA). The CLI derives the 32-byte secp256k1 private key in-memory using BIP39 entropy + BIP44 HMAC-SHA512 chain. The derived key is then wrapped and imported via the D7 flow. The mnemonic is not stored anywhere.

**Rationale**: Storing the raw mnemonic in Vault KV would allow anyone with KV read access to derive all child keys. Derive-then-import scopes the secret to a single derived key per import call — consistent with the principle of least privilege.

**Alternatives considered**: Store mnemonic in KV + HD wallet derivation on-demand — rejected because it widens the mnemonic's blast radius and requires KV read at sign time.

---

### D9: Import metadata tag — `imported` vs. `generated`

**Decision**: After import, write a metadata entry in Vault KV (`secret/kms-metadata/<path>`) with `source: imported` and `imported_at: <RFC3339 timestamp>`. Vault-generated keys get `source: generated`. Key path convention and Transit routing are unchanged.

**Rationale**: Audit requirements — operators need to distinguish keys that originated externally (higher risk of prior exposure) from keys generated entirely in Vault. KV metadata is the least-invasive way to attach this without modifying the key path scheme.

---

### D10: Cosmos multisig partial-sign — gateway-only perspective

**Decision**: The gateway signs with its own managed Transit key and returns a single `SignatureV2` (protobuf-encoded, base64). It does NOT assemble the full `MultiSignature` — the caller provides all signer public keys and threshold in the request, uses them for tx building on their side, and submits the gateway's partial sig alongside their own.

**Rationale**: The gateway's security model is to sign with its keys and return signatures — not to coordinate between parties. Keeping partial-sign stateless avoids the complexity of session management, retry logic, and TTL enforcement. The caller (client app) is the natural coordinator.

**What the request includes**: `key_path`, `sign_mode` (DIRECT or AMINO_JSON), `sign_doc` (base64), `signer_index` (position of this key in the multisig account's pubkey list — needed to construct `SignatureV2` correctly).

---

### D11: EVM Gnosis Safe sign — `safeTxHash` only

**Decision**: `POST /sign/evm/safe` accepts a pre-computed 32-byte `safe_tx_hash` (hex). The gateway treats it identically to EIP-712 digest signing (D existing). Response is a 65-byte Ethereum-compatible `signature` hex string. No Safe ABI awareness in the gateway.

**Rationale**: The safeTxHash is already a domain-specific EIP-712 digest — signing it is cryptographically identical to signing any other EIP-712 digest. A dedicated endpoint is justified for discoverability and to enforce that the input is a Safe hash (named field, explicit 32-byte validation), not to add Safe-specific logic.

**Alternatives considered**: Reuse existing `POST /sign/evm` with `eip712_digest` field and document the Safe use case — rejected because it's not obvious to callers that this endpoint can be used for Safe signing, and there's no field for `safe_address` context in audit logs.

---

### D12: New REST routes

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/keys/import` | Bearer | Import EVM or Cosmos-derived key via Transit wrapping |
| POST | `/sign/cosmos/partial` | Bearer | Return partial Cosmos multisig signature |
| POST | `/sign/evm/safe` | Bearer | Sign Gnosis Safe safeTxHash |

All existing routes unchanged.

---

### D13: New CLI subcommands

| Command | Flags | Purpose |
|---------|-------|---------|
| `kms-wrapper keys import` | `--path`, `--chain` (evm\|cosmos), `--private-key` OR `--mnemonic`, `--derivation-path` | Import key via Transit wrapping |
| `kms-wrapper sign cosmos partial` | `--path`, `--hrp`, `--mode`, `--sign-doc`, `--signer-index` | Partial Cosmos multisig sign |
| `kms-wrapper sign evm safe` | `--path`, `--safe-tx-hash` | Sign Gnosis Safe safeTxHash |

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| Imported keys were previously exposed (e.g. lived in plaintext config) | Document that import does not retroactively make a key safe if it was already compromised. Recommend rotating on-chain accounts after import. |
| BIP39 mnemonic exposure in shell history | Document: use `--mnemonic` with `$(cat -)` or env var injection, not inline. Add warning in CLI output. |
| Vault 1.11 wrapping key API unavailable (older Vault) | CLI detects `404` on `transit/wrapping_key` and exits with clear error: "Vault 1.11+ required for key import". |
| Cosmos BIP44 derivation path mistake (wrong account index) | CLI prints the derived address before confirming import. Operator can abort if address is wrong. |
| Partial-sign returns a signature the caller misuses (wrong signer index) | `signer_index` is validated against the number of provided public keys in the request. Error returned for out-of-range index. |
| `safeTxHash` passed incorrectly (e.g. full tx data instead of hash) | 32-byte enforcement at the REST layer catches all non-digest inputs. |

## Open Questions

1. **Derivation path defaults**: Should MANTRA chain use `m/44'/118'/0'/0/0` (Cosmos generic) or a MANTRA-specific coin type? Confirm with chain team before implementation.
2. **Import endpoint auth**: Should `POST /keys/import` require an elevated bearer token (separate `KMS_IMPORT_TOKEN`) vs. the standard gateway token? The import operation is significantly more sensitive than signing. Recommend: yes, separate token, but left open for operator feedback.
3. **Metadata KV mount**: Use `secret/` KV v2 mount or a dedicated `kms-metadata/` mount? Depends on what's pre-provisioned in the target Vault. Default to `secret/` KV v2 but make mount path configurable.
