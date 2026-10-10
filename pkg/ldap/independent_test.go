package ldap_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"
	routeldap "github.com/miclle/routex/pkg/ldap"
)

const ldapIndependentBase = "ou=people,dc=example,dc=test"
const ldapIndependentDN = "uid=found," + ldapIndependentBase
const ldapIndependentUUID = "A1b2c3D4-1234-5678-9AbC-1234567890aB"

// Valid peer envelopes use the pinned maintained BER codec. Raw malformed
// octets appear only in the adversarial admission cases below. Every successful
// flow uses a real verified TLS handshake and the package's actual LDAP wire.
func ldapIndependentBER(tag byte, body ...[]byte) []byte {
	value := bytes.Join(body, nil)
	class, kind, number := ber.Class(tag&0xc0), ber.Type(tag&0x20), ber.Tag(tag&0x1f)
	if kind == ber.TypePrimitive && (tag == 2 || tag == 0x0a) {
		integer, err := ber.ParseInt64(value)
		if err != nil {
			panic("invalid test-owned integer")
		}
		return ber.NewInteger(class, kind, number, integer, "").Bytes()
	}
	if kind == ber.TypePrimitive && (tag == 4 || tag == 0x80) {
		return ber.NewString(class, kind, number, string(value), "").Bytes()
	}
	packet := ber.Encode(class, kind, number, nil, "")
	_, _ = packet.Data.Write(value)
	return packet.Bytes()
}
func ldapIndependentSplit(raw []byte) (byte, []byte, []byte, error) {
	reader := bytes.NewReader(raw)
	packet, err := ber.ReadPacket(reader)
	if err != nil {
		return 0, nil, nil, err
	}
	if packet.Tag > 31 {
		return 0, nil, nil, fmt.Errorf("unexpected test-owned high tag")
	}
	tag := byte(packet.ClassType) | byte(packet.TagType) | byte(packet.Tag)
	return tag, append([]byte(nil), packet.Data.Bytes()...), raw[len(raw)-reader.Len():], nil
}
func ldapIndependentRead(c net.Conn) (byte, []byte, []byte, error) {
	// All client requests are locally bounded; this peer never changes a global
	// BER parser limit or uses the production decoder to invent a successful DN.
	envelope, err := ber.ReadPacket(io.LimitReader(c, 32769))
	if err != nil {
		return 0, nil, nil, err
	}
	if envelope.ClassType != ber.ClassUniversal || envelope.TagType != ber.TypeConstructed || envelope.Tag != ber.TagSequence || len(envelope.Children) != 2 {
		return 0, nil, nil, fmt.Errorf("message envelope or unexpected request controls")
	}
	id, op := envelope.Children[0], envelope.Children[1]
	if id.ClassType != ber.ClassUniversal || id.TagType != ber.TypePrimitive || id.Tag != ber.TagInteger || len(id.Data.Bytes()) == 0 {
		return 0, nil, nil, fmt.Errorf("message id")
	}
	if op.ClassType != ber.ClassApplication || op.TagType != ber.TypeConstructed || op.Tag > 31 {
		return 0, nil, nil, fmt.Errorf("request operation")
	}
	return byte(op.ClassType) | byte(op.TagType) | byte(op.Tag), append([]byte(nil), id.Data.Bytes()...), append([]byte(nil), op.Data.Bytes()...), nil
}
func ldapIndependentWrite(c net.Conn, id []byte, tag byte, body []byte) error {
	_, err := c.Write(ldapIndependentBER(0x30, ldapIndependentBER(2, id), ldapIndependentBER(tag, body)))
	return err
}
func ldapIndependentResult(code byte) []byte {
	return bytes.Join([][]byte{ldapIndependentBER(0x0a, []byte{code}), ldapIndependentBER(4, nil), ldapIndependentBER(4, []byte("private-directory-message"))}, nil)
}
func ldapIndependentBind(c net.Conn, dn, password string, code byte) error {
	tag, id, op, err := ldapIndependentRead(c)
	if err != nil {
		return err
	}
	if tag != 0x60 {
		return fmt.Errorf("expected bind")
	}
	v, version, rest, err := ldapIndependentSplit(op)
	if err != nil || v != 2 || !bytes.Equal(version, []byte{3}) {
		return fmt.Errorf("LDAP version")
	}
	v, name, rest, err := ldapIndependentSplit(rest)
	if err != nil || v != 4 || string(name) != dn {
		return fmt.Errorf("bind DN differs")
	}
	v, proof, rest, err := ldapIndependentSplit(rest)
	if err != nil || v != 0x80 || string(proof) != password || len(rest) != 0 || len(proof) == 0 {
		return fmt.Errorf("bind proof differs or anonymous")
	}
	return ldapIndependentWrite(c, id, 0x61, ldapIndependentResult(code))
}
func ldapIndependentSearch(c net.Conn, username string) ([]byte, error) {
	return ldapIndependentSearchMode(c, username, routeldap.IdentityAttributeEntryUUID, false)
}
func ldapIndependentSearchMode(c net.Conn, username string, attribute routeldap.IdentityAttribute, recheck bool) ([]byte, error) {
	tag, id, op, err := ldapIndependentRead(c)
	if err != nil {
		return nil, err
	}
	if tag != 0x63 {
		return nil, fmt.Errorf("expected search")
	}
	base, scope := ldapIndependentBase, byte(2)
	if recheck {
		base, scope = ldapIndependentDN, 0
	}
	expected := []struct {
		tag   byte
		value []byte
	}{{4, []byte(base)}, {0x0a, []byte{scope}}, {0x0a, []byte{0}}, {2, []byte{2}}}
	for _, want := range expected {
		var got byte
		var value []byte
		got, value, op, err = ldapIndependentSplit(op)
		if err != nil || got != want.tag || !bytes.Equal(value, want.value) {
			return nil, fmt.Errorf("search scope/deref/size differs")
		}
	}
	got, limit, rest, err := ldapIndependentSplit(op)
	if err != nil || got != 2 || len(limit) != 1 || limit[0] == 0 || limit[0] > 10 {
		return nil, fmt.Errorf("search time bound")
	}
	got, value, rest, err := ldapIndependentSplit(rest)
	if err != nil || got != 1 || !bytes.Equal(value, []byte{0}) {
		return nil, fmt.Errorf("search types-only")
	}
	got, filter, rest, err := ldapIndependentSplit(rest)
	if err != nil {
		return nil, err
	}
	if recheck {
		if got != 0x87 || string(filter) != "objectClass" {
			return nil, fmt.Errorf("post-bind filter is not fixed presence")
		}
	} else {
		if got != 0xa3 {
			return nil, fmt.Errorf("escaped username changed filter structure")
		}
		got, field, tail, parseErr := ldapIndependentSplit(filter)
		if parseErr != nil || got != 4 || string(field) != "uid" {
			return nil, fmt.Errorf("search attribute")
		}
		got, value, tail, parseErr = ldapIndependentSplit(tail)
		if parseErr != nil || got != 4 || string(value) != username || len(tail) != 0 {
			return nil, fmt.Errorf("username not exact escaped value")
		}
	}
	got, attributes, rest, err := ldapIndependentSplit(rest)
	if err != nil || got != 0x30 || len(rest) != 0 {
		return nil, fmt.Errorf("search attributes shape")
	}
	got, value, attributes, err = ldapIndependentSplit(attributes)
	if err != nil || got != 4 || string(value) != string(attribute) || len(attributes) != 0 {
		return nil, fmt.Errorf("unnecessary profile attributes requested")
	}
	return id, nil
}
func ldapIndependentAttribute(name string, values ...[]byte) []byte {
	var encoded [][]byte
	for _, value := range values {
		encoded = append(encoded, ldapIndependentBER(4, value))
	}
	return ldapIndependentBER(0x30, ldapIndependentBER(4, []byte(name)), ldapIndependentBER(0x31, encoded...))
}
func ldapIndependentEntryWith(c net.Conn, id []byte, dn string, attributes ...[]byte) error {
	return ldapIndependentWrite(c, id, 0x64, bytes.Join([][]byte{ldapIndependentBER(4, []byte(dn)), ldapIndependentBER(0x30, attributes...)}, nil))
}
func ldapIndependentEntry(c net.Conn, id []byte, dn string) error {
	return ldapIndependentEntryWith(c, id, dn, ldapIndependentAttribute("entryUUID", []byte(ldapIndependentUUID)))
}
func ldapIndependentRecheck(c net.Conn, attribute routeldap.IdentityAttribute, subject []byte) error {
	id, err := ldapIndependentSearchMode(c, "", attribute, true)
	if err != nil {
		return err
	}
	if err := ldapIndependentEntryWith(c, id, ldapIndependentDN, ldapIndependentAttribute(string(attribute), subject)); err != nil {
		return err
	}
	if err := ldapIndependentWrite(c, id, 0x65, ldapIndependentResult(0)); err != nil {
		return err
	}
	return ldapIndependentEOF(c)
}
func ldapIndependentEOF(c net.Conn) error {
	var b [1]byte
	n, err := c.Read(b[:])
	if n != 0 || err == nil {
		return fmt.Errorf("unexpected replay or extra operation")
	}
	return nil
}

