package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func smtpDeliveryFixture(t *testing.T) (string, int, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var messages atomic.Int32
	var wg sync.WaitGroup
	var connections sync.Map
	wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(conn, true)
			wg.Go(func() {
				defer connections.Delete(conn)
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				reader := textproto.NewReader(bufio.NewReader(conn))
				_, _ = io.WriteString(conn, "220 controlled SMTP\r\n")
				for {
					line, err := reader.ReadLine()
					if err != nil {
						return
					}
					switch strings.SplitN(line, " ", 2)[0] {
					case "EHLO", "HELO", "NOOP", "MAIL", "RCPT":
						_, _ = io.WriteString(conn, "250 ok\r\n")
					case "DATA":
						_, _ = io.WriteString(conn, "354 send data\r\n")
						body, err := reader.ReadDotBytes()
						if err != nil {
							return
						}
						if !strings.Contains(string(body), "Subject: RouteX SMTP test") || strings.Contains(string(body), "test-only-smtp-password") {
							t.Error("unsafe test message")
						}
						messages.Add(1)
						_, _ = io.WriteString(conn, "250 accepted\r\n")
					case "QUIT":
						_, _ = io.WriteString(conn, "221 bye\r\n")
						return
					default:
						_, _ = io.WriteString(conn, "500 unsupported\r\n")
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
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return host, port, &messages
}
func testSMTPLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	host, port, messages := smtpDeliveryFixture(t)
	store, err := secretstore.New(bytes.Repeat([]byte{32}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithSMTPPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"smtp-admin@example.invalid","password":"test-only-admin-password","name":"SMTP Admin"}`, nil, "")
	expectStatus(t, setup, 201)
	auth, cookie := readIdentity(t, setup)
	request := func(method, path string, input any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(input)
		return identityRequest(router, method, path, string(encoded), cookie, auth.CSRFToken)
	}
	defaults := decodeCatalogResponse[service.SMTPView](t, request("GET", "/api/v1/admin/smtp", nil), 200)
	if defaults.Enabled || defaults.AuthConfigured || defaults.ETag != "0" {
		t.Fatal("unsafe SMTP bootstrap defaults")
	}
	expectStatus(t, request("PUT", "/api/v1/admin/smtp", map[string]any{"unknown": true}), 400)
	expectStatus(t, identityRequest(router, "PUT", "/api/v1/admin/smtp", `{}`, cookie, ""), 403)
	// Disabled configuration can preserve encrypted credentials without connecting.
	input := service.SMTPInput{Host: host, Port: port, Security: "STARTTLS", ETag: defaults.ETag, Auth: service.SMTPAuthInput{Action: "replace", Username: "smtp-user", Password: "test-only-smtp-password"}}
	secured := decodeCatalogResponse[service.SMTPView](t, request("PUT", "/api/v1/admin/smtp", input), 200)
	if !secured.AuthConfigured {
		t.Fatal("missing credential state")
	}
	var stored entity.SMTPSetting
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatal(err)
	}
	if stored.AuthCiphertext == "" || strings.Contains(stored.AuthCiphertext, "test-only-smtp-password") {
		t.Fatal("SMTP secret stored in plaintext")
	}
	input.ETag = secured.ETag
	input.Host = "other.example.com"
	input.Auth = service.SMTPAuthInput{Action: "keep"}
	expectStatus(t, request("PUT", "/api/v1/admin/smtp", input), 400)
	// Local plaintext transport never carries authentication. Explicit removal is required.
	input.Host = host
	input.Security = "NONE"
	input.Enabled = true
	input.Auth = service.SMTPAuthInput{Action: "remove"}
	enabled := decodeCatalogResponse[service.SMTPView](t, request("PUT", "/api/v1/admin/smtp", input), 200)
	if messages.Load() != 0 {
		t.Fatal("configuration verification sent a message")
	}
	sender := decodeCatalogResponse[service.SMTPView](t, request("PUT", "/api/v1/admin/smtp/sender", service.SMTPSenderInput{SenderName: "RouteX", SenderEmail: "sender@example.invalid", ReplyTo: "reply@example.invalid", ETag: enabled.ETag}), 200)
	test := service.SMTPTestInput{ETag: sender.ETag, Recipient: "recipient@example.invalid", RequestID: "smtp_test_request_0001"}
	result := decodeCatalogResponse[service.SMTPTestView](t, request("POST", "/api/v1/admin/smtp/test", test), 200)
	if result.Status != "accepted" || messages.Load() != 1 || result.CompletedAt == nil {
		t.Fatal("real SMTP DATA acceptance not recorded")
	}
	repeat := decodeCatalogResponse[service.SMTPTestView](t, request("POST", "/api/v1/admin/smtp/test", test), 200)
	if repeat.Status != "accepted" || messages.Load() != 1 {
		t.Fatal("idempotent retry delivered twice")
	}
	changed := test
	changed.Recipient = "other@example.invalid"
	expectStatus(t, request("POST", "/api/v1/admin/smtp/test", changed), 409)
	changed = test
	changed.RequestID = "smtp_test_request_0002"
	expectStatus(t, request("POST", "/api/v1/admin/smtp/test", changed), 429)
	// Lost/ambiguous receipts never cause another delivery after reopening service.
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithSMTPPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.TestSMTP(ctx, auth.User.ID, test); err != nil || got.Status != "accepted" || messages.Load() != 1 {
		t.Fatal("restart broke receipt deduplication")
	}
	if err := db.Model(&entity.SMTPTest{}).Where("request_id = ?", test.RequestID).Updates(map[string]any{"status": "pending", "result_json": "", "started_at": time.Now().Add(-time.Minute), "completed_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	unknown, err := restarted.TestSMTP(ctx, auth.User.ID, test)
	if err != nil || unknown.Status != "unknown" || messages.Load() != 1 {
		t.Fatal("interrupted delivery was replayed")
	}
	// Verification failure preserves the previous valid configuration and revision.
	input.ETag = sender.ETag
	input.Security = "STARTTLS"
	input.Auth = service.SMTPAuthInput{Action: "keep"}
	expectStatus(t, request("PUT", "/api/v1/admin/smtp", input), 422)
	unchanged := decodeCatalogResponse[service.SMTPView](t, request("GET", "/api/v1/admin/smtp", nil), 200)
	if unchanged.ETag != sender.ETag || unchanged.Security != "NONE" {
		t.Fatal("failed verification replaced last valid configuration")
	}
	input.Security = "NONE"
	input.Enabled = false
	disabled := decodeCatalogResponse[service.SMTPView](t, request("PUT", "/api/v1/admin/smtp", input), 200)
	changed.ETag = disabled.ETag
	expectStatus(t, request("POST", "/api/v1/admin/smtp/test", changed), 409)
	if messages.Load() != 1 {
		t.Fatal("disabled SMTP dispatched a new message")
	}
	member, err := svc.CreateMember(ctx, auth.User.ID, "smtp-member@example.invalid", "test-only-member-password", "Member", "member")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TestSMTP(ctx, member.User.ID, test); err == nil {
		t.Fatal("unprivileged member replayed another actor's receipt")
	}
	var receipt entity.SMTPTest
	if err := db.First(&receipt, "request_id = ?", test.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(receipt)
	if strings.Contains(string(encoded), "recipient@example.invalid") || strings.Contains(string(encoded), "test-only-smtp-password") {
		t.Fatal("recipient or credentials leaked into retained facts")
	}
}
