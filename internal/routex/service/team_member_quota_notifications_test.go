package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

func memberMonthlyNotificationFixture(t *testing.T) (*teamLimitContext, *eventqueue.AccountQuotaUsage) {
	t.Helper()
	row, created, usage := monthlyNotificationFixture()
	row.ScopeKind, row.ScopeID = "team_member", teamMemberLimitScopeID("tem_notice", "usr_notice")
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	parentRow := entity.ResourceLimit{ScopeKind: "team", ScopeID: "tem_notice", ETag: "lim_parent", TokensMonth: limitNumber(100), IPMode: "none", IPRangesJSON: "[]"}
	parent, err := policyFromRow(parentRow)
	if err != nil {
		t.Fatal(err)
	}
	member := &entity.TeamMembership{ID: "tmm_original", TeamID: "tem_notice", UserID: "usr_notice", Status: entity.ResourceActive, Role: entity.TeamMember}
	return &teamLimitContext{Actor: entity.User{ID: member.UserID}, Team: entity.Team{ID: member.TeamID, Name: "Recorded Team", Status: entity.ResourceActive, CreatedAt: created}, Member: member, Row: row, Stored: policy, ParentRow: parentRow, Parent: parent, Pricing: entity.PricingSetting{PlatformCurrency: "USD"}, Resolved: resolvedLimitTarget{kind: "team_member", id: row.ScopeID, teamID: member.TeamID, userID: member.UserID}}, usage
}

func TestTeamMemberQuotaObservationsIndependentSettledDimensions(t *testing.T) {
	for _, name := range []string{"settled", "holds_only", "tokens_unknown", "money_unknown", "both_unknown", "one_unit_below_money", "wrong_currency", "incomplete_coverage", "unlimited", "finite_zero", "wrong_pair", "parent_only", "unsafe_user", "calendar_unknown"} {
		t.Run(name, func(t *testing.T) {
			current, usage := memberMonthlyNotificationFixture(t)
			want := []string{"tokens", "money"}
			switch name {
			case "holds_only":
				usage.Month.TokensUsed = 0
				usage.Month.MoneyUsed = nil
				usage.Active.TokensHeld = 10
				usage.Active.MoneyHeld = map[string]string{"USD": "100"}
				usage.Month.TokensHeld = 10
				usage.Month.MoneyHeld = map[string]string{"USD": "100"}
				want = nil
			case "tokens_unknown":
				usage.Month.TokensUnknown = 1
				want = []string{"money"}
			case "money_unknown":
				usage.Month.MoneyUnknown = 1
				want = []string{"tokens"}
			case "both_unknown":
				usage.Month.TokensUnknown = 1
				usage.Month.MoneyUnknown = 1
				want = nil
			case "one_unit_below_money":
				usage.Month.MoneyUsed["USD"] = "1.000000000000000000"
				want = []string{"tokens"}
			case "wrong_currency":
				current.Pricing.PlatformCurrency = "EUR"
				want = []string{"tokens"}
			case "incomplete_coverage":
				usage.CoverageStart = current.Team.CreatedAt.Add(time.Microsecond)
				want = nil
			case "unlimited":
				current.Row.TokensMonth = nil
				current.Row.MoneyMonth = nil
				want = nil
			case "finite_zero":
				current.Row.TokensMonth = limitNumber(0)
				zero := "0"
				current.Row.MoneyMonth = &zero
				usage.Month.TokensUsed = 0
				usage.Month.MoneyUsed = nil
			case "wrong_pair":
				current.Row.ScopeID = teamMemberLimitScopeID(current.Team.ID, "usr_other")
				want = nil
			case "parent_only":
				current.Row = current.ParentRow
				want = nil
			case "unsafe_user":
				current.Member.UserID = "usr_notice "
				want = nil
			case "calendar_unknown":
				usage.TimeZone = "Local"
				want = nil
			}
			before := *usage
			observations := teamMemberMonthlyQuotaObservations(current, usage)
			var dimensions []string
			for _, observation := range observations {
				dimensions = append(dimensions, observation.Dimension)
				if !validTeamMemberQuotaObservation(observation, current.Team.ID, current.Member.UserID) || observation.ScopeName != "Recorded Team" || observation.PolicyRevision != current.Row.ETag || !observation.ResourceCreatedAt.Equal(current.Team.CreatedAt) {
					t.Fatal("child observation borrowed parent/another member basis", observation)
				}
			}
			if !reflect.DeepEqual(dimensions, want) || !reflect.DeepEqual(*usage, before) {
				t.Fatal("settled facts altered or independent dimensions lost", dimensions, want)
			}
		})
	}
	current, usage := memberMonthlyNotificationFixture(t)
	child := teamMemberMonthlyQuotaObservations(current, usage)
	if len(child) != 2 || child[1].Settled != "1.000000000000000001" {
		t.Fatal("exact child exhaustion lost")
	}
	if parent := monthlyQuotaObservations(current.ParentRow, current.Team.CreatedAt, usage, "USD"); len(parent) != 0 {
		t.Fatal("child settlement inferred aggregate exhaustion", parent)
	}
	current.Team.ID = "tem_renamed"
	if *child[0].TeamID != "tem_notice" || child[0].ScopeName != "Recorded Team" {
		t.Fatal("snapshot shares mutable current context")
	}
}

