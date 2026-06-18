// Package keypath holds the canonical {project}/{environment}/{username} validator
// shared by the REST gateway, the CLI, and the Vault plugin. Keeping it as a
// leaf package avoids an internal/plugin → internal/vault import cycle while
// guaranteeing the three trust boundaries agree on the format.
package keypath

import (
	"errors"
	"regexp"
	"strings"
)

var segmentRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Validate checks a fully-qualified key path of the form
// {project}/{environment}/{username}.
func Validate(path string) error {
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		return errors.New("key path must have format {project}/{environment}/{username}")
	}
	return validateSegments(parts)
}

// ValidateListPrefix accepts any leading prefix of the key path hierarchy:
// the empty string, "<project>", "<project>/<environment>", or a trailing slash on
// either of those forms. Anything else (e.g. a 4-segment value, ".." segment,
// uppercase) is rejected with the same per-segment regex as Validate.
func ValidateListPrefix(s string) error {
	trimmed := strings.TrimSuffix(s, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) > 3 {
		return errors.New("key path must have format {project}/{environment}/{username}")
	}
	return validateSegments(parts)
}

func validateSegments(parts []string) error {
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
