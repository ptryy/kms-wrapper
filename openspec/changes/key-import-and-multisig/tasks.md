## Dependencies (must land first)

> This change has hard dependencies on two upstream changes from the deep-review proposals batch. Implementation tasks below assume both are merged.

- **`harden-vault-backend`** — provides plugin-side key-path validation (reused on the new `import` write path), the typed `*vaultapi.ResponseError`-based error mapping (consumed by task 2.x for 409 detection), the scoped Vault policy install in `vault/init.sh` (extended by task 0.5 and section 11), and the non-root-token guard (`KMS_DEV=true` escape hatch covers Docker dev).
- **`polish-api-correctness`** — provides the `/v1/` route prefix and dual-mount pattern (new routes mount under `/v1/...`), `cosmos-sdk/types.SortJSON` canonicalisation for AMINO mode (inherited by `/v1/sign/cosmos/partial`), the EVM oneOf discriminator pattern (not used by `/sign/evm/safe`, which is a dedicated endpoint), and the err-shadowing fix pattern (NEW CLI subcommands here SHALL NOT reintroduce the bug).

Conflict-resolution rule if implementations interleave: typed-errors and `/v1/` paths win; this change is updated to follow.

## 0. Plugin — Import & Metadata Endpoints (prerequisite)

> These tasks extend `internal/plugin` from the `multi-chain-kms-gateway` change. Complete those tasks first.

- [ ] 0.1 Add `POST kms/keys/<path>/import` path handler in `internal/plugin/path_keys.go`: validate the path against the shared `ValidateKeyPath` helper FIRST (per `harden-vault-backend` task 1.1-1.3); accept `private_key_hex` field; validate 32-byte secp256k1 scalar; derive EVM address + compressed pubkey; write `KeyEntry{Source: "imported", ImportedAt: now}` to logical storage; zero input bytes via `defer`
- [ ] 0.2 Map "key already exists" (storage entry non-nil) → `logical.CodedError(409, "key already exists at <path>")` so the Vault HTTP response carries StatusCode 409 distinctly. Map invalid scalar / hex / format → HTTP 400 via `logical.ErrInvalidRequest` wrapping the validator message. (Distinct status codes are required for the typed-error mapping in task 2.2.)
- [ ] 0.3 Ensure `GET kms/keys/<path>` response includes `source`, `created_at`, `imported_at` fields (extend existing read handler)
- [ ] 0.4 Write unit tests for import handler: success EVM key, 409 on duplicate path (assert distinct from 400), 400 on invalid hex, 400 on invalid scalar, 400 on malformed key_path (uppercase, `..`, fewer-than-3-segments — inherits the validator from `harden-vault-backend`)
- [ ] 0.5 Extend `vault/init.sh`'s scoped policy (installed in `harden-vault-backend` task 6.2) with `create` capability on `kms/keys/+/import`. Add a smoke test asserting the issued token can import under its own project but NOT under another project (extends `harden-vault-backend` task 6.6).

## 1. Dependencies & Config

- [ ] 1.1 Add `github.com/tyler-smith/go-bip39` to `go.mod` for BIP39 mnemonic entropy and validation
- [ ] 1.2 Add `github.com/btcsuite/btcd/btcutil/hdkeychain` (or `github.com/cosmos/btcutil`) for BIP44 HD key derivation
- [ ] ~~1.3~~ ~~Extend `Config` struct with `MetadataKVMount` field~~ — **removed**: metadata is plugin-native (D9), no KV mount needed
- [ ] 1.4 Update `.env.example` to remove `KMS_METADATA_KV_MOUNT`; note Vault 1.17+ plugin requirement
- [ ] 1.5 Update README: confirm minimum Vault version is 1.17 (plugin SDK requirement), document new Vault policy path (`kms/keys/+/import`)

## 2. Vault Client — Key Import (Plugin Direct Import)

> Replaces Transit wrapping flow (original D7, now superseded).

