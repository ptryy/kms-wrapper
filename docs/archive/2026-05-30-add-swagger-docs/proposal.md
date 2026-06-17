## Why

The REST gateway exposes signing endpoints (`/sign/evm`, `/sign/cosmos`) whose request shapes are non-trivial — the EVM endpoint accepts one of three mutually exclusive payloads, and Cosmos signing has mode-specific encoding rules. Today these contracts live only in `pkg/types/types.go` and `internal/gateway/gateway.go`, forcing consumers (other MANTRA services, on-call operators, integration tests) to read Go source to learn how to call the API. An OpenAPI 3.0 spec plus an embedded Swagger UI gives us machine-readable docs, a try-it-out console for debugging, and a stable contract surface other teams can codegen against.

## What Changes

- Add `github.com/swaggo/swag` (CLI) and `github.com/swaggo/http-swagger` (UI handler) as build/runtime dependencies.
- Annotate the gateway handlers (`signEVM`, `signCosmos`, `health`) and request/response structs in `pkg/types` with swaggo OpenAPI 3.0 annotations.
- Generate `docs/swagger.json`, `docs/swagger.yaml`, and `docs/docs.go` via `swag init` and **commit them to the repo** so the binary builds reproducibly without `swag` installed.
- Mount two new gateway routes under `/swagger/*`:
  - `GET /swagger/index.html` — interactive Swagger UI
  - `GET /swagger/doc.json` — raw OpenAPI 3.0 spec
- Extend `config.Config.Gateway` with two new fields:
  - `swagger_enabled` (default `true`) — toggle the routes on/off
  - `swagger_auth` (default `false`) — when `true`, reuse the existing bearer-token middleware on `/swagger/*`
- Add a `make swagger` Makefile target that runs `swag init` and a `make swagger-check` target (used by CI) that regenerates and `git diff --exit-code`s to catch drift.
- Use OpenAPI 3.0 `oneOf` with a discriminator to describe the three EVM payload variants (raw_tx + chain_id, personal_message, eip712_digest) precisely.
- Update `README.md` with a short "API docs" section pointing at `/swagger/index.html`.

## Capabilities

### New Capabilities
- `api-docs`: OpenAPI 3.0 specification, swaggo annotation conventions, and the Swagger UI / spec endpoints served by the gateway.

### Modified Capabilities
- `rest-gateway`: Adds requirements for the `/swagger/*` routes, the `swagger_enabled` and `swagger_auth` config knobs, and the rule that the auth middleware skips `/swagger/*` when `swagger_auth=false`.
- `cli`: No behavior change, but the runtime CLI must load the two new config keys via the existing viper flow (env vars `KMS_GATEWAY_SWAGGER_ENABLED`, `KMS_GATEWAY_SWAGGER_AUTH`).

## Impact

- **Code**: `internal/gateway/gateway.go` (route registration, auth bypass logic), `internal/config/config.go` (two new fields + env binds), `pkg/types/types.go` (annotations only), `cmd/kms-wrapper/root.go` (import generated `docs` package side-effect).
- **New files**: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` (generated, committed).
- **Build**: `go.mod` picks up swaggo and http-swagger; `Makefile` gains `swagger` and `swagger-check` targets.
- **CI**: Add a `swagger-check` step so PRs that change handlers without regenerating docs fail loudly.
- **Operations**: Operators get a self-serve UI at `http://<gateway>/swagger/index.html`. In production, recommended posture is `swagger_enabled=true` with `swagger_auth=true` (or `swagger_enabled=false` if the gateway is internet-exposed).
- **No breaking changes** to existing endpoints, request/response shapes, or config keys.
