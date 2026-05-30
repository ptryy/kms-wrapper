# kms-wrapper

`kms-wrapper` is a Go CLI and REST gateway for multi-chain signing backed by HashiCorp Vault. Keys are managed by a custom Vault secrets plugin, **`kms-vault-plugin`**, that natively supports `secp256k1` for both EVM and Cosmos signing. Private key material never leaves the Vault process boundary.

## Why a custom plugin (not Transit)?

- Vault OSS Transit **does not support `secp256k1`** (no `ecdsa-p256k1` key type).
- The community `vault-ethereum` plugin keccak256-hashes inputs internally — incompatible with Cosmos SDK's SHA-256 signing — and does not expose raw compressed pubkeys.
- `kms-vault-plugin` is purpose-built: it signs **pre-hashed 32-byte inputs** with no internal hashing, so the gateway computes the chain-appropriate digest (keccak256 for EVM, SHA-256 for Cosmos) before calling the plugin.

See `openspec/changes/multi-chain-kms-gateway/design.md` for the full rationale.

## Prerequisites

- Go 1.25+
- Vault **1.17+** (older versions also work for the plugin SDK API, but local dev assumes 1.17)
- Docker Compose for local development

## Quickstart

```sh
cp .env.example .env

# 1. Cross-compile the plugin binary for linux/amd64 into vault/plugins/.
make build-plugin

# 2. Start Vault 1.17 (dev mode) + register the plugin + mount it at kms/.
make dev-up

# 3. Use the CLI to create a key and start the gateway.
export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root KMS_GATEWAY_TOKEN=dev
go run ./cmd/kms-wrapper keys create --path proj-a/evm/alice
go run ./cmd/kms-wrapper serve
```

The config file (`~/.kms-wrapper/config.yaml` by default, override with `--config`) is **optional**. If it is missing, the CLI prints a warning to stderr and continues using environment variables and built-in defaults. If the file exists but is malformed (invalid YAML, unreadable, etc.) the CLI exits non-zero with a `read config` error.

Resolution precedence: **defaults → config file (if present) → env overrides**. After resolution, required runtime fields (`vault.addr`, `vault.token`, `gateway.token`) are validated and startup fails with a descriptive error if any are missing. `kms-wrapper health` distinguishes these config/validation errors from Vault connectivity failures.

`make dev-up` is idempotent — re-running it rebuilds the plugin and re-registers it with a fresh SHA-256. `make dev-down` tears down the stack (in-memory dev mode, so all keys are lost).

### Manually inspecting the plugin

```sh
# Inside the container — VAULT_ADDR defaults to https:// in the image.
docker compose exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=root vault \
  vault plugin info -version="" secret kms-vault-plugin

docker compose exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=root vault \
  vault write -force kms/keys/proj-a/evm/alice
```

## Key paths

Logical key paths are `{project}/{chain}/{username}` using lowercase `[a-z0-9_-]` segments.

- Read / write / delete: `kms/keys/<project>/<chain>/<username>`
- Sign: `kms/sign/<project>/<chain>/<username>`

Reserved chain identifiers are `evm`, `eth`, `mantra`, `cosmos`, and `osmosis`; unknown values are allowed with a warning. See `vault/policy-project.hcl` for a sample project-scoped Vault policy.

### Plugin endpoint contract

| Method | Path | Purpose |
|--------|------|---------|
| POST   | `kms/keys/<name>` | Generate a secp256k1 key (idempotent). Response includes `compressed_pub_key` (base64), `evm_address`, `source`, `created_at`. Never returns the private key. |
| GET    | `kms/keys/<name>` | Read the same public-safe view as create. |
| LIST   | `kms/keys/<prefix>/` | List key names under a prefix. |
| DELETE | `kms/keys/<name>` | Delete the key entry. |
| POST   | `kms/sign/<name>` | Sign a 32-byte pre-hashed input. Body: `{"input": "<64-hex>"}`. Response: `{"r": "<hex>", "s": "<hex>"}`. |

## REST API

`GET /health` is unauthenticated and returns `{"status":"ok","vault":"reachable"}` or HTTP 503 with degraded status.

All other routes require `Authorization: Bearer <KMS_GATEWAY_TOKEN>`.

`POST /sign/evm`:

```json
{"key_path":"proj-a/evm/alice","chain_id":1,"raw_tx":"0x..."}
```