- [ ] 2.1 Add `ImportKey(ctx context.Context, path string, rawKeyBytes []byte) error` to `vault.Client` — POST to `kms/keys/<path>/import` with `{"private_key_hex": hex}`; zero `rawKeyBytes` via `defer`
- [ ] 2.2 Map 409 via the typed-error pattern from `harden-vault-backend` task 2.x: `errors.As(err, &rerr)` against `*vaultapi.ResponseError`; on `rerr.StatusCode == 409` return `types.ErrKeyExists`. Add `ErrKeyExists` sentinel to `pkg/types/errors.go` alongside `ErrPermission`/`ErrNotFound`/`ErrBadRequest`. SHALL NOT substring-match `err.Error()` (the legacy anti-pattern removed by `harden-vault-backend` task 2.3).
- [ ] 2.3 Write unit tests for `ImportKey` using mock HTTP server returning bodies that do NOT contain the literal string "key already exists" — assert `errors.Is(err, types.ErrKeyExists)` is true based on the 409 status alone. Cover: success, 409 (typed), 403 (`ErrPermission`), 400 (`ErrBadRequest`).

## 3. Key Metadata — Plugin-Native (No KV mount)

> D9 revised: metadata is stored inside the plugin's `KeyEntry`. No separate Vault KV tasks needed.

- [ ] 3.1 Extend `KeyInfo` struct in `pkg/types` with `Source string`, `ImportedAt *time.Time` fields
- [ ] 3.2 Update `vault.Client.GetPublicKey` (renamed to `GetKeyInfo`) to parse `source` and `imported_at` fields from plugin response and populate `KeyInfo`
- [ ] 3.3 Write unit tests for enriched `GetKeyInfo` response (generated key, imported key with timestamps)

## 4. BIP39/BIP44 Derivation (Cosmos Mnemonic Import)

- [ ] 4.1 Implement `DeriveCosmosPrvKey(mnemonic, derivationPath string) ([]byte, error)` — validate BIP39 mnemonic, PBKDF2 seed, BIP44 HD derivation, return 32-byte secp256k1 private key; zero intermediate key material
- [ ] 4.2 Add mnemonic validation: check word count (12 or 24), validate against BIP39 wordlist, return descriptive error for invalid input
- [ ] 4.3 Add derivation path parser: validate BIP44 format `m/44'/…`, return error for malformed paths
- [ ] 4.4 Add default derivation path constant: `m/44'/118'/0'/0/0`
- [ ] 4.5 Write unit tests with known BIP39 test vectors (mnemonic → expected secp256k1 private key → expected Cosmos address)

## 5. Key Import — Internal Package

- [ ] 5.1 Implement `ImportEVMKey(ctx, path, privateKeyHex string) (KeyInfo, error)` in `internal/vault` — parse hex, validate scalar, call `ImportKey`, call `GetKeyInfo`, return populated `KeyInfo`; no separate metadata write needed
- [ ] 5.2 Implement `ImportCosmosKey(ctx, path, mnemonic, derivationPath, hrp string) (KeyInfo, error)` — call `DeriveCosmosPrvKey`, call `ImportKey`, call `GetKeyInfo`, return `KeyInfo` with Cosmos bech32 address derived from `compressed_pub_key`; zero all intermediate key bytes
- [ ] 5.3 Write integration-style unit tests for both import paths using mock Vault server (mock plugin import + GetKeyInfo endpoints)

## 6. REST Gateway — Key Import Endpoint

