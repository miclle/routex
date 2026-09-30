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
						text := string(body)
						if (!strings.Contains(text, "Subject: RouteX SMTP test") && !strings.Contains(text, "Subject: RouteX operational alert:")) || strings.Contains(text, "test-only-smtp-password") {
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

func smtpUnknownAcceptanceFixture(t *testing.T) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		defer func() { _ = listener.Close() }()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
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
			case "EHLO", "HELO", "MAIL", "RCPT":
				_, _ = io.WriteString(conn, "250 ok\r\n")
			case "DATA":
				_, _ = io.WriteString(conn, "354 send data\r\n")
				_, _ = reader.ReadDotBytes()
				return
			default:
				_, _ = io.WriteString(conn, "500 unsupported\r\n")
			}
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return host, port
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

	// Operational delivery executes the same guarded SMTP client with durable
	// intents, exact configuration revisions and explicit terminal outcomes.
	now := time.Now().UTC()
	const deliveryETag = "rev_notification_delivery"
	if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{
		"enabled": true, "host": host, "port": port, "security": "NONE", "auth_ciphertext": "", "secret_generation": "",
		"sender_name": "RouteX", "sender_email": "sender@example.invalid", "e_tag": deliveryETag, "updated_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.NotificationSetting{UserID: auth.User.ID, ExternalEmail: "alerts@example.invalid", EmailHigh: true, EmailMedium: true, ETag: "rev_delivery_settings", UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	completed := now.Add(-time.Minute)
	if err := db.Create(&entity.SystemJob{
		ID: "job_ntf_delivery_accept", Code: service.SystemJobRuntimePublication, Status: "failed", ItemsTotal: 1,
		DetailCode: "publication_failed", StartedAt: completed, UpdatedAt: completed, CompletedAt: &completed,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := restarted.FlushNotificationDelivery(ctx, 10); err != nil || count != 1 {
		t.Fatalf("flush accepted delivery: count=%d err=%v", count, err)
	}
	var accepted entity.NotificationDeliveryIntent
	if err := db.First(&accepted, "recipient_id = ? AND status = ?", auth.User.ID, "accepted").Error; err != nil || accepted.Attempts != 1 || messages.Load() != 2 {
		t.Fatalf("application delivery was not accepted exactly once: %+v messages=%d err=%v", accepted, messages.Load(), err)
	}

	directAlert := entity.OperationalAlert{
		ID: "alr_ntf_delivery_states", GroupKey: "test:notification-delivery-states", Kind: "system_job_failure",
		Severity: "high", DetailCode: "persistence_failed", State: "open", OccurrenceCount: 4,
		FirstSeenAt: now, LastSeenAt: now, ETag: "rev_ntf_delivery_states", UpdatedAt: now,
	}
	if err := db.Create(&directAlert).Error; err != nil {
		t.Fatal(err)
	}
	createIntent := func(idValue, statusValue, smtpETag string, leaseUntil *time.Time) {
		t.Helper()
		occurrenceID := "occ_" + idValue
		if err := db.Create(&entity.OperationalAlertOccurrence{
			ID: occurrenceID, AlertID: directAlert.ID, SourceType: "system_job", SourceID: "job_" + idValue,
			DetailCode: directAlert.DetailCode, OccurredAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.NotificationDeliveryIntent{
			ID: idValue, OccurrenceID: occurrenceID, RecipientID: auth.User.ID, RecipientEmail: "alerts@example.invalid",
			Kind: directAlert.Kind, Severity: directAlert.Severity, DetailCode: directAlert.DetailCode, SMTPETag: smtpETag,
			Status: statusValue, NextAttemptAt: now.Add(-time.Minute), LeaseToken: "lease_" + idValue, LeaseUntil: leaseUntil,
			CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	createIntent("ndl_ntf_config", "pending", "rev_stale_smtp", nil)
	if _, err := restarted.FlushNotificationDelivery(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var configChanged entity.NotificationDeliveryIntent
	if err := db.First(&configChanged, "id = ?", "ndl_ntf_config").Error; err != nil || configChanged.Status != "failed" || configChanged.ResultCode != "configuration_changed" || messages.Load() != 2 {
		t.Fatalf("changed configuration was not terminal: %+v messages=%d err=%v", configChanged, messages.Load(), err)
	}

	expiredLease := now.Add(-time.Minute)
	createIntent("ndl_notification_lease", "sending", deliveryETag, &expiredLease)
	if _, err := restarted.FlushNotificationDelivery(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var expired entity.NotificationDeliveryIntent
	if err := db.First(&expired, "id = ?", "ndl_notification_lease").Error; err != nil || expired.Status != "unknown" || expired.ResultCode != "lease_expired" || messages.Load() != 2 {
		t.Fatalf("expired lease was replayed: %+v messages=%d err=%v", expired, messages.Load(), err)
	}

	unknownHost, unknownPort := smtpUnknownAcceptanceFixture(t)
	const unknownETag = "rev_notification_unknown"
	if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{"host": unknownHost, "port": unknownPort, "e_tag": unknownETag, "updated_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	createIntent("ndl_notification_unknown", "pending", unknownETag, nil)
	if _, err := restarted.FlushNotificationDelivery(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var unknownDelivery entity.NotificationDeliveryIntent
	if err := db.First(&unknownDelivery, "id = ?", "ndl_notification_unknown").Error; err != nil || unknownDelivery.Status != "unknown" || unknownDelivery.Attempts != 1 {
		t.Fatalf("ambiguous SMTP acceptance was not terminal unknown: %+v err=%v", unknownDelivery, err)
	}

	unused, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, unusedPortText, _ := net.SplitHostPort(unused.Addr().String())
	unusedPort, _ := strconv.Atoi(unusedPortText)
	_ = unused.Close()
	const retryETag = "rev_notification_retry"
	if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{"host": "127.0.0.1", "port": unusedPort, "e_tag": retryETag, "updated_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	createIntent("ndl_notification_retry", "pending", retryETag, nil)
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			if err := db.Model(&entity.NotificationDeliveryIntent{}).Where("id = ?", "ndl_notification_retry").Update("next_attempt_at", time.Now().Add(-time.Minute)).Error; err != nil {
				t.Fatal(err)
			}
		}
		if _, err := restarted.FlushNotificationDelivery(ctx, 1); err != nil {
			t.Fatal(err)
		}
		var retried entity.NotificationDeliveryIntent
		if err := db.First(&retried, "id = ?", "ndl_notification_retry").Error; err != nil {
			t.Fatal(err)
		}
		want := "retry"
		if attempt == 3 {
			want = "failed"
		}
		if retried.Status != want || retried.Attempts != attempt {
			t.Fatalf("retry attempt %d produced %+v", attempt, retried)
		}
	}

	createIntent("ndl_ntf_ineligible", "pending", retryETag, nil)
	if err := db.Model(&entity.NotificationDeliveryIntent{}).Where("id = ?", "ndl_ntf_ineligible").Update("recipient_id", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.FlushNotificationDelivery(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var ineligible entity.NotificationDeliveryIntent
	if err := db.First(&ineligible, "id = ?", "ndl_ntf_ineligible").Error; err != nil || ineligible.Status != "failed" || ineligible.ResultCode != "recipient_ineligible" {
		t.Fatalf("recipient permission was not rechecked: %+v err=%v", ineligible, err)
	}
}
