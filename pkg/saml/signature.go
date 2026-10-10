package saml

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

func (c *Client) signatureProfile(signature *element) error {
	parent := signature.parent
	if !signature.named(signatureNS, "Signature") || !signature.attrs() || !signature.container() || (!parent.named(protocolNS, "Response") && !parent.named(assertionNS, "Assertion")) {
		return ErrProtocol
	}
	for _, child := range signature.children {
		if !child.named(signatureNS, "SignedInfo") && !child.named(signatureNS, "SignatureValue") && !child.named(signatureNS, "KeyInfo") {
			return ErrProtocol
		}
	}
	info, e := signature.only(signatureNS, "SignedInfo")
	if e != nil || !info.attrs() || !childrenAllowed(info, signatureNS, "CanonicalizationMethod", "SignatureMethod", "Reference") || len(info.children) != 3 {
		return ErrProtocol
	}
	canonical, e := info.only(signatureNS, "CanonicalizationMethod")
	if e != nil || !algorithm(canonical, exclusiveC14N) {
		return ErrProtocol
	}
	method, e := info.only(signatureNS, "SignatureMethod")
	if e != nil || !method.attrs("Algorithm") || !method.simple() || !method.container() {
		return ErrProtocol
	}
	switch method.attr("Algorithm") {
	case "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256", "http://www.w3.org/2001/04/xmldsig-more#rsa-sha384", "http://www.w3.org/2001/04/xmldsig-more#rsa-sha512", "http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha256", "http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha384", "http://www.w3.org/2001/04/xmldsig-more#ecdsa-sha512":
	default:
		return ErrProtocol
	}
	reference, e := info.only(signatureNS, "Reference")
	if e != nil || !reference.attrs("URI") || reference.attr("URI") != "#"+parent.attr("ID") || !childrenAllowed(reference, signatureNS, "Transforms", "DigestMethod", "DigestValue") || len(reference.children) != 3 {
		return ErrProtocol
	}
	transforms, e := reference.only(signatureNS, "Transforms")
	if e != nil || !transforms.attrs() || !childrenAllowed(transforms, signatureNS, "Transform") || len(transforms.children) != 2 || !algorithm(transforms.children[0], signatureNS+"enveloped-signature") || !algorithm(transforms.children[1], exclusiveC14N) {
		return ErrProtocol
	}
	digest, e := reference.only(signatureNS, "DigestMethod")
	if e != nil || !digest.attrs("Algorithm") || !digest.simple() || !digest.container() {
		return ErrProtocol
	}
	digestBytes := 0
	switch digest.attr("Algorithm") {
	case "http://www.w3.org/2001/04/xmlenc#sha256":
		digestBytes = 32
	case "http://www.w3.org/2001/04/xmldsig-more#sha384":
		digestBytes = 48
	case "http://www.w3.org/2001/04/xmlenc#sha512":
		digestBytes = 64
	default:
		return ErrProtocol
	}
	value, e := reference.only(signatureNS, "DigestValue")
	if e != nil || !value.attrs() || !value.simple() || !base64Value(value.value.String(), digestBytes) {
		return ErrProtocol
	}
	signatureValue, e := signature.only(signatureNS, "SignatureValue")
	if e != nil || !signatureValue.attrs() || !signatureValue.simple() || !base64Value(signatureValue.value.String(), 0) {
		return ErrProtocol
	}
	keys := signature.childrenNamed(signatureNS, "KeyInfo")
	if len(keys) > 1 {
		return ErrProtocol
	}
	if len(keys) == 1 {
		key := keys[0]
		if !key.attrs() || !childrenAllowed(key, signatureNS, "X509Data") || len(key.children) != 1 {
			return ErrProtocol
		}
		data := key.children[0]
		if !data.attrs() || !childrenAllowed(data, signatureNS, "X509Certificate") || len(data.children) != 1 {
			return ErrProtocol
		}
		cert := data.children[0]
		if !cert.attrs() || !cert.simple() || strings.Join(strings.Fields(cert.value.String()), "") != c.certificateBase64 {
			return ErrProtocol
		}
	}
	return nil
}
func algorithm(e *element, expected string) bool {
	return e.attrs("Algorithm") && e.attr("Algorithm") == expected && e.simple() && e.container()
}
func base64Value(value string, exact int) bool {
	compact := strings.Join(strings.Fields(value), "")
	raw, e := base64.StdEncoding.Strict().DecodeString(compact)
	return e == nil && len(raw) > 0 && len(raw) <= 8192 && (exact == 0 || len(raw) == exact) && base64.StdEncoding.EncodeToString(raw) == compact
}

// verifiedProof consumes goxmldsig1.6.1's returned canonical referenced element,
// rather than treating crewjam's separately unmarshalled original as that node.
// Both default maintained validators must succeed; no custom verifier is used.
func (c *Client) verifiedProof(ctx context.Context, raw []byte, original proof, requestID string, now time.Time) (proof, error) {
	doc := etree.NewDocument()
	if doc.ReadFromBytes(raw) != nil || doc.Root() == nil {
		return proof{}, ErrProtocol
	}
	target := doc.Root()
	if original.signedParent == "Assertion" {
		var err error
		target, err = etreeutils.NSFindOneChild(target, assertionNS, "Assertion")
		if err != nil || target == nil {
			return proof{}, ErrProtocol
		}
	}
	ns, err := etreeutils.NSBuildParentContext(target)
	if err != nil {
		return proof{}, ErrProtocol
	}
	ns, err = ns.SubContext(target)
	if err != nil {
		return proof{}, ErrProtocol
	}
	target, err = etreeutils.NSDetatch(ns, target)
	if err != nil {
		return proof{}, ErrProtocol
	}
	validator := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{c.certificate}})
	validator.IdAttribute = "ID"
	if ctx.Err() != nil {
		return proof{}, ErrUnavailable
	}
	verified, err := validator.Validate(target)
	if ctx.Err() != nil {
		return proof{}, ErrUnavailable
	}
	if err != nil || verified == nil {
		return proof{}, ErrProtocol
	}
	document := etree.NewDocument()
	document.SetRoot(verified)
	canonical, err := document.WriteToBytes()
	if err != nil || len(canonical) > MaxResponseBytes {
		return proof{}, ErrProtocol
	}
	covered, err := parseXML(ctx, canonical)
	if err != nil {
		return proof{}, err
	}
	assertion := covered
	if original.signedParent == "Response" {
		if !covered.named(protocolNS, "Response") {
			return proof{}, ErrProtocol
		}
		assertion, err = covered.only(assertionNS, "Assertion")
		if err != nil {
			return proof{}, ErrProtocol
		}
	}
	result, err := c.assertionProfile(assertion, requestID, now)
	if err != nil {
		return proof{}, ErrProtocol
	}
	// These proof fields are comparable and contain no attributes or library nodes.
	result.signedParent = original.signedParent
	if result != original {
		return proof{}, ErrProtocol
	}
	return result, nil
}
