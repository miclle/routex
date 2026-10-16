package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
)

func sharedEconomicHistoryTestReceipt(t *testing.T) entity.AdmissionEconomicReceipt {
	t.Helper()
	c, _, _, _ := sharedTestFixture(t)
	intent := c.intent
	intent.Owner = sharedAdmissionOwner{InstallationID: "led_01arz3ndektsv4rrffq69g5fav", SpoolID: "spool_shared", IncarnationID: "ins_01arz3ndektsv4rrffq69g5fav", ModeRevision: 2, Fence: 1}
	var err error
	intent.Digest, err = sharedDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	body, err := sharedEconomicCanonicalBody(intent)
	if err != nil {
		t.Fatal(err)
	}
	at := sharedAuthorizationTestTime().Add(time.Second)
	start, end, err := eventqueue.QuotaMonthBounds(at.UnixNano(), intent.TimeZone)
	if err != nil {
		t.Fatal(err)
	}
	return entity.AdmissionEconomicReceipt{
		InstallationID: intent.Owner.InstallationID, RequestDigest: sharedEconomicIdentityDigest(intent.RequestID), RequestID: intent.RequestID,
		ExecutionID: intent.ExecutionID, ExecutionDigest: sharedEconomicIdentityDigest(intent.ExecutionID), ReceiptID: "receipt_Shared", ReceiptDigest: sharedEconomicIdentityDigest("receipt_Shared"),
		SpoolID: intent.Owner.SpoolID, IncarnationID: intent.Owner.IncarnationID, ModeRevision: int64(intent.Owner.ModeRevision), Fence: int64(intent.Owner.Fence),
		OwnerStartedAt: intent.OwnerStartedAt, AdmissionDigest: intent.Digest, IntentBody: body, AdmittedAt: at.UnixNano(), MonthStart: start, MonthEnd: end,
	}
}