type ldapIndependentTracked struct {
	net.Conn
	once   sync.Once
	closed atomic.Bool
}

func (c *ldapIndependentTracked) Close() error {
	c.once.Do(func() { c.closed.Store(true) })
	return c.Conn.Close()
}

type ldapIndependentPeer struct {
	listener    net.Listener
	address     string
	roots       *x509.CertPool
	certificate *x509.Certificate
	done        chan error
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	connection  *ldapIndependentTracked
	dials       atomic.Int32
	joined      bool
}

func ldapIndependentServer(t *testing.T, allowHandshakeFailure bool, handle func(*tls.Conn) error) *ldapIndependentPeer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "directory.example.invalid"}, DNSNames: []string{"directory.example.invalid"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	p := &ldapIndependentPeer{listener: listener, address: listener.Addr().String(), roots: roots, certificate: cert, done: make(chan error, 1), ctx: ctx, cancel: cancel}
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			p.done <- err
			return
		}
		defer func() { _ = raw.Close() }()
		_ = raw.SetDeadline(now.Add(10 * time.Second))
		c := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
		if err := c.HandshakeContext(ctx); err != nil {
			if allowHandshakeFailure {
				p.done <- nil
			} else {
				p.done <- fmt.Errorf("verified handshake failed")
			}
			return
		}
		p.done <- handle(c)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		p.mu.Lock()
		owned := p.connection
		p.mu.Unlock()
		if owned != nil {
			_ = owned.Close()
		}
		if !p.joined {
			select {
			case <-p.done:
				p.joined = true
			case <-time.After(time.Second):
				t.Error("owned TLS peer did not join within cleanup allowance")
			}
		}
		cancel()
	})
	return p
}
func (p *ldapIndependentPeer) config() routeldap.Config {
	return routeldap.Config{Endpoint: "ldaps://directory.example.invalid:636", BindDN: "cn=service,dc=example,dc=test", BindPassword: " service-secret ", BaseDN: ldapIndependentBase, UserFilter: "(uid={username})", IdentityAttribute: routeldap.IdentityAttributeEntryUUID, TLSConfig: &tls.Config{RootCAs: p.roots, ServerName: "ignored-wrong-name.invalid", MinVersion: tls.VersionTLS12}, EndpointPolicy: func(ctx context.Context, u *url.URL) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if u.String() != "ldaps://directory.example.invalid:636" {
			return fmt.Errorf("endpoint changed")
		}
		return nil
	}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		p.dials.Add(1)
		if network != "tcp" || address != "directory.example.invalid:636" {
			return nil, fmt.Errorf("dial target changed")
		}
		raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", p.address)
		if err != nil {
			return nil, err
		}
		tracked := &ldapIndependentTracked{Conn: raw}
		p.mu.Lock()
		p.connection = tracked
		p.mu.Unlock()
		return tracked, nil
	}}
}
func (p *ldapIndependentPeer) joinedClosed(t *testing.T) {
	t.Helper()
	if p.dials.Load() != 1 {
		t.Fatal("expected exactly one connection, no retry", p.dials.Load())
	}
	p.mu.Lock()
	owned := p.connection
	p.mu.Unlock()
	if owned == nil || !owned.closed.Load() {
		t.Error("SDK did not close owned connector")
	}
	select {
	case err := <-p.done:
		p.joined = true
		if err != nil {
			t.Error(err)
		}
	case <-p.ctx.Done():
		t.Error("peer did not observe closure before shared deadline")
	}
}
func ldapIndependentClient(t *testing.T, cfg routeldap.Config) *routeldap.Client {
	t.Helper()
	c, err := routeldap.New(cfg)
	if err != nil {
		t.Fatal("positive client configuration", err)
	}
	return c
}
func ldapIndependentSafe(t *testing.T, err error, private ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("negative unexpectedly authenticated")
	}
	for _, value := range private {
		if value != "" && strings.Contains(err.Error(), value) {
			t.Fatal("error exposed private input")
		}
	}
}

