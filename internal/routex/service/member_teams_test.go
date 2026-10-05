package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

const memberTeamsActor = "usr_01j00000000000000000000000"
const memberTeamsSubject = "usr_01j00000000000000000000001"
const memberTeamsTeam = "tea_01j00000000000000000000000"
const memberTeamsRelation = "tmm_01j00000000000000000000000"

func memberTeamsRow() memberTeamIdentity {
	return memberTeamIdentity{ID: memberTeamsTeam, ActualTeamID: memberTeamsTeam, Name: "Retained Team", Status: entity.ResourceActive, MembershipID: memberTeamsRelation, MembershipUserID: memberTeamsSubject, MembershipRole: entity.TeamMember, MembershipStatus: entity.ResourceActive, CreatedAt: time.Now().UTC().Add(-time.Hour)}
}

func TestMemberTeamsCursorBindsActorSubjectAndCanonicalPage(t *testing.T) {
	cursor := memberTeamsCursor(memberTeamsActor, memberTeamsSubject, memberTeamsTeam)
	if len(cursor) != 123 {
		t.Fatal("maximum valid IDs exceeded cursor budget", len(cursor))
	}
	normalized, after, err := normalizeMemberTeamsFilter(memberTeamsActor, memberTeamsSubject, MemberTeamsFilter{Cursor: cursor, Limit: 50})
	if err != nil || after != memberTeamsTeam || normalized.Limit != 50 {
		t.Fatal(normalized, after, err)
	}
	normalized, _, err = normalizeMemberTeamsFilter(memberTeamsActor, memberTeamsSubject, MemberTeamsFilter{})
	if err != nil || normalized.Limit != 20 {
		t.Fatal(normalized, err)
	}
	for _, bad := range []string{memberTeamsCursor(memberTeamsSubject, memberTeamsSubject, memberTeamsTeam), memberTeamsCursor(memberTeamsActor, memberTeamsActor, memberTeamsTeam), memberTeamsCursor(memberTeamsActor, memberTeamsSubject, strings.ToUpper(memberTeamsTeam)), cursor + "=", strings.Repeat("a", 129), base64.RawURLEncoding.EncodeToString([]byte(memberTeamsActor + "|" + memberTeamsSubject + "|" + memberTeamsTeam + "|extra"))} {
		if _, _, err := normalizeMemberTeamsFilter(memberTeamsActor, memberTeamsSubject, MemberTeamsFilter{Cursor: bad}); err != apperrors.ErrBadRequest {
			t.Fatal("foreign or malformed cursor accepted", bad, err)
		}
	}
	for _, limit := range []int{-1, 51, 100} {
		if _, _, err := normalizeMemberTeamsFilter(memberTeamsActor, memberTeamsSubject, MemberTeamsFilter{Limit: limit}); err != apperrors.ErrBadRequest {
			t.Fatal(limit, err)
		}
	}
	for _, bad := range []string{"", memberTeamsSubject + " ", strings.ToUpper(memberTeamsSubject), "usr_short"} {
		if _, err := (&Service{}).ListMemberTeams(context.Background(), memberTeamsActor, bad, MemberTeamsFilter{}); err != apperrors.ErrNotFound {
			t.Fatal("unsafe target reached database", bad, err)
		}
	}
}

func TestMemberTeamsPagesRetainedRelationsBeforeBoundedExactLookup(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			query := memberTeamsQuery(db, memberTeamsSubject, memberTeamsTeam, 50)
			query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "ORDER BY", "LIMIT"}
			callbacks.BuildQuerySQL(query)
			sql := query.Statement.SQL.String()
			if !strings.Contains(sql, "team_memberships") || strings.Contains(sql, "JOIN") || strings.Contains(sql, "status =") || strings.Contains(sql, "active") || !strings.Contains(sql, "joined_at") || !strings.Contains(sql, "LIMIT") {
				t.Fatal("retained relations silently filtered or unbounded", sql)
			}
			if !reflect.DeepEqual(query.Statement.Vars, []any{memberTeamsSubject, memberTeamsTeam, 51}) {
				t.Fatal(sql, query.Statement.Vars)
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") != 2 {
				t.Fatal("subject lookup lost exact collation", sql)
			}
			rows := []memberTeamIdentity{memberTeamsRow()}
			if err := hydrateMemberTeams(rows, nil); err != apperrors.ErrInternal {
				t.Fatal("orphan relation disappeared", err)
			}
			team := entity.Team{ID: memberTeamsTeam, Name: "Retained Team", Status: entity.ResourceDisabled, CreatedAt: rows[0].CreatedAt}
			for _, teams := range [][]entity.Team{{team, team}, {{ID: strings.ToUpper(memberTeamsTeam)}}, {{ID: "tea_foreign"}}} {
				if err := hydrateMemberTeams(rows, teams); err != apperrors.ErrInternal {
					t.Fatal("duplicate or aliased Team accepted", teams, err)
				}
			}
			if err := hydrateMemberTeams(rows, []entity.Team{team}); err != nil || rows[0].Status != entity.ResourceDisabled {
				t.Fatal(rows, err)
			}
		})
	}
}

