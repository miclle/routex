package service

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func teamMonthlyNotificationFixture() (entity.ResourceLimit, time.Time, *eventqueue.AccountQuotaUsage) {
	row, created, usage := monthlyNotificationFixture()
	row.ScopeKind, row.ScopeID = "team", "tem_quota_notice"
	return row, created, usage
}

func TestTeamMonthlyNotificationsSettledDimensionsIndependent(t *testing.T) {
	for _, name := range []string{"settled", "holds_only", "tokens_unknown", "money_unknown", "both_unknown", "below_money_exact", "stale_currency", "incomplete_coverage", "member_account", "scope_alias", "unlimited", "finite_zero"} {
		t.Run(name, func(t *testing.T) {
			row, created, usage := teamMonthlyNotificationFixture()
			want := []string{"tokens", "money"}
			currency := "USD"
			switch name {
			case "holds_only":
				usage.Month.TokensUsed = 0
				usage.Month.MoneyUsed = nil
				usage.Month.TokensHeld = math.MaxInt64
				usage.Month.MoneyHeld = map[string]string{"USD": "100"}
				want = nil
			case "tokens_unknown":
				usage.Month.TokensUnknown = 1
				want = []string{"money"}
			case "money_unknown":
				usage.Month.MoneyUnknown = 1
				want = []string{"tokens"}
			case "both_unknown":
				usage.Month.TokensUnknown, usage.Month.MoneyUnknown = 1, 1
				want = nil
			case "below_money_exact":
				usage.Month.MoneyUsed["USD"] = "1.000000000000000000"
				want = []string{"tokens"}
			case "stale_currency":
				currency = "EUR"
				want = []string{"tokens"}
			case "incomplete_coverage":
				usage.CoverageStart = created.Add(time.Microsecond)
				want = nil
			case "member_account":
				row.ScopeKind = "team_member"
				want = nil
			case "scope_alias":
				row.ScopeKind = "TEAM"
				want = nil
			case "unlimited":
				row.TokensMonth, row.MoneyMonth = nil, nil
				want = nil
			case "finite_zero":
				zero, money := int64(0), "0"
				row.TokensMonth, row.MoneyMonth = &zero, &money
				usage.Month.TokensUsed, usage.Month.MoneyUsed = 0, nil
			}
			observations := monthlyQuotaObservations(row, created, usage, currency)
			var dimensions []string
			for _, observation := range observations {
				dimensions = append(dimensions, observation.Dimension)
				if observation.ScopeKind != "team" || observation.ScopeID != "tem_quota_notice" || observation.PolicyRevision != row.ETag || !observation.ResourceCreatedAt.Equal(created) || !observation.CoverageStart.Equal(usage.CoverageStart) {
					t.Fatal("observation borrowed another scope or creation/coverage basis")
				}
			}
			if !reflect.DeepEqual(dimensions, want) {
				t.Fatalf("settled observation dimensions %v, want %v", dimensions, want)
			}
		})
	}
}

func TestTeamMonthlyNotificationRequiresAppliedActiveExactResource(t *testing.T) {
	for _, name := range []string{"current", "missing_team", "aliased_team", "team_tombstone", "expired", "revision", "policy", "currency", "calendar", "account_tombstone", "calendar_tombstone", "creation_basis", "unknown_creation", "personal_account_only"} {
		t.Run(name, func(t *testing.T) {
			row, created, _ := teamMonthlyNotificationFixture()
			policy, err := policyFromRow(row)
			if err != nil {
				t.Fatal(err)
			}
			account := limitAccount(row.ScopeKind, row.ScopeID)
			if account != "team_tem_quota_notice" {
				t.Fatal("notification must use aggregate journal identity", account)
			}
			s := &Service{runtime: &gatewayRuntime{}}
			auth := &runtimeAuthorization{
				ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{row.ScopeID: {CreatedAt: created}},
				Quota:         &runtimeQuotaData{Setting: entity.QuotaSetting{TimeZone: "UTC"}, Currency: "USD", Revisions: map[string]string{account: row.ETag}},
				LimitPolicies: map[string]limits.Policy{account: policy},
			}
			switch name {
			case "missing_team":
				delete(auth.Teams, row.ScopeID)
			case "aliased_team":
				auth.Teams = map[string]runtimeTeam{strings.ToUpper(row.ScopeID): {CreatedAt: created}}
			case "team_tombstone":
				s.runtime.deniedTeams.Store(row.ScopeID, uint64(1))
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "revision":
				auth.Quota.Revisions[account] = "other"
			case "policy":
				changed := policy
				changed.TokensMonth = limitNumber(11)
				auth.LimitPolicies[account] = changed
			case "currency":
				auth.Quota.Currency = "EUR"
			case "calendar":
				auth.Quota.Setting.TimeZone = "Etc/UTC"
			case "account_tombstone":
				s.runtime.deniedLimits.Store(account, uint64(1))
			case "calendar_tombstone":
				s.runtime.deniedLimits.Store("quota_settings", uint64(1))
			case "creation_basis":
				auth.Teams[row.ScopeID] = runtimeTeam{CreatedAt: created.Add(time.Millisecond)}
			case "unknown_creation":
				created = time.Time{}
			case "personal_account_only":
				auth.Quota.Revisions = map[string]string{"user_usr_member": row.ETag}
				auth.LimitPolicies = map[string]limits.Policy{"user_usr_member": policy}
			}
			s.runtime.auth.Store(auth)
			if got := s.quotaNotificationResourceApplied(row, created, policy, "UTC", "USD"); got != (name == "current") {
				t.Fatal("unavailable or stale Team resources promoted to current observation", got)
			}
		})
	}
}

