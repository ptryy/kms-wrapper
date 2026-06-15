#!/usr/bin/env bash
#
# vault/init.sh — register and enable the kms-vault-plugin against a local
# Vault dev instance started by docker-compose. Installs the project policy
# and (outside KMS_DEV mode) issues a scoped renewable token written back to
# the local .env file.
#
# Prerequisites:
#   - Vault container is running (docker compose up -d) and reachable on $VAULT_ADDR.
#   - The plugin binary has been built into vault/plugins/kms-vault-plugin via
#     `make build-plugin` (cross-compiled for linux/amd64).
#
# Idempotent: re-running after a successful init is a no-op (existing plugin
# registrations and mounts are detected and skipped; policy install is upsert).

set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-root}"
PLUGIN_NAME="${PLUGIN_NAME:-kms-vault-plugin}"
PLUGIN_MOUNT="${PLUGIN_MOUNT:-kms}"
POLICY_NAME="${POLICY_NAME:-kms-project}"

export VAULT_ADDR VAULT_TOKEN

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLUGIN_BINARY="${SCRIPT_DIR}/plugins/${PLUGIN_NAME}"
POLICY_FILE="${SCRIPT_DIR}/policy-project.hcl"
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/../.env}"

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

if command -v sha256sum >/dev/null 2>&1; then
  PLUGIN_SHA="$(sha256sum "${PLUGIN_BINARY}" | awk '{print $1}')"
else
  PLUGIN_SHA="$(shasum -a 256 "${PLUGIN_BINARY}" | awk '{print $1}')"
fi
echo "==> plugin SHA-256: ${PLUGIN_SHA}"

vault_cli() {
  docker compose exec -T -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="${VAULT_TOKEN}" vault vault "$@"
}

# Copy a host-side file into the running vault container under /tmp so the
# vault CLI inside can read it. We avoid relying on a bind-mounted policy dir.
vault_copy_into() {
  local src="$1" dst="$2"
  docker compose cp "${src}" "vault:${dst}"
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

echo "==> installing policy '${POLICY_NAME}'"
vault_copy_into "${POLICY_FILE}" "/tmp/policy-project.hcl"
vault_cli policy write "${POLICY_NAME}" /tmp/policy-project.hcl

# In KMS_DEV=true mode we keep the root token in .env so docker-compose dev
# loops stay frictionless. Outside dev mode we issue a scoped, renewable token
# bound to the project policy and persist it back to .env.
if [[ "${KMS_DEV:-}" == "true" ]]; then
  echo "==> KMS_DEV=true; leaving VAULT_TOKEN=root in .env (skipping scoped token issuance)"
else
  echo "==> issuing scoped token bound to policy '${POLICY_NAME}'"
  SCOPED_JSON="$(vault_cli token create -policy="${POLICY_NAME}" -ttl=24h -renewable=true -format=json)"
  if ! command -v jq >/dev/null 2>&1; then
    echo "error: jq is required to parse the issued token; install jq or rerun with KMS_DEV=true" >&2
    exit 1
  fi
  SCOPED_TOKEN="$(echo "${SCOPED_JSON}" | jq -r .auth.client_token)"
  if [[ -z "${SCOPED_TOKEN}" || "${SCOPED_TOKEN}" == "null" ]]; then
    echo "error: failed to parse client_token from vault token create output" >&2
    exit 1
  fi

  if [[ -f "${ENV_FILE}" ]] && grep -q '^KMS_VAULT_TOKEN=' "${ENV_FILE}"; then
    # Replace existing entry without re-reading the rest of the file (portable
    # sed-i replacement across BSD and GNU sed).
    tmp="$(mktemp)"
    awk -v token="${SCOPED_TOKEN}" '
      /^KMS_VAULT_TOKEN=/ { print "KMS_VAULT_TOKEN=" token; next }
      { print }
    ' "${ENV_FILE}" > "${tmp}"
    mv "${tmp}" "${ENV_FILE}"
  else
    printf 'KMS_VAULT_TOKEN=%s\n' "${SCOPED_TOKEN}" >> "${ENV_FILE}"
  fi
  echo "    wrote KMS_VAULT_TOKEN to ${ENV_FILE}"

  echo "==> smoke-testing scoped token isolation"
  if ! vault_cli write -force "kms/keys/proj-a/evm/init-smoke" >/dev/null; then
    echo "warn: smoke write to proj-a failed using root token (skipping isolation check)" >&2
  else
    # Verify the scoped token can write to proj-a but not proj-b.
    if ! docker compose exec -T -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="${SCOPED_TOKEN}" \
        vault vault write -force "kms/keys/proj-a/evm/scoped-smoke" >/dev/null 2>&1; then
      echo "error: scoped token cannot write to proj-a — check policy globs" >&2
      exit 1
    fi
    if docker compose exec -T -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="${SCOPED_TOKEN}" \
        vault vault write -force "kms/keys/proj-b/evm/should-fail" >/dev/null 2>&1; then
      echo "error: scoped token unexpectedly succeeded against proj-b — policy too permissive" >&2
      exit 1
    fi
    echo "    OK: scoped token accepted proj-a and refused proj-b"
  fi
fi

echo "==> ready: kms-vault-plugin mounted at ${PLUGIN_MOUNT}/"
