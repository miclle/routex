package saml

import (
	"compress/flate"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
)

func TestSAMLRealSignedIdentityAndSignatureParents(t *testing.T) {
	for _, parent := range []string{"Response", "Assertion"} {
		t.Run(parent, func(t *testing.T) {
			f := newFixture(t, parent == "Response")
			client, e := New(f.config)
			if e != nil {
				t.Fatal("client config")
			}
			doc := f.document(t)
			sessionEnd := f.now.Add(time.Minute)
			mustElement(t, doc, "./Response/Assertion/AuthnStatement").CreateAttr("SessionNotOnOrAfter", sessionEnd.Format(time.RFC3339Nano))
			identity, e := client.Verify(context.Background(), Callback{SAMLResponse: f.sign(t, doc, parent), RequestID: fixtureRequestID})
			if e != nil || identity.Issuer != f.config.IDPIssuer || identity.Subject != fixtureSubject || identity.RequestID != fixtureRequestID || identity.AssertionID != "_assertion" || !identity.ExpiresAt.Equal(sessionEnd) {
				t.Fatal("signed identity or minimum expiry rejected", e)
			}
			// Protocol proof alone is deliberately replayable. The application must
			// atomically consume request/assertion/browser correlation before login.
		})
	}
}

func TestSAMLRedirectExactRequestAndOpaqueRelayState(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	raw, e := client.AuthorizationURL(context.Background(), Authorization{RequestID: fixtureRequestID, RelayState: fixtureRelayState})
	if e != nil {
		t.Fatal("authorization")
	}
	target, e := url.Parse(raw)
	if e != nil || target.Scheme != "https" || target.Host != "idp.example" || target.Path != "/sso" || len(target.Query()) != 2 || target.Query().Get("RelayState") != fixtureRelayState {
		t.Fatal("redirect shape")
	}
	compressed, e := base64.StdEncoding.DecodeString(target.Query().Get("SAMLRequest"))
	if e != nil {
		t.Fatal("redirect encoding")
	}
	reader := flate.NewReader(strings.NewReader(string(compressed)))
	xml, e := io.ReadAll(io.LimitReader(reader, 8193))
	closeErr := reader.Close()
	if e != nil || closeErr != nil || len(xml) > 8192 {
		t.Fatal("request compression bound")
	}
	for _, expected := range []string{fixtureRequestID, client.acs.String(), client.entity, persistentNameID, `AllowCreate="false"`, coresamlPostBinding()} {
		if !strings.Contains(string(xml), expected) {
			t.Fatal("request contract missing")
		}
	}
	for _, relay := range []string{fixtureRelayState + "&next=https://evil.example", strings.Repeat("a", 81), "https://evil.example"} {
		if _, e = client.AuthorizationURL(context.Background(), Authorization{RequestID: fixtureRequestID, RelayState: relay}); !errors.Is(e, ErrState) {
			t.Fatal("unsafe RelayState admitted")
		}
	}
}
func coresamlPostBinding() string { return "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" }

