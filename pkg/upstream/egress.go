package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// EgressConfig contains a trusted proxy endpoint and optional proxy-only credentials.
// It must never be logged or returned by management APIs.
type egressRequestContextKey struct{}

type EgressConfig struct {
	Kind     string
	Host     string
	Port     int
	Username string
	Password string
}

var errEgress = errors.New("egress configuration is invalid")
var errProxy = errors.New("egress tunnel failed")

// ValidateEgress validates the proxy endpoint independently from target policy.
// HTTPS means HTTP CONNECT over a verified TLS connection to the proxy.
func ValidateEgress(config EgressConfig, allowPrivate bool) error {
	if (config.Kind != "https" && config.Kind != "socks5") || !validHostname(config.Host) || config.Host != strings.TrimSpace(config.Host) || config.Port < 1 || config.Port > 65535 {
		return errEgress
	}
	if _, err := ValidateBaseURL("https://"+net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), allowPrivate); err != nil {
		return errEgress
	}
	if len(config.Username) > 255 || len(config.Password) > 255 || strings.ContainsAny(config.Username, ":\r\n\x00") || strings.ContainsAny(config.Password, "\r\n\x00") || (config.Username == "") != (config.Password == "") {
		return errEgress
	}
	return nil
}

// NewEgressClient preserves target validation, redirects and TLS verification.
// Both proxy and target DNS answers are pinned to validated numeric addresses.
// Proxy credentials are used only during tunnel establishment.
func NewEgressClient(targetPrivate, proxyPrivate bool, config EgressConfig) (*http.Client, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return newEgressClient(targetPrivate, proxyPrivate, config, net.DefaultResolver.LookupNetIP, dialer.DialContext, nil, nil)
}

func newEgressClient(targetPrivate, proxyPrivate bool, config EgressConfig, lookup lookupFunc, dial dialFunc, proxyTLS *tls.Config, observe stageObserver) (*http.Client, error) {
	if err := ValidateEgress(config, proxyPrivate); err != nil {
		return nil, err
	}
	client := newClient(targetPrivate, lookup, dial)
	policy := client.Transport.(*policyTransport)
	policy.bindDialContext = true
	transport := policy.base
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		// net/http detaches cancellation while dialing to permit reuse. Tunnel
		// negotiation must still stop when the requesting caller goes away.
		if requestCtx, ok := ctx.Value(egressRequestContextKey{}).(context.Context); ok {
			bounded, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(requestCtx, cancel)
			defer func() { stop(); cancel() }()
			ctx = bounded
		}

		host, port, err := net.SplitHostPort(address)
		if err != nil || (network != "tcp" && network != "tcp4" && network != "tcp6") {
			return nil, errAddress
		}
		var targets []netip.Addr
		err = observe.run("target_dns", func() error {
			var err error
			targets, err = resolveEgressAddress(ctx, host, targetPrivate, lookup)
			return err
		})
		if err != nil {
			return nil, err
		}
		var proxyIPs []netip.Addr
		err = observe.run("proxy_dns", func() error {
			var err error
			proxyIPs, err = resolveEgressAddress(ctx, config.Host, proxyPrivate, lookup)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			conn, tunnelErr := openEgressTunnel(ctx, network, port, target, proxyIPs, config, dial, proxyTLS, observe)
			if tunnelErr == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errProxy
	}
	return client, nil
}

func openEgressTunnel(ctx context.Context, network, port string, target netip.Addr, proxyIPs []netip.Addr, config EgressConfig, dial dialFunc, proxyTLS *tls.Config, observe stageObserver) (net.Conn, error) {
	var conn net.Conn
	err := observe.run("tcp", func() error {
		for _, ip := range proxyIPs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var dialErr error
			conn, dialErr = dial(ctx, network, net.JoinHostPort(ip.String(), strconv.Itoa(config.Port)))
			if dialErr == nil {
				return nil
			}
		}
		return errProxy
	})
	if err != nil {
		return nil, err
	}
	successful := false
	defer func() {
		if !successful {
			_ = conn.Close()
		}
	}()
	// Cancellation and setup deadlines also cover a peer that stalls its handshake.
	setupDeadline := time.Now().Add(10 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(setupDeadline) {
		setupDeadline = deadline
	}
	if err := conn.SetDeadline(setupDeadline); err != nil {
		return nil, errProxy
	}
	rawConn := conn
	cancelDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close(); close(cancelDone) })
	defer func() {
		if !stop() {
			<-cancelDone
		}
	}()
	if config.Kind == "https" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host}
		if proxyTLS != nil {
			tlsConfig = proxyTLS.Clone()
			tlsConfig.ServerName = config.Host
		}
		secured := tls.Client(conn, tlsConfig)
		if err := observe.run("proxy_tls", func() error { return secured.HandshakeContext(ctx) }); err != nil {
			return nil, errProxy
		}
		conn = secured
	}
	// Numeric targets prevent proxy-side DNS resolution/rebinding. TLS of the
	// target is still performed by http.Transport using the original hostname.
	if config.Kind == "https" {
		conn, err = connectHTTPSProxy(conn, net.JoinHostPort(target.String(), port), config, observe)
	} else {
		err = connectSOCKS5(conn, target, port, config, observe)
	}
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, errProxy
	}
	successful = true
	return conn, nil
}

