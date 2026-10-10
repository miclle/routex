// Package saml validates a bounded SP-initiated SAML authentication proof.
// It creates no Sessions, performs no network I/O and prevents no replay by itself.
// Callers own durable one-use request/assertion and browser correlation, member
// authorization, explicit binding and native MFA after protocol verification.
package saml

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	coresaml "github.com/crewjam/saml"
)

const (
	OperationTimeout    = 10 * time.Second
	MaxResponseBytes    = 256 << 10
	MaxCertificateBytes = 16 << 10
	maxLifetime         = 5 * time.Minute
	maxIssueAge         = 90 * time.Second
	assertionNS         = "urn:oasis:names:tc:SAML:2.0:assertion"
	protocolNS          = "urn:oasis:names:tc:SAML:2.0:protocol"
	signatureNS         = "http://www.w3.org/2000/09/xmldsig#"
	exclusiveC14N       = "http://www.w3.org/2001/10/xml-exc-c14n#"
	persistentNameID    = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	bearerMethod        = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
	entityFormat        = "urn:oasis:names:tc:SAML:2.0:nameid-format:entity"
	successStatus       = "urn:oasis:names:tc:SAML:2.0:status:Success"
)

var (
	ErrConfig      = errors.New("SAML configuration rejected")
	ErrState       = errors.New("SAML request proof rejected")
	ErrProtocol    = errors.New("SAML authentication proof rejected")
	ErrUnavailable = errors.New("SAML operation unavailable")
)

// Config captures one explicit IdP public signing certificate (DER, not PEM).
// No metadata, URLs, keys or claims are fetched or discovered. Issuer/entity are
// exact absolute URI identifiers. SSO/ACS require HTTPS without query or fragment.
type Config struct {
	IDPIssuer             string
	SSOURL                string
	SPEntityID            string
	ACSURL                string
	SigningCertificateDER []byte
}

// Client is immutable and safe for independent concurrent ceremonies. No
// upstream clock/skew/verifier globals or Session middleware are modified.
type Client struct {
	issuer            string
	sso               string
	entity            string
	acs               url.URL
	certificate       *x509.Certificate
	certificateBase64 string
}

type Authorization struct{ RequestID, RelayState string }
type Callback struct{ SAMLResponse, RequestID string }

// Identity contains only verified authentication facts, not application rights.
// Subject preserves exact persistent NameID bytes/case, namespaced by Issuer and
// the caller's exact provider/configuration. DN, email and attributes are absent.
type Identity struct {
	Issuer      string
	Subject     string
	RequestID   string
	AssertionID string
	ExpiresAt   time.Time
}

// New captures configuration and copies the bounded DER before parsing it.
func New(config Config) (*Client, error) {
	if !identifier(config.IDPIssuer) || !identifier(config.SPEntityID) || len(config.SigningCertificateDER) == 0 || len(config.SigningCertificateDER) > MaxCertificateBytes {
		return nil, ErrConfig
	}
	if _, ok := endpoint(config.SSOURL); !ok {
		return nil, ErrConfig
	}
	acs, ok := endpoint(config.ACSURL)
	if !ok {
		return nil, ErrConfig
	}
	der := append([]byte(nil), config.SigningCertificateDER...)
	cert, err := x509.ParseCertificate(der)
	if err != nil || !certificateAllowed(cert, time.Now()) {
		return nil, ErrConfig
	}
	return &Client{issuer: config.IDPIssuer, sso: config.SSOURL, entity: config.SPEntityID, acs: *acs, certificate: cert, certificateBase64: base64.StdEncoding.EncodeToString(der)}, nil
}

// AuthorizationURL encodes one unsigned Redirect AuthnRequest. The caller must
// persist its cryptographically random unique RequestID and opaque RelayState
// before redirecting. RelayState is never a return URL or application authority.
func (c *Client) AuthorizationURL(ctx context.Context, input Authorization) (string, error) {
	if c == nil || !certificateAllowed(c.certificate, time.Now()) {
		return "", ErrConfig
	}
	if ctx == nil || !xmlID(input.RequestID, 32) || !opaque(input.RelayState, 32, 80) {
		return "", ErrState
	}
	operation, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	if operation.Err() != nil {
		return "", ErrUnavailable
	}
	sp := c.provider()
	req, err := sp.MakeAuthenticationRequest(c.sso, coresaml.HTTPRedirectBinding, coresaml.HTTPPostBinding)
	if err != nil {
		return "", ErrProtocol
	}
	// Do not use mutable library clock globals as ceremony authority.
	req.ID = input.RequestID
	req.IssueInstant = time.Now().UTC()
	allowCreate := false
	req.NameIDPolicy.AllowCreate = &allowCreate
	target, err := req.Redirect(input.RelayState, sp)
	if operation.Err() != nil {
		return "", ErrUnavailable
	}
	if err != nil || target == nil || len(target.String()) > 8192 {
		return "", ErrProtocol
	}
	return target.String(), nil
}

