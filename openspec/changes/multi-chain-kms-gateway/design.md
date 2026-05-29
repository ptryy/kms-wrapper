## Context

No existing signing infrastructure. Teams running EVM validators, Cosmos relayers, and multi-chain ops currently handle private keys in plaintext config files or roll bespoke Vault integrations per chain. This project establishes a canonical, self-hostable KMS layer backed by HashiCorp Vault Transit. Target environment is standard Debian/Ubuntu Linux VMs on an internal VPN — no HSM, no cloud KMS dependency.

Constraints:
- Keys never leave Vault (Transit engine handles signing server-side).
- Single binary deployment (`kms-wrapper`) for both CLI and gateway server modes.
- Internal-network-first: gateway listens on localhost/VPN interface, not 0.0.0.0.
- Multi-tenant from day 1: key paths encode project, chain, and user identity.

## Goals / Non-Goals

**Goals:**
- Define package structure, interfaces, and config schema for the Go module.
- Specify REST gateway endpoint contract (`/sign/evm`, `/sign/cosmos`, `/keys/...`).
- Define CLI subcommand tree and flag conventions.
- Specify Vault Transit key type per chain (secp256k1 for both EVM and Cosmos).
- Define key path convention and validation rules.
- Produce Docker Compose stack for local dev (Vault dev mode).
- Produce Makefile targets: `build`, `test`, `lint`, `dev-up`, `dev-down`.

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

### D2: Vault Transit key type — `ecdsa-p256k1` (secp256k1)

**Decision**: All keys use Vault's `ecdsa-p256k1` key type (Vault 1.10+).

**Rationale**: Both EVM (Ethereum) and Cosmos SDK chains use secp256k1. A single key type supports both ecosystems, so the same Vault key can sign EVM and Cosmos transactions without duplication.

**Alternatives considered**: `ed25519` for Cosmos — some Cosmos chains support it, but EVM does not. Rejected to keep key management uniform.

---

### D3: Key path convention — `transit/keys/{project}/{chain}/{username}`

**Decision**: Encode project, chain, and username in the Vault Transit key name using `/`-separated segments.

**Rationale**: Vault Transit key names support `/` as a path separator in the API path (`transit/sign/<key-name>`). This allows Vault policies to be scoped to `transit/keys/project-a/*` without needing separate mounts per tenant.

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
├── cmd/kms-wrapper/        # cobra root command + main.go
├── internal/
│   ├── vault/              # Vault Transit client (auth, sign, key mgmt)
│   ├── signer/
│   │   ├── evm/            # EVM signing logic
│   │   └── cosmos/         # Cosmos signing logic
│   ├── gateway/            # HTTP server, middleware, handlers
│   └── config/             # Config struct, viper loading, validation
├── pkg/
│   └── types/              # Public types: SignRequest, SignResponse, KeyInfo
├── openspec/               # Change proposals, design, specs, tasks
├── docker-compose.yml      # Vault dev mode stack
├── Makefile
└── go.mod
```

**Rationale**: `internal/` prevents accidental import of unstable packages. `pkg/types` exposes only the stable request/response surface for potential SDK consumers.

---

### D6: Config schema — file + env var override

**Decision**: YAML config file (`~/.kms-wrapper/config.yaml` or `--config` flag), with all fields overridable by env vars (`KMS_*`). Loaded via `spf13/viper`.

**Rationale**: Standard Go CLI pattern. Env var override is essential for container/CI use without modifying config files.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| Static bearer token leaks | Document rotation procedure; mark token as short-lived secret. Future: AppRole. |
| Vault Transit `ecdsa-p256k1` unavailable on older Vault | Document minimum Vault version (1.10+) in README. |
| Cosmos signing: amino vs direct encoding mismatch | Unit tests with known-good tx vectors from `cosmjs` / `cosmwasm`. |
| EIP-712 domain separator inconsistency | Test against MetaMask reference vectors. |
| Key path with `/` breaks some Vault CLI tooling | Documented workaround: use API directly or `vault kv` equivalents. |

## Open Questions

1. **mTLS timeline**: When should the gateway move from static bearer token to mTLS? Depends on PKI infrastructure maturity — revisit after first production deployment.
2. **AppRole interface**: Should the `AuthProvider` interface be defined in this change (as a stub) or deferred entirely? Recommend: define the interface, implement only token provider.
3. **Health endpoint auth**: Should `GET /health` be unauthenticated (for load balancer probes) or require the bearer token? Recommend: unauthenticated health, authenticated all other routes.
