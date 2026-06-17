## Why

Swagger UI currently uses a hard-coded OpenAPI server URL (`http://localhost:8080/`) instead of the actual gateway origin. When the gateway runs on a different address/port (for example `127.0.0.1:3010`), "Try it out" requests target the wrong endpoint and fail.

## What Changes

- Remove hard-coded server origin behavior from generated Swagger docs at runtime.
- Make `/swagger/doc.json` resolve API requests against the current request origin so Swagger UI works on custom gateway addresses.
- Add regression coverage to ensure non-default gateway ports keep working in Swagger UI.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `rest-gateway`: Update API documentation serving behavior so OpenAPI server URL follows the active gateway origin instead of fixed localhost:8080.

## Impact

- Affected code: `internal/gateway` swagger doc serving path and related tests.
- Affected docs behavior: Swagger UI "Try it out" targets the running gateway address/port.
- No external dependency changes expected.