func TestLDAPIndependentEscapingAndExactSequence(t *testing.T) {
	username := `a*)(uid=*)\é`
	password := " user-password "
	p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
		if err := ldapIndependentBind(c, "cn=service,dc=example,dc=test", " service-secret ", 0); err != nil {
			return err
		}
		id, err := ldapIndependentSearch(c, username)
		if err != nil {
			return err
		}
		if err := ldapIndependentEntry(c, id, ldapIndependentDN); err != nil {
			return err
		}
		if err := ldapIndependentWrite(c, id, 0x65, ldapIndependentResult(0)); err != nil {
			return err
		}
		if err := ldapIndependentBind(c, ldapIndependentDN, password, 0); err != nil {
			return err
		}
		return ldapIndependentRecheck(c, routeldap.IdentityAttributeEntryUUID, []byte(ldapIndependentUUID))
	})
	cfg := p.config()
	client := ldapIndependentClient(t, cfg)
	// Constructor must own a TLS clone; mutation cannot turn trusted roots/name
	// into a later external configuration dependency.
	cfg.TLSConfig.RootCAs = x509.NewCertPool()
	cfg.TLSConfig.ServerName = "changed.invalid"
	identity, err := client.Authenticate(p.ctx, username, password)
	if err != nil || identity.DN != ldapIndependentDN || identity.Attribute != routeldap.IdentityAttributeEntryUUID || !bytes.Equal(identity.Subject, []byte(ldapIndependentUUID)) {
		t.Fatal("exact DN and stable identity authentication failed", err)
	}
	p.joinedClosed(t)
}

