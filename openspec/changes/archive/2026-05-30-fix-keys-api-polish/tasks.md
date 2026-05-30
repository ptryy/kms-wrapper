## 1. Fix 405 responses to return JSON

- [x] 1.1 Add a `json405Handler` wrapper in `internal/gateway/gateway.go` that intercepts 405 status writes and replaces the plain-text body with `{"error": "method not allowed"}` and sets `Content-Type: application/json`
- [x] 1.2 Apply the wrapper to the handler returned by `routes()` so it covers all registered routes without per-route changes
- [x] 1.3 Add a test in `gateway_test.go` asserting that `DELETE /keys` returns HTTP 405 with a JSON body (not plain text)

## 2. Update the spec

- [x] 2.1 Run `openspec archive-change fix-keys-api-polish` (or `/openspec-archive-change`) once implementation is complete to merge the delta spec into `openspec/specs/rest-gateway/spec.md`

## 3. Verify swagger docs are unaffected

- [x] 3.1 Run `make swagger-check` and confirm no diff in `docs/` — no new endpoints or schema changes means docs should be clean
