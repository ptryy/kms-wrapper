# kms-wrapper — Project Constitution

> **Master Project Vision** for the Superpowers workflow. Synthesized 2026-06-17 from `README.md`,
> the seven OpenSpec capability specs (`openspec/specs/`), and the change history during the
> OpenSpec → Superpowers migration. This document is the durable source of truth for *what the
> project is and the rules it holds itself to*. Per-feature designs live under
> `docs/superpowers/specs/` and plans under `docs/superpowers/plans/`; the current system's behavioral
> contract is `docs/specs/`; legacy OpenSpec material is archived under `docs/archive/`.

## 1. Purpose

`kms-wrapper` is a Go **CLI + REST gateway** for **multi-chain (EVM + Cosmos) transaction signing**
backed by HashiCorp Vault. Keys are a single `secp256k1` keypair managed by a purpose-built Vault
secrets plugin, **`kms-vault-plugin`**. **Private key material never leaves the Vault process
boundary** — the gateway computes the chain-appropriate digest and the plugin signs a pre-hashed
32-byte input with no internal hashing.

**Why a custom plugin (not Vault Transit):**
- Vault OSS Transit does **not** support `secp256k1` (no `ecdsa-p256k1` key type in OSS).
- `vault-ethereum` keccak256-hashes internally (incompatible with Cosmos SHA-256) and hides raw
  compressed pubkeys.
- `kms-vault-plugin` signs **pre-hashed 32-byte inputs** so the gateway owns digesting:
  keccak256 for EVM, SHA-256 for Cosmos.

## 2. Tech Stack

- **Language:** Go 1.25+
- **Secrets backend:** HashiCorp Vault 1.17+ via the custom `kms-vault-plugin` (plugin SDK)
- **Crypto:** `secp256k1`; EVM keccak256 + EIP-155/EIP-712; Cosmos SHA-256 + `SIGN_MODE_DIRECT` /
  `SIGN_MODE_LEGACY_AMINO_JSON` (canonicalised with cosmos-sdk `types.SortJSON`)
- **HTTP:** stdlib mux + middleware (auth, rate-limit, request-id, panic-recovery)
- **Config:** Viper — precedence **defaults → config file (optional) → env overrides**
- **Docs:** `swaggo/swag` v2 → OpenAPI 3.0, committed under `docs/`, post-processed by
  `cmd/swagger-postprocess`
- **Observability:** Prometheus `/metrics`, structured `slog`, `/livez` + `/readyz` probes
- **Local dev:** Docker Compose + `make` targets (`build-plugin`, `dev-up`, `dev-down`, `swagger`,
  `swagger-check`)

## 3. Capability Map (current specs)

| Capability | Scope |
|------------|-------|
| `key-path-policy` | `{project}/{chain}/{username}` validation, chain conventions, uniqueness, multi-tenant Vault policy, plugin-side independent validation |
| `vault-backend` | Token auth + weak-token guards, key create (idempotent), pubkey retrieval (cached), pre-hashed signing, typed error mapping, observable TTL-adaptive renewal |
| `evm-signer` | Raw tx (RLP + EIP-155), personal_message, EIP-712 digest, EIP-55 address derivation |
| `cosmos-signer` | DIRECT + AMINO_JSON signing, bech32 address derivation, compressed pubkey export |
| `rest-gateway` | Bearer auth, `/sign/*`, `/keys*`, `/v1/` dual-mount, rate limiting, probes, metrics, Swagger surface, structured errors |
| `cli` | `serve`, `keys create/show`, `sign evm/cosmos`, `health`, optional config fallback, Swagger toggles |
| `api-docs` | OpenAPI 3.0 generated from annotations, CI sync check, EVM `oneOf` discriminator, `/v1/` paths |

## 4. Architecture Principles (non-negotiable)

1. **Private key material never leaves Vault.** The gateway and CLI only ever hold public keys,
   digests, and signatures. Mnemonics/private keys (import paths) exist in memory only for the
   duration of the request to the plugin — never logged, never persisted outside Vault.