func TestTeamQuotaRecipientRequiresCurrentExactMembership(t *testing.T) {
	valid := quotaTeamIdentity{MembershipID: "tmm_legacy", MembershipUserID: "usr_member", MembershipTeamID: "tem_scope", UserID: "usr_member", TeamID: "tem_scope", TeamStatus: entity.ResourceActive, MembershipStatus: entity.ResourceActive, Role: entity.TeamMember}
	if !validQuotaTeamMember(valid, "usr_member", "tem_scope") {
		t.Fatal("exact enabled active legacy identity was rejected")
	}
	owner := valid
	owner.Role = entity.TeamOwner
	if !validQuotaTeamMember(owner, "usr_member", "tem_scope") {
		t.Fatal("active owner membership omitted")
	}
	for _, mutate := range []func(*quotaTeamIdentity){
		func(row *quotaTeamIdentity) { row.MembershipID = "" },
		func(row *quotaTeamIdentity) { row.MembershipID = "unsafe/member" },
		func(row *quotaTeamIdentity) { row.MembershipUserID = "USR_MEMBER" },
		func(row *quotaTeamIdentity) { row.UserID = "usr_other" },
		func(row *quotaTeamIdentity) { row.MembershipTeamID = "TEM_SCOPE" },
		func(row *quotaTeamIdentity) { row.TeamID = "tem_other" },
		func(row *quotaTeamIdentity) { row.TeamStatus = "ACTIVE" },
		func(row *quotaTeamIdentity) { row.TeamStatus = entity.ResourceDisabled },
		func(row *quotaTeamIdentity) { row.TeamStatus = entity.ResourceArchived },
		func(row *quotaTeamIdentity) { row.MembershipStatus = entity.ResourceDisabled },
		func(row *quotaTeamIdentity) { row.Role = "OWNER" },
		func(row *quotaTeamIdentity) { row.Role = "admin" },
		func(row *quotaTeamIdentity) { row.Disabled = true },
		func(row *quotaTeamIdentity) { row.Offboarded = true },
	} {
		row := valid
		mutate(&row)
		if validQuotaTeamMember(row, "usr_member", "tem_scope") {
			t.Fatal("alias, inactive membership, or operator bypass accepted", row)
		}
	}
}

func TestTeamQuotaHistoryRejoinRetainsOnlyOriginalRecipient(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	row := quotaInboxRow{ID: "qni_team", RecipientID: "usr_original", InboxObservationID: "qob_team", CreatedAt: now, ReadAt: &now,
		Observation: entity.QuotaNotificationObservation{ID: "qob_team", ScopeKind: "team", ScopeID: "tem_scope", ScopeName: "Recorded Team", Dimension: "money", PolicyRevision: "lim_original", Currency: "USD", Limit: "0.000000000000000001", Settled: "9007199254740993.123456789012345678", TimeZone: "UTC", AsOf: now}}
	for _, access := range []quotaInboxAccess{{ActorID: "usr_original", Operational: true}, {ActorID: "usr_later", TeamIDs: []string{"tem_scope"}}, {ActorID: "USR_ORIGINAL", TeamIDs: []string{"tem_scope"}}, {ActorID: "usr_original", TeamIDs: []string{"TEM_SCOPE"}}} {
		if validQuotaInboxRow(row, access) {
			t.Fatal("recorded recipients or current authority were substituted", access)
		}
	}
	// A new active membership grants current aggregate read authority, but only
	// this original recipient row becomes visible. Its read state is unchanged.
	rejoined := quotaInboxAccess{ActorID: "usr_original", TeamIDs: []string{"tem_scope"}}
	if !validQuotaInboxRow(row, rejoined) {
		t.Fatal("current active original recipient cannot read aggregate history")
	}
	record := quotaNotificationRecord(row)
	if record.SubjectType != "team" || record.SubjectID != "tem_scope" || record.SubjectName != "Recorded Team" || !record.Read || record.ReadAt != &now || record.Quota == nil || record.Quota.ScopeKind != "team" || record.Quota.Settled != row.Observation.Settled {
		t.Fatal("immutable Team snapshot or read state changed", record)
	}
	raw, err := json.Marshal(record)
	if err != nil || strings.Contains(string(raw), "alert_id") || strings.Contains(string(raw), "delivery_status") || strings.Contains(string(raw), "membership") {
		t.Fatal("quota snapshot acquired operational delivery or invocation identity", err, string(raw))
	}
}

