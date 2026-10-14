package deliverycontroller

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	fixtureSlot      = "kds_01aaaaaaaaaaaaaaaaaaaaaaaa"
	fixtureKey       = "key_01bbbbbbbbbbbbbbbbbbbbbbbb"
	fixtureProfile   = "vap_01cccccccccccccccccccccccc"
	fixturePrincipal = "vpb_01dddddddddddddddddddddddd"
	fixtureDigest    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func fixtureConfiguration() configuration {
	contents := "{{ with secret " + strconv.Quote("secret/data/routex/"+fixtureProfile+"/routex-api-key-"+fixtureKey+"?version=1") + " }}{{ $key := index .Data.data \"api_key\" }}{{ if not $key }}{{ sprig_fail \"RouteX API Key field is missing or empty\" }}{{ end }}{{ $key }}{{ end }}"
	text := "vault {\n  address = \"https://vault.example.test\"\n}\n\nauto_auth {\n  method \"jwt\" {\n    mount_path = \"auth/jwt\"\n    config = {\n      role = \"routex-api-key-" + fixturePrincipal + "\"\n      path = \"" + jwtFile + "\"\n      remove_jwt_after_reading = false\n    }\n  }\n}\n\ntemplate_config {\n  exit_on_retry_failure = true\n}\n\ntemplate {\n  contents = " + strconv.Quote(contents) + "\n  destination = \"" + keyDirectory + "/" + fixtureSlot + "\"\n  perms = \"0600\"\n  backup = false\n  error_on_missing_key = true\n}\n"
	return configuration{SchemaVersion: 1, SlotID: fixtureSlot, DescriptorRevision: "1", KeyID: fixtureKey, ConfigurationDigest: fixtureDigest, AgentConfiguration: text, JWTFile: jwtFile, KeyFile: keyDirectory + "/" + fixtureSlot}
}

func TestReportTimestampMatchesServerPrecision(t *testing.T) {
	for _, text := range []string{"2026-10-14T00:00:00Z", "2026-10-14T00:00:00.123456Z"} {
		value, err := time.Parse(time.RFC3339Nano, text)
		if err != nil || !recordedAtValid(value) {
			t.Fatal("server UTC microsecond timestamp rejected")
		}
	}
	for _, text := range []string{"2026-10-14T00:00:00.123456789Z", "2026-10-14T00:00:00+08:00"} {
		value, err := time.Parse(time.RFC3339Nano, text)
		if err != nil || recordedAtValid(value) {
			t.Fatal("non-server timestamp precision or offset accepted")
		}
	}
	if recordedAtValid(time.Time{}) {
		t.Fatal("missing server timestamp accepted")
	}
}

func fixturePins() Config {
	return Config{RouteXURL: "https://routex.example.test", SlotID: fixtureSlot, DescriptorRevision: "1", ConfigurationDigest: fixtureDigest, BearerFile: "/secure/controller.bearer", StateDirectory: "/secure/controller-state", AgentExecutable: "/secure/vault", AgentSHA256: fixtureDigest, TimeoutSeconds: 30}
}