- [ ] 6.1 Register the handler at `/v1/keys/import` AND `/keys/import` via the dual-mount slice introduced by `polish-api-correctness` task 4.1-4.2; bare path inherits the `Deprecation`/`Sunset` middleware from task 4.4 of that change.
- [ ] 6.2 Handler logic — route on `chain` field to `ImportEVMKey` or `ImportCosmosKey`; validate all required fields; map `types.ErrKeyExists` → HTTP 409; map `types.ErrPermission` → HTTP 403; map `types.ErrBadRequest` → HTTP 400 (all via the typed-error sentinels from `harden-vault-backend`).
- [ ] 6.3 On first-time successful import, set `w.WriteHeader(http.StatusCreated)` (HTTP 201). The `already_existed` pattern from `/keys` POST does NOT apply here — import is not idempotent.
- [ ] 6.4 Add request sanitisation: ensure `private_key` and `mnemonic` fields are never written to logs (redact in middleware if structured logging middleware touches body; ensure the request-ID middleware from `add-observability-and-ops` does not log bodies).
- [ ] 6.5 Write handler unit tests using `httptest` (mock vault backend): success EVM returns 201, success Cosmos returns 201, missing fields → 400, duplicate key → 409 (typed mapping), invalid hex → 400, invalid mnemonic → 400, 403 from Vault → 403 at gateway.

## 7. REST Gateway — Cosmos Partial Sign Endpoint

- [ ] 7.1 Register the handler at `/v1/sign/cosmos/partial` AND `/sign/cosmos/partial` via the dual-mount slice from `polish-api-correctness`.
- [ ] 7.2 Implement handler — parse request, validate `signer_index` bounds and pubkey match, dispatch to Cosmos signer (which uses `cosmos-sdk/types.SortJSON` per the `cosmos-signer` capability updated by `polish-api-correctness`), return `SignatureV2` + `pub_key` + `signer_index`.
- [ ] 7.3 Add `signer_index` validation: out-of-range → HTTP 400; gateway pubkey ≠ multisig_pubkeys[signer_index] → HTTP 400.
- [ ] 7.4 Write handler unit tests: success DIRECT, success AMINO (with `cosmos-sdk/types.SortJSON` canonicalisation), AMINO with duplicate keys → 400, index mismatch, pubkey mismatch, invalid sign_doc, missing fields.

## 8. REST Gateway — EVM Safe Sign Endpoint

- [ ] 8.1 Register the handler at `/v1/sign/evm/safe` AND `/sign/evm/safe` via the dual-mount slice from `polish-api-correctness`.
- [ ] 8.2 Implement handler — parse and validate 32-byte `safe_tx_hash`, call EVM signer's `SignEIP712Digest` (which uses the pubkey cache from `harden-vault-backend` task 4.x), return `signature` + `signer_address`.
- [ ] 8.3 Implement audit log emission: structured `info` log with `key_path`, `signer_address`, `safe_tx_hash`, `request_id` (from `add-observability-and-ops` middleware), timestamp; on error emit `warn`/`error` with `error` field and the same `request_id`.
- [ ] 8.4 Write handler unit tests: success (assert `request_id` field present in captured log), invalid hash length, non-hex hash, missing fields, Vault error (assert typed-error mapping produces correct status).

## 9. CLI — `keys import` Subcommand

- [ ] 9.1 Implement `kms-wrapper keys import` cobra command with flags: `--path`, `--chain`, `--private-key`, `--mnemonic`, `--derivation-path` (default `m/44'/118'/0'/0/0`), `--hrp`, `--yes`
- [ ] 9.2 Add mutex validation: `--private-key` requires `chain=evm`; `--mnemonic` requires `chain=cosmos`; exactly one of `--private-key` or `--mnemonic` must be provided
- [ ] 9.3 Implement interactive address-confirmation prompt for Cosmos import (skip if `--yes` or non-TTY stdin)
- [ ] 9.4 Print mnemonic shell-history warning in help text: "Avoid passing --mnemonic inline; use: kms-wrapper keys import --mnemonic \"$(cat -)\" or env injection"
- [ ] 9.5 Write CLI smoke tests: success EVM import, success Cosmos import with `--yes`, confirmation rejection, missing flags

## 10. CLI — `sign cosmos partial` and `sign evm safe` Subcommands

