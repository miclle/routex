package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadDefaultsDriver(t *testing.T) {
	path := writeConfig(t, `addr: "127.0.0.1:9000"
dsn: "host=localhost port=5432 user=postgres password=postgres dbname=app sslmode=disable"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Driver != "postgres" {
		t.Fatalf("Driver = %q, want postgres", cfg.Driver)
	}
}

func TestLoadSupportsMySQL(t *testing.T) {
	path := writeConfig(t, `addr: "127.0.0.1:9000"
driver: mysql
dsn: "root:password@tcp(localhost:3306)/app?charset=utf8mb4&parseTime=True&loc=Local"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Driver != "mysql" {
		t.Fatalf("Driver = %q, want mysql", cfg.Driver)
	}
}

func TestLoadRejectsUnsupportedDriver(t *testing.T) {
	path := writeConfig(t, `addr: "127.0.0.1:9000"
driver: sqlite
dsn: "app.db"
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load should reject unsupported driver")
	}
}

func TestLoadExpandsEnvironmentWithFallback(t *testing.T) {
	t.Setenv("ROUTEX_TEST_ADDR", "")
	t.Setenv("ROUTEX_TEST_DSN", "host=db user=app password=secret dbname=app sslmode=disable")
	path := writeConfig(t, `addr: "${ROUTEX_TEST_ADDR:-127.0.0.1:9000}"
dsn: "${ROUTEX_TEST_DSN:-fallback}"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9000" {
		t.Fatalf("Addr = %q, want fallback", cfg.Addr)
	}
	if cfg.DSN != "host=db user=app password=secret dbname=app sslmode=disable" {
		t.Fatalf("DSN = %q, want environment value", cfg.DSN)
	}
}

func TestLoadPreservesEnvironmentDSN(t *testing.T) {
	for _, dsn := range []string{
		`host=db user=app password=ab"cd dbname=routex`,
		`host=db user=app password='ab\\cd' dbname=routex`,
		"host=db user=app password='first\nsecond' dbname=routex",
		`host=db user=app password='$literal' dbname=routex`,
	} {
		t.Run(dsn, func(t *testing.T) {
			t.Setenv("ROUTEX_TEST_DSN", dsn)
			cfg, err := Load(writeConfig(t, `addr: "127.0.0.1:9000"
dsn: "${ROUTEX_TEST_DSN}"
`))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.DSN != dsn {
				t.Fatal("environment DSN was modified during configuration loading")
			}
		})
	}
}

func TestLoadExpandsAllBootstrapFields(t *testing.T) {
	t.Setenv("ROUTEX_TEST_ADDR", "127.0.0.1:9100")
	t.Setenv("ROUTEX_TEST_DRIVER", "mysql")
	t.Setenv("ROUTEX_TEST_DSN", "")
	cfg, err := Load(writeConfig(t, `addr: "${ROUTEX_TEST_ADDR}"
driver: "${ROUTEX_TEST_DRIVER}"
dsn: "${ROUTEX_TEST_DSN:-root:password@tcp(localhost:3306)/routex}"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9100" || cfg.Driver != "mysql" || cfg.DSN != "root:password@tcp(localhost:3306)/routex" {
		t.Fatal("bootstrap field expansion did not preserve values or fallbacks")
	}
}

func TestLoadRequiresAddrAndDSN(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing addr", body: `dsn: "postgres://example"`},
		{name: "missing dsn", body: `addr: "127.0.0.1:9000"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tc.body)); err == nil {
				t.Fatal("Load should reject incomplete config")
			}
		})
	}
}

