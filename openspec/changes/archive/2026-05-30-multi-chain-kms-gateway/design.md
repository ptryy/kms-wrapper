## Context

No existing signing infrastructure. Teams running EVM validators, Cosmos relayers, and multi-chain ops currently handle private keys in plaintext config files or roll bespoke Vault integrations per chain. This project establishes a canonical, self-hostable KMS layer backed by HashiCorp Vault 1.17 with a custom secp256k1 signing plugin (`kms-vault-plugin`). Target environment is standard Debian/Ubuntu Linux VMs on an internal VPN — no HSM, no cloud KMS dependency.

> **Why not Vault Transit?** Vault OSS Transit engine does not support the `secp256k1` curve natively. The `ecdsa-p256k1` key type is absent from Vault OSS. The `vault-ethereum` community plugin does support secp256k1 but (a) hashes inputs with keccak256 internally — incompatible with Cosmos SDK's SHA-256 requirement — and (b) does not expose raw compressed public keys, which Cosmos `SignatureV2` requires. A purpose-built plugin is the only approach that correctly handles both ecosystems.

Constraints:
- Keys never leave Vault (plugin storage is encrypted by Vault's seal key).
- Single binary deployment (`kms-wrapper`) for both CLI and gateway server modes.
- Internal-network-first: gateway listens on localhost/VPN interface, not 0.0.0.0.
- Multi-tenant from day 1: key paths encode project, chain, and user identity.

## Goals / Non-Goals

**Goals:**
- Define package structure, interfaces, and config schema for the Go module.
- Specify REST gateway endpoint contract (`/sign/evm`, `/sign/cosmos`, `/keys/...`).
- Define CLI subcommand tree and flag conventions.
- Specify custom `kms-vault-plugin` providing secp256k1 key management and signing for both EVM and Cosmos.
- Define key path convention and validation rules.
- Produce Docker Compose stack for local dev (Vault 1.17 dev mode + plugin volume mount).
- Produce Makefile targets: `build`, `build-plugin`, `test`, `lint`, `dev-up`, `dev-down`.

**Non-Goals:**
- AppRole auth implementation (interface defined, not wired).
- Kubernetes / Helm deployment manifests (follow-up change).
- Key rotation automation.
- Multi-region Vault replication.
- Broadcast / submission of signed transactions (sign only, not submit).

## Decisions

### D1: Single binary, mode flag

**Decision**: One compiled binary (`kms-wrapper`) that runs either as a CLI or as an HTTP gateway server (`kms-wrapper serve`).

**Rationale**: Simplifies distribution and deployment — operators `scp` one binary. Alternative (separate `kms-cli` + `kms-gateway` binaries) doubles build/release complexity with no operational benefit at this scale.

**Alternatives considered**: Separate binaries — rejected due to operational overhead.

---

### D2: Key backend — custom `kms-vault-plugin` (secp256k1)

**Decision**: All keys are managed by a purpose-built Vault plugin (`kms-vault-plugin`) mounted at `kms/`. The plugin stores secp256k1 private keys in Vault's encrypted logical storage and exposes dedicated endpoints for key creation, public key retrieval, raw signing, and key import. Vault's seal key encrypts all plugin storage at rest.

**Rationale**: Vault OSS Transit does not support secp256k1. The `vault-ethereum` community plugin does support secp256k1 but hashes inputs with keccak256 internally, making it incompatible with Cosmos SDK's SHA-256 signing requirement, and it does not expose raw compressed public keys needed for Cosmos `SignatureV2`. A single custom plugin is the only approach that handles both ecosystems correctly without splitting backends.

**Alternatives considered**:
- Vault Transit `ecdsa-p256k1` — key type does not exist in Vault OSS; rejected.
- `vault-ethereum` for EVM + custom plugin for Cosmos — two backends, two policy surfaces, two mount configurations; rejected in favour of a single unified plugin.
- `vault-ethereum` for everything — keccak256/SHA-256 mismatch for Cosmos; rejected.

---

### D2a: Plugin endpoint contract

**Decision**: The `kms-vault-plugin` exposes the following paths under its mount (`kms/`):

| Method | Path | Purpose |
|--------|------|---------|
| POST | `kms/keys/<path>` | Generate a new secp256k1 key |
| GET | `kms/keys/<path>` | Read key info (address, compressed pubkey, source, timestamps) |
| LIST | `kms/keys/` | List key names under a prefix |
| DELETE | `kms/keys/<path>` | Delete key entry |
| POST | `kms/keys/<path>/import` | Import raw 32-byte secp256k1 private key (hex) |
| POST | `kms/sign/<path>` | Raw secp256k1 sign of a pre-hashed 32-byte input |

The `sign` endpoint intentionally does **no internal hashing** — callers are responsible for hashing (keccak256 for EVM, SHA-256 for Cosmos). This preserves the existing `vault.Client.Sign(path, 32-byte-hash)` interface; only the backend path changes.

**Rationale**: Separating the hash step (gateway) from the sign step (plugin) keeps the plugin minimal and chain-agnostic. It also means `evm.go` and `cosmos.go` require zero changes — the gateway already computes the correct hash for each chain before calling `vault.Sign`.

---

### D2b: Plugin key storage

**Decision**: Each key entry is stored as a JSON-encoded `KeyEntry` struct in Vault's logical storage at `keys/<path>`:

```go
type KeyEntry struct {
    PrivateKey       []byte     // 32-byte secp256k1 scalar
    CompressedPubKey []byte     // 33-byte compressed public key
    EVMAddress       string     // EIP-55 checksummed hex address
    Source           string     // "generated" | "imported"
    CreatedAt        time.Time
    ImportedAt       *time.Time // non-nil only when Source == "imported"
}
```

`PrivateKey` is never returned in any API response. The `GET kms/keys/<path>` response includes only `evm_address`, `compressed_pub_key` (base64), `source`, and timestamps.

**Rationale**: Vault's logical storage is encrypted by the seal key using AES-256-GCM. Storing the private key here is equivalent in security to Vault Transit — the key is protected by Vault's encryption and never leaves the Vault process boundary.

---

### D2c: Plugin build and deployment

**Decision**: The plugin is compiled as a separate binary (`cmd/kms-vault-plugin/main.go`) and delivered via Docker volume mount for local dev:

```
make build-plugin        # cross-compiles for linux/amd64 → vault/plugins/kms-vault-plugin
make dev-up              # docker-compose up + vault/init.sh (registers + enables plugin)
```

`vault/init.sh` runs after Vault starts and performs:
1. `vault plugin register secret -sha256=<hash> kms-vault-plugin`
2. `vault secrets enable -path=kms kms-vault-plugin`

The plugin SHA-256 is computed at init time from the mounted binary — no hard-coded hash in source.

**Vault version**: 1.17 (upgraded from 1.15). No breaking changes to the plugin SDK or Docker dev mode between 1.15 and 1.17.

**Alternatives considered**: Custom Docker image with plugin baked in — cleaner for prod but heavier for local dev iteration; deferred to a future prod deployment change.

---

### D3: Key path convention — `kms/keys/{project}/{chain}/{username}`

**Decision**: Encode project, chain, and username in the plugin key name using `/`-separated segments. Plugin storage path: `kms/keys/{project}/{chain}/{username}`.

**Rationale**: The `/` separator works identically in the plugin's storage namespace as it did in Vault Transit. Vault policies can still be scoped to `kms/keys/project-a/*` without separate mounts per tenant. The three-segment format and `[a-z0-9_-]` character restriction in `path.go` are unchanged.

**Change from previous design**: path prefix flips from `transit/keys/` to `kms/keys/` and `transit/sign/` to `kms/sign/`. `ValidateKeyPath` and the three-segment format are unchanged.

**Alternatives considered**: Separate Vault mounts per project — too many mounts to manage operationally.

---

### D4: REST gateway auth — bearer token (static shared secret, v1)

**Decision**: Gateway v1 authenticates callers via a static bearer token in `Authorization: Bearer <token>`. Token configured via env var (`KMS_GATEWAY_TOKEN`).

**Rationale**: Simplest possible auth that is not anonymous. AppRole and mTLS are the correct long-term solutions but add operational complexity before the gateway is proven useful. Static token is acceptable for internal-VPN-only deployments.

**Alternatives considered**: mTLS — correct for production but requires PKI setup; deferred. AppRole per-caller — right direction, flagged as follow-up.

---

### D5: Package layout

```
kms-wrapper/
├── cmd/
│   ├── kms-wrapper/        # cobra root command + main.go (CLI + gateway)
│   └── kms-vault-plugin/   # Vault plugin binary entrypoint
│       └── main.go
├── internal/
│   ├── plugin/             # kms-vault-plugin implementation
│   │   ├── backend.go      # Vault plugin framework wiring (logical.Backend)
│   │   ├── path_keys.go    # create / read / delete / import endpoints
│   │   └── path_sign.go    # raw secp256k1 sign endpoint
│   ├── vault/              # Gateway-side Vault client (calls plugin API)
│   ├── signer/
│   │   ├── evm/            # EVM signing logic (hashing + sig recovery)
│   │   └── cosmos/         # Cosmos signing logic (hashing + address derivation)
│   ├── gateway/            # HTTP server, middleware, handlers
│   └── config/             # Config struct, viper loading, validation
├── pkg/
│   └── types/              # Public types: SignRequest, SignResponse, KeyInfo
├── vault/
│   ├── plugins/            # Plugin binary output dir (git-ignored)
│   └── init.sh             # Register + enable plugin after Vault starts
├── openspec/               # Change proposals, design, specs, tasks
├── docker-compose.yml      # Vault 1.17 dev mode + plugin volume mount
├── Makefile
└── go.mod
```

**Rationale**: `internal/plugin` houses the Vault plugin code — a separate compilation target from the gateway. Both binaries share `pkg/types` and can share secp256k1 utility code. `vault/plugins/` is git-ignored; the binary is built locally via `make build-plugin` before `make dev-up`.

**Key insight on signing layers**: `internal/signer/evm` and `internal/signer/cosmos` are **unchanged** — they compute hashes and assemble signatures using the same `vault.Client.Sign(path, hash)` interface. Only the client's backend path changes from `transit/sign/<path>` to `kms/sign/<path>`.

---

### D6: Config schema — file + env var override

**Decision**: YAML config file (`~/.kms-wrapper/config.yaml` or `--config` flag), with all fields overridable by env vars (`KMS_*`). Loaded via `spf13/viper`.

**Rationale**: Standard Go CLI pattern. Env var override is essential for container/CI use without modifying config files.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| Static bearer token leaks | Document rotation procedure; mark token as short-lived secret. Future: AppRole. |
| Plugin binary mismatch (wrong arch or stale SHA-256) | `vault/init.sh` computes SHA-256 at registration time from the mounted binary. `make build-plugin` cross-compiles for `linux/amd64` regardless of host OS. |
| Plugin storage not replicated in HA Vault | Plugin uses Vault's integrated storage — same replication guarantees as Transit. No separate replication concern for OSS single-node. |
| Cosmos signing: amino vs direct encoding mismatch | Unit tests with known-good tx vectors from `cosmjs` / `cosmwasm`. |
| EIP-712 domain separator inconsistency | Test against MetaMask reference vectors. |
| Key path with `/` breaks some Vault CLI tooling | Documented workaround: use API directly. Plugin keys listed via `vault list kms/keys/`. |
| Vault 1.17 upgrade from 1.15 | No breaking changes to plugin SDK or Docker dev mode between 1.15 and 1.17. `go-jose` v3→v4 bump in Vault internals has no impact (kms-wrapper uses no JWT/OIDC). |

## Open Questions

1. **mTLS timeline**: When should the gateway move from static bearer token to mTLS? Depends on PKI infrastructure maturity — revisit after first production deployment.
2. **AppRole interface**: Should the `AuthProvider` interface be defined in this change (as a stub) or deferred entirely? Recommend: define the interface, implement only token provider.
3. **Health endpoint auth**: Should `GET /health` be unauthenticated (for load balancer probes) or require the bearer token? Recommend: unauthenticated health, authenticated all other routes.
