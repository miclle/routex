package ldap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"

	directory "github.com/go-ldap/ldap/v3"
)

func testConfig() Config {
	return Config{IdentityAttribute: IdentityAttributeEntryUUID, Endpoint: "ldaps://directory.example:636", BindDN: "cn=service,dc=example", BindPassword: " service password ", BaseDN: "dc=example", UserFilter: "(&(objectClass=person)(uid={username}))", TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("private dial diagnostic")
	}, EndpointPolicy: func(context.Context, *url.URL) error { return nil }}
}

func TestLDAPConfigurationAdmissionAndOwnership(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"missing identity attribute", func(c *Config) { c.IdentityAttribute = "" }},
		{"mutable identity attribute", func(c *Config) { c.IdentityAttribute = "uid" }},
		{"plaintext", func(c *Config) { c.Endpoint = "ldap://directory.example:389" }},
		{"absent port", func(c *Config) { c.Endpoint = "ldaps://directory.example" }},
		{"userinfo", func(c *Config) { c.Endpoint = "ldaps://user@directory.example:636" }},
		{"path", func(c *Config) { c.Endpoint += "/" }},
		{"query", func(c *Config) { c.Endpoint += "?x=1" }},
		{"fragment", func(c *Config) { c.Endpoint += "#x" }},
		{"empty service password", func(c *Config) { c.BindPassword = "" }},
		{"invalid base", func(c *Config) { c.BaseDN = "not-a-dn" }},
		{"empty bind DN", func(c *Config) { c.BindDN = "" }},
		{"no placeholder", func(c *Config) { c.UserFilter = "(uid=literal)" }},
		{"duplicate placeholder", func(c *Config) { c.UserFilter = "(|(uid={username})(cn={username}))" }},
		{"attribute placeholder", func(c *Config) { c.UserFilter = "({username}=value)" }},
		{"deep filter", func(c *Config) {
			c.UserFilter = strings.Repeat("(!", 33) + "(uid={username})" + strings.Repeat(")", 33)
		}},
		{"unverified TLS", func(c *Config) { c.TLSConfig = &tls.Config{InsecureSkipVerify: true} }},
		{"old maximum TLS", func(c *Config) { c.TLSConfig = &tls.Config{MaxVersion: tls.VersionTLS11} }},
		{"no guarded dial", func(c *Config) { c.DialContext = nil }},
		{"no endpoint policy", func(c *Config) { c.EndpointPolicy = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c := testConfig()
			test.change(&c)
			if client, err := New(c); client != nil || !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("admission: %v", err)
			}
		})
	}
	config := testConfig()
	config.TLSConfig.RootCAs = x509.NewCertPool()
	original := config.TLSConfig
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if client.tlsConfig == original || client.tlsConfig.RootCAs == original.RootCAs {
		t.Fatal("TLS trust was not cloned")
	}
	original.InsecureSkipVerify = true
	original.ServerName = "changed.example"
	original.MinVersion = tls.VersionTLS10
	config.BaseDN = "dc=changed"
	if client.tlsConfig.InsecureSkipVerify || client.tlsConfig.ServerName != "directory.example" || client.tlsConfig.MinVersion != tls.VersionTLS12 || client.baseDN != "dc=example" {
		t.Fatal("caller mutation changed admitted configuration")
	}
}

func TestLDAPInvalidInputDoesNotDispatch(t *testing.T) {
	calls := 0
	config := testConfig()
	config.EndpointPolicy = func(context.Context, *url.URL) error { calls++; return nil }
	config.DialContext = func(context.Context, string, string) (net.Conn, error) { calls++; return nil, ErrUnavailable }
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][2]string{{"", "password"}, {"user", ""}, {strings.Repeat("u", 257), "password"}, {"user", strings.Repeat("p", 4097)}, {"bad\x00user", "password"}, {string([]byte{0xff}), "password"}} {
		identity, err := client.Authenticate(context.Background(), input[0], input[1])
		if !errors.Is(err, ErrInvalidInput) || identity.DN != "" {
			t.Fatal("invalid input admitted")
		}
	}
	// Deliberately exercise the public nil-context rejection boundary.
	//nolint:staticcheck // SA1012: this negative test must pass a nil context.
	if _, err := client.Authenticate(nil, "user", "password"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("nil context admitted")
	}
	if calls != 0 {
		t.Fatal("invalid input performed I/O")
	}
}

