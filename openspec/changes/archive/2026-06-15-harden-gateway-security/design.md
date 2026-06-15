## Context

The REST gateway's auth, rate limiting, and trusted-input handling were sized for a single-caller, loopback-only deployment. The current implementation has four issues that begin to bite the moment the gateway is exposed beyond a developer laptop:

1. `s.limiter` is a single process-wide `rate.Limiter`. One client's burst exhausts the budget for every other client.
2. `/health` is unauthenticated and unlimited, and proxies to Vault — a free DoS amplifier.
3. The bearer-token compare via `subtle.ConstantTimeCompare` returns 0 immediately on unequal length, leaking the expected token length via timing.
4. `requestOrigin` trusts `X-Forwarded-Proto` and `Host` unconditionally and reflects them into the served OpenAPI `servers[].url`. Combined with `swagger_auth` defaulting to `false`, anyone reachable can cause Swagger UI to advertise an attacker-controlled origin to subsequent visitors.

A local `.env` file is the standard dev workflow here — gitignored, with `.env.example` as the only tracked template. The risk is that operators following the dev workflow leave weak placeholder values (e.g. `KMS_GATEWAY_TOKEN=dev-token`, `VAULT_TOKEN=root`) in their local `.env`, then carry the same file forward into a production deploy via `git add -f`, zipping the repo, or copying the workflow onto a server. This change makes those weak values fail-closed at startup so a misconfigured `.env` cannot silently become live credentials.

## Goals / Non-Goals

**Goals:**
- One slow or hostile principal cannot starve the signing-rate budget for everyone else.
- Bearer-token compare leaks no length information; auth failures are diagnosable from logs.
- `X-Forwarded-*` is trusted only when a configured upstream proxy says so.
- Swagger surface is auth-gated by default and refuses non-loopback exposure without an explicit dev escape.
- The on-disk `.env` cannot leak a "live-looking" gateway token by accident.

**Non-Goals:**
- mTLS for the gateway listener (deferred; orthogonal).
- OIDC / OAuth2 for gateway auth (out of scope; bearer-token continues to be the auth model).
- Per-key access control on `/keys/*` (still rely on Vault policy, which the `harden-vault-backend` change makes real).
- Switching to a third-party HTTP router; `http.ServeMux` continues.

## Decisions

### D1 — Per-principal limiter via `golang.org/x/time/rate.Limiter` map

**Decision:** Replace the single `s.limiter` with a `principalLimiters struct { mu sync.Mutex; m map[string]*rate.Limiter; rate, burst }`. The key SHALL be `bearerSubject || remoteIP`, where `bearerSubject` is a fingerprint (HMAC-SHA256) of the bearer token, falling back to `r.RemoteAddr`'s IP component if no token (e.g. on `/health`, which still wants a slow-path limiter — see D2). Entries SHALL be evicted on a 5-minute idle window via a periodic sweep.

**Rationale:** A `map[string]*rate.Limiter` is the idiomatic Go pattern (`x/time/rate` README example). 5-minute eviction caps memory at `(active principals)·~200B`. HMAC of the token avoids leaking the token into logs or memory dumps.

**Alternative considered — pure per-IP:** misses behind-proxy clients that share an IP.

**Alternative considered — pure per-token:** allows un-bearer `/health` traffic to bypass; see D2.

### D2 — Slow-path `/health` limiter

**Decision:** `/health` SHALL be subject to a separate, conservative `rate.Limiter` (default 10 rps, burst 5) keyed only on remote IP. Exceeded requests get HTTP 429 with body `{"error": "rate limit exceeded"}`. The result of the underlying Vault health check SHALL also be cached for 1 second to absorb micro-bursts.

**Rationale:** Health is unauthenticated by design (load balancers, K8s probes). The cost of letting it be DoS'd is high (Vault round-trips on every call). 1-second caching is invisible to legitimate probers and absorbs the typical scanner pattern.

### D3 — HMAC token compare with structured 401 logging

**Decision:** Replace the existing compare with:

```go
hmacKey := serverNonce // a 32-byte random value, rotated only at startup
got := hmac.New(sha256.New, hmacKey); got.Write([]byte(suppliedAfterStrippingBearer))
want := hmac.New(sha256.New, hmacKey); want.Write([]byte(cfg.Gateway.Token))
if subtle.ConstantTimeCompare(got.Sum(nil), want.Sum(nil)) != 1 { return 401 }
```

