package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
)

func TestSMTPEncryptedCredentialsCannotFollowEndpointChanges(t *testing.T) {
	store, err := secretstore.New(bytes.Repeat([]byte{31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{secrets: store}
	input := SMTPInput{Host: "smtp.example.com", Port: 587, Security: "STARTTLS", ETag: "0", Auth: SMTPAuthInput{Action: "replace", Username: "account@example.invalid", Password: "test-only-smtp-password"}}
	row, err := svc.prepareSMTP(entity.SMTPSetting{ID: 1}, input)
	if err != nil {
		t.Fatal(err)
	}
	if row.AuthCiphertext == "" || strings.Contains(row.AuthCiphertext, input.Auth.Password) || strings.Contains(row.AuthCiphertext, input.Auth.Username) {
		t.Fatal("SMTP credentials not encrypted")
	}
	readback, _ := json.Marshal(smtpView(row))
	if strings.Contains(string(readback), "test-only-smtp-password") || strings.Contains(string(readback), "account@example.invalid") {
		t.Fatal("credential readback leaked")
	}
	wrong := row
	wrong.SecretGeneration = "sec_wrong"
	if _, err := svc.smtpConfig(wrong); err == nil {
		t.Fatal("ciphertext moved between secret generations")
	}
	input.Auth = SMTPAuthInput{Action: "keep"}
	for _, change := range []func(*SMTPInput){func(v *SMTPInput) { v.Host = "other.example.com" }, func(v *SMTPInput) { v.Port = 465 }, func(v *SMTPInput) { v.Security = "SSL_TLS" }} {
		next := input
		change(&next)
		if _, err := svc.prepareSMTP(row, next); err == nil {
			t.Fatal("stored credentials were reused for a different endpoint")
		}
	}
	svc.secrets = nil
	preserved, err := svc.prepareSMTP(row, input)
	if err != nil || preserved.AuthCiphertext != row.AuthCiphertext {
		t.Fatal("safe disable/keep required decryption")
	}
	input.Host = "other.example.com"
	input.Auth = SMTPAuthInput{Action: "remove"}
	cleared, err := svc.prepareSMTP(row, input)
	if err != nil || cleared.AuthCiphertext != "" || cleared.SecretGeneration != "" {
		t.Fatal("explicit authentication removal failed")
	}
}
func TestSMTPInterruptedTestDoesNotBecomeRetryable(t *testing.T) {
	row := entity.SMTPTest{RequestID: "smtp_test_request_001", ConfigETag: "rev_old", Status: "pending", StartedAt: time.Now().Add(-time.Minute)}
	result := smtpTestView(row)
	if result.Status != "unknown" || result.Code != "interrupted" {
		t.Fatal("interrupted submission reported a definitive failure")
	}
	row.StartedAt = time.Now()
	if smtpTestView(row).Status != "pending" {
		t.Fatal("active test reported interrupted")
	}
}
