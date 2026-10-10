package handler

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/beevik/etree"
	samlprotocol "github.com/miclle/routex/pkg/saml"
	"github.com/miclle/routex/pkg/secret"
	dsig "github.com/russellhaering/goxmldsig"
)

const samlFixtureRequestID = "_0123456789abcdef0123456789abcdef"
const samlFixtureSubject = "Exact-Persistent-Subject"

type samlHTTPFixture struct {
	config samlprotocol.Config
	signer crypto.Signer
	der    []byte
	now    time.Time
}

func newSAMLHTTPFixture(t *testing.T, rsaKey bool) samlHTTPFixture {
	t.Helper()
	var signer crypto.Signer
	if rsaKey {
		key, e := rsa.GenerateKey(rand.Reader, 2048)
		if e != nil {
			t.Fatal("samlHTTPFixture RSA key")
		}
		signer = key
	} else {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal("samlHTTPFixture EC key")
		}
		signer = key
	}
	now := time.Now().UTC().Truncate(time.Second)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, template, template, signer.Public(), signer)
	if e != nil {
		t.Fatal("samlHTTPFixture public certificate")
	}
	return samlHTTPFixture{config: samlprotocol.Config{IDPIssuer: "https://idp.example/entity", SSOURL: "https://idp.example/sso", SPEntityID: "https://routex.test/saml/entity", ACSURL: "https://routex.test/api/v1/auth/saml/acs", SigningCertificateDER: der}, signer: signer, der: der, now: now}
}
func (f samlHTTPFixture) document(t *testing.T) *etree.Document {
	t.Helper()
	instant := f.now.Add(-time.Second).Format(time.RFC3339Nano)
	before := f.now.Add(-30 * time.Second).Format(time.RFC3339Nano)
	expiry := f.now.Add(2 * time.Minute).Format(time.RFC3339Nano)
	raw := fmt.Sprintf(`<samlp:Response xmlns:samlp="%s" xmlns:saml="%s" ID="_response" Version="2.0" IssueInstant="%s" Destination="%s" InResponseTo="%s"><saml:Issuer>%s</saml:Issuer><samlp:Status><samlp:StatusCode Value="%s"/></samlp:Status><saml:Assertion xmlns:saml="%s" ID="_assertion" Version="2.0" IssueInstant="%s"><saml:Issuer>%s</saml:Issuer><saml:Subject><saml:NameID Format="%s" NameQualifier="%s" SPNameQualifier="%s">%s</saml:NameID><saml:SubjectConfirmation Method="%s"><saml:SubjectConfirmationData Recipient="%s" InResponseTo="%s" NotOnOrAfter="%s"/></saml:SubjectConfirmation></saml:Subject><saml:Conditions NotBefore="%s" NotOnOrAfter="%s"><saml:AudienceRestriction><saml:Audience>%s</saml:Audience></saml:AudienceRestriction></saml:Conditions><saml:AuthnStatement AuthnInstant="%s"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement></saml:Assertion></samlp:Response>`, samlProtocolNS, samlAssertionNS, instant, f.config.ACSURL, samlFixtureRequestID, f.config.IDPIssuer, samlSuccessStatus, samlAssertionNS, instant, f.config.IDPIssuer, samlPersistentNameID, f.config.IDPIssuer, f.config.SPEntityID, samlFixtureSubject, samlBearerMethod, f.config.ACSURL, samlFixtureRequestID, expiry, before, expiry, f.config.SPEntityID, instant)
	doc := etree.NewDocument()
	if doc.ReadFromString(raw) != nil {
		t.Fatal("samlHTTPFixture document")
	}
	return doc
}
func samlFixtureElement(t *testing.T, doc *etree.Document, path string) *etree.Element {
	t.Helper()
	e := doc.FindElement(path)
	if e == nil {
		t.Fatal("samlHTTPFixture element absent", path)
	}
	return e
}
func (f samlHTTPFixture) sign(t *testing.T, doc *etree.Document, parent string) string {
	t.Helper()
	return f.signMethod(t, doc, parent, "")
}
func (f samlHTTPFixture) signMethod(t *testing.T, doc *etree.Document, parent, override string) string {
	t.Helper()
	return f.signPrefix(t, doc, parent, override, nil)
}
func (f samlHTTPFixture) signPrefix(t *testing.T, doc *etree.Document, parent, override string, prefix *string) string {
	t.Helper()
	if parent != "Assertion" && parent != "Response" {
		t.Fatal("unknown signature parent selector")
	}
	target := doc.Root()
	if parent == "Assertion" {
		target = samlFixtureElement(t, doc, "./Response/Assertion")
	}
	signer, e := dsig.NewSigningContext(f.signer, [][]byte{f.der})
	if e != nil {
		t.Fatal("samlHTTPFixture signing context")
	}
	if prefix != nil {
		signer.Prefix = *prefix
	}
	signer.IdAttribute = "ID"
	signer.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	method := dsig.ECDSASHA256SignatureMethod
	if _, ok := f.signer.(*rsa.PrivateKey); ok {
		method = dsig.RSASHA256SignatureMethod
	}
	if override != "" {
		method = override
	}
	if signer.SetSignatureMethod(method) != nil {
		t.Fatal("samlHTTPFixture signing method")
	}
	signed, e := signer.SignEnveloped(target)
	if e != nil {
		t.Fatal("samlHTTPFixture signature")
	}
	signature := signed.FindElement("./Signature")
	if signature == nil {
		t.Fatal("samlHTTPFixture signature missing")
	}
	// SignEnveloped appends its Signature without etree parent/index metadata.
	// Remove by the actual child slot before placing it once after Issuer.
	signatureIndex := -1
	for i, child := range signed.Child {
		if child == signature {
			signatureIndex = i
			break
		}
	}
	if signatureIndex < 0 || signed.RemoveChildAt(signatureIndex) != signature {
		t.Fatal("samlHTTPFixture signature removal")
	}
	signed.InsertChildAt(1, signature)
	if len(signed.FindElements("./Signature")) != 1 || signature.Parent() != signed || signature.Index() != 1 {
		t.Fatal("samlHTTPFixture signature count or ownership")
	}
	if parent == "Assertion" {
		doc.Root().RemoveChild(target)
		doc.Root().AddChild(signed)
	} else {
		doc.SetRoot(signed)
	}
	return samlFixtureEncoded(t, doc)
}
func samlFixtureEncoded(t *testing.T, doc *etree.Document) string {
	t.Helper()
	raw, e := doc.WriteToBytes()
	if e != nil {
		t.Fatal("samlHTTPFixture XML encoding")
	}
	return base64.StdEncoding.EncodeToString(raw)
}
func samlFixtureDecode(t *testing.T, raw string) *etree.Document {
	t.Helper()
	bytes, e := base64.StdEncoding.DecodeString(raw)
	if e != nil {
		t.Fatal("samlHTTPFixture base64")
	}
	doc := etree.NewDocument()
	if doc.ReadFromBytes(bytes) != nil {
		t.Fatal("samlHTTPFixture XML")
	}
	return doc
}

