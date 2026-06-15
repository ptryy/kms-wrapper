# Project-scoped policy for the kms-wrapper gateway.
#
# Globs target the live plugin mount (the wrapper is purpose-built on top of
# the kms-vault-plugin secrets engine at /kms, NOT the stock /transit).
#
# Vault's path-policy globs:
#   *  matches any remaining characters; only valid at the END of a path.
#   +  matches a single path segment; valid in any segment except the first.
#
# We use a single trailing `*` per resource family so the glob is portable
# across Vault versions — `proj-a/*` matches `proj-a/<chain>/<user>` and the
# bare `proj-a/` LIST path alike (where `*` is the empty string).
path "kms/keys/proj-a/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

path "kms/sign/proj-a/*" {
  capabilities = ["update"]
}