func TestLDAPIndependentSearchDenials(t *testing.T) {
	for _, mode := range []string{"zero", "ambiguous", "referral", "outside_base", "invalid_dn", "unexpected_attributes"} {
		t.Run(mode, func(t *testing.T) {
			var searched atomic.Bool
			p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
				if err := ldapIndependentBind(c, "cn=service,dc=example,dc=test", " service-secret ", 0); err != nil {
					return err
				}
				id, err := ldapIndependentSearch(c, "member")
				if err != nil {
					return err
				}
				searched.Store(true)
				switch mode {
				case "ambiguous":
					for _, dn := range []string{ldapIndependentDN, "uid=second," + ldapIndependentBase} {
						if err := ldapIndependentEntry(c, id, dn); err != nil {
							return err
						}
					}
				case "referral":
					if err := ldapIndependentWrite(c, id, 0x73, ldapIndependentBER(4, []byte("ldaps://untrusted.example.invalid/dc=other"))); err != nil {
						return err
					}
					return ldapIndependentEOF(c)
				case "outside_base":
					if err := ldapIndependentEntry(c, id, "uid=found,dc=other,dc=test"); err != nil {
						return err
					}
				case "invalid_dn":
					if err := ldapIndependentEntry(c, id, "uid=broken,not-an-rdn"); err != nil {
						return err
					}
				case "unexpected_attributes":
					attribute := ldapIndependentBER(0x30, ldapIndependentBER(4, []byte("mail")), ldapIndependentBER(0x31, ldapIndependentBER(4, []byte("private-profile@example.invalid"))))
					if err := ldapIndependentWrite(c, id, 0x64, bytes.Join([][]byte{ldapIndependentBER(4, []byte(ldapIndependentDN)), ldapIndependentBER(0x30, attribute)}, nil)); err != nil {
						return err
					}
				}
				if err := ldapIndependentWrite(c, id, 0x65, ldapIndependentResult(0)); err != nil {
					return err
				}
				return ldapIndependentEOF(c)
			})
			identity, err := ldapIndependentClient(t, p.config()).Authenticate(p.ctx, "member", "private-user-password")
			ldapIndependentSafe(t, err, "private-directory-message", "private-user-password", "untrusted.example.invalid", "private-profile@example.invalid")
			expected := routeldap.ErrAuthentication
			if mode == "referral" {
				expected = routeldap.ErrUnavailable
			}
			if !errors.Is(err, expected) || identity.DN != "" || identity.Attribute != "" || len(identity.Subject) != 0 || !searched.Load() {
				t.Error("denial did not reach exact search or returned identity", err)
			}
			p.joinedClosed(t)
		})
	}
}

func TestLDAPIndependentNoAnonymousBindOrRetry(t *testing.T) {
	for _, stage := range []string{"service", "user"} {
		t.Run(stage, func(t *testing.T) {
			var rejected atomic.Bool
			p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
				code := byte(0)
				if stage == "service" {
					code = 49
				}
				if err := ldapIndependentBind(c, "cn=service,dc=example,dc=test", " service-secret ", code); err != nil {
					return err
				}
				if stage == "service" {
					rejected.Store(true)
					return ldapIndependentEOF(c)
				}
				id, err := ldapIndependentSearch(c, "member")
				if err != nil {
					return err
				}
				if err := ldapIndependentEntry(c, id, ldapIndependentDN); err != nil {
					return err
				}
				if err := ldapIndependentWrite(c, id, 0x65, ldapIndependentResult(0)); err != nil {
					return err
				}
				if err := ldapIndependentBind(c, ldapIndependentDN, "private-user-password", 49); err != nil {
					return err
				}
				rejected.Store(true)
				return ldapIndependentEOF(c)
			})
			identity, err := ldapIndependentClient(t, p.config()).Authenticate(p.ctx, "member", "private-user-password")
			// The peer records denial after its response write; join before observing it.
			p.joinedClosed(t)
			if !errors.Is(err, routeldap.ErrAuthentication) || identity.DN != "" || identity.Attribute != "" || len(identity.Subject) != 0 || !rejected.Load() {
				t.Error("bind denial lost", err)
			}
			ldapIndependentSafe(t, err, "private-directory-message", "private-user-password", " service-secret ")
		})
	}
	var dials atomic.Int32
	cfg := routeldap.Config{Endpoint: "ldaps://directory.example.invalid:636", BindDN: "cn=service,dc=example,dc=test", BindPassword: "service", BaseDN: ldapIndependentBase, UserFilter: "(uid={username})", IdentityAttribute: routeldap.IdentityAttributeEntryUUID, EndpointPolicy: func(context.Context, *url.URL) error { return nil }, DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("must not dial")
	}}
	client := ldapIndependentClient(t, cfg)
	for _, input := range [][2]string{{"", "password"}, {"member", ""}, {"bad\x00user", "password"}, {string([]byte{0xff}), "password"}, {strings.Repeat("x", 257), "password"}, {"member", strings.Repeat("x", 4097)}} {
		if _, err := client.Authenticate(context.Background(), input[0], input[1]); !errors.Is(err, routeldap.ErrInvalidInput) {
			t.Error("invalid input classified incorrectly", err)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("invalid input attempted anonymous/network bind")
	}
	for _, mutate := range []func(*routeldap.Config){func(c *routeldap.Config) { c.IdentityAttribute = "" }, func(c *routeldap.Config) { c.IdentityAttribute = routeldap.IdentityAttribute("entryuuid") }, func(c *routeldap.Config) { c.BindDN = "" }, func(c *routeldap.Config) { c.BindPassword = "" }, func(c *routeldap.Config) { c.TLSConfig = &tls.Config{InsecureSkipVerify: true} }, func(c *routeldap.Config) { c.Endpoint = "ldap://directory.example.invalid:389" }} {
		invalid := cfg
		mutate(&invalid)
		if _, err := routeldap.New(invalid); !errors.Is(err, routeldap.ErrInvalidConfig) {
			t.Error("unsafe configuration admitted", err)
		}
	}
}

