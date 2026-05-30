## ADDED Requirements

### Requirement: Swagger UI and OpenAPI spec endpoints
When `gateway.swagger_enabled` is true (the default), the REST gateway SHALL expose two routes:

- `GET /swagger/index.html` — interactive Swagger UI served by `swaggo/http-swagger`.
- `GET /swagger/doc.json` — the raw OpenAPI 3.0 specification document.

The UI route SHALL also serve any sibling static assets (`/swagger/swagger-ui*.js`, `/swagger/swagger-ui.css`, etc.) required by the bundled Swagger UI distribution.

#### Scenario: UI is reachable in default config
- **WHEN** the gateway starts with default config and a client requests `GET /swagger/index.html`
- **THEN** the gateway responds with HTTP 200 and an HTML document containing the Swagger UI

#### Scenario: Spec endpoint serves OpenAPI 3.0
- **WHEN** a client requests `GET /swagger/doc.json`
- **THEN** the gateway responds with HTTP 200, `Content-Type: application/json`, and a body whose top-level `openapi` field starts with `3.0`

#### Scenario: Swagger disabled
- **WHEN** `gateway.swagger_enabled=false` is set and a client requests `GET /swagger/index.html`
- **THEN** the gateway responds with HTTP 404

---

### Requirement: Swagger surface respects optional bearer auth
When `gateway.swagger_auth` is true, all `/swagger/*` routes SHALL be wrapped by the same bearer-token middleware that protects `/sign/evm` and `/sign/cosmos`. When `gateway.swagger_auth` is false (the default), the `/swagger/*` routes SHALL be publicly reachable.

#### Scenario: Default public access
- **WHEN** `gateway.swagger_auth=false` and a client requests `GET /swagger/index.html` without an `Authorization` header
- **THEN** the gateway responds with HTTP 200

#### Scenario: Auth gate enabled and token missing
- **WHEN** `gateway.swagger_auth=true` and a client requests `GET /swagger/doc.json` without an `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`

#### Scenario: Auth gate enabled with valid token
- **WHEN** `gateway.swagger_auth=true` and a client requests `GET /swagger/doc.json` with `Authorization: Bearer <correct-token>`
- **THEN** the gateway responds with HTTP 200 and the spec document

---

### Requirement: Swagger routes are exempt from rate limiting
The `/swagger/*` routes SHALL NOT count against the per-process rate limiter that protects the signing endpoints. Doc-surface traffic SHALL never starve signing requests, and signing-bound bursts SHALL never make the docs unreachable.

#### Scenario: Docs available under signing load
- **WHEN** the signing rate limiter is exhausted (subsequent `/sign/*` calls would receive HTTP 429)
- **THEN** `GET /swagger/index.html` and `GET /swagger/doc.json` still respond with HTTP 200

---

### Requirement: Swagger routes do not appear in spec discovery when disabled
The `/swagger/*` operations SHALL NOT be advertised in the OpenAPI spec document. The spec describes the signing/health API surface only.

#### Scenario: Spec omits swagger routes
- **WHEN** a client retrieves `GET /swagger/doc.json`
- **THEN** the returned spec's `paths` object contains entries for `/health`, `/sign/evm`, and `/sign/cosmos`, and no entry for `/swagger/index.html` or `/swagger/doc.json`