// Verify accepts only one canonical base64 HTTP-POST response, never an Artifact
// or IdP-initiated message. Signature verification stays in the maintained
// library. Bounded CPU verification is synchronous; context is checked around
// it, without an unjoinable goroutine or a false preemptive-crypto guarantee.
func (c *Client) Verify(ctx context.Context, input Callback) (Identity, error) {
	if c == nil || c.certificate == nil {
		return Identity{}, ErrConfig
	}
	if ctx == nil || !xmlID(input.RequestID, 32) {
		return Identity{}, ErrState
	}
	operation, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	if operation.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	if len(input.SAMLResponse) == 0 || len(input.SAMLResponse) > base64.StdEncoding.EncodedLen(MaxResponseBytes) {
		return Identity{}, ErrProtocol
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(input.SAMLResponse)
	if err != nil || len(raw) > MaxResponseBytes || base64.StdEncoding.EncodeToString(raw) != input.SAMLResponse {
		return Identity{}, ErrProtocol
	}
	now := time.Now().UTC()
	if !certificateAllowed(c.certificate, now) {
		return Identity{}, ErrProtocol
	}
	root, err := parseXML(operation, raw)
	if err != nil {
		if operation.Err() != nil {
			return Identity{}, ErrUnavailable
		}
		return Identity{}, ErrProtocol
	}
	witness, err := c.profile(root, input.RequestID, now)
	if err != nil {
		return Identity{}, ErrProtocol
	}
	if operation.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	// Default verification, with only the configured pinned certificate. No
	// SignatureVerifier, audience override, HTTP client or decryption key is set.
	verified, err := c.provider().ParseXMLResponse(raw, []string{input.RequestID}, c.acs)
	if operation.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	if err != nil || verified == nil || !witness.matches(verified) {
		return Identity{}, ErrProtocol
	}
	covered, err := c.verifiedProof(operation, raw, witness, input.RequestID, now)
	if operation.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	if err != nil || !covered.matches(verified) {
		return Identity{}, ErrProtocol
	}
	finished := time.Now().UTC()
	if !certificateAllowed(c.certificate, finished) || !finished.Before(witness.identity.ExpiresAt) {
		return Identity{}, ErrProtocol
	}
	return covered.identity, nil
}

func (c *Client) provider() *coresaml.ServiceProvider {
	certificate := c.certificateBase64
	return &coresaml.ServiceProvider{EntityID: c.entity, AcsURL: c.acs, IDPMetadata: &coresaml.EntityDescriptor{EntityID: c.issuer}, IDPCertificate: &certificate, AuthnNameIDFormat: coresaml.PersistentNameIDFormat}
}

func certificateAllowed(cert *x509.Certificate, now time.Time) bool {
	if cert == nil || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || (cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageDigitalSignature == 0) {
		return false
	}
	switch cert.SignatureAlgorithm {
	case x509.SHA256WithRSA, x509.SHA384WithRSA, x509.SHA512WithRSA, x509.ECDSAWithSHA256, x509.ECDSAWithSHA384, x509.ECDSAWithSHA512:
	default:
		return false
	}
	switch key := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		return key.N.BitLen() >= 2048 && key.N.BitLen() <= 4096 && key.E >= 65537
	case *ecdsa.PublicKey:
		return key.Curve != nil && (key.Curve.Params().Name == "P-256" || key.Curve.Params().Name == "P-384" || key.Curve.Params().Name == "P-521")
	default:
		return false
	}
}
func text(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func identifier(value string) bool {
	if !text(value, 2048) || strings.ContainsAny(value, " \\\t\r\n") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) {
			return false
		}
	}
	u, e := url.Parse(value)
	return e == nil && u.IsAbs() && u.User == nil && u.Fragment == ""
}
func endpoint(value string) (*url.URL, bool) {
	if !text(value, 2048) || strings.ContainsAny(value, " \\\t\r\n") {
		return nil, false
	}
	for _, r := range value {
		if unicode.IsSpace(r) {
			return nil, false
		}
	}
	u, e := url.Parse(value)
	return u, e == nil && u.Scheme == "https" && u.Host != "" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == "" && u.String() == value
}
func opaque(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, b := range []byte(value) {
		if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && (b < '0' || b > '9') && !strings.ContainsRune("-._~", rune(b)) {
			return false
		}
	}
	return true
}
func xmlID(value string, minimum int) bool {
	if !opaque(value, minimum, 128) {
		return false
	}
	b := value[0]
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
