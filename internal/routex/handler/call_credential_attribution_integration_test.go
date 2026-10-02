package handler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func testCallCredentialAttributionLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"attempt-attribution@example.com","password":"attribution-password","name":"Attribution admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	member := entity.User{ID: "usr_attempt_attribution", Email: "attempt-member@example.com", Name: "Attempt member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"attempt-member@example.com","password":"attribution-password"}`, nil, "")
	expectStatus(t, login, 200)
	_, memberCookie := readIdentity(t, login)
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	fact := service.CallFact{SnapshotID: "cfg_logical_final", RequestID: "req_exact_attribution", UserID: member.ID, KeyID: "key_historical", ModelID: "mdl_historical", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now.Add(time.Second), Attempts: []service.CallAttempt{
		{ID: "att_exact_first", CredentialID: "crd_exact_first", SnapshotID: "cfg_first_attempt", Status: "error", StartedAt: now, CompletedAt: now.Add(time.Millisecond)},
		{ID: "att_exact_second", CredentialID: "crd_exact_second", SnapshotID: "cfg_second_attempt", Status: "success", StartedAt: now.Add(time.Millisecond), CompletedAt: now.Add(time.Second)},
	}}
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
		if err != nil || stored.Record.SnapshotID != fact.SnapshotID || len(stored.Attempts) != 2 {
			t.Fatalf("exact call roundtrip failed: %+v %v", stored, err)
		}
		for index, attempt := range stored.Attempts {
			if attempt.CredentialID != fact.Attempts[index].CredentialID || attempt.SnapshotID != fact.Attempts[index].SnapshotID {
				t.Fatalf("attempt %d inferred or lost exact attribution: %+v", index, attempt)
			}
		}
	}
	assertExact()
	replayed := fact
	replayed.SnapshotID = "cfg_changed_parent"
	replayed.Attempts = append([]service.CallAttempt(nil), fact.Attempts...)
	for index := range replayed.Attempts {
		replayed.Attempts[index].CredentialID = "crd_changed"
		replayed.Attempts[index].SnapshotID = "cfg_changed"
	}
	if err := svc.RecordCall(context.Background(), replayed); err != nil {
		t.Fatal(err)
	}
	assertExact()
	var count int64
	if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", fact.RequestID).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("duplicate replay changed immutable attempt count")
	}
	legacy := fact
	legacy.RequestID = "req_legacy_attribution"
	legacy.Attempts = []service.CallAttempt{{ID: "att_legacy_attribution", Status: "success", FinalUsageKnown: true, StartedAt: now, CompletedAt: now}}
	if err := svc.RecordCall(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	var unknown entity.CallAttempt
	if err := db.First(&unknown, "id = ?", legacy.Attempts[0].ID).Error; err != nil || unknown.CredentialID != "" || unknown.SnapshotID != "" {
		t.Fatal("legacy RecordCall inferred attribution from parent or usage")
	}
	for _, field := range []string{"credential", "snapshot"} {
		invalid := fact
		invalid.RequestID = "req_invalid_" + field
		invalid.Attempts = append([]service.CallAttempt(nil), fact.Attempts[:1]...)
		invalid.Attempts[0].ID = "att_invalid_" + field
		if field == "credential" {
			invalid.Attempts[0].CredentialID = "unsafe/credential"
		} else {
			invalid.Attempts[0].SnapshotID = strings.Repeat("x", 31)
		}
		if err := svc.RecordCall(context.Background(), invalid); err == nil {
			t.Fatal("invalid attribution reached persistence")
		}
		if err := db.Model(&entity.CallRecord{}).Where("request_id = ?", invalid.RequestID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("invalid attribution left a partial call")
		}
	}
	// Neither personal nor administrator wire DTOs expose these internal facts.
	for _, request := range []struct {
		path  string
		admin bool
	}{{"/api/v1/calls/", false}, {"/api/v1/admin/calls/", true}} {
		cookie := memberCookie
		if request.admin {
			cookie = adminCookie
		}
		response := identityRequest(router, "GET", request.path+fact.RequestID, "", cookie, "")
		expectStatus(t, response, 200)
		var payload map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		for _, secretInternal := range []string{"credential_id", "snapshot_id", "crd_exact_first", "crd_exact_second", "cfg_first_attempt", "cfg_second_attempt"} {
			if strings.Contains(response.Body.String(), secretInternal) {
				t.Fatalf("wire response exposed internal attribution %q", secretInternal)
			}
		}
	}
	// Catalog deletion cannot cascade or prevent historical attempts: these
	// exact IDs were already persisted while all catalog rows were absent.
	provider := entity.Provider{ID: "prv_attribution", Name: "Attribution provider"}
	connection := entity.ProviderConnection{ID: "con_attribution", ProviderID: provider.ID, Name: "Attribution connection", BaseURL: "https://example.invalid/v1", Protocol: entity.ProtocolOpenAIChat}
	credential := entity.ProviderCredential{ID: fact.Attempts[1].CredentialID, ConnectionID: connection.ID, Name: "Deleted credential", Ciphertext: "fixture-only-opaque-envelope", VerificationStatus: "pending"}
	for _, row := range []any{&provider, &connection, &credential} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&credential).Error; err != nil {
		t.Fatal("historical attempts introduced live credential FK")
	}
	assertExact()
}
