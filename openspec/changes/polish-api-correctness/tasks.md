## 1. Fix CLI Cosmos sign error shadowing

- [ ] 1.1 In `cmd/kms-wrapper/root.go` around the `kms-wrapper sign cosmos` command (the `switch sign_mode { case "DIRECT": ... }` block near line 225-236), declare `var err error` BEFORE the switch. Inside `case "DIRECT":`, rename the base64-decode local to `decErr` (e.g. `doc, decErr := base64.StdEncoding.DecodeString(signDoc)`); on `decErr != nil`, assign `err = fmt.Errorf("decode sign-doc: %w", decErr)` and break out of the switch. `SignDirect`/`SignAmino` then write into the outer `err`.
- [ ] 1.2 After the switch, on `err != nil`, return `fmt.Errorf("sign cosmos: %w", err)` and ensure no `Println` of `sig`/`pub` happens on the error path. Confirm Cobra surfaces the returned error to stderr and non-zero exit.
- [ ] 1.3 Add `cmd/kms-wrapper/sign_cosmos_test.go` (or extend existing) with a fake `Signer` that returns an error; assert non-zero exit and that stdout is empty.

## 2. Canonical AMINO JSON via cosmos-sdk

- [ ] 2.1 Import cosmos-sdk's `SortJSON` (path: `github.com/cosmos/cosmos-sdk/types`, function `types.SortJSON`). Confirm it is in the existing module graph; if not, add it (the project already vendors cosmos-sdk per `go.mod:27`).
- [ ] 2.2 In `internal/signer/cosmos/cosmos.go:SignAmino`, replace the `json.Decode(UseNumber)` → `json.Marshal` block with: `sorted, err := types.SortJSON(rawSignDocBytes)` and use `sorted` as the SHA-256 input. Remove the now-unused helpers if any.
- [ ] 2.3 Add a duplicate-key detector: scan the input bytes with a small recursive function (or vendor `github.com/tidwall/gjson` and walk `gjson.ParseBytes(...).ForEach(...)`); on duplicate detection at any nesting level, return `fmt.Errorf("duplicate key in amino sign doc: %s", key)` before canonicalisation.
- [ ] 2.4 Test: `cosmos_test.go` adds a fixture `TestSignAminoCanonicalMatchesCosmosSDK` that signs the same input via this function AND via a direct call to `types.SortJSON` + SHA-256 + Vault; assert the two signatures verify against the same pubkey. Add `TestSignAminoRejectsDuplicateKeys`.

## 3. Propagate swallowed handler errors

- [ ] 3.1 In `internal/gateway/gateway.go` line ~359, replace `_ = tx.UnmarshalBinary(out)` with: `if err := tx.UnmarshalBinary(out); err != nil { writeError(w, http.StatusInternalServerError, "decode signed tx: "+err.Error()); return }`. Confirm `out` (the signed-tx bytes from Vault) is still in scope.
- [ ] 3.2 In `internal/gateway/gateway.go` line ~452, replace `addr, _ := DeriveCosmosAddressFromCompressed(...)` with: `addr, err := DeriveCosmosAddressFromCompressed(...); if err != nil { writeError(w, http.StatusInternalServerError, "derive cosmos address: "+err.Error()); return }`.
- [ ] 3.3 Add table tests in `gateway_test.go`:
  - `TestSignEVMRawTxUnmarshalFailure` — stub the signer to return non-RLP bytes; assert HTTP 500 with the expected error.
  - `TestSignCosmosAddressDerivationFailure` — stub the signer to return a malformed pubkey; assert HTTP 500.

## 4. Dual-mount `/v1/` prefix

- [ ] 4.1 In `internal/gateway/gateway.go:routes()`, refactor route registration into a slice: `var routesList = []struct{ method, pattern string; handler http.Handler }{ ... }` listing every current route.
- [ ] 4.2 Loop over `routesList` and register each entry twice: once at `entry.pattern`, once at `"/v1" + entry.pattern`. The handler is the same in both cases.
- [ ] 4.3 Update all swag annotations on handlers in `internal/gateway/*.go` so `@Router` paths use `/v1/...` form. The `cmd/swagger-postprocess/main.go` step (run via `make swagger`) must produce a `paths` object whose keys all start with `/v1/`.
- [ ] 4.4 Add middleware that, on the bare-path code path, sets response headers `Deprecation: true` and `Sunset: <RFC1123 date ≥ 90 days out>`. Achieve this by wrapping the bare-form registration in a `withDeprecation` middleware that calls through to the same handler but adds the headers.
- [ ] 4.5 Test: `TestRoutesDualMounted` — assert `POST /sign/evm` and `POST /v1/sign/evm` produce identical responses; assert the bare path's response includes `Deprecation` and `Sunset` headers and the `/v1/` path's response does not.

## 5. Typed `Signature` field + EVM `oneOf` discriminator

