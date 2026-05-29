## Why

The current KMS gateway only supports keys generated inside Vault — operators cannot migrate existing EVM private keys or Cosmos wallets (mnemonic-derived) into the system without manual, error-prone steps. Additionally, teams requiring m-of-n signing control (e.g., ops + user co-signing) have no supported path today, forcing unsafe workarounds like key duplication or off-system coordination.

## What Changes

- Add `kms-wrapper keys import` CLI command and `POST /keys/import` REST endpoint supporting:
  - EVM raw private key (`--private-key <hex>`) import via Vault Transit wrapping flow (Vault 1.11+).
  - Cosmos seed phrase (`--mnemonic <words> --derivation-path <path>`) import: derive private key locally from BIP39/BIP44, wrap, and import into Vault Transit. Mnemonic is never stored.
- Add `POST /sign/cosmos/partial` REST endpoint for Cosmos native multisig (m-of-n): gateway signs with its managed key(s) and returns a partial signature; caller owns the full signer set and assembles the final `MultiSignature`.
- Add `POST /sign/evm/safe` REST endpoint for EVM Gnosis Safe multisig: accepts a pre-computed 32-byte `safeTxHash` (EIP-712), signs it, returns a 65-byte signature. No Safe-specific tx construction in the gateway.
- Extend key path convention to tag keys as `imported` vs. `generated` for audit purposes (metadata only, no routing change).
- Extend CLI: `kms-wrapper keys import`, `kms-wrapper sign cosmos partial`, `kms-wrapper sign evm safe`.

## Capabilities

### New Capabilities

- `key-import`: Vault Transit wrapping-based key import for EVM private keys and Cosmos mnemonic-derived keys. Covers CLI UX, REST endpoint, wrapping protocol, and BIP39/BIP44 derivation flow.
- `cosmos-multisig-partial-sign`: Partial signing for Cosmos native m-of-n multisig. Gateway signs with its key and returns `SignatureV2`; caller provides full signer pubkey set and threshold, assembles `MultiSignature`.
- `evm-safe-sign`: Sign Gnosis Safe transaction hash (pre-computed EIP-712 `safeTxHash`) via a dedicated endpoint. Thin wrapper over existing EIP-712 signing, with explicit Safe-oriented request/response schema.

### Modified Capabilities

- `cli`: New subcommands `keys import`, `sign cosmos partial`, `sign evm safe` added to the existing command tree.
- `rest-gateway`: New routes `POST /keys/import`, `POST /sign/cosmos/partial`, `POST /sign/evm/safe` added alongside existing sign endpoints.
- `key-path-policy`: Add optional `imported` metadata tag to key records. Path format unchanged; import status tracked in Vault KV metadata alongside the Transit key.

## Impact

- **Dependencies added**: `tyler-smith/go-bip39` (mnemonic entropy), `btcsuite/btcutil/hdkeychain` or `cosmos/go-bip44` (BIP44 derivation).
- **Vault version requirement**: Minimum bumped from 1.10 to **1.11** (Transit key import API).
- **New Vault policy capabilities required**: `transit/wrapping_key` (GET) and `transit/import/<path>` (POST/UPDATE) — existing token policies must be updated.
- **No breaking changes** to existing endpoints or CLI subcommands.
- **Security boundary maintained**: private key material and mnemonic only exist in memory for the duration of the wrapping operation; never logged, never persisted outside Vault.
