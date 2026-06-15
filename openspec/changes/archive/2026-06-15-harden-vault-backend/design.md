## Context

The Vault-backed signing path has three trust boundaries: gateway → vault client → vault plugin. Today only the **gateway** validates key-name format, only the **gateway** maps Vault errors back to HTTP codes via fragile string matching, and only the **gateway** notices when its token can no longer be renewed (and only after a signing failure). The dev `init.sh` registers and mounts the plugin with the **root** token, and `policy-project.hcl` (which would scope the gateway to one project) is never installed — so the documented multi-tenant model is unenforced.

These are the kinds of defects that lie dormant until an operational incident exercises the boundary. The fixes are small and additive; the hard part is making them consistent across plugin, client, and CLI.

## Goals / Non-Goals

**Goals:**
- The Vault plugin enforces key-name format independently of the gateway.
- The deployed gateway runs as a scoped Vault token, not root.
- Vault error classification survives Vault version upgrades and proxy rewording.
- Token renewal failures are visible in logs before they cause sign failures.
- One Vault call per sign, not two, for the steady-state path.

**Non-Goals:**
- Migrating to AppRole, Kubernetes auth, or workload identity (out of scope; the `AuthProvider` interface remains the extension seam).
- Rewriting the stale Transit-era prose in `vault-backend`/`key-path-policy` specs (drift cleanup is a separate change).
- Cache invalidation for the pubkey cache — secp256k1 pubkeys are immutable for a stored private key; no invalidation needed.
- New CLI flags or REST endpoints.

## Decisions

### D1 — Reuse `internal/vault/path.go:ValidateKeyPath` from inside the plugin

**Decision:** Import the existing validator package from the plugin handlers. Both `handleCreateKey` and `handleListKeys` reject names that fail the `{project}/{chain}/{username}` regex with `logical.ErrInvalidRequest` → HTTP 400.

**Rationale:** The validator is already small, well-tested, and the single source of truth. Forking a copy inside `internal/plugin/` would invite drift the moment the gateway loosens or tightens the format. The plugin package already imports `internal/keyinfo` and shared types, so adding `internal/vault` (or pulling the validator into a leaf package) is structurally OK.

**Alternative considered — inline a new regex in the plugin:** rejected; guaranteed to drift.

**Alternative considered — move the validator into `pkg/types`:** acceptable but slightly larger refactor; preferred only if cycles emerge from `internal/plugin` → `internal/vault`. If that import cycle appears at implementation time, lift `ValidateKeyPath` into a new `internal/keypath/` package and re-export from both.

### D2 — Refuse to start on `Vault.Token == "root"` unless `KMS_DEV=true`

**Decision:** In `cmd/kms-wrapper/root.go` (after config load, before `vault.NewClient`), refuse to start when `cfg.Vault.Token` is empty, literally `"root"`, or matches a small known-weak list (`"dev"`, `"dev-token"`, `"change-me"`). Override is `KMS_DEV=true`. Same check applies to `cfg.Gateway.Token` (covered in `harden-gateway-security`).

**Rationale:** The dev environment uses `root` deliberately. Production deployments must not. A single env-gated guard is far cheaper than retrofitting a Vault-auth strategy.

**Alternative considered — refuse only when binding non-loopback:** insufficient; weak tokens behind a corporate proxy are still weak.

### D3 — Bootstrap a scoped token in `vault/init.sh`

**Decision:** After plugin registration, the bootstrap script SHALL:

1. `vault policy write kms-project policy-project.hcl`
2. `vault token create -policy=kms-project -ttl=24h -renewable=true -format=json` → write `KMS_VAULT_TOKEN=...` to `.env`
3. Update `policy-project.hcl` path globs to match the actual plugin mount (`kms/keys/+/*`, `kms/sign/+/*`) rather than the stale `transit/*`.

**Rationale:** A non-root token is meaningless without an installed policy. Issuing the token *and* writing it back to `.env` is what makes the local dev experience work without manual steps.

### D4 — Typed Vault error mapping

**Decision:** `mapVaultErr` becomes:

```go
var rerr *vaultapi.ResponseError
if errors.As(err, &rerr) {
    switch rerr.StatusCode {
    case http.StatusForbidden:   return types.ErrPermission
    case http.StatusNotFound:    return types.ErrNotFound
    case http.StatusBadRequest:  return fmt.Errorf("%w: %s", types.ErrBadRequest, strings.Join(rerr.Errors, "; "))
    }
}
return err
```

The legacy substring matcher is removed entirely. If `errors.As` does not match, the error bubbles up as 500 (same conservative default as today).

**Rationale:** vault/api always wraps non-2xx HTTP responses in `*ResponseError`. The substring path was bug-prone (`"not found"` substring matches a misleading error from a misconfigured proxy) and breaks the moment Vault changes a string.

### D5 — Renewal observability and adaptive cadence

**Decision:** `StartRenewal` SHALL:

- Retry the initial `LookupSelf` with capped exponential backoff (1s, 2s, 4s … up to 30s, then steady) until either it succeeds or `ctx.Done()`.
- Compute tick interval as `max(30s, ttl/3)` from the most recent `LookupSelf`.
- Log every `LookupSelf` and `RenewSelf` error at `warn` with `slog.WarnContext(ctx, "vault token renewal failed", "err", err)`.
- Log successful renewal at `debug` with the new TTL.

**Rationale:** Today's 5-minute tick can be longer than the token TTL; a 60-second TTL with a 300-second tick guarantees expiry. The observability change makes the failure mode self-describing.

### D6 — Public-key cache on `vault.Client`

**Decision:** Add `pubkeyCache sync.Map` (`map[string][]byte`) to `vault.Client`. `GetPublicKey(ctx, path)` reads the cache first; on miss, fetches from the plugin and stores. No invalidation.

**Rationale:** A secp256k1 public key is a deterministic function of its (immutable) private key — the plugin never rotates a key's material in place. Cache lifetime is process lifetime. On bug or compromise scenarios where a key is recreated under a new private key, restart the gateway.

**Risk:** if a future change allows key rotation/replace (e.g., key-import overwrite), this cache becomes incorrect. Mitigation: any future replace operation must be implemented as a new-path import, not in-place mutation — call out in the `key-import-and-multisig` design.

## Risks / Trade-offs

- **Import cycle risk** in D1 — `internal/plugin` importing `internal/vault` may cycle. Fallback: extract validator to `internal/keypath`.
- **Bootstrap-script breakage** in D3 — current developers may have a `.env` with `VAULT_TOKEN=root` cached; the new script overwrites it. Document the migration in the README.
- **D4 may swallow new Vault error types** — typed mapping covers 403/404/400 only. 5xx falls through to generic 500, same as today. Acceptable.
- **Pubkey cache memory** is bounded by the active key set; for tens of thousands of keys, hundreds of KB at most. Not a concern.

## Migration Plan

1. Land D1 + D4 + D5 + D6 first (no infra changes).
2. Land D2 + D3 second (infra changes — requires re-running `make dev-up` to install policy and re-issue token).
3. README diff describes the one-time `make dev-up` re-run and the new `KMS_DEV=true` escape hatch for local Docker.
4. Rollback: revert per-decision; D6's cache is purely additive.

## Open Questions

- Should `KMS_DEV=true` also relax the `policy-project.hcl` requirement (i.e., allow root token in Docker compose)? **Proposed answer:** yes, mention in design but don't gate dev-mode behind policy install. Implementation flips the policy install to a no-op in `KMS_DEV=true` mode.
