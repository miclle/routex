package service

import (
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestOffboardingAssignmentsValidateWithoutMutatingCaller(t *testing.T) {
	input := OffboardingAssignments{Projects: []OffboardingProjectAssignment{{ProjectID: "prj_one", ManagerUserIDs: []string{"usr_z", "usr_a"}}}, Teams: []OffboardingTeamAssignment{{TeamID: "tem_one", OwnerUserIDs: []string{"usr_b"}, AddMemberUserIDs: []string{"usr_b"}}}}
	normalized, err := normalizeOffboardingAssignments(input, "usr_departing")
	if err != nil || normalized.Projects[0].ManagerUserIDs[0] != "usr_a" || input.Projects[0].ManagerUserIDs[0] != "usr_z" {
		t.Fatal("assignment normalization mutated the caller or failed to sort")
	}
	for _, invalid := range []OffboardingAssignments{
		{Projects: []OffboardingProjectAssignment{{ProjectID: "prj_one", ManagerUserIDs: []string{"usr_departing"}}}},
		{Projects: []OffboardingProjectAssignment{{ProjectID: "prj_one", ManagerUserIDs: []string{"usr_a", "usr_a"}}}},
		{Projects: []OffboardingProjectAssignment{{ProjectID: "prj_one"}, {ProjectID: "prj_one"}}},
		{Teams: []OffboardingTeamAssignment{{TeamID: "tem_one", OwnerUserIDs: []string{"usr_a"}, AddMemberUserIDs: []string{"usr_b"}}}},
	} {
		if _, err := normalizeOffboardingAssignments(invalid, "usr_departing"); err == nil {
			t.Fatal("unsafe or ambiguous successor assignment accepted")
		}
	}
}

func TestEmergencyOffboardingPreservesContinuity(t *testing.T) {
	inventory := &OffboardingInventory{UserID: "usr_departing", Projects: []OffboardingResource{{ID: "prj_sole", RequiresSuccessor: true}, {ID: "prj_shared"}}, Teams: []OffboardingResource{
		{ID: "tem_member", RequiresSuccessor: true, People: []OffboardingPerson{{UserID: "usr_departing", Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: "usr_disabled", Disabled: true, Status: entity.ResourceActive}, {UserID: "usr_successor", Status: entity.ResourceActive}}},
		{ID: "tem_empty", RequiresSuccessor: true},
	}}
	got := emergencyOffboardingAssignments(inventory, "usr_admin", OffboardingAssignments{})
	if len(got.Projects) != 1 || got.Projects[0].ProjectID != "prj_sole" || got.Projects[0].ManagerUserIDs[0] != "usr_admin" {
		t.Fatal("sole Project did not receive the acting administrator")
	}
	if len(got.Teams) != 1 || got.Teams[0].TeamID != "tem_member" || got.Teams[0].OwnerUserIDs[0] != "usr_successor" {
		t.Fatal("emergency Team selection used a disabled or departing member")
	}
	// No implicit outsider is added to an empty Team. The caller must select a
	// successor explicitly, so emergency disable cannot bypass membership review.
	for _, assignment := range got.Teams {
		if assignment.TeamID == "tem_empty" {
			t.Fatal("empty Team received an unreviewed successor")
		}
	}
}

func TestEmergencyRequestDigestExcludesPasswordProof(t *testing.T) {
	input := OffboardingEmergencyInput{RequestID: "req_test", Reason: "Incident response", CurrentPassword: "test-only-first-password"}
	first, err := offboardingDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.CurrentPassword = "test-only-second-password"
	second, err := offboardingDigest(input)
	if err != nil || first != second {
		t.Fatal("reauthentication proof entered persisted idempotency data")
	}
	input.Reason = "Different incident"
	third, err := offboardingDigest(input)
	if err != nil || third == first {
		t.Fatal("idempotency did not bind the action payload")
	}
}
