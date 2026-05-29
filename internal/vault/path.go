package vault

import (
	"errors"
	"log"
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
	if _, ok := reservedChains[parts[1]]; !ok {
		log.Printf("unknown chain identifier: %s", parts[1])
	}
	return nil
}

func ToVaultPath(path string) string {
	return "transit/keys/" + path
}