func TestSAMLSignedSemanticDenials(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	cases := []struct {
		name   string
		mutate func(*testing.T, *etree.Document)
	}{
		{"outer_issuer_missing", func(t *testing.T, d *etree.Document) { d.Root().RemoveChild(mustElement(t, d, "./Response/Issuer")) }},
		{"outer_issuer_wrong", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Issuer").SetText("https://other.example")
		}},
		{"destination_missing", func(_ *testing.T, d *etree.Document) { d.Root().RemoveAttr("Destination") }},
		{"destination_wrong", func(_ *testing.T, d *etree.Document) { d.Root().CreateAttr("Destination", "https://evil.example/acs") }},
		{"response_request_wrong", func(_ *testing.T, d *etree.Document) { d.Root().CreateAttr("InResponseTo", "_other") }},
		{"bad_status", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Status/StatusCode").CreateAttr("Value", "urn:oasis:names:tc:SAML:2.0:status:Responder")
		}},
		{"assertion_issuer_wrong", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Issuer").SetText("https://other.example")
		}},
		{"no_audiences", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Conditions")
			n.RemoveChild(n.ChildElements()[0])
		}},
		{"and_not_or_audiences", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Conditions")
			r := n.ChildElements()[0].Copy()
			r.ChildElements()[0].SetText("https://other.example/entity")
			n.AddChild(r)
		}},
		{"multiple_audiences_outside_profile", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Conditions/AudienceRestriction")
			n.AddChild(n.ChildElements()[0].Copy())
		}},
		{"unknown_condition", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Conditions").CreateElement("saml:OneTimeUse")
		}},
		{"missing_not_before", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Conditions").RemoveAttr("NotBefore")
		}},
		{"future_not_before", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Conditions").CreateAttr("NotBefore", f.now.Add(time.Minute).Format(time.RFC3339Nano))
		}},
		{"expired_conditions", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Conditions").CreateAttr("NotOnOrAfter", f.now.Add(-time.Second).Format(time.RFC3339Nano))
		}},
		{"missing_confirmation", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Subject")
			n.RemoveChild(mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation"))
		}},
		{"wrong_bearer_method", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation").CreateAttr("Method", "urn:unsupported")
		}},
		{"wrong_recipient", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").CreateAttr("Recipient", "https://evil.example/acs")
		}},
		{"wrong_confirmation_request", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").CreateAttr("InResponseTo", "_other")
		}},
		{"missing_confirmation_expiry", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").RemoveAttr("NotOnOrAfter")
		}},
		{"future_confirmation", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").CreateAttr("NotBefore", f.now.Add(time.Minute).Format(time.RFC3339Nano))
		}},
		{"empty_name", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/NameID").SetText("")
		}},
		{"transient_name", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/NameID").CreateAttr("Format", "urn:oasis:names:tc:SAML:2.0:nameid-format:transient")
		}},
		{"wrong_qualifier", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/NameID").CreateAttr("NameQualifier", "other")
		}},
		{"provided_id_alias", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/NameID").CreateAttr("SPProvidedID", "other")
		}},
		{"multiple_confirmations", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Subject")
			n.AddChild(mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation").Copy())
		}},
		{"multiple_name_ids", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Subject")
			n.AddChild(mustElement(t, d, "./Response/Assertion/Subject/NameID").Copy())
		}},
		{"multiple_authn_statements", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion")
			n.AddChild(mustElement(t, d, "./Response/Assertion/AuthnStatement").Copy())
		}},
		{"confirmation_extension", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").CreateElement("saml:NameID").SetText("unconsumed")
		}},
		{"nested_status_code", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Status/StatusCode").CreateElement("samlp:StatusCode").CreateAttr("Value", successStatus)
		}},
		{"attribute_only_not_authentication", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion")
			n.RemoveChild(mustElement(t, d, "./Response/Assertion/AuthnStatement"))
		}},
		{"future_authentication", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/AuthnStatement").CreateAttr("AuthnInstant", f.now.Add(time.Minute).Format(time.RFC3339Nano))
		}},
		{"expired_session", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/AuthnStatement").CreateAttr("SessionNotOnOrAfter", f.now.Add(-time.Second).Format(time.RFC3339Nano))
		}},
		{"missing_context", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/AuthnStatement/AuthnContext").RemoveChild(mustElement(t, d, "./Response/Assertion/AuthnStatement/AuthnContext/AuthnContextClassRef"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := f.document(t)
			tc.mutate(t, d)
			_, e := client.Verify(context.Background(), Callback{SAMLResponse: f.sign(t, d, "Response"), RequestID: fixtureRequestID})
			if !errors.Is(e, ErrProtocol) || e.Error() != ErrProtocol.Error() {
				t.Fatal("signed invalid semantics admitted or error leaked", e)
			}
		})
	}
}