func TestLDAPIndependentCertificateVerificationAndPolicy(t *testing.T) {
	p := ldapIndependentServer(t, true, func(*tls.Conn) error { return fmt.Errorf("untrusted peer received LDAP") })
	cfg := p.config()
	pool := x509.NewCertPool()
	cfg.TLSConfig.RootCAs = pool
	client := ldapIndependentClient(t, cfg)
	// Adding trust to the caller-owned pool after New must not alter its snapshot.
	pool.AddCert(p.certificate)
	identity, err := client.Authenticate(p.ctx, "member", "private-user-password")
	if !errors.Is(err, routeldap.ErrUnavailable) || identity.DN != "" || identity.Attribute != "" || len(identity.Subject) != 0 {
		t.Error("untrusted certificate admitted or wrong boundary", err)
	}
	ldapIndependentSafe(t, err, "directory.example.invalid", "private-user-password")
	p.joinedClosed(t)
	var dials atomic.Int32
	cfg = p.config()
	cfg.DialContext = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("must not dial")
	}
	cfg.EndpointPolicy = func(context.Context, *url.URL) error { return fmt.Errorf("private-policy-detail") }
	if _, err := ldapIndependentClient(t, cfg).Authenticate(context.Background(), "member", "private-user-password"); !errors.Is(err, routeldap.ErrUnavailable) {
		t.Error("policy denial not fail closed", err)
	} else {
		ldapIndependentSafe(t, err, "private-policy-detail")
	}
	if dials.Load() != 0 {
		t.Fatal("endpoint policy was evaluated after dial")
	}
}

func TestLDAPIndependentCancellationOwnsConnection(t *testing.T) {
	for _, stage := range []string{"service", "search", "user", "recheck"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var reached atomic.Bool
			p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
				if stage == "service" {
					tag, _, _, err := ldapIndependentRead(c)
					if err != nil || tag != 0x60 {
						return fmt.Errorf("service cancellation stage missing")
					}
					reached.Store(true)
					cancel()
					return ldapIndependentEOF(c)
				}
				if err := ldapIndependentBind(c, "cn=service,dc=example,dc=test", " service-secret ", 0); err != nil {
					return err
				}
				id, err := ldapIndependentSearch(c, "member")
				if err != nil {
					return err
				}
				if stage == "search" {
					reached.Store(true)
					cancel()
					return ldapIndependentEOF(c)
				}
				if err := ldapIndependentEntry(c, id, ldapIndependentDN); err != nil {
					return err
				}
				if err := ldapIndependentWrite(c, id, 0x65, ldapIndependentResult(0)); err != nil {
					return err
				}
				if stage == "user" {
					tag, _, _, err := ldapIndependentRead(c)
					if err != nil || tag != 0x60 {
						return fmt.Errorf("user cancellation stage missing")
					}
					reached.Store(true)
					cancel()
					return ldapIndependentEOF(c)
				}
				if err := ldapIndependentBind(c, ldapIndependentDN, "private-user-password", 0); err != nil {
					return err
				}
				if _, err := ldapIndependentSearchMode(c, "", routeldap.IdentityAttributeEntryUUID, true); err != nil {
					return err
				}
				reached.Store(true)
				cancel()
				return ldapIndependentEOF(c)
			})
			identity, err := ldapIndependentClient(t, p.config()).Authenticate(ctx, "member", "private-user-password")
			ldapIndependentSafe(t, err, "private-user-password", " service-secret ")
			if identity.DN != "" || identity.Attribute != "" || len(identity.Subject) != 0 || !reached.Load() || ctx.Err() != context.Canceled {
				t.Error("selected cancellation was not observed")
			}
			p.joinedClosed(t)
		})
	}
}

