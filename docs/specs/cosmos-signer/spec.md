## Purpose
Define Cosmos signing behaviors, sign modes, and Cosmos address/public key derivation.
## Requirements
### Requirement: Sign Cosmos transaction in SIGN_MODE_DIRECT
The Cosmos signer SHALL accept a `SignDoc` (protobuf-encoded), hash it with SHA-256, sign via the Vault backend (secp256k1), and return the `SignatureV2` compatible with Cosmos SDK's `tx.Builder`.

#### Scenario: Successful DIRECT mode signing
- **WHEN** a valid protobuf `SignDoc` bytes, account number, sequence, and key path are provided
- **THEN** the system returns a `SignatureV2` with `SingleSignatureData` of mode `SIGN_MODE_DIRECT` and the DER-decoded `(r, s)` signature bytes

#### Scenario: Invalid SignDoc bytes
- **WHEN** the `SignDoc` bytes cannot be unmarshalled as a valid proto message
- **THEN** the system returns an error: "invalid SignDoc proto encoding"

---

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

### Requirement: Derive Cosmos account address
The Cosmos signer SHALL derive a bech32 account address from the secp256k1 public key in Vault, given a human-readable part (HRP, e.g. `"cosmos"`, `"mantra"`).

#### Scenario: Address derivation with custom HRP
- **WHEN** a key path and HRP string (e.g. `"mantra"`) are provided
- **THEN** the system returns the correct bech32 address (e.g. `mantra1abc...`)

#### Scenario: Invalid HRP
- **WHEN** an empty or invalid HRP string is provided
- **THEN** the system returns an error: "invalid bech32 HRP"

---

### Requirement: Public key export in Cosmos format
The Cosmos signer SHALL export the public key for a given key path as a compressed secp256k1 public key (33 bytes) suitable for use in Cosmos SDK `PubKey` protobuf messages.

#### Scenario: Compressed public key export
- **WHEN** the public key is requested for an existing key path
- **THEN** the system returns a 33-byte compressed secp256k1 public key

#### Scenario: Key not found
- **WHEN** the key path does not exist in Vault
- **THEN** the system propagates the not-found error from the Vault backend

