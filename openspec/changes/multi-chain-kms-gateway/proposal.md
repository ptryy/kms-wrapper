## Why

Teams running EVM and Cosmos SDK validators/relayers/signers today must either embed private keys directly in application configs or build custom integrations with Vault per-chain — there is no unified, self-hostable signing layer. This project delivers a single KMS gateway and CLI that centralises all transaction signing behind HashiCorp Vault's Transit engine, eliminating key exposure and supporting multi-tenant deployments without HSM hardware.

## What Changes

- Introduce a new Go module (`kms-wrapper`) with a clean interface layer for multi-chain signing.
- Implement a Vault Transit backend adapter supporting token-based auth (AppRole-ready interface for future expansion).
- Add an EVM signer: raw tx signing, `personal_sign`, EIP-712 typed-data signing.
- Add a Cosmos SDK signer: `SIGN_MODE_DIRECT` and `SIGN_MODE_LEGACY_AMINO_JSON` support.
- Expose a REST gateway (HTTP server) so downstream services can request signatures without embedding Vault credentials.
- Provide a CLI (`kms-wrapper`) for operator use: key management, ad-hoc signing, health checks.
- Define multi-tenant key path conventions: `transit/keys/{project}/{chain}/{username}`.
- Scaffold repo structure: packages, interfaces, config schema, Makefile, Docker Compose (Vault dev mode).

## Capabilities

### New Capabilities

- `vault-backend`: Vault Transit Engine client — key creation, public key retrieval, signing via Transit API. Handles auth (token now, AppRole interface defined).
- `evm-signer`: EVM transaction signing — raw tx, personal_sign, EIP-712 typed-data. Wraps `vault-backend`.
- `cosmos-signer`: Cosmos SDK transaction signing — SIGN_MODE_DIRECT and SIGN_MODE_LEGACY_AMINO_JSON. Wraps `vault-backend`.
- `rest-gateway`: Internal HTTP server exposing `/sign/evm` and `/sign/cosmos` endpoints. Authentication via shared token or mTLS (design decision deferred to design.md).
- `cli`: Operator-facing CLI (`kms-wrapper`) — subcommands: `keys`, `sign`, `health`, `config`.
- `key-path-policy`: Convention and validation for multi-tenant key path layout (`transit/keys/{project}/{chain}/{username}`).

### Modified Capabilities

## Impact

- **New repo/module**: `github.com/ryan-truong/kms-wrapper` — no existing code affected.
- **Dependencies**: `hashicorp/vault/api`, `ethereum/go-ethereum`, `cosmos/cosmos-sdk`, `spf13/cobra`, `spf13/viper`.
- **Infrastructure**: Requires a reachable Vault instance (dev mode via Docker Compose for local dev; production via internal VPN).
- **Security boundary**: Gateway must never log or return private key material. Transit engine ensures keys never leave Vault.
