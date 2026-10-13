package handler

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

func assertNoOffboardingNotifications(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, model := range []any{&entity.OffboardingNotificationRecipient{}, &entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}, &entity.OperationalAlertOccurrence{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("uncommitted offboarding produced notification facts", count, err)
		}
	}
}

func assertOffboardingCompletionNotifications(t *testing.T, db *gorm.DB, svc *service.Service, actorID, caseID string, expectedRecipients, expectedEmailRecipients []string) {
	t.Helper()
	ctx := context.Background()
	slices.Sort(expectedRecipients)
	slices.Sort(expectedEmailRecipients)
	var receipts []entity.OffboardingNotificationRecipient
	if err := db.Where("case_id = ?", caseID).Order("recipient_id").Find(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		ids = append(ids, receipt.RecipientID)
		var user entity.User
		if err := db.First(&user, "id = ?", receipt.RecipientID).Error; err != nil || user.CreatedAt.IsZero() || !user.CreatedAt.Equal(receipt.RecipientCreatedAt) {
			t.Fatal("notification lost exact recipient birth")
		}
	}
	if !reflect.DeepEqual(ids, expectedRecipients) {
		t.Fatal("notification enumerated unrelated owners/administrators or omitted actual successor", ids)
	}
	offboardingNotificationReceiptControls(t, db, receipts)
	var occurrences []entity.OperationalAlertOccurrence
	if err := db.Where("source_type = ? AND source_id = ?", "offboarding_completed", caseID).Find(&occurrences).Error; err != nil || len(occurrences) != 1 {
		t.Fatal("completion replay duplicated occurrence", err)
	}
	occurrence := occurrences[0]
	if occurrence.SubjectType != "" || occurrence.SubjectID != "" || occurrence.SubjectName != "" || occurrence.DetailCode != "completed" {
		t.Fatal("completion occurrence contains private subject")
	}
	var intents []entity.NotificationDeliveryIntent
	if err := db.Where("occurrence_id = ?", occurrence.ID).Order("recipient_id").Find(&intents).Error; err != nil {
		t.Fatal(err)
	}
	mailIDs := make([]string, 0, len(intents))
	for _, intent := range intents {
		mailIDs = append(mailIDs, intent.RecipientID)
		if intent.Kind != "offboarding_completed" || intent.SMTPETag != "rev_offboarding_smtp" || intent.Status != "pending" || intent.SubjectID != "" || intent.SubjectName != "" {
			t.Fatal("completion intent lost captured preference/SMTP/privacy")
		}
	}
	if !reflect.DeepEqual(mailIDs, expectedEmailRecipients) {
		t.Fatal("completion email ignored existing medium preference or explicit recipient set", mailIDs)
	}
	for _, recipientID := range expectedRecipients {
		page, err := svc.ListNotifications(ctx, recipientID, service.NotificationFilter{})
		if err != nil {
			t.Fatal("recipient could not read own completion without operational permission", err)
		}
		var own *service.NotificationRecord
		for i := range page.Items {
			if page.Items[i].Kind == "offboarding_completed" && page.Items[i].FirstSeenAt.Equal(occurrence.OccurredAt) {
				own = &page.Items[i]
			}
		}
		if own == nil {
			t.Fatal("own completion notice absent")
		}
		body, err := json.Marshal(own)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{caseID, recipientID, occurrence.ID, occurrence.AlertID, "usr_departing", "subject_", "alert_id"} {
			if strings.Contains(string(body), private) {
				t.Fatal("completion DTO disclosed private source/recipient fact", private)
			}
		}
		if recipientID != actorID {
			if _, err := svc.ListOperationalAlerts(ctx, recipientID, service.OperationalAlertFilter{}); !errors.Is(err, apperrors.ErrForbidden) {
				t.Fatal("recipient notice broadened operational alert authority")
			}
		}
		if _, err := svc.MarkNotificationRead(ctx, recipientID, own.ID); err != nil {
			t.Fatal(err)
		}
		if err := svc.MarkAllNotificationsRead(ctx, recipientID); err != nil {
			t.Fatal(err)
		}
		var read entity.Notification
		if err := db.First(&read, "id = ?", own.ID).Error; err != nil || !read.Read || read.ReadAt == nil {
			t.Fatal("recipient exact read did not persist")
		}
		// Birth mismatch cannot inherit inbox/read access; stored history remains intact.
		var user entity.User
		if err := db.First(&user, "id = ?", recipientID).Error; err != nil {
			t.Fatal(err)
		}
		changed := user.CreatedAt.Add(time.Second)
		if err := db.Model(&entity.User{}).Where("id = ?", recipientID).Update("created_at", changed).Error; err != nil {
			t.Fatal(err)
		}
		hidden, err := svc.ListNotifications(ctx, recipientID, service.NotificationFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range hidden.Items {
			if item.ID == own.ID {
				t.Fatal("same-ID rebirth inherited completion history")
			}
		}
		if _, err := svc.MarkNotificationRead(ctx, recipientID, own.ID); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("same-ID rebirth marked original notice")
		}
		if slices.Contains(expectedEmailRecipients, recipientID) {
			if _, err := svc.FlushNotificationDelivery(ctx, 10); err != nil {
				t.Fatal(err)
			}
			var denied entity.NotificationDeliveryIntent
			if err := db.Where("occurrence_id = ? AND recipient_id = ?", occurrence.ID, recipientID).Take(&denied).Error; err != nil || denied.Status != "failed" || denied.ResultCode != "recipient_ineligible" || denied.Attempts != 1 {
				t.Fatal("birth mismatch did not stop recipient email before SMTP", err)
			}
		}
		if err := db.Model(&entity.User{}).Where("id = ?", recipientID).Update("created_at", user.CreatedAt).Error; err != nil {
			t.Fatal(err)
		}
		var retained entity.Notification
		if err := db.First(&retained, "id = ?", own.ID).Error; err != nil || !reflect.DeepEqual(read, retained) {
			t.Fatal("birth denial changed retained history")
		}
	}
	// Unrelated admitted member cannot borrow any explicit recipient's notice.
	var stranger string
	for _, candidate := range []string{"usr_remaining", "usr_successor"} {
		if !slices.Contains(expectedRecipients, candidate) {
			stranger = candidate
			break
		}
	}
	if stranger != "" {
		if _, err := svc.MarkNotificationRead(ctx, stranger, receipts[0].NotificationID); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("foreign recipient marked completion")
		}
	}
	alerts, err := svc.ListOperationalAlerts(ctx, actorID, service.OperationalAlertFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range alerts.Items {
		if item.Kind == "offboarding_completed" {
			t.Fatal("recipient event entered operational alert list")
		}
	}
	if _, err := svc.UpdateOperationalAlertState(ctx, actorID, occurrence.AlertID, service.OperationalAlertStateInput{State: "open", ETag: "unused"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("operational state action found recipient event")
	}
	deliveryPage, err := svc.ListNotificationDeliveries(ctx, actorID, service.NotificationDeliveryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range deliveryPage.Items {
		if item.Kind == "offboarding_completed" {
			t.Fatal("recipient event leaked to operational delivery view")
		}
	}
	// A retained recipient fact prevents historical schema peel, even though the
	// Case has completed and no resource currently has to remain assigned.
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 111 || before[110].Version != 111 || before[109].Version != 110 {
		t.Fatal("retained recipient peel control requires exact current V109 head")
	}
	peelErr := offboardingNotificationsPeelV107(db, before)
	if peelErr == nil || peelErr.Error() != "retained offboarding recipients prevent historical peel" || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("retained recipient history did not reject peel without changing receipts", peelErr)
	}
}

func offboardingNotificationTransactionSnapshot(t *testing.T, db *gorm.DB) []byte {
	t.Helper()
	var users []entity.User
	var cases []entity.OffboardingCase
	var keys []entity.APIKey
	var managers []entity.ProjectManager
	var members []entity.TeamMembership
	var roles []entity.UserRole
	var sessions []entity.Session
	var audits []entity.AuditEvent
	for _, item := range []struct {
		value any
		order string
	}{{&users, "id"}, {&cases, "id"}, {&keys, "id"}, {&managers, "id"}, {&members, "id"}, {&roles, "user_id, role_id"}, {&sessions, "id"}, {&audits, "id"}} {
		if err := db.Order(item.order).Find(item.value).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := json.Marshal([]any{users, cases, keys, managers, members, roles, sessions, audits})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testOffboardingNotificationSMTP(t *testing.T, db *gorm.DB, svc *service.Service, administrator entity.User) {
	t.Helper()
	ctx := context.Background()
	if err := db.Model(&entity.NotificationSetting{}).Where("user_id = ?", administrator.ID).Update("email_medium", true).Error; err != nil {
		t.Fatal(err)
	}
	host, port, accepted := smtpDeliveryFixture(t)
	unknownHost, unknownPort := smtpUnknownAcceptanceFixture(t)
	for _, scenario := range []struct {
		name, host, status string
		port               int
	}{{"accepted", host, "accepted", port}, {"unknown", unknownHost, "unknown", unknownPort}} {
		etag := "rev_offboarding_" + scenario.name
		if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{"host": scenario.host, "port": scenario.port, "security": "NONE", "enabled": true, "e_tag": etag}).Error; err != nil {
			t.Fatal(err)
		}
		target := entity.User{ID: "usr_mail_" + scenario.name, Email: scenario.name + "@example.invalid", Name: "Departing member", PasswordHash: administrator.PasswordHash, Role: entity.RoleMember}
		if err := db.Create(&target).Error; err != nil {
			t.Fatal(err)
		}
		request := service.OffboardingEmergencyInput{RequestID: "req_mail_" + scenario.name, Reason: "Controlled completion delivery", CurrentPassword: "test-only-offboarding-password"}
		completed, err := svc.EmergencyOffboarding(ctx, administrator.ID, target.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.FlushNotificationDelivery(ctx, 100); err != nil {
			t.Fatal(err)
		}
		var receipt entity.OffboardingNotificationRecipient
		if err := db.Where("case_id = ? AND recipient_id = ?", completed.ID, administrator.ID).Take(&receipt).Error; err != nil {
			t.Fatal(err)
		}
		var intent entity.NotificationDeliveryIntent
		if err := db.Where("occurrence_id = ? AND recipient_id = ?", receipt.OccurrenceID, administrator.ID).Take(&intent).Error; err != nil || intent.Status != scenario.status || intent.Attempts != 1 || intent.CompletedAt == nil || intent.SMTPETag != etag {
			t.Fatal("completion email lost native accepted/unknown durable result", scenario.name, err)
		}
		if accepted.Load() != 1 {
			t.Fatal("completion sent duplicate or missing accepted message")
		}
		replayed, err := svc.EmergencyOffboarding(ctx, administrator.ID, target.ID, request)
		if err != nil || replayed.ID != completed.ID {
			t.Fatal("completion email replay changed Case")
		}
		if count, err := svc.FlushNotificationDelivery(ctx, 100); err != nil || count != 0 {
			t.Fatal("accepted or uncertain completion email replayed", count, err)
		}
		var after entity.NotificationDeliveryIntent
		if err := db.First(&after, "id = ?", intent.ID).Error; err != nil || !reflect.DeepEqual(intent, after) || accepted.Load() != 1 {
			t.Fatal("delivery replay changed immutable terminal result")
		}
	}
}
