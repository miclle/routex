package smtpclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type smtpFixture struct {
	host     string
	port     int
	roots    *x509.CertPool
	messages atomic.Int32
	authSeen atomic.Bool
	body     atomic.Value
}

func fixture(t *testing.T, security, failure string) *smtpFixture {
	t.Helper()
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certificate := certServer.TLS.Certificates[0]
	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	certServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	f := &smtpFixture{host: host, port: port, roots: x509.NewCertPool()}
	f.roots.AddCert(parsed)
	f.body.Store("")
	var wg sync.WaitGroup
	var connections sync.Map
	wg.Go(func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(raw, true)
			wg.Go(func() {
				defer connections.Delete(raw)
				defer func() { _ = raw.Close() }()
				_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
				conn := raw
				if security == "SSL_TLS" {
					secured := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
					if secured.Handshake() != nil {
						return
					}
					conn = secured
				}
				write := func(value string) { _, _ = io.WriteString(conn, value+"\r\n") }
				if failure == "stall" {
					var value [1]byte
					_, _ = raw.Read(value[:])
					return
				}
				if failure == "greeting" {
					write("554 sensitive greeting")
					return
				}
				write("220 test SMTP")
				reader := textproto.NewReader(bufio.NewReader(conn))
				for {
					line, err := reader.ReadLine()
					if err != nil {
						return
					}
					command := strings.SplitN(line, " ", 2)[0]
					switch command {
					case "EHLO":
						write("250-test")
						if security == "STARTTLS" && failure != "tls" {
							write("250-STARTTLS")
						}
						write("250 AUTH PLAIN")
					case "HELO":
						write("250 test")
					case "STARTTLS":
						write("220 begin TLS")
						secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
						if secured.Handshake() != nil {
							return
						}
						conn = secured
						reader = textproto.NewReader(bufio.NewReader(conn))
					case "AUTH":
						f.authSeen.Store(true)
						if failure == "auth" {
							write("535 private authentication detail")
						} else {
							write("235 authenticated")
						}
					case "NOOP":
						if failure == "noop" {
							write("500 private noop detail")
						} else {
							write("250 ok")
						}
					case "MAIL":
						if failure == "sender" {
							write("550 private sender detail")
						} else {
							write("250 ok")
						}
					case "RCPT":
						if failure == "recipient" {
							write("550 private recipient detail")
						} else {
							write("250 ok")
						}
					case "DATA":
						if failure == "data" {
							write("554 private data detail")
							continue
						}
						write("354 send data")
						body, err := reader.ReadDotBytes()
						if err != nil {
							return
						}
						f.body.Store(string(body))
						f.messages.Add(1)
						if failure == "acceptance" {
							return
						}
						write("250 accepted")
					case "QUIT":
						write("221 bye")
						return
					default:
						write("500 unsupported")
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		connections.Range(func(k, v any) bool { _ = k.(net.Conn).Close(); return true })
		wg.Wait()
	})
	return f
}
func (f *smtpFixture) config(security string) Config {
	return Config{Host: f.host, Port: f.port, Security: security}
}
func testMessage() Message {
	return Message{From: "sender@example.invalid", Name: "RouteX Test", ReplyTo: "reply@example.invalid", To: "recipient@example.invalid", RequestID: "smtp_test_request_001"}
}
func TestSMTPRealTLSAndFixedDelivery(t *testing.T) {
	for _, security := range []string{"STARTTLS", "SSL_TLS", "NONE"} {
		t.Run(security, func(t *testing.T) {
			f := fixture(t, security, "")
			client := New(true)
			client.roots = f.roots
			config := f.config(security)
			if security != "NONE" {
				config.Username, config.Password = "smtp-user", "test-only-password"
			}
			checked := client.Check(context.Background(), config)
			if checked.Status != "verified" || f.messages.Load() != 0 {
				t.Fatalf("verification sent mail or failed: %+v", checked)
			}
			sent := client.SendTest(context.Background(), config, testMessage())
			if sent.Status != "accepted" || f.messages.Load() != 1 {
				t.Fatalf("delivery was not accepted: %+v", sent)
			}
			body := f.body.Load().(string)
			if !strings.Contains(body, "Subject: RouteX SMTP test") || !strings.Contains(body, "Reply-To: reply@example.invalid") || strings.Contains(body, "test-only-password") {
				t.Fatal("fixed test message missing or secret leaked")
			}
			if (security != "NONE") != f.authSeen.Load() {
				t.Fatal("unexpected authentication boundary")
			}
		})
	}
}
func TestSMTPStageFailuresAndUnknownAcceptance(t *testing.T) {
	for _, stage := range []string{"greeting", "tls", "auth", "sender", "recipient", "data", "acceptance"} {
		t.Run(stage, func(t *testing.T) {
			f := fixture(t, "STARTTLS", stage)
			client := New(true)
			client.roots = f.roots
			config := f.config("STARTTLS")
			config.Username, config.Password = "user", "password"
			result := client.SendTest(context.Background(), config, testMessage())
			want := "failed"
			if stage == "acceptance" {
				want = "unknown"
			}
			if result.Status != want || result.Code != stage+"_failed" {
				t.Fatalf("incorrect measured failure: %+v", result)
			}
			if len(result.Stages) == 0 || result.Stages[len(result.Stages)-1].Name != stage || result.Stages[len(result.Stages)-1].Status != "failed" {
				t.Fatal("stage sequence fabricated")
			}
		})
	}
}
func TestSMTPTrustCancellationAndAddressPolicy(t *testing.T) {
	f := fixture(t, "SSL_TLS", "")
	config := f.config("SSL_TLS")
	result := New(true).Check(context.Background(), config)
	if result.Status != "failed" || result.Code != "tls_failed" {
		t.Fatal("untrusted TLS certificate accepted")
	}
	if result := New(false).Check(context.Background(), config); result.Code != "invalid_configuration" {
		t.Fatal("private endpoint allowed by default")
	}
	stalled := fixture(t, "NONE", "stall")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result = New(true).Check(ctx, stalled.config("NONE"))
	if result.Code != "timed_out" || result.Status != "failed" {
		t.Fatal("deadline did not stop greeting")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	result = New(true).Check(ctx, stalled.config("NONE"))
	if result.Code != "canceled" {
		t.Fatal("canceled request connected")
	}
	for _, config := range []Config{
		{Host: "smtp.example.com", Port: 25, Security: "NONE"},
		{Host: "127.0.0.1", Port: 25, Security: "NONE", Username: "user", Password: "secret"},
		{Host: "169.254.169.254", Port: 25, Security: "STARTTLS"},
		{Host: "::1", Port: 0, Security: "SSL_TLS"},
		{Host: "127.0.0.1", Port: 25, Security: "SSL_TLS", Username: "x\r\nMAIL", Password: "secret"},
	} {
		if Validate(config, true) == nil {
			t.Fatal("unsafe SMTP configuration accepted")
		}
	}
}
func TestSMTPHeaderAndRecipientValidation(t *testing.T) {
	for _, value := range []string{"a@example.invalid,b@example.invalid", "Name <a@example.invalid>", "a@example.invalid\r\nBcc: b@example.invalid", "用户@example.invalid"} {
		if ValidAddress(value) {
			t.Fatal("non-single or unsafe address accepted")
		}
	}
	message := testMessage()
	message.Name = "name\r\nBcc: hidden@example.invalid"
	if message.Validate() == nil {
		t.Fatal("sender header injection accepted")
	}
	message = testMessage()
	message.RequestID = "injected>\r\n"
	if message.Validate() == nil {
		t.Fatal("message identity injection accepted")
	}
}

func TestSMTPConnectAndNoopFailuresAreMeasured(t *testing.T) {
	client := New(true)
	client.dial = func(context.Context, string, string) (net.Conn, error) { return nil, io.EOF }
	result := client.Check(context.Background(), Config{Host: "127.0.0.1", Port: 25, Security: "NONE"})
	if result.Code != "connect_failed" || len(result.Stages) != 1 || result.Stages[0].Name != "connect" {
		t.Fatal("connection failure stage missing")
	}
	f := fixture(t, "NONE", "noop")
	result = New(true).Check(context.Background(), f.config("NONE"))
	if result.Code != "noop_failed" || result.Status != "failed" || f.messages.Load() != 0 {
		t.Fatal("NOOP verification was not independent of delivery")
	}
}