func TestMemberTeamsIntegrityAndHistoricalNull(t *testing.T) {
	now := time.Now().UTC()
	for _, name := range []string{"historical_null", "known_join", "disabled", "archived", "foreign_user", "team_alias", "relation_alias", "duplicate_team", "duplicate_relationship", "role", "membership_status", "team_status", "missing_creation", "future_creation", "zero_join", "before_creation", "future_join"} {
		t.Run(name, func(t *testing.T) {
			rows := []memberTeamIdentity{memberTeamsRow()}
			row := &rows[0]
			switch name {
			case "known_join":
				stamp := row.CreatedAt.Add(time.Minute)
				row.JoinedAt = &stamp
			case "disabled":
				row.Status = entity.ResourceDisabled
				row.MembershipStatus = entity.ResourceDisabled
			case "archived":
				row.Status = entity.ResourceArchived
			case "foreign_user":
				row.MembershipUserID = memberTeamsActor
			case "team_alias":
				row.ActualTeamID = strings.ToUpper(row.ID)
			case "relation_alias":
				row.MembershipID = strings.ToUpper(row.MembershipID)
			case "duplicate_team":
				rows = append(rows, *row)
			case "duplicate_relationship":
				other := *row
				other.ID = "tea_01j00000000000000000000001"
				other.ActualTeamID = other.ID
				rows = append(rows, other)
			case "role":
				row.MembershipRole = "manager"
			case "membership_status":
				row.MembershipStatus = entity.ResourceArchived
			case "team_status":
				row.Status = "ACTIVE"
			case "missing_creation":
				row.CreatedAt = time.Time{}
			case "future_creation":
				row.CreatedAt = now.Add(time.Hour)
			case "zero_join":
				stamp := time.Time{}
				row.JoinedAt = &stamp
			case "before_creation":
				stamp := row.CreatedAt.Add(-time.Second)
				row.JoinedAt = &stamp
			case "future_join":
				stamp := now.Add(time.Hour)
				row.JoinedAt = &stamp
			}
			valid := name == "historical_null" || name == "known_join" || name == "disabled" || name == "archived"
			if err := validateMemberTeams(rows, memberTeamsSubject, now); (err == nil) != valid {
				t.Fatal(name, err)
			}
		})
	}
	row := memberTeamsRow()
	row.JoinedAt = nil
	if err := validateMemberTeamRelations([]memberTeamIdentity{row}, memberTeamsSubject); err != nil {
		t.Fatal("historical date invented or required", err)
	}
}

