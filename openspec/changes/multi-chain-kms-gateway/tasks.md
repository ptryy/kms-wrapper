## 1. Repository & Module Scaffold

- [ ] 1.1 Initialise Go module (`go mod init github.com/ryan-truong/kms-wrapper`)
- [ ] 1.2 Create directory tree: `cmd/kms-wrapper/`, `internal/vault/`, `internal/signer/evm/`, `internal/signer/cosmos/`, `internal/gateway/`, `internal/config/`, `pkg/types/`
- [ ] 1.3 Add core dependencies: `hashicorp/vault/api`, `ethereum/go-ethereum`, `cosmos/cosmos-sdk`, `spf13/cobra`, `spf13/viper`
- [ ] 1.4 Create `Makefile` with targets: `build`, `test`, `lint`, `dev-up`, `dev-down`, `run-gateway`
- [ ] 1.5 Create `docker-compose.yml` with Vault in dev mode (port 8200, root token `root`)
- [ ] 1.6 Create `.env.example` with `VAULT_ADDR`, `VAULT_TOKEN`, `KMS_GATEWAY_TOKEN`, `KMS_GATEWAY_ADDR`

## 2. Config Package

- [ ] 2.1 Define `Config` struct in `internal/config/config.go` with fields: `Vault.Addr`, `Vault.Token`, `Gateway.Addr`, `Gateway.Token`, `LogLevel`
- [ ] 2.2 Implement viper-based loader: YAML file + env var override (`KMS_*` prefix)
- [ ] 2.3 Add config validation: non-empty `Vault.Addr`, non-empty `Vault.Token` required at runtime
- [ ] 2.4 Write unit tests for config loading with env var override

## 3. Public Types (`pkg/types`)

- [ ] 3.1 Define `SignRequest` and `SignResponse` structs covering EVM and Cosmos payloads
- [ ] 3.2 Define `KeyInfo` struct: `Path`, `PublicKeyHex`, `EVMAddress`, `CosmosAddress`
- [ ] 3.3 Define error types: `ErrNotFound`, `ErrPermission`, `ErrInvalidInput`

## 4. Vault Backend (`internal/vault`)

- [ ] 4.1 Define `AuthProvider` interface with `Token() (string, error)` method
- [ ] 4.2 Implement `TokenAuthProvider` (returns static token from config)
- [ ] 4.3 Add `AppRoleAuthProvider` stub that satisfies the interface and returns `errNotImplemented`
- [ ] 4.4 Implement `Client` struct with constructor: validates Vault connectivity via health endpoint
- [ ] 4.5 Implement `CreateKey(path string) error` — idempotent, `ecdsa-p256k1` type
- [ ] 4.6 Implement `GetPublicKey(path string) ([]byte, error)` — returns 65-byte uncompressed pubkey
- [ ] 4.7 Implement `Sign(path string, hash []byte) (r, s *big.Int, err error)` — `hash_algorithm=none`, decodes DER signature
- [ ] 4.8 Write unit tests with a mock Vault HTTP server (no real Vault dependency)

## 5. Key Path Policy (`internal/vault` or `pkg/types`)

- [ ] 5.1 Implement `ValidateKeyPath(path string) error` — enforces `{project}/{chain}/{username}` format, `[a-z0-9_-]` segments
- [ ] 5.2 Define reserved chain segment list (`evm`, `eth`, `mantra`, `cosmos`, `osmosis`) and log warning for unknown values
- [ ] 5.3 Implement `ToVaultPath(path string) string` — returns `transit/keys/<path>`
- [ ] 5.4 Write unit tests for valid paths, invalid characters, missing segments, empty segments

## 6. EVM Signer (`internal/signer/evm`)

