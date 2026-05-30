## Purpose
Define Vault backend authentication, key management, public key retrieval, and signing semantics.

## Requirements

### Requirement: Connect to Vault via token auth
The system SHALL authenticate to HashiCorp Vault using a token supplied via the `VAULT_TOKEN` environment variable or `vault.token` config field. The client SHALL validate connectivity on startup by calling the Vault health endpoint.

#### Scenario: Successful connection
- **WHEN** `VAULT_TOKEN` is set and Vault is reachable at the configured address
- **THEN** the client initialises without error and reports the Vault version in debug logs

#### Scenario: Missing token
- **WHEN** no token is provided in env or config
- **THEN** startup fails with a descriptive error: "vault token is required"

#### Scenario: Vault unreachable
- **WHEN** Vault address is misconfigured or network is unavailable
- **THEN** startup fails with a descriptive error including the attempted address

---

### Requirement: Create Transit key
The system SHALL create a new Vault Transit key of type `ecdsa-p256k1` at a given key path. Key creation SHALL be idempotent — if the key already exists, the operation SHALL succeed without error.

#### Scenario: New key creation
- **WHEN** a key creation request is issued for a path that does not exist
- **THEN** the key is created in Vault Transit and the operation returns success

#### Scenario: Idempotent creation
- **WHEN** a key creation request is issued for a path that already exists
- **THEN** the operation returns success without modifying the existing key

#### Scenario: Insufficient Vault policy
- **WHEN** the Vault token lacks `create` capability on the Transit path
- **THEN** the operation returns a permission error surfaced to the caller

---

### Requirement: Retrieve public key
The system SHALL retrieve the public key (uncompressed secp256k1, 65 bytes) for a given Transit key path. The public key SHALL be returned as a hex-encoded string.

#### Scenario: Key exists
- **WHEN** the public key is requested for an existing key path
- **THEN** the system returns the 65-byte uncompressed public key as a hex string

#### Scenario: Key does not exist
- **WHEN** the public key is requested for a non-existent key path
- **THEN** the system returns a not-found error

---

### Requirement: Sign payload via Transit
The system SHALL submit a raw byte payload to Vault Transit for signing. The payload SHALL be pre-hashed by the caller. The Transit API's `hash_algorithm=none` option SHALL be used so Vault signs the hash directly without re-hashing.

#### Scenario: Successful signing
- **WHEN** a 32-byte hash is submitted for a valid key path
- **THEN** Vault returns a DER-encoded signature, which the system decodes and returns as `(r, s, v)` components

#### Scenario: Invalid hash length
- **WHEN** a payload other than 32 bytes is submitted
- **THEN** the system returns an error before calling Vault: "payload must be 32 bytes (pre-hashed)"

#### Scenario: Key not found during sign
- **WHEN** the key path does not exist in Vault
- **THEN** the system returns a not-found error with the key path in the message

---

### Requirement: AuthProvider interface
The system SHALL define an `AuthProvider` interface with a `Token() (string, error)` method. The token-based implementation SHALL return the static token from config. The interface SHALL be structured to allow future AppRole and Kubernetes auth implementations without changing the Vault client.

#### Scenario: Token provider returns token
- **WHEN** the token auth provider is initialised with a non-empty token string
- **THEN** `Token()` returns that string without error

#### Scenario: AppRole stub compiles
- **WHEN** the codebase is compiled
- **THEN** an `AppRoleAuthProvider` struct exists, satisfies the `AuthProvider` interface, and returns `errNotImplemented` from `Token()`
