package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Vault struct {
		Addr  string `mapstructure:"addr"`
		Token string `mapstructure:"token"`
	} `mapstructure:"vault"`
	Gateway struct {
		Addr  string `mapstructure:"addr"`
		Token string `mapstructure:"token"`
	} `mapstructure:"gateway"`
	LogLevel string `mapstructure:"log_level"`
}

func Default() Config {
	var cfg Config
	cfg.Gateway.Addr = "127.0.0.1:8080"
	cfg.LogLevel = "info"
	return cfg
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("KMS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("gateway.addr", "127.0.0.1:8080")
	v.SetDefault("log_level", "info")
	_ = v.BindEnv("vault.addr", "KMS_VAULT_ADDR", "VAULT_ADDR")
	_ = v.BindEnv("vault.token", "KMS_VAULT_TOKEN", "VAULT_TOKEN")
	_ = v.BindEnv("gateway.addr", "KMS_GATEWAY_ADDR")
	_ = v.BindEnv("gateway.token", "KMS_GATEWAY_TOKEN")
	_ = v.BindEnv("log_level", "KMS_LOG_LEVEL")
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) {
				return Config{}, fmt.Errorf("read config: %w", err)
			}
		}
	}
	cfg := Default()
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) ValidateRuntime() error {
	if c.Vault.Addr == "" {
		return errors.New("vault addr is required")
	}
	if c.Vault.Token == "" {
		return errors.New("vault token is required")
	}
	return nil
}
