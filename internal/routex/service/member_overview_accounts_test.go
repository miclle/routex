package service

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
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

func TestMemberOverviewCursorAndPageAreActorBound(t *testing.T) {
	cursor := memberOverviewCursor("usr_one", "tem_after")
	filter, after, err := normalizeMemberOverviewFilter("usr_one", MemberOverviewAccountsFilter{Cursor: cursor})
	if err != nil || filter.Limit != 10 || after != "tem_after" {
		t.Fatal(filter, after, err)
	}
	for _, input := range []MemberOverviewAccountsFilter{{Cursor: "bad"}, {Cursor: cursor, Limit: 51}, {Limit: -1}, {Cursor: memberOverviewCursor("usr_other", "tem_after")}, {Cursor: memberOverviewCursor("usr_one", "bad/team")}, {Cursor: memberOverviewCursor("USR_ONE", "tem_after")}} {
		if _, _, err := normalizeMemberOverviewFilter("usr_one", input); err == nil {
			t.Fatal("cursor expanded actor/page scope", input)
		}
	}
	if _, _, err := normalizeMemberOverviewFilter("unsafe/actor", MemberOverviewAccountsFilter{}); err == nil {
		t.Fatal("unsafe context accepted")
	}
}

func TestMemberOverviewTeamIdentityRequiresExactActiveOwnMembership(t *testing.T) {
	valid := overviewTeamIdentity{ID: "tem_legacy", Name: "Team", MembershipID: "tmm_legacy", MembershipTeamID: "tem_legacy", MembershipUserID: "usr_actor", TeamStatus: entity.ResourceActive, MembershipStatus: entity.ResourceActive, Role: entity.TeamMember, CreatedAt: time.Now()}
	if !validOverviewTeam(valid, "usr_actor") {
		t.Fatal("exact current member rejected")
	}
	for _, mutate := range []func(*overviewTeamIdentity){
		func(row *overviewTeamIdentity) { row.MembershipTeamID = "TEM_LEGACY" },
		func(row *overviewTeamIdentity) { row.MembershipUserID = "USR_ACTOR" },
		func(row *overviewTeamIdentity) { row.MembershipID = "" },
		func(row *overviewTeamIdentity) { row.MembershipID = "bad/member" },
		func(row *overviewTeamIdentity) { row.TeamStatus = entity.ResourceDisabled },
		func(row *overviewTeamIdentity) { row.MembershipStatus = entity.ResourceDisabled },
		func(row *overviewTeamIdentity) { row.Role = "admin" },
		func(row *overviewTeamIdentity) { row.Role = "OWNER" },
		func(row *overviewTeamIdentity) { row.CreatedAt = time.Time{} },
	} {
		row := valid
		mutate(&row)
		if validOverviewTeam(row, "usr_actor") {
			t.Fatal("authority alias or stale membership accepted", row)
		}
	}
}

func TestMemberOverviewQueriesKeepExactBoundedOwnScope(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		query := memberOverviewTeamQuery(db, "usr_actor", "tem_after", 10)
		query.Statement.Clauses["WHERE"].Build(query.Statement)
		if !reflect.DeepEqual(query.Statement.Vars, []any{"usr_actor", "active", "active", "owner", "member", "tem_after"}) || strings.Contains(query.Statement.SQL.String(), "permissions") || !strings.HasSuffix(query.Statement.SQL.String(), " AND team.id > ?") {
			t.Fatal("self-only membership paging borrowed platform privileges", query.Statement.SQL.String(), query.Statement.Vars)
		}
		if dialect.Name() == "mysql" && strings.Count(query.Statement.SQL.String(), "AS BINARY") != 10 {
			t.Fatal("current membership filters lost exact collation", query.Statement.SQL.String())
		}
		targets := []overviewAccountTarget{{kind: "user", id: "usr_actor"}, {kind: "team", id: "tem_one"}, {kind: "team_member", id: teamMemberLimitScopeID("tem_one", "usr_actor")}}
		policyQuery := memberOverviewPolicyQuery(db.Session(&gorm.Session{NewDB: true}), targets)
		policyQuery.Statement.Clauses["WHERE"].Build(policyQuery.Statement)
		if len(policyQuery.Statement.Vars) != 6 || strings.Contains(policyQuery.Statement.SQL.String(), " IN ") || dialect.Name() == "mysql" && strings.Count(policyQuery.Statement.SQL.String(), "AS BINARY") != 12 {
			t.Fatal("policy batch lost exact kind/ID pairs", policyQuery.Statement.SQL.String(), policyQuery.Statement.Vars)
		}
	}
}