func resolveEgressAddress(ctx context.Context, host string, allowPrivate bool, lookup lookupFunc) ([]netip.Addr, error) {
	var addresses []netip.Addr
	if ip, literal := parseIP(host); literal {
		addresses = []netip.Addr{ip}
	} else {
		var err error
		addresses, err = lookup(ctx, "ip", host)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errAddress
		}
	}
	if len(addresses) == 0 {
		return nil, errAddress
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, ip := range addresses {
		if !allowedIP(ip, allowPrivate) || (strings.EqualFold(host, "localhost") && !ip.Unmap().IsLoopback()) {
			return nil, errAddress
		}
		result = append(result, ip.Unmap())
	}
	return result, nil
}

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedProxyConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func connectHTTPSProxy(conn net.Conn, target string, config EgressConfig, observe stageObserver) (net.Conn, error) {
	var reader *bufio.Reader
	err := observe.run("proxy_connect", func() error {
		request := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
		if config.Username != "" {
			request += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(config.Username+":"+config.Password)) + "\r\n"
		}
		if _, err := io.WriteString(conn, request+"\r\n"); err != nil {
			return errProxy
		}
		// Bound response headers without imposing a lifetime byte limit on the tunnel.
		reader = bufio.NewReader(io.LimitReader(conn, 16<<10))
		response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
		if err != nil {
			return errProxy
		}
		if config.Username != "" {
			authErr := error(nil)
			if response.StatusCode == http.StatusProxyAuthRequired {
				authErr = errProxy
			}
			if response.StatusCode == http.StatusOK || authErr != nil {
				if observe != nil {
					observe("proxy_auth", -1, authErr)
				}
			}
		}
		if response.StatusCode != http.StatusOK {
			return errProxy
		}
		return nil
	})
	if err != nil {
		return conn, err
	}
	prefix := make([]byte, reader.Buffered())
	if _, err := io.ReadFull(reader, prefix); err != nil {
		return conn, errProxy
	}
	return &bufferedProxyConn{Conn: conn, reader: bufio.NewReader(io.MultiReader(strings.NewReader(string(prefix)), conn))}, nil
}

func connectSOCKS5(conn net.Conn, target netip.Addr, port string, config EgressConfig, observe stageObserver) error {
	return observe.run("proxy_connect", func() error { return handshakeSOCKS5(conn, target, port, config, observe) })
}

func handshakeSOCKS5(conn net.Conn, target netip.Addr, port string, config EgressConfig, observe stageObserver) error {
	err := observe.run("proxy_auth", func() error {
		method := byte(0)
		if config.Username != "" {
			method = 2
		}
		if _, err := conn.Write([]byte{5, 1, method}); err != nil {
			return errProxy
		}
		response := make([]byte, 2)
		if _, err := io.ReadFull(conn, response); err != nil || response[0] != 5 || response[1] != method {
			return errProxy
		}
		if method == 2 {
			auth := append([]byte{1, byte(len(config.Username))}, []byte(config.Username)...)
			auth = append(auth, byte(len(config.Password)))
			auth = append(auth, []byte(config.Password)...)
			if _, err := conn.Write(auth); err != nil {
				return errProxy
			}
			if _, err := io.ReadFull(conn, response); err != nil || response[0] != 1 || response[1] != 0 {
				return errProxy
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return func() error {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return errProxy
		}
		atyp := byte(4)
		address := target.AsSlice()
		if target.Is4() {
			atyp = 1
		}
		request := append([]byte{5, 1, 0, atyp}, address...)
		request = append(request, byte(value>>8), byte(value))
		if _, err := conn.Write(request); err != nil {
			return errProxy
		}
		response := make([]byte, 4)
		if _, err := io.ReadFull(conn, response); err != nil || response[0] != 5 || response[1] != 0 || response[2] != 0 {
			return errProxy
		}
		size := 0
		switch response[3] {
		case 1:
			size = 4
		case 4:
			size = 16
		case 3:
			length := []byte{0}
			if _, err := io.ReadFull(conn, length); err != nil || length[0] == 0 {
				return errProxy
			}
			size = int(length[0])
		default:
			return errProxy
		}
		if _, err := io.CopyN(io.Discard, conn, int64(size+2)); err != nil {
			return errProxy
		}
		return nil
	}()
}

func (c EgressConfig) String() string {
	return fmt.Sprintf("EgressConfig{kind:%s, credentials:redacted}", c.Kind)
}
