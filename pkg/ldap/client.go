package ldap

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	directory "github.com/go-ldap/ldap/v3"
)

var (
	ErrInvalidConfig  = errors.New("ldap: invalid configuration")
	ErrInvalidInput   = errors.New("ldap: invalid input")
	ErrAuthentication = errors.New("ldap: authentication rejected")
	ErrUnavailable    = errors.New("ldap: unavailable")
)

const operationLimit = 10 * time.Second
const cleanupLimit = time.Second

type IdentityAttribute string

const (
	IdentityAttributeEntryUUID  IdentityAttribute = "entryUUID"
	IdentityAttributeObjectGUID IdentityAttribute = "objectGUID"
)

type Config struct {
	Endpoint          string
	BindDN            string
	BindPassword      string
	BaseDN            string
	UserFilter        string
	IdentityAttribute IdentityAttribute
	TLSConfig         *tls.Config
	DialContext       func(context.Context, string, string) (net.Conn, error)
	EndpointPolicy    func(context.Context, *url.URL) error
}

// Identity is a rechecked directory attribute proof, not an application binding.
// DN is transient; Subject is an owned exact byte copy without normalization.
type Identity struct {
	DN        string
	Attribute IdentityAttribute
	Subject   []byte
}

type Client struct {
	endpoint          url.URL
	address           string
	bindDN            string
	bindPassword      string
	baseDN            string
	base              *directory.DN
	filter            string
	identityAttribute IdentityAttribute
	tlsConfig         *tls.Config
	dial              func(context.Context, string, string) (net.Conn, error)
	policy            func(context.Context, *url.URL) error
}

