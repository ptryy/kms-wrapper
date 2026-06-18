package keypath

import (
	"strings"
	"testing"
)

func TestValidate_ValidEnvironmentPath(t *testing.T) {
	if err := Validate("proj-a/prod/alice"); err != nil {
		t.Fatalf("expected valid path, got error: %v", err)
	}
}

func TestValidate_WrongSegmentCount_MentionsEnvironment(t *testing.T) {
	err := Validate("proj/prod")
	if err == nil {
		t.Fatal("expected error for 2-segment path")
	}
	if !strings.Contains(err.Error(), "{project}/{environment}/{username}") {
		t.Fatalf("error must reference {environment} format, got: %q", err.Error())
	}
	if strings.Contains(err.Error(), "{chain}") {
		t.Fatalf("error must not reference {chain}, got: %q", err.Error())
	}
}

func TestValidateListPrefix_WrongCount_MentionsEnvironment(t *testing.T) {
	err := ValidateListPrefix("a/b/c/d")
	if err == nil {
		t.Fatal("expected error for 4-segment prefix")
	}
	if !strings.Contains(err.Error(), "{project}/{environment}/{username}") {
		t.Fatalf("error must reference {environment} format, got: %q", err.Error())
	}
	if strings.Contains(err.Error(), "{chain}") {
		t.Fatalf("error must not reference {chain}, got: %q", err.Error())
	}
}

func TestValidate_FreeFormMiddleSegment_NoReservedList(t *testing.T) {
	// Any [a-z0-9_-] middle segment is valid; there is no reserved-name list.
	for _, p := range []string{"proj/staging/bob", "proj/mychain/bob", "proj/dev/bob"} {
		if err := Validate(p); err != nil {
			t.Fatalf("expected %q valid, got: %v", p, err)
		}
	}
}
