# Design: add-key-chain-capability

**Date:** 2026-06-17
**Status:** Approved (migrated from legacy OpenSpec change `add-key-chain-capability`, hardened for resilience/stability)
**Governing doc:** [`/CONSTITUTION.md`](../../../CONSTITUTION.md)
**Depends on:** `update-key-path-scheme` (new `{project}/{environment}/{username}` path shape) — must land first.

## Goal

Introduce an explicit, persisted, plugin-enforced per-key `chains` capability tag
(`["evm" | "cosmos", ...]`), set at create, enforced with HTTP 403 at every sign boundary, and
expandable (never shrinkable) via `PATCH /keys/{path}`. The tag is documentation **and** enforcement.

## Why

After `update-key-path-scheme`, the path no longer hints at a chain, yet every key is a single
secp256k1 keypair both `/sign/evm` and `/sign/cosmos` accept. There is no signal of which chain a
key is *meant* for. This change adds real, enforced intent: a `["evm"]` key refuses `/sign/cosmos`
with 403 at both the gateway (fast-fail) and the plugin (trust boundary).

## Adopted legacy decisions (D1–D7, verbatim intent)

- **D1** `chains` required at create; missing/empty → HTTP 400. No permissive default.
- **D2** HTTP 403 on chain mismatch at sign time; body names key, attempted chain, allowed chains.
- **D3** Plugin AND gateway both enforce. Plugin is authoritative; gateway is fast-fail.
- **D4** `KeyEntry.Chains []string` persisted canonical (lowercase, dedupe, sort); closed set
  `{"evm","cosmos"}`; unknown → 400.
- **D5** Expand-only `PATCH /keys/{path}` `{"add_chains":[...]}`; any shrink/replace → 400.
- **D6** Response shape mirrors tag: `evm_address`/`cosmos_address` present iff the chain is enabled;
  `public_key_hex` always present; `chains` always present.
- **D7** CLI: `keys create --chains evm,cosmos` (required); `keys update-chains --path --add`.
- Legacy entries with no persisted tag are treated as `Chains=[]` and **fail closed**.

## Resilience & stability upgrades (this migration's additions)

### R-A — TTL'd chains cache + re-validate-on-deny
The gateway caches a key's `chains` tag with a short TTL (default **30s**, configurable
`gateway.chains_cache_ttl`, env `KMS_GATEWAY_CHAINS_CACHE_TTL`). This is a separate TTL'd cache, NOT
the process-lifetime `GetPublicKey` cache (which never expires and would make PATCH expansion
invisible). On a cache hit that *would* deny (attempted chain not in cached tag), the gateway
performs **one authoritative re-fetch from the plugin before returning 403**. Rationale: denials are
the rare/cold path (misrouting), so the extra round-trip is cheap, and a freshly-`PATCH`ed key is
never falsely denied — even across replicas where another replica did the PATCH. The allow path
stays cache-fast.

### R-B — Fail-safe on lookup errors (never fail-open)
If the chains lookup itself returns a transient error (not a clean "key not found"), the gateway
returns **HTTP 503 `{"error": "chain authorization unavailable"}`** (retryable) and **does NOT**
dispatch to the signer. A transient Vault error can never produce an unauthorized signature, and the
client retries instead of receiving a misleading permanent 403. (A clean not-found still maps to the
existing 404/500 sign error path.)

### R-C — Per-key serialized update in the plugin
The plugin `update-chains` read-modify-write (read existing → union with `add_chains` → write) runs
under the plugin's per-key lock (`b.Lock(name)` / storage lock) so concurrent `PATCH`es cannot lose
an update. Idempotent no-op (nothing new to add) skips the write entirely.

### R-D — Resilient list endpoint
`GET /keys` reads each entry's tag with **bounded concurrency** (default 8) and a per-entry timeout.
If an individual entry's tag read fails, that entry returns `"chains": null` (documented "unknown"
marker) instead of failing the whole page. A single slow/broken key never takes down the list.

### R-E — Distinct denial metric
Add `kms_chain_authz_denials_total{chain}` (counter), incremented on every chain-mismatch 403
(gateway side). Separate from `kms_http_requests_total` 401s so a misrouting storm is dashboard-visible.
Resolves the legacy "Open Question" rather than deferring it.

## Goals / Non-Goals

- **Goals:** explicit intent at create; trust-boundary + fast-fail enforcement; monotonic
  (expand-only) mutability; response mirrors capability; resilient under transient faults, concurrent
  updates, and large lists.