func TestTeamMemberQuotaRequiresOneCurrentPublishedPair(t *testing.T) {
	for _, name := range []string{"current", "missing_team", "creation_changed", "member_removed", "rejoined_old_generation", "disabled_member", "disabled_user", "offboarded_user", "team_disabled", "team_alias", "user_alias", "member_alias", "expired", "child_revision", "child_policy", "parent_revision", "parent_policy", "currency", "calendar", "calendar_revision", "child_tombstone", "parent_tombstone", "user_tombstone", "team_tombstone", "member_tombstone", "calendar_tombstone", "personal_account", "no_recorder"} {
		t.Run(name, func(t *testing.T) {
			current, _ := memberMonthlyNotificationFixture(t)
			child, parent := limitAccount("team_member", current.Row.ScopeID), limitAccount("team", current.Team.ID)
			svc := &Service{runtime: &gatewayRuntime{}, recorder: &callRecorder{}}
			auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{current.Team.ID: {CreatedAt: current.Team.CreatedAt, Members: map[string]string{current.Member.UserID: current.Member.ID}}}, Quota: &runtimeQuotaData{Setting: entity.QuotaSetting{TimeZone: "UTC", ETag: "quota_current"}, Currency: "USD", Revisions: map[string]string{child: current.Row.ETag, parent: current.ParentRow.ETag}}, LimitPolicies: map[string]limits.Policy{child: current.Stored, parent: current.Parent}}
			switch name {
			case "missing_team":
				delete(auth.Teams, current.Team.ID)
			case "creation_changed":
				team := auth.Teams[current.Team.ID]
				team.CreatedAt = team.CreatedAt.Add(time.Millisecond)
				auth.Teams[current.Team.ID] = team
			case "member_removed":
				delete(auth.Teams[current.Team.ID].Members, current.Member.UserID)
			case "rejoined_old_generation":
				auth.Teams[current.Team.ID].Members[current.Member.UserID] = "tmm_rejoined"
			case "disabled_member":
				current.Member.Status = entity.ResourceDisabled
			case "disabled_user":
				current.Actor.Disabled = true
			case "offboarded_user":
				stamp := time.Now()
				current.Actor.OffboardedAt = &stamp
			case "team_disabled":
				current.Team.Status = entity.ResourceDisabled
			case "team_alias":
				current.Team.ID = strings.ToUpper(current.Team.ID)
			case "user_alias":
				current.Actor.ID = strings.ToUpper(current.Actor.ID)
			case "member_alias":
				current.Member.UserID = strings.ToUpper(current.Member.UserID)
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "child_revision":
				auth.Quota.Revisions[child] = "other"
			case "child_policy":
				changed := current.Stored
				changed.TokensMonth = limitNumber(11)
				auth.LimitPolicies[child] = changed
			case "parent_revision":
				auth.Quota.Revisions[parent] = "other"
			case "parent_policy":
				changed := current.Parent
				changed.TokensMonth = limitNumber(101)
				auth.LimitPolicies[parent] = changed
			case "currency":
				auth.Quota.Currency = "EUR"
			case "calendar":
				auth.Quota.Setting.TimeZone = "Etc/UTC"
			case "calendar_revision":
				auth.Quota.Setting.ETag = "quota_old"
			case "child_tombstone":
				svc.runtime.deniedLimits.Store(child, true)
			case "parent_tombstone":
				svc.runtime.deniedLimits.Store(parent, true)
			case "user_tombstone":
				svc.runtime.deniedUsers.Store(current.Actor.ID, true)
			case "team_tombstone":
				svc.runtime.deniedTeams.Store(current.Team.ID, true)
			case "member_tombstone":
				svc.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey(current.Team.ID, current.Actor.ID), true)
			case "calendar_tombstone":
				svc.runtime.deniedLimits.Store("quota_settings", true)
			case "personal_account":
				auth.Quota.Revisions = map[string]string{"user_" + current.Actor.ID: current.Row.ETag}
				auth.LimitPolicies = map[string]limits.Policy{"user_" + current.Actor.ID: current.Stored}
			case "no_recorder":
				svc.recorder = nil
			}
			svc.runtime.auth.Store(auth)
			if got := svc.teamMemberQuotaNotificationApplied(current, entity.QuotaSetting{TimeZone: "UTC", ETag: "quota_current"}); got != (name == "current") {
				t.Fatal("stale or borrowed authority became applied", name, got)
			}
		})
	}
	// Absent aggregate policy is a published unlimited policy, not missing proof.
	current, _ := memberMonthlyNotificationFixture(t)
	current.ParentRow = entity.ResourceLimit{ScopeKind: "team", ScopeID: current.Team.ID, ETag: "0"}
	current.Parent, _ = policyFromRow(current.ParentRow)
	svc := &Service{runtime: &gatewayRuntime{}, recorder: &callRecorder{}}
	child := limitAccount("team_member", current.Row.ScopeID)
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{current.Team.ID: {CreatedAt: current.Team.CreatedAt, Members: map[string]string{current.Actor.ID: current.Member.ID}}}, Quota: &runtimeQuotaData{Setting: entity.QuotaSetting{TimeZone: "UTC", ETag: "quota_current"}, Currency: "USD", Revisions: map[string]string{child: current.Row.ETag}}, LimitPolicies: map[string]limits.Policy{child: current.Stored}}
	svc.runtime.auth.Store(auth)
	if !svc.teamMemberQuotaNotificationApplied(current, entity.QuotaSetting{TimeZone: "UTC", ETag: "quota_current"}) {
		t.Fatal("current unlimited aggregate prevented exact child observation")
	}
}