func TestLDAPIndependentMalformedAndBoundedBER(t *testing.T) {
	for _, mode := range []string{"indefinite", "oversize_declared", "wrong_operation", "wrong_message_id", "response_control", "depth_limit"} {
		t.Run(mode, func(t *testing.T) {
			var mutated atomic.Bool
			p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
				tag, id, _, err := ldapIndependentRead(c)
				if err != nil || tag != 0x60 {
					return fmt.Errorf("malformed control did not reach initial bind")
				}
				mutated.Store(true)
				switch mode {
				case "indefinite":
					_, err = c.Write([]byte{0x30, 0x80, 0, 0})
				case "oversize_declared":
					_, err = c.Write([]byte{0x30, 0x83, 1, 0, 1})
				case "wrong_operation":
					err = ldapIndependentWrite(c, id, 0x65, ldapIndependentResult(0))
				case "wrong_message_id":
					original, parseErr := ber.ParseInt64(id)
					if parseErr != nil {
						return parseErr
					}
					next := ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, original+1, "")
					err = ldapIndependentWrite(c, next.Data.Bytes(), 0x61, ldapIndependentResult(0))
				case "response_control":
					_, err = c.Write(ldapIndependentBER(0x30, ldapIndependentBER(2, id), ldapIndependentBER(0x61, ldapIndependentResult(0)), ldapIndependentBER(0xa0, ldapIndependentBER(0x30, ldapIndependentBER(4, []byte("1.2.3")), ldapIndependentBER(4, []byte("private-control-value"))))))
				case "depth_limit":
					nested := ldapIndependentBER(0x30, nil)
					for range 40 {
						nested = ldapIndependentBER(0x30, nested)
					}
					err = ldapIndependentWrite(c, id, 0x61, nested)
				}
				if err != nil {
					return err
				}
				return ldapIndependentEOF(c)
			})
			ctx, cancel := context.WithTimeout(p.ctx, 3*time.Second)
			defer cancel()
			identity, err := ldapIndependentClient(t, p.config()).Authenticate(ctx, "member", "private-user-password")
			if !errors.Is(err, routeldap.ErrUnavailable) || identity.DN != "" || identity.Attribute != "" || len(identity.Subject) != 0 || !mutated.Load() {
				t.Error("malformed/bounded response admitted or unrelated failure", err)
			}
			ldapIndependentSafe(t, err, "private-directory-message", "private-user-password")
			p.joinedClosed(t)
		})
	}
}

