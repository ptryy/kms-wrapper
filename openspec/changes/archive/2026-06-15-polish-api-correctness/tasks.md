## 1. CLI Cosmos sign — outer-scope `err`

- [x] 1.1 Restructure `signCosmosCmd` `RunE` so the DIRECT case's base64 decode uses a separate `decErr` local; signer error writes to the outer `err`.
- [x] 1.2 Wrap signer error with `fmt.Errorf("sign cosmos: %w", err)`; ensure no `Println` on the error path.
- [x] 1.3 Regression test in `sign_cosmos_test.go`. (Decode error and unreachable-Vault paths both assert no `signature:` line in stdout.)

## 2. AMINO canonicalisation via `SortJSON`-equivalent + duplicate-key detection

- [x] 2.1 Use cosmos-sdk-equivalent canonicalisation. Implementation note: the full `cosmos-sdk/types` package transitively pulls in cometbft; an inline 6-line `canonicaliseJSON` (Unmarshal→Marshal round-trip — byte-identical to `types.SortJSON`) keeps the chain-verify contract without the dep graph.
- [x] 2.2 Replace `SignAmino`'s body with `canonicaliseJSON` + SHA-256 + Vault.
- [x] 2.3 Add duplicate-key detector that scans the raw bytes via a streaming `json.Decoder`; on duplicate returns `fmt.Errorf("...: %w", apptypes.ErrBadRequest)`.
- [x] 2.4 Tests: `TestSignAminoCanonicalMatchesUnmarshalMarshalRoundtrip` + `TestSignAminoRejectsDuplicateKeys`.
- [x] 2.5 Gateway `signCosmos` maps `errors.Is(err, ErrBadRequest)` to HTTP 400. Tests `TestSignCosmosDuplicateKeysReturns400` + `TestSignCosmosGenericSignerErrorReturns500`.

## 3. Propagate swallowed handler errors

- [x] 3.1 `tx.UnmarshalBinary(out)` error now surfaces as HTTP 500 with `"decode signed tx: <message>"`.
- [x] 3.2 `DeriveCosmosAddressFromCompressed` error in `signCosmos` now surfaces as HTTP 500 with `"derive cosmos address: <message>"`.
- [x] 3.3 Table tests added under `polish_test.go`.

## 4. `/v1/` dual-mount + RFC 8594 deprecation headers

- [x] 4.1 `routes()` refactored to iterate a `routeEntry` slice.
- [x] 4.2 Each entry is registered at the bare path AND at `/v1` + bare path.
- [x] 4.3 `@Router` annotations updated to `/v1/...` form. (Doc regeneration deferred to manual `make swagger` — `swag` CLI required.)
- [x] 4.4 `withDeprecation` middleware sets `Deprecation: true` and `Sunset: <RFC1123 ≥ 90d>` on bare-path responses only.
- [x] 4.5 `TestRoutesDualMounted` asserts identical bodies and presence/absence of the headers.

## 5. Typed Signature + discriminator

- [x] 5.1 `Signature any` removed; `EVMSignRawTxResponse` and `EVMSignPersonalResponse` split.
- [x] 5.2 `EVMSignRequest.Type` discriminator added (`raw_tx`/`personal_message`/`eip712_digest`).
- [x] 5.3 `countNonEmpty` replaced by `switch req.Type`; per-variant dispatch helpers.
- [x] 5.4 `cmd/swagger-postprocess/main.go` injects `discriminator: { propertyName: "type", mapping: ... }` on the EVM `oneOf`.
- [ ] 5.5 Regenerate docs via `make swagger`. (Manual; `swag` CLI required; tests pass against existing in-tree docs.)
- [x] 5.6 `TestEVMDiscriminatorMissing` (400) + `TestEVMDiscriminatorMismatch` (400).

## 6. Cursor pagination on `GET /keys`

- [x] 6.1 `?limit=` parsed (default 100, max clamps to 1000).
- [x] 6.2 `?cursor=` is base64 URL-encoded `{prefix, offset}` JSON; invalid → 400.
- [x] 6.3 Client-side pagination implementation documented inline.
- [x] 6.4 `KeyListResponse.NextCursor` field added.
- [x] 6.5 `TestListKeysPagination`, `TestListKeysInvalidCursor`, `TestListKeysLimitClamp` cover the cases.

## 7. 405 `Allow` header preservation

- [x] 7.1 `methodNotAllowedRewriter.WriteHeader` now snapshots and re-sets `Allow` before writing the JSON body.
- [x] 7.2 `TestMethodNotAllowedIncludesAllowHeader` asserts `Allow: GET, POST` survives the 405 rewrite.

## 8. HTTP 201 on first POST /keys

- [x] 8.1 `createKey` calls `writeJSONStatus(201)` on first create, `writeJSONStatus(200)` on idempotent re-create.
- [x] 8.2 Existing `TestCreateKeyHappyPath` updated to expect 201; `TestKeysIdempotentReturns200` covers the 200 path.

## 9. Verification and archive

- [x] 9.1 `go test ./...` passes.
- [ ] 9.2 `make swagger-check` shows the regenerated docs match (deferred — requires `swag` CLI run).
- [ ] 9.3 Manual smoke (duplicate-keys 400; 201 on first create) deferred.
- [ ] 9.4 `openspec validate polish-api-correctness --strict` (run after all four apply).
- [ ] 9.5 `openspec archive-change polish-api-correctness` (pending verification).
