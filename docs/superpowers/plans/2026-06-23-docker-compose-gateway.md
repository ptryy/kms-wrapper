# Docker Compose Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a containerized `gateway` service to docker-compose.yml so the full local stack (Vault + gateway) can be started with `make dev-up` for integration testing.

**Architecture:** A multi-stage Dockerfile builds the `kms-wrapper` binary; docker-compose adds a `gateway` service that depends on a healthy Vault, connects to it via Docker's internal network, and exposes port 3010 on the host. The gateway runs in `KMS_DEV=true` mode using the Vault root token so no scoped-token bootstrapping is required.

**Tech Stack:** Docker multi-stage build (golang:1.25-alpine → alpine:3.20), Docker Compose v2, Go 1.25.

## Global Constraints

- Gateway token: `dev-token` (weak token, allowed via `KMS_DEV=true`)
- Vault token: `root` (weak token, allowed via `KMS_DEV=true`)
- `KMS_DEV=true` is **required** for both the gateway container and `vault/init.sh`
- Gateway listen address inside the container: `0.0.0.0:3010` (non-loopback allowed only with `KMS_DEV=true`)
- Vault address from the gateway container: `http://vault:8200` (Docker service DNS, not `127.0.0.1`)
- `vault/init.sh` still runs on the host (it registers the plugin — without it the `kms/` mount does not exist and all signing calls fail)
- Do not modify `vault/init.sh` — it is idempotent and already handles `KMS_DEV=true` correctly

---

### Task 1: Create Dockerfile and .dockerignore

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

**Interfaces:**
- Produces: `kms-wrapper` binary at `/app/kms-wrapper` inside the image, entrypoint `["./kms-wrapper"]`

---

- [ ] **Step 1: Create `.dockerignore`**

```
# build outputs
bin/
kms-wrapper
vault/plugins/kms-vault-plugin

# dev / docs artifacts
*.html
testing-results.md
testing-guide.md
docs/archive/
docs/superpowers/
```

Write to `.dockerignore` at the project root.

- [ ] **Step 2: Create `Dockerfile`**

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o kms-wrapper ./cmd/kms-wrapper

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /build/kms-wrapper .
ENTRYPOINT ["./kms-wrapper"]
CMD ["serve"]
```

Write to `Dockerfile` at the project root.

- [ ] **Step 3: Verify the image builds**

Run from the project root:
```bash
docker build -t kms-wrapper-gateway .
```

Expected: Build completes. Final line should be similar to:
```
Successfully tagged kms-wrapper-gateway:latest
```
The build will pull `golang:1.25-alpine` and `alpine:3.20` on first run — this is expected.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "build: add multi-stage Dockerfile for gateway"
```

---

### Task 2: Add Vault health check and gateway service to docker-compose.yml

**Files:**
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: `Dockerfile` from Task 1 (gateway image build)
- Produces: `vault` service with health check; `gateway` service listening at `0.0.0.0:3010`, reachable on the host at `http://localhost:3010`

---

The current `docker-compose.yml`:
```yaml
services:
  vault:
    image: hashicorp/vault:1.17
    command: server -dev -dev-root-token-id=root -dev-listen-address=0.0.0.0:8200
    ports:
      - "8200:8200"
    cap_add:
      - IPC_LOCK
    environment:
      VAULT_DEV_ROOT_TOKEN_ID: root
      VAULT_LOCAL_CONFIG: |
        plugin_directory = "/vault/plugins"
    volumes:
      - ./vault/plugins:/vault/plugins
```

- [ ] **Step 1: Add health check to the vault service**

The `vault status -address=...` command returns exit 0 when Vault is active and unsealed (the normal state in dev mode). This is how `depends_on: condition: service_healthy` knows Vault is ready.

Replace the vault service block with:
```yaml
  vault:
    image: hashicorp/vault:1.17
    command: server -dev -dev-root-token-id=root -dev-listen-address=0.0.0.0:8200
    ports:
      - "8200:8200"
    cap_add:
      - IPC_LOCK
    environment:
      VAULT_DEV_ROOT_TOKEN_ID: root
      VAULT_LOCAL_CONFIG: |
        plugin_directory = "/vault/plugins"
    volumes:
      - ./vault/plugins:/vault/plugins
    healthcheck:
      test: ["CMD", "vault", "status", "-address=http://127.0.0.1:8200"]
      interval: 2s
      timeout: 5s
      retries: 15
      start_period: 5s
```

- [ ] **Step 2: Add gateway service**

Append the gateway service to `docker-compose.yml`. The full file becomes:

