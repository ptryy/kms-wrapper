## ADDED Requirements

### Requirement: Liveness and readiness endpoints
The gateway SHALL expose `GET /livez` and `GET /readyz` as separate endpoints. `/livez` SHALL return HTTP 200 with body `{"status": "alive"}` whenever the process is running, with no external dependency checks. `/readyz` SHALL return HTTP 200 with body `{"status": "ready"}` when both Vault is reachable AND the most recent `LookupSelf` succeeded within the last 30 seconds; otherwise HTTP 503 with body `{"status": "not_ready", "reason": "<vault_unreachable|token_invalid|token_lookup_stale>"}`. Both endpoints SHALL be unauthenticated. The existing `GET /health` route SHALL remain as an alias for `/readyz` for one minor-version cycle, with `Deprecation: true` and `Sunset` response headers (per the `/v1/` deprecation pattern).

#### Scenario: Liveness always alive
- **WHEN** the process is up and serving HTTP, regardless of Vault state
- **THEN** `GET /livez` returns HTTP 200 with `{"status": "alive"}`

#### Scenario: Readiness ready
- **WHEN** Vault is reachable AND `LookupSelf` succeeded within the last 30 seconds
- **THEN** `GET /readyz` returns HTTP 200 with `{"status": "ready"}`

#### Scenario: Readiness not ready — Vault unreachable
- **WHEN** Vault is unreachable
- **THEN** `GET /readyz` returns HTTP 503 with `{"status": "not_ready", "reason": "vault_unreachable"}`

#### Scenario: Readiness not ready — token expired
- **WHEN** Vault is reachable but the gateway's token can no longer `LookupSelf` (token expired or revoked)
- **THEN** `GET /readyz` returns HTTP 503 with `{"status": "not_ready", "reason": "token_invalid"}`

#### Scenario: Legacy `/health` continues
- **WHEN** a client GETs `/health`
- **THEN** the response is identical to `/readyz` AND includes `Deprecation: true` and `Sunset: <RFC1123-date≥90-days-out>` headers

---

### Requirement: Prometheus metrics endpoint
The gateway SHALL expose `GET /metrics` returning Prometheus exposition format. The endpoint SHALL be unauthenticated and SHALL be subject to a per-IP slow-path rate limiter (default 10 rps, burst 5). The endpoint SHALL be served from the bare path `/metrics` (NOT under `/v1/`). The following metrics SHALL be registered:

- `kms_http_requests_total{path,method,status}` (counter)
- `kms_http_request_duration_seconds{path,method}` (histogram)
- `kms_signing_duration_seconds{chain}` (histogram) — `chain` is `"evm"` or `"cosmos"`
- `kms_rate_limit_rejections_total{path}` (counter)
- `kms_vault_calls_total{op,status}` (counter)
- `kms_vault_call_duration_seconds{op}` (histogram)
- `kms_token_renewal_failures_total` (counter)
- `kms_panics_total{path}` (counter)

The `path` label SHALL use the matched route pattern, not the raw URL, to bound cardinality.

#### Scenario: Metrics endpoint reachable
- **WHEN** a client GETs `/metrics`
- **THEN** the response is HTTP 200 with `Content-Type: text/plain; version=0.0.4` (Prometheus exposition format) and the body includes the listed metric names

#### Scenario: Request metric is incremented
- **WHEN** an authorized client successfully POSTs to `/v1/sign/evm`
- **THEN** `kms_http_requests_total{path="/v1/sign/evm",method="POST",status="200"}` increments by 1 AND `kms_http_request_duration_seconds_bucket{path="/v1/sign/evm",method="POST",...}` increments

#### Scenario: Path label uses matched route, not raw URL
- **WHEN** a request hits `/v1/keys/info?path=proj-a/evm/alice`
- **THEN** the `path` label on `kms_http_requests_total` is `"/v1/keys/info"` (the matched route), not the full URL with query parameters

#### Scenario: Rate-limit rejection metered
- **WHEN** a request is rejected with HTTP 429 by the per-principal limiter
- **THEN** `kms_rate_limit_rejections_total{path="<matched-pattern>"}` increments by 1

#### Scenario: Token renewal failure metered
- **WHEN** the Vault client's renewal goroutine receives a non-nil error from `RenewSelf` or `LookupSelf`
- **THEN** `kms_token_renewal_failures_total` increments by 1

---

### Requirement: Request-ID propagation
The gateway SHALL accept an inbound `X-Request-ID` header (validated against pattern `^[A-Za-z0-9._-]{1,128}$`) or generate a UUIDv4 if absent or invalid. The canonical request ID SHALL be echoed back via the `X-Request-ID` response header, stored in `r.Context()`, and emitted as a `request_id=<id>` field on every `slog.*Context` log line for that request.

#### Scenario: Inbound request ID is preserved
- **WHEN** a request includes `X-Request-ID: my-trace-id-123`
- **THEN** the response includes `X-Request-ID: my-trace-id-123` AND every log line for that request includes `request_id=my-trace-id-123`

