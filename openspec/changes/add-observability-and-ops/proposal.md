## Why

The gateway currently has no Prometheus metrics, no request-ID propagation, a single `/health` endpoint that conflates liveness and readiness (so K8s pods get killed on a transient Vault blip rather than just stopped from routing), and no panic-recovery middleware (one handler panic crashes the process). The `go.mod` toolchain floor is also overspecified to `go 1.25.9` despite no language feature past `1.22` being used, which raises the bar for contributors and CI runners unnecessarily. This change adds the operational surface every production HTTP service needs and lowers the toolchain floor.

## What Changes

- **Split `/health` into `/livez` and `/readyz`.** `/livez` returns 200 as long as the process is up. `/readyz` checks Vault reachability AND token renewability (i.e., the current token still works against `LookupSelf`). The existing `/health` route remains as an alias for `/readyz` for one minor-version cycle.
- **Add `/metrics` (Prometheus).** Counters and histograms: `kms_http_requests_total{path,method,status}`, `kms_http_request_duration_seconds`, `kms_signing_duration_seconds{chain}`, `kms_rate_limit_rejections_total{principal_class,path}`, `kms_vault_calls_total{op,status}`, `kms_vault_call_duration_seconds`, `kms_token_renewal_failures_total`. Use `prometheus/client_golang`.
- **X-Request-ID propagation.** Middleware reads `X-Request-ID` if present (validate against a regex), generates a UUIDv4 otherwise, stuffs it into `r.Context()`, echoes it back in the response, and includes it in every `slog.*Context` call.
- **Panic-recovery middleware.** Top-level handler wraps every route with `defer func() { if r := recover() { ... } }()` that logs the panic with the request ID and returns HTTP 500 `{"error": "internal server error", "request_id": "..."}`. Does NOT crash the process.
- **Rate-limit rejection logging and metering.** 429 responses are logged at `info` (not `warn` — they are expected under load) with `reason=rate_limited` and counted in `kms_rate_limit_rejections_total`. Pairs with the per-principal limiter from `harden-gateway-security`.
- **Startup log includes Swagger UI URL.** When `gateway.swagger_enabled=true`, log the externally-reachable Swagger UI URL at `info` once during startup (computed via the same trusted-proxy resolver from `harden-gateway-security`).
- **Lower `go.mod` toolchain floor.** Change `go 1.25.9` to `go 1.22` (or whatever cosmos-sdk `v0.54.3` actually requires — verify with `go mod why` against the highest-floor transitive dep). Update CI's `actions/setup-go` configuration and `testing-guide.md` accordingly.
- **Re-enable CI on push and pull_request.** `.github/workflows/ci.yml` triggers are currently commented out except `workflow_dispatch`. Re-enable `push:` (on `main` and the openspec branches) and `pull_request:` (on `main`).

## Capabilities

### New Capabilities
<!-- None — observability requirements attach to existing rest-gateway capability -->

### Modified Capabilities
- `rest-gateway`: adds `/livez`, `/readyz`, `/metrics`, request-ID propagation, panic-recovery, and rate-limit metering requirements.

## Impact

- `internal/gateway/gateway.go`: new middleware (request-ID, panic-recovery), new routes (`/livez`, `/readyz`, `/metrics`), metric instrumentation in existing handlers.
- New file `internal/gateway/metrics.go` for Prometheus collector definitions.
- `go.mod`: add `github.com/prometheus/client_golang v1.x`; lower `go` directive.
- `cmd/kms-wrapper/root.go`: emit the startup Swagger-UI URL log line.
- `.github/workflows/ci.yml`: uncomment `push:` and `pull_request:` triggers.
- `testing-guide.md` and `README.md`: align Go version statements with the new `go.mod` floor.
- No breaking changes: `/health` keeps working as a `/readyz` alias for one minor-version cycle.
