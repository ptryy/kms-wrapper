package config

import (
	"os"
	"path/filepath"
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
	cfg, err := Load(cfgPath)
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