func TestLDAPIndependentMalformedSearchEntryShape(t *testing.T) {
	for _, mode := range []string{"dn_utf8_string", "attributes_octet_string", "attributes_primitive_sequence", "attributes_wrong_class_sequence", "identity_name_utf8_string", "identity_values_sequence", "identity_value_utf8_string"} {
		t.Run(mode, func(t *testing.T) {
			var searched atomic.Bool
			var userBindOrLaterOperation atomic.Bool
			p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
				if err := ldapIndependentBind(c, "cn=service,dc=example,dc=test", " service-secret ", 0); err != nil {
					return err
				}
				id, err := ldapIndependentSearch(c, "member")
				if err != nil {
					return err
				}
				searched.Store(true)
				dn := ldapIndependentBER(4, []byte(ldapIndependentDN))
				attributes := ldapIndependentBER(0x30, ldapIndependentAttribute("entryUUID", []byte(ldapIndependentUUID)))
				switch mode {
				case "dn_utf8_string":
					dn = ldapIndependentBER(0x0c, []byte(ldapIndependentDN))
				case "attributes_octet_string":
					attributes = ldapIndependentBER(4, nil)
				case "attributes_primitive_sequence":
					attributes = ldapIndependentBER(0x10, nil)
				case "attributes_wrong_class_sequence":
					attributes = ldapIndependentBER(0xa0, nil)
				case "identity_name_utf8_string":
					attributes = ldapIndependentBER(0x30, ldapIndependentBER(0x30, ldapIndependentBER(0x0c, []byte("entryUUID")), ldapIndependentBER(0x31, ldapIndependentBER(4, []byte(ldapIndependentUUID)))))
				case "identity_values_sequence":
					attributes = ldapIndependentBER(0x30, ldapIndependentBER(0x30, ldapIndependentBER(4, []byte("entryUUID")), ldapIndependentBER(0x30, ldapIndependentBER(4, []byte(ldapIndependentUUID)))))
				case "identity_value_utf8_string":
					attributes = ldapIndependentBER(0x30, ldapIndependentBER(0x30, ldapIndependentBER(4, []byte("entryUUID")), ldapIndependentBER(0x31, ldapIndependentBER(0x0c, []byte(ldapIndependentUUID)))))
				}
				entry := ldapIndependentBER(0x30, ldapIndependentBER(2, id), ldapIndependentBER(0x64, dn, attributes))
				done := ldapIndependentBER(0x30, ldapIndependentBER(2, id), ldapIndependentBER(0x65, ldapIndependentResult(0)))
				// A complete success result follows the malformed entry in the same write.
				// Failure cannot be manufactured by withholding SearchResultDone.
				response := append(entry, done...)
				n, err := c.Write(response)
				if err != nil {
					return err
				}
				if n != len(response) {
					return io.ErrShortWrite
				}
				var next [1]byte
				n, err = c.Read(next[:])
				if n != 0 {
					userBindOrLaterOperation.Store(true)
					return fmt.Errorf("malformed entry permitted a user bind or later operation")
				}
				if err == nil {
					return fmt.Errorf("malformed entry did not close owned I/O")
				}
				return nil
			})
			identity, err := ldapIndependentClient(t, p.config()).Authenticate(p.ctx, "member", "private-user-password")
			if !errors.Is(err, routeldap.ErrUnavailable) || identity.DN != "" || identity.Attribute != "" || len(identity.Subject) != 0 || !searched.Load() {
				t.Error("malformed entry was not rejected at its observed search boundary", err)
			}
			ldapIndependentSafe(t, err, "private-user-password", " service-secret ", "private-directory-message")
			p.joinedClosed(t)
			if userBindOrLaterOperation.Load() {
				t.Error("user bind followed a malformed search entry")
			}
		})
	}
}

