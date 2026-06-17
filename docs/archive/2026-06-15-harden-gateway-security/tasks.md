## 1. Per-principal rate limiting

- [x] 1.1 Introduce a `principalLimiters` type in `internal/gateway/gateway.go` with `mu sync.Mutex`, `m map[string]*limiterEntry{limiter *rate.Limiter; lastSeen time.Time}`, `rate rate.Limit`, `burst int`, `cap int` (10000).
- [x] 1.2 Add `(p *principalLimiters) get(key string) *rate.Limiter` that returns or constructs an entry; on insert when `len(p.m) >= p.cap`, evict the entry with the oldest `lastSeen`.
- [x] 1.3 Start a sweeper goroutine in `Server.Run` (or `ListenAndServe`) that every 60s removes entries with `lastSeen` older than 5 minutes; exit on `ctx.Done()`. (Implemented as `StartLimiterSweeper`, wired from `serveCmd`.)
- [x] 1.4 Replace the global `s.limiter` reference in the middleware with `p.get(principalKey(r)).Allow()`. `principalKey(r)` returns `hex(hmac-sha256(serverNonce, token)) || "|" || ipFromRemoteAddr(r)`.
- [x] 1.5 Add `serverNonce []byte` field on `Server`, populated in `New` via `crypto/rand.Read(32 bytes)`.
- [x] 1.6 Tests: per-principal split, IP-disjoint budgets, map eviction. (Covered in `internal/gateway/security_test.go`.)

## 2. Slow-path `/health` limiter and 1s cache

- [x] 2.1 Health rate/burst defaults via `gateway.health_rate_limit` / `gateway.health_rate_burst`.
- [x] 2.2 `/health` uses a separate `principalLimiters` keyed on IP only.
- [x] 2.3 1-second response cache on /health to absorb micro-bursts.
- [x] 2.4 Test: 30 rapid /health calls produce mixed 200/429 with at most 2 Vault round-trips. (`TestHealthRateLimitedAndCached`.)

## 3. HMAC token compare + structured 401 logging

- [x] 3.1 HMAC-SHA256 compare with server nonce.
- [x] 3.2 Reason logging (`missing`/`bad-format`/`mismatch`). Token never logged.
- [x] 3.3 Tests for reason fields + token-leak resistance.

## 4. Trusted-proxy gate on forwarded headers

- [x] 4.1 `TrustedProxies []string` and `PublicURL string` config; CIDRs parsed at startup.
- [x] 4.2 `resolveOrigin` honours forwarded headers only when peer matches a trusted CIDR.
- [x] 4.3 Swagger doc rewrite uses the same resolver.
- [x] 4.4 Tests covering untrusted peer, trusted peer, public_url override.

## 5. Swagger default + non-loopback startup guard

- [x] 5.1 `SwaggerAuth` default flipped to `true` in config + viper defaults.
- [x] 5.2 `guardSwaggerNonLoopback` refuses non-loopback bind when swagger is unauthenticated.
- [x] 5.3 `KMS_DEV=true` downgrades the refusal to a warn line.
- [x] 5.4 Tests cover loopback / auth-on / refusal / KMS_DEV bypass.

## 6. Weak gateway-token guard

- [x] 6.1 `guardWeakGatewayToken` refuses placeholder literals unless `KMS_DEV=true`.
- [x] 6.2 Wired into `serveCmd`.
- [x] 6.3 Tests in `serve_guards_test.go`.

## 7. `.env` placeholder reset + `make scrub-env`

- [x] 7.1 `make scrub-env` target restores `.env` from `.env.example`.
- [x] 7.2 `.env.example` placeholders no longer contain a live-looking token. README update pending.

## 8. Verification and archive

- [x] 8.1 `go test ./...` passes.
- [ ] 8.2 Manual smoke: refusal of `0.0.0.0` bind without dev mode (deferred).
- [ ] 8.3 `openspec validate harden-gateway-security --strict` (run after all four changes apply).
- [ ] 8.4 `openspec archive-change harden-gateway-security` (pending verification).
