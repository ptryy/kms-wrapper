package vault

import "testing"

func TestValidateKeyPath(t *testing.T) {
	for _, path := range []string{"proj-a/evm/alice", "proj/mantra/bob", "proj/mychain/user_1"} {
		if err := ValidateKeyPath(path); err != nil {
			t.Fatalf("%s should be valid: %v", path, err)
		}
	}
	if got := ToVaultPath("proj-a/evm/alice"); got != "transit/keys/proj-a/evm/alice" {
		t.Fatalf("unexpected vault path %s", got)
	}
	cases := map[string]string{
		"Proj A/EVM/Alice": "key path segments must match [a-z0-9_-]",
		"proj/evm":         "key path must have format {project}/{chain}/{username}",
		"proj//alice":      "key path segments must not be empty",
	}
	for path, want := range cases {
		if err := ValidateKeyPath(path); err == nil || err.Error() != want {
			t.Fatalf("%s error = %v, want %q", path, err, want)
		}
	}
}