func TestTeamMemberQuotaHistoryPrivateStableRejoinAndImmutableSnapshot(t *testing.T) {
	current, usage := memberMonthlyNotificationFixture(t)
	observation := teamMemberMonthlyQuotaObservations(current, usage)[1]
	observation.ID = "qob_member"
	stamp := observation.AsOf
	row := quotaInboxRow{ID: "qni_member", RecipientID: current.Actor.ID, InboxObservationID: observation.ID, CreatedAt: stamp, ReadAt: &stamp, Observation: observation}
	valid := quotaInboxAccess{ActorID: current.Actor.ID, TeamIDs: []string{current.Team.ID}}
	for _, access := range []quotaInboxAccess{{ActorID: current.Actor.ID, Operational: true}, {ActorID: "usr_owner", TeamIDs: valid.TeamIDs}, {ActorID: "usr_admin", Operational: true, TeamIDs: valid.TeamIDs}, {ActorID: "USR_NOTICE", TeamIDs: valid.TeamIDs}, {ActorID: current.Actor.ID, TeamIDs: []string{"TEM_NOTICE"}}, {ActorID: "usr_later", TeamIDs: valid.TeamIDs}} {
		if validQuotaInboxRow(row, access) {
			t.Fatal("another member, admin or absent membership can read child history", access)
		}
	}
	if !validQuotaInboxRow(row, valid) {
		t.Fatal("rejoined same stable pair lost original inbox")
	}
	for _, name := range []string{"foreign_team", "foreign_user", "missing_proof", "wrong_digest"} {
		bad := row
		switch name {
		case "foreign_team":
			v := "tem_other"
			bad.Observation.TeamID = &v
		case "foreign_user":
			v := "usr_other"
			bad.Observation.MemberUserID = &v
		case "missing_proof":
			bad.Observation.MemberUserID = nil
		case "wrong_digest":
			bad.Observation.ScopeID = teamMemberLimitScopeID("tem_notice", "usr_other")
		}
		if validQuotaInboxRow(bad, valid) || sameQuotaObservationIdentity(bad.Observation, row.Observation) {
			t.Fatal("immutable identity admitted substituted pair", name)
		}
	}
	record := quotaNotificationRecord(row)
	if record.SubjectType != "team_member" || record.SubjectName != "Recorded Team" || record.SubjectID != observation.ScopeID || !record.Read || record.ReadAt != &stamp || record.Quota == nil || record.Quota.TeamID == nil || *record.Quota.TeamID != current.Team.ID || record.Quota.MemberUserID == nil || *record.Quota.MemberUserID != current.Actor.ID || record.Quota.Settled != "1.000000000000000001" {
		t.Fatal("record altered frozen member facts", record)
	}
	raw, err := json.Marshal(record)
	if err != nil || strings.Contains(string(raw), "membership") || strings.Contains(string(raw), "alert_id") || strings.Contains(string(raw), "delivery_status") {
		t.Fatal("notice borrowed invocation or SMTP/operational facts", err, string(raw))
	}
	aggregate := row
	aggregate.Observation = entity.QuotaNotificationObservation{ScopeKind: "team", ScopeID: current.Team.ID, Dimension: "tokens"}
	raw, err = json.Marshal(quotaNotificationRecord(aggregate))
	if err != nil || strings.Contains(string(raw), "member_user_id") || strings.Contains(string(raw), "team_id") {
		t.Fatal("aggregate snapshot acquired member proof", err, string(raw))
	}
}