func New(config Config) (*Client, error) {
	if config.DialContext == nil || config.EndpointPolicy == nil || len(config.Endpoint) > 2048 || (config.IdentityAttribute != IdentityAttributeEntryUUID && config.IdentityAttribute != IdentityAttributeObjectGUID) {
		return nil, ErrInvalidConfig
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "ldaps" || endpoint.Opaque != "" || endpoint.User != nil || endpoint.Host == "" || endpoint.Path != "" || endpoint.RawPath != "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.RawFragment != "" || strings.ContainsAny(config.Endpoint, "\\ \t\r\n") {
		return nil, ErrInvalidConfig
	}
	host := endpoint.Hostname()
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil || port < 1 || port > 65535 || host == "" || strings.Contains(host, "%") {
		return nil, ErrInvalidConfig
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	if endpoint.Host != address {
		return nil, ErrInvalidConfig
	}
	if !text(config.BindDN, 2048) || !text(config.BaseDN, 2048) || len(config.BindPassword) == 0 || len(config.BindPassword) > 4096 || !text(config.UserFilter, 4096) || strings.Count(config.UserFilter, "{username}") != 1 {
		return nil, ErrInvalidConfig
	}
	if _, err := directory.ParseDN(config.BindDN); err != nil {
		return nil, ErrInvalidConfig
	}
	base, err := directory.ParseDN(config.BaseDN)
	if err != nil || len(base.RDNs) == 0 {
		return nil, ErrInvalidConfig
	}
	slot := strings.Index(config.UserFilter, "{username}")
	if strings.LastIndex(config.UserFilter[:slot], "=") < strings.LastIndex(config.UserFilter[:slot], "(") {
		return nil, ErrInvalidConfig
	}
	filter := strings.Replace(config.UserFilter, "{username}", directory.EscapeFilter("validation"), 1)
	if !validFilter(filter) {
		return nil, ErrInvalidConfig
	}
	tc := &tls.Config{}
	if config.TLSConfig != nil {
		tc = config.TLSConfig.Clone()
	}
	if tc.InsecureSkipVerify {
		return nil, ErrInvalidConfig
	}
	if tc.MinVersion < tls.VersionTLS12 {
		tc.MinVersion = tls.VersionTLS12
	}
	if tc.MaxVersion != 0 && tc.MaxVersion < tc.MinVersion {
		return nil, ErrInvalidConfig
	}
	tc.ServerName = host
	if tc.RootCAs != nil {
		tc.RootCAs = tc.RootCAs.Clone()
	}
	// Do not retain a resumable session cache shared with the caller.
	tc.ClientSessionCache = nil
	tc.Renegotiation = tls.RenegotiateNever
	return &Client{endpoint: *endpoint, address: address, bindDN: config.BindDN, bindPassword: config.BindPassword, baseDN: config.BaseDN, base: base, filter: config.UserFilter, identityAttribute: config.IdentityAttribute, tlsConfig: tc, dial: config.DialContext, policy: config.EndpointPolicy}, nil
}

func text(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validFilter(value string) bool {
	if len(value) > 8192 {
		return false
	}
	depth := 0
	for _, b := range []byte(value) {
		if b == '(' {
			depth++
			if depth > depthLimit {
				return false
			}
		}
		if b == ')' {
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	if depth != 0 {
		return false
	}
	packet, err := directory.CompileFilter(value)
	if err != nil {
		return false
	}
	return validBER(packet.Bytes())
}

// Authenticate makes no unauthenticated bind, referral, retry, or fallback.
func (c *Client) Authenticate(parent context.Context, username, password string) (identity Identity, resultErr error) {
	if parent == nil || !text(username, 256) || len(password) == 0 || len(password) > 4096 {
		return Identity{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(parent, operationLimit)
	defer cancel()
	endpoint := c.endpoint
	if ctx.Err() != nil || c.policy(ctx, &endpoint) != nil || ctx.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	raw, err := c.dial(ctx, "tcp", c.address)
	if err != nil {
		if raw != nil {
			_ = raw.Close()
		}
		return Identity{}, ErrUnavailable
	}
	if raw == nil {
		return Identity{}, ErrUnavailable
	}
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		_ = raw.Close()
		return Identity{}, ErrUnavailable
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = raw.Close(); close(closed) })
	var connection *directory.Conn
	defer func() {
		end := time.Now().Add(cleanupLimit)
		// There is no per-request timer worker: raw deadline/cancellation owns the
		// operation. SetTimeout is enabled only for the dependency's final Close wait.
		if connection != nil {
			connection.SetTimeout(cleanupLimit)
		}
		closeErr := raw.Close()
		if !stop() {
			<-closed
		}
		var dependencyErr error
		if connection != nil {
			dependencyErr = connection.Close()
		}
		if closeErr != nil && !errors.Is(closeErr, net.ErrClosed) || dependencyErr != nil && !errors.Is(dependencyErr, net.ErrClosed) || !time.Now().Before(end) || ctx.Err() != nil {
			identity = Identity{}
			resultErr = ErrUnavailable
		}
	}()
	secure := tls.Client(&wireConn{Conn: raw}, c.tlsConfig.Clone())
	if err := secure.HandshakeContext(ctx); err != nil || ctx.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	guarded := &boundedConn{Conn: secure}
	connection = directory.NewConn(guarded, true)
	connection.SetTimeout(0)
	connection.Start()
	if err := connection.Bind(c.bindDN, c.bindPassword); err != nil {
		return Identity{}, classify(err)
	}
	if ctx.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	filter := strings.Replace(c.filter, "{username}", directory.EscapeFilter(username), 1)
	if !validFilter(filter) {
		return Identity{}, ErrInvalidInput
	}
	reviewed, err := c.searchIdentity(connection, deadline, c.baseDN, directory.ScopeWholeSubtree, filter)
	if err != nil {
		return Identity{}, err
	}
	if ctx.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	if err := connection.Bind(reviewed.DN, password); err != nil {
		return Identity{}, classify(err)
	}
	if ctx.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	current, err := c.searchIdentity(connection, deadline, reviewed.DN, directory.ScopeBaseObject, "(objectClass=*)")
	if err != nil {
		return Identity{}, err
	}
	if ctx.Err() != nil {
		return Identity{}, ErrUnavailable
	}
	if current.DN != reviewed.DN || current.Attribute != reviewed.Attribute || !bytes.Equal(current.Subject, reviewed.Subject) {
		return Identity{}, ErrAuthentication
	}
	return Identity{DN: reviewed.DN, Attribute: reviewed.Attribute, Subject: append([]byte(nil), reviewed.Subject...)}, nil
}

func (c *Client) searchIdentity(connection *directory.Conn, deadline time.Time, base string, scope int, filter string) (Identity, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return Identity{}, ErrUnavailable
	}
	seconds := int(remaining.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	request := directory.NewSearchRequest(base, scope, directory.NeverDerefAliases, 2, seconds, false, filter, []string{string(c.identityAttribute)}, nil)
	request.EnforceSizeLimit = true
	search, err := connection.Search(request)
	if err != nil {
		if directory.IsErrorAnyOf(err, directory.LDAPResultInsufficientAccessRights) {
			return Identity{}, ErrAuthentication
		}
		return Identity{}, classify(err)
	}
	if search == nil || len(search.Entries) != 1 || len(search.Referrals) != 0 || len(search.Controls) != 0 {
		return Identity{}, ErrAuthentication
	}
	entry := search.Entries[0]
	if entry == nil || !text(entry.DN, 2048) {
		return Identity{}, ErrAuthentication
	}
	dn, err := directory.ParseDN(entry.DN)
	if err != nil || !c.base.Equal(dn) && !c.base.AncestorOf(dn) {
		return Identity{}, ErrAuthentication
	}
	subject, ok := stableSubject(entry, c.identityAttribute)
	if !ok {
		return Identity{}, ErrAuthentication
	}
	return Identity{DN: entry.DN, Attribute: c.identityAttribute, Subject: subject}, nil
}

func stableSubject(entry *directory.Entry, attribute IdentityAttribute) ([]byte, bool) {
	if entry == nil || len(entry.Attributes) != 1 {
		return nil, false
	}
	recorded := entry.Attributes[0]
	if recorded == nil || !attributeNameEqual(recorded.Name, attribute) || len(recorded.Values) != 1 || len(recorded.ByteValues) != 1 {
		return nil, false
	}
	value := recorded.ByteValues[0]
	switch attribute {
	case IdentityAttributeEntryUUID:
		if len(value) != 36 {
			return nil, false
		}
		nonzero := false
		for i, b := range value {
			if i == 8 || i == 13 || i == 18 || i == 23 {
				if b != '-' {
					return nil, false
				}
				continue
			}
			if (b < '0' || b > '9') && (b < 'a' || b > 'f') && (b < 'A' || b > 'F') {
				return nil, false
			}
			if b != '0' {
				nonzero = true
			}
		}
		if !nonzero {
			return nil, false
		}
	case IdentityAttributeObjectGUID:
		if len(value) != 16 {
			return nil, false
		}
		nonzero := false
		for _, b := range value {
			if b != 0 {
				nonzero = true
			}
		}
		if !nonzero {
			return nil, false
		}
	default:
		return nil, false
	}
	return append([]byte(nil), value...), true
}

func attributeNameEqual(name string, attribute IdentityAttribute) bool {
	expected := string(attribute)
	if len(name) != len(expected) {
		return false
	}
	for i := 0; i < len(name); i++ {
		a, b := name[i], expected[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func classify(err error) error {
	if directory.IsErrorAnyOf(err, directory.LDAPResultInvalidCredentials, directory.LDAPResultNoSuchObject, directory.LDAPResultInappropriateAuthentication) {
		return ErrAuthentication
	}
	return ErrUnavailable
}
