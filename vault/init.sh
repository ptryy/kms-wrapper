#!/usr/bin/env bash
#
# vault/init.sh — register and enable the kms-vault-plugin against a local
# Vault dev instance started by docker-compose.
#
# Prerequisites:
#   - Vault container is running (docker compose up -d) and reachable on $VAULT_ADDR.
#   - The plugin binary has been built into vault/plugins/kms-vault-plugin via
#     `make build-plugin` (cross-compiled for linux/amd64).
#
# Idempotent: re-running after a successful init is a no-op (existing plugin
# registrations and mounts are detected and skipped).

set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"
PLUGIN_NAME="${PLUGIN_NAME:-kms-vault-plugin}"
PLUGIN_MOUNT="${PLUGIN_MOUNT:-kms}"

export VAULT_ADDR VAULT_TOKEN

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLUGIN_BINARY="${SCRIPT_DIR}/plugins/${PLUGIN_NAME}"

if [[ ! -f "${PLUGIN_BINARY}" ]]; then
  echo "error: plugin binary not found at ${PLUGIN_BINARY}" >&2
  echo "       run 'make build-plugin' first" >&2
  exit 1
fi

echo "==> waiting for Vault at ${VAULT_ADDR} to become ready"
for attempt in $(seq 1 30); do
  if curl --silent --fail --max-time 2 "${VAULT_ADDR}/v1/sys/health" >/dev/null 2>&1; then
    break
  fi
  if (( attempt == 30 )); then
    echo "error: vault did not become ready within 30 attempts" >&2
    exit 1
  fi
  sleep 1
done

# Compute SHA-256 of the binary as Vault sees it (inside the container, mounted
# at /vault/plugins/<name>). The hash is independent of mount point, so the
# host-side hash is identical.
if command -v sha256sum >/dev/null 2>&1; then
  PLUGIN_SHA="$(sha256sum "${PLUGIN_BINARY}" | awk '{print $1}')"
else
  PLUGIN_SHA="$(shasum -a 256 "${PLUGIN_BINARY}" | awk '{print $1}')"
fi
echo "==> plugin SHA-256: ${PLUGIN_SHA}"

vault_cli() {
  docker compose exec -T -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="${VAULT_TOKEN}" vault vault "$@"
}

echo "==> registering plugin '${PLUGIN_NAME}'"
if vault_cli plugin info -version="" secret "${PLUGIN_NAME}" >/dev/null 2>&1; then
  echo "    plugin already registered; re-registering with current SHA"
  vault_cli plugin deregister secret "${PLUGIN_NAME}" >/dev/null 2>&1 || true
fi
vault_cli plugin register \
  -sha256="${PLUGIN_SHA}" \
  -command="${PLUGIN_NAME}" \
  secret "${PLUGIN_NAME}"

echo "==> enabling plugin at mount '${PLUGIN_MOUNT}/'"
if vault_cli secrets list -format=json | grep -q "\"${PLUGIN_MOUNT}/\""; then
  echo "    mount '${PLUGIN_MOUNT}/' already exists; skipping enable"
else
  vault_cli secrets enable -path="${PLUGIN_MOUNT}" "${PLUGIN_NAME}"
fi

echo "==> ready: kms-vault-plugin mounted at ${PLUGIN_MOUNT}/"