```yaml
services:
  vault:
    image: hashicorp/vault:1.17
    command: server -dev -dev-root-token-id=root -dev-listen-address=0.0.0.0:8200
    ports:
      - "8200:8200"
    cap_add:
      - IPC_LOCK
    environment:
      VAULT_DEV_ROOT_TOKEN_ID: root
      VAULT_LOCAL_CONFIG: |
        plugin_directory = "/vault/plugins"
    volumes:
      - ./vault/plugins:/vault/plugins
    healthcheck:
      test: ["CMD", "vault", "status", "-address=http://127.0.0.1:8200"]
      interval: 2s
      timeout: 5s
      retries: 15
      start_period: 5s

  gateway:
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "3010:3010"
    environment:
      VAULT_ADDR: http://vault:8200
      VAULT_TOKEN: root
      KMS_VAULT_ADDR: http://vault:8200
      KMS_VAULT_TOKEN: root
      KMS_GATEWAY_TOKEN: dev-token
      KMS_GATEWAY_ADDR: 0.0.0.0:3010
      KMS_DEV: "true"
      KMS_GATEWAY_SWAGGER_ENABLED: "true"
      KMS_GATEWAY_SWAGGER_AUTH: "false"
    depends_on:
      vault:
        condition: service_healthy
```

- [ ] **Step 3: Validate the compose file**

```bash
docker compose config
```

Expected: YAML printed with no errors. Confirm `vault.healthcheck` and `gateway.depends_on` appear in the output.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml
git commit -m "docker: add vault healthcheck and containerized gateway service"
```

---

### Task 3: Update Makefile dev-up target

**Files:**
- Modify: `Makefile`

**Interfaces:**
- Consumes: `vault/init.sh` (unchanged) — just adds `KMS_DEV=true` to its invocation
- Produces: `make dev-up` starts both vault and gateway, registers the plugin, and prints a ready message

---

Current `dev-up` target:
```makefile
dev-up: build-plugin
	docker compose up -d
	./vault/init.sh
```

- [ ] **Step 1: Update dev-up in Makefile**

Replace the `dev-up` target with:
```makefile
dev-up: build-plugin
	docker compose up -d --build
	KMS_DEV=true ./vault/init.sh
	@echo "==> stack ready: gateway http://localhost:3010  vault http://localhost:8200"
	@echo "    gateway token: dev-token"
```

Key changes:
- `--build` ensures the gateway image is rebuilt when source changes (otherwise docker compose uses the cached image and stale code goes unnoticed)
- `KMS_DEV=true` tells init.sh to skip scoped-token issuance and leave `VAULT_TOKEN=root` in `.env`

- [ ] **Step 2: Verify the Makefile change is syntactically valid**

```bash
make --dry-run dev-up
```

Expected output (indented commands printed without executing):
```
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o vault/plugins/kms-vault-plugin ./cmd/kms-vault-plugin
docker compose up -d --build
KMS_DEV=true ./vault/init.sh
```
(The `@echo` lines will NOT appear because `--dry-run` suppresses `@`-prefixed recipes.)

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "make: rebuild gateway on dev-up and set KMS_DEV=true for init"
```

---

### Task 4: Smoke-test the full stack

**Files:** none — verification only

**Interfaces:**
- Consumes: all of Tasks 1–3

> **Note:** This task requires Docker to be running and ports 8200 and 3010 to be free on the host.

- [ ] **Step 1: Bring up the stack**

```bash
make dev-up
```

Expected: build-plugin compiles, `docker compose up -d --build` builds the gateway image and starts both containers, `vault/init.sh` registers the plugin and prints `==> ready: kms-vault-plugin mounted at kms/`, final echo prints the addresses.

If `make dev-up` fails at init.sh with "vault did not become ready", the healthcheck retries (up to 30s) — wait and rerun `KMS_DEV=true ./vault/init.sh` manually.

- [ ] **Step 2: Check container health**

```bash
docker compose ps
```

Expected: both `vault` and `gateway` show `Status: Up` (vault shows `healthy`). Example:
```
NAME                     STATUS
kms-wrapper-gateway-1    Up
kms-wrapper-vault-1      Up (healthy)
```

- [ ] **Step 3: Verify gateway liveness probe**

```bash
curl -s http://localhost:3010/livez
```

Expected HTTP 200, body: `ok`

- [ ] **Step 4: Verify gateway readiness probe**

```bash
curl -s http://localhost:3010/readyz
```

Expected HTTP 200 (Vault reachable + plugin mount exists after init.sh).

- [ ] **Step 5: Verify authenticated endpoint**

```bash
curl -s -H "Authorization: Bearer dev-token" http://localhost:3010/v1/keys
```

Expected HTTP 200, body: `{"keys":[]}` (empty key list — no keys created yet, but the route is reachable).

- [ ] **Step 6: Tear down**

```bash
make dev-down
```

Expected: both containers stop and are removed.
