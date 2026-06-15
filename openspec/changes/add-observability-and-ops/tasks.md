## 1. Liveness and readiness endpoints

- [ ] 1.1 In `internal/gateway/gateway.go`, add handlers `handleLivez` (always 200) and `handleReadyz` (checks Vault reachability + recent successful `LookupSelf`).
- [ ] 1.2 Cache the most recent `LookupSelf` timestamp on `Server` (write-side from the renewal goroutine in `internal/vault/client.go`); readiness checks `time.Since(lastLookup) < 30*time.Second`.
- [ ] 1.3 Register both routes in `routes()`. Keep `/health` registered as a route that calls `handleReadyz` AND wraps the response with `Deprecation` and `Sunset` headers (reuse the deprecation middleware from `polish-api-correctness` task 4.4).
- [ ] 1.4 Tests: `TestLivez200Always`, `TestReadyzReady`, `TestReadyzNotReadyVaultDown`, `TestReadyzNotReadyTokenInvalid`, `TestHealthAliasesReadyzWithDeprecation`.

## 2. Prometheus metrics endpoint and collectors

- [ ] 2.1 Add `github.com/prometheus/client_golang` to `go.mod` (`go get` the latest stable). Verify the indirect deps it pulls are acceptable.
- [ ] 2.2 New file `internal/gateway/metrics.go` defining the collectors listed in the proposal (`kms_http_requests_total`, `kms_http_request_duration_seconds`, `kms_signing_duration_seconds`, `kms_rate_limit_rejections_total`, `kms_vault_calls_total`, `kms_vault_call_duration_seconds`, `kms_token_renewal_failures_total`, `kms_panics_total`). Register on a package-level `prometheus.NewRegistry` exposed via `MetricsRegistry()`.
- [ ] 2.3 Mount `promhttp.HandlerFor(metricsRegistry, ...)` at `/metrics` in `routes()`. Apply the same per-IP slow-path limiter as `/health`.
- [ ] 2.4 Instrument request middleware: after the matched route is known, call `kmsHttpRequestsTotal.WithLabelValues(matchedPath, method, statusStr).Inc()` and observe duration on the histogram.
- [ ] 2.5 Instrument signing handlers: wrap the call to the signer in `start := time.Now(); defer kmsSigningDuration.WithLabelValues("evm"|"cosmos").Observe(time.Since(start).Seconds())`.
- [ ] 2.6 Instrument the Vault client (`internal/vault/client.go`): wrap each `Sign`, `Create`, `Read`, `List`, `Health` call with `kms_vault_calls_total` increments using the typed-error classification from `harden-vault-backend` (`ok`/`permission_denied`/`not_found`/`error`).
- [ ] 2.7 Wire `kms_token_renewal_failures_total.Inc()` into the renewal goroutine's failure branches (depends on `harden-vault-backend` task 3.3).
- [ ] 2.8 Tests: `TestMetricsEndpointExposesCollectors` (parse the response, assert all metric names present), `TestRequestMetricIncrementsOnSign`.

## 3. Request-ID middleware