Alternatively use `personal_message` or `eip712_digest`. Responses include `signed_tx` for raw transactions or a `signature` hex string.

`POST /sign/cosmos`:

```json
{"key_path":"proj-a/mantra/alice","hrp":"mantra","sign_mode":"DIRECT","sign_doc":"<base64>"}
```

Use `sign_mode: "AMINO_JSON"` with a JSON `sign_doc` for legacy amino. Responses include base64 `signature` and compressed `pub_key`.

### Key management

The gateway exposes the same key lifecycle as `kms-wrapper keys`. All three require `Authorization: Bearer <KMS_GATEWAY_TOKEN>` and share the `/sign/*` rate-limit budget (`gateway.rate_limit` / `gateway.rate_burst`).

| Method | Path | Purpose |
|--------|------|---------|
| POST   | `/keys` | Create a secp256k1 Transit key. Idempotent — re-create returns the existing key with `already_existed: true`. |
| GET    | `/keys/info?path=<key-path>` | Show public key (hex), EVM address, and Cosmos bech32 address. |
| GET    | `/keys?prefix=<prefix>` | List bare key names under prefix; `prefix` optional. |

```sh
# Create
curl -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"path":"proj-a/evm/alice"}' \
     http://127.0.0.1:8080/keys

# Show
curl -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" \
     "http://127.0.0.1:8080/keys/info?path=proj-a/evm/alice"

# List
curl -H "Authorization: Bearer $KMS_GATEWAY_TOKEN" \
     "http://127.0.0.1:8080/keys?prefix=proj-a/"
```

## API documentation

Swagger UI is served by the gateway at `GET /swagger/index.html` (or `GET /swagger/`) and the raw OpenAPI spec at `GET /swagger/doc.json`.

The committed `docs/swagger.json` is the regenerated artifact for codegen and Postman import. Its `servers[0].url` is the placeholder `http://localhost:8080/` — valid OpenAPI, but a static stand-in. The **runtime** `GET /swagger/doc.json` endpoint is the source of truth for the live UI: it rewrites `servers[0].url` on every request to reflect the gateway's actual origin (honouring `X-Forwarded-Proto` and `Host` when behind a reverse proxy).

Install the generator CLI once:

```sh
go install github.com/swaggo/swag/v2/cmd/swag@latest
```

Regenerate committed docs after editing handlers/types:

```sh
make swagger
make swagger-check
```

In Swagger UI, click **Authorize** and enter `Bearer <KMS_GATEWAY_TOKEN>` to run authenticated `/sign/*` requests.

For internet-exposed deployments, set `KMS_GATEWAY_SWAGGER_AUTH=true` to require bearer auth on `/swagger/*`, or `KMS_GATEWAY_SWAGGER_ENABLED=false` to disable the docs surface entirely.

## CLI

```sh
kms-wrapper --config ~/.kms-wrapper/config.yaml --log-level info
kms-wrapper serve
kms-wrapper keys create --path proj-a/evm/alice
kms-wrapper keys show --path proj-a/evm/alice
kms-wrapper sign evm --path proj-a/evm/alice --chain-id 1 --raw-tx 0x...
kms-wrapper sign cosmos --path proj-a/mantra/alice --hrp mantra --mode DIRECT --sign-doc <base64>
kms-wrapper health
```

## Layout

```
cmd/
  kms-wrapper/         # CLI + gateway entrypoint
  kms-vault-plugin/    # Plugin binary entrypoint
internal/
  plugin/              # kms-vault-plugin (backend, path_keys, path_sign)
  vault/               # Gateway-side Vault client (calls plugin API)
  signer/{evm,cosmos}/ # Chain-specific hash + signature assembly
  gateway/             # HTTP server, middleware, handlers
  config/              # Viper config + validation
pkg/types/             # Shared types (SignRequest/Response, KeyInfo, errors)
vault/
  plugins/             # Plugin binary output (git-ignored)
  init.sh              # Registers + enables the plugin against a running Vault
  policy-project.hcl   # Sample per-project Vault policy
```

## HA / production deployment

Vault OSS HA replicates plugin **registration** and **key data** via Raft, but **not the plugin binary itself**. Every Vault node must have an identical `kms-vault-plugin` binary at the configured `plugin_directory` with a SHA-256 matching the registered value. The recommended pattern is a custom Vault Docker image with the plugin baked in. This is currently a deferred follow-up — see design D2c.
