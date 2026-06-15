## Context

This proposal adds the operational surface every production HTTP service needs but the gateway currently lacks. The gaps were flagged in the deep review as medium severity, but they compound: a single `/health` endpoint causes liveness/readiness confusion (K8s kills pods on transient Vault blips), no `/metrics` means no SLO measurement, no request-ID means impossible cross-service log correlation, no panic-recovery means one handler bug crashes the process, and the over-specified Go version raises the bar for every contributor and CI runner.

These are deferrable individually; they are cheap together. None require new infrastructure beyond `prometheus/client_golang`.

## Goals / Non-Goals

**Goals:**
- Liveness and readiness are separately observable; readiness includes Vault token validity.
- Standard Prometheus metrics cover request latency, error rate, signing latency, rate-limit pressure, and Vault dependency health.
- Every log line for a given request can be correlated via a request ID.
- A panic in any handler returns 500 to the caller and logs context, without crashing the process.
- The Go toolchain floor matches what the code actually requires, not what one developer happened to have installed.

**Non-Goals:**
- OpenTelemetry tracing (deferred; metrics-first is a deliberate ordering).
- Histograms tuned to specific SLOs — defaults from `client_golang` are fine for v1.
- Structured-log emission to anything other than stdout (no Loki/Splunk shipper, no JSON-vs-text toggle beyond what exists).
- Removing the legacy `/health` alias.

## Decisions

### D1 — `/livez` is process-up, `/readyz` is Vault-reachable + token-valid

**Decision:** `/livez` returns 200 with `{"status": "alive"}` unconditionally (process is up). `/readyz` returns 200 with `{"status": "ready"}` when (a) Vault is reachable AND (b) the most recent `LookupSelf` succeeded (cached, with a 30-second freshness window). Otherwise 503 with `{"status": "not_ready", "reason": "<reason>"}`.

**Rationale:** K8s liveness probe failures restart the pod (last-resort recovery). Readiness probe failures stop traffic routing without restarting. Conflating them — as today's `/health` does — causes pods to thrash during a 30-second Vault blip.

**Alternative considered — single `/health` with verbosity flag:** rejected; convention says split endpoints, and split endpoints map cleanly to K8s probe config.

### D2 — `client_golang` is the metrics dep; `/metrics` is unauthenticated; routed under `/metrics` not `/v1/metrics`

**Decision:** Add `github.com/prometheus/client_golang v1.x` (latest). Mount `promhttp.Handler()` at `/metrics`. The endpoint is unauthenticated by convention (Prometheus scrape configs do not natively support bearer auth without extra config) but is subject to the same per-IP slow-path limiter as `/health` (configurable via `gateway.metrics_rate_limit`). It is NOT mounted under `/v1/` — metrics endpoints are conventionally unversioned.

**Rationale:** Default Prometheus conventions; least friction for the scrape side. Operators who want auth in front of `/metrics` use a sidecar or proxy.

### D3 — Metric set: requests, signing, rate-limit, Vault calls, token renewal

**Decision:** Register these collectors (all in `internal/gateway/metrics.go`):

- `kms_http_requests_total{path,method,status}` (counter)
- `kms_http_request_duration_seconds{path,method}` (histogram, default buckets)
- `kms_signing_duration_seconds{chain}` (histogram) — `chain` ∈ `{"evm","cosmos"}`
- `kms_rate_limit_rejections_total{path}` (counter) — increment when a 429 is returned
- `kms_vault_calls_total{op,status}` (counter) — `op` ∈ `{"create","read","list","sign","health","lookup_self","renew_self"}`; `status` ∈ `{"ok","permission_denied","not_found","error"}`
- `kms_vault_call_duration_seconds{op}` (histogram)
- `kms_token_renewal_failures_total` (counter) — increment in the renewal goroutine on `LookupSelf`/`RenewSelf` failure

**Rationale:** Covers request-side SLOs (latency + error rate), the critical-path signing operation, and the Vault dependency. Token renewal failures are the most insidious failure mode (silent until next sign) and deserve their own counter.

The `path` label uses the matched route pattern (e.g. `/v1/sign/evm`), not the raw URL — cardinality stays bounded.

### D4 — Request-ID propagation via `chi/middleware`-style pattern (but built-in, not the chi dependency)

**Decision:** New middleware `requestID(next http.Handler) http.Handler`:
- Read `X-Request-ID`. If present, validate against `^[A-Za-z0-9._-]{1,128}$`; on validation failure, treat as absent.
- If absent or invalid, generate `uuid.NewV4()` (use `google/uuid` — likely already a transitive dep; if not, add it).
- Echo the (now-canonical) ID back via `w.Header().Set("X-Request-ID", id)`.
- Stuff into context via `ctx = context.WithValue(ctx, requestIDKey{}, id)`.
- Include in every `slog.*Context` call automatically by extending the logger's handler to read `requestIDKey` from context and emit `request_id=<id>`. Implement via a `slog.Handler` wrapper.

**Rationale:** Standard pattern; minimal code; covers the cross-service correlation use case.

