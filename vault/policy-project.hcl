path "transit/keys/proj-a/*" {
  capabilities = ["create", "read", "update", "list"]
}

path "transit/sign/proj-a/*" {
  capabilities = ["update"]
}
