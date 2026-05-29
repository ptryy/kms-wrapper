## ADDED Requirements

### Requirement: Partial Cosmos multisig signature — SIGN_MODE_DIRECT
The system SHALL accept a `SignDoc` (protobuf-encoded, base64), the gateway's key path, a `signer_index` (zero-based position of the gateway's key in the multisig account's ordered public key list), and return a single `SignatureV2` with `SingleSignatureData` of mode `SIGN_MODE_DIRECT`. The system SHALL NOT assemble the full `MultiSignature` — that is the caller's responsibility.

#### Scenario: Successful partial DIRECT sign
- **WHEN** `POST /sign/cosmos/partial` is called with `{"key_path": "...", "sign_mode": "DIRECT", "sign_doc": "<base64>", "signer_index": 0, "multisig_pubkeys": ["<base64-compressed-pubkey1>", "<base64-compressed-pubkey2>"], "threshold": 2}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "<base64-SignatureV2>", "pub_key": "<base64-compressed-33-byte-pubkey>", "signer_index": 0}`

#### Scenario: signer_index out of range
- **WHEN** `signer_index` is greater than or equal to the length of `multisig_pubkeys`
- **THEN** the gateway returns HTTP 400: `{"error": "signer_index 3 is out of range for multisig_pubkeys of length 2"}`

#### Scenario: Gateway key pubkey not in multisig_pubkeys list
- **WHEN** the public key derived from `key_path` does not match the entry at `signer_index` in `multisig_pubkeys`
- **THEN** the gateway returns HTTP 400: `{"error": "key at signer_index does not match gateway key for path <path>"}`

#### Scenario: Invalid SignDoc bytes
- **WHEN** the `sign_doc` base64 cannot be decoded or cannot be unmarshalled as a valid proto SignDoc
- **THEN** the gateway returns HTTP 400: `{"error": "invalid SignDoc proto encoding"}`

---

### Requirement: Partial Cosmos multisig signature — SIGN_MODE_LEGACY_AMINO_JSON
The system SHALL accept an `StdSignDoc` (amino JSON string), canonicalise it (sorted keys, no trailing whitespace), hash with SHA-256, sign via the Vault backend, and return a single partial amino-compatible signature. The caller assembles the full amino multisig.

#### Scenario: Successful partial AMINO sign
- **WHEN** `POST /sign/cosmos/partial` is called with `{"key_path": "...", "sign_mode": "AMINO_JSON", "sign_doc": "<amino-json-string>", "signer_index": 1, "multisig_pubkeys": [...], "threshold": 2}`
- **THEN** the gateway returns HTTP 200 with `{"signature": "<base64-amino-sig>", "pub_key": "<base64-compressed-33-byte-pubkey>", "signer_index": 1}`

#### Scenario: Non-canonical amino JSON (auto-canonicalised)
- **WHEN** the amino JSON has unsorted keys or trailing whitespace
- **THEN** the system canonicalises the JSON before hashing and returns the signature without error

---

### Requirement: Multisig request validation
The system SHALL validate all required fields in a partial-sign request before performing any signing operation.

#### Scenario: Missing required fields
- **WHEN** `POST /sign/cosmos/partial` is called without `key_path`, `sign_mode`, `sign_doc`, `signer_index`, `multisig_pubkeys`, or `threshold`
- **THEN** the gateway returns HTTP 400: `{"error": "<field> is required"}`

#### Scenario: threshold less than 1 or greater than multisig_pubkeys length
- **WHEN** `threshold` is 0 or greater than the number of entries in `multisig_pubkeys`
- **THEN** the gateway returns HTTP 400: `{"error": "threshold must be between 1 and len(multisig_pubkeys)"}`

#### Scenario: Unsupported sign_mode
- **WHEN** `sign_mode` is not `DIRECT` or `AMINO_JSON`
- **THEN** the gateway returns HTTP 400: `{"error": "unsupported sign_mode for partial signing: <value>"}`
