// Package config provides application configuration loading and structures.
package config

import (
	"bytes"
	"encoding/base64"
	"errors"
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
	TrustedProxies        []string           `mapstructure:"trusted_proxies"`
	Addr                  string             `mapstructure:"addr"`   // listen address, e.g. "0.0.0.0:9000"
	Driver                string             `mapstructure:"driver"` // database driver: "postgres" (default) or "mysql"
	DSN                   string             `mapstructure:"dsn"`    // database connection string
	EncryptionKey         string             `mapstructure:"encryption_key"`
	EncryptionKeyring     *EncryptionKeyring `mapstructure:"-"`
	EventQueuePath        string             `mapstructure:"event_queue_path"`
	AllowPrivateStorage   bool               `mapstructure:"-"`
	AllowPrivateSMTP      bool               `mapstructure:"-"`
	AllowPrivateEgresses  bool               `mapstructure:"-"`
	AllowPrivateUpstreams bool               `mapstructure:"-"`
}

// EncryptionKeyring declares externally provisioned root slots. Key IDs are exact
// references, and omitted legacy selection permits only versioned envelopes.
type EncryptionKeyring struct {
	LegacyKeyID string
	WriteKeyID  string
	Keys        []EncryptionRootKey
}

// EncryptionRootKey contains one bootstrap-only root; it is never an HTTP or SQL value.
type EncryptionRootKey struct {
	ID  string
	Key string
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
		return nil, errors.New("parse config failed")
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, errors.New("unmarshal config failed")
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
	cfg.EncryptionKeyring, err = readEncryptionKeyring(v, cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
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

	if raw := expandEnv(v.GetString("allow_private_storage")); raw != "" {
		var err error
		cfg.AllowPrivateStorage, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("allow_private_storage must be a boolean")
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

func readEncryptionKeyring(v *viper.Viper, legacy string) (*EncryptionKeyring, error) {
	raw := v.Get("encryption_keyring")
	if raw == nil {
		for _, key := range v.AllKeys() {
			if key == "encryption_keyring" {
				return nil, errors.New("encryption_keyring must be a mapping")
			}
		}
		return nil, nil
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("encryption_keyring must be a mapping")
	}
	for key := range fields {
		if key != "legacy_key_id" && key != "write_key_id" && key != "keys" {
			return nil, errors.New("encryption_keyring contains an unsupported field")
		}
	}
	ring := &EncryptionKeyring{}
	for field, target := range map[string]*string{"legacy_key_id": &ring.LegacyKeyID, "write_key_id": &ring.WriteKeyID} {
		value, present := fields[field]
		if !present && field == "legacy_key_id" {
			continue
		}
		text, ok := value.(string)
		text = expandEnv(text)
		if !ok || !validRootKeyID(text) {
			return nil, fmt.Errorf("encryption_keyring.%s must be a valid key ID", field)
		}
		*target = text
	}
	entries, ok := fields["keys"].([]any)
	if !ok || len(entries) == 0 || len(entries) > 64 {
		return nil, errors.New("encryption_keyring.keys must contain 1 to 64 slots")
	}
	keys := make(map[string][]byte, len(entries)+1)
	defer func() {
		for _, key := range keys {
			clear(key)
		}
	}()
	for i, entry := range entries {
		slot, ok := entry.(map[string]any)
		if !ok || len(slot) != 2 {
			return nil, fmt.Errorf("encryption_keyring.keys[%d] must contain only id and key", i)
		}
		id, idOK := slot["id"].(string)
		encoded, keyOK := slot["key"].(string)
		id, encoded = expandEnv(id), expandEnv(encoded)
		if !idOK || !validRootKeyID(id) {
			return nil, fmt.Errorf("encryption_keyring.keys[%d].id must be a valid key ID", i)
		}
		if _, exists := keys[id]; exists {
			return nil, errors.New("encryption_keyring.keys contains duplicate IDs")
		}
		key, err := decodeRootKey(encoded)
		if !keyOK || err != nil {
			clear(key)
			return nil, fmt.Errorf("encryption_keyring.keys[%d].key must be a base64-encoded 32-byte key", i)
		}
		keys[id] = key
		ring.Keys = append(ring.Keys, EncryptionRootKey{ID: id, Key: encoded})
	}
	if legacy != "" {
		if ring.LegacyKeyID == "" {
			return nil, errors.New("encryption_key requires an explicit encryption_keyring.legacy_key_id")
		}
		key, err := decodeRootKey(legacy)
		if err != nil {
			clear(key)
			return nil, errors.New("encryption_key must be a base64-encoded 32-byte key")
		}
		defer clear(key)
		if existing, ok := keys[ring.LegacyKeyID]; ok {
			if !bytes.Equal(existing, key) {
				return nil, errors.New("encryption_key conflicts with its encryption_keyring legacy slot")
			}
		} else {
			if len(keys) == 64 {
				return nil, errors.New("encryption_keyring.keys must contain at most 64 slots including legacy")
			}
			keys[ring.LegacyKeyID] = bytes.Clone(key)
			ring.Keys = append(ring.Keys, EncryptionRootKey{ID: ring.LegacyKeyID, Key: legacy})
		}
	}
	if ring.LegacyKeyID != "" && keys[ring.LegacyKeyID] == nil {
		return nil, errors.New("encryption_keyring.legacy_key_id references an unconfigured slot")
	}
	if keys[ring.WriteKeyID] == nil {
		return nil, errors.New("encryption_keyring.write_key_id references an unconfigured slot")
	}
	for i, slot := range ring.Keys {
		for _, other := range ring.Keys[:i] {
			if bytes.Equal(keys[slot.ID], keys[other.ID]) {
				return nil, errors.New("encryption_keyring.keys must use distinct root material")
			}
		}
	}
	return ring, nil
}

func validRootKeyID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, ch := range []byte(value) {
		valid := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-'
		if !valid {
			return false
		}
	}
	return true
}

func decodeRootKey(encoded string) ([]byte, error) {
	if len(encoded) != base64.StdEncoding.EncodedLen(32) {
		return nil, errors.New("invalid root key")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != encoded {
		clear(key)
		return nil, errors.New("invalid root key")
	}
	return key, nil
}
