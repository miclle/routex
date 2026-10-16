package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The existing insights_ai registry entry runs these reads on both real drivers.
// SQL-only fixtures exercise stored metadata, not runtime application or usage.
func testInsightsQuotaBatchIntegration(t *testing.T, db *gorm.DB, actorID, teamID string, get func(string) *httptest.ResponseRecorder) {
	t.Helper()
	read := func(t *testing.T, path string) service.InsightsQuotaReport {
		t.Helper()
		response := get(path)
		expectStatus(t, response, 200)
		if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("batch policy response lost its private headers")
		}
		var report service.InsightsQuotaReport
		if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
			t.Fatal("decode batch policy response", err)
		}
		return report
	}
	policyQuery := func(tx *gorm.DB, kind, scopeID string) *gorm.DB {
		return tx.Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, kind)).
			Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, scopeID))
	}

	t.Run("quota_reports_team_batch_stored_policies", func(t *testing.T) {
		var original []entity.ResourceLimit
		if err := policyQuery(db, "team", teamID).Find(&original).Error; err != nil || len(original) > 1 {
			t.Fatal("capture exact original Team policy", err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := policyQuery(tx, "team", teamID).Delete(&entity.ResourceLimit{}).Error; err != nil {
					return err
				}
				if len(original) != 0 {
					return tx.Create(&original).Error
				}
				return nil
			})
			if err != nil {
				t.Error("restore exact original Team policy", err)
			}
		}()
		if err := policyQuery(db, "team", teamID).Delete(&entity.ResourceLimit{}).Error; err != nil {
			t.Fatal("prepare absent Team policy", err)
		}
		teamRow := func(dimension string) service.InsightsQuotaRow {
			report := read(t, "/api/v1/admin/insights/quota/teams?limit=50&dimension="+dimension)
			for _, row := range report.Items {
				if row.ID == teamID {
					return row
				}
			}
			t.Fatal("authorized Team disappeared from the bounded page")
			return service.InsightsQuotaRow{}
		}
		for _, dimension := range []string{"tokens", "money"} {
			if row := teamRow(dimension); row.LocalCap != nil || row.Behavior != "stop" {
				t.Fatal("absent Team policy acquired a cap or behavior from another batch row", dimension)
			}
		}
		var pricing entity.PricingSetting
		if err := db.Take(&pricing, 1).Error; err != nil {
			t.Fatal(err)
		}
		zero, money := int64(0), "0.000000000000000001"
		stored := entity.ResourceLimit{ScopeKind: "team", ScopeID: teamID, ETag: "lim_batch_team", ActorID: actorID, Reason: "Controlled stored policy batch", TokensMonth: &zero, MoneyMonth: &money, Currency: pricing.PlatformCurrency, TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "alert_only", IPMode: "none", IPRangesJSON: "[]"}
		if err := db.Create(&stored).Error; err != nil {
			t.Fatal("prepare exact stored Team policy", err)
		}
		if row := teamRow("tokens"); row.LocalCap == nil || *row.LocalCap != "0" || row.Behavior != "alert_only" {
			t.Fatal("batch lost explicit zero Team cap or stored behavior")
		}
		if row := teamRow("money"); row.LocalCap == nil || *row.LocalCap != money || row.Behavior != "alert_only" {
			t.Fatal("batch rounded exact Team money or lost its independent behavior")
		}
	})

	t.Run("quota_reports_key_batch_alias_and_sentinel", func(t *testing.T) {
		newID := func(prefix string) string {
			t.Helper()
			value, err := id.NewPrefixed(prefix)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		birth := time.Now().UTC().Truncate(time.Microsecond)
		// This isolated subject and two revoked roots have no journal history,
		// models or bearer delivery. They prove only metadata/paging behavior.
		subject := entity.User{ID: newID("usr"), Name: "Batch policy subject", Role: entity.RoleMember, CreatedAt: birth, UpdatedAt: birth}
		subject.Email = subject.ID + "@example.invalid"
		keys := []entity.APIKey{
			{ID: newID("key"), UserID: subject.ID, Name: "Batch root one", Status: entity.KeyRevoked, Prefix: "batch-test", CreatedAt: birth, UpdatedAt: birth},
			{ID: newID("key"), UserID: subject.ID, Name: "Batch root two", Status: entity.KeyRevoked, Prefix: "batch-test", CreatedAt: birth, UpdatedAt: birth},
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
		for i := range keys {
			keys[i].TokenHash = secret.SHA256Hex("controlled-quota-batch-" + keys[i].ID)
		}
		alias := strings.ToUpper(keys[0].ID)
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				for _, scopeID := range []string{keys[0].ID, keys[1].ID, alias} {
					if err := policyQuery(tx, "key", scopeID).Delete(&entity.ResourceLimit{}).Error; err != nil {
						return err
					}
				}
				if err := tx.Where("user_id = ? AND id IN ?", subject.ID, []string{keys[0].ID, keys[1].ID}).Delete(&entity.APIKey{}).Error; err != nil {
					return err
				}
				return tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, subject.ID)).Delete(&entity.User{}).Error
			})
			if err != nil {
				t.Error("remove exact SQL-only policy fixtures", err)
			}
		}()
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&subject).Error; err != nil {
				return err
			}
			return tx.Create(&keys).Error
		}); err != nil {
			t.Fatal("prepare bounded SQL-only root accounts", err)
		}
		cap := int64(999)
		aliasPolicy := entity.ResourceLimit{ScopeKind: "key", ScopeID: alias, ETag: "lim_batch_alias", ActorID: actorID, Reason: "Controlled alias omission", TokensMonth: &cap, IPMode: "none", IPRangesJSON: "[]"}
		if err := db.Create(&aliasPolicy).Error; err != nil {
			t.Fatal("prepare unrelated case-alias policy", err)
		}
		path := "/api/v1/admin/insights/quota/keys?dimension=tokens&scope_kind=member&scope_id=" + url.QueryEscape(subject.ID) + "&limit=1"
		first := read(t, path)
		if len(first.Items) != 1 || first.Items[0].ID != keys[0].ID || first.Items[0].LocalCap != nil || first.NextCursor == nil {
			t.Fatal("a database collation alias supplied the selected root's cap")
		}
		assertUnknownUsage := func(row service.InsightsQuotaRow) {
			t.Helper()
			if row.Level != "unknown" || row.MonthCovered || row.MonthSettled != nil || row.MonthHeld != nil || row.MonthUnknown != nil || row.UtilizationPercent != nil || row.EstimateAt != nil {
				t.Fatal("SQL-only policy metadata fabricated settled usage or a forecast")
			}
		}
		assertUnknownUsage(first.Items[0])
		if err := policyQuery(db, "key", alias).Delete(&entity.ResourceLimit{}).Error; err != nil {
			t.Fatal(err)
		}
		zero := int64(0)
		policies := []entity.ResourceLimit{
			{ScopeKind: "key", ScopeID: keys[0].ID, ETag: "lim_batch_first", ActorID: actorID, Reason: "Controlled explicit zero", TokensMonth: &zero, IPMode: "none", IPRangesJSON: "[]"},
			{ScopeKind: "key", ScopeID: keys[1].ID, ETag: "lim_batch_sentinel", ActorID: actorID, Reason: "Controlled invalid lookahead", IPMode: "none", IPRangesJSON: "invalid"},
		}
		if err := db.Create(&policies).Error; err != nil {
			t.Fatal("prepare selected and sentinel policies", err)
		}
		failed := get(path)
		expectStatus(t, failed, 500)
		for _, private := range []string{keys[0].ID, keys[1].ID, "local_cap", "items"} {
			if strings.Contains(failed.Body.String(), private) {
				t.Fatal("invalid lookahead exposed partial root policy facts")
			}
		}
		updated := policyQuery(db, "key", keys[1].ID).Model(&entity.ResourceLimit{}).Update("ip_ranges_json", "[]")
		if updated.Error != nil || updated.RowsAffected != 1 {
			t.Fatal("repair only the controlled sentinel", updated.Error)
		}
		first = read(t, path)
		if len(first.Items) != 1 || first.Items[0].ID != keys[0].ID || first.Items[0].LocalCap == nil || *first.Items[0].LocalCap != "0" || first.NextCursor == nil {
			t.Fatal("fresh explicit read lost the selected root's zero cap")
		}
		assertUnknownUsage(first.Items[0])
		last := read(t, path+"&cursor="+url.QueryEscape(*first.NextCursor))
		if len(last.Items) != 1 || last.Items[0].ID != keys[1].ID || last.Items[0].LocalCap != nil || last.NextCursor != nil {
			t.Fatal("repaired lookahead changed root pagination or acquired another cap")
		}
		assertUnknownUsage(last.Items[0])
	})
}
