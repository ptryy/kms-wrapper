# Current System Specs (behavioral contract)

These seven capability specs are the **authoritative behavioral contract of the implemented
system** — the WHEN/THEN acceptance scenarios for what `kms-wrapper` does today. They were migrated
verbatim from `openspec/specs/` on 2026-06-17 during the OpenSpec → Superpowers migration.

- `key-path-policy/` — key-path validation, uniqueness, multi-tenant Vault policy, plugin-side validation
- `vault-backend/` — token auth + weak-token guards, key create, pubkey retrieval (cached), pre-hashed signing, typed errors, renewal
- `evm-signer/` — raw tx (EIP-155), personal_message, EIP-712, EIP-55 address
- `cosmos-signer/` — DIRECT + AMINO_JSON signing, bech32 derivation, compressed pubkey export
- `rest-gateway/` — auth, sign/keys routes, `/v1/` dual-mount, rate limiting, probes, metrics, swagger
- `cli/` — serve, keys, sign, health, config fallback, swagger toggles
- `api-docs/` — OpenAPI 3.0 generation, CI sync, EVM discriminator, `/v1/` paths

## Status & relationship to other docs

- The master Project Vision is [`/CONSTITUTION.md`](../../CONSTITUTION.md) (these specs are its detail).
- In-flight feature designs live in [`docs/superpowers/specs/`](../superpowers/specs/); their plans in
  [`docs/superpowers/plans/`](../superpowers/plans/).
- Implemented historical changes are archived in [`docs/archive/`](../archive/).

> ⚠️ **Known drift:** `key-path-policy` and `vault-backend` still describe **Vault Transit**
> (`transit/keys/...`, `ecdsa-p256k1`) in places, while the live system uses the custom
> `kms-vault-plugin` (`kms/...`). See `CONSTITUTION.md §7`. These files are preserved byte-for-byte;
> the Transit→plugin wording update is tracked as part of the `update-key-path-scheme` change
> (`docs/superpowers/plans/2026-06-17-update-key-path-scheme.md`), not edited here.