- [ ] 3.1 Add `github.com/google/uuid` to `go.mod` if not already present.
- [ ] 3.2 New file `internal/gateway/requestid.go` with: a context key type `type requestIDKey struct{}`, a `RequestIDFromContext(ctx) string` helper, and a `requestID` middleware that reads `X-Request-ID`, validates against `^[A-Za-z0-9._-]{1,128}$`, generates UUIDv4 on absence/invalid, sets the response header, and stores in context.
- [ ] 3.3 Add a `slog.Handler` wrapper (or use `slog.NewTextHandler`'s `ReplaceAttr`) that reads the context's request ID and emits `request_id=<id>` on every `slog.*Context` call.
- [ ] 3.4 Mount the `requestID` middleware as the **outermost** layer in the chain so the request context carries the ID before any panic can be recovered by `recoverPanic` (see task 4.2). Chain order: `requestID(recoverPanic(...))`.
- [ ] 3.5 Tests: `TestRequestIDPreservesInbound`, `TestRequestIDGeneratesOnMissing`, `TestRequestIDReplacesMalformed`, `TestRequestIDInLogs` (capture slog output to a buffer, assert the ID appears).

## 4. Panic-recovery middleware

- [ ] 4.1 New file `internal/gateway/recover.go` with `recoverPanic(next http.Handler) http.Handler`. Implementation:
  - `defer func() { if rec := recover(); rec != nil { ... } }()`
  - In the recover branch: `slog.ErrorContext(ctx, "panic in handler", "panic", fmt.Sprint(rec), "stack", string(debug.Stack()), "matched_path", matchedPath)`
  - Increment `kmsPanicsTotal.WithLabelValues(matchedPath).Inc()`.
  - Write HTTP 500 with body `{"error": "internal server error", "request_id": "<id>"}`.
- [ ] 4.2 Mount `recoverPanic` **immediately inside** the `requestID` middleware (not outermost). This ensures the recovered request still has the request ID available in its context for inclusion in the 500 response body and the panic log line. Chain order: `requestID(recoverPanic(rest of chain))`.
- [ ] 4.3 Test: `TestPanicRecovery` — register a route that panics; assert response is 500 with the expected body, assert no process crash, assert log line emitted, assert `kms_panics_total` incremented.

## 5. Rate-limit observability

- [ ] 5.1 In the per-principal limiter middleware (added in `harden-gateway-security` task 1), on rejection: `slog.InfoContext(ctx, "rate limit exceeded", "path", matchedPath, "reason", "rate_limited")`.
- [ ] 5.2 Increment `kmsRateLimitRejectionsTotal.WithLabelValues(matchedPath).Inc()` on rejection.
- [ ] 5.3 Same wiring for the `/health` and `/metrics` slow-path limiter.
- [ ] 5.4 Test: `TestRateLimitRejectionLogsInfoNotWarn` — capture slog output, assert at most one `info` line and zero `warn` lines per rejection.

## 6. Lower `go.mod` toolchain floor

- [ ] 6.1 Run `go mod why -m all` and `go mod graph | head -50` to find the highest-required Go floor from any transitive dep.
- [ ] 6.2 Change `go 1.25.9` in `go.mod` to that floor (likely `go 1.23`). Add a `toolchain` directive pinning a specific patch — e.g. `toolchain go1.23.6` (the directive requires a concrete patch version; wildcards like `go1.23.x` are invalid `go.mod` syntax).
- [ ] 6.3 Verify `go build ./...` and `go test ./...` pass against the new floor (use `gotip` or a local 1.23 install).
- [ ] 6.4 Update `.github/workflows/ci.yml` `actions/setup-go` `go-version` field to read from `go.mod` (`go-version-file: go.mod`).
- [ ] 6.5 Update `testing-guide.md` and `README.md` Go-version statements to match the new floor.

## 7. Startup Swagger UI URL log

- [ ] 7.1 In `cmd/kms-wrapper/root.go` `serveCmd` (or in `Server.ListenAndServe`), after listener startup succeeds and BEFORE `srv.Serve(ln)`, compute the UI URL using the same resolver as the OpenAPI server URL (from `harden-gateway-security` task 4.2).
- [ ] 7.2 Emit one `slog.Info("swagger UI", "url", uiURL)` when `cfg.Gateway.SwaggerEnabled` is true. No log when disabled.
- [ ] 7.3 Test: smoke (manual or `TestStartupLogsSwaggerURL` that captures slog output during `Server.New` + `Serve` on a `httptest.NewUnstartedServer`-style harness).

## 8. Re-enable CI triggers

- [ ] 8.1 Edit `.github/workflows/ci.yml`: uncomment `push:` (add `branches: [main, "openspec/**"]`) and `pull_request:` (add `branches: [main]`). Keep `workflow_dispatch:` for manual runs.
- [ ] 8.2 Run the suite once manually before pushing (`act` or via `workflow_dispatch`) to confirm green. If broken, file follow-up tasks per failure — DO NOT silence them.
- [ ] 8.3 Update README's "CI" section (if any) to reflect the new triggers.

## 9. Verification and archive

- [ ] 9.1 `go test ./...` passes locally on the new Go floor.
- [ ] 9.2 Manual: hit `/livez`, `/readyz`, `/health`, `/metrics`. Confirm `Deprecation`+`Sunset` headers on `/health`. Confirm `/metrics` lists all collectors. Confirm a panic-injecting test handler returns 500 with `request_id`.
- [ ] 9.3 `openspec validate add-observability-and-ops --strict` passes.
- [ ] 9.4 Run `openspec archive-change add-observability-and-ops` once implementation is complete to merge the delta into `openspec/specs/rest-gateway/spec.md`.
