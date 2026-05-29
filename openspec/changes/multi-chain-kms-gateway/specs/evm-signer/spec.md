## ADDED Requirements

### Requirement: Sign raw EVM transaction
The EVM signer SHALL accept a RLP-encoded unsigned transaction, hash it with Keccak-256, submit the hash to the Vault backend for signing, recover the `v` value for EIP-155 replay protection, and return the fully signed RLP-encoded transaction.

#### Scenario: Successful raw tx signing
- **WHEN** a valid RLP-encoded unsigned EVM transaction and chain ID are provided
- **THEN** the system returns a signed RLP transaction with correct `r`, `s`, `v` fields compatible with `eth_sendRawTransaction`

#### Scenario: Invalid RLP input
- **WHEN** the input bytes cannot be decoded as a valid Ethereum transaction
- **THEN** the system returns an error: "invalid RLP encoding"

#### Scenario: Chain ID mismatch protection
- **WHEN** the requested chain ID does not match the chain ID encoded in the transaction
- **THEN** the system returns an error before signing

---

### Requirement: Sign personal message (eth_sign / personal_sign)
The EVM signer SHALL accept an arbitrary byte message, prefix it with the Ethereum signed message prefix (`"\x19Ethereum Signed Message:\n" + len`), hash with Keccak-256, and sign via the Vault backend.

#### Scenario: Successful personal sign
- **WHEN** an arbitrary message byte slice and key path are provided
- **THEN** the system returns a 65-byte signature `[r(32) || s(32) || v(1)]` compatible with `eth_sign`

#### Scenario: Empty message
- **WHEN** an empty byte slice is provided as the message
- **THEN** the system signs the empty-prefixed hash (valid operation, not an error)

---

### Requirement: Sign EIP-712 typed data
The EVM signer SHALL accept a pre-computed EIP-712 digest (32 bytes) or a structured `TypedData` object, compute the domain-separator hash, and sign via the Vault backend.

#### Scenario: Sign pre-computed EIP-712 digest
- **WHEN** a 32-byte EIP-712 digest is provided directly
- **THEN** the system signs it without re-hashing and returns a 65-byte signature

#### Scenario: Invalid digest length
- **WHEN** the digest is not exactly 32 bytes
- **THEN** the system returns an error: "EIP-712 digest must be 32 bytes"

---

### Requirement: Recover Ethereum address from key
The EVM signer SHALL derive the Ethereum address (20-byte checksummed, EIP-55) from the secp256k1 public key stored in Vault for a given key path.

#### Scenario: Address derivation
- **WHEN** the public key for a key path is retrieved from Vault
- **THEN** the system returns the correct EIP-55 checksummed Ethereum address

#### Scenario: Key not found
- **WHEN** the key path does not exist in Vault
- **THEN** the system propagates the not-found error from the Vault backend