- [ ] 5.1 In `pkg/types/types.go`, replace `Signature any` in the EVM response. Split into two response types: `EVMSignRawTxResponse{SignedTx, SignatureParts}` and `EVMSignPersonalResponse{Signature string}`. Update swag tags accordingly.
- [ ] 5.2 Add a required `Type string` field on the EVM sign request (or, if preserving the current "fat union" struct, add a `Type string` discriminator field and have the handler dispatch on it). Update swag annotations: `@Description type — one of raw_tx | personal_message | eip712_digest`.
- [ ] 5.3 Replace the `countNonEmpty` dispatch in `internal/gateway/gateway.go` with `switch req.Type { case "raw_tx": ...; case "personal_message": ...; case "eip712_digest": ... default: writeError(400, "type is required and must be one of raw_tx|personal_message|eip712_digest") }`.
- [ ] 5.4 Add post-processing in `cmd/swagger-postprocess/main.go` to inject the `discriminator` block on the EVM request `oneOf` (swag does not emit this natively). The injection writes to the generated `docs/swagger.json` / `docs/swagger.yaml`.
- [ ] 5.5 Regenerate docs: `make swagger`. Confirm `docs/swagger.json` has `paths./v1/sign/evm.post.requestBody.content.application/json.schema.discriminator.propertyName == "type"` and `mapping` entries point to the three variant schemas. Confirm the response shows `oneOf` over the two response types (no `{}` schema anywhere).
- [ ] 5.6 Tests: `TestEVMSignRequestDiscriminatorMissing` (400), `TestEVMSignRequestDiscriminatorMismatch` (e.g. `type=raw_tx` but no `raw_tx` field — 400).

## 6. Pagination on `GET /keys`

- [ ] 6.1 In `internal/gateway/gateway.go` add `limit` and `cursor` parsing in `handleListKeys`: `limit` default 100, max 1000 (clamp + 400 on out-of-range — pick "clamp" per design; update spec scenario accordingly).
- [ ] 6.2 Cursor format: base64-encode a small struct `{Prefix string; Offset int}` (use `encoding/base64.URLEncoding`). Decode on read; reject malformed with 400 `"invalid cursor"`.
- [ ] 6.3 Implementation: fetch the full `vault.Client.ListKeys(ctx, prefix)` result, slice `[offset:offset+limit]`, set `next_cursor = encode({prefix, offset+limit})` if more entries remain; else empty string. Document in design.md as a temporary client-side pagination implementation.
- [ ] 6.4 Add `next_cursor string` field to `pkg/types.KeyListResponse` with `json:"next_cursor"` tag.
- [ ] 6.5 Tests: `TestListKeysPagination` (5 entries, limit=2 → 3 pages with non-empty/empty next_cursor pattern), `TestListKeysInvalidCursor` (400), `TestListKeysLimitClamp` (limit=99999 → clamped to 1000).

## 7. `Allow` header on 405

- [ ] 7.1 In `internal/gateway/gateway.go:methodNotAllowedRewriter.WriteHeader`, before clearing `Content-Length`: capture `m.inner.Header().Get("Allow")` into a local; after clearing `Content-Length`, re-set `m.inner.Header().Set("Allow", capturedAllow)` (the rewrite of the body to JSON does not touch the headers map directly — confirm).
- [ ] 7.2 Test: `TestMethodNotAllowedIncludesAllowHeader` — send `DELETE /keys`; assert response has `Allow:` header containing `GET` and `POST`.

## 8. 201 on first `POST /keys`

- [ ] 8.1 In `internal/gateway/gateway.go:handleCreateKey`, after determining `alreadyExisted`, set `if !alreadyExisted { w.WriteHeader(http.StatusCreated) } else { w.WriteHeader(http.StatusOK) }` BEFORE writing the JSON body. Ensure `writeJSON` does not pre-emptively call `WriteHeader(200)` (audit it; if it does, accept a status parameter).
- [ ] 8.2 Update the existing test `TestKeysCreate` to assert status 201 on first create and 200 on idempotent re-create.

## 9. Verification and archive

- [ ] 9.1 `go test ./...` passes.
- [ ] 9.2 `make swagger-check` shows the regenerated docs (with `discriminator`, `/v1/` paths, typed response) match what's committed.
- [ ] 9.3 Manual: hit `POST /v1/sign/cosmos` with a fixture amino doc that has duplicate keys → expect HTTP 400. Hit `POST /v1/keys` for a fresh path → expect HTTP 201; hit again → expect HTTP 200.
- [ ] 9.4 `openspec validate polish-api-correctness --strict` passes.
- [ ] 9.5 Run `openspec archive-change polish-api-correctness` once implementation is complete to merge deltas into `openspec/specs/{rest-gateway,cosmos-signer,cli,api-docs}/spec.md`.
