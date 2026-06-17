## MODIFIED Requirements
### Requirement: Create key endpoint
The REST gateway SHALL expose `POST /keys` (and equivalently `POST /v1/keys`) accepting a JSON body `{"path": "<key-path>", "chains": ["evm" | "cosmos", ...]}` where `<key-path>` matches the format defined by the `key-path-policy` capability and `chains` is a non-empty subset of `{"evm", "cosmos"}`. The handler SHALL create a secp256k1 key at that path, persist the canonicalized chains tag via the plugin, then derive and return the public key (hex) and only the addresses corresponding to the enabled chains. The operation SHALL be idempotent. On first create the response status SHALL be HTTP 201; on subsequent idempotent re-create with matching `chains` the response status SHALL be HTTP 200. The response body in both cases SHALL include `"already_existed": <bool>` matching the status (`false` for 201, `true` for 200) and `"chains": [<canonical-sorted-list>]`.

`evm_address` SHALL be present in the response if and only if `chains` contains `"evm"`. `cosmos_address` SHALL be present if and only if `chains` contains `"cosmos"`. `public_key_hex` SHALL always be present.

#### Scenario: First create returns 201 with both chains
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice", "chains": ["evm", "cosmos"]}` to `/keys` and that path does not yet exist
- **THEN** the gateway responds with HTTP 201, `Content-Type: application/json`, and body `{"path": "proj-a/prod/alice", "public_key_hex": "<hex>", "evm_address": "0x<EIP-55>", "cosmos_address": "cosmos1<bech32>", "chains": ["cosmos", "evm"], "already_existed": false}`

#### Scenario: First create with evm-only omits cosmos_address
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice", "chains": ["evm"]}` to `/keys` and that path does not yet exist
- **THEN** the gateway responds with HTTP 201 and body `{"path": "proj-a/prod/alice", "public_key_hex": "<hex>", "evm_address": "0x<EIP-55>", "chains": ["evm"], "already_existed": false}` and the response object does NOT contain a `cosmos_address` field

#### Scenario: First create with cosmos-only omits evm_address
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice", "chains": ["cosmos"]}` to `/keys` and that path does not yet exist
- **THEN** the gateway responds with HTTP 201 and body `{"path": "proj-a/prod/alice", "public_key_hex": "<hex>", "cosmos_address": "cosmos1<bech32>", "chains": ["cosmos"], "already_existed": false}` and the response object does NOT contain an `evm_address` field

#### Scenario: Idempotent re-create with matching chains returns 200
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice", "chains": ["evm"]}` to `/keys` and a key already exists at that path with `chains=[evm]`
- **THEN** the gateway responds with HTTP 200 and the same body shape as the original create, with `already_existed: true`

#### Scenario: Idempotent re-create with mismatched chains returns 400
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice", "chains": ["cosmos"]}` to `/keys` and a key already exists at that path with `chains=[evm]`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "chains mismatch on idempotent create"}`

#### Scenario: Missing chains field
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice"}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "chains is required and must be a non-empty subset of [evm, cosmos]"}`

#### Scenario: Empty chains list
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice", "chains": []}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "chains is required and must be a non-empty subset of [evm, cosmos]"}`

#### Scenario: Unknown chain in list
- **WHEN** an authorized client POSTs `{"path": "proj-a/prod/alice", "chains": ["evm", "solana"]}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "chains is required and must be a non-empty subset of [evm, cosmos]"}` and the key is NOT created

#### Scenario: Missing path field
- **WHEN** an authorized client POSTs `{"chains": ["evm"]}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "path is required"}`

#### Scenario: Invalid key path format
- **WHEN** an authorized client POSTs `{"path": "Proj A/Prod/Alice", "chains": ["evm"]}` to `/keys`
- **THEN** the gateway responds with HTTP 400 and body whose `error` field contains the validation message from `ValidateKeyPath`