func TestSAMLSignatureTamperAndAmbiguity(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	signed := f.sign(t, f.document(t), "Assertion")
	cases := []struct {
		name   string
		mutate func(*testing.T, *etree.Document)
	}{
		{"unsigned", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion")
			signature := mustElement(t, d, "./Response/Assertion/Signature")
			if n.RemoveChild(signature) != signature || len(n.FindElements("./Signature")) != 0 {
				t.Fatal("unsigned fixture retains signature")
			}
		}},
		{"multiple_signatures", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion")
			n.AddChild(mustElement(t, d, "./Response/Assertion/Signature").Copy())
		}},
		{"encrypted_alternative", func(t *testing.T, d *etree.Document) { d.Root().CreateElement("saml:EncryptedAssertion") }},
		{"signed_subject_tamper", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/NameID").SetText("attacker")
		}},
		{"duplicate_ids", func(t *testing.T, d *etree.Document) { d.Root().CreateAttr("ID", "_assertion") }},
		{"multiple_assertions", func(t *testing.T, d *etree.Document) {
			d.Root().AddChild(mustElement(t, d, "./Response/Assertion").Copy())
		}},
		{"wrapped_assertion", func(t *testing.T, d *etree.Document) {
			a := mustElement(t, d, "./Response/Assertion")
			d.Root().RemoveChild(a)
			d.Root().CreateElement("samlp:Extensions").AddChild(a)
		}},
		{"reference_no_hash", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/Reference").CreateAttr("URI", "_assertion")
		}},
		{"empty_reference", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/Reference").CreateAttr("URI", "")
		}},
		{"external_reference", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/Reference").CreateAttr("URI", "https://evil.example/")
		}},
		{"multiple_references", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Signature/SignedInfo")
			n.AddChild(mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/Reference").Copy())
		}},
		{"sha1_method", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/SignatureMethod").CreateAttr("Algorithm", "http://www.w3.org/2000/09/xmldsig#rsa-sha1")
		}},
		{"sha1_digest", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/Reference/DigestMethod").CreateAttr("Algorithm", "http://www.w3.org/2000/09/xmldsig#sha1")
		}},
		{"xpath_exclusion", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/Reference/Transforms/Transform").CreateAttr("Algorithm", "http://www.w3.org/TR/1999/REC-xpath-19991116")
		}},
		{"canonical_prefix_parameter", func(t *testing.T, d *etree.Document) {
			n := mustElement(t, d, "./Response/Assertion/Signature/SignedInfo/CanonicalizationMethod")
			n.CreateElement("ec:InclusiveNamespaces").CreateAttr("xmlns:ec", exclusiveC14N)
		}},
		{"prefix_rebinding", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject").CreateAttr("xmlns:saml", "urn:other")
		}},
		{"namespaced_name_attribute", func(t *testing.T, d *etree.Document) {
			mustElement(t, d, "./Response/Assertion/Subject/NameID").CreateAttr("saml:Format", persistentNameID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := decodeFixture(t, signed)
			tc.mutate(t, doc)
			_, e := client.Verify(context.Background(), Callback{SAMLResponse: encoded(t, doc), RequestID: fixtureRequestID})
			if !errors.Is(e, ErrProtocol) {
				t.Fatal("signature ambiguity admitted", e)
			}
		})
	}
	// Wrong private key with trusted KeyInfo restored must fail real signature
	// validation, rather than only the certificate equality admission guard.
	wrong := newFixture(t, false)
	doc := wrong.document(t)
	raw := wrong.sign(t, doc, "Assertion")
	doc = decodeFixture(t, raw)
	mustElement(t, doc, "./Response/Assertion/Signature/KeyInfo/X509Data/X509Certificate").SetText(client.certificateBase64)
	if _, e = client.Verify(context.Background(), Callback{SAMLResponse: encoded(t, doc), RequestID: fixtureRequestID}); !errors.Is(e, ErrProtocol) {
		t.Fatal("wrong key admitted")
	}
}

func TestSAMLImmutableConfigParallelCancellationAndPrivacy(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	signed := f.sign(t, f.document(t), "Assertion")
	for i := range f.config.SigningCertificateDER {
		f.config.SigningCertificateDER[i] ^= 0xff
	}
	f.config.IDPIssuer = "https://attacker.example"
	f.config.SSOURL = "https://attacker.example/sso"
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			identity, e := client.Verify(context.Background(), Callback{SAMLResponse: signed, RequestID: fixtureRequestID})
			if e != nil || identity.Subject != fixtureSubject {
				t.Error("immutable parallel verification", e)
			}
		}()
	}
	group.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = client.Verify(ctx, Callback{SAMLResponse: signed, RequestID: fixtureRequestID}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("cancellation ignored")
	}
	if _, e = client.AuthorizationURL(ctx, Authorization{RequestID: fixtureRequestID, RelayState: fixtureRelayState}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("canceled authorization")
	}
	for _, input := range []Callback{{SAMLResponse: "raw-private-XML", RequestID: fixtureRequestID}, {SAMLResponse: signed, RequestID: "bad-private-request"}} {
		_, e = client.Verify(context.Background(), input)
		if e == nil || strings.Contains(e.Error(), "private") || strings.Contains(e.Error(), fixtureSubject) || strings.Contains(e.Error(), signed) {
			t.Fatal("private error leaked")
		}
	}
}

func TestSAMLRealWeakSignatureAndCertificateDenials(t *testing.T) {
	f := newFixture(t, true)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	// This is a genuinely signed SHA-1 fixture, not an algorithm string changed
	// after signing. Maintained validation must never bypass profile admission.
	raw := f.signMethod(t, f.document(t), "Response", "http://www.w3.org/2000/09/xmldsig#rsa-sha1")
	if _, e := client.Verify(context.Background(), Callback{SAMLResponse: raw, RequestID: fixtureRequestID}); !errors.Is(e, ErrProtocol) {
		t.Fatal("real SHA-1 signature admitted")
	}
	for _, tc := range []struct {
		name   string
		change func(*x509.Certificate)
	}{
		{"expired", func(c *x509.Certificate) { c.NotAfter = f.now.Add(-time.Second) }},
		{"future", func(c *x509.Certificate) { c.NotBefore = f.now.Add(time.Hour) }},
		{"wrong_usage", func(c *x509.Certificate) { c.KeyUsage = x509.KeyUsageKeyEncipherment }},
		{"weak_signature", func(c *x509.Certificate) { c.SignatureAlgorithm = x509.SHA1WithRSA }},
		{"small_rsa", func(c *x509.Certificate) {
			key := *c.PublicKey.(*rsa.PublicKey)
			key.N = big.NewInt(65537)
			c.PublicKey = &key
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cert := *client.certificate
			tc.change(&cert)
			if certificateAllowed(&cert, time.Now()) {
				t.Fatal("unsafe pinned certificate admitted")
			}
		})
	}
}