#### Scenario: Missing request ID is generated
- **WHEN** a request has no `X-Request-ID` header
- **THEN** the gateway generates a UUIDv4, returns it in the `X-Request-ID` response header, and uses it in logs

#### Scenario: Malformed request ID is replaced
- **WHEN** a request includes `X-Request-ID: ` containing 1000 characters or invalid characters (`<`, `>`, spaces)
- **THEN** the gateway generates a fresh UUIDv4 and uses that as the canonical ID (the inbound malformed value is discarded and not logged as-is)

---

### Requirement: Panic-recovery middleware
The gateway SHALL wrap every route with a top-level recovery middleware that catches any panic, logs it at `error` with the request ID and stack trace, increments `kms_panics_total`, and returns HTTP 500 with body `{"error": "internal server error", "request_id": "<id>"}`. A panic in any handler SHALL NOT crash the gateway process or terminate the listener.

#### Scenario: Handler panic returns 500
- **WHEN** a handler invokes a `panic("unexpected nil")`
- **THEN** the client receives HTTP 500 with body `{"error": "internal server error", "request_id": "<the-request's-id>"}` AND the process keeps running

#### Scenario: Panic stack trace logged
- **WHEN** a handler panics
- **THEN** a single `error`-level log line is emitted with fields `panic=<value>`, `stack=<...>`, `request_id=<...>`

#### Scenario: Panic counter increments
- **WHEN** a handler panics
- **THEN** `kms_panics_total{path="<matched-pattern>"}` increments by 1

---

### Requirement: Rate-limit rejections are logged at info and counted
When the per-principal rate limiter (or the `/health`/`/metrics` slow-path limiter) rejects a request, the gateway SHALL emit a single `info`-level log line with fields `reason=rate_limited`, `path=<matched-pattern>`, and the request ID. The gateway SHALL increment `kms_rate_limit_rejections_total{path=...}`. The rejection SHALL NOT be logged at `warn` (rate limiting is expected behaviour under load).

#### Scenario: 429 logged at info
- **WHEN** the gateway returns HTTP 429 due to rate-limit exhaustion
- **THEN** a single log line is emitted at `info` level with `reason=rate_limited`

#### Scenario: 429 not logged at warn
- **WHEN** the gateway returns HTTP 429
- **THEN** NO `warn`-level log line is emitted for this rejection (only the `info` line)

---

### Requirement: Startup logs the Swagger UI URL
When `gateway.swagger_enabled=true` and the listener starts successfully, the gateway SHALL emit one `info`-level log line containing the externally-reachable Swagger UI URL, derived using the same trusted-proxy resolver used to compute the OpenAPI `servers[].url`.

#### Scenario: UI URL logged once at startup
- **WHEN** the gateway starts with `swagger_enabled=true` and `gateway.public_url=https://kms.example.com`
- **THEN** a single log line is emitted: `info ... swagger UI url=https://kms.example.com/swagger/index.html`

#### Scenario: URL respects loopback bind
- **WHEN** the gateway starts on `127.0.0.1:8080` with no `public_url` set
- **THEN** the logged URL is `http://127.0.0.1:8080/swagger/index.html`

#### Scenario: No log when swagger disabled
- **WHEN** `swagger_enabled=false`
- **THEN** no Swagger UI URL log line is emitted at startup

---

### Requirement: Cross-change integration — probe endpoints bypass auth middleware
The gateway SHALL exempt `/livez`, `/readyz`, `/health`, and `/metrics` from the bearer-token authentication middleware introduced by `harden-gateway-security`. The auth middleware MUST NOT intercept requests to these probe and observability endpoints regardless of whether an `Authorization` header is present. These scenarios verify the combined behaviour of `harden-gateway-security` (auth middleware) and this change (probe endpoints); both changes must be applied for these scenarios to be testable.

#### Scenario: /livez is reachable without a token
- **WHEN** `harden-gateway-security` auth middleware is active AND a client sends `GET /livez` with no `Authorization` header
- **THEN** the gateway returns HTTP 200 with `{"status": "alive"}` — the auth middleware MUST NOT intercept probe endpoints

#### Scenario: /readyz is reachable without a token
- **WHEN** `harden-gateway-security` auth middleware is active AND a client sends `GET /readyz` with no `Authorization` header
- **THEN** the gateway returns HTTP 200 or 503 (depending on Vault state) — the auth middleware MUST NOT intercept probe endpoints

#### Scenario: /health alias returns the /readyz shape after both changes land
- **WHEN** both `harden-gateway-security` and `add-observability-and-ops` are applied AND a client sends `GET /health`
- **THEN** the response body is `{"status": "ready"}` (HTTP 200) or `{"status": "not_ready", "reason": "..."}` (HTTP 503) — the legacy `{"status": "ok", "vault": "reachable"}` shape from `harden-gateway-security` alone is superseded; `Deprecation: true` and `Sunset` headers are present
