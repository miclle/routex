package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestProjectListPolicyKeepsStoredZeroNullAndExactMoney(t *testing.T) {
	zero := int64(0)
	amount := "0.000000000000000001"
	for _, row := range []entity.ResourceLimit{
		{TokensMonth: &zero, MoneyMonth: &amount, Currency: "USD", RPM: &zero, TPM: &zero},
		{},
	} {
		result, err := projectListPolicy(row)
		if err != nil || !result.Stored || result.TokensMonth != row.TokensMonth || result.MoneyMonth != row.MoneyMonth || result.Currency != row.Currency || result.RPM != row.RPM || result.TPM != row.TPM {
			t.Fatalf("stored values changed: %+v, %v", result, err)
		}
	}
	absent, err := json.Marshal(&ProjectListLimits{})
	if err != nil || string(absent) != `{"stored":false,"tokens_month":null,"money_month":null,"currency":"","rpm":null,"tpm":null}` {
		t.Fatalf("absence became a default or denied fact: %s, %v", absent, err)
	}
	negative := int64(-1)
	badMoney := "1e3"
	for _, row := range []entity.ResourceLimit{{TokensMonth: &negative}, {RPM: &negative}, {MoneyMonth: &badMoney, Currency: "USD"}, {MoneyMonth: &amount}} {
		if result, err := projectListPolicy(row); result != nil || err == nil {
			t.Fatal("invalid stored policy became a summary", row, result, err)
		}
	}
}

func TestProjectListBatchScopeKeepsIndependentPredicates(t *testing.T) {
	db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	query := projectListScope(db.Table("resource_limits"), clause.Column{Name: "scope_id"}, []string{"prj_one", "prj_two"}).Where("scope_kind = ?", "project")
	query.Statement.Clauses["WHERE"].Build(query.Statement)
	if !strings.HasSuffix(query.Statement.SQL.String(), `("scope_id" = ? OR "scope_id" = ?) AND scope_kind = ?`) || !reflect.DeepEqual(query.Statement.Vars, []any{"prj_one", "prj_two", "project"}) {
		t.Fatal("batch union escaped its resource kind", query.Statement.SQL.String(), query.Statement.Vars)
	}
}
