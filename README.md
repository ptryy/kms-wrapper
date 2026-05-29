# kms-wrapper

`kms-wrapper` is a Go CLI and REST gateway for multi-chain signing with HashiCorp Vault Transit. Keys use Vault `ecdsa-p256k1`; private key material never leaves Vault.

## Prerequisites

- Go 1.25.9+
- Vault 1.10+
- Docker Compose for local development

## Quickstart

```sh
cp .env.example .env
docker compose up -d
export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root KMS_GATEWAY_TOKEN=dev
vault secrets enable transit
go run ./cmd/kms-wrapper keys create --path proj-a/evm/alice
go run ./cmd/kms-wrapper serve
```

## Key paths

Logical key paths are `{project}/{chain}/{username}` using lowercase `[a-z0-9_-]` segments. Vault paths are `transit/keys/<path>`. Reserved chain identifiers are `evm`, `eth`, `mantra`, `cosmos`, and `osmosis`; unknown values are allowed with a warning.

See `vault/policy-project.hcl` for a sample project-scoped Vault policy.

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
