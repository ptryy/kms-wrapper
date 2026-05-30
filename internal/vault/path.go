package vault

import (
	"errors"
	"regexp"
	"strings"
)

var segmentRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

var reservedChains = map[string]struct{}{
	"evm": {}, "eth": {}, "mantra": {}, "cosmos": {}, "osmosis": {},
}

func ValidateKeyPath(path string) error {
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		return errors.New("key path must have format {project}/{chain}/{username}")
	}
	for _, part := range parts {
		if part == "" {
			return errors.New("key path segments must not be empty")
		}
		if !segmentRE.MatchString(part) {
			return errors.New("key path segments must match [a-z0-9_-]")
		}
	}
	return nil
}

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