func TestLDAPFreshPolicyAndGenericFailure(t *testing.T) {
	config := testConfig()
	policies, dials := 0, 0
	config.EndpointPolicy = func(_ context.Context, u *url.URL) error {
		policies++
		u.Host = "mutated.example:636"
		return errors.New("private policy diagnostic")
	}
	config.DialContext = func(context.Context, string, string) (net.Conn, error) { dials++; return nil, ErrUnavailable }
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if identity, err := client.Authenticate(context.Background(), "sensitive-user", "sensitive-password"); identity.DN != "" || err != ErrUnavailable {
			t.Fatal("policy error was not generic")
		}
	}
	if policies != 2 || dials != 0 || client.endpoint.Host != "directory.example:636" {
		t.Fatal("fresh policy boundary changed")
	}
}

func TestLDAPStableSubjectBoundsAndOwnership(t *testing.T) {
	uuid := []byte("12345678-9ABC-4def-8123-123456789abc")
	guid := []byte{0xff, 0, 0x80, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	entry := func(name string, values ...[]byte) *directory.Entry {
		texts := make([]string, len(values))
		for i, value := range values {
			texts[i] = string(value)
		}
		return &directory.Entry{DN: "uid=member,dc=example", Attributes: []*directory.EntryAttribute{{Name: name, Values: texts, ByteValues: values}}}
	}
	for _, test := range []struct {
		name      string
		attribute IdentityAttribute
		value     []byte
	}{{"UUID preserves case", IdentityAttributeEntryUUID, uuid}, {"GUID preserves byte order", IdentityAttributeObjectGUID, guid}} {
		t.Run(test.name, func(t *testing.T) {
			source := append([]byte(nil), test.value...)
			subject, ok := stableSubject(entry(string(test.attribute), source), test.attribute)
			if !ok || string(subject) != string(test.value) {
				t.Fatal("exact opaque subject lost")
			}
			source[0] ^= 0xff
			if string(subject) != string(test.value) {
				t.Fatal("subject aliases directory response")
			}
			subject[1] ^= 0xff
			if source[1] != test.value[1] {
				t.Fatal("returned subject changed directory response")
			}
		})
	}
	for _, test := range []struct {
		name      string
		attribute IdentityAttribute
		entry     *directory.Entry
	}{
		{"missing", IdentityAttributeEntryUUID, &directory.Entry{}},
		{"wrong attribute", IdentityAttributeEntryUUID, entry("uid", uuid)},
		{"wrong mode", IdentityAttributeObjectGUID, entry("entryUUID", uuid)},
		{"duplicate", IdentityAttributeEntryUUID, &directory.Entry{Attributes: []*directory.EntryAttribute{entry("entryUUID", uuid).Attributes[0], entry("entryUUID", uuid).Attributes[0]}}},
		{"multivalue", IdentityAttributeEntryUUID, entry("entryUUID", uuid, uuid)},
		{"empty values", IdentityAttributeEntryUUID, entry("entryUUID")},
		{"nil UUID", IdentityAttributeEntryUUID, entry("entryUUID", []byte("00000000-0000-0000-0000-000000000000"))},
		{"wrong UUID hyphen", IdentityAttributeEntryUUID, entry("entryUUID", []byte("123456789ABC-4def-8123-123456789abc-"))},
		{"invalid UUID hex", IdentityAttributeEntryUUID, entry("entryUUID", []byte("12345678-9ABZ-4def-8123-123456789abc"))},
		{"short UUID", IdentityAttributeEntryUUID, entry("entryUUID", uuid[:35])},
		{"zero GUID", IdentityAttributeObjectGUID, entry("objectGUID", make([]byte, 16))},
		{"short GUID", IdentityAttributeObjectGUID, entry("objectGUID", guid[:15])},
		{"long GUID", IdentityAttributeObjectGUID, entry("objectGUID", append(append([]byte(nil), guid...), 0))},
		{"unknown mode", IdentityAttribute("mail"), entry("mail", uuid)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if subject, ok := stableSubject(test.entry, test.attribute); ok || subject != nil {
				t.Fatal("invalid stable subject admitted")
			}
		})
	}
}

func TestLDAPStableAttributeNameASCIIFoldingOnly(t *testing.T) {
	value := []byte("12345678-9ABC-4def-8123-123456789abc")
	for _, name := range []string{"entryUUID", "ENTRYUUID", "entryuuid"} {
		entry := &directory.Entry{Attributes: []*directory.EntryAttribute{{Name: name, Values: []string{string(value)}, ByteValues: [][]byte{value}}}}
		subject, ok := stableSubject(entry, IdentityAttributeEntryUUID)
		if !ok || string(subject) != string(value) {
			t.Fatal("valid ASCII attribute case changed exact subject")
		}
	}
	for _, name := range []string{"entryUUID;binary", "1.3.6.1.1.16.4", "entryUUI\u212a", "entryU\u0130D", "entryUUId ", "objectGUID"} {
		if attributeNameEqual(name, IdentityAttributeEntryUUID) {
			t.Fatal("attribute alias admitted")
		}
	}
}
