# Design: key-import-and-multisig

**Date:** 2026-06-17
**Status:** Approved (migrated from legacy OpenSpec change `key-import-and-multisig`, reconciled with #1/#2 and hardened)
**Governing doc:** [`/CONSTITUTION.md`](../../../CONSTITUTION.md)
**Depends on (must land first):** `update-key-path-scheme` (#1), `add-key-chain-capability` (#2). Upstream deps `harden-vault-backend` and `polish-api-correctness` are already merged/archived.

## Goal

Add three capabilities — **key import** (EVM raw key + Cosmos mnemonic), **Cosmos multisig
partial-sign**, **EVM Gnosis Safe sign** — with `/v1/` REST routes and CLI subcommands. Private key
material and mnemonics never leave the request memory window; keys never leave Vault after import.

## Why

The gateway generates all keys inside the plugin and never imports. Teams migrating externally-managed
wallets (raw EVM keys, Cosmos mnemonics) have no supported path, and m-of-n co-signing forces unsafe
workarounds. The `kms-vault-plugin` exposes `POST kms/keys/<path>/import` (raw 32-byte secp256k1 over
TLS, encrypted at rest immediately) — the foundation for import without Transit's RSA-wrapping ceremony
(which OSS Transit can't do for secp256k1 anyway).

## Adopted legacy decisions (D7–D13)

- **D7** Plugin direct import (`POST kms/keys/<path>/import`, `private_key_hex`); 400 on bad hex/scalar, 409 on existing path.
- **D8** Cosmos derive-then-import: BIP39 + BIP44 in-memory → 32-byte key → plugin import. Mnemonic never stored.
- **D9** Plugin-native metadata: `source` (`generated`|`imported`) + `imported_at` stored in `KeyEntry`; no KV mount; `KMS_METADATA_KV_MOUNT` removed.
- **D10** Cosmos partial-sign is stateless: gateway returns one `SignatureV2` + `signer_index`; caller assembles `MultiSignature`.
- **D11** EVM Safe sign: `safe_tx_hash` (32-byte hex) treated as an EIP-712 digest; returns 65-byte signature.
- **D12** Dual-mounted routes (`/v1/...` primary, bare deprecated): `/v1/keys/import`, `/v1/sign/cosmos/partial`, `/v1/sign/evm/safe`.
- **D13** CLI: `keys import`, `sign cosmos partial`, `sign evm safe`.
- Import authorization is enforced at the **Vault policy boundary** (`create` on `kms/keys/+/import`), not a second bearer token.

## Reconciliation with #1 and #2 (gap analysis)

The legacy design predates #1 (path rename) and #2 (chains tag). Required adjustments:

1. **Paths are `{project}/{environment}/{username}`** (post-#1) in all examples, validators, and policy globs (`kms/keys/<project>/*`, `kms/sign/<project>/*`, `kms/keys/<project>/*/import` via the `+`-glob).
2. **Imported keys carry the #2 `chains` authz tag.** `keys import` **requires `--chains`** (decision: no silent default, consistent with #2's create rule). The `--chain evm|cosmos` flag *only* routes derivation (EVM-raw vs Cosmos-mnemonic) and is orthogonal to the persisted `chains` tag. The plugin import handler canonicalizes + persists `Chains` alongside `source: imported`.
3. **The three new sign routes are subject to #2 chain-authz.** `/sign/evm/safe` calls `authorizeChain(path, "evm")`; `/sign/cosmos/partial` calls `authorizeChain(path, "cosmos")` — a mistagged key yields 403 before signing, reusing #2's helper (with its TTL cache, re-validate-on-deny, fail-safe-503, and denial metric).

## Decisions applied this migration

- **Derivation default:** `m/44'/118'/0'/0/0` (SLIP-44 coin type 118, Cosmos standard) for MANTRA. CLI prints the derived bech32 address for confirmation before import (skippable with `--yes`/non-TTY).
- **Import requires `--chains`** (above).

## Resilience & security hardening (beyond legacy)

- **Secret zeroization end-to-end:** private-key hex bytes and mnemonic-derived key bytes are zeroed via `defer` in the CLI, the gateway handler, and the plugin. Held in memory only for the request (< ~100ms).
- **Never-log guarantee:** `private_key` and `mnemonic` request fields are redacted; the request-ID/observability middleware never logs bodies. A test asserts neither secret appears in captured logs.
- **Non-idempotent, fail-safe import:** duplicate path → typed `types.ErrKeyExists` (from the 409 `*vaultapi.ResponseError.StatusCode`, never substring-matched) → HTTP 409. A re-import cannot silently overwrite an existing key.
- **Strict input validation:** `safe_tx_hash` exactly 32 bytes; `signer_index` in range AND gateway pubkey == `multisig_pubkeys[signer_index]`; BIP39 word-count (12/24) + wordlist + BIP44 path-format checks with descriptive errors.
- **Audit log** on every Safe/partial sign: structured `info` with `key_path`, `signer_address`, `safe_tx_hash`/`signer_index`, `request_id`; errors at `warn`/`error` with `request_id`. No secrets.
- **Rate limiting:** all new routes share the existing per-principal signing limiter.

## Component map

| Layer | File(s) | Change |
|-------|---------|--------|
| Plugin | `internal/plugin/path_keys.go` | `import` handler: validate path + scalar, persist `KeyEntry{Source, ImportedAt, Chains}`, 409/400 coded errors; `GET` returns `source`/`imported_at` |
| Errors | `pkg/types/errors.go` | add `ErrKeyExists` sentinel |
| Types | `pkg/types/types.go` | `KeyInfo` gains `Source string`, `ImportedAt *time.Time` |
| Vault client | `internal/vault/client.go` | `ImportKey(ctx, path, rawKeyBytes)` (zero via defer, 409→`ErrKeyExists`); `GetKeyInfo` parses `source`/`imported_at` |
| Derivation | `internal/vault` (or `internal/derive`) | `DeriveCosmosPrvKey(mnemonic, path)`; BIP39 validate; BIP44 parse; default const; zero intermediates |
| Import svc | `internal/vault` | `ImportEVMKey`, `ImportCosmosKey` → `ImportKey` + `GetKeyInfo` |
| Gateway | `internal/gateway/*.go` | `/v1/keys/import`, `/v1/sign/cosmos/partial`, `/v1/sign/evm/safe` (dual-mount); typed-error → status; #2 chain-authz on the two sign routes; redact secrets |
| CLI | `cmd/kms-wrapper/...` | `keys import` (`--chains` required), `sign cosmos partial`, `sign evm safe`; outer-scope `err` discipline; secret-handling warnings |
| Policy/docs | `vault/init.sh`, `README.md`, `docs/e2e-runbook.md`, `docs/*` | `create` on `kms/keys/+/import`; runbook; regenerated OpenAPI |

## Error handling

- Import: bad hex/length → 400 `private key must be 64 hex characters (32 bytes)`; bad scalar → 400 `invalid secp256k1 private key`; duplicate → 409 `key already exists at path <path>`; missing `chains` → 400 (the #2 subset message); Vault 403 → 403.
- Partial-sign: out-of-range `signer_index` → 400; pubkey mismatch → 400; AMINO duplicate keys → 400 (inherited `cosmos-signer`); unsupported mode → 400.
- Safe-sign: non-32-byte / non-hex `safe_tx_hash` → 400; chain-authz fail → 403; Vault error → typed status.

## Testing

- Plugin: import success (EVM), 409 duplicate (distinct from 400), 400 bad hex/scalar/path, persisted `Chains`+`source`+`imported_at`; policy smoke test import-positive + cross-project-negative.
- Client: `ImportKey` 409→`ErrKeyExists` from status alone (body without the literal "key already exists"), 403/400 typed; `GetKeyInfo` source/imported_at parse.
- Derivation: BIP39 known test vectors (mnemonic → key → Cosmos address); invalid mnemonic / path.
- Gateway: import EVM/Cosmos → 201, missing fields → 400, dup → 409, secrets-not-logged; partial-sign DIRECT/AMINO success, index/pubkey/mode failures; Safe-sign success + audit `request_id` present, bad hash, chain-authz 403.
- CLI: import success EVM + Cosmos `--yes`, confirmation reject, missing/mutually-exclusive flags; partial & safe subcommands non-zero exit + empty stdout on signer error (err-shadowing regression).
- `go test ./...`, `make lint`, `make swagger-check` clean (new routes + 201/409 documented).
- E2E runbook (`docs/e2e-runbook.md`) covers the full lifecycle incl. import, partial multisig (2-of-3), Safe sign.

## Risks / Trade-offs

- Import does not retroactively make a previously-exposed key safe — document; recommend rotating on-chain accounts after import.
- Mnemonic in shell history — CLI warns; document `--mnemonic "$(cat -)"` / env injection.
- BIP44 wrong account index — CLI prints derived address for confirmation.
- New HD-derivation deps add supply-chain surface — pin versions; both are widely-used Cosmos/btcsuite libs.

## Constitution alignment

- Private key material never leaves Vault; only transits TLS in a bounded memory window (§4.1) — zeroized.
- Plugin remains the authoritative trust boundary; gateway fast-fails (§4.3); import authz at Vault policy (§4.5 spirit).
- Fail-safe: non-idempotent import + typed 409 (§4.4 idempotency exception is explicit and safe).
- Plugin-reality terms throughout (§7); generated docs enforced by `swagger-check` (§4.7).