func TestMemberOverviewCalendarQueryPreservesReleasedColumnNames(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		query := memberOverviewCalendarQuery(db)
		if err := query.Statement.Parse(&entity.QuotaSetting{}); err != nil {
			t.Fatal(err)
		}
		// V19's released calendar revision column is e_tag, independently of
		// the JSON API field spelling. A literal etag SQL projection is invalid.
		if field := query.Statement.Schema.LookUpField("ETag"); field == nil || field.DBName != "e_tag" {
			t.Fatal("calendar revision no longer matches the released schema", field)
		}
		callbacks.BuildQuerySQL(query)
		columns := query.Statement.Clauses["SELECT"].Expression.(clause.Select).Columns
		if !reflect.DeepEqual(columns, []clause.Column{{Name: "time_zone"}, {Name: "e_tag"}}) {
			t.Fatal("calendar snapshot selected an API spelling or extra columns", columns, query.Statement.SQL.String())
		}
	}
}

func TestMemberOverviewPoliciesPreserveFullStoredConfigurationAndAbsentDefault(t *testing.T) {
	targets := []overviewAccountTarget{{kind: "user", id: "usr_actor"}, {kind: "team", id: "tem_one"}, {kind: "team_member", id: "pair_one"}}
	money, zero := "0.000000000000000001", int64(0)
	basis := "default_original"
	row := entity.ResourceLimit{ScopeKind: "team", ScopeID: "tem_one", ETag: "lim_saved", TokensMonth: &zero, MoneyMonth: &money, Currency: "USD", RPM: limitNumber(7), IPMode: "none", AppliedDefaultETag: &basis}
	if err := memberOverviewPolicies(targets, []entity.ResourceLimit{row}); err != nil {
		t.Fatal(err)
	}
	if targets[0].row.ETag != "0" || targets[0].policy.TokensMonth != nil || targets[0].policy.MoneyMonth != nil || targets[1].row.AppliedDefaultETag != &basis || *targets[1].policy.RPM != 7 || *targets[1].policy.TokensMonth != 0 || *targets[1].policy.MoneyMonth != money || targets[2].row.ETag != "0" {
		t.Fatal("snapshot fabricated current defaults or rewrote saved policy", targets)
	}
	for _, other := range []entity.ResourceLimit{{ScopeKind: "TEAM", ScopeID: "tem_one", ETag: "revision"}, {ScopeKind: "team", ScopeID: "TEM_ONE", ETag: "revision"}, {ScopeKind: "user", ScopeID: "usr_other", ETag: "revision"}, {ScopeKind: "team", ScopeID: "tem_one", ETag: ""}} {
		if memberOverviewPolicies(targets, []entity.ResourceLimit{other}) == nil {
			t.Fatal("aliased, foreign or missing-revision policy accepted", other)
		}
	}
	if memberOverviewPolicies(targets, []entity.ResourceLimit{row, row}) == nil {
		t.Fatal("duplicate canonical policy accepted")
	}
}

func TestMemberOverviewMonthlyUsageIsExactAndCoverageAware(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, zone)
	usage := eventqueue.AccountQuotaUsage{AsOf: start.AddDate(0, 0, 20), TimeZone: "America/New_York", CoverageStart: start, Month: eventqueue.QuotaUsage{TokensUsed: math.MaxInt64, TokensHeld: 9007199254740993, TokensUnknown: math.MaxInt64, MoneyUsed: map[string]string{"USD": "9007199254740993.123456789012345678", "EUR": "0.000000000000000001"}, MoneyHeld: map[string]string{"USD": "0.000000000000000001"}, MoneyUnknown: math.MaxInt64}}
	value, err := overviewMonthlyUsage(usage, start.AddDate(0, -1, 0))
	if err != nil || !value.Covered || value.MonthEnd.Sub(value.MonthStart) != 743*time.Hour || value.TokensUsed != "9223372036854775807" || value.TokensHeld != "9007199254740993" || value.TokensUnknown != "9223372036854775807" || value.MoneyUnknown != "9223372036854775807" || !reflect.DeepEqual(value.MoneyUsed, usage.Month.MoneyUsed) {
		t.Fatal("decimal/int64/calendar evidence changed", value, err)
	}
	usage.CoverageStart = start.Add(time.Microsecond)
	value, err = overviewMonthlyUsage(usage, start.AddDate(0, -1, 0))
	if err != nil || value.Covered {
		t.Fatal("incomplete month reported as fully covered", value, err)
	}
	value, err = overviewMonthlyUsage(usage, usage.CoverageStart)
	if err != nil || !value.Covered {
		t.Fatal("new account lifetime coverage rejected", value, err)
	}
	for _, mutate := range []func(*eventqueue.AccountQuotaUsage){func(u *eventqueue.AccountQuotaUsage) { u.AsOf = time.Time{} }, func(u *eventqueue.AccountQuotaUsage) { u.Month.TokensHeld = -1 }, func(u *eventqueue.AccountQuotaUsage) { u.CoverageStart = u.AsOf.Add(time.Second) }, func(u *eventqueue.AccountQuotaUsage) { u.TimeZone = "Local" }} {
		bad := usage
		mutate(&bad)
		if value, err := overviewMonthlyUsage(bad, start); err == nil || value != nil {
			t.Fatal("invalid usage became known zero", value, err)
		}
	}
}