func TestSAMLScopedDefaultNamespacesThroughBothValidators(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	for _, parent := range []string{"Response", "Assertion"} {
		for _, defaultSignature := range []bool{false, true} {
			name := parent + "/named_signature"
			var prefix *string
			if defaultSignature {
				name = parent + "/default_signature"
				empty := ""
				prefix = &empty
			}
			t.Run(name, func(t *testing.T) {
				doc := f.defaultDocument(t)
				raw := f.signPrefix(t, doc, parent, "", prefix)
				identity, e := client.Verify(context.Background(), Callback{SAMLResponse: raw, RequestID: fixtureRequestID})
				if e != nil || identity.Issuer != f.config.IDPIssuer || identity.Subject != fixtureSubject || identity.RequestID != fixtureRequestID || identity.AssertionID != "_assertion" || !identity.ExpiresAt.Equal(f.now.Add(2*time.Minute)) {
					t.Fatal("scoped default namespace proof rejected", e)
				}
				// These admissions go through Verify, including both maintained validators
				// and canonical verified-node consumption, rather than only parseXML.
				for _, kind := range []string{"foreign_default", "named_rebinding"} {
					t.Run(kind, func(t *testing.T) {
						var badRaw string
						if kind == "foreign_default" {
							bad := f.defaultDocument(t)
							mustElement(t, bad, "./Response/Assertion").CreateAttr("xmlns", "urn:foreign")
							badRaw = f.signPrefix(t, bad, parent, "", prefix)
						} else {
							// Exclusive canonicalization drops unused declarations while signing.
							// Add this unused rebinding to the genuinely signed positive afterward.
							bad := decodeFixture(t, raw)
							bad.Root().CreateAttr("xmlns:alias", assertionNS)
							mustElement(t, bad, "./Response/Assertion/Subject").CreateAttr("xmlns:alias", protocolNS)
							badRaw = encoded(t, bad)
							serialized := decodeFixture(t, badRaw)
							if serialized.Root().SelectAttrValue("xmlns:alias", "") != assertionNS || mustElement(t, serialized, "./Response/Assertion/Subject").SelectAttrValue("xmlns:alias", "") != protocolNS {
								t.Fatal("named rebinding fixture lost serialized declarations")
							}
						}
						if _, e := client.Verify(context.Background(), Callback{SAMLResponse: badRaw, RequestID: fixtureRequestID}); !errors.Is(e, ErrProtocol) {
							t.Fatal("foreign namespace or named rebinding admitted", e)
						}
					})
				}
			})
		}
	}
}

func TestSAMLShortAssertionWithLongIdPSession(t *testing.T) {
	f := newFixture(t, false)
	client, e := New(f.config)
	if e != nil {
		t.Fatal("client")
	}
	for _, parent := range []string{"Response", "Assertion"} {
		t.Run(parent, func(t *testing.T) {
			doc := f.document(t)
			mustElement(t, doc, "./Response/Assertion/AuthnStatement").CreateAttr("SessionNotOnOrAfter", f.now.Add(8*time.Hour).Format(time.RFC3339Nano))
			identity, e := client.Verify(context.Background(), Callback{SAMLResponse: f.sign(t, doc, parent), RequestID: fixtureRequestID})
			if e != nil || identity.Subject != fixtureSubject || !identity.ExpiresAt.Equal(f.now.Add(2*time.Minute)) {
				t.Fatal("long IdP session incorrectly extended or rejected short proof", e)
			}
			expired := f.document(t)
			mustElement(t, expired, "./Response/Assertion/AuthnStatement").CreateAttr("SessionNotOnOrAfter", f.now.Add(-time.Second).Format(time.RFC3339Nano))
			if _, e := client.Verify(context.Background(), Callback{SAMLResponse: f.sign(t, expired, parent), RequestID: fixtureRequestID}); !errors.Is(e, ErrProtocol) {
				t.Fatal("expired IdP session admitted", e)
			}
		})
	}
}