func ldapIndependentCompleteSearch(c net.Conn, id []byte, dn string, attributes ...[]byte) error {
	entry := ldapIndependentBER(0x30, ldapIndependentBER(2, id), ldapIndependentBER(0x64, ldapIndependentBER(4, []byte(dn)), ldapIndependentBER(0x30, attributes...)))
	done := ldapIndependentBER(0x30, ldapIndependentBER(2, id), ldapIndependentBER(0x65, ldapIndependentResult(0)))
	response := append(entry, done...)
	n, err := c.Write(response)
	if err == nil && n != len(response) {
		return io.ErrShortWrite
	}
	return err
}
func TestLDAPIndependentStableIdentityPositive(t *testing.T) {
	for _, attribute := range []routeldap.IdentityAttribute{routeldap.IdentityAttributeEntryUUID, routeldap.IdentityAttributeObjectGUID} {
		for _, nameMode := range []string{"canonical", "lowercase", "uppercase"} {
			t.Run(string(attribute)+"/"+nameMode, func(t *testing.T) {
				responseAttribute := string(attribute)
				if nameMode == "lowercase" {
					responseAttribute = strings.ToLower(responseAttribute)
				}
				if nameMode == "uppercase" {
					responseAttribute = strings.ToUpper(responseAttribute)
				}
				subject := []byte(ldapIndependentUUID)
				if attribute == routeldap.IdentityAttributeObjectGUID {
					subject = []byte{0, 1, 2, 3, 0x80, 0xff, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
				}
				var rechecked atomic.Bool
				p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
					if err := ldapIndependentBind(c, "cn=service,dc=example,dc=test", " service-secret ", 0); err != nil {
						return err
					}
					id, err := ldapIndependentSearchMode(c, "member", attribute, false)
					if err != nil {
						return err
					}
					if err := ldapIndependentCompleteSearch(c, id, ldapIndependentDN, ldapIndependentAttribute(responseAttribute, subject)); err != nil {
						return err
					}
					if err := ldapIndependentBind(c, ldapIndependentDN, "private-user-password", 0); err != nil {
						return err
					}
					id, err = ldapIndependentSearchMode(c, "", attribute, true)
					if err != nil {
						return err
					}
					rechecked.Store(true)
					if err := ldapIndependentCompleteSearch(c, id, ldapIndependentDN, ldapIndependentAttribute(responseAttribute, subject)); err != nil {
						return err
					}
					return ldapIndependentEOF(c)
				})
				cfg := p.config()
				cfg.IdentityAttribute = attribute
				identity, err := ldapIndependentClient(t, cfg).Authenticate(p.ctx, "member", "private-user-password")
				if err != nil || identity.DN != ldapIndependentDN || identity.Attribute != attribute || !bytes.Equal(identity.Subject, subject) || !rechecked.Load() {
					t.Error("exact stable identity and post-bind proof lost", err)
				}
				p.joinedClosed(t)
			})
		}
	}
}
func TestLDAPIndependentStableIdentityDenials(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "multivalue", "wrong_mode", "attribute_options", "attribute_oid", "attribute_unicode_alias", "nil_uuid", "invalid_uuid", "zero_guid", "invalid_guid_shape", "changed_identity", "changed_dn", "post_acl", "missing_recheck", "duplicate_recheck"} {
		t.Run(mode, func(t *testing.T) {
			attribute := routeldap.IdentityAttributeEntryUUID
			subject := []byte(ldapIndependentUUID)
			if mode == "zero_guid" || mode == "invalid_guid_shape" {
				attribute = routeldap.IdentityAttributeObjectGUID
				subject = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
			}
			post := mode == "changed_identity" || mode == "changed_dn" || mode == "post_acl" || mode == "missing_recheck" || mode == "duplicate_recheck"
			var searched, userBound, rechecked atomic.Bool
			p := ldapIndependentServer(t, false, func(c *tls.Conn) error {
				if err := ldapIndependentBind(c, "cn=service,dc=example,dc=test", " service-secret ", 0); err != nil {
					return err
				}
				id, err := ldapIndependentSearchMode(c, "member", attribute, false)
				if err != nil {
					return err
				}
				searched.Store(true)
				attrs := [][]byte{ldapIndependentAttribute(string(attribute), subject)}
				switch mode {
				case "missing":
					attrs = nil
				case "duplicate":
					attrs = append(attrs, attrs[0])
				case "multivalue":
					attrs = [][]byte{ldapIndependentAttribute(string(attribute), subject, subject)}
				case "wrong_mode":
					attrs = [][]byte{ldapIndependentAttribute("objectGUID", subject)}
				case "attribute_options":
					attrs = [][]byte{ldapIndependentAttribute("entryUUID;binary", subject)}
				case "attribute_oid":
					attrs = [][]byte{ldapIndependentAttribute("1.3.6.1.1.16.4", subject)}
				case "attribute_unicode_alias":
					attrs = [][]byte{ldapIndependentAttribute("entryUUİD", subject)}
				case "nil_uuid":
					attrs = [][]byte{ldapIndependentAttribute("entryUUID", []byte("00000000-0000-0000-0000-000000000000"))}
				case "invalid_uuid":
					attrs = [][]byte{ldapIndependentAttribute("entryUUID", []byte("G1b2c3D4-1234-5678-9AbC-1234567890aB"))}
				case "zero_guid":
					attrs = [][]byte{ldapIndependentAttribute("objectGUID", make([]byte, 16))}
				case "invalid_guid_shape":
					attrs = [][]byte{ldapIndependentAttribute("objectGUID", make([]byte, 15))}
				}
				if err := ldapIndependentCompleteSearch(c, id, ldapIndependentDN, attrs...); err != nil {
					return err
				}
				if !post {
					return ldapIndependentEOF(c)
				}
				if err := ldapIndependentBind(c, ldapIndependentDN, "private-user-password", 0); err != nil {
					return err
				}
				userBound.Store(true)
				id, err = ldapIndependentSearchMode(c, "", attribute, true)
				if err != nil {
					return err
				}
				rechecked.Store(true)
				if mode == "post_acl" {
					if err := ldapIndependentWrite(c, id, 0x65, ldapIndependentResult(50)); err != nil {
						return err
					}
					return ldapIndependentEOF(c)
				}
				dn := ldapIndependentDN
				attrs = [][]byte{ldapIndependentAttribute(string(attribute), subject)}
				switch mode {
				case "changed_identity":
					changed := append([]byte(nil), subject...)
					changed[0] = 'B'
					attrs = [][]byte{ldapIndependentAttribute(string(attribute), changed)}
				case "changed_dn":
					dn = "uid=other," + ldapIndependentBase
				case "missing_recheck":
					attrs = nil
				case "duplicate_recheck":
					attrs = append(attrs, attrs[0])
				}
				if err := ldapIndependentCompleteSearch(c, id, dn, attrs...); err != nil {
					return err
				}
				return ldapIndependentEOF(c)
			})
			cfg := p.config()
			cfg.IdentityAttribute = attribute
			identity, err := ldapIndependentClient(t, cfg).Authenticate(p.ctx, "member", "private-user-password")
			if !errors.Is(err, routeldap.ErrAuthentication) || identity.DN != "" || identity.Attribute != "" || len(identity.Subject) != 0 || !searched.Load() || userBound.Load() != post || rechecked.Load() != post {
				t.Error("stable identity denial or exact operation phase lost", err)
			}
			ldapIndependentSafe(t, err, "private-user-password", " service-secret ", "private-directory-message", string(subject))
			p.joinedClosed(t)
		})
	}
}