func TestLoadCredentialBootstrap(t *testing.T) {
	t.Setenv("ROUTEX_TEST_ROOT", "opaque-bootstrap-key")
	t.Setenv("ROUTEX_TEST_PRIVATE", "true")
	cfg, err := Load(writeConfig(t, `addr: localhost:9000
dsn: test
encryption_key: "${ROUTEX_TEST_ROOT}"
allow_private_upstreams: "${ROUTEX_TEST_PRIVATE:-false}"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EncryptionKey != "opaque-bootstrap-key" || !cfg.AllowPrivateUpstreams {
		t.Fatal("bootstrap settings were not expanded")
	}
	if _, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_upstreams: maybe\n")); err == nil {
		t.Fatal("invalid private-upstream policy must fail")
	}
}

func TestLoadEventQueuePath(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "default"},
		{name: "relative", value: "journal/events.db"},
		{name: "absolute", value: filepath.Join(t.TempDir(), "absolute.db")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ROUTEX_TEST_EVENT_QUEUE", tc.value)
			path := writeConfig(t, "addr: localhost:9000\ndsn: test\nevent_queue_path: '${ROUTEX_TEST_EVENT_QUEUE}'\n")
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.value
			if want == "" {
				want = filepath.Join("data", "calls.db")
			}
			if !filepath.IsAbs(want) {
				want = filepath.Join(filepath.Dir(path), want)
			}
			if cfg.EventQueuePath != want {
				t.Fatalf("queue path = %q, want %q", cfg.EventQueuePath, want)
			}
		})
	}
}

func TestEventQueueDefaultsIsolateConfigurationDirectories(t *testing.T) {
	firstPath := writeConfig(t, "addr: localhost:9000\ndsn: test\n")
	secondPath := writeConfig(t, "addr: localhost:9000\ndsn: test\n")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, firstPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Load(relative)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventQueuePath == second.EventQueuePath || first.EventQueuePath != filepath.Join(filepath.Dir(firstPath), "data", "calls.db") {
		t.Fatal("relative configuration paths did not preserve isolated queue directories")
	}
}

func TestEventQueueEnvironmentRemainsData(t *testing.T) {
	value := "journal/quotes\"-backslash\\-newline\n-literal${UNCHANGED}.db"
	t.Setenv("ROUTEX_TEST_EVENT_QUEUE", value)
	path := writeConfig(t, "addr: localhost:9000\ndsn: test\nevent_queue_path: '${ROUTEX_TEST_EVENT_QUEUE:-fallback.db}'\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EventQueuePath != filepath.Join(filepath.Dir(path), value) || cfg.Addr != "localhost:9000" {
		t.Fatal("queue path environment expansion changed YAML structure or expanded twice")
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	t.Setenv("ROUTEX_TEST_PROXY", "10.0.0.0/8")
	cfg, err := Load(writeConfig(t, "addr: 127.0.0.1:9000\ndsn: test-only\ntrusted_proxies:\n  - '${ROUTEX_TEST_PROXY}'\n  - '2001:db8::/32'\n"))
	if err != nil || len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "10.0.0.0/8" {
		t.Fatal("proxy expansion failed", err)
	}
	for _, raw := range []string{"example.invalid", "10.0.0.1/33", "fe80::1%en0"} {
		if _, err := Load(writeConfig(t, "addr: 127.0.0.1:9000\ndsn: test-only\ntrusted_proxies: ['"+raw+"']\n")); err == nil {
			t.Fatal("invalid trusted proxy accepted")
		}
	}
}

func rootFixture(value byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}

func TestLoadEncryptionKeyring(t *testing.T) {
	oldKey, nextKey := rootFixture(11), rootFixture(22)
	for _, tc := range []struct {
		name, body string
		legacy     string
		keys       int
	}{
		{"explicit legacy", "  legacy_key_id: old\n  write_key_id: next\n  keys:\n    - {id: old, key: '" + oldKey + "'}\n    - {id: next, key: '" + nextKey + "'}\n", "", 2},
		{"matching encryption key", "  legacy_key_id: old\n  write_key_id: next\n  keys:\n    - {id: old, key: '" + oldKey + "'}\n    - {id: next, key: '" + nextKey + "'}\n", oldKey, 2},
		{"add explicit legacy slot", "  legacy_key_id: old\n  write_key_id: next\n  keys:\n    - {id: next, key: '" + nextKey + "'}\n", oldKey, 2},
		{"versioned only", "  write_key_id: next\n  keys:\n    - {id: next, key: '" + nextKey + "'}\n", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nencryption_key: '"+tc.legacy+"'\nencryption_keyring:\n"+tc.body))
			if err != nil {
				t.Fatal(err)
			}
			ring := cfg.EncryptionKeyring
			if ring == nil || ring.WriteKeyID != "next" || len(ring.Keys) != tc.keys || cfg.EncryptionKey != tc.legacy {
				t.Fatal("keyring selection or legacy config changed")
			}
			if tc.name == "versioned only" && ring.LegacyKeyID != "" {
				t.Fatal("versioned-only ring inferred a legacy slot")
			}
			if tc.legacy != "" {
				found := false
				for _, slot := range ring.Keys {
					if slot.ID == "old" {
						found = slot.Key == oldKey
					}
				}
				if !found {
					t.Fatal("legacy key was not mapped exactly")
				}
			}
		})
	}
}

func TestLoadRejectsAmbiguousEncryptionKeyrings(t *testing.T) {
	key, other := rootFixture(11), rootFixture(22)
	base := "addr: localhost:9000\ndsn: test\n"
	validSlot := "    - {id: private-root-marker, key: '" + key + "'}\n"
	for _, tc := range []struct{ name, body string }{
		{"null", "encryption_keyring: null\n"},
		{"empty map", "encryption_keyring: {}\n"},
		{"scalar", "encryption_keyring: private-shape-marker\n"},
		{"sequence", "encryption_keyring: []\n"},
		{"unknown field", "encryption_keyring:\n  private-field-marker: value\n"},
		{"missing write", "encryption_keyring:\n  keys:\n" + validSlot},
		{"empty write", "encryption_keyring:\n  write_key_id: ''\n  keys:\n" + validSlot},
		{"null write", "encryption_keyring:\n  write_key_id: null\n  keys:\n" + validSlot},
		{"numeric write", "encryption_keyring:\n  write_key_id: 101\n  keys:\n" + validSlot},
		{"missing keys", "encryption_keyring:\n  write_key_id: private-root-marker\n"},
		{"empty keys", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys: []\n"},
		{"mapping keys", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys: {private-root-marker: '" + key + "'}\n"},
		{"nonmapping slot", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys: [private-root-marker]\n"},
		{"missing id", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n    - {key: '" + key + "'}\n"},
		{"numeric id", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n    - {id: 101, key: '" + key + "'}\n"},
		{"extra slot field", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n    - {id: private-root-marker, key: '" + key + "', private-field-marker: data}\n"},
		{"duplicate IDs", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n" + validSlot + "    - {id: private-root-marker, key: '" + other + "'}\n"},
		{"material aliases", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n" + validSlot + "    - {id: another-root, key: '" + key + "'}\n"},
		{"missing write slot", "encryption_keyring:\n  write_key_id: missing-root\n  keys:\n" + validSlot},
		{"missing legacy slot", "encryption_keyring:\n  legacy_key_id: missing-root\n  write_key_id: private-root-marker\n  keys:\n" + validSlot},
		{"empty explicit legacy", "encryption_keyring:\n  legacy_key_id: ''\n  write_key_id: private-root-marker\n  keys:\n" + validSlot},
		{"legacy without mapping", "encryption_key: '" + key + "'\nencryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n" + validSlot},
		{"conflicting legacy", "encryption_key: '" + other + "'\nencryption_keyring:\n  legacy_key_id: private-root-marker\n  write_key_id: private-root-marker\n  keys:\n" + validSlot},
		{"legacy material alias", "encryption_key: '" + key + "'\nencryption_keyring:\n  legacy_key_id: another-root\n  write_key_id: private-root-marker\n  keys:\n" + validSlot},
		{"invalid legacy key", "encryption_key: private-invalid-marker\nencryption_keyring:\n  legacy_key_id: another-root\n  write_key_id: private-root-marker\n  keys:\n" + validSlot},
		{"numeric root key", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n    - {id: private-root-marker, key: 123}\n"},
		{"invalid root key", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n    - {id: private-root-marker, key: private-invalid-marker}\n"},
		{"short root key", "encryption_keyring:\n  write_key_id: private-root-marker\n  keys:\n    - {id: private-root-marker, key: '" + base64.StdEncoding.EncodeToString([]byte("short")) + "'}\n"},
		{"duplicate YAML field", "encryption_keyring:\n  write_key_id: private-root-marker\n  write_key_id: private-other-marker\n  keys:\n" + validSlot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, base+tc.body))
			if err == nil || cfg != nil {
				t.Fatal("invalid keyring was accepted")
			}
			for _, marker := range []string{key, other, "private-root-marker", "private-other-marker", "private-invalid-marker", "private-field-marker", "private-shape-marker", "missing-root"} {
				if strings.Contains(err.Error(), marker) {
					t.Fatal("bootstrap error leaked configuration material or root identity")
				}
			}
		})
	}
}

func TestKeyringIDsAndRootEncodingAreExact(t *testing.T) {
	key := rootFixture(11)
	for _, id := range []string{"", " private-root-marker", "private-root-marker ", "private.root.marker", "root/marker", "root:marker", "根", strings.Repeat("a", 65)} {
		t.Run("invalid ID "+id, func(t *testing.T) {
			t.Setenv("ROUTEX_TEST_SLOT_ID", id)
			_, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nencryption_keyring:\n  write_key_id: '${ROUTEX_TEST_SLOT_ID}'\n  keys:\n    - {id: '${ROUTEX_TEST_SLOT_ID}', key: '"+key+"'}\n"))
			if err == nil {
				t.Fatal("invalid exact slot identity accepted")
			}
		})
	}
	for _, encoded := range []string{key + "\n", " " + key, strings.TrimSuffix(key, "="), key[:len(key)-2] + "t="} {
		t.Run("noncanonical base64", func(t *testing.T) {
			t.Setenv("ROUTEX_TEST_SLOT_KEY", encoded)
			_, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nencryption_keyring:\n  write_key_id: root\n  keys:\n    - {id: root, key: '${ROUTEX_TEST_SLOT_KEY}'}\n"))
			if err == nil || strings.Contains(err.Error(), encoded) {
				t.Fatal("noncanonical root encoding accepted or leaked")
			}
		})
	}
	id := strings.Repeat("A", 64)
	cfg, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nencryption_keyring:\n  write_key_id: '"+id+"'\n  keys:\n    - {id: '"+id+"', key: '"+key+"'}\n"))
	if err != nil || cfg.EncryptionKeyring.WriteKeyID != id {
		t.Fatal("maximum-length exact ID rejected", err)
	}
	cfg, err = Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nencryption_keyring:\n  legacy_key_id: Old\n  write_key_id: old\n  keys:\n    - {id: Old, key: '"+key+"'}\n    - {id: old, key: '"+rootFixture(22)+"'}\n"))
	if err != nil || cfg.EncryptionKeyring.Keys[0].ID != "Old" || cfg.EncryptionKeyring.Keys[1].ID != "old" {
		t.Fatal("case-distinct exact IDs were folded", err)
	}
}

func TestKeyringEnvironmentExpansionKeepsValuesAsData(t *testing.T) {
	key, next := rootFixture(11), rootFixture(22)
	t.Setenv("ROUTEX_TEST_LEGACY_ID", "Old")
	t.Setenv("ROUTEX_TEST_WRITE_ID", "Next")
	t.Setenv("ROUTEX_TEST_LEGACY_KEY", key)
	t.Setenv("ROUTEX_TEST_NEXT_KEY", next)
	body := "addr: localhost:9000\ndsn: test\nencryption_key: '${ROUTEX_TEST_LEGACY_KEY}'\nencryption_keyring:\n  legacy_key_id: '${ROUTEX_TEST_LEGACY_ID}'\n  write_key_id: '${ROUTEX_TEST_WRITE_ID}'\n  keys:\n    - {id: '${ROUTEX_TEST_LEGACY_ID}', key: '${ROUTEX_TEST_LEGACY_KEY}'}\n    - {id: '${ROUTEX_TEST_WRITE_ID}', key: '${ROUTEX_TEST_NEXT_KEY}'}\n"
	cfg, err := Load(writeConfig(t, body))
	if err != nil || cfg.EncryptionKeyring.LegacyKeyID != "Old" || cfg.EncryptionKeyring.WriteKeyID != "Next" || cfg.EncryptionKeyring.Keys[0].Key != key || cfg.EncryptionKeyring.Keys[1].Key != next {
		t.Fatal("keyring scalar expansion did not preserve exact provisioned values", err)
	}
	for _, injection := range []string{next + "\naddr: private-address-marker", next + "\nkeys:\n  - id: private-root-marker", `${ROUTEX_TEST_LEGACY_KEY}`} {
		t.Setenv("ROUTEX_TEST_NEXT_KEY", injection)
		_, err := Load(writeConfig(t, body))
		if err == nil || !strings.Contains(err.Error(), "keys[1].key") || strings.Contains(err.Error(), injection) || strings.Contains(err.Error(), "private-") {
			t.Fatal("environment root value became YAML syntax, expanded twice or leaked")
		}
	}
	t.Setenv("ROUTEX_TEST_NEXT_KEY", next)
	t.Setenv("ROUTEX_TEST_WRITE_ID", "Next\nlegacy_key_id: private-root-marker")
	_, err = Load(writeConfig(t, body))
	if err == nil || !strings.Contains(err.Error(), "write_key_id") || strings.Contains(err.Error(), "private-") {
		t.Fatal("environment ID became YAML syntax or leaked")
	}
}

func TestKeyringSlotBoundIncludesAddedLegacyRoot(t *testing.T) {
	var slots strings.Builder
	for i := range 64 {
		fmt.Fprintf(&slots, "    - {id: root_%d, key: '%s'}\n", i, rootFixture(byte(i)))
	}
	base := "addr: localhost:9000\ndsn: test\nencryption_keyring:\n  write_key_id: root_0\n  keys:\n"
	cfg, err := Load(writeConfig(t, base+slots.String()))
	if err != nil || len(cfg.EncryptionKeyring.Keys) != 64 {
		t.Fatal("valid 64-slot boundary rejected", err)
	}
	if _, err := Load(writeConfig(t, base+slots.String()+"    - {id: root_64, key: '"+rootFixture(64)+"'}\n")); err == nil {
		t.Fatal("more than 64 configured slots accepted")
	}
	withLegacy := "addr: localhost:9000\ndsn: test\nencryption_key: '" + rootFixture(64) + "'\nencryption_keyring:\n  legacy_key_id: extra_legacy\n  write_key_id: root_0\n  keys:\n"
	if _, err := Load(writeConfig(t, withLegacy+slots.String())); err == nil {
		t.Fatal("appended legacy slot exceeded ring bound")
	}
}
