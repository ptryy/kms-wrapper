package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvOverrideAndCompatVaultEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("vault:\n  addr: http://file:8200\n  token: file-token\ngateway:\n  addr: 127.0.0.1:9090\n  token: file-gw\nlog_level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAULT_ADDR", "http://env:8200")
	t.Setenv("VAULT_TOKEN", "env-token")
	t.Setenv("KMS_GATEWAY_TOKEN", "env-gw")
	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Vault.Addr != "http://env:8200" || cfg.Vault.Token != "env-token" || cfg.Gateway.Token != "env-gw" {
		t.Fatalf("env overrides not applied: %+v", cfg)
	}
	if err := cfg.ValidateRuntime(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRuntime(t *testing.T) {
	cfg := Default()
	if err := cfg.ValidateRuntime(); err == nil {
		t.Fatal("expected missing vault config error")
	}
}

func TestLoadMissingExplicitPathFallsBackToEnv(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.yaml")
	t.Setenv("VAULT_ADDR", "http://env:8200")
	t.Setenv("VAULT_TOKEN", "env-token")
	t.Setenv("KMS_GATEWAY_TOKEN", "env-gw")

	var warnings []string
	cfg, err := Load(missing, func(msg string) { warnings = append(warnings, msg) })
	if err != nil {
		t.Fatalf("missing file should be non-fatal, got: %v", err)
	}
	if cfg.Vault.Addr != "http://env:8200" || cfg.Vault.Token != "env-token" || cfg.Gateway.Token != "env-gw" {
		t.Fatalf("env fallback not applied: %+v", cfg)
	}
	if err := cfg.ValidateRuntime(); err != nil {
		t.Fatalf("validation should pass with env vars: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], missing) {
		t.Fatalf("warning should include the missing path %q, got: %q", missing, warnings[0])
	}
	if !strings.Contains(warnings[0], "env/defaults") {
		t.Fatalf("warning should mention env/defaults fallback, got: %q", warnings[0])
	}
}

func TestLoadMissingDefaultPathFallsBackToEnv(t *testing.T) {
	// Simulates the default ~/.kms-wrapper/config.yaml scenario by pointing at a
	// path under a temp HOME that does not exist.
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, ".kms-wrapper", "config.yaml")
	t.Setenv("VAULT_ADDR", "http://env:8200")
	t.Setenv("VAULT_TOKEN", "env-token")
	t.Setenv("KMS_GATEWAY_TOKEN", "env-gw")

	cfg, err := Load(defaultPath, nil)
	if err != nil {
		t.Fatalf("missing default path should be non-fatal, got: %v", err)
	}
	if err := cfg.ValidateRuntime(); err != nil {
		t.Fatalf("validation should pass with env vars: %v", err)
	}
}

func TestLoadMissingFileAndMissingEnvFailsValidation(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.yaml")
	// Explicitly clear any env vars that could satisfy validation.
	t.Setenv("VAULT_ADDR", "")
	t.Setenv("VAULT_TOKEN", "")
	t.Setenv("KMS_VAULT_ADDR", "")
	t.Setenv("KMS_VAULT_TOKEN", "")
	t.Setenv("KMS_GATEWAY_TOKEN", "")

	cfg, err := Load(missing, nil)
	if err != nil {
		t.Fatalf("Load itself should not fail on missing file, got: %v", err)
	}
	if err := cfg.ValidateRuntime(); err == nil {
		t.Fatal("expected runtime validation to fail with no file and no env")
	}
}

func TestLoadMalformedYAMLIsFatal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("vault: : :\n  bad: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(cfgPath, nil)
	if err == nil {
		t.Fatal("expected malformed YAML to be fatal")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Fatalf("expected 'read config' in error, got: %v", err)
	}
}

func TestLoadNilOnWarnIsSafe(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.yaml")
	t.Setenv("VAULT_ADDR", "http://env:8200")
	t.Setenv("VAULT_TOKEN", "env-token")
	t.Setenv("KMS_GATEWAY_TOKEN", "env-gw")
	if _, err := Load(missing, nil); err != nil {
		t.Fatalf("nil onWarn should not cause failure, got: %v", err)
	}
}
