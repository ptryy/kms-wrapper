## 1. `/livez` and `/readyz` (+ `/health` alias with Deprecation)

- [x] 1.1 `handleLivez` + `handleReadyz` in `internal/gateway/probes.go`.
- [x] 1.2 `vault.Client.LastLookupSelf` exposes the cached timestamp; `handleReadyz` checks it via the `ReadinessChecker` interface (nil-safe).
- [x] 1.3 `/health` mounted via the bare-path route family so the existing `withDeprecation` middleware applies; the handler aliases `handleReadyz`. (`/health` now returns the `/readyz` shape — the legacy ok/degraded payload is superseded.)
- [x] 1.4 Tests: `TestLivezAlwaysAlive`, `TestReadyzVaultDownReturns503`, `TestReadyzStaleLookupReturns503`, `TestReadyzFreshLookupReturns200`, `TestHealthAliasIncludesDeprecationHeaders`.

## 2. Prometheus `/metrics`

- [x] 2.1 `github.com/prometheus/client_golang@v1.23.2` promoted to a direct module dep.
- [x] 2.2 `internal/gateway/metrics.go` defines all eight collectors on a dedicated `prometheus.NewRegistry()`.
- [x] 2.3 `/metrics` mounted under the per-IP slow-path limiter.
- [x] 2.4 `instrumentRoutes` middleware emits `kms_http_requests_total` + `kms_http_request_duration_seconds`.
- [x] 2.5 Signing handlers (`signEVMRawTx`/`signEVMPersonal`/`signEVMEIP712`/`signCosmos`) record `kms_signing_duration_seconds`.
- [x] 2.6 Vault client records `kms_vault_calls_total` + `kms_vault_call_duration_seconds` via `SetVaultCallObserver` (no prometheus import in the vault package).
- [x] 2.7 `vault.Client.SetRenewalFailureHook` increments `kms_token_renewal_failures_total` from the renewal goroutine's failure branches.
- [x] 2.8 Tests: `TestMetricsEndpointExposesCollectors`, `TestRequestMetricIncrementsOnSign`.

## 3. Request-ID propagation

- [x] 3.1 `github.com/google/uuid` already a transitive dep; no go.mod change.
- [x] 3.2 `internal/gateway/requestid.go` defines `requestIDKey`, `RequestIDFromContext`, `requestID` middleware.
- [x] 3.3 `NewRequestIDLogHandler` wraps an `slog.Handler` to emit `request_id` on every `slog.*Context` call.
- [x] 3.4 `requestID` mounted as the outermost middleware in `routes()`.
- [x] 3.5 Tests: preserve, generate, replace, slog-injection.

## 4. Panic-recovery middleware

- [x] 4.1 `internal/gateway/recover.go` with `recoverPanic`.
- [x] 4.2 Mounted immediately inside `requestID` so the recovered request has the ID in scope.
- [x] 4.3 `TestPanicRecoveryReturns500WithRequestID` covers the 500 envelope + counter bump.

## 5. Rate-limit rejection observability

- [x] 5.1 `onRateLimitRejected` emits `slog.InfoContext(..., "rate limit exceeded", ...)`.
- [x] 5.2 Increments `kms_rate_limit_rejections_total{path=...}` on rejection.
- [x] 5.3 Health/metrics slow-path limiter uses the same hook.
- [x] 5.4 `TestRateLimitRejectionLogsInfoAndCounts` asserts info-level log and counter bump.

## 6. Go toolchain floor

- [x] 6.1 `go mod why -m github.com/cosmos/cosmos-sdk` confirms cosmos-sdk v0.54.3 already requires go 1.25.9 — the current floor matches. No lowering possible.
- [x] 6.2 No change to `go.mod` floor (cosmos-sdk drives it).
- [x] 6.3 `go build ./...` and `go test ./...` pass on the current toolchain.
- [x] 6.4 `.github/workflows/ci.yml` already reads `go-version-file: go.mod`.
- [ ] 6.5 README / testing-guide Go-version statements update — manual follow-up.

## 7. Startup Swagger UI URL log

- [x] 7.1 `swaggerUIURL` derives the URL using the same `public_url`-or-loopback precedence used elsewhere.
- [x] 7.2 `serveCmd` logs `slog.Info("swagger UI", "url", ...)` only when `swagger_enabled=true`.
- [ ] 7.3 Manual/smoke test deferred.

## 8. Re-enable CI triggers

- [x] 8.1 `.github/workflows/ci.yml` uncomments `push:` (branches main + openspec/**) and `pull_request:` (branches main); keeps `workflow_dispatch`.
- [ ] 8.2 First push will exercise the suite; if anything breaks, file follow-up tasks per failure.
- [ ] 8.3 README CI section update — manual follow-up.

## 9. Verification and archive

- [x] 9.1 `go test ./...` passes locally.
- [ ] 9.2 Manual end-to-end smoke (livez/readyz/health/metrics + panic + request ID) deferred to operator.
- [ ] 9.3 `openspec validate add-observability-and-ops --strict` (run after all four apply).
- [ ] 9.4 `openspec archive-change add-observability-and-ops` (pending verification).
