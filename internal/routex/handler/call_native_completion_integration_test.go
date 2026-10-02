package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func testCallNativeCompletionLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"native-evidence@example.com","password":"native-evidence-password","name":"Native evidence admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	member := entity.User{ID: "usr_native_evidence", Email: "native-member@example.com", Name: "Native member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"native-member@example.com","password":"native-evidence-password"}`, nil, "")
	expectStatus(t, login, 200)
	_, memberCookie := readIdentity(t, login)
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	fact := service.CallFact{RequestID: "req_native_evidence", SnapshotID: "cfg_native_parent", UserID: member.ID, Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now}
	for _, status := range []string{"success", "error", "canceled"} {
		for _, marker := range []string{"unknown", "completed", "handoff", "blocked", "incomplete"} {
			fact.Attempts = append(fact.Attempts, service.CallAttempt{ID: fmt.Sprintf("att_native_%s_%s", status, marker), CredentialID: "crd_native_attempt", SnapshotID: "cfg_native_attempt", NativeCompletionEvidence: marker, Status: status, StartedAt: now, CompletedAt: now})
		}
	}
	failures := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { failures <- svc.RecordCall(context.Background(), fact) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertExact := func() {
		t.Helper()
		stored, err := svc.GetCall(context.Background(), "", fact.RequestID)
		if err != nil || len(stored.Attempts) != len(fact.Attempts) {
			t.Fatalf("native fact roundtrip failed: %+v %v", stored, err)
		}
		for index, actual := range stored.Attempts {
			expected := fact.Attempts[index]
			if actual.NativeCompletionEvidence != expected.NativeCompletionEvidence || actual.Status != expected.Status || actual.CredentialID != expected.CredentialID || actual.SnapshotID != expected.SnapshotID {
				t.Fatalf("native evidence inferred/status changed at attempt %d: %+v", index, actual)
			}
		}
	}
	assertExact()
	for _, attempt := range fact.Attempts {
		if attempt.AttemptNumber != 0 || attempt.WorkEvidence != "" || attempt.FailureClass != "" {
			t.Fatal("RecordCall mutated caller's attempt slice during normalization")
		}
	}
	replay := fact
	replay.Attempts = append([]service.CallAttempt(nil), fact.Attempts...)
	for index := range replay.Attempts {
		replay.Attempts[index].NativeCompletionEvidence = "unknown"
	}
	if err := svc.RecordCall(context.Background(), replay); err != nil {
		t.Fatal(err)
	}
	assertExact()
	legacy := service.CallFact{RequestID: "req_native_legacy", SnapshotID: "cfg_native_parent", UserID: member.ID, Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now, Attempts: []service.CallAttempt{{ID: "att_native_legacy", Status: "success", WorkEvidence: "completed", FinalUsageKnown: true, StartedAt: now, CompletedAt: now}}}
	if err := svc.RecordCall(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	var unknown entity.CallAttempt
	if err := db.First(&unknown, "id = ?", legacy.Attempts[0].ID).Error; err != nil || unknown.NativeCompletionEvidence != "unknown" || unknown.Status != "success" || !unknown.FinalUsageKnown || legacy.Attempts[0].NativeCompletionEvidence != "" {
		t.Fatal("legacy success/usage was promoted or caller was mutated")
	}
	for _, marker := range []string{"COMPLETED", "completed\n", "unsupported", strings.Repeat("x", 21)} {
		invalid := legacy
		invalid.RequestID = "req_native_invalid"
		invalid.Attempts = append([]service.CallAttempt(nil), legacy.Attempts...)
		invalid.Attempts[0].ID = "att_native_invalid"
		invalid.Attempts[0].NativeCompletionEvidence = marker
		if err := svc.RecordCall(context.Background(), invalid); err == nil {
			t.Fatal("noncanonical native marker reached persistence")
		}
		var count int64
		if err := db.Model(&entity.CallRecord{}).Where("request_id = ?", invalid.RequestID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("invalid native marker left partial call data")
		}
	}
	for _, endpoint := range []struct {
		path  string
		admin bool
	}{{"/api/v1/calls/", false}, {"/api/v1/admin/calls/", true}} {
		cookie := memberCookie
		if endpoint.admin {
			cookie = adminCookie
		}
		response := identityRequest(router, "GET", endpoint.path+fact.RequestID, "", cookie, "")
		expectStatus(t, response, 200)
		if strings.Contains(response.Body.String(), "native_completion_evidence") || strings.Contains(response.Body.String(), "NativeCompletionEvidence") {
			t.Fatal("internal native evidence leaked into public wire DTO")
		}
	}
}