func memberTeamsProofFixture(t *testing.T) (*Service, *runtimeAuthorization, []overviewAccountTarget, entity.QuotaSetting, entity.User, memberTeamIdentity) {
	t.Helper()
	row := memberTeamsRow()
	subject := entity.User{ID: memberTeamsSubject, Role: entity.RoleMember, CreatedAt: row.CreatedAt.Add(-time.Hour)}
	targets := memberTeamsTargets([]memberTeamIdentity{row}, subject.ID)
	if err := memberOverviewPolicies(targets, nil); err != nil {
		t.Fatal(err)
	}
	setting := entity.QuotaSetting{ETag: "calendar_current", TimeZone: "UTC"}
	_, admission := registrationAdmission(subject, nil)
	auth := &runtimeAuthorization{UserAdmissions: map[string]runtimeAdmissionProof{subject.ID: admission}, ValidUntil: time.Now().Add(time.Minute), UserProofs: map[string]runtimeUserProof{subject.ID: {CreatedAt: subject.CreatedAt, Enabled: true}}, Teams: map[string]runtimeTeam{row.ID: {CreatedAt: row.CreatedAt, Members: map[string]string{subject.ID: row.MembershipID}}}, Quota: &runtimeQuotaData{Setting: setting, Currency: "USD", Created: map[string]time.Time{}, Revisions: map[string]string{}}, LimitPolicies: map[string]limits.Policy{}}
	for _, target := range targets {
		account := limitAccount(target.kind, target.id)
		auth.Quota.Created[account] = target.created
		auth.LimitPolicies[account] = target.policy
	}
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}}
	s.runtime.auth.Store(auth)
	until := auth.ValidUntil
	s.runtime.status.Store(&RuntimeStatus{Ready: true, AuthorizationValidUntil: &until})
	return s, auth, targets, setting, subject, row
}

func TestMemberTeamsRuntimeApplicationIsExactSubjectAndStablePairProof(t *testing.T) {
	for _, name := range []string{"current", "missing_subject", "subject_alias", "subject_disabled", "offboarded", "subject_generation", "team_disabled", "membership_disabled", "membership_generation", "missing_team", "expired", "publication_changed", "runtime_unready", "calendar", "currency", "parent_revision", "child_revision", "parent_missing", "parent_policy", "user_tombstone", "team_tombstone", "member_tombstone", "parent_tombstone", "actor_tombstone"} {
		t.Run(name, func(t *testing.T) {
			s, auth, targets, setting, subject, row := memberTeamsProofFixture(t)
			parent := limitAccount(targets[0].kind, targets[0].id)
			switch name {
			case "missing_subject":
				delete(auth.UserProofs, subject.ID)
			case "subject_alias":
				delete(auth.UserProofs, subject.ID)
				auth.UserProofs[strings.ToUpper(subject.ID)] = runtimeUserProof{CreatedAt: subject.CreatedAt, Enabled: true}
			case "subject_disabled":
				subject.Disabled = true
			case "offboarded":
				stamp := time.Now()
				subject.OffboardedAt = &stamp
			case "subject_generation":
				subject.CreatedAt = subject.CreatedAt.Add(time.Microsecond)
			case "team_disabled":
				row.Status = entity.ResourceDisabled
			case "membership_disabled":
				row.MembershipStatus = entity.ResourceDisabled
			case "membership_generation":
				team := auth.Teams[row.ID]
				team.Members[subject.ID] = "tmm_other"
			case "missing_team":
				delete(auth.Teams, row.ID)
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "publication_changed":
				other := *auth
				s.runtime.auth.Store(&other)
			case "runtime_unready":
				s.runtime.status.Store(&RuntimeStatus{})
			case "calendar":
				setting.ETag = "changed"
			case "currency":
				auth.Quota.Currency = "EUR"
			case "parent_revision":
				auth.Quota.Revisions[parent] = "new"
			case "child_revision":
				auth.Quota.Revisions[limitAccount(targets[1].kind, targets[1].id)] = "new"
			case "parent_missing":
				targets[0].kind = "user"
			case "parent_policy":
				p := auth.LimitPolicies[parent]
				p.RPM = limitNumber(1)
				auth.LimitPolicies[parent] = p
			case "user_tombstone":
				s.runtime.deniedUsers.Store(subject.ID, uint64(1))
			case "team_tombstone":
				s.runtime.deniedTeams.Store(row.ID, uint64(1))
			case "member_tombstone":
				s.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey(row.ID, subject.ID), uint64(1))
			case "parent_tombstone":
				s.runtime.deniedLimits.Store(parent, uint64(1))
			case "actor_tombstone":
				s.runtime.deniedUsers.Store(memberTeamsActor, uint64(1))
			}
			if got := s.memberTeamsApplied(auth, subject, row, targets[1], targets, setting, "USD", nil); got != (name == "current" || name == "actor_tombstone") {
				t.Fatal("reader or stale publication changed subject proof", name, got)
			}
		})
	}
}

