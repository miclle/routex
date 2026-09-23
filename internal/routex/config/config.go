// Package config provides application configuration loading and structures.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/viper"

	"github.com/miclle/routex/pkg/limits"
)

// Config represents the application configuration.
type Config struct {
	TrustedProxies        []string `mapstructure:"trusted_proxies"`
	Addr                  string   `mapstructure:"addr"`   // listen address, e.g. "0.0.0.0:9000"
	Driver                string   `mapstructure:"driver"` // database driver: "postgres" (default) or "mysql"
	DSN                   string   `mapstructure:"dsn"`    // database connection string
	EncryptionKey         string   `mapstructure:"encryption_key"`
	EventQueuePath        string   `mapstructure:"event_queue_path"`
	AllowPrivateSMTP      bool     `mapstructure:"-"`
	AllowPrivateEgresses  bool     `mapstructure:"-"`
	AllowPrivateUpstreams bool     `mapstructure:"-"`
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
	for i := range cfg.TrustedProxies {
		cfg.TrustedProxies[i] = expandEnv(cfg.TrustedProxies[i])
	}
	if _, err := limits.ParseTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("trusted_proxies must contain literal IP addresses or CIDRs")
	}
	cfg.Addr = expandEnv(cfg.Addr)
	cfg.Driver = expandEnv(cfg.Driver)
	cfg.DSN = expandEnv(cfg.DSN)
	cfg.EncryptionKey = expandEnv(cfg.EncryptionKey)
	cfg.EventQueuePath = expandEnv(cfg.EventQueuePath)
	if cfg.EventQueuePath == "" {
		cfg.EventQueuePath = filepath.Join("data", "calls.db")
	}
	if strings.ContainsRune(cfg.EventQueuePath, 0) {
		return nil, fmt.Errorf("event_queue_path contains an invalid character")
	}
	if !filepath.IsAbs(cfg.EventQueuePath) {
		cfg.EventQueuePath = filepath.Join(filepath.Dir(path), cfg.EventQueuePath)
	}
	cfg.EventQueuePath, err = filepath.Abs(cfg.EventQueuePath)
	if err != nil {
		return nil, fmt.Errorf("resolve event_queue_path failed")
	}
	if raw := expandEnv(v.GetString("allow_private_upstreams")); raw != "" {
		var err error
		cfg.AllowPrivateUpstreams, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("allow_private_upstreams must be a boolean")
		}
	}

	if raw := expandEnv(v.GetString("allow_private_egresses")); raw != "" {
		var err error
		cfg.AllowPrivateEgresses, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("allow_private_egresses must be a boolean")
		}
	}

	if raw := expandEnv(v.GetString("allow_private_smtp")); raw != "" {
		var err error
		cfg.AllowPrivateSMTP, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("allow_private_smtp must be a boolean")
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
