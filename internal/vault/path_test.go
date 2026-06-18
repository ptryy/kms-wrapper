package vault

import "testing"

func TestValidateKeyPath(t *testing.T) {
	for _, path := range []string{"proj-a/prod/alice", "proj/prod/bob", "proj/mychain/user_1"} {
		if err := ValidateKeyPath(path); err != nil {
			t.Fatalf("%s should be valid: %v", path, err)
		}
	}
	if got := ToVaultPath("proj-a/prod/alice"); got != "kms/keys/proj-a/prod/alice" {
		t.Fatalf("unexpected vault path %s", got)
	}
	if got := ToSignPath("proj-a/prod/alice"); got != "kms/sign/proj-a/prod/alice" {
		t.Fatalf("unexpected sign path %s", got)
	}
	cases := map[string]string{
		"Proj A/PROD/Alice": "key path segments must match [a-z0-9_-]",
		"proj/prod":         "key path must have format {project}/{environment}/{username}",
		"proj//alice":       "key path segments must not be empty",
	}
	for path, want := range cases {
		if err := ValidateKeyPath(path); err == nil || err.Error() != want {
			t.Fatalf("%s error = %v, want %q", path, err, want)
		}
	}
}
