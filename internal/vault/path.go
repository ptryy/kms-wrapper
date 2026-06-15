package vault

import "github.com/ryan-truong/kms-wrapper/internal/keypath"

// ValidateKeyPath delegates to internal/keypath so that the gateway, CLI, and
// the Vault plugin (which cannot import this package without pulling in the
// whole vault/api client surface) all share a single validator.
func ValidateKeyPath(path string) error { return keypath.Validate(path) }

// ToVaultPath returns the Vault logical path for the key entry inside the
// kms-vault-plugin (`kms/keys/<path>`).
func ToVaultPath(path string) string {
	return "kms/keys/" + path
}

// ToSignPath returns the Vault logical path for the plugin's raw-sign endpoint
// (`kms/sign/<path>`).
func ToSignPath(path string) string {
	return "kms/sign/" + path
}
