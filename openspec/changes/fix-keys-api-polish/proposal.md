## Why

Verification of `add-keys-rest-endpoints` revealed three rough edges in the `/keys` API that will cause confusion or integration bugs for callers: 405 errors return plain text instead of JSON, the hierarchical list behavior of `GET /keys` is undocumented, and the intentional absence of key deletion is nowhere specified.

## What Changes

- **405 responses return JSON**: Register a custom `MethodNotAllowedHandler` on the gateway mux so that unsupported methods (e.g. `DELETE /keys`) return `{"error": "method not allowed"}` with `Content-Type: application/json`, consistent with every other error in the API.
- **`GET /keys` list behavior documented in spec**: Clarify that the Vault LIST returns one level of the key hierarchy at a time (intermediate prefixes end with `/`); callers must use `?prefix=<path>/` to drill down to leaf-level key names.
- **Key deletion explicitly ruled out in spec**: Add a requirement stating that no DELETE endpoint is exposed for keys, intentionally, to preserve Vault's audit trail and key history.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `rest-gateway`: Three requirement-level changes — JSON-only error format for 405, specified list traversal semantics for `GET /keys`, and explicit no-delete policy for the keys resource.

## Impact

- `internal/gateway/gateway.go`: register a `MethodNotAllowedHandler` on the `http.ServeMux` (or wrap it)
- `openspec/specs/rest-gateway/spec.md`: add/update requirements for the three items above
- `docs/`: swagger docs regenerated after handler change (no new endpoints, no request/response shape changes)
- No breaking changes to existing callers