- [ ] 10.1 Implement `kms-wrapper sign cosmos partial` cobra command with flags: `--path`, `--mode`, `--sign-doc`, `--signer-index`, `--multisig-pubkeys` (comma-separated base64), `--threshold`. CRITICAL: use a single outer-scope `var err error` declared BEFORE any `switch` on sign mode. Inner blocks SHALL use distinctly-named locals for decode errors (e.g. `decErr`). This avoids the err-shadowing bug fixed in `polish-api-correctness` task 1.1.
- [ ] 10.2 Implement `kms-wrapper sign evm safe` cobra command with flags: `--path`, `--safe-tx-hash`. Same outer-scope `err` discipline as 10.1.
- [ ] 10.3 Wire both subcommands to call REST gateway endpoints (or internal packages directly in CLI mode, consistent with existing `sign` subcommands).
- [ ] 10.4 Write CLI smoke tests for both subcommands using a fake signer that returns errors — assert non-zero exit code AND empty stdout on the error path (regression test for the err-shadowing pattern). Also: success, missing flags, invalid input.

## 11. Vault Policy & Documentation

- [ ] 11.1 Extend the project policy template from `harden-vault-backend` task 6.1 — add `path "kms/keys/+/import" { capabilities = ["create"] }`. The `vault/init.sh` smoke test from `harden-vault-backend` 6.6 SHALL be extended to assert import-positive and import-cross-project-negative cases.
- [ ] 11.2 Document the key import CLI UX in README: EVM example, Cosmos example, shell history warning.
- [ ] 11.3 Document the Cosmos partial-sign flow in README: who provides signer pubkeys, how caller assembles `MultiSignature`, example with 2-of-3.
- [ ] 11.4 Document the EVM Safe-sign flow: caller computes `safeTxHash` (link to Gnosis Safe SDK), passes to gateway, collects signature for Safe UI or direct contract call.
- [ ] 11.5 Update API documentation with new routes: `POST /v1/keys/import`, `POST /v1/sign/cosmos/partial`, `POST /v1/sign/evm/safe` — request/response schemas, error codes (note 201 on first import, 409 on duplicate). Bare-path aliases get `Deprecation`/`Sunset` headers automatically via the dual-mount middleware.

## 12. E2E Operator Runbook

- [ ] 12.1 Create `docs/e2e-runbook.md` with prerequisites section: Docker 24+, Go 1.22+, `make`, `vault` CLI, `curl` or `httpie`
- [ ] 12.2 Document plugin build and stack startup: `make build-plugin && make dev-up`, expected output from `vault secrets list` and `vault plugin list`
- [ ] 12.3 Document key lifecycle section: `keys create`, `keys show` — include full CLI output examples with EVM address and compressed pubkey
- [ ] 12.4 Document EVM signing flow: raw tx sign, personal_sign, EIP-712 digest — include `curl` request and how to verify the recovered address matches the key
- [ ] 12.5 Document Cosmos signing flow: DIRECT and AMINO_JSON modes — include `curl` request, base64-encoded sign doc example, how to verify signature using `cosmjs`
- [ ] 12.6 Document EVM key import flow: `kms-wrapper keys import --chain evm --private-key <hex>` — include before/after `keys show` output confirming `source: imported`
- [ ] 12.7 Document Cosmos mnemonic import flow: stdin pipe pattern (`--mnemonic "$(cat -)"`), interactive address confirmation, `--yes` flag for non-interactive use
- [ ] 12.8 Document Cosmos partial multisig flow: 2-of-3 example — gateway signs one share, caller provides other signers' pubkeys, show how to assemble `MultiSignature` with `cosmjs`
- [ ] 12.9 Document EVM Gnosis Safe sign flow: how caller computes `safeTxHash` using `@safe-global/protocol-kit`, sends to `/sign/evm/safe`, submits signature to Safe UI
- [ ] 12.10 Add "Verify your setup" checklist at the end: 10 shell commands an operator can run to confirm each capability is working end-to-end
