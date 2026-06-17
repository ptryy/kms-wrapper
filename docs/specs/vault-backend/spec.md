## Purpose
Define Vault backend authentication, key management, public key retrieval, and signing semantics.
## Requirements
### Requirement: Connect to Vault via token auth
The system SHALL authenticate to HashiCorp Vault using a token supplied via the `VAULT_TOKEN` environment variable or `vault.token` config field. The client SHALL validate connectivity on startup by calling the Vault health endpoint. The system SHALL refuse to start unconditionally (no `KMS_DEV` bypass) when the supplied token is empty. The system SHALL also refuse to start when the supplied token is equal to `root` or matches a known-weak placeholder (`dev`, `dev-token`, `change-me`), unless the environment variable `KMS_DEV` is set to `true`.

#### Scenario: Successful connection
- **WHEN** `VAULT_TOKEN` is set to a non-root, non-placeholder value and Vault is reachable at the configured address
- **THEN** the client initialises without error and reports the Vault version in debug logs

#### Scenario: Missing token
- **WHEN** no token is provided in env or config
- **THEN** startup fails with a descriptive error: "vault token is required"

#### Scenario: Root token refused outside dev mode
- **WHEN** `VAULT_TOKEN=root` and `KMS_DEV` is not set
- **THEN** startup fails with the error `"refusing to start with weak vault token; set KMS_DEV=true for local dev"`

#### Scenario: Root token allowed in dev mode
- **WHEN** `VAULT_TOKEN=root` and `KMS_DEV=true`
- **THEN** startup proceeds and a `warn` log is emitted: `"running with weak vault token (KMS_DEV=true)"`

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

### Requirement: Vault error responses are mapped via typed status codes
The Vault client SHALL classify errors from `github.com/hashicorp/vault/api` by extracting `*vaultapi.ResponseError` via `errors.As` and switching on `StatusCode`. The client SHALL NOT classify Vault errors by substring-matching `err.Error()`. HTTP 403 SHALL map to `types.ErrPermission`, HTTP 404 SHALL map to `types.ErrNotFound`, and HTTP 400 SHALL map to a `types.ErrBadRequest`-wrapped error carrying the Vault-reported message.

#### Scenario: Permission denied
- **WHEN** Vault returns HTTP 403 on any call
- **THEN** the client returns an error satisfying `errors.Is(err, types.ErrPermission)` regardless of the Vault error message text

#### Scenario: Key path not found
- **WHEN** Vault returns HTTP 404 on a read/list/sign call for a path that does not exist
- **THEN** the client returns an error satisfying `errors.Is(err, types.ErrNotFound)`

#### Scenario: Locale-independent classification
- **WHEN** Vault is fronted by a proxy that rewords the error body (e.g. removing the literal string `"permission denied"`) but preserves the 403 status
- **THEN** the client still returns `types.ErrPermission`

#### Scenario: Non-Vault errors pass through
- **WHEN** the underlying call fails before reaching Vault (e.g. TCP reset, DNS failure) and the error is not a `*vaultapi.ResponseError`
- **THEN** the client returns the raw error unchanged for the caller to map to HTTP 500

---

### Requirement: Vault token renewal is observable and TTL-adaptive
The Vault client `StartRenewal` goroutine SHALL retry the initial `LookupSelf` call with capped exponential backoff (1s, 2s, 4s, 8s, 16s, 30s, then steady) until success or `ctx.Done()`. Once a token TTL is known, the renewal tick interval SHALL be `max(30s, ttl/3)` rather than a fixed 5-minute interval. Every `LookupSelf` or `RenewSelf` error SHALL be logged at `warn` with the error wrapped via `slog.WarnContext`. Successful renewals SHALL be logged at `debug` with the new TTL.

#### Scenario: Renewal logs on failure
- **WHEN** `RenewSelf` returns a non-nil error during the periodic renewal tick
- **THEN** a `warn`-level log entry is emitted with the error string and the failure does not silently exit the renewal goroutine

#### Scenario: Initial LookupSelf retries on transient outage
- **WHEN** Vault is unreachable at the moment `StartRenewal` is called, then becomes reachable 5 seconds later
- **THEN** the goroutine reaches a successful `LookupSelf` within the next backoff window and begins periodic renewal

#### Scenario: Renewal cadence adapts to short TTL
- **WHEN** the Vault token has a TTL of 90 seconds
- **THEN** the renewal tick fires at most every 30 seconds (the floor), not every 5 minutes

---

### Requirement: Public-key lookups are cached
The Vault client SHALL cache the result of `GetPublicKey(path)` per-path in memory for the lifetime of the process. Subsequent `GetPublicKey` calls for the same path SHALL be served from the cache without a network round-trip.

#### Scenario: Cached pubkey returned on second call
- **WHEN** `GetPublicKey(ctx, "proj-a/evm/alice")` is called twice in succession on the same client and the first call succeeds
- **THEN** the second call returns the same bytes and does not issue an HTTP request to Vault

#### Scenario: Cache is per-path
- **WHEN** `GetPublicKey` is called for two different paths
- **THEN** each path is fetched once and cached independently; neither cache entry serves the other path

#### Scenario: Cache miss after process restart
- **WHEN** the gateway process restarts
- **THEN** the cache is empty and the next `GetPublicKey` call fetches from Vault

