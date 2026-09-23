package config

import (
	"os"
	"path/filepath"
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
