## 1. Per-principal rate limiter

- [ ] 1.1 Introduce a `principalLimiters` type in `internal/gateway/gateway.go` with `mu sync.Mutex`, `m map[string]*limiterEntry{limiter *rate.Limiter; lastSeen time.Time}`, `rate rate.Limit`, `burst int`, `cap int` (10000).
- [ ] 1.2 Add `(p *principalLimiters) get(key string) *rate.Limiter` that returns or constructs an entry; on insert when `len(p.m) >= p.cap`, evict the entry with the oldest `lastSeen`.
- [ ] 1.3 Start a sweeper goroutine in `Server.Run` (or `ListenAndServe`) that every 60s removes entries with `lastSeen` older than 5 minutes; exit on `ctx.Done()`.
- [ ] 1.4 Replace the global `s.limiter` reference in the middleware with `p.get(principalKey(r)).Allow()`. `principalKey(r)` returns `hex(hmac-sha256(serverNonce, token)) || "|" || ipFromRemoteAddr(r)`.
- [ ] 1.5 Add `serverNonce []byte` field on `Server`, populated in `New` via `crypto/rand.Read(32 bytes)`.
- [ ] 1.6 Tests: `TestRateLimitPerPrincipal` proves principal A exhaustion does not affect principal B; `TestRateLimitSameTokenTwoIPs` proves IP-disjoint budgets; `TestRateLimitMapEviction` proves cap and idle eviction.

## 2. Health-endpoint slow-path limiter and result cache

- [ ] 2.1 Add a second `*rate.Limiter` field `healthLimiter` on `Server` (rate 10, burst 5 — configurable via `gateway.health_rate_limit` / `gateway.health_rate_burst`, defaults applied in `internal/config/config.go`).
- [ ] 2.2 In the `/health` handler, take the limiter per remote IP using the same map pattern as D1 (separate `principalLimiters` instance keyed only on IP).
- [ ] 2.3 Add a 1-second response cache: store the last `(status, body, expiresAt)` under a mutex; on cache hit, serve directly without calling Vault.
- [ ] 2.4 Test: 30 rapid `GET /health` calls receive at most ~15 200s and the rest 429; underlying `vault.Client.Health` is called at most twice during the burst (cache + once on miss).

## 3. HMAC-based bearer-token compare + structured 401 logs

- [ ] 3.1 In `internal/gateway/gateway.go:auth`, replace the existing `subtle.ConstantTimeCompare(expectedAuth, got)` with: extract the bearer value via `strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")`; compute `hmac.New(sha256.New, s.serverNonce)` over supplied and configured tokens; `subtle.ConstantTimeCompare(got.Sum(nil), want.Sum(nil))`.
- [ ] 3.2 On 401, branch the log reason: header missing → `reason=missing`; CutPrefix `ok=false` or value empty → `reason=bad-format`; HMAC mismatch → `reason=mismatch`. `slog.WarnContext(ctx, "unauthorized request", "reason", reason)`. NEVER log the supplied token.
- [ ] 3.3 Test: `TestAuthLengthLeakResistance` — fire 1,000 requests with random-length wrong tokens; assert min/max response-time spread is within `<10ms` (skip on CI noise but document the manual check). At minimum, assert the 401 log line contains `reason=mismatch` and does NOT contain any substring of the supplied token.

## 4. Trusted-proxy gate

- [ ] 4.1 Add `TrustedProxies []string` and `PublicURL string` fields under `Gateway` in `internal/config/config.go`. Parse CIDRs at config-load time and store as `[]*net.IPNet` on the `Server`; reject malformed CIDRs at startup.
- [ ] 4.2 Replace `requestOrigin` in `internal/gateway/gateway.go` with a function that:
  - returns `cfg.Gateway.PublicURL` if set
  - else, if remote-peer IP is in any trusted CIDR, honour `X-Forwarded-Proto` and `X-Forwarded-Host`
  - else, scheme is `https` if `r.TLS != nil` else `http`; host is `r.Host`
- [ ] 4.3 Apply the same resolver to the OpenAPI `servers[].url` rewrite in the swagger handler.
- [ ] 4.4 Tests: `TestRequestOriginUntrustedPeer` (forwarded headers ignored), `TestRequestOriginTrustedPeer`, `TestRequestOriginPublicURLOverride`.

## 5. Swagger-auth default + non-loopback guard

- [ ] 5.1 In `internal/config/config.go`, change `SwaggerAuth` default from `false` to `true` (look for the place that fills defaults — `viper.SetDefault` or a `defaultConfig()` helper).
- [ ] 5.2 In `cmd/kms-wrapper/root.go` (serveCmd), after config load: parse the listen address; if `swagger_auth=false` AND the address is not loopback (`net.ParseIP(host).IsLoopback()` is false) AND `KMS_DEV != "true"`, exit with the documented error message.
- [ ] 5.3 When `KMS_DEV=true` and the unsafe combo applies, emit one `slog.Warn` line.
- [ ] 5.4 Test: a config with `swagger_auth=false`, `addr=0.0.0.0:8080`, no `KMS_DEV` env produces a startup error matching the spec wording.

## 6. Weak gateway-token startup guard

- [ ] 6.1 In `cmd/kms-wrapper/root.go` (paired with the Vault-token guard from `harden-vault-backend`), add: if `cfg.Gateway.Token` ∈ `{"", "change-me", "dev", "dev-token", "password"}` and `KMS_DEV != "true"`, exit with `"refusing to start with weak gateway token; set KMS_DEV=true for local dev"`.
- [ ] 6.2 Apply to all subcommands that start the gateway (`serve`).
- [ ] 6.3 Test: startup failure on each weak literal; success when `KMS_DEV=true`; success when token is non-weak.

## 7. `.env` cleanup

- [ ] 7.1 Replace contents of `.env` in the repo root with the same placeholder values as `.env.example` (no `dev-token`, no `root` token). Confirm the file remains gitignored.
- [ ] 7.2 Add a Makefile target `make scrub-env` that resets `.env` to placeholder values, for use after `make dev-up`.
- [ ] 7.3 Document the new behavior in `README.md`: developers must `KMS_DEV=true` to run the gateway with placeholder tokens, or follow the new `vault/init.sh` flow to receive a scoped token.

## 8. Verification and archive

- [ ] 8.1 `go test ./...` passes.
- [ ] 8.2 Manual: start gateway with default config bound to `0.0.0.0` — expect refusal (with and without `swagger_auth=false`). Set `KMS_DEV=true` and confirm a warn line. Set `gateway.token=actual-strong-token` and confirm successful start.
- [ ] 8.3 `openspec validate harden-gateway-security --strict` passes.
- [ ] 8.4 Run `openspec archive-change harden-gateway-security` (or `/openspec-archive-change`) once implementation is complete to merge the delta spec into `openspec/specs/rest-gateway/spec.md`.
