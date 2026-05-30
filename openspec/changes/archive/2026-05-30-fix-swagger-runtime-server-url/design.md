## Context

The gateway serves Swagger UI at `/swagger/index.html` and the generated OpenAPI document at `/swagger/doc.json`.  
Today the generated spec contains a fixed server URL (`http://localhost:8080/`), which causes Swagger UI "Try it out" to target the wrong host/port whenever the gateway runs at a custom address (for example `127.0.0.1:3010`).

## Goals / Non-Goals

**Goals:**
- Ensure Swagger UI sends requests to the currently running gateway origin (host/port and scheme seen by the client).
- Preserve existing Swagger feature toggles (`swagger_enabled`, `swagger_auth`) and route behavior.
- Add regression tests that fail if a fixed localhost server URL reappears.

**Non-Goals:**
- Redesigning the broader OpenAPI generation pipeline.
- Changing signing/auth/rate-limit behavior for non-swagger routes.
- Adding environment-based server URL configuration knobs in this change.

## Decisions

1. **Serve a runtime-normalized Swagger document**
   - Decision: intercept `/swagger/doc.json` in gateway code and rewrite the OpenAPI `servers` value to match request origin.
   - Rationale: this ties Swagger UI requests to the actual endpoint the user opened, including non-default ports.
   - Alternatives considered:
     - Keep fixed generated host/port: rejected, reproduces the bug.
     - Bind to configured `gateway.addr` only: rejected, may still mismatch externally visible origin (reverse proxy/TLS termination).

2. **Preserve relative behavior where possible**
   - Decision: prefer origin derived from incoming request (including scheme) so docs remain usable behind local/proxied setups.
   - Rationale: request-derived origin reflects user-visible entrypoint better than static build-time values.

3. **Keep changes scoped to swagger document serving path**
   - Decision: avoid changing unrelated middleware/handler plumbing except what is required to serve normalized doc JSON.
   - Rationale: minimizes regression risk.

## Risks / Trade-offs

- **[Risk] Proxy headers or TLS termination may affect perceived scheme** → **Mitigation:** derive origin from standard request properties and add tests around expected local behavior.
- **[Risk] Runtime JSON rewriting adds minor overhead per `/swagger/doc.json` request** → **Mitigation:** keep logic minimal and scoped to small spec payload.
- **[Risk] Inconsistent behavior between generated files and served JSON** → **Mitigation:** assert served `/swagger/doc.json` behavior in gateway tests (source of truth for UI).
