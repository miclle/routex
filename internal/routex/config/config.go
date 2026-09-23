// Package config provides application configuration loading and structures.
package config

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the application configuration.
type Config struct {
	Addr                  string `mapstructure:"addr"`   // listen address, e.g. "0.0.0.0:9000"
	Driver                string `mapstructure:"driver"` // database driver: "postgres" (default) or "mysql"
	DSN                   string `mapstructure:"dsn"`    // database connection string
	EncryptionKey         string `mapstructure:"encryption_key"`
	AllowPrivateUpstreams bool   `mapstructure:"-"`
}

// Load reads configuration from the given file path.
func Load(path string) (*Config, error) {
	cfgFile, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(cfgFile)); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	// Expand parsed values so quotes, backslashes, and newlines from the
	// environment remain data rather than becoming YAML syntax.
	cfg.Addr = expandEnv(cfg.Addr)
	cfg.Driver = expandEnv(cfg.Driver)
	cfg.DSN = expandEnv(cfg.DSN)
	cfg.EncryptionKey = expandEnv(cfg.EncryptionKey)
	if raw := expandEnv(v.GetString("allow_private_upstreams")); raw != "" {
		var err error
		cfg.AllowPrivateUpstreams, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("allow_private_upstreams must be a boolean")
		}
	}

	if cfg.Addr == "" {
		return nil, fmt.Errorf("addr is required")
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("dsn is required")
	}
	if cfg.Driver == "" {
		cfg.Driver = "postgres"
	}
	if cfg.Driver != "postgres" && cfg.Driver != "mysql" {
		return nil, fmt.Errorf("unsupported driver: %s (supported: postgres, mysql)", cfg.Driver)
	}

	return &cfg, nil
}

func expandEnv(s string) string {
	return os.Expand(s, func(name string) string {
		key, fallback, ok := strings.Cut(name, ":-")
		if !ok {
			return os.Getenv(name)
		}
		if value := os.Getenv(key); value != "" {
			return value
		}
		return fallback
	})
}