- [ ] 6.1 Implement `DeriveEVMAddress(pubkey []byte) (string, error)` — Keccak-256 hash of pubkey[1:], last 20 bytes, EIP-55 checksum
- [ ] 6.2 Implement `SignRawTx(keyPath string, chainID *big.Int, rawTx []byte) ([]byte, error)` — RLP decode, Keccak-256 hash, sign via vault client, EIP-155 `v` recovery, RLP encode signed tx
- [ ] 6.3 Implement `SignPersonalMessage(keyPath string, msg []byte) ([]byte, error)` — Ethereum prefix + Keccak-256, sign, return 65-byte sig
- [ ] 6.4 Implement `SignEIP712Digest(keyPath string, digest []byte) ([]byte, error)` — validate 32-byte length, sign directly
- [ ] 6.5 Write unit tests with known-good EVM test vectors (use `go-ethereum` test fixtures)

## 7. Cosmos Signer (`internal/signer/cosmos`)

- [ ] 7.1 Implement `DeriveCosmosAddress(pubkey []byte, hrp string) (string, error)` — compress pubkey to 33 bytes, RIPEMD-160(SHA-256(compressed)), bech32 encode
- [ ] 7.2 Implement `ExportCompressedPubKey(keyPath string) ([]byte, error)` — retrieves 65-byte pubkey from Vault, compresses to 33 bytes
- [ ] 7.3 Implement `SignDirect(keyPath string, signDocBytes []byte) (sigBytes []byte, pubKeyBytes []byte, err error)` — SHA-256 hash, sign via vault client
- [ ] 7.4 Implement `SignAmino(keyPath string, stdSignDocJSON []byte) (sigBytes []byte, pubKeyBytes []byte, err error)` — canonicalise JSON, SHA-256 hash, sign
- [ ] 7.5 Write unit tests with known-good Cosmos test vectors (amino and direct modes)

## 8. REST Gateway (`internal/gateway`)

- [ ] 8.1 Implement bearer token middleware — validates `Authorization: Bearer <token>` against config
- [ ] 8.2 Implement `GET /health` handler — checks Vault connectivity, returns JSON status
- [ ] 8.3 Implement `POST /sign/evm` handler — parses request, dispatches to EVM signer, returns JSON response
- [ ] 8.4 Implement `POST /sign/cosmos` handler — parses request, dispatches to Cosmos signer, returns JSON response
- [ ] 8.5 Implement structured error response helper — never logs or returns key material
- [ ] 8.6 Implement configurable listen address from config (`127.0.0.1:8080` default)
- [ ] 8.7 Implement graceful shutdown with 30-second drain timeout on SIGINT/SIGTERM
- [ ] 8.8 Write handler unit tests using `httptest` package (mock signers)

## 9. CLI (`cmd/kms-wrapper`)

- [ ] 9.1 Implement root command with `--config` and `--log-level` flags, wires viper config loading
- [ ] 9.2 Implement `serve` subcommand — starts REST gateway, blocks on signal
- [ ] 9.3 Implement `keys create --path` subcommand — calls vault backend, prints `KeyInfo`
- [ ] 9.4 Implement `keys show --path` subcommand — retrieves and prints `KeyInfo`
- [ ] 9.5 Implement `sign evm --path --chain-id --raw-tx` subcommand — prints signed tx hex
- [ ] 9.6 Implement `sign cosmos --path --hrp --mode --sign-doc` subcommand — prints base64 sig + pubkey
- [ ] 9.7 Implement `health` subcommand — checks Vault reachability, exits 0/1
- [ ] 9.8 Write CLI smoke tests using `cobra` test helpers

## 10. Documentation & Dev Tooling

- [ ] 10.1 Write `README.md`: project overview, prerequisites (Vault 1.10+, Go 1.22+), quickstart with Docker Compose
- [ ] 10.2 Document key path convention and reserved chain segments
- [ ] 10.3 Document REST gateway API (request/response schemas for `/sign/evm`, `/sign/cosmos`, `/health`)
- [ ] 10.4 Add sample Vault policy HCL for multi-tenant project isolation
- [ ] 10.5 Add `golangci-lint` config (`.golangci.yaml`) and wire into `make lint`
- [ ] 10.6 Add GitHub Actions CI workflow: `go vet`, `golangci-lint`, `go test ./...`
