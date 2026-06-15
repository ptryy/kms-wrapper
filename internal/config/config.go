package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Vault struct {
		Addr  string `mapstructure:"addr"`
		Token string `mapstructure:"token"`
	} `mapstructure:"vault"`
	Gateway struct {
		Addr             string   `mapstructure:"addr"`
		Token            string   `mapstructure:"token"`
		TLSCertFile      string   `mapstructure:"tls_cert_file"`
		TLSKeyFile       string   `mapstructure:"tls_key_file"`
		RateLimit        float64  `mapstructure:"rate_limit"`
		RateBurst        int      `mapstructure:"rate_burst"`
		HealthRateLimit  float64  `mapstructure:"health_rate_limit"`
		HealthRateBurst  int      `mapstructure:"health_rate_burst"`
		TrustedProxies   []string `mapstructure:"trusted_proxies"`
		PublicURL        string   `mapstructure:"public_url"`
		SwaggerEnabled   bool     `mapstructure:"swagger_enabled"`
		SwaggerAuth      bool     `mapstructure:"swagger_auth"`
	} `mapstructure:"gateway"`
	LogLevel string `mapstructure:"log_level"`
}

func Default() Config {
	var cfg Config
	cfg.Gateway.Addr = "127.0.0.1:8080"
	cfg.Gateway.RateLimit = 100
	cfg.Gateway.RateBurst = 20
	cfg.Gateway.HealthRateLimit = 10
	cfg.Gateway.HealthRateBurst = 5
	cfg.Gateway.SwaggerEnabled = true
	cfg.Gateway.SwaggerAuth = true
	cfg.LogLevel = "info"
	return cfg
}

// Load reads configuration from `path` (if present), env vars, and defaults.
// A missing file at `path` is non-fatal: onWarn is invoked with a message and
// resolution continues using env/defaults. Malformed or unreadable files are
// fatal. onWarn may be nil.
func Load(path string, onWarn func(string)) (Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("KMS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("gateway.addr", "127.0.0.1:8080")
	v.SetDefault("gateway.rate_limit", 100)
	v.SetDefault("gateway.rate_burst", 20)
	v.SetDefault("gateway.health_rate_limit", 10)
	v.SetDefault("gateway.health_rate_burst", 5)
	v.SetDefault("gateway.swagger_enabled", true)
	v.SetDefault("gateway.swagger_auth", true)
	v.SetDefault("log_level", "info")
	_ = v.BindEnv("vault.addr", "KMS_VAULT_ADDR", "VAULT_ADDR")
	_ = v.BindEnv("vault.token", "KMS_VAULT_TOKEN", "VAULT_TOKEN")
	_ = v.BindEnv("gateway.addr", "KMS_GATEWAY_ADDR")
	_ = v.BindEnv("gateway.token", "KMS_GATEWAY_TOKEN")
	_ = v.BindEnv("gateway.tls_cert_file", "KMS_GATEWAY_TLS_CERT_FILE")
	_ = v.BindEnv("gateway.tls_key_file", "KMS_GATEWAY_TLS_KEY_FILE")
	_ = v.BindEnv("gateway.rate_limit", "KMS_GATEWAY_RATE_LIMIT")
	_ = v.BindEnv("gateway.rate_burst", "KMS_GATEWAY_RATE_BURST")
	_ = v.BindEnv("gateway.health_rate_limit", "KMS_GATEWAY_HEALTH_RATE_LIMIT")
	_ = v.BindEnv("gateway.health_rate_burst", "KMS_GATEWAY_HEALTH_RATE_BURST")
	_ = v.BindEnv("gateway.public_url", "KMS_GATEWAY_PUBLIC_URL")
	_ = v.BindEnv("gateway.swagger_enabled", "KMS_GATEWAY_SWAGGER_ENABLED")
	_ = v.BindEnv("gateway.swagger_auth", "KMS_GATEWAY_SWAGGER_AUTH")
	_ = v.BindEnv("log_level", "KMS_LOG_LEVEL")
	if err := validateBoolEnv("KMS_GATEWAY_SWAGGER_ENABLED"); err != nil {
		return Config{}, err
	}
	if err := validateBoolEnv("KMS_GATEWAY_SWAGGER_AUTH"); err != nil {
		return Config{}, err
	}
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			if isFileNotFound(err) {
				if onWarn != nil {
					onWarn(fmt.Sprintf("warning: config file %q not found; falling back to env/defaults", path))
				}
			} else {
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

// isFileNotFound returns true for the two error shapes viper can produce when a
// config file is absent: ConfigFileNotFoundError (discovery flow) and a wrapped
// fs.ErrNotExist (explicit SetConfigFile path).
func isFileNotFound(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	return errors.Is(err, fs.ErrNotExist)
}

func validateBoolEnv(key string) error {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if _, err := strconv.ParseBool(raw); err != nil {
		return fmt.Errorf("invalid boolean value for %s: %q", key, raw)
	}
	return nil
}

func (c Config) ValidateRuntime() error {
	if c.Vault.Addr == "" {
		return errors.New("vault addr is required")
	}
	if c.Vault.Token == "" {
		return errors.New("vault token is required")
	}
	if c.Gateway.Token == "" {
		return errors.New("gateway token is required")
	}
	return nil
}