func memberOverviewProofFixture(t *testing.T) (*Service, *runtimeAuthorization, []overviewAccountTarget, entity.QuotaSetting) {
	t.Helper()
	created := time.Now().UTC().Add(-time.Hour)
	targets := []overviewAccountTarget{{kind: "user", id: "usr_actor", created: created}, {kind: "team", id: "tem_one", teamID: "tem_one", membershipID: "tmm_current", created: created}, {kind: "team_member", id: teamMemberLimitScopeID("tem_one", "usr_actor"), teamID: "tem_one", membershipID: "tmm_current", created: created}}
	if err := memberOverviewPolicies(targets, nil); err != nil {
		t.Fatal(err)
	}
	s := &Service{runtime: &gatewayRuntime{}}
	setting := entity.QuotaSetting{ETag: "quota_current", TimeZone: "UTC"}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{"tem_one": {CreatedAt: created, Members: map[string]string{"usr_actor": "tmm_current"}}}, Quota: &runtimeQuotaData{Setting: setting, Currency: "USD", Created: map[string]time.Time{}, Revisions: map[string]string{}}, LimitPolicies: map[string]limits.Policy{}}
	for _, target := range targets {
		account := limitAccount(target.kind, target.id)
		auth.Quota.Created[account] = created
		auth.LimitPolicies[account] = target.policy
	}
	s.runtime.auth.Store(auth)
	return s, auth, targets, setting
}

func TestMemberOverviewAppliedRequiresCapturedCompleteCurrentProof(t *testing.T) {
	for _, name := range []string{"current", "expired", "new_publication", "calendar_revision", "calendar_zone", "currency", "missing_parent", "parent_revision", "parent_other_dimension", "creation", "unknown_creation", "membership_generation", "missing_team", "user_tombstone", "team_tombstone", "membership_tombstone", "parent_tombstone", "calendar_tombstone"} {
		t.Run(name, func(t *testing.T) {
			s, auth, targets, setting := memberOverviewProofFixture(t)
			parentAccount := limitAccount("team", "tem_one")
			switch name {
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "new_publication":
				other := *auth
				s.runtime.auth.Store(&other)
			case "calendar_revision":
				setting.ETag = "quota_new"
			case "calendar_zone":
				setting.TimeZone = "Etc/UTC"
			case "currency":
				auth.Quota.Currency = "EUR"
			case "missing_parent":
				targets[1].kind = "user"
			case "parent_revision":
				auth.Quota.Revisions[parentAccount] = "lim_other"
			case "parent_other_dimension":
				changed := auth.LimitPolicies[parentAccount]
				changed.RPM = limitNumber(5)
				auth.LimitPolicies[parentAccount] = changed
			case "creation":
				auth.Quota.Created[parentAccount] = targets[1].created.Add(time.Millisecond)
			case "unknown_creation":
				targets[2].created = time.Time{}
				auth.Teams["tem_one"] = runtimeTeam{CreatedAt: time.Time{}, Members: map[string]string{"usr_actor": "tmm_current"}}
				auth.Quota.Created[limitAccount(targets[2].kind, targets[2].id)] = time.Time{}
			case "membership_generation":
				team := auth.Teams["tem_one"]
				team.Members["usr_actor"] = "tmm_rejoined"
			case "missing_team":
				delete(auth.Teams, "tem_one")
			case "user_tombstone":
				s.runtime.deniedUsers.Store("usr_actor", uint64(1))
			case "team_tombstone":
				s.runtime.deniedTeams.Store("tem_one", uint64(1))
			case "membership_tombstone":
				s.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey("tem_one", "usr_actor"), uint64(1))
			case "parent_tombstone":
				s.runtime.deniedLimits.Store(parentAccount, uint64(1))
			case "calendar_tombstone":
				s.runtime.deniedLimits.Store("quota_settings", uint64(1))
			}
			if got := s.memberOverviewApplied(auth, "usr_actor", targets[2], targets, setting, "USD"); got != (name == "current") {
				t.Fatal("stale runtime proved current member policy", name, got)
			}
		})
	}
}

