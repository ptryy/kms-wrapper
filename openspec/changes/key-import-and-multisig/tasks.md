## 1. Dependencies & Config

- [ ] 1.1 Add `tyler-smith/go-bip39` to `go.mod` for BIP39 mnemonic entropy and validation
- [ ] 1.2 Add `btcsuite/btcd/hdkeychain` (or `cosmos/btcutil`) for BIP44 HD key derivation
- [ ] 1.3 Extend `Config` struct with `MetadataKVMount` field (default `secret`), wired to `KMS_METADATA_KV_MOUNT` env var
- [ ] 1.4 Update `.env.example` with `KMS_METADATA_KV_MOUNT` and note on Vault 1.11+ requirement
- [ ] 1.5 Update README: bump minimum Vault version to 1.11, document new Vault policy paths (`transit/wrapping_key`, `transit/import/<path>`, `secret/kms-metadata/*`)

## 2. Vault Backend — Key Import (Transit Wrapping)

- [ ] 2.1 Implement `GetWrappingKey() (*rsa.PublicKey, error)` — GET `transit/wrapping_key`, parse PEM, detect 404 → return "Vault 1.11+ required" error
- [ ] 2.2 Implement `ImportKey(path string, rawKeyBytes []byte) error` — RSA-OAEP wrap raw 32-byte key, POST to `transit/import/<path>` with `type=ecdsa-p256k1`; zero `rawKeyBytes` via `defer`
- [ ] 2.3 Add 409-detection in `ImportKey`: map Vault "key already exists" error to `ErrKeyExists` sentinel
- [ ] 2.4 Write unit tests for `GetWrappingKey` and `ImportKey` using mock HTTP server (simulate 404 for old Vault, 403 for policy denial, 409 for existing key)

## 3. Key Metadata (Vault KV)

- [ ] 3.1 Implement `WriteKeyMetadata(path, source string) error` — write KV v2 entry at `<mount>/kms-metadata/<path>` with `source`, `chain`, `imported_at`/`created_at`
- [ ] 3.2 Make metadata write non-fatal: on error, log `WARN` and return nil to caller
- [ ] 3.3 Extend `GetPublicKey` flow to also read KV metadata and return it as part of `KeyInfo` struct (`Source`, `ImportedAt` fields)
- [ ] 3.4 Write unit tests for metadata write and read (mock KV v2 responses)

## 4. BIP39/BIP44 Derivation (Cosmos Mnemonic Import)

- [ ] 4.1 Implement `DeriveCosmosPrvKey(mnemonic, derivationPath string) ([]byte, error)` — validate BIP39 mnemonic, PBKDF2 seed, BIP44 HD derivation, return 32-byte secp256k1 private key; zero intermediate key material
- [ ] 4.2 Add mnemonic validation: check word count (12 or 24), validate against BIP39 wordlist, return descriptive error for invalid input
- [ ] 4.3 Add derivation path parser: validate BIP44 format `m/44'/…`, return error for malformed paths
- [ ] 4.4 Add default derivation path constant: `m/44'/118'/0'/0/0`
- [ ] 4.5 Write unit tests with known BIP39 test vectors (mnemonic → expected secp256k1 private key → expected Cosmos address)

## 5. Key Import — Internal Package

- [ ] 5.1 Implement `ImportEVMKey(path, privateKeyHex string) (KeyInfo, error)` in `internal/vault` — parse hex, validate scalar, call `ImportKey`, write metadata, return `KeyInfo` with EVM address
- [ ] 5.2 Implement `ImportCosmosKey(path, mnemonic, derivationPath, hrp string) (KeyInfo, error)` — call `DeriveCosmosPrvKey`, call `ImportKey`, write metadata, return `KeyInfo` with Cosmos bech32 address; zero all intermediate key bytes
- [ ] 5.3 Write integration-style unit tests for both import paths using mock Vault server

## 6. REST Gateway — Key Import Endpoint

