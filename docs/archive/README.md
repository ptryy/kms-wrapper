# Archived Changes — Migrated / Legacy (OpenSpec)

> **Status: `Migrated/Legacy`.** These folders are the historical, **implemented & archived**
> change proposals from the retired OpenSpec workflow. They were moved verbatim from
> `openspec/changes/archive/` on 2026-06-17 during the OpenSpec → Superpowers migration.
> Original folder structure and contents are preserved unchanged for audit/history.
>
> **Do not edit these.** They are a record of decisions already shipped. New work is tracked as
> Superpowers plans under `docs/plans/`, governed by [`/CONSTITUTION.md`](../../CONSTITUTION.md).

Each folder retains its OpenSpec shape: `proposal.md`, `design.md`, `tasks.md`, and capability
`specs/` deltas.

## Index (11 implemented changes)

| Change (legacy) | Summary |
|-----------------|---------|
| `2026-05-30-multi-chain-kms-gateway` | Foundational EVM + Cosmos signing gateway over the custom Vault plugin. |
| `2026-05-30-add-keys-rest-endpoints` | REST `/keys` create/show/list parity with the CLI. |
| `2026-05-30-add-swagger-docs` | OpenAPI 3.0 generation + Swagger UI surface. |
| `2026-05-30-config-file-fallback` | Optional config file with warning fallback to env/defaults. |
| `2026-05-30-fix-keys-api-polish` | Rough-edge fixes on the `/keys` API. |
| `2026-05-30-fix-swagger-runtime-server-url` | Runtime OpenAPI server-URL resolution (vs hard-coded localhost). |
| `2026-05-30-fix-swagger-ui-polish` | Swagger UI polish (root path, assets, auth gate). |
| `2026-06-15-harden-vault-backend` | Typed Vault error mapping, scoped policy, TTL-adaptive renewal. |
| `2026-06-15-harden-gateway-security` | Constant-time auth, per-principal rate limiting, trusted-proxy gate, weak-token guards. |
| `2026-06-15-add-observability-and-ops` | Prometheus metrics, request-ID, `/livez` + `/readyz`, panic recovery. |
| `2026-06-15-polish-api-correctness` | `/v1/` dual-mount, EVM `oneOf` discriminator, AMINO `SortJSON` canonicalisation. |
