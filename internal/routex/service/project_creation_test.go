package service

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestProjectCreationStrictPresenceAndLegacyShape(t *testing.T) {
	var legacy ProjectCreationInput
	if err := json.Unmarshal([]byte(`{"name":"Independent project","description":"Long-running work"}`), &legacy); err != nil || legacy.ManagerIDs != nil {
		t.Fatal("legacy creation manager contract changed", err)
	}
	for _, body := range []string{
		`null`, `[]`, `{"name":null}`, `{"name":"a","description":null}`, `{"name":"a","manager_ids":null}`,
		`{"name":"a","manager_ids":[]}`, `{"name":"a","manager_ids":"usr_manager"}`,
		`{"name":"a","manager_ids":[1]}`, `{"name":"a","manager_ids":["usr_manager"],"manager_ids":["usr_other"]}`,
		`{"name":"a","model_ids":[]}`, `{"name":"a","owner_ids":["usr_manager"]}`, `{"name":"a","creator_id":"usr_other"}`,
		`{"name":"a","tokens_month":1}`, `{"name":"a","name":"b"}`, `{"name":"a"} {}`,
	} {
		var input ProjectCreationInput
		if json.Unmarshal([]byte(body), &input) == nil {
			t.Fatalf("accepted malformed creation: %s", body)
		}
	}
	var selected ProjectCreationInput
	if err := json.Unmarshal([]byte(`{"name":"a","manager_ids":["usr_other"]}`), &selected); err != nil || selected.ManagerIDs == nil || !reflect.DeepEqual(*selected.ManagerIDs, []string{"usr_other"}) {
		t.Fatal("explicit selection lost presence", err)
	}
	// Missing name still goes through ordinary label validation at the service.
	var noName ProjectCreationInput
	if json.Unmarshal([]byte(`{"description":"Only description"}`), &noName) != nil || validCatalogLabel(noName.Name) {
		t.Fatal("legacy label requirement weakened")
	}
}

func TestProjectCreationManagerAuthorityDoesNotImplyPermanentCreatorOwnership(t *testing.T) {
	request := []string{"usr_other", "usr_second"}
	before := append([]string(nil), request...)
	selected, err := projectCreationManagers("usr_creator", false, &request)
	if err != nil || !reflect.DeepEqual(selected, []string{"usr_creator", "usr_other", "usr_second"}) {
		t.Fatal("ordinary creator disappeared", selected, err)
	}
	platform, err := projectCreationManagers("usr_creator", true, &request)
	if err != nil || !reflect.DeepEqual(platform, request) {
		t.Fatal("platform selection was not complete", platform, err)
	}
	for _, platform := range []bool{false, true} {
		legacy, err := projectCreationManagers("usr_creator", platform, nil)
		if err != nil || !reflect.DeepEqual(legacy, []string{"usr_creator"}) {
			t.Fatal("omitted selection no longer creator-only", legacy, err)
		}
	}
	includingCreator := []string{"usr_other", "usr_creator"}
	selected, err = projectCreationManagers("usr_creator", false, &includingCreator)
	if err != nil || !reflect.DeepEqual(selected, []string{"usr_creator", "usr_other"}) {
		t.Fatal("creator inclusion duplicated a relationship", selected, err)
	}
	if !reflect.DeepEqual(before, request) {
		t.Fatal("selection mutated caller's frozen intent")
	}
	selected[0] = "usr_changed"
	if request[0] != "usr_other" {
		t.Fatal("result borrowed caller manager slice")
	}
}

func TestProjectCreationRejectsUnsafeDuplicateAndOversizedSelections(t *testing.T) {
	for _, selected := range [][]string{{}, {"usr_one", "usr_one"}, {"USR_ONE"}, {" usr_one"}, {"usr_one "}, {"usr_one\n"}, {""}, {"tea_one"}, {"usr_012345678901234567890123456"}} {
		if _, err := projectCreationManagers("usr_creator", false, &selected); err != apperrors.ErrBadRequest {
			t.Fatal("invalid selection accepted", selected, err)
		}
	}
	tooMany := make([]string, 1001)
	if _, err := projectCreationManagers("usr_creator", true, &tooMany); err != apperrors.ErrBadRequest {
		t.Fatal("unbounded full selection accepted")
	}
	// An ordinary creation's mandatory creator counts toward the complete bound.
	full := make([]string, 1000)
	for i := range full {
		full[i] = "usr_selection_" + strconv.Itoa(i)
	}
	if _, err := projectCreationManagers("usr_creator", false, &full); err != apperrors.ErrBadRequest {
		t.Fatal("creator union escaped complete relationship bound")
	}
	if _, err := projectCreationManagers("usr_creator", true, &full); err != nil {
		t.Fatal("valid platform complete bound rejected", err)
	}
}
