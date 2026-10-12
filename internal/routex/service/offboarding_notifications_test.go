package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestOffboardingCompletionRecipientsUseOnlyActualAssignments(t *testing.T) {
	row := entity.OffboardingCase{UserID: "usr_departing", ActorID: "usr_planner", CompletedBy: "usr_executor"}
	assignments := OffboardingAssignments{Projects: []OffboardingProjectAssignment{{ProjectID: "prj_one", ManagerUserIDs: []string{"usr_successor", "usr_executor"}}, {ProjectID: "prj_two", ManagerUserIDs: []string{"usr_successor", "usr_departing"}}}, Teams: []OffboardingTeamAssignment{{TeamID: "tem_one", OwnerUserIDs: []string{"usr_owner", "usr_successor"}, AddMemberUserIDs: []string{"usr_member_only"}}}}
	before, _ := json.Marshal(assignments)
	if got := offboardingCompletionRecipients(row, assignments); !reflect.DeepEqual(got, []string{"usr_executor", "usr_owner", "usr_successor"}) {
		t.Fatal("recipient set borrowed planner, departing member or ordinary membership", got)
	}
	after, _ := json.Marshal(assignments)
	if string(before) != string(after) {
		t.Fatal("recipient selection mutated assignments")
	}
	if got := offboardingCompletionRecipients(entity.OffboardingCase{UserID: "usr_departing", CompletedBy: "usr_departing"}, OffboardingAssignments{}); len(got) != 0 {
		t.Fatal("departing executor received notice")
	}
}
func TestOffboardingCompletionPublicProjectionAndEmailArePrivate(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	row := entity.Notification{ID: "ntf_test", RecipientID: "usr_private", AlertID: "alr_private", LatestOccurrenceID: "occ_private", Kind: notificationKindOffboardingCompleted, Severity: "medium", DetailCode: "completed", OccurrenceCount: 1, SubjectType: "user", SubjectID: "usr_departing", SubjectName: "Secret identity", FirstSeenAt: at, LastSeenAt: at}
	data, err := json.Marshal(notificationRecord(row))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"usr_private", "usr_departing", "alr_private", "occ_private", "Secret identity", "subject_", "alert_id"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("private completion fact leaked", forbidden)
		}
	}
	intent := entity.NotificationDeliveryIntent{Kind: notificationKindOffboardingCompleted, Severity: "medium", DetailCode: "completed"}
	if !validOffboardingDeliveryMetadata(intent) {
		t.Fatal("closed completion metadata rejected")
	}
	intent.SubjectName = "Secret identity"
	if validOffboardingDeliveryMetadata(intent) {
		t.Fatal("private subject admitted")
	}
	body := notificationDeliveryBody(intent)
	if strings.Contains(body, "Secret") || strings.Contains(body, "usr_") || strings.Contains(body, "off_") {
		t.Fatal("email borrowed identity or Case metadata")
	}
	if notificationEventAllowed(notificationKindOffboardingCompleted, notificationKindOffboardingCompleted, "medium", "completed") {
		t.Fatal("recipient event entered operational broadcast allowlist")
	}
}