### D5 — Panic-recovery middleware

**Decision:** New middleware `recoverPanic(next http.Handler) http.Handler`:
- `defer func() { if r := recover(); r != nil { ... } }()`
- On panic: `slog.ErrorContext(ctx, "panic in handler", "panic", r, "stack", string(debug.Stack()))`. Write HTTP 500 with body `{"error": "internal server error", "request_id": "<id>"}`. Increment `kms_panics_total` counter.
- Mounted **immediately inside** the `requestID` middleware (i.e. `requestID(recoverPanic(...))`) so the request context already carries the request ID when a panic is recovered. The chain order is: `requestID` (outermost) → `recoverPanic` → other middleware → handler.

**Rationale:** A single handler panic should not take down the entire process. The request ID in the error body lets the caller report the failure.

### D6 — Rate-limit rejection observability

**Decision:** When the per-principal limiter (from `harden-gateway-security`) returns false:
- Write HTTP 429.
- `slog.InfoContext(ctx, "rate limit exceeded", "path", matchedRoutePattern, "reason", "rate_limited")` — `info` not `warn` because rate limiting is expected behaviour under load. Use the matched route pattern (e.g. `/v1/sign/evm`), not `r.URL.Path`, to keep log cardinality bounded.
- Increment `kms_rate_limit_rejections_total{path=<matched-pattern>}`.

**Rationale:** `info`-level logging plus counter means 429 storms are visible in Prometheus *without* spamming logs. Operators paging on `kms_rate_limit_rejections_total` rate-of-change is the recommended alert.

### D7 — Lower `go.mod` toolchain floor

**Decision:** Run `go mod why -m all | head -50` to find the highest-floor transitive dep. If it requires `1.23` (cosmos-sdk does, last checked), set `go 1.23` in `go.mod`. Add a `toolchain` directive pinning a concrete patch version (e.g. `toolchain go1.23.6` — the latest 1.23.x patch at merge time) to pin a consistent floor without forcing 1.25. `go.mod`'s `toolchain` directive requires a concrete patch number; wildcards are not valid syntax.

**Rationale:** Today's `1.25.9` excludes contributors and CI images on `1.22`/`1.23` for no benefit. Going to whatever cosmos-sdk needs is the correct floor.

### D8 — Startup Swagger-UI URL log line

**Decision:** In `cmd/kms-wrapper/root.go` `serveCmd`, after `s.ListenAndServe` returns (or in the goroutine that wraps it), log once: `slog.Info("swagger UI", "url", uiURL)` where `uiURL` is derived from `gateway.public_url` (preferred) or the listen address. Emit only when `cfg.Gateway.SwaggerEnabled` is true.

**Rationale:** Operators need this URL daily. Today they have to derive it manually.

### D9 — Re-enable CI triggers

**Decision:** Edit `.github/workflows/ci.yml` to uncomment `push: { branches: [main, "openspec/**"] }` and `pull_request: { branches: [main] }`. Keep `workflow_dispatch` as a manual fallback.

**Rationale:** `workflow_dispatch`-only CI never runs on every push, which means broken commits sit unnoticed. Anyone on the team can land work that breaks `main` until the next manual run.

## Risks / Trade-offs

- **`prometheus/client_golang` is a sizeable dep** (~150 KB compiled). Acceptable; standard.
- **`google/uuid` may not be a current dep.** If pulling it in is undesirable, fall back to `crypto/rand.Read(16)` + hex encoding — slightly less standard but zero new deps.
- **D2 unauthenticated `/metrics`** — exposes timing and cardinality info. Operators who care put the gateway behind a proxy that auth-gates `/metrics`. We do NOT bake metrics auth into the gateway in this change.
- **D7 lowering the floor may surface a previously-hidden dep that needs 1.24+.** Run `make test` against the candidate floor before merging.
- **D9 re-enabling CI** — if the suite is currently broken (it has been since `workflow_dispatch`-only landed), re-enabling will surface that. Run `make ci-local` first.

## Migration Plan

1. Land D1 (livez/readyz split) — keep `/health` as a `/readyz` alias.
2. Land D3 + D2 (metrics collectors + `/metrics` endpoint).
3. Land D4 (request-ID middleware) + D5 (panic-recovery) together — they share the middleware chain.
4. Land D6 (rate-limit observability) — depends on the per-principal limiter from `harden-gateway-security`; this proposal explicitly orders after that one.
5. Land D7 (go.mod floor) — independent of the above.
6. Land D8 + D9 (startup log + CI triggers) — small, independent.

Rollback: revert individual decisions; D2's metrics endpoint can be disabled via a `gateway.metrics_enabled` flag if needed.

## Open Questions

- Should `/metrics` be auth-gated when `gateway.metrics_auth=true` is set? **Proposed:** add the config knob but default false, matching Prometheus convention.
- What histogram buckets for `kms_signing_duration_seconds`? **Proposed:** `client_golang` defaults (5ms to 10s). If signing reliably stays under 200ms, we can tune later from real data.
