## Context

Verification of `add-keys-rest-endpoints` identified three gaps in the `/keys` REST surface:

1. **405 plain-text body** — Go's `http.ServeMux` (≥1.22) automatically returns `Method Not Allowed` as plain text when a method-specific pattern is registered but the caller uses a different method (e.g. `DELETE /keys`). Every other error from this gateway returns JSON, so this is inconsistent and will break callers that parse error bodies uniformly.

2. **Undocumented list traversal** — `GET /keys` proxies Vault's LIST API, which returns one level of the key hierarchy at a time. Intermediate nodes appear as `"proj/"` (with a trailing slash); callers must page down with `?prefix=proj/` to reach leaf names. This behaviour is correct but nowhere documented, making the endpoint look broken on first use.

3. **Silent no-delete** — key deletion is intentionally not exposed (preserves Vault's audit log and key history). Without a spec statement to that effect, callers and reviewers will keep asking whether it is missing or planned.

All three issues are non-breaking; no existing callers are affected.

## Goals / Non-Goals

**Goals:**
- 405 responses use `Content-Type: application/json` and body `{"error": "method not allowed"}` in all cases
- Spec records the hierarchical list traversal semantics for `GET /keys`
- Spec records that key deletion is intentionally absent

**Non-Goals:**
- No new endpoints or request/response shape changes
- No changes to 404 or other existing error codes
- No `?flat=true` or recursive list mode (out of scope for this change)

## Decisions

### D1 — Intercept 405 at the mux boundary, not per-handler

**Decision**: Wrap the `http.ServeMux` returned by `routes()` in a thin middleware that detects a 405 response and rewrites the body to JSON before it is flushed to the client.

**Rationale**: `http.ServeMux` generates the 405 body internally; there is no `MethodNotAllowedHandler` hook in the stdlib. Registering duplicate handlers for every unsupported method on every route would be noisy and error-prone. A wrapping `ResponseWriter` that buffers the status and rewrites if needed is the smallest possible change and consistent with the existing `statusResponseWriter` pattern already in `gateway.go`.

**Alternative considered — register explicit `405` handlers per route**: would require listing every unsupported method for every route and keeping them in sync as routes are added. Rejected: high maintenance burden.

**Alternative considered — switch to a third-party router (chi, gorilla/mux)**: provides a `MethodNotAllowedHandler` hook out of the box. Rejected: introduces a new dependency for a one-line behavioural fix.

### D2 — Document list traversal in spec only, no code change

**Decision**: Add a spec requirement describing the hierarchical list behaviour. No code changes to `GET /keys`.

**Rationale**: The behaviour (Vault LIST returns one level at a time) is correct and intentional. The spec gap, not the implementation, is the bug. A `?flat=true` parameter could be added later if callers need it, but that is a separate feature.

### D3 — Explicit no-delete requirement in spec

**Decision**: Add a spec requirement stating that no DELETE endpoint is exposed for the keys resource, with the reason.

**Rationale**: Without a documented decision, every reviewer and future contributor will ask why DELETE is missing. An explicit requirement closes the question and prevents accidental addition.

## Risks / Trade-offs

- **Buffered response writer** — the 405-rewrite wrapper must not buffer large successful responses. Since 405 is set before any body is written by the mux, buffering only the status line (not the body) is sufficient and safe.
- **Swagger docs** — no request/response shape changes; `make swagger` will be a no-op diff except for any incidental reformatting. Confirm with `make swagger-check` after the handler change.

## Migration Plan

1. Add the 405-rewrite middleware in `gateway.go` (wraps `routes()` output before `ListenAndServe`)
2. Update `openspec/specs/rest-gateway/spec.md` (archive step will merge the delta)
3. Run `make swagger-check` to confirm docs are still clean
4. No config, no env var, no deployment step required — the change is purely in-process

## Open Questions

None — all three items have clear, scoped solutions.
