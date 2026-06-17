## MODIFIED Requirements

### Requirement: Sign Cosmos transaction in SIGN_MODE_LEGACY_AMINO_JSON
The Cosmos signer SHALL accept an `StdSignDoc` (amino JSON), canonicalise it using cosmos-sdk's `types.SortJSON` function (the same function the chain uses to re-derive sign bytes during signature verification), hash with SHA-256, sign via the Vault backend, and return the `StdSignature` compatible with legacy Cosmos amino encoding. The signer SHALL reject inputs containing duplicate JSON keys at any nesting level with an error before signing.

#### Scenario: Successful AMINO mode signing
- **WHEN** a valid amino JSON `StdSignDoc` and key path are provided
- **THEN** the system returns an `StdSignature` with the correct public key and signature bytes; the signed bytes are byte-equal to `types.SortJSON(input)` (NOT to Go `json.Marshal` of the parsed map)

#### Scenario: Non-canonical amino JSON input
- **WHEN** the input JSON has unsorted keys or trailing whitespace
- **THEN** the system canonicalises via `types.SortJSON` before hashing — the canonical bytes are what the chain will re-derive on verification

#### Scenario: Duplicate keys rejected
- **WHEN** the input JSON contains a duplicate key at any object level (e.g. `{"a":1, "a":2}`)
- **THEN** the signer returns an error `"duplicate key in amino sign doc: a"` and does NOT produce a signature

#### Scenario: Canonical bytes match cosmos-sdk reference
- **WHEN** the same `StdSignDoc` bytes are passed through both this signer's canonicalisation step AND `cosmos-sdk/types.SortJSON` directly
- **THEN** the two outputs are byte-identical (verified via a vendored fixture in `cosmos_test.go`)
