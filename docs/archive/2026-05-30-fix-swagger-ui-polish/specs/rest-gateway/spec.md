## ADDED Requirements

### Requirement: Swagger root path serves UI without redirect
`GET /swagger/` SHALL return HTTP 200 with the Swagger UI body directly. The gateway SHALL NOT issue a redirect (301, 302, 307, or 308) to `/swagger/index.html`. This ensures the UI is reachable in environments where reverse proxies or CSP policies strip or block `Location` headers, and avoids permanently-cached redirect entries in clients.

#### Scenario: Root swagger path returns UI directly
- **WHEN** a client sends `GET /swagger/` to the gateway
- **THEN** the gateway responds with HTTP 200 and an HTML body containing the Swagger UI, without issuing any redirect response

#### Scenario: Index path still works
- **WHEN** a client sends `GET /swagger/index.html` to the gateway
- **THEN** the gateway responds with HTTP 200 and the same Swagger UI HTML body

#### Scenario: Auth gate applies to root path too
- **WHEN** `gateway.swagger_auth=true` and a client sends `GET /swagger/` without an `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}`, not a redirect to the login page