func TestTeamMemberQuotaScanBoundedPoliciesFairWrapAndCancellation(t *testing.T) {
	var candidates []quotaTeamIdentity
	var policies []entity.ResourceLimit
	for i := range 33 {
		row := quotaTeamIdentity{MembershipID: fmt.Sprintf("tmm_%03d", i), MembershipUserID: fmt.Sprintf("usr_%03d", i), MembershipTeamID: "tem_batch", UserID: fmt.Sprintf("usr_%03d", i), TeamID: "tem_batch", TeamStatus: entity.ResourceActive, MembershipStatus: entity.ResourceActive, Role: entity.TeamMember}
		candidates = append(candidates, row)
		if i != 1 {
			policies = append(policies, entity.ResourceLimit{ScopeKind: "team_member", ScopeID: teamMemberLimitScopeID(row.TeamID, row.UserID), TokensMonth: limitNumber(5)})
		}
	}
	transient := errors.New("temporary publication unavailable")
	var seen []string
	observe := func(_ context.Context, target teamMemberQuotaTarget) error {
		seen = append(seen, target.UserID)
		if target.UserID == "usr_000" {
			return transient
		}
		return nil
	}
	cursor := ""
	if done, err := reconcileTeamMemberQuotaNotificationRows(context.Background(), candidates[:32], policies, &cursor, observe); done || !errors.Is(err, transient) || cursor != "tmm_031" || len(seen) != 31 {
		t.Fatal("first failure or absent policy starved later members", done, err, cursor, len(seen))
	}
	if done, err := reconcileTeamMemberQuotaNotificationRows(context.Background(), candidates[32:], policies, &cursor, observe); done || err != nil || cursor != "tmm_032" || len(seen) != 32 {
		t.Fatal("later-page member was skipped", done, err, cursor, len(seen))
	}
	if done, err := reconcileTeamMemberQuotaNotificationRows(context.Background(), nil, nil, &cursor, observe); !done || err != nil || cursor != "" {
		t.Fatal("EOF failed to wrap")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reconcileTeamMemberQuotaNotificationRows(ctx, candidates, policies, &cursor, observe); !errors.Is(err, context.Canceled) || len(seen) != 32 || cursor != "" {
		t.Fatal("cancellation observed another member or advanced cursor")
	}
}

func TestTeamMemberQuotaQueriesRetainExactPrivateScopeAndPageBounds(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
		query := teamMemberQuotaCandidatesQuery(db, "tmm_after")
		query.Statement.Clauses["WHERE"].Build(query.Statement)
		sqlText := query.Statement.SQL.String()
		limit := query.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
		if limit.Limit == nil || *limit.Limit != 32 || !strings.HasSuffix(sqlText, " AND member.id > ?") || !strings.Contains(sqlText, "actor.offboarded_at IS NULL") {
			t.Fatal("candidate query is unbounded or lacks current authority", sqlText)
		}
		access := quotaInboxAccess{ActorID: "usr_self", TeamIDs: []string{"tem_own"}, Operational: true}
		for _, query := range []*gorm.DB{quotaInboxQuery(db, access), quotaInboxMutationQuery(db, access)} {
			query.Statement.Clauses["WHERE"].Build(query.Statement)
			sqlText := query.Statement.SQL.String()
			for _, fragment := range []string{"recipient_id", "scope_kind", "scope_id", "team_id", "member_user_id"} {
				if !strings.Contains(sqlText, fragment) {
					t.Fatal("read or mark-all omitted exact member proof", fragment, sqlText)
				}
			}
			for _, value := range []string{"usr_self", "tem_own", teamMemberLimitScopeID("tem_own", "usr_self"), "team_member"} {
				found := false
				for _, bound := range query.Statement.Vars {
					found = found || bound == value
				}
				if !found {
					t.Fatal("private proof was not bound", value, query.Statement.Vars)
				}
			}
			if dialect.Name() == "mysql" && !strings.Contains(sqlText, "AS BINARY") {
				t.Fatal("case-insensitive SQL can borrow member authority", sqlText)
			}
		}
	}
}