func TestMemberTeamsCountersAndPoliciesStaySeparateAcrossRejoin(t *testing.T) {
	s, auth, targets, setting, subject, row := memberTeamsProofFixture(t)
	oldScope := targets[1].id
	newRow := row
	newRow.MembershipID = "tmm_01j00000000000000000000001"
	newRow.JoinedAt = newMemberTeamJoinedAt(time.Now())
	renewed := memberTeamsTargets([]memberTeamIdentity{newRow}, subject.ID)
	if renewed[1].id != oldScope || !renewed[1].created.Equal(row.CreatedAt) {
		t.Fatal("rejoin reset stable quota identity or coverage", renewed)
	}
	zero := int64(0)
	money := "1.000000000000000001"
	targets[1].policy = limits.Policy{TokensMonth: &zero, MoneyMonth: &money, Currency: "USD", RPM: limitNumber(9007199254740993)}
	now := time.Now().UTC()
	batch := &eventqueue.QuotaUsageBatch{Active: true, AsOf: now, TimeZone: "UTC", CoverageStart: row.CreatedAt, Accounts: map[string]eventqueue.AccountQuotaUsage{limitAccount(targets[1].kind, oldScope): {AsOf: now, TimeZone: "UTC", CoverageStart: row.CreatedAt, Month: eventqueue.QuotaUsage{TokensUsed: 9007199254740993, TokensUnknown: 1, MoneyUsed: map[string]string{"EUR": "0.000000000000000001"}}, Active: eventqueue.QuotaUsage{TokensHeld: 5, MoneyHeld: map[string]string{"USD": "2.000000000000000001"}}}}}
	account := s.memberOverviewMonthlyAccount(targets[1], targets, batch, auth, subject.ID, setting, "USD")
	values := memberTeamPolicyValues(targets[1].policy)
	subject.Disabled = true
	record := MemberTeamRecord{ID: row.ID, JoinedAt: nil, Limits: MemberTeamLimits{PolicyRecorded: true, Stored: values, ParentStored: memberTeamPolicyValues(targets[0].policy), Usage: account.Usage, UsageStatus: account.UsageStatus, ActiveReservations: account.ActiveReservations, RuntimeApplied: s.memberTeamsApplied(auth, subject, row, targets[1], targets, setting, "USD", nil)}}
	raw, err := json.Marshal(record)
	if err != nil || record.Limits.RuntimeApplied || account.Usage == nil || account.Usage.TokensUsed != "9007199254740993" || account.Usage.TokensUnknown != "1" || account.ActiveReservations.TokensHeld != "5" || values.TokensMonth == nil || *values.TokensMonth != "0" || *values.MoneyMonth != money || *values.RPM != "9007199254740993" || record.Limits.ParentStored.TokensMonth != nil || !strings.Contains(string(raw), `"joined_at":null`) {
		t.Fatal(string(raw), err)
	}
	for _, forbidden := range []string{"email", "password", "token_hash", "aggregate", "remaining", "secret", "actor_user_id"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("unrelated or invented facts exposed", string(raw))
		}
	}
	if plain := memberTeamPolicyValues(limits.Policy{Currency: "USD"}); plain.Currency != nil || plain.MoneyMonth != nil || plain.RPM != nil {
		t.Fatal("absent money policy invented denomination", plain)
	}
	for _, unavailable := range []*eventqueue.QuotaUsageBatch{nil, {Active: false}} {
		value := s.memberOverviewMonthlyAccount(targets[1], targets, unavailable, auth, subject.ID, setting, "USD")
		if value.Usage != nil || value.ActiveReservations != nil || value.RuntimeApplied || value.MoneyMonth == nil {
			t.Fatal("outage invented zero or erased retained policy", value)
		}
	}
}

