## Why

The deep review surfaced eight correctness and API-shape defects that, while individually small, together undermine integrator confidence and risk silently-wrong outputs. The most serious are: the CLI Cosmos sign path swallows errors via a Go scoping mistake (so failures look like success with empty output); the AMINO sign path "canonicalises" JSON with Go's `json.Marshal` whose key-sort rules do not match Cosmos amino canonical form (so the signature can cover a different document than the operator intended); and two REST handlers silently truncate response fields when downstream parsing fails. The remaining medium-severity items (`/v1` prefix, `oneOf` discriminator, list pagination, `Allow` header on 405, `201` on first create) are the kinds of items that block clean OpenAPI codegen and HTTP-aware proxies.

## What Changes

- **Fix CLI Cosmos sign error shadowing.** `cmd/kms-wrapper/root.go:225-236` declares an inner `err` inside a `case` block, so `SignDirect` failures go out of scope and the CLI prints zero-bytes as success. Reshape to use the outer `err`.
- **Use cosmos-sdk canonical JSON for `SignAmino`.** Replace the `json.Decode` → `json.Marshal` round-trip in `internal/signer/cosmos/cosmos.go:SignAmino` with `types.SortJSON` from `github.com/cosmos/cosmos-sdk/types` (or `legacy.Cdc.MarshalJSON`) so the signed document matches what the chain re-derives. Reject inputs with duplicate JSON keys explicitly.
- **Propagate swallowed handler errors.** `internal/gateway/gateway.go:359` (`_ = tx.UnmarshalBinary(out)`) and `:452` (`addr, _ := DeriveCosmosAddressFromCompressed(...)`) become non-discarding; on error the handler returns HTTP 500 with a structured error. Add explicit table tests for both failure paths.
- **Add `/v1/` API prefix.** All current routes (`/sign/*`, `/keys`, `/keys/info`, `/health`) SHALL be mounted under `/v1/` as well, served alongside the bare routes for one minor-version cycle. The OpenAPI spec advertises `/v1/` paths.
- **Type the `SignResponse.Signature` field.** Replace `Signature any` (which renders as `{}` in OpenAPI) with a typed `string` for the personal-message/eip712 variants OR remove it in favour of the structured `signature_parts`. Update the spec and clients accordingly.
- **Add an OpenAPI `discriminator` to the EVM sign-request `oneOf`.** Introduce an explicit `type` discriminator field (`raw_tx | personal_message | eip712_digest`) so codegen tools can pick the variant deterministically.
- **Bounded list response + cursor pagination on `GET /keys`.** Add a `?limit=` (default 100, max 1000) and an opaque `?cursor=` so a large tenant cannot blow up the response or starve the shared rate budget.
- **`Allow` header on 405 responses.** Preserve and emit the `Allow` header that `http.ServeMux` sets on 405; current `methodNotAllowedRewriter` clobbers it. RFC 7231 §6.5.5 makes this mandatory.
- **`POST /keys` returns 201 on first create.** Return HTTP 201 on the first-create path, 200 on the idempotent re-create path. The response body's `already_existed` boolean already distinguishes the two, so this is purely a status-code-correctness change.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `rest-gateway`: `/v1/` prefix, typed `Signature`, `Allow` header on 405, propagated handler errors, paginated `GET /keys`, `201` on first POST /keys.
- `cosmos-signer`: canonical-JSON enforcement on AMINO mode, explicit rejection of duplicate keys.
- `cli`: outer-scope error handling for `kms-wrapper sign cosmos`; non-zero exit on signer failure.
- `api-docs`: `discriminator` on the EVM payload `oneOf`; documented `/v1/` paths; updated `SignResponse` schema.

## Impact

- `internal/signer/cosmos/cosmos.go`: import cosmos-sdk's `types.SortJSON` (or `legacy.Cdc.MarshalJSON`) from `github.com/cosmos/cosmos-sdk/types`; remove the home-grown re-marshal.
- `internal/gateway/gateway.go`: error propagation in `signEVM` / `signCosmos`; `Allow` header in `methodNotAllowedRewriter`; pagination logic on `/keys`; `/v1/` route-mount loop; 201/200 split on POST /keys.
- `pkg/types/types.go`: typed `Signature` field; explicit discriminator field on EVM sign request types; `KeyListResponse` carries `next_cursor`.
- `cmd/kms-wrapper/root.go`: fix the cosmos sign error scoping; non-zero exit on any signing failure.
- `cmd/swagger-postprocess/main.go`: emit `discriminator` block on EVM payload `oneOf`.
- `docs/swagger.json` / `docs/swagger.yaml`: regenerated via `make swagger` (these are the actual artifact filenames produced by `swag` + `cmd/swagger-postprocess`; the repo does not emit `openapi.json`/`openapi.yaml`).
- Test additions: `gateway_test.go` (UnmarshalBinary error, cosmos address derivation error, 405 `Allow` header, pagination); `cosmos_test.go` (duplicate-key rejection, canonical-JSON match against cosmos-sdk reference); `root_test.go` (CLI cosmos sign failure exit code).
- **Non-breaking** for HTTP callers: bare paths continue to work alongside `/v1/`. Codegen clients regenerating against the new spec will see `Signature` as a string instead of free-form object — this is a typing improvement, not a breaking wire change. Cosmos amino-sign output may change for inputs that were not already canonical; this is the bug fix and is intentional.
