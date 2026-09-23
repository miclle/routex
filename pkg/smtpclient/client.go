// Package smtpclient performs bounded SMTP verification and fixed test delivery.
// Peer replies, credentials, and email addresses are never returned in results.
package smtpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/pkg/upstream"
)

type Config struct {
	Host                         string
	Port                         int
	Security, Username, Password string
}
type Stage struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Code       string `json:"code,omitempty"`
}
type Result struct {
	Status     string  `json:"status"`
	Code       string  `json:"code,omitempty"`
	DurationMS int64   `json:"duration_ms"`
	Stages     []Stage `json:"stages"`
}
type Client struct {
	allowPrivate bool
	dial         func(context.Context, string, string) (net.Conn, error)
	roots        *x509.CertPool
}

func New(allowPrivate bool) *Client {
	return &Client{allowPrivate: allowPrivate, dial: upstream.NewTCPDialer(allowPrivate)}
}

var errConfig = errors.New("invalid SMTP configuration")

func Validate(config Config, allowPrivate bool) error {
	if config.Host == "" || config.Host != strings.TrimSpace(config.Host) || config.Port < 1 || config.Port > 65535 || (config.Security != "STARTTLS" && config.Security != "SSL_TLS" && config.Security != "NONE") || len(config.Username) > 320 || len(config.Password) > 4096 || strings.ContainsAny(config.Username+config.Password, "\r\n\x00") || (config.Username == "") != (config.Password == "") {
		return errConfig
	}
	scheme := "https"
	if config.Security == "NONE" {
		scheme = "http"
		if config.Username != "" {
			return errConfig
		}
	}
	_, err := upstream.ValidateBaseURL(scheme+"://"+net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), allowPrivate)
	if err != nil {
		return errConfig
	}
	return nil
}

// Check verifies the connection, required TLS, authentication, and NOOP only.
// It never issues MAIL FROM or sends a message.
func (c *Client) Check(ctx context.Context, config Config) Result { return c.run(ctx, config, nil) }

// SendTest sends exactly one fixed plaintext message to one explicit recipient.
func (c *Client) SendTest(ctx context.Context, config Config, message Message) Result {
	return c.run(ctx, config, &message)
}

type boundedConn struct {
	net.Conn
	reader io.Reader
}

func (c *boundedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *Client) run(ctx context.Context, config Config, message *Message) (result Result) {
	started := time.Now()
	result = Result{Status: "failed", Stages: []Stage{}}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	if c == nil || Validate(config, c.allowPrivate) != nil || (message != nil && message.Validate() != nil) {
		result.Code = "invalid_configuration"
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stage := func(name string, fn func() error) bool {
		start := time.Now()
		err := fn()
		item := Stage{Name: name, Status: "passed", DurationMS: time.Since(start).Milliseconds()}
		if err != nil {
			item.Status = "failed"
			item.Code = name + "_failed"
			var timeout net.Error
			if ctx.Err() != nil || (errors.As(err, &timeout) && timeout.Timeout()) {
				item.Code = "timed_out"
				if errors.Is(ctx.Err(), context.Canceled) {
					item.Code = "canceled"
				}
			}
			result.Code = item.Code
		}
		result.Stages = append(result.Stages, item)
		return err == nil
	}
	var raw net.Conn
	if !stage("connect", func() error {
		var err error
		raw, err = c.dial(ctx, "tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port)))
		return err
	}) {
		return
	}
	defer func() { _ = raw.Close() }()
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		result.Code = "connect_failed"
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	// A server cannot grow greeting/EHLO/reply buffers without a fixed bound.
	var conn net.Conn = &boundedConn{Conn: raw, reader: io.LimitReader(raw, 64<<10)}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host, RootCAs: c.roots}
	if config.Security == "SSL_TLS" {
		secured := tls.Client(conn, tlsConfig)
		if !stage("tls", func() error { return secured.HandshakeContext(ctx) }) {
			return
		}
		conn = secured
	}
	var client *smtp.Client
	if !stage("greeting", func() error { var err error; client, err = smtp.NewClient(conn, config.Host); return err }) {
		return
	}
	defer func() { _ = client.Close() }()
	if config.Security == "STARTTLS" {
		if !stage("tls", func() error {
			supported, _ := client.Extension("STARTTLS")
			if !supported {
				return errConfig
			}
			return client.StartTLS(tlsConfig)
		}) {
			return
		}
	}
	if config.Username != "" {
		if !stage("auth", func() error { return client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)) }) {
			return
		}
	}
	if message == nil {
		if stage("noop", client.Noop) {
			result.Status = "verified"
		}
		return
	}
	if !stage("sender", func() error { return client.Mail(message.From) }) {
		return
	}
	if !stage("recipient", func() error { return client.Rcpt(message.To) }) {
		return
	}
	var writer io.WriteCloser
	if !stage("data", func() error { var err error; writer, err = client.Data(); return err }) {
		return
	}
	// Once DATA content can have reached the server, failure is ambiguous. Never
	// resend automatically merely because the final acceptance reply was lost.
	result.Status = "unknown"
	if !stage("acceptance", func() error {
		if _, err := io.WriteString(writer, message.text()); err != nil {
			return err
		}
		return writer.Close()
	}) {
		return
	}
	result.Status = "accepted"
	// DATA acceptance is the delivery boundary. QUIT failure cannot undo it.
	_ = client.Quit()
	return
}

func (Config) String() string { return "SMTP configuration (redacted)" }