func TestMemberTeamsJoinedAtDoesNotChangeExistingPolicyReview(t *testing.T) {
	row := memberTeamsRow()
	team := entity.Team{ID: row.ID, Status: row.Status, CreatedAt: row.CreatedAt}
	member := entity.TeamMembership{ID: row.MembershipID, TeamID: row.ID, UserID: row.MembershipUserID, Role: row.MembershipRole, Status: row.MembershipStatus}
	pricing := entity.PricingSetting{ETag: "pricing", PlatformCurrency: "USD"}
	before, err := teamLimitReviewETag(team, &member, entity.ResourceLimit{}, limits.Policy{}, entity.ResourceLimit{}, limits.Policy{}, pricing)
	if err != nil {
		t.Fatal(err)
	}
	member.JoinedAt = newMemberTeamJoinedAt(time.Now())
	after, err := teamLimitReviewETag(team, &member, entity.ResourceLimit{}, limits.Policy{}, entity.ResourceLimit{}, limits.Policy{}, pricing)
	if err != nil || after != before {
		t.Fatal("joined-at metadata silently invalidated unrelated review", before, after, err)
	}
}

func TestMemberTeamsWriterPreservesLegacyGenerationAndNull(t *testing.T) {
	now := time.Now().In(time.FixedZone("legacy", 8*60*60))
	stamp := newMemberTeamJoinedAt(now)
	if stamp.Location() != time.UTC || stamp.Nanosecond()%1000 != 0 || !stamp.Equal(now.Truncate(time.Microsecond)) {
		t.Fatal("new generation timestamp is not portable UTC precision", stamp)
	}
	historical := entity.TeamMembership{ID: "tmm_historical", TeamID: "tea_historical", UserID: "usr_historical", JoinedAt: nil}
	recorded := entity.TeamMembership{ID: "tmm_recorded", TeamID: historical.TeamID, UserID: "usr_recorded", JoinedAt: stamp}
	rows := []entity.TeamMembership{historical, recorded}
	retained, err := validateRetainedMemberTeamGenerations(rows, historical.TeamID)
	if err != nil || !reflect.DeepEqual(retained[historical.UserID], historical) || !reflect.DeepEqual(retained[recorded.UserID], recorded) {
		t.Fatal("writer rewrote old IDs/date/null", retained, err)
	}
	for _, bad := range [][]entity.TeamMembership{{historical, historical}, {{ID: historical.ID, TeamID: "tea_foreign", UserID: historical.UserID}}, {{ID: recorded.ID, TeamID: historical.TeamID, UserID: historical.UserID}, historical}, make([]entity.TeamMembership, 1001)} {
		if _, err := validateRetainedMemberTeamGenerations(bad, historical.TeamID); err != apperrors.ErrInternal {
			t.Fatal("writer accepted ambiguous ownership", err)
		}
	}
}

func TestMemberTeamsMaximumPageUsesOneHundredIndependentAccounts(t *testing.T) {
	rows := make([]memberTeamIdentity, 50)
	for index := range rows {
		rows[index] = memberTeamsRow()
		rows[index].ID = fmt.Sprintf("tea_%026d", index)
		rows[index].ActualTeamID = rows[index].ID
		rows[index].MembershipID = fmt.Sprintf("tmm_%026d", index)
	}
	targets := memberTeamsTargets(rows, memberTeamsSubject)
	if len(targets) != 100 {
		t.Fatal("maximum page escaped journal batch budget", len(targets))
	}
	seen := map[string]bool{}
	for index, target := range targets {
		account := limitAccount(target.kind, target.id)
		if seen[account] || target.kind == "user" || target.kind == "project" || target.kind == "key" || !target.created.Equal(rows[index/2].CreatedAt) {
			t.Fatal("borrowed/summed account or invented coverage", target)
		}
		seen[account] = true
	}
	if err := memberOverviewPolicies(targets, nil); err != nil {
		t.Fatal(err)
	}
	bad := entity.ResourceLimit{ScopeKind: "team_member", ScopeID: strings.ToLower(targets[1].id), ETag: "foreign"}
	if bad.ScopeID == targets[1].id {
		bad.ScopeID += " "
	}
	if err := memberOverviewPolicies(targets, []entity.ResourceLimit{bad}); err != apperrors.ErrInternal {
		t.Fatal("aliased parent/child policy accepted", err)
	}
	duplicate := entity.ResourceLimit{ScopeKind: targets[0].kind, ScopeID: targets[0].id, ETag: "revision"}
	if err := memberOverviewPolicies(targets, []entity.ResourceLimit{duplicate, duplicate}); err != apperrors.ErrInternal {
		t.Fatal("duplicate policy was summed", err)
	}
}
