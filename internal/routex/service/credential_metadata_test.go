package service

import (
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestCredentialMetadataValidator(t *testing.T) {
	verified := time.Date(2026, 10, 2, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	row := entity.ProviderCredential{ID: "crd_first", ConnectionID: "con_first", Name: "Original", Priority: 1, VerificationStatus: "verified", VerifiedAt: &verified, Ciphertext: "never-in-validator"}
	initial := credentialMetadataRecord(row)
	if len(initial.ETag) != 64 {
		t.Fatal("metadata validator is not a SHA256 digest")
	}
	row.Ciphertext = "different-secret"
	row.CreatedAt = time.Now()
	if credentialMetadataRecord(row).ETag != initial.ETag {
		t.Fatal("secret or creation timestamp affected metadata validator")
	}
	utc := verified.UTC()
	row.VerifiedAt = &utc
	if credentialMetadataRecord(row).ETag != initial.ETag {
		t.Fatal("equivalent verification instant affected validator")
	}
	for _, test := range []struct {
		name   string
		change func(*entity.ProviderCredential)
	}{
		{"id", func(row *entity.ProviderCredential) { row.ID = "crd_second" }},
		{"connection", func(row *entity.ProviderCredential) { row.ConnectionID = "con_second" }},
		{"name", func(row *entity.ProviderCredential) { row.Name = "Changed" }},
		{"priority", func(row *entity.ProviderCredential) { row.Priority = 0 }},
		{"enabled", func(row *entity.ProviderCredential) { row.Enabled = true }},
		{"verification status", func(row *entity.ProviderCredential) { row.VerificationStatus = "failed" }},
		{"verification instant", func(row *entity.ProviderCredential) { row.VerifiedAt = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := row
			test.change(&changed)
			if credentialMetadataRecord(changed).ETag == initial.ETag {
				t.Fatal("metadata change preserved validator")
			}
		})
	}
}

func TestCredentialMetadataReasonValidation(t *testing.T) {
	for _, reason := range []string{"", strings.Repeat("a", 1025), strings.Repeat("界", 342), "bad\nreason", "bad\x00reason", string([]byte{0xff})} {
		if validCredentialMetadataReason(reason) {
			t.Fatal("invalid metadata reason accepted")
		}
	}
	for _, reason := range []string{"Reviewed order", strings.Repeat("a", 1024), strings.Repeat("界", 341)} {
		if !validCredentialMetadataReason(reason) {
			t.Fatal("valid metadata reason rejected")
		}
	}
}
