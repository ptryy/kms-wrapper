## Why

The current KMS gateway only supports keys generated inside Vault — operators cannot migrate existing EVM private keys or Cosmos wallets (mnemonic-derived) into the system without manual, error-prone steps. Additionally, teams requiring m-of-n signing control (e.g., ops + user co-signing) have no supported path today, forcing unsafe workarounds like key duplication or off-system coordination.

## Dependencies

This change depends on two upstream changes from the deep-review proposals batch:

- `harden-vault-backend` — establishes the typed Vault error pattern (`*vaultapi.ResponseError.StatusCode` via `errors.As`), the scoped Vault policy install in `vault/init.sh`, and the plugin-side key-path validator that the new `/keys/import` write path inherits. Specifically, the "key already exists" 409 mapping defined here uses the typed-error machinery from that change, not substring matching.
- `polish-api-correctness` — establishes the `/v1/` route prefix and route dual-mount pattern, the EVM oneOf discriminator, and the AMINO canonicalisation via `sdk.SortJSON`. All new routes proposed here are specified under `/v1/` and inherit the dual-mount machinery (bare-path aliases get `Deprecation`/`Sunset` headers).

Implementation of this change SHALL NOT begin until both upstream changes are merged. If timelines force interleaving, the conflict-resolution rule is: typed errors and `/v1/` paths win; this change is updated to follow.

## What Changes

- Add `kms-wrapper keys import` CLI command and `POST /v1/keys/import` REST endpoint supporting:
  - EVM raw private key (`--private-key <hex>`) import via the `kms-vault-plugin`'s direct import endpoint.
  - Cosmos seed phrase (`--mnemonic <words> --derivation-path <path>`) import: derive private key locally from BIP39/BIP44 and import into the plugin via the same direct flow. Mnemonic is never stored.
- Add `POST /v1/sign/cosmos/partial` REST endpoint for Cosmos native multisig (m-of-n): gateway signs with its managed key(s) and returns a partial signature; caller owns the full signer set and assembles the final `MultiSignature`.
- Add `POST /v1/sign/evm/safe` REST endpoint for EVM Gnosis Safe multisig: accepts a pre-computed 32-byte `safeTxHash` (EIP-712), signs it, returns a 65-byte signature. No Safe-specific tx construction in the gateway.
- Extend key path convention to tag keys as `imported` vs. `generated` for audit purposes (metadata stored plugin-native alongside the key — see design D9).
- Extend CLI: `kms-wrapper keys import`, `kms-wrapper sign cosmos partial`, `kms-wrapper sign evm safe`.

## Capabilities

### New Capabilities

- `key-import`: Plugin direct-import-based key import for EVM private keys and Cosmos mnemonic-derived keys. Covers CLI UX, REST endpoint, plugin import protocol, and BIP39/BIP44 derivation flow. (Supersedes the original Transit-wrapping design — see design D7.)
- `cosmos-multisig-partial-sign`: Partial signing for Cosmos native m-of-n multisig. Gateway signs with its key and returns `SignatureV2`; caller provides full signer pubkey set and threshold, assembles `MultiSignature`.
- `evm-safe-sign`: Sign Gnosis Safe transaction hash (pre-computed EIP-712 `safeTxHash`) via a dedicated endpoint. Thin wrapper over existing EIP-712 signing, with explicit Safe-oriented request/response schema.

### Modified Capabilities

- `cli`: New subcommands `keys import`, `sign cosmos partial`, `sign evm safe` added to the existing command tree. Subcommands SHALL exit non-zero on signer failure (the err-shadowing pattern fixed in `polish-api-correctness` SHALL NOT be reintroduced in any new subcommand).
- `rest-gateway`: New routes `POST /v1/keys/import`, `POST /v1/sign/cosmos/partial`, `POST /v1/sign/evm/safe` added alongside existing sign endpoints.
- `key-path-policy`: Add `source` and `imported_at` metadata fields to plugin-native `KeyEntry` (no separate KV mount). Path format unchanged. Plugin enforces the `{project}/{chain}/{username}` regex on the `import` write path — inherited from `harden-vault-backend`.

## Impact

- **Dependencies added**: `github.com/tyler-smith/go-bip39` (mnemonic entropy), `github.com/btcsuite/btcutil/hdkeychain` or `github.com/cosmos/btcutil` (BIP44 derivation).
- **Vault version requirement**: Vault 1.17+ (plugin SDK). No bump beyond the existing requirement from `multi-chain-kms-gateway`.
- **New Vault policy capabilities required**: `create` on `kms/keys/+/import`. The scoped policy installed by `harden-vault-backend`'s `vault/init.sh` SHALL be extended with this capability for import-capable tokens. (No `transit/*` paths are involved — the original Transit-wrapping design is superseded by D7.)
- **No breaking changes** to existing endpoints or CLI subcommands.
- **Security boundary maintained**: private key material and mnemonic only exist in memory for the duration of the HTTPS request to the plugin; never logged, never persisted outside Vault's encrypted storage.