func TestConfigurationClosedAgentStructure(t *testing.T) {
	cfg := fixturePins()
	valid := fixtureConfiguration()
	if err := valid.validate(cfg); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*configuration){
		"other slot":     func(c *configuration) { c.SlotID = "kds_01eeeeeeeeeeeeeeeeeeeeeeee" },
		"other revision": func(c *configuration) { c.DescriptorRevision = "2" },
		"other digest":   func(c *configuration) { c.ConfigurationDigest = strings.Repeat("a", 64) },
		"unknown schema": func(c *configuration) { c.SchemaVersion = 2 },
		"other JWT":      func(c *configuration) { c.JWTFile = "/tmp/workload.jwt" },
		"other output":   func(c *configuration) { c.KeyFile = "/tmp/key" },
		"extra hook":     func(c *configuration) { c.AgentConfiguration += "exec { command = [\"sh\"] }\n" },
		"token sink": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "    config = {", "    sink \"file\" {}\n    config = {", 1)
		},
		"backup": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "backup = false", "backup = true", 1)
		},
		"remove JWT": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "remove_jwt_after_reading = false", "remove_jwt_after_reading = true", 1)
		},
		"permissions": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "perms = \"0600\"", "perms = \"0644\"", 1)
		},
		"alternate version": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "?version=1", "?version=2", 1)
		},
		"unversioned": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "?version=1", "", 1)
		},
		"missing field guard": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "sprig_fail", "printf", 1)
		},
		"arbitrary function": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "{{ $key }}", "{{ env `SECRET` }}", 1)
		},
		"endpoint interpolation": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "https://vault.example.test", "https://${VAULT_HOST}", 1)
		},
		"insecure remote": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "https://vault.example.test", "http://vault.example.test", 1)
		},
		"opaque endpoint": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "https://vault.example.test", "https://user:password@vault.example.test", 1)
		},
		"path traversal": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "secret/data/routex/", "secret/data/../", 1)
		},
		"noncanonical escape": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "https://", "https:\\u002f/", 1)
		},
		"other Key": func(c *configuration) { c.KeyID = "pky_01eeeeeeeeeeeeeeeeeeeeeeee" },
		"other role prefix": func(c *configuration) {
			c.AgentConfiguration = strings.Replace(c.AgentConfiguration, "routex-api-key-vpb_", "other-vpb_", 1)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			c := valid
			mutate(&c)
			if err := c.validate(cfg); err == nil {
				t.Fatal("unreviewed Agent configuration accepted")
			}
		})
	}
	project := valid
	project.KeyID = strings.Replace(fixtureKey, "key_", "pky_", 1)
	project.AgentConfiguration = strings.ReplaceAll(project.AgentConfiguration, fixtureKey, project.KeyID)
	project.AgentConfiguration = strings.Replace(project.AgentConfiguration, "}\n\nauto_auth", "  namespace = \"team/production\"\n}\n\nauto_auth", 1)
	if err := project.validate(cfg); err != nil {
		t.Fatalf("Project namespace configuration rejected: %v", err)
	}
}

func TestClosedJSONEncoding(t *testing.T) {
	valid, err := json.Marshal(fixtureConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"duplicate":        []byte(`{"schema_version":1,"schema_version":2}`),
		"nested duplicate": []byte(`{"a":{"x":1,"x":2}}`),
		"high surrogate":   []byte(`{"slot_id":"\ud800"}`),
		"low surrogate":    []byte(`{"slot_id":"\udc00"}`),
		"UTF8":             append([]byte(`{"slot_id":"`), 0xff, '"', '}'),
		"unknown field":    []byte(`{"unexpected":"value"}`),
		"trailing":         append(append([]byte{}, valid...), []byte(`{}`)...),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var c configuration
			if decodeJSON(raw, &c) == nil {
				t.Fatal("invalid or ambiguous JSON accepted")
			}
		})
	}
	var c configuration
	if err := decodeJSON(valid, &c); err != nil {
		t.Fatal(err)
	}
	var unicode struct {
		Value string `json:"value"`
	}
	if err := decodeJSON([]byte(`{"value":"\ud83d\ude80"}`), &unicode); err != nil || unicode.Value != "🚀" {
		t.Fatal("valid surrogate pair rejected")
	}
	if err := decodeJSON([]byte(`{"value":"\\ud800"}`), &unicode); err != nil || unicode.Value != `\ud800` {
		t.Fatal("literal escaped backslash rejected")
	}
}

func TestControllerBootstrapTLSAndPins(t *testing.T) {
	valid := fixturePins()
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"http://routex.example.test", "http://localhost:9000", "http://192.0.2.1:9000", "https://routex.example.test/path", "https://user:password@routex.example.test", "https://routex.example.test?query=secret", "https://routex.example.test/#fragment"} {
		cfg := valid
		cfg.RouteXURL, cfg.AllowLoopbackHTTP = target, true
		if cfg.validate() == nil {
			t.Fatalf("unsafe origin accepted: %s", target)
		}
	}
	for _, target := range []string{"http://127.0.0.1:9000", "http://[::1]:9000"} {
		cfg := valid
		cfg.RouteXURL, cfg.AllowLoopbackHTTP = target, true
		if cfg.validate() != nil {
			t.Fatalf("explicit numeric-loopback fixture rejected: %s", target)
		}
		cfg.AllowLoopbackHTTP = false
		if cfg.validate() == nil {
			t.Fatal("HTTP accepted without explicit fixture opt-in")
		}
	}
	for _, revision := range []string{"0", "01", "+1", "18446744073709551615", "9223372036854775808"} {
		cfg := valid
		cfg.DescriptorRevision = revision
		if cfg.validate() == nil {
			t.Fatal("noncanonical revision accepted")
		}
	}
}
