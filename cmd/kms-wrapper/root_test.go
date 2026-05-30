package main

import (
	"bytes"
	"path/filepath"
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

// executeSplit runs the CLI with separate stdout/stderr buffers so tests can
// assert on the warning/error stream independently from command output.
func executeSplit(args ...string) (stdout, stderr string, err error) {
	cmd := NewRootCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
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

// TestHealthMissingConfigFile_WithEnv verifies that env-only startup proceeds
// past config loading when the config file is absent: a warning is emitted on
// stderr and the command advances to the Vault reachability check (which fails
// here because no Vault is running — that's expected and distinct from a
// config error).
func TestHealthMissingConfigFile_WithEnv(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-config.yaml")
	t.Setenv("VAULT_ADDR", "http://127.0.0.1:1") // unreachable on purpose
	t.Setenv("VAULT_TOKEN", "env-token")
	t.Setenv("KMS_GATEWAY_TOKEN", "env-gw")

	stdout, stderr, err := executeSplit("health", "--config", missing)
	if err == nil {
		t.Fatal("expected health to fail against unreachable vault")
	}
	if !strings.Contains(stderr, "warning") || !strings.Contains(stderr, missing) {
		t.Fatalf("expected warning containing path %q on stderr, got stderr=%q", missing, stderr)
	}
	if strings.Contains(stdout, "Config: INVALID") {
		t.Fatalf("config should be valid when env is set, got stdout=%q", stdout)
	}
	if !strings.Contains(stdout, "Vault: UNREACHABLE") {
		t.Fatalf("expected Vault: UNREACHABLE in stdout, got stdout=%q", stdout)
	}
}

// TestHealthMissingConfigFile_NoEnv verifies that without a config file AND
// without required env vars, health reports a config error and SHALL NOT print
// "Vault: UNREACHABLE".
func TestHealthMissingConfigFile_NoEnv(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-config.yaml")
	t.Setenv("VAULT_ADDR", "")
	t.Setenv("VAULT_TOKEN", "")
	t.Setenv("KMS_VAULT_ADDR", "")
	t.Setenv("KMS_VAULT_TOKEN", "")
	t.Setenv("KMS_GATEWAY_TOKEN", "")

	stdout, _, err := executeSplit("health", "--config", missing)
	if err == nil {
		t.Fatal("expected health to fail with missing config and no env")
	}
	if strings.Contains(stdout, "Vault: UNREACHABLE") {
		t.Fatalf("config-error path must not print Vault: UNREACHABLE, got stdout=%q", stdout)
	}
	if !strings.Contains(stdout, "Config: INVALID") {
		t.Fatalf("expected Config: INVALID on stdout, got stdout=%q", stdout)
	}
	if !strings.Contains(err.Error(), "config error") {
		t.Fatalf("expected wrapped config error, got: %v", err)
	}
}