#### Scenario: Malformed JSON body
- **WHEN** an authorized client POSTs a body that is not valid JSON to `/keys`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "invalid JSON"}`

#### Scenario: Vault permission denied
- **WHEN** Vault rejects the create call with a permission-denied error
- **THEN** the gateway responds with HTTP 403 and body `{"error": "permission denied"}`

---

### Requirement: Show key endpoint
The REST gateway SHALL expose `GET /keys/info?path=<key-path>` returning the public key (hex), the `chains` tag, and only the derived addresses corresponding to the enabled chains. When the key does not exist, the gateway SHALL respond with HTTP 404. The `path` query parameter is required and SHALL be validated against the `key-path-policy` format before any Vault call.

#### Scenario: Show existing dual-chain key
- **WHEN** an authorized client GETs `/keys/info?path=proj-a/prod/alice` and that key exists in Vault with `chains=[evm, cosmos]`
- **THEN** the gateway responds with HTTP 200, `Content-Type: application/json`, and body `{"path": "proj-a/prod/alice", "public_key_hex": "<hex>", "evm_address": "0x<EIP-55>", "cosmos_address": "cosmos1<bech32>", "chains": ["cosmos", "evm"]}`

#### Scenario: Show evm-only key omits cosmos_address
- **WHEN** an authorized client GETs `/keys/info?path=proj-a/prod/alice` and that key exists with `chains=[evm]`
- **THEN** the gateway responds with HTTP 200 and body `{"path": "proj-a/prod/alice", "public_key_hex": "<hex>", "evm_address": "0x<EIP-55>", "chains": ["evm"]}` and the response object does NOT contain a `cosmos_address` field

#### Scenario: Key not found
- **WHEN** an authorized client GETs `/keys/info?path=proj-a/prod/ghost` and no such key exists in Vault
- **THEN** the gateway responds with HTTP 404 and body `{"error": "key not found: proj-a/prod/ghost"}`

#### Scenario: Missing path query parameter
- **WHEN** an authorized client GETs `/keys/info` (no `path` query)
- **THEN** the gateway responds with HTTP 400 and body `{"error": "path is required"}`

#### Scenario: Invalid key path format
- **WHEN** an authorized client GETs `/keys/info?path=BadPath`
- **THEN** the gateway responds with HTTP 400 and body whose `error` field contains the validation message from `ValidateKeyPath`

#### Scenario: Vault permission denied on show
- **WHEN** Vault rejects the read with a permission-denied error
- **THEN** the gateway responds with HTTP 403 and body `{"error": "permission denied"}`

---

### Requirement: List keys endpoint
The REST gateway SHALL expose `GET /keys` (and equivalently `GET /v1/keys`) accepting `?prefix=<prefix>`, `?limit=<n>` (default 100, max 1000), and `?cursor=<opaque>` query parameters. The response SHALL be `{"keys": [...], "count": <n>, "next_cursor": "<opaque>" | ""}`. Each entry in `keys` SHALL be `{"path": "<p>", "chains": [<canonical-sorted-list>]}` — the `chains` tag is part of every list entry so callers can filter by capability without an additional `/keys/info` round-trip. When more results exist than fit in `limit`, `next_cursor` SHALL be non-empty; clients pass it back via `?cursor=` to fetch the next page. When no more results exist, `next_cursor` SHALL be the empty string.

#### Scenario: List entries include chains
- **WHEN** an authorized client GETs `/keys?prefix=proj-a/&limit=2` and the underlying list contains keys with mixed tags
- **THEN** the gateway responds with HTTP 200 and each entry in `keys` carries a `chains` array reflecting that key's persisted tag

#### Scenario: List with limit returns up to limit entries
- **WHEN** an authorized client GETs `/keys?prefix=proj-a/&limit=2` and the underlying list has 5 entries
- **THEN** the gateway responds with HTTP 200, `"count": 2`, and `next_cursor` is non-empty

#### Scenario: Cursor pagination drives next page
- **WHEN** the client follows up with `GET /keys?prefix=proj-a/&limit=2&cursor=<previous-next-cursor>`
- **THEN** the gateway responds with the next 2 entries (or fewer if fewer remain); `next_cursor` continues to be non-empty until the last page

#### Scenario: Empty `next_cursor` on final page
- **WHEN** the page returned contains the last of the available entries
- **THEN** `next_cursor` is the empty string

#### Scenario: Invalid limit
- **WHEN** `?limit=99999` is passed
- **THEN** the gateway clamps to the maximum (1000) and returns the clamped page; values below 1 SHALL also be clamped to the default of 100

#### Scenario: Tampered cursor
- **WHEN** `?cursor=not-a-valid-cursor` is passed
- **THEN** the gateway returns HTTP 400 with `{"error": "invalid cursor"}`

#### Scenario: List with no prefix
- **WHEN** an authorized client GETs `/keys` (no query)
- **THEN** the gateway calls the underlying list with empty prefix and returns the first page (`limit` default 100)

---

### Requirement: Sign EVM transaction endpoint
The gateway SHALL expose `POST /sign/evm` (and equivalently `POST /v1/sign/evm`) accepting a JSON body with `key_path`, an explicit `type` discriminator field whose value is one of `raw_tx`, `personal_message`, `eip712_digest`, and the payload field matching the discriminator: `raw_tx` (hex RLP), `personal_message` (hex bytes), or `eip712_digest` (hex 32 bytes). `chain_id` SHALL be required when `type=raw_tx` (used to scope the secp256k1 signature to a specific EVM chain) and SHALL be optional/ignored for `personal_message` and `eip712_digest` (which do not bind to a chain at the signature layer).

Before dispatching to the signer, the gateway SHALL fetch the target key's `chains` tag and reject with HTTP 403 if `"evm"` is not in the tag. The error body SHALL be `{"error": "key <path> not authorized for evm signing (allowed chains: [<sorted-list>])"}`. The gateway SHALL emit a `slog` warn log carrying `key_path`, `attempted_chain="evm"`, and `allowed_chains`. The gateway SHALL NOT call the signer when the chain check fails.

The handler SHALL dispatch on `type`; payload fields not matching the discriminator SHALL be ignored. The response SHALL be one of two typed shapes based on the request variant:
- `raw_tx` → `{"signed_tx": "0x...", "signature_parts": {"r": "...", "s": "...", "v": N}}`
- `personal_message` or `eip712_digest` → `{"signature": "0x<65-byte-hex>"}`

When the gateway cannot decode the signed transaction bytes (e.g. `tx.UnmarshalBinary` fails on the `raw_tx` path), the handler SHALL respond with HTTP 500 and body `{"error": "decode signed tx: <message>"}` and SHALL NOT return a partially-populated response.

#### Scenario: Sign raw EVM transaction on evm-tagged key
- **WHEN** `POST /sign/evm` is called with `{"type": "raw_tx", "key_path": "payment/prod/alice", "chain_id": 1, "raw_tx": "0x..."}` and the key's tag includes `evm`
- **THEN** the gateway returns HTTP 200 with `{"signed_tx": "0x...", "signature_parts": {"r": "...", "s": "...", "v": N}}` and NO field named `signature`

#### Scenario: EVM sign on cosmos-only key returns 403
- **WHEN** `POST /sign/evm` is called with `{"type": "personal_message", "key_path": "payment/prod/alice", "personal_message": "0x6869"}` and the key has `chains=[cosmos]`
- **THEN** the gateway returns HTTP 403 with body `{"error": "key payment/prod/alice not authorized for evm signing (allowed chains: [cosmos])"}` and the signer is NOT invoked

#### Scenario: Sign personal message on dual-tagged key
- **WHEN** `POST /sign/evm` is called with `{"type": "personal_message", "key_path": "...", "personal_message": "0x..."}` and the key has `chains=[cosmos, evm]`
- **THEN** the gateway returns HTTP 200 with `{"signature": "0x<65-byte-hex>"}`

#### Scenario: Missing discriminator
- **WHEN** `POST /sign/evm` is called without a `type` field
- **THEN** the gateway returns HTTP 400 with `{"error": "type is required and must be one of raw_tx|personal_message|eip712_digest"}`

#### Scenario: Discriminator/payload mismatch
- **WHEN** `POST /sign/evm` is called with `{"type": "raw_tx", "personal_message": "0x...", "key_path": "..."}` (raw_tx field absent)
- **THEN** the gateway returns HTTP 400 with `{"error": "raw_tx is required when type=raw_tx"}`

#### Scenario: UnmarshalBinary failure surfaces as 500
- **WHEN** the signed-tx bytes returned by the signer fail to `UnmarshalBinary`
- **THEN** the gateway returns HTTP 500 and body `{"error": "decode signed tx: <message>"}` — no zero-valued `signature_parts` is returned

#### Scenario: Vault signing error
- **WHEN** Vault returns an error (e.g. key not found, policy denied)
- **THEN** the gateway returns HTTP 500 with `{"error": "<vault-error-message>"}` — never exposing the Vault token or key material

---

### Requirement: Sign Cosmos transaction endpoint
The gateway SHALL expose `POST /sign/cosmos` (and equivalently `POST /v1/sign/cosmos`) accepting a JSON body with `key_path`, `hrp`, `sign_mode`, and `sign_doc` (base64-encoded protobuf or amino JSON string).

Before dispatching to the signer, the gateway SHALL fetch the target key's `chains` tag and reject with HTTP 403 if `"cosmos"` is not in the tag. The error body SHALL be `{"error": "key <path> not authorized for cosmos signing (allowed chains: [<sorted-list>])"}`. The gateway SHALL emit a `slog` warn log carrying `key_path`, `attempted_chain="cosmos"`, and `allowed_chains`. The gateway SHALL NOT call the signer when the chain check fails.

When the gateway cannot derive the Cosmos bech32 address from the public key (`DeriveCosmosAddressFromCompressed` returns an error), the handler SHALL respond with HTTP 500 and body `{"error": "derive cosmos address: <message>"}` and SHALL NOT return a partially-populated response.

#### Scenario: Sign DIRECT mode on cosmos-tagged key
- **WHEN** `POST /sign/cosmos` is called with `{"key_path": "payment/prod/alice", "hrp": "mantra", "sign_mode": "DIRECT", "sign_doc": "<base64>"}` and the key has `chains` containing `cosmos`
- **THEN** the gateway returns HTTP 200 with `{"signature": "<base64>", "pub_key": "<base64-compressed-pubkey>", "cosmos_address": "mantra1..."}`

#### Scenario: Cosmos sign on evm-only key returns 403
- **WHEN** `POST /sign/cosmos` is called with `{"key_path": "payment/prod/alice", "hrp": "mantra", "sign_mode": "DIRECT", "sign_doc": "<base64>"}` and the key has `chains=[evm]`
- **THEN** the gateway returns HTTP 403 with body `{"error": "key payment/prod/alice not authorized for cosmos signing (allowed chains: [evm])"}` and the signer is NOT invoked

#### Scenario: Sign AMINO mode on dual-tagged key
- **WHEN** `POST /sign/cosmos` is called with `{"sign_mode": "AMINO_JSON", "sign_doc": "<amino-json-string>"}` against a `chains=[cosmos, evm]` key
- **THEN** the gateway returns HTTP 200 with the amino-compatible signature

#### Scenario: Unknown sign mode
- **WHEN** `sign_mode` is not one of `DIRECT` or `AMINO_JSON`
- **THEN** the gateway returns HTTP 400 with `{"error": "unsupported sign_mode"}`

#### Scenario: Address derivation failure surfaces as 500
- **WHEN** `DeriveCosmosAddressFromCompressed` fails on the compressed pubkey returned by Vault
- **THEN** the gateway returns HTTP 500 and body `{"error": "derive cosmos address: <message>"}` and `cosmos_address` is NOT zero-valued in any returned response

## ADDED Requirements
### Requirement: Update key chains endpoint
The REST gateway SHALL expose `PATCH /keys/{path}` (and equivalently `PATCH /v1/keys/{path}`) accepting a JSON body `{"add_chains": ["evm" | "cosmos", ...]}` where the value is a non-empty subset of `{"evm", "cosmos"}`. The handler SHALL call the plugin's `update-chains` operation, which performs an expand-only union with the persisted `chains` tag. The handler SHALL return HTTP 200 with body `{"path": "<p>", "chains": [<canonical-sorted-list>]}` reflecting the new tag.

The handler SHALL reject any payload that contains fields other than `add_chains` (specifically `chains` or `remove_chains`) with HTTP 400 and body `{"error": "only add_chains is supported"}`. The handler SHALL reject an empty or missing `add_chains` field with HTTP 400 and body `{"error": "add_chains is required and must be a non-empty subset of [evm, cosmos]"}`. The handler SHALL reject unknown chain values with the same 400 body.

The endpoint SHALL be wrapped by the same bearer-token middleware that protects the other `/keys` routes and SHALL be subject to the per-process rate limiter. After a successful update, the gateway SHALL invalidate the per-process chains cache for the affected key so subsequent sign calls observe the new tag.

#### Scenario: Expand from evm to evm+cosmos
- **WHEN** an authorized client PATCHes `/keys/payment/prod/alice` with `{"add_chains": ["cosmos"]}` and the key currently has `chains=[evm]`
- **THEN** the gateway responds with HTTP 200 and body `{"path": "payment/prod/alice", "chains": ["cosmos", "evm"]}` and the next `POST /sign/cosmos` against that key succeeds with HTTP 200

#### Scenario: Idempotent add is a no-op
- **WHEN** an authorized client PATCHes `/keys/payment/prod/alice` with `{"add_chains": ["evm"]}` and the key currently has `chains=[cosmos, evm]`
- **THEN** the gateway responds with HTTP 200 and body `{"path": "payment/prod/alice", "chains": ["cosmos", "evm"]}` unchanged

#### Scenario: Remove attempt is rejected
- **WHEN** an authorized client PATCHes `/keys/payment/prod/alice` with `{"remove_chains": ["cosmos"]}`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "only add_chains is supported"}` and the persisted tag is unchanged

#### Scenario: Replace attempt is rejected
- **WHEN** an authorized client PATCHes `/keys/payment/prod/alice` with `{"chains": ["evm"]}`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "only add_chains is supported"}` and the persisted tag is unchanged

#### Scenario: Empty add_chains rejected
- **WHEN** an authorized client PATCHes `/keys/payment/prod/alice` with `{"add_chains": []}`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "add_chains is required and must be a non-empty subset of [evm, cosmos]"}`

#### Scenario: Unknown chain rejected
- **WHEN** an authorized client PATCHes `/keys/payment/prod/alice` with `{"add_chains": ["solana"]}`
- **THEN** the gateway responds with HTTP 400 and body `{"error": "add_chains is required and must be a non-empty subset of [evm, cosmos]"}`

#### Scenario: Missing bearer token
- **WHEN** a client PATCHes `/keys/payment/prod/alice` with no `Authorization` header
- **THEN** the gateway responds with HTTP 401 and body `{"error": "unauthorized"}` before reaching the handler

#### Scenario: Cache invalidation after PATCH
- **WHEN** a successful PATCH expands a key's chains and a subsequent sign call targets the newly-added chain within the same process
- **THEN** the sign call observes the new tag (does not return 403 from a stale cache entry)
