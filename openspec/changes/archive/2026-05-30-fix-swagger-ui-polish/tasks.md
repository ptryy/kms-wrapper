## 1. Gateway: Serve /swagger/ without redirect

- [x] 1.1 In `internal/gateway/gateway.go`, replace the bare `swaggerUI` handler at `GET /swagger/` with a closure that rewrites `r.URL.Path` from `/swagger/` to `/swagger/index.html` before calling `swaggerUI.ServeHTTP`, leaving all other `/swagger/*` paths unchanged
- [x] 1.2 Add a test in `internal/gateway/gateway_test.go` asserting `GET /swagger/` returns HTTP 200 (not 301) and has `Content-Type` containing `text/html`

## 2. Gateway: Log rejected X-Forwarded-Proto values

- [x] 2.1 In `requestOrigin()` in `internal/gateway/gateway.go`, add `slog.DebugContext(r.Context(), "ignoring unrecognised X-Forwarded-Proto", "value", forwarded)` in the `default` branch of the scheme switch, after the fallback to `http` is already in effect

## 3. Docs: README note on static spec file

- [x] 3.1 In `README.md`, add a short note under the "API docs" section clarifying that `docs/swagger.json` contains a `localhost:8080` placeholder server URL (correct OpenAPI, intentional for codegen/import) and that the live `GET /swagger/doc.json` endpoint is the source of truth for Swagger UI

## 4. Archive

- [x] 4.1 Run `openspec archive-change fix-swagger-ui-polish` (or `/openspec-archive-change`) once all implementation tasks are complete to merge the delta spec into `openspec/specs/rest-gateway/spec.md`
