# Copilot instructions for kms-wrapper

## Build, test, and lint

- Build the CLI/gateway binary: `make build` or `go build ./cmd/kms-wrapper`
- Build the Vault plugin for local Docker Vault: `make build-plugin`
- Run the full Go test suite: `go test ./...` or `make test`
- Run one package: `go test ./internal/gateway`
- Run one test: `go test ./internal/gateway -run TestCreateKeyHappyPath`
- Run vet/build/test validation: `go vet ./... && go build ./cmd/kms-wrapper && go test ./...`
- Run lint when `golangci-lint` is installed: `make lint`
- Regenerate Swagger/OpenAPI after changing handlers, DTOs, or annotations: `make swagger`
- Check committed Swagger docs are current: `make swagger-check`
- Start the local Vault dev stack and register the plugin: `make dev-up`; stop it with `make dev-down`
- Reset the gitignored `.env` back to placeholders after local Vault sessions: `make scrub-env`

## Architecture

`kms-wrapper` is a Go CLI and REST gateway for multi-chain signing backed by HashiCorp Vault. It uses a custom Vault secrets plugin, `kms-vault-plugin`, because Vault OSS Transit does not support secp256k1 and because chain-specific digesting must happen outside Vault. Private key material stays inside the Vault plugin; callers submit 32-byte pre-hashed digests for raw secp256k1 signing.

There are two binaries:

- `cmd/kms-wrapper` is the Cobra CLI and REST gateway entrypoint. It loads config, creates the Vault client, wires EVM/Cosmos signers, and starts the HTTP server.
- `cmd/kms-vault-plugin` serves the Vault plugin backend from `internal/plugin`.

Core package flow:

- `internal/plugin` owns Vault storage and plugin endpoints under `kms/keys/...` and `kms/sign/...`. It generates secp256k1 keys, stores `KeyEntry` records under `keys/`, returns only public-safe key info, and signs only 32-byte hex digests.
- `internal/vault` is the gateway-side Vault client. It maps logical key paths to Vault plugin paths with `ToVaultPath`, validates paths before calls, caches public-key lookups per path, maps Vault response errors to `pkg/types` sentinel errors, and exposes observability hooks without importing Prometheus.
- `internal/signer/evm` and `internal/signer/cosmos` perform chain-specific hashing/signature assembly before calling Vault. EVM supports raw transactions, personal messages, and EIP-712 digests; Cosmos supports DIRECT protobuf sign docs and AMINO_JSON with duplicate-key detection and canonical JSON.
- `internal/gateway` owns HTTP routing, middleware, auth, rate limiting, probes, metrics, Swagger serving, and handler-to-signer orchestration. Canonical API routes are mounted at `/v1/...`; bare routes are still mounted with `Deprecation` and `Sunset` headers for backward compatibility.
- `internal/config` loads defaults, optional YAML config, and environment variables. Precedence is defaults -> config file -> environment overrides. Missing config files warn and continue; malformed or unreadable config files are fatal.
- `pkg/types` contains shared API request/response DTOs and sentinel errors used across gateway and Vault-client boundaries.

## Repository conventions

- Key paths are always `{project}/{environment}/{username}` with lowercase `[a-z0-9_-]` segments. The `{environment}` segment is free-form (e.g. `prod`, `staging`, `dev`). Use `internal/keypath` as the shared validator across CLI, gateway, plugin, and Vault client code; list prefixes may be empty or leading path segments.
- Plugin paths are `kms/keys/<path>` for key lifecycle and `kms/sign/<path>` for signing. The plugin must never return private key bytes in responses.
- Signing inputs to the plugin must already be chain-appropriate 32-byte hashes. Do not add hashing inside `internal/plugin`; hashing belongs in the chain-specific signer packages.
- Key creation is idempotent. Existing keys are returned without regenerating key material; the gateway returns `already_existed: true` and uses HTTP 200 for existing keys, HTTP 201 for newly created keys.
- Gateway errors use JSON shape `{"error":"..."}` via `writeError`. Vault/client errors should be mapped through `pkg/types` sentinels so handlers can preserve 400/403/404 vs 500 behavior.
- Authenticated routes use bearer auth and the shared authenticated rate-limit budget. `/sign/*` and `/keys*` are authenticated and rate-limited; probe/metrics endpoints are unauthenticated but IP-rate-limited.
- `gateway.swagger_auth` defaults to true. The server refuses weak Vault/gateway tokens and unauthenticated Swagger on non-loopback addresses unless `KMS_DEV=true`.
- Request middleware order in `internal/gateway` is significant: request ID -> panic recovery -> request logging -> 405 JSON rewrite -> mux. Keep request ID outermost so recovered errors include the request ID.
- Swagger docs are generated from annotations and committed under `docs/`. Runtime `/swagger/doc.json` rewrites `servers[0].url` from the request origin; keep committed docs as the static artifact and runtime docs as the live source of truth.
- `CLAUDE.md` delegates planning/proposal workflows to OpenSpec. For new capabilities, breaking changes, architecture shifts, big performance/security work, or ambiguous changes, read `openspec/AGENTS.md`, inspect current specs/changes, create a verb-led kebab-case change proposal, and validate with `openspec validate <change-id> --strict --no-interactive` before implementation.
