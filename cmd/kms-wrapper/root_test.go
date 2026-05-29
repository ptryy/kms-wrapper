package main

import (
	"bytes"
	"strings"
	"testing"
)

func execute(args ...string) (string, error) {
	cmd := NewRootCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestHelpAndUnknownCommand(t *testing.T) {
	out, err := execute("--help")
	if err != nil || !strings.Contains(out, "serve") || !strings.Contains(out, "sign") {
		t.Fatalf("help out=%q err=%v", out, err)
	}
	out, err = execute("unknowncmd")
	if err == nil || !strings.Contains(out+err.Error(), "unknown command") {
		t.Fatalf("unknown out=%q err=%v", out, err)
	}
}

func TestMissingRequiredFlagMessage(t *testing.T) {
	out, err := execute("sign", "evm")
	if err == nil || !strings.Contains(out+err.Error(), "required flag missing: path") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