// defaultDocument uses ordinary scoped default namespaces: Response/Status are
// protocol elements, the outer Issuer and Assertion subtree are assertion
// elements. Signature may independently switch to the XMLDSig default namespace.
func (f samlHTTPFixture) defaultDocument(t *testing.T) *etree.Document {
	t.Helper()
	doc := f.document(t)
	var plain func(*etree.Element)
	plain = func(e *etree.Element) {
		e.Space = ""
		e.RemoveAttr("xmlns:samlp")
		e.RemoveAttr("xmlns:saml")
		for _, child := range e.ChildElements() {
			plain(child)
		}
	}
	plain(doc.Root())
	doc.Root().CreateAttr("xmlns", samlProtocolNS)
	samlFixtureElement(t, doc, "./Response/Issuer").CreateAttr("xmlns", samlAssertionNS)
	samlFixtureElement(t, doc, "./Response/Assertion").CreateAttr("xmlns", samlAssertionNS)
	return doc
}

const samlProtocolNS = "urn:oasis:names:tc:SAML:2.0:protocol"
const samlAssertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"
const samlSuccessStatus = "urn:oasis:names:tc:SAML:2.0:status:Success"
const samlPersistentNameID = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
const samlBearerMethod = "urn:oasis:names:tc:SAML:2.0:cm:bearer"

// response uses genuine maintained signing with exact per-ceremony request and
// fresh assertion IDs. No successful application identity proof is manufactured.
func (f samlHTTPFixture) response(t *testing.T, requestID, subject, parent string) string {
	t.Helper()
	f.now = time.Now().UTC().Truncate(time.Second)
	doc := f.document(t)
	nonce, err := secret.RandomURLSafe(32)
	if err != nil {
		t.Fatal("fixture identity")
	}
	doc.Root().CreateAttr("ID", "_response_"+nonce)
	doc.Root().CreateAttr("InResponseTo", requestID)
	samlFixtureElement(t, doc, "./Response/Assertion").CreateAttr("ID", "_assertion_"+nonce)
	samlFixtureElement(t, doc, "./Response/Assertion/Subject/NameID").SetText(subject)
	samlFixtureElement(t, doc, "./Response/Assertion/Subject/SubjectConfirmation/SubjectConfirmationData").CreateAttr("InResponseTo", requestID)
	return f.sign(t, doc, parent)
}