2. **Gateway owns digesting; plugin signs pre-hashed bytes.** No double-hashing.
3. **Defense in depth at trust boundaries.** Key-path format and authorization are validated
   *independently* at the gateway, the CLI, and the plugin — the plugin never assumes a caller
   pre-validated input.
4. **Idempotent key lifecycle.** Re-creating a key returns the existing one; deletion is
   intentionally **not** exposed by the gateway (preserves Vault audit log / version history).
5. **Auth everywhere except probes.** All routes require `Authorization: Bearer <KMS_GATEWAY_TOKEN>`
   except `/health`, `/livez`, `/readyz`, `/metrics`. Token comparison is constant-time.
6. **Fail closed on weak config.** Empty/weak gateway and Vault tokens refuse startup (no
   `KMS_DEV` bypass for empty; `KMS_DEV=true` required for known-weak placeholders).
7. **Docs are a generated artifact, not hand-edited.** `make swagger` regenerates; CI
   `swagger-check` fails the build on drift.

## 5. Conventions

- **Code style:** idiomatic Go, `golangci-lint` (`.golangci.yaml`). Errors wrapped with context;
  no err-shadowing that swallows signer failures (CLI must exit non-zero on signer error).
- **Errors:** structured JSON `{"error": "..."}` on every response (including 405 with `Allow`).
  Never leak tokens, key material, or stack traces.
- **Vault error classification:** by typed `*vaultapi.ResponseError.StatusCode` via `errors.As` —
  **never** substring-matching `err.Error()`.
- **Testing:** scenario-driven (every spec requirement has WHEN/THEN scenarios that map to tests
  across `internal/*_test.go`). TDD for new features.
- **Git:** feature branches → PR to `main`. Conventional-commit-style prefixes (`feat:`, `docs:`,
  `fix:`). Branch off `main`; commit/push only when asked.
- **Versioning:** public routes dual-mounted; `/v1/` is primary in the spec, bare paths carry
  `Deprecation`/`Sunset` headers (RFC 8594).

## 6. Constraints

- **Vault OSS HA caveat:** Raft replicates plugin *registration* and *key data* but **not the plugin
  binary** — every node needs an identical binary + matching SHA-256 (bake into a custom Vault
  image). Deferred follow-up (design D2c).
- **Security boundary is the hard line:** any feature that would expose private key material outside
  the Vault process is rejected by design.
- **No breaking changes** to existing endpoints/CLI without an explicit BREAKING-flagged change and
  migration notes.

## 7. ⚠️ Known doc/reality drift (resolve during migration)

The `key-path-policy` and `vault-backend` specs still describe **Vault Transit** (`transit/keys/...`,
`ecdsa-p256k1`) while the README and the live system describe the **custom `kms-vault-plugin`**
(`kms/keys/...`). A prior commit ("align spec/design wording with kms-vault-plugin reality") began
reconciling this. **Superpowers plans derived from these specs MUST use plugin-reality terms
(`kms/...`), not Transit terms.** This is captured so the knowledge is not lost when OpenSpec is retired.

## 8. Roadmap (pending features → Superpowers plans)

Build order respects dependencies. Plans authored under `docs/superpowers/plans/`.

1. **`update-key-path-scheme`** *(BREAKING)* — rename middle path segment `{chain}` → `{environment}`;
   drop the reserved-chain table and the unknown-chain warning. *No dependency.*
2. **`add-key-chain-capability`** — explicit per-key `chains: ["evm"|"cosmos"]` tag, set at create,
   persisted by the plugin, enforced (HTTP 403) at every sign boundary; `PATCH /keys/{path}`
   expand-only. *Depends on #1.*
3. **`key-import-and-multisig`** — `keys import` (EVM raw key + Cosmos mnemonic), `sign cosmos partial`
   (m-of-n), `sign evm safe` (Gnosis Safe). *Upstream deps `harden-vault-backend` + `polish-api-correctness`
   already merged/archived.*
4. **`refactor-swagger-schema-names`** — shorten OpenAPI component prefix; fix the broken EVM
   discriminator mapping. *Independent.*
