package service

import "testing"

func TestNotificationPolicyAllowlist(t *testing.T) {
	if !notificationEventAllowed(notificationSourceSystemJob, notificationKindSystemJobFailure, "high", "persistence_failed") {
		t.Fatal("allowlisted system failure was rejected")
	}
	if !notificationEventAllowed(notificationSourceCredentialVerification, notificationKindCredentialFailure, "high", "verification_failed") {
		t.Fatal("allowlisted credential failure was rejected")
	}
	for _, test := range []struct{ source, kind, severity, detail string }{
		{"request_body", notificationKindSystemJobFailure, "high", "persistence_failed"},
		{notificationSourceSystemJob, "raw_error", "high", "dial tcp private"},
		{notificationSourceCredentialVerification, notificationKindCredentialFailure, "medium", "verification_failed"},
		{notificationSourceSystemJob, notificationKindSystemJobFailure, "critical", "persistence_failed"},
	} {
		if notificationEventAllowed(test.source, test.kind, test.severity, test.detail) {
			t.Fatalf("unsafe event accepted: %+v", test)
		}
	}
}

func TestSystemJobNotificationMetadata(t *testing.T) {
	severity, group, ok := systemJobNotificationMetadata(SystemJobStorageCleanup, "delete_failed")
	if !ok || severity != "medium" || group != "system_job:storage_cleanup:delete_failed" {
		t.Fatalf("unexpected cleanup alert metadata: %q %q %v", severity, group, ok)
	}
	severity, _, ok = systemJobNotificationMetadata(SystemJobRuntimePublication, "publication_failed")
	if !ok || severity != "high" {
		t.Fatal("runtime publication failure must be high severity")
	}
	if _, _, ok := systemJobNotificationMetadata(SystemJobCallRecordDelivery, "delivered"); ok {
		t.Fatal("successful job created an alert")
	}
}

func TestNotificationDeliveryResultCodesAreRedacted(t *testing.T) {
	if got := notificationDeliveryResultCode("recipient_failed"); got != "recipient_failed" {
		t.Fatalf("allowlisted result changed: %q", got)
	}
	if got := notificationDeliveryResultCode("550 private mailbox detail"); got != "delivery_failed" {
		t.Fatalf("raw SMTP response escaped redaction: %q", got)
	}
}