func TestMemberOverviewAvailabilityKeepsNullZeroAndUsageSeparate(t *testing.T) {
	s, auth, targets, setting := memberOverviewProofFixture(t)
	zero, money := int64(0), "0"
	targets[0].policy.TokensMonth, targets[0].policy.MoneyMonth, targets[0].policy.Currency = &zero, &money, "USD"
	now := time.Now().UTC()
	account := limitAccount(targets[0].kind, targets[0].id)
	batch := &eventqueue.QuotaUsageBatch{Active: true, AsOf: now, CoverageStart: targets[0].created.Add(-time.Second), TimeZone: "UTC", Accounts: map[string]eventqueue.AccountQuotaUsage{account: {AsOf: now, CoverageStart: targets[0].created.Add(-time.Second), TimeZone: "UTC", Month: eventqueue.QuotaUsage{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}}}}
	for _, name := range []string{"active_stale_policy", "inactive", "outage", "missing_account"} {
		t.Run(name, func(t *testing.T) {
			copyBatch := *batch
			selected := &copyBatch
			want := "unavailable"
			switch name {
			case "active_stale_policy":
				want = "active"
			case "inactive":
				copyBatch.Active = false
				want = "inactive"
			case "outage":
				selected = nil
			case "missing_account":
				copyBatch.Accounts = nil
			}
			value := s.memberOverviewMonthlyAccount(targets[0], targets, selected, auth, "usr_actor", setting, "USD")
			if value.UsageStatus != want || (value.Usage != nil) != (want == "active") || (value.ActiveReservations != nil) != (want == "active") || value.RuntimeApplied || value.TokensMonth == nil || *value.TokensMonth != "0" || value.MoneyMonth == nil || *value.MoneyMonth != "0" || value.Currency == nil || *value.Currency != "USD" {
				t.Fatal("outage/inactive invented zero, stale policy applied, or finite zero erased", value)
			}
		})
	}
	value := s.memberOverviewMonthlyAccount(targets[1], targets, nil, nil, "usr_actor", setting, "USD")
	raw, err := json.Marshal(value)
	if err != nil || value.TokensMonth != nil || value.MoneyMonth != nil || value.Currency != nil || strings.Contains(string(raw), "reason") || strings.Contains(string(raw), "user_id") {
		t.Fatal("unlimited cap or private policy metadata changed", string(raw), err)
	}
}

func TestMemberOverviewReservationsRemainSeparateExactAndAccountBound(t *testing.T) {
	s, auth, targets, setting := memberOverviewProofFixture(t)
	now := time.Now().UTC()
	batch := &eventqueue.QuotaUsageBatch{Active: true, AsOf: now, CoverageStart: targets[0].created.Add(-time.Second), TimeZone: "UTC", Accounts: map[string]eventqueue.AccountQuotaUsage{}}
	for index, target := range targets {
		batch.Accounts[limitAccount(target.kind, target.id)] = eventqueue.AccountQuotaUsage{
			AsOf: now, CoverageStart: batch.CoverageStart, TimeZone: "UTC",
			Month:  eventqueue.QuotaUsage{TokensUsed: 5, TokensHeld: 2, MoneyUsed: map[string]string{"USD": "5"}, MoneyHeld: map[string]string{"USD": "2"}},
			Active: eventqueue.QuotaUsage{TokensHeld: 9007199254740993 + int64(index), MoneyHeld: map[string]string{"USD": "5.000000000000000002"}},
		}
	}
	for index, target := range targets {
		value := s.memberOverviewMonthlyAccount(target, targets, batch, auth, "usr_actor", setting, "USD")
		if value.UsageStatus != "active" || value.Usage == nil || !value.Usage.AsOf.Equal(batch.AsOf) || value.Usage.TokensUsed != "5" || value.Usage.TokensHeld != "2" || value.Usage.MoneyHeld["USD"] != "2" || value.ActiveReservations == nil || value.ActiveReservations.TokensHeld != strconv.FormatInt(9007199254740993+int64(index), 10) || value.ActiveReservations.MoneyHeld["USD"] != "5.000000000000000002" {
			t.Fatal("monthly/live or aggregate/member facts were combined or rounded", value)
		}
		value.ActiveReservations.MoneyHeld["USD"] = "999"
		if batch.Accounts[value.AccountID].Active.MoneyHeld["USD"] != "5.000000000000000002" {
			t.Fatal("reservation DTO aliases captured journal state")
		}
	}
	account := limitAccount(targets[0].kind, targets[0].id)
	invalid := batch.Accounts[account]
	invalid.Active.TokensHeld = -1
	batch.Accounts[account] = invalid
	value := s.memberOverviewMonthlyAccount(targets[0], targets, batch, auth, "usr_actor", setting, "USD")
	if value.UsageStatus != "unavailable" || value.Usage != nil || value.ActiveReservations != nil || value.RuntimeApplied {
		t.Fatal("invalid live snapshot became a partial or applied claim", value)
	}
}
