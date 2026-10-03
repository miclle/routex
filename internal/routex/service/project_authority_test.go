package service

import (
	"reflect"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestProjectSelectionsRequireExactCompleteCanonicalIdentities(t *testing.T) {
	canonical := []string{"usr_project_one", "usr_project_two"}
	before := append([]string(nil), canonical...)
	if !projectSelectionMatches(canonical, []string{"usr_project_two", "usr_project_one"}) {
		t.Fatal("valid complete replacement depends on request order")
	}
	for _, submitted := range [][]string{
		{"USR_PROJECT_ONE", "usr_project_two"},
		{"usr_project_one ", "usr_project_two"},
		{"usr_project_one", "usr_missing"},
		{"usr_project_one", "usr_project_one"},
		{"usr_project_one"},
		{"", "usr_project_two"},
	} {
		if projectSelectionMatches(canonical, submitted) {
			t.Fatal("partial, duplicate or aliased replacement accepted", submitted)
		}
	}
	if projectSelectionMatches([]string{"usr_project_one", "usr_project_one"}, canonical) || !reflect.DeepEqual(canonical, before) {
		t.Fatal("corrupt selection accepted or current identities mutated")
	}
	if !projectSelectionMatches(nil, nil) {
		t.Fatal("explicit empty model replacement rejected")
	}
}

func TestProjectIdentitySelectionPreservesExactBoundPredicates(t *testing.T) {
	db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	query := projectIdentityQuery(db.Model(&entity.User{}), []string{"usr_one", "usr_two"}).Where("disabled = ? AND offboarded_at IS NULL", false)
	query.Statement.Clauses["WHERE"].Build(query.Statement)
	if query.Statement.SQL.String() != `WHERE ("id" = ? OR "id" = ?) AND (disabled = ? AND offboarded_at IS NULL)` || !reflect.DeepEqual(query.Statement.Vars, []any{"usr_one", "usr_two", false}) {
		t.Fatal("selection union escaped the enabled-user predicate", query.Statement.SQL.String(), query.Statement.Vars)
	}
}

func TestProjectManagerReplacementRetainsOnlyCanonicalRelationshipIdentity(t *testing.T) {
	rows := []entity.ProjectManager{
		{ID: "pmg_retained", ProjectID: "prj_exact", UserID: "usr_retained"},
		{ID: "pmg_project_alias", ProjectID: "PRJ_EXACT", UserID: "usr_project_alias"},
		{ID: "pmg_user_alias", ProjectID: "prj_exact", UserID: "USR_USER_ALIAS"},
		{ID: "pmg_removed", ProjectID: "prj_exact", UserID: "usr_removed"},
	}
	got := projectManagerIdentities(rows, "prj_exact", []string{"usr_retained", "usr_project_alias", "usr_user_alias"})
	if !reflect.DeepEqual(got, map[string]string{"usr_retained": "pmg_retained"}) {
		t.Fatal("canonical replacement borrowed an aliased or removed relationship identity", got)
	}
}
