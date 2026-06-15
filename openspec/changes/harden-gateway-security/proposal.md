## Why

The REST gateway has four authentication, rate-limiting, and trusted-input defects that, in combination, allow a denial-of-service against signing, a phishing vector via a poisoned Swagger origin, and an information leak on bearer-token length. In addition, the local `.env` workflow (the file is gitignored — `.env.example` is the only tracked template) historically carried weak placeholder tokens such as `KMS_GATEWAY_TOKEN=dev-token` and `VAULT_TOKEN=root`; these would silently ship as "production" credentials if an operator copies the dev workflow without rotating values.

## What Changes

- **Per-principal rate limiter.** Replace the single process-wide `rate.Limiter` with a per-key (client IP and/or bearer token) limiter map so one noisy caller cannot starve `/sign/*` for every other tenant.
- **`/health` is rate-limited separately.** Add a slow-path limiter (or short-lived cache) so an unauthenticated hammering of `/health` cannot induce Vault round-trip storms.
- **Constant-time bearer-token comparison.** HMAC both the supplied and expected tokens to a fixed-length digest before comparing, so unequal-length inputs do not short-circuit the `subtle.ConstantTimeCompare` call. Log a structured `reason=` (`missing`, `bad-format`, `mismatch`) on every 401 — without ever logging the supplied token.
- **Trusted-proxy gate on `X-Forwarded-*` headers.** Honour `X-Forwarded-Proto` and `Host` only when the immediate peer is in a configured `gateway.trusted_proxies` CIDR list. Otherwise derive scheme from `r.TLS` and host from a new `gateway.public_url` config field.
- **Swagger auth default flipped + non-loopback guard.** `gateway.swagger_auth` defaults to `true`. The gateway SHALL refuse to start when `swagger_auth=false` AND `gateway.addr` is not a loopback address, unless `KMS_DEV=true`.
- **Weak-token startup guard.** Refuse to start when `gateway.token` is empty, `change-me`, `dev`, `dev-token`, or `password`, unless `KMS_DEV=true`. Pairs with the Vault-token guard in `harden-vault-backend`.
- **Local `.env` scrub helper.** Add `make scrub-env` to reset a developer's local (gitignored) `.env` back to the placeholders from `.env.example` after `make dev-up`, so weak dev tokens don't linger between sessions. `.env` itself is not tracked in version control.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `rest-gateway`: tightens authentication, rate-limiting, and trusted-header handling. Flips `swagger_auth` default and adds startup guards.

## Impact

- `internal/gateway/gateway.go`: per-IP/token limiter map, `/health` slow-path limiter, HMAC token compare, structured 401 logging, trusted-proxy logic in `requestOrigin`.
- `internal/config/config.go`: new fields `gateway.trusted_proxies` (string slice CIDR), `gateway.public_url` (string), flip default of `gateway.swagger_auth` to `true`.
- `cmd/kms-wrapper/root.go`: weak-token guard for `gateway.token`; refuse non-loopback bind with `swagger_auth=false`.
- `.env` (gitignored, local-only): operators reset to placeholders via the new `make scrub-env` target.
- `.env.example` (tracked): no change (already uses `change-me`).
- `Makefile`: add `scrub-env` target.
- `openspec/specs/rest-gateway/spec.md`: requirement updates per delta.
- No REST request/response shape changes; client-side change required only for callers using `X-Forwarded-Proto` to influence the OpenAPI server URL (rare).