type memberNoticeContextKey struct{}

func TestTeamMemberQuotaSubjectQueriesDoNotLeakInitializedStatement(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			current, _ := memberMonthlyNotificationFixture(t)
			target := teamMemberQuotaTarget{current.Member.ID, current.Team.ID, current.Actor.ID}
			calls := 0
			if err := db.Callback().Query().Register("test:member-notification-subjects", func(tx *gorm.DB) {
				callbacks.BuildQuerySQL(tx)
				calls++
				table := tx.Statement.Schema.Table
				if strings.Contains(tx.Statement.SQL.String(), "sentinel") {
					t.Fatal("initialized predicate leaked into subject query", tx.Statement.SQL.String())
				}
				switch dest := tx.Statement.Dest.(type) {
				case *entity.Team:
					*dest = current.Team
				case *entity.User:
					*dest = current.Actor
				case *entity.TeamMembership:
					*dest = *current.Member
				case *entity.ResourceLimit:
					if calls == 4 {
						*dest = current.Row
					} else {
						*dest = current.ParentRow
					}
				case *entity.PricingSetting:
					*dest = current.Pricing
				default:
					t.Fatalf("unexpected subject destination %T", dest)
				}
				if tx.Statement.Context.Value(memberNoticeContextKey{}) != "retained" {
					t.Fatal("fresh statement lost context")
				}
				for _, foreign := range []string{"teams", "users", "team_memberships", "resource_limits", "pricing_settings"} {
					if foreign != table && strings.Contains(tx.Statement.SQL.String(), "FROM "+"\""+foreign+"\"") {
						t.Fatal("query kept previous destination table", tx.Statement.SQL.String())
					}
				}
				if calls <= 5 && tx.Statement.Clauses["FOR"].Expression == nil {
					t.Fatal("subject lost transaction lock", table)
				}
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			inherited := db.WithContext(context.WithValue(context.Background(), memberNoticeContextKey{}, "retained")).Clauses(clause.Locking{Strength: "UPDATE"}).Where("sentinel = ?", "prior-scope").Model(&entity.User{})
			loaded, err := loadTeamMemberQuotaContext(inherited, target)
			if err != nil || calls != 6 || loaded.Member == nil || loaded.Team.ID != target.TeamID || loaded.Member.UserID != target.UserID {
				t.Fatal("initialized statement leaked into subject authorization", err, calls, loaded)
			}
		})
	}
}
