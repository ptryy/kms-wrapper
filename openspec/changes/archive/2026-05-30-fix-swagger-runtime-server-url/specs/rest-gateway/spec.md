## ADDED Requirements

### Requirement: Swagger OpenAPI server URL reflects active gateway origin
When a client retrieves `GET /swagger/doc.json`, the gateway SHALL serve an OpenAPI document whose server URL targets the same origin the client is using for the gateway, rather than a fixed default localhost port.

#### Scenario: Custom gateway port is reflected
- **WHEN** the gateway is running on `127.0.0.1:3010` and a client opens Swagger UI from that origin
- **THEN** operations executed from Swagger UI target `http://127.0.0.1:3010` (not `http://localhost:8080`)

#### Scenario: Default port remains valid
- **WHEN** the gateway is running on its default local address
- **THEN** Swagger UI operations still target the running gateway origin and continue to work without manual edits
