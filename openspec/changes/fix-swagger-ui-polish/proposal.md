## Why

Verification of `fix-swagger-runtime-server-url` and `add-swagger-docs` surfaced three minor but addressable gaps: the static committed spec has an undocumented localhost placeholder that will confuse Postman/codegen users, a rejected proxy header falls through silently making misconfigured reverse proxies hard to diagnose, and `GET /swagger/` issues a cacheable 301 that could strand users if the path ever moves.

## What Changes

- **README note on static `docs/swagger.json`**: Add a short paragraph to the API Docs section in `README.md` explaining that `docs/swagger.json` is a generated artifact whose `servers[0].url` is a placeholder (`http://localhost:8080/`). The runtime `GET /swagger/doc.json` endpoint is the source of truth — it reflects the actual gateway origin on every request.
- **Debug log for rejected `X-Forwarded-Proto` values**: When `requestOrigin()` receives an unrecognised `X-Forwarded-Proto` value (anything other than `http` or `https`), emit a `slog.DebugContext` line with the rejected value. Silently falling back to `http` is correct behaviour; logging it at debug lets operators catch reverse-proxy misconfigurations without adding noise to production logs.
- **`GET /swagger/` serves 200 directly**: Instead of letting the `http-swagger` library issue a 301 redirect to `/swagger/index.html`, intercept `GET /swagger/` in the mux wrapper and rewrite the request path to `/swagger/index.html` before passing it to the handler. The client receives HTTP 200 with the UI, no redirect. This removes a permanently-cached redirect and avoids the (edge-case) scenario where a strict-CSP reverse proxy strips the `Location` header.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `rest-gateway`: Adds a scenario to the Swagger UI endpoint requirement: `GET /swagger/` SHALL return HTTP 200 with the UI directly, without a redirect.

## Impact

- `README.md`: one short paragraph in the existing "API docs" section
- `internal/gateway/gateway.go`: two-line change — path rewrite in the `/swagger/` handler wrapper, plus one `slog.DebugContext` call in `requestOrigin()`
- No new dependencies, no endpoint shape changes, no config changes
- No breaking changes
