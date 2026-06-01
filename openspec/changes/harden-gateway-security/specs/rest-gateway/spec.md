## MODIFIED Requirements

### Requirement: Bearer token authentication middleware
The REST gateway SHALL require all non-health, non-metrics requests to include `Authorization: Bearer <token>` matching the value of `KMS_GATEWAY_TOKEN`. The comparison SHALL be performed in constant time over fixed-length HMAC-SHA256 digests of the supplied and configured tokens (using a server-side nonce generated at startup as the HMAC key), so the comparison does not short-circuit on unequal input lengths. Requests without a valid token SHALL be rejected with HTTP 401 and a single log line indicating the failure reason (`missing`, `bad-format`, or `mismatch`). The supplied token SHALL NOT appear in any log entry.

#### Scenario: Valid token
- **WHEN** a request includes `Authorization: Bearer <correct-token>`
- **THEN** the request is forwarded to the handler and no auth log line is emitted

#### Scenario: Missing token
- **WHEN** a request includes no `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}` and logs `unauthorized request reason=missing` at `warn`

#### Scenario: Wrong token
- **WHEN** a request includes an incorrect bearer token
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}` and logs `unauthorized request reason=mismatch` at `warn`; the supplied token is NOT in the log

#### Scenario: Malformed header
- **WHEN** the `Authorization` header is present but does not start with `Bearer ` (e.g. `Basic ...`)
- **THEN** the gateway responds with HTTP 401 and logs `unauthorized request reason=bad-format` at `warn`

#### Scenario: Length-leak resistance
- **WHEN** an attacker probes with bearer tokens of varying lengths against an endpoint that returns 401
- **THEN** the response time distribution does not correlate with the supplied token length (HMAC digests are fixed-length so the comparison runs the same number of bytes regardless of input)

---

### Requirement: Health endpoint
The gateway SHALL expose `GET /health` without authentication. The response SHALL include Vault connectivity status. The endpoint SHALL be subject to a dedicated slow-path rate limiter (default 10 rps, burst 5, keyed on remote IP) and its result SHALL be cached for 1 second to absorb micro-bursts. When the slow-path limiter is exhausted, `/health` SHALL return HTTP 429 with body `{"error": "rate limit exceeded"}`.

#### Scenario: Healthy
- **WHEN** Vault is reachable and the token is valid
- **THEN** `GET /health` returns HTTP 200 with `{"status": "ok", "vault": "reachable"}`

#### Scenario: Vault unreachable
- **WHEN** Vault cannot be reached
- **THEN** `GET /health` returns HTTP 503 with `{"status": "degraded", "vault": "unreachable"}`

#### Scenario: Health response is cached for 1 second
- **WHEN** `GET /health` is called twice within 1 second from any client(s)
- **THEN** at most one Vault round-trip is performed; the second response reuses the first response's status and body

#### Scenario: Health rate-limited under burst
- **WHEN** a single remote IP issues 30 `GET /health` requests in 1 second
- **THEN** at most ~15 (rate × 1s + burst) succeed; the rest receive HTTP 429 with body `{"error": "rate limit exceeded"}`

---

### Requirement: Swagger surface respects optional bearer auth
When `gateway.swagger_auth` is true (the default), all `/swagger/*` routes SHALL be wrapped by the same bearer-token middleware that protects `/sign/evm` and `/sign/cosmos`. When `gateway.swagger_auth` is false, the `/swagger/*` routes SHALL be publicly reachable. The gateway SHALL refuse to start when `gateway.swagger_auth=false` and the configured listen address is not a loopback address (`127.0.0.0/8` or `::1`), unless the environment variable `KMS_DEV` is set to `true`.

#### Scenario: Default auth gate is on
- **WHEN** `gateway.swagger_auth` is not explicitly set and a client requests `GET /swagger/index.html` without an `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

#### Scenario: Auth gate enabled with valid token
- **WHEN** `gateway.swagger_auth=true` and a client requests `GET /swagger/doc.json` with `Authorization: Bearer <correct-token>`
- **THEN** the gateway responds with HTTP 200 and the spec document

#### Scenario: Loopback bind permits public swagger
- **WHEN** `gateway.swagger_auth=false` and `gateway.addr=127.0.0.1:8080`
- **THEN** the gateway starts normally and `GET /swagger/index.html` is publicly reachable

#### Scenario: Non-loopback bind refuses public swagger
- **WHEN** `gateway.swagger_auth=false` and `gateway.addr=0.0.0.0:8080` and `KMS_DEV` is not set
- **THEN** startup fails with `"refusing to expose unauthenticated swagger on non-loopback address; set KMS_DEV=true for local dev"`

#### Scenario: Non-loopback bind with KMS_DEV escape
- **WHEN** `gateway.swagger_auth=false` and `gateway.addr=0.0.0.0:8080` and `KMS_DEV=true`
- **THEN** startup proceeds and a `warn` log is emitted: `"running with unauthenticated swagger on non-loopback (KMS_DEV=true)"`

---

## ADDED Requirements

### Requirement: Per-principal rate limiting for signing and key endpoints
The gateway SHALL apply rate limiting per principal, where the principal key is `HMAC-SHA256(server_nonce, bearer_token) || ip`. A principal SHALL have its own `golang.org/x/time/rate.Limiter` instance using the configured `gateway.rate_limit` and `gateway.rate_burst` values. Principal entries SHALL be evicted from the map after 5 minutes of idle time. The map SHALL be capped at 10,000 entries; when full, the least-recently-used entry is evicted. The principal key value SHALL NOT appear in logs in cleartext; only its fingerprint may be logged.

#### Scenario: One principal does not starve another
- **WHEN** principal A exhausts its rate budget on `/sign/evm`
- **THEN** principal B with a different token continues to receive `2xx` on its own `/sign/evm` calls until B's own budget is exhausted

#### Scenario: Same token from two IPs has separate budgets
- **WHEN** the same bearer token is used from two different remote IPs
- **THEN** the two callers have independent rate budgets (the principal key concatenates the IP)

#### Scenario: Idle entries are evicted
- **WHEN** a principal has not issued a request for 5 minutes
- **THEN** its limiter entry is removed from the map; the next request from that principal allocates a fresh limiter at full burst

#### Scenario: Map capacity is bounded
- **WHEN** 10,000 distinct principals have active limiters and a 10,001st arrives
- **THEN** the least-recently-used entry is evicted before the new one is inserted

---

### Requirement: Trusted-proxy gate on forwarded headers
The gateway SHALL honour `X-Forwarded-Proto` and `X-Forwarded-Host` only when the immediate peer's IP matches one of the CIDR entries in `gateway.trusted_proxies`. When the peer is untrusted, the gateway SHALL derive scheme from `r.TLS != nil` and host from `gateway.public_url` if configured, otherwise from `r.Host`. The OpenAPI `servers[].url` exposed at `GET /swagger/doc.json` SHALL be computed via this same resolver.

#### Scenario: Default config does not trust forwarded headers
- **WHEN** `gateway.trusted_proxies` is empty (default) and a client sends `Host: attacker.example` and `X-Forwarded-Proto: https`
- **THEN** the OpenAPI document served back uses `gateway.public_url` (if set) or `r.Host` as the host, and scheme `http` (because `r.TLS` is nil for a plaintext listener) — NOT `https://attacker.example`

#### Scenario: Trusted proxy is honoured
- **WHEN** `gateway.trusted_proxies=["10.0.0.0/8"]`, the peer IP is `10.0.0.5`, and headers say `X-Forwarded-Proto: https`, `X-Forwarded-Host: api.example.com`
- **THEN** the OpenAPI document uses `https://api.example.com`

#### Scenario: Public URL override
- **WHEN** `gateway.public_url=https://kms.example.com` is set and `gateway.trusted_proxies` is empty
- **THEN** OpenAPI documents target `https://kms.example.com` regardless of `Host` headers from clients

---

### Requirement: Weak-token startup guard
The gateway SHALL refuse to start when `gateway.token` is empty or matches a known-weak placeholder (`change-me`, `dev`, `dev-token`, `password`) unless `KMS_DEV=true` is set. The same rule SHALL apply to `vault.token` (with `root` added to the weak list — covered by the `vault-backend` capability).

#### Scenario: Default placeholder refused
- **WHEN** `gateway.token=change-me` and `KMS_DEV` is not set
- **THEN** startup fails with `"refusing to start with weak gateway token; set KMS_DEV=true for local dev"`

#### Scenario: Empty token refused
- **WHEN** `gateway.token` is empty and `KMS_DEV` is not set
- **THEN** startup fails with `"gateway token is required"`

#### Scenario: KMS_DEV bypass with warn
- **WHEN** `gateway.token=dev-token` and `KMS_DEV=true`
- **THEN** startup proceeds and a `warn` log is emitted: `"running with weak gateway token (KMS_DEV=true)"`
