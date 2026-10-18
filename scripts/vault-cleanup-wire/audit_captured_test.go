package vaultwire

import (
	"os"
	"testing"
	"time"
)

func TestCapturedAppRoleAuditBracket(t *testing.T) {
	path, digest := os.Getenv("ROUTEX_TEST_VAULT_AUDIT_DESCRIPTOR"), os.Getenv("ROUTEX_TEST_VAULT_AUDIT_DESCRIPTOR_SHA256")
	if path == "" && digest == "" {
		t.Skip("Actual captured-audit descriptor absent; no actual-data or runtime qualification is inferred")
	}
	if path == "" || digest == "" {
		t.Fatal("Actual captured-audit descriptor incomplete")
	}
	value, err := uniqueJSON(privateCapture(t, path, digest, 16384))
	if err != nil || len(value) != 12 || !allowedKeys(value, "before", "before_sha256", "after", "after_sha256", "reader_mode", "data_path", "login_path", "version_hmac", "reader_policy", "cleanup_policy", "start", "end") {
		t.Fatal("Captured-audit descriptor invalid")
	}
	get := func(name string) string {
		t.Helper()
		v, ok := text(value[name])
		if !ok || v == "" {
			t.Fatal("Captured-audit expected field missing")
		}
		return v
	}
	start, err := time.Parse(time.RFC3339Nano, get("start"))
	if err != nil {
		t.Fatal("Captured-audit original start missing")
	}
	end, err := time.Parse(time.RFC3339Nano, get("end"))
	if err != nil {
		t.Fatal("Captured-audit original end missing")
	}
	plan := AuditExpectation{ReaderMode: get("reader_mode"), DataPath: get("data_path"), LoginPath: get("login_path"), VersionOneHMAC: get("version_hmac"), ReaderPolicy: get("reader_policy"), CleanupPolicy: get("cleanup_policy"), Start: start, End: end}
	if plan.ReaderMode != "app_role" {
		t.Fatal("Captured AppRole profile missing")
	}
	before := privateCapture(t, get("before"), get("before_sha256"), 1<<20)
	after := privateCapture(t, get("after"), get("after_sha256"), 1<<20)
	bracket, err := ParseAuditDelta(before, after, plan)
	if err != nil || bracket.Pairs != 5 || bracket.Logins != 1 || bracket.OwnershipReads != 1 || bracket.Destroys != 1 || bracket.Metrics != 2 {
		t.Fatal("Actual complete AppRole audit bracket refused")
	}
	t.Log("Actual original appended five-pair audit grammar parsed; this is not numeric SDK status or product cleanup acceptance")
}