- [ ] 6.1 Implement `POST /keys/import` handler — route on `chain` field to `ImportEVMKey` or `ImportCosmosKey`; validate all required fields; map `ErrKeyExists` → HTTP 409
- [ ] 6.2 Add request sanitisation: ensure `private_key` and `mnemonic` fields are never written to logs (redact in middleware if structured logging middleware touches body)
- [ ] 6.3 Write handler unit tests using `httptest` (mock vault backend): success EVM, success Cosmos, missing fields, duplicate key, invalid hex, invalid mnemonic

## 7. REST Gateway — Cosmos Partial Sign Endpoint

- [ ] 7.1 Implement `POST /sign/cosmos/partial` handler — parse request, validate `signer_index` bounds and pubkey match, dispatch to Cosmos signer, return `SignatureV2` + `pub_key` + `signer_index`
- [ ] 7.2 Add `signer_index` validation: out-of-range → HTTP 400; gateway pubkey ≠ multisig_pubkeys[signer_index] → HTTP 400
- [ ] 7.3 Write handler unit tests: success DIRECT, success AMINO, index mismatch, pubkey mismatch, invalid sign_doc, missing fields

## 8. REST Gateway — EVM Safe Sign Endpoint

- [ ] 8.1 Implement `POST /sign/evm/safe` handler — parse and validate 32-byte `safe_tx_hash`, call EVM signer's `SignEIP712Digest`, return `signature` + `signer_address`
- [ ] 8.2 Implement audit log emission: structured `INFO` log with `key_path`, `signer_address`, `safe_tx_hash`, timestamp; on error emit `WARN`/`ERROR` with `error` field
- [ ] 8.3 Write handler unit tests: success, invalid hash length, non-hex hash, missing fields, Vault error

## 9. CLI — `keys import` Subcommand

- [ ] 9.1 Implement `kms-wrapper keys import` cobra command with flags: `--path`, `--chain`, `--private-key`, `--mnemonic`, `--derivation-path` (default `m/44'/118'/0'/0/0`), `--hrp`, `--yes`
- [ ] 9.2 Add mutex validation: `--private-key` requires `chain=evm`; `--mnemonic` requires `chain=cosmos`; exactly one of `--private-key` or `--mnemonic` must be provided
- [ ] 9.3 Implement interactive address-confirmation prompt for Cosmos import (skip if `--yes` or non-TTY stdin)
- [ ] 9.4 Print mnemonic shell-history warning in help text: "Avoid passing --mnemonic inline; use: kms-wrapper keys import --mnemonic \"$(cat -)\" or env injection"
- [ ] 9.5 Write CLI smoke tests: success EVM import, success Cosmos import with `--yes`, confirmation rejection, missing flags

## 10. CLI — `sign cosmos partial` and `sign evm safe` Subcommands

- [ ] 10.1 Implement `kms-wrapper sign cosmos partial` cobra command with flags: `--path`, `--mode`, `--sign-doc`, `--signer-index`, `--multisig-pubkeys` (comma-separated base64), `--threshold`
- [ ] 10.2 Implement `kms-wrapper sign evm safe` cobra command with flags: `--path`, `--safe-tx-hash`
- [ ] 10.3 Wire both subcommands to call REST gateway endpoints (or internal packages directly in CLI mode, consistent with existing `sign` subcommands)
- [ ] 10.4 Write CLI smoke tests for both subcommands: success, missing flags, invalid input

## 11. Vault Policy & Documentation

- [ ] 11.1 Add sample Vault policy HCL snippet for import-capable `proj-a` token: include `transit/wrapping_key` (read), `transit/import/proj-a/*` (create, update), `secret/kms-metadata/proj-a/*` (create, update, read)
- [ ] 11.2 Document the key import CLI UX in README: EVM example, Cosmos example, shell history warning
- [ ] 11.3 Document the Cosmos partial-sign flow in README: who provides signer pubkeys, how caller assembles `MultiSignature`, example with 2-of-3
- [ ] 11.4 Document the EVM Safe-sign flow: caller computes `safeTxHash` (link to Gnosis Safe SDK), passes to gateway, collects signature for Safe UI or direct contract call
- [ ] 11.5 Update API documentation with new routes: `POST /keys/import`, `POST /sign/cosmos/partial`, `POST /sign/evm/safe` — request/response schemas, error codes
