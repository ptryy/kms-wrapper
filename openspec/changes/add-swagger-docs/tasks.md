## 1. Dependencies and toolchain

- [ ] 1.1 Add `github.com/swaggo/swag` (v2.x for OpenAPI 3.0) and `github.com/swaggo/http-swagger` to `go.mod` via `go get`
- [ ] 1.2 Document `swag` CLI installation in `README.md` (`go install github.com/swaggo/swag/cmd/swag@latest`)
- [ ] 1.3 Add `make swagger` target that runs `swag init -g cmd/kms-wrapper/root.go --output docs --outputTypes go,json,yaml --v3.1` (or matching v3.0 flag)
- [ ] 1.4 Add `make swagger-check` target that runs `make swagger` then `git diff --exit-code docs/`
- [ ] 1.5 Wire `swagger-check` into the existing CI workflow (Makefile target list and any GitHub Actions / CI YAML)

## 2. Config plumbing

- [ ] 2.1 Add `SwaggerEnabled bool` and `SwaggerAuth bool` to `config.Config.Gateway` in `internal/config/config.go` with mapstructure tags `swagger_enabled` and `swagger_auth`
- [ ] 2.2 Set defaults in `config.Default()` and via `v.SetDefault` in `Load`: `swagger_enabled=true`, `swagger_auth=false`
- [ ] 2.3 Bind env vars `KMS_GATEWAY_SWAGGER_ENABLED` and `KMS_GATEWAY_SWAGGER_AUTH` in `Load`
- [ ] 2.4 Extend `internal/config/config_test.go` with cases for defaults, env override, YAML override, and malformed boolean
- [ ] 2.5 Update sample `config.yaml` with the two new keys (commented examples)

## 3. Handler annotations

- [ ] 3.1 Add a top-of-package swaggo `// @title`, `// @version`, `// @description`, `// @host`, `// @BasePath` block — place in `cmd/kms-wrapper/root.go` or `internal/gateway/gateway.go` (whichever swag scans by default)
- [ ] 3.2 Declare the `BearerAuth` security scheme via `// @securityDefinitions.apikey` / OpenAPI 3.0 `@securityScheme` annotation
- [ ] 3.3 Annotate `health` handler with `@Summary`, `@Tags health`, `@Success 200`, `@Failure 503`, `@Router /health [get]`, and `@Security` omitted (public)
- [ ] 3.4 Annotate `signEVM` handler with `@Summary`, `@Tags signing`, `@Accept json`, `@Produce json`, `@Param body body apptypes.EVMSignRequest true ...`, `@Success 200 {object} apptypes.SignResponse`, `@Failure 400/401/429/500 {object} ErrorResponse`, `@Security BearerAuth`, `@Router /sign/evm [post]`
- [ ] 3.5 Define three OpenAPI variant schemas for the EVM payload (`EVMSignRawTxRequest`, `EVMSignPersonalMessageRequest`, `EVMSignEIP712Request`) — either as Go types in `pkg/types/types.go` used only for docs, or via swaggo override comments — and reference them with a `oneOf` on the request body
- [ ] 3.6 Annotate `signCosmos` handler similarly, with `sign_mode` declared as enum `[DIRECT, AMINO_JSON]` and a field-level description on `sign_doc` explaining the per-mode encoding
- [ ] 3.7 Define an `ErrorResponse` schema (struct + annotations) and reference it from every non-2xx response declaration

## 4. Gateway wiring

- [ ] 4.1 Import generated `docs` package for side-effects in `cmd/kms-wrapper/root.go`: `_ "github.com/ryan-truong/kms-wrapper/docs"`
- [ ] 4.2 In `gateway.routes`, conditionally register `/swagger/*` when `s.cfg.Gateway.SwaggerEnabled` is true, using `httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json"))`
- [ ] 4.3 Wrap the registered handler with `s.auth(...)` when `s.cfg.Gateway.SwaggerAuth` is true; otherwise mount it unwrapped
- [ ] 4.4 Ensure `/swagger/*` is NOT wrapped by the existing `rateLimit` middleware
- [ ] 4.5 Keep the `requestLogger` middleware on the swagger routes (consistency with other endpoints)

## 5. Tests

- [ ] 5.1 Add `gateway_test.go` cases verifying `GET /swagger/index.html` returns 200 in default config
- [ ] 5.2 Add a case asserting the response body of `/swagger/index.html` contains the expected Swagger UI markers (e.g. `<title>Swagger UI</title>` or `swagger-ui-bundle`)
- [ ] 5.3 Add a case for `GET /swagger/doc.json`: unmarshal the body as a generic map, assert `openapi` field starts with `3.0`, and assert `paths` contains `/health`, `/sign/evm`, `/sign/cosmos`
- [ ] 5.4 Add a case asserting `paths` does NOT contain any `/swagger/*` entries
- [ ] 5.5 Add a case for `swagger_enabled=false`: `GET /swagger/index.html` returns 404
- [ ] 5.6 Add a case for `swagger_auth=true`: `GET /swagger/doc.json` without `Authorization` returns 401; with correct bearer returns 200
- [ ] 5.7 Add a case asserting the EVM `oneOf` schema is present in the generated spec (look up `paths./sign/evm.post.requestBody.content.application/json.schema.oneOf` and assert it has length 3)
- [ ] 5.8 Add a case asserting `/health` operation has `security: []` and `/sign/*` operations require `BearerAuth`
- [ ] 5.9 Add a case asserting `/swagger/*` routes are not rate-limited (hammer past the limiter, then assert swagger still returns 200)

## 6. Documentation and rollout

- [ ] 6.1 Update `README.md` with an "API documentation" section pointing at `/swagger/index.html`, including the bearer-token try-it-out flow
- [ ] 6.2 Document the production-hardening guidance: set `KMS_GATEWAY_SWAGGER_AUTH=true` (or `KMS_GATEWAY_SWAGGER_ENABLED=false`) when the gateway is internet-exposed
- [ ] 6.3 Regenerate docs once via `make swagger` and commit `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`
- [ ] 6.4 Run `go build ./...` on a clean `$GOPATH` without `swag` installed to verify the build is reproducible
- [ ] 6.5 Run `make swagger-check` locally to confirm the CI gate passes
