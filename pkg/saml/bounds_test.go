package saml

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
)

func TestSAMLXMLAndEncodingBounds(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	signed := f.sign(t, f.document(t), "Response")
	plain, e := base64.StdEncoding.DecodeString(signed)
	if e != nil {
		t.Fatal("fixture")
	}
	cases := []struct {
		name string
		raw  []byte
	}{
		{"doctype", append([]byte(`<!DOCTYPE x [<!ENTITY attack "private">]>`), plain...)},
		{"directive", append([]byte(`<!anything>`), plain...)},
		{"processing_instruction", append([]byte(`<?target private?>`), plain...)},
		{"comment", append([]byte(`<!-- ignored ambiguity -->`), plain...)},
		{"second_root", append(append([]byte(nil), plain...), plain...)},
		{"invalid_utf8", append([]byte{0xff}, plain...)},
		{"oversized_response", []byte(strings.Repeat("x", MaxResponseBytes+1))},
		{"deep_tree", []byte(strings.Repeat(`<saml:Subject xmlns:saml="`+assertionNS+`">`, 33) + strings.Repeat(`</saml:Subject>`, 33))},
		{"too_many_elements", []byte(`<saml:Subject xmlns:saml="` + assertionNS + `">` + strings.Repeat(`<saml:NameID/>`, 4096) + `</saml:Subject>`)},
		{"oversized_text", []byte(`<saml:NameID xmlns:saml="` + assertionNS + `">` + strings.Repeat("x", 65537) + `</saml:NameID>`)},
		{"duplicate_attributes", []byte(strings.Replace(string(plain), `Version="2.0"`, `Version="2.0" Version="2.0"`, 1))},
		{"alternate_id", []byte(strings.Replace(string(plain), `ID="_response"`, `id="_response"`, 1))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := client.Verify(context.Background(), Callback{SAMLResponse: base64.StdEncoding.EncodeToString(tc.raw), RequestID: fixtureRequestID})
			if !errors.Is(e, ErrProtocol) {
				t.Fatal("XML admission bound ignored", e)
			}
		})
	}
	for _, raw := range []string{signed + "\n", signed[:len(signed)-1], " " + signed, base64.RawStdEncoding.EncodeToString(plain)} {
		if raw == signed {
			continue
		}
		if _, e := client.Verify(context.Background(), Callback{SAMLResponse: raw, RequestID: fixtureRequestID}); !errors.Is(e, ErrProtocol) {
			t.Fatal("noncanonical response encoding admitted")
		}
	}
}

func TestSAMLConfigurationAdmissionAndNoRawErrors(t *testing.T) {
	f := newFixture(t, false)
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"http_sso", func(c *Config) { c.SSOURL = "http://idp.example/sso" }},
		{"sso_query", func(c *Config) { c.SSOURL += "?scope=private" }},
		{"acs_fragment", func(c *Config) { c.ACSURL += "#private" }},
		{"sso_userinfo", func(c *Config) { c.SSOURL = "https://private@idp.example/sso" }},
		{"relative_entity", func(c *Config) { c.SPEntityID = "relative" }},
		{"missing_issuer", func(c *Config) { c.IDPIssuer = "" }},
		{"missing_cert", func(c *Config) { c.SigningCertificateDER = nil }},
		{"certificate_bound", func(c *Config) { c.SigningCertificateDER = make([]byte, MaxCertificateBytes+1) }},
		{"bad_cert", func(c *Config) { c.SigningCertificateDER = []byte("private-certificate-invalid") }},
		{"concatenated_cert", func(c *Config) { c.SigningCertificateDER = append(append([]byte(nil), f.der...), f.der...) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := f.config
			tc.mutate(&c)
			_, e := New(c)
			if !errors.Is(e, ErrConfig) || e.Error() != ErrConfig.Error() {
				t.Fatal("invalid config admitted or raw error exposed", e)
			}
		})
	}
	var zero Client
	if _, e := zero.AuthorizationURL(context.Background(), Authorization{RequestID: fixtureRequestID, RelayState: fixtureRelayState}); !errors.Is(e, ErrConfig) {
		t.Fatal("zero client redirect admitted")
	}
	if _, e := zero.Verify(context.Background(), Callback{RequestID: fixtureRequestID}); !errors.Is(e, ErrConfig) {
		t.Fatal("zero client response admitted")
	}
}

func TestSAMLStrictTimeAudienceAndExactPersistentSubject(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	d := f.document(t)
	exact := " Case-Sensitive-Subject "
	mustElement(t, d, "./Response/Assertion/Subject/NameID").SetText(exact)
	conditions := mustElement(t, d, "./Response/Assertion/Conditions")
	conditions.AddChild(conditions.ChildElements()[0].Copy())
	// A valid offset encodes the same signed instant; only identity bytes, not
	// timestamps, require raw spelling preservation.
	zone := time.FixedZone("offset", 5*3600+45*60)
	conditions.CreateAttr("NotBefore", f.now.Add(-30*time.Second).In(zone).Format(time.RFC3339Nano))
	conditions.CreateAttr("NotOnOrAfter", f.now.Add(90*time.Second).In(zone).Format(time.RFC3339Nano))
	data := mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData")
	data.CreateAttr("NotOnOrAfter", f.now.Add(time.Minute).Format(time.RFC3339Nano))
	data.CreateAttr("NotBefore", f.now.Add(-time.Second).Format(time.RFC3339Nano))
	identity, e := client.Verify(context.Background(), Callback{SAMLResponse: f.sign(t, d, "Response"), RequestID: fixtureRequestID})
	if e != nil || identity.Subject != exact || !identity.ExpiresAt.Equal(f.now.Add(time.Minute)) {
		t.Fatal("exact subject, AND audiences or expiry minimum", e)
	}
	// These boundaries do not use sleeps or mutable upstream clocks.
	for _, mutate := range []func(*etree.Document){
		func(d *etree.Document) {
			mustElement(t, d, "./Response/Assertion").CreateAttr("IssueInstant", f.now.Add(time.Minute).Format(time.RFC3339Nano))
		},
		func(d *etree.Document) {
			mustElement(t, d, "./Response/Assertion").CreateAttr("IssueInstant", f.now.Add(-2*time.Minute).Format(time.RFC3339Nano))
		},
		func(d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").CreateAttr("NotOnOrAfter", f.now.Add(-time.Second).Format(time.RFC3339Nano))
		},
	} {
		doc := f.document(t)
		mutate(doc)
		if _, e := client.Verify(context.Background(), Callback{SAMLResponse: f.sign(t, doc, "Response"), RequestID: fixtureRequestID}); !errors.Is(e, ErrProtocol) {
			t.Fatal("strict time boundary ignored")
		}
	}
}