func TestSharedEconomicHistoryRejectsCorruptAcceptedIdentity(t *testing.T) {
	valid := sharedEconomicHistoryTestReceipt(t)
	intent, err := sharedEconomicReceiptIntent(valid)
	if err != nil || !sharedEconomicKeyValid(sharedIntentKey(intent)) {
		t.Fatalf("valid retained receipt: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(*entity.AdmissionEconomicReceipt)
	}{
		{"request_case", func(r *entity.AdmissionEconomicReceipt) { r.RequestID = strings.ToUpper(r.RequestID) }},
		{"request_digest", func(r *entity.AdmissionEconomicReceipt) { r.RequestDigest = strings.Repeat("a", 64) }},
		{"execution_case", func(r *entity.AdmissionEconomicReceipt) { r.ExecutionID = "OtherExecution" }},
		{"execution_digest", func(r *entity.AdmissionEconomicReceipt) { r.ExecutionDigest = strings.Repeat("a", 64) }},
		{"receipt_case", func(r *entity.AdmissionEconomicReceipt) { r.ReceiptID = strings.ToLower(r.ReceiptID) }},
		{"receipt_digest", func(r *entity.AdmissionEconomicReceipt) { r.ReceiptDigest = strings.Repeat("a", 64) }},
		{"installation", func(r *entity.AdmissionEconomicReceipt) { r.InstallationID = "led_01arz3ndektsv4rrffq69g5faw" }},
		{"owner_incarnation", func(r *entity.AdmissionEconomicReceipt) { r.IncarnationID = "ins_01arz3ndektsv4rrffq69g5faw" }},
		{"owner_fence", func(r *entity.AdmissionEconomicReceipt) { r.Fence++ }},
		{"owner_mode", func(r *entity.AdmissionEconomicReceipt) { r.ModeRevision++ }},
		{"owner_birth_future", func(r *entity.AdmissionEconomicReceipt) {
			r.OwnerStartedAt = time.Unix(0, r.AdmittedAt).Add(time.Second)
		}},
		{"owner_birth_precision", func(r *entity.AdmissionEconomicReceipt) { r.OwnerStartedAt = r.OwnerStartedAt.Add(time.Nanosecond) }},
		{"owner_birth_changed", func(r *entity.AdmissionEconomicReceipt) { r.OwnerStartedAt = r.OwnerStartedAt.Add(-time.Microsecond) }},
		{"admission_before_authorization", func(r *entity.AdmissionEconomicReceipt) {
			r.AdmittedAt = sharedAuthorizationTestTime().Add(-time.Second).UnixNano()
		}},
		{"month_start", func(r *entity.AdmissionEconomicReceipt) { r.MonthStart++ }},
		{"month_end", func(r *entity.AdmissionEconomicReceipt) { r.MonthEnd++ }},
		{"missing_body", func(r *entity.AdmissionEconomicReceipt) { r.IntentBody = nil }},
		{"noncanonical_json", func(r *entity.AdmissionEconomicReceipt) { r.IntentBody = append([]byte(" "), r.IntentBody...) }},
		{"trailing_json", func(r *entity.AdmissionEconomicReceipt) {
			r.IntentBody = append(append([]byte(nil), r.IntentBody...), []byte("{}")...)
		}},
		{"terminal_body_without_digest", func(r *entity.AdmissionEconomicReceipt) { r.TerminalBody = []byte("{}") }},
		{"terminal_digest_without_body", func(r *entity.AdmissionEconomicReceipt) { d := strings.Repeat("a", 64); r.TerminalDigest = &d }},
		{"admission_digest", func(r *entity.AdmissionEconomicReceipt) { r.AdmissionDigest = strings.Repeat("a", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := valid
			test.change(&row)
			if _, err := sharedEconomicReceiptIntent(row); !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatalf("accepted changed history: %v", err)
			}
		})
	}
}

func TestSharedEconomicHistoryCanonicalBytesAndExactDigests(t *testing.T) {
	row := sharedEconomicHistoryTestReceipt(t)
	intent, err := sharedEconomicReceiptIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := sharedEconomicCanonicalBody(intent)
	if err != nil || !bytes.Equal(encoded, row.IntentBody) {
		t.Fatalf("round-trip immutable intent: %v", err)
	}
	if sharedEconomicIdentityDigest("req_Case") == sharedEconomicIdentityDigest("req_case") {
		t.Fatal("case-distinct identity keys aliased")
	}
	var result sharedAdmissionIntent
	unknownField := append([]byte(`{"UnknownField":1,`), row.IntentBody[1:]...)
	for _, raw := range [][]byte{unknownField, append([]byte("\n"), row.IntentBody...), bytes.Repeat([]byte(" "), eventqueue.SharedSpoolMaxDataBytes+1), nil} {
		if sharedEconomicDecodeBody(raw, &result) {
			t.Fatal("accepted malformed, oversized or noncanonical immutable bytes")
		}
	}
}

func TestSharedEconomicHistoryRequiresInitializedCoverage(t *testing.T) {
	installation := "led_01arz3ndektsv4rrffq69g5fav"
	valid := entity.AdmissionEconomicDomain{InstallationID: installation, FormatVersion: 1, Initialized: true, CoverageStart: 1, ClockHighWater: 2, ReceiptCapacity: 10, ReceiptCount: 1}
	if !sharedEconomicDomainValid(valid, installation) {
		t.Fatal("valid domain refused")
	}
	for _, change := range []func(*entity.AdmissionEconomicDomain){
		func(d *entity.AdmissionEconomicDomain) { d.Initialized = false },
		func(d *entity.AdmissionEconomicDomain) { d.CoverageStart = 0 },
		func(d *entity.AdmissionEconomicDomain) { d.ClockHighWater = 0 },
		func(d *entity.AdmissionEconomicDomain) { d.ReceiptCapacity = 0 },
		func(d *entity.AdmissionEconomicDomain) { d.ReceiptCount = 11 },
		func(d *entity.AdmissionEconomicDomain) { d.ReceiptCount = -1 },
		func(d *entity.AdmissionEconomicDomain) { d.FormatVersion++ },
		func(d *entity.AdmissionEconomicDomain) { d.InstallationID = "led_01arz3ndektsv4rrffq69g5faw" },
	} {
		domain := valid
		change(&domain)
		if sharedEconomicDomainValid(domain, installation) {
			t.Fatalf("invented domain authority: %+v", domain)
		}
	}
}

func TestSharedEconomicHistoryMissingAndCancellationRemainExplicit(t *testing.T) {
	for _, test := range []struct{ input, errorWant error }{
		{gorm.ErrRecordNotFound, eventqueue.ErrMissing},
		{gorm.ErrDuplicatedKey, errSharedAdmissionConflict},
		{context.Canceled, context.Canceled},
		{context.DeadlineExceeded, context.DeadlineExceeded},
		{errSharedAdmissionConflict, errSharedAdmissionConflict},
	} {
		if got := sharedEconomicSQLError(test.input); !errors.Is(got, test.errorWant) {
			t.Fatalf("%v translated to %v", test.input, got)
		}
	}
	storage := errors.New("unavailable storage")
	got := sharedEconomicSQLError(storage)
	if !errors.Is(got, storage) || !errors.Is(got, errSharedEconomicUnavailable) || errors.Is(got, eventqueue.ErrMissing) {
		t.Fatalf("storage failure is not absence: %v", got)
	}
	var store *sharedEconomicSQLStore
	if receipt, err := store.Read(context.Background(), sharedAdmissionKey{}); receipt != nil || !errors.Is(err, errSharedAdmissionInvalid) {
		t.Fatalf("nil store read: %+v,%v", receipt, err)
	}
}