- **Non-Goals:** per-call chain override; per-principal/token chain roles (the tag lives on the key);
  in-place migration of existing keys (recreate at same path with explicit `chains`); cross-replica
  cache invalidation (R-A's re-validate-on-deny makes it unnecessary pre-production).

## Component map

| Layer | File(s) | Change |
|-------|---------|--------|
| Types | `pkg/types/types.go` | `Chain` enum + `ParseChains`; `Chains` on create req/resp, `KeyInfo`, list entry; `omitempty` addresses; `KeyUpdateChainsRequest` |
| Plugin storage | `internal/plugin/backend.go:24`, `internal/plugin/path_keys.go` | `KeyEntry.Chains`; require+canonicalize at create; mismatch-on-recreate → 400 |
| Plugin update | `internal/plugin/path_keys_update.go` (new) | expand-only `update-chains`, per-key lock (R-C) |
| Plugin sign | `internal/plugin/path_sign.go:37` | required `chain` param; deny via `logical.ErrPermission` → 403; legacy-empty fails closed |
| Vault client | `internal/vault/client.go` | `CreateKey(...,chains)`, `Sign(...,chain)`, new `GetKeyChains`, `UpdateKeyChains` |
| keyinfo | `internal/keyinfo/keyinfo.go:25` | derive only enabled chains' addresses |
| Gateway | `internal/gateway/*.go` | create/show/list thread `chains`; sign fast-fail 403 + R-A/R-B/R-E; new `PATCH /keys/{path}`; R-D list |
| CLI | `cmd/kms-wrapper/...` | `--chains` (required) on create; `update-chains` subcommand |
| Docs | `docs/*` | regenerate via `make swagger` |

## Error handling

- Create: missing/empty/unknown `chains` → 400 (`"chains is required and must be a non-empty subset of [evm, cosmos]"`); idempotent mismatch → 400 (`"chains mismatch on idempotent create"`).
- Sign mismatch → 403 (`"key <path> not authorized for <chain> signing (allowed chains: [<sorted>])"`) + `slog` warn (`key_path`, `attempted_chain`, `allowed_chains`) + `kms_chain_authz_denials_total{chain}++`.
- Sign authz lookup transient error → 503 (R-B), signer NOT called.
- PATCH non-`add_chains` field → 400 (`"only add_chains is supported"`); empty/unknown → 400 (`"add_chains is required and must be a non-empty subset of [evm, cosmos]"`).

## Testing

- Unit: `ParseChains` canonicalization/rejection; plugin create require/mismatch; plugin sign allow/deny/legacy-fails-closed; plugin `update-chains` expand/idempotent/reject + concurrent-PATCH no-lost-update (R-C); keyinfo conditional addresses.
- Gateway: EVM-on-[evm]→200, EVM-on-[cosmos]→403 (exact body), Cosmos-on-[evm]→403, dual→200; PATCH expand then sign succeeds (R-A re-validate); lookup-error→503 (R-B); list with a failing entry → `chains: null` for that entry only (R-D); `kms_chain_authz_denials_total` increments (R-E).
- `make swagger-check` clean (closed-set enum + PATCH op present); `make lint` + `go test ./...` green.
- E2E: create `payment/prod/alice` `chains=[evm]`; sign EVM 200; sign Cosmos 403; PATCH add cosmos; sign Cosmos 200.

## Risks / Trade-offs

- Adding a third chain later touches the closed enum + enforcement points (centralized in `pkg/types` + gateway/plugin sign paths) — deliberate, reviewable.
- Conditional response shape (D6): callers hard-coding `cosmos_address` fail on `[evm]` keys — by design; branch on `chains`.
- R-A adds one Vault round-trip on the deny path only (cold path). R-D adds bounded per-entry reads on list (cost accepted; future optimization is storing chains in list metadata).
- Rollback: revert PR. `KeyEntry` is JSON; an older binary ignores the unknown `chains` field on read (not auto-deleted); key material preserved.

## Constitution alignment

- Plugin remains the authoritative trust boundary; gateway is defense-in-depth fast-fail (§4.3).
- Fail-closed on weak/unknown authorization (§4.6 spirit) — R-B never fail-opens.
- Plugin-reality terms (`kms/...`) throughout (§7). No change to digesting model or private-key boundary (§4.1–§4.2).