func TestTeamQuotaInboxScopesKeepRecipientAndDimensionsConjunctive(t *testing.T) {
	db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	access := quotaInboxAccess{ActorID: "usr_actor", ProjectIDs: []string{"prj_one"}, TeamIDs: []string{"tem_one", "tem_two"}}
	query := quotaInboxQuery(db, access).Where("quota_notification_inboxes.id < ?", "qni_cursor")
	query.Statement.Clauses["WHERE"].Build(query.Statement)
	sql := query.Statement.SQL.String()
	if !strings.Contains(sql, `"quota_notification_inboxes"."recipient_id" = ? AND (`) || !strings.Contains(sql, `"observation"."dimension" = ? OR "observation"."dimension" = ?`) || !strings.HasSuffix(sql, " AND quota_notification_inboxes.id < ?") {
		t.Fatal("shared scopes bypassed recipient, dimension, or cursor bounds", sql)
	}
	for _, scope := range []string{"usr_actor", "prj_one", "tem_one", "tem_two"} {
		found := false
		for _, value := range query.Statement.Vars {
			found = found || value == scope
		}
		if !found {
			t.Fatal("authorized exact scope omitted", scope)
		}
	}
}

func TestTeamQuotaNotificationScanSharesBoundedCycle(t *testing.T) {
	rows := []entity.ResourceLimit{{ScopeKind: "project", ScopeID: "prj_one"}, {ScopeKind: "team", ScopeID: "tem_one"}, {ScopeKind: "user", ScopeID: "usr_one"}}
	cursor := quotaNotificationCursor{}
	var got []string
	observe := func(_ context.Context, kind, scopeID string) error {
		got = append(got, limitAccount(kind, scopeID))
		return nil
	}
	if done, err := reconcileQuotaNotificationRows(context.Background(), rows, &cursor, observe); done || err != nil || cursor != (quotaNotificationCursor{Kind: "user", ID: "usr_one"}) || !reflect.DeepEqual(got, []string{"project_prj_one", "team_tem_one", "user_usr_one"}) {
		t.Fatal("Team starved or substituted another account", done, err, cursor, got)
	}
	if done, err := reconcileQuotaNotificationRows(context.Background(), nil, &cursor, observe); !done || err != nil || cursor != (quotaNotificationCursor{}) {
		t.Fatal("cycle did not wrap", done, err)
	}
}

func TestTeamQuotaMembershipQueryRejectsCollationAliases(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		query := quotaTeamQuery(db).Where(database.ExactText(db, clause.Column{Table: "actor", Name: "id"}, "usr_exact")).Where(database.ExactText(db, clause.Column{Table: "team", Name: "id"}, "tem_exact"))
		query.Statement.Clauses["WHERE"].Build(query.Statement)
		sql := query.Statement.SQL.String()
		if !reflect.DeepEqual(query.Statement.Vars, []any{false, "active", "active", "owner", "member", "usr_exact", "tem_exact"}) || !strings.Contains(sql, "actor.offboarded_at IS NULL") || strings.Contains(sql, " IN ") {
			t.Fatal("membership authority lost exact current actor/target/state filters", sql, query.Statement.Vars)
		}
		if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") != 16 {
			t.Fatal("membership status/role/IDs and joins lost exact MySQL comparisons", sql)
		}
		if dialect.Name() == "postgres" && (!strings.Contains(sql, `"actor"."id" = "member"."user_id"`) || !strings.Contains(sql, `"team"."id" = "member"."team_id"`)) {
			t.Fatal("recipient joins can borrow another canonical resource", sql)
		}
	}
}
