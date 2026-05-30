path "kms/keys/proj-a/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

path "kms/sign/proj-a/*" {
  capabilities = ["update"]
}