Both digests are 32 bytes, so length inequality is impossible. On every 401 emit a single log line: `slog.WarnContext(ctx, "unauthorized request", "reason", reason)` where `reason` is one of `missing` (no `Authorization` header), `bad-format` (header present but not `Bearer X`), or `mismatch` (digest comparison failed). The token SHALL NOT appear in any log.

**Rationale:** HMAC fixes the length-leak comprehensively (the digest length is constant regardless of input length) without changing the auth contract for callers.

### D4 — Trusted-proxy gate + `gateway.public_url`

**Decision:** New config `gateway.trusted_proxies []string` (CIDR list, default empty) and `gateway.public_url string` (default empty). In `requestOrigin`:

- If `r.RemoteAddr`'s IP matches any CIDR in `trusted_proxies`, honour `X-Forwarded-Proto` and `X-Forwarded-Host` (preferred) / `Host`.
- Otherwise: scheme is `https` if `r.TLS != nil`, else `http`; host is `gateway.public_url`'s host if set, else `r.Host`.

The OpenAPI `servers[].url` reflection logic SHALL use this same resolver.

**Rationale:** Trusting `X-Forwarded-*` unconditionally is the documented attack pattern for proxy header injection. Making it opt-in via CIDR mirrors how mature HTTP frameworks (Caddy, Traefik, Envoy) gate this.

### D5 — Default `swagger_auth=true`; refuse `false` on non-loopback bind

**Decision:** Flip the default of `gateway.swagger_auth` from `false` to `true`. Add a startup check: if `swagger_auth=false` AND the parsed listen address is not a loopback IP (`127.0.0.0/8`, `::1`), refuse to start unless `KMS_DEV=true`.

**Rationale:** A public Swagger UI is a documentation feature, not a security boundary, but pairing public docs with an unauthenticated endpoint surface invites reconnaissance and the header-poisoning vector in D4. Loopback-bind dev keeps zero friction.

### D6 — Weak-token startup guard and `.env` placeholder

**Decision:** In `cmd/kms-wrapper/root.go` (paired with the Vault-token guard from `harden-vault-backend`), refuse to start when `cfg.Gateway.Token` ∈ `{"", "change-me", "dev", "dev-token", "password"}` unless `KMS_DEV=true`. Reset `.env`'s contents to the placeholder values from `.env.example`.

**Rationale:** Weak gateway tokens are the most common deployment footgun. The dev workflow is unaffected (`KMS_DEV=true` covers Docker Compose).

## Risks / Trade-offs

- **Per-principal limiter memory** — under DDoS with many spoofed IPs, the map grows. Mitigation: cap map size at 10k entries with LRU eviction. Acceptable.
- **HMAC nonce regeneration** — restarting the gateway rotates the HMAC key, but expected and supplied tokens are both re-HMAC'd each request, so it does not affect correctness; only an in-flight comparison would see inconsistency, and that window is the duration of a single 32-byte HMAC computation.
- **`X-Forwarded-*` gating breaks deployments behind unconfigured proxies** — first deployment after this change MUST configure `gateway.trusted_proxies`. Document in README.
- **Flipping `swagger_auth=true`** is a behavioural change for existing operators who relied on the public default. Document and mention in the changelog. The `KMS_DEV=true` escape hatch covers local laptops; production must opt in via `swagger_auth: false` explicitly with a loopback bind, or pass a token through Swagger UI.

## Migration Plan

1. Land D1, D2, D3 first (no config schema changes; per-principal limiter rolls out transparently).
2. Land D4 second; add `gateway.trusted_proxies` / `gateway.public_url` to `config.yaml`/`.env.example`. Document required config for proxied deployments.
3. Land D5 + D6 third (default flips). Operators upgrading get a clear startup error if their existing config is now incompatible.
4. Rollback: D4's defaults are backwards-compatible (empty `trusted_proxies` = no forwarded headers honoured, matching the safest interpretation). D5's flip can be reverted by setting `swagger_auth: false` explicitly.

## Open Questions

- Should the per-principal limiter key on token-fingerprint OR IP, or both concatenated? **Proposed answer:** both concatenated (`fingerprint||ip`) so the same token from two IPs gets separate budgets — slightly looser than the strictest reading but resilient to NAT.
- Default rate/burst values for `/health`? **Proposed:** 10 rps, burst 5 — conservative enough to absorb K8s liveness probes at 1-second intervals across multiple pods.
