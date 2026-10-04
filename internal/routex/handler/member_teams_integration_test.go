package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

// Source fixture only. Root runs it after exact endpoint/V53/harness carry.
func testMemberTeamsLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"member-teams-admin@example.invalid","password":"member-teams-password","name":"Teams administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	subject, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-teams-subject", nil)
	peer, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-teams-peer", nil)
	_, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-teams-reader", []string{"members.read"})
	_, directoryCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-teams-directory", []string{"teams.read_all"})
	both, bothCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-teams-both", []string{"members.read", "teams.read_all"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-teams-writer", []string{"members.write", "teams.write", "limits.users.write"})
	endpoint := "/api/v1/admin/members/" + subject.User.ID + "/teams"
	for _, cookie := range []*http.Cookie{readerCookie, directoryCookie, writerCookie} {
		response := identityRequest(router, "GET", endpoint, "", cookie, "")
		expectStatus(t, response, 403)
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private denial is cacheable")
		}
	}
	expectStatus(t, identityRequest(router, "GET", endpoint, "", nil, ""), 401)
	get := func(cookie *http.Cookie, path string) service.MemberTeamsPage {
		t.Helper()
		response := identityRequest(router, "GET", path, "", cookie, "")
		expectStatus(t, response, 200)
		var page service.MemberTeamsPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.UserID != subject.User.ID || page.Items == nil || page.ObservedAt.IsZero() || page.PlatformCurrency == "" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("incomplete/private-context response", page, response.Header())
		}
		for _, forbidden := range []string{`"email"`, `"password"`, `"secret"`, `"actor_user_id"`, `"aggregate"`, `"remaining"`} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatal("unrelated or invented facts", forbidden)
			}
		}
		return page
	}
	if page := get(bothCookie, endpoint); len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatal("empty subject borrowed actor directory", page)
	}
	newTeam := func(name string, owners []string) *service.ResourceRecord {
		t.Helper()
		team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, name, "", owners)
		if err != nil {
			t.Fatal(err)
		}
		return team
	}
	read := func(teamID, userID string) entity.TeamMembership {
		t.Helper()
		var row entity.TeamMembership
		if err := db.Where("team_id = ? AND user_id = ?", teamID, userID).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	set := func(teamID string, inputs []service.TeamMemberInput) {
		t.Helper()
		if _, err := svc.SetTeamMembers(ctx, admin.User.ID, teamID, inputs); err != nil {
			t.Fatal(err)
		}
	}
	teamA := newTeam("Recorded membership", []string{subject.User.ID})
	created := read(teamA.ID, subject.User.ID)
	if created.JoinedAt == nil || created.JoinedAt.IsZero() {
		t.Fatal("CreateResource omitted explicit new join")
	}
	teamB := newTeam("Historical membership", []string{admin.User.ID})
	set(teamB.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: subject.User.ID, Role: entity.TeamMember, Status: entity.ResourceDisabled}})
	historical := read(teamB.ID, subject.User.ID)
	if err := db.Model(&memberTeamsJoinedAtFixture{}).Where("id = ?", historical.ID).Update("joined_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	historical = read(teamB.ID, subject.User.ID)
	actorTeam := newTeam("Actor-only Team", []string{both.User.ID})
	_ = actorTeam
	set(teamA.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: subject.User.ID, Role: entity.TeamMember, Status: entity.ResourceDisabled}})
	if after := read(teamA.ID, subject.User.ID); after.ID != created.ID || after.JoinedAt == nil || !after.JoinedAt.Equal(*created.JoinedAt) {
		t.Fatal("role/status replacement reset recorded generation", after)
	}
	set(teamB.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: subject.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}})
	if after := read(teamB.ID, subject.User.ID); after.ID != historical.ID || after.JoinedAt != nil {
		t.Fatal("replacement invented historical join", after)
	}
	zero := int64(0)
	money := "1.000000000000000001"
	rpm := int64(3)
	parent := int64(100)
	// Resolve the released exact pair account instead of inventing a scope.
	review, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, teamB.ID, subject.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	scopeID := strings.TrimPrefix(review.AccountID, "team_member_")
	if scopeID == review.AccountID || scopeID == "" {
		t.Fatal("missing authoritative pair account", review)
	}
	policy := entity.ResourceLimit{ScopeKind: "team_member", ScopeID: scopeID, ETag: "lim_member_teams", TokensMonth: &zero, MoneyMonth: &money, Currency: "USD", RPM: &rpm}
	// Team creation already persists its default policy. The review above only
	// reads the pair policy; seed just these controls without rewriting defaults,
	// actor/reason metadata, ownership or recorded timestamps of an existing row.
	seedPolicy := func(desired entity.ResourceLimit) {
		t.Helper()
		var before entity.ResourceLimit
		err := db.Where("scope_kind = ? AND scope_id = ?", desired.ScopeKind, desired.ScopeID).First(&before).Error
		missing := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missing {
			t.Fatal(err)
		}
		expected := desired
		if missing {
			if err := db.Create(&desired).Error; err != nil {
				t.Fatal(err)
			}
		} else {
			if before.ScopeKind != desired.ScopeKind || before.ScopeID != desired.ScopeID {
				t.Fatal("fixture policy lookup changed exact ownership")
			}
			expected = before
			expected.ETag, expected.TokensMonth = desired.ETag, desired.TokensMonth
			fields := map[string]any{"ETag": desired.ETag, "tokens_month": desired.TokensMonth}
			if desired.ScopeKind == "team_member" {
				expected.MoneyMonth, expected.Currency, expected.RPM = desired.MoneyMonth, desired.Currency, desired.RPM
				fields["money_month"], fields["currency"], fields["rpm"] = desired.MoneyMonth, desired.Currency, desired.RPM
			}
			result := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", before.ScopeKind, before.ScopeID).UpdateColumns(fields)
			if result.Error != nil || result.RowsAffected != 1 {
				t.Fatal("fixture policy update was not one exact row", result.RowsAffected, result.Error)
			}
		}
		var saved entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", desired.ScopeKind, desired.ScopeID).First(&saved).Error; err != nil {
			t.Fatal(err)
		}
		if missing {
			if saved.UpdatedAt.IsZero() {
				t.Fatal("new fixture policy has no persisted timestamp")
			}
			expected.UpdatedAt = saved.UpdatedAt
		}
		if !reflect.DeepEqual(saved, expected) {
			t.Fatal("fixture policy changed unintended fields or failed exact readback", saved, expected)
		}
	}
	seedPolicy(policy)
	seedPolicy(entity.ResourceLimit{ScopeKind: "team", ScopeID: teamB.ID, ETag: "lim_member_teams_parent", TokensMonth: &parent})
	page := get(bothCookie, endpoint)
	if len(page.Items) != 2 || page.NextCursor != nil {
		t.Fatal("target scope borrowed actor membership", page)
	}
	find := func(page service.MemberTeamsPage, teamID string) service.MemberTeamRecord {
		t.Helper()
		for _, row := range page.Items {
			if row.ID == teamID {
				return row
			}
		}
		t.Fatal("retained membership missing", teamID)
		return service.MemberTeamRecord{}
	}
	record := find(page, teamB.ID)
	if record.JoinedAt != nil || !record.Limits.PolicyRecorded || record.Limits.Stored.TokensMonth == nil || *record.Limits.Stored.TokensMonth != "0" || record.Limits.Stored.MoneyMonth == nil || *record.Limits.Stored.MoneyMonth != money || record.Limits.Stored.RPM == nil || *record.Limits.Stored.RPM != "3" || record.Limits.ParentStored.TokensMonth == nil || *record.Limits.ParentStored.TokensMonth != "100" || record.Limits.Usage != nil || record.Limits.ActiveReservations != nil || record.Limits.RuntimeApplied || record.Limits.UsageStatus != "unavailable" {
		t.Fatal("cold policy projection erased zero/null, precision or independence", record)
	}
	// A live journal/current publication proves the target pair, not the reader.
	journalPath := filepath.Join(t.TempDir(), "member-teams.db")
	// This owned fixture journal contains no calls. Its late coverage start is
	// explicit; it is not proof of native dispatch or complete historical usage.
	journal, err := eventqueue.Open(journalPath, 4096, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.EnableQuota("UTC", time.Now().UTC()); err != nil {
		_ = journal.Close()
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("AccountingStarted", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, journalPath); err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	current := find(get(bothCookie, endpoint), teamB.ID)
	if !current.Limits.RuntimeApplied || current.Limits.UsageStatus != "active" || current.Limits.Usage == nil || current.Limits.ActiveReservations == nil || current.Limits.Usage.TokensUsed != "0" || current.Limits.Usage.TokensUnknown != "0" || current.Limits.ActiveReservations.TokensHeld != "0" {
		t.Fatal("exact current subject pair was not proven by actual journal/publication", current)
	}
	if current.Limits.Usage.Covered {
		t.Fatal("late journal initialization invented historical coverage")
	}
	inactive := find(get(bothCookie, endpoint), teamA.ID)
	if inactive.Limits.RuntimeApplied || inactive.Limits.Usage == nil {
		t.Fatal("disabled membership hid retained usage or claimed application", inactive)
	}
	first := get(bothCookie, endpoint+"?limit=1")
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatal("bounded page claimed completeness", first)
	}
	second := get(bothCookie, endpoint+"?limit=1&cursor="+*first.NextCursor)
	if len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].ID == first.Items[0].ID {
		t.Fatal("keyset page repeated or lost relation", second)
	}
	want := []string{teamA.ID, teamB.ID}
	sort.Strings(want)
	if first.Items[0].ID != want[0] || second.Items[0].ID != want[1] {
		t.Fatal("unstable Team order", first, second)
	}
	expectStatus(t, identityRequest(router, "GET", endpoint+"?cursor="+*first.NextCursor, "", adminCookie, ""), 400)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members/"+peer.User.ID+"/teams?cursor="+*first.NextCursor, "", bothCookie, ""), 400)
	for _, query := range []string{"?q=name", "?user_id=" + peer.User.ID, "?team_id=" + actorTeam.ID, "?limit=51", "?limit=01", "?cursor="} {
		expectStatus(t, identityRequest(router, "GET", endpoint+query, "", bothCookie, ""), 400)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	page = get(bothCookie, endpoint)
	if len(page.Items) != 2 || find(page, teamB.ID).Limits.RuntimeApplied {
		t.Fatal("disabled subject hid retained metadata or claimed enforcement", page)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// Inactive/archived Team status never removes retained administrative facts.
	for _, status := range []string{entity.ResourceDisabled, entity.ResourceArchived, entity.ResourceActive} {
		if err := db.Model(&entity.Team{}).Where("id = ?", teamB.ID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		inactive := find(get(bothCookie, endpoint), teamB.ID)
		if inactive.Status != status || !inactive.Limits.PolicyRecorded || status != entity.ResourceActive && inactive.Limits.RuntimeApplied {
			t.Fatal("inactive Team lost retained facts or claimed runtime application", inactive)
		}
	}
	// Removed/readded generation changes descriptive identity only, not saved pair policy.
	set(teamB.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}})
	set(teamB.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: subject.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}})
	rejoined := read(teamB.ID, subject.User.ID)
	if rejoined.ID == historical.ID || rejoined.JoinedAt == nil {
		t.Fatal("new generation reused unknown historical join", rejoined)
	}
	record = find(get(bothCookie, endpoint), teamB.ID)
	if !record.Limits.PolicyRecorded || record.Limits.Stored.TokensMonth == nil || *record.Limits.Stored.TokensMonth != "0" {
		t.Fatal("rejoin reset stable-pair policy", record)
	}
	// An unrefreshed saved revision remains readable but cannot claim application.
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", scopeID).Update("ETag", "lim_member_teams_new").Error; err != nil {
		t.Fatal(err)
	}
	stale := find(get(bothCookie, endpoint), teamB.ID)
	if stale.Limits.RuntimeApplied || stale.Limits.PolicyETag != "lim_member_teams_new" || stale.Limits.Usage == nil {
		t.Fatal("stale publication erased facts or proved saved revision", stale)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if refreshed := find(get(bothCookie, endpoint), teamB.ID); !refreshed.Limits.RuntimeApplied {
		t.Fatal("fresh exact revision failed publication proof", refreshed)
	}
	// Orphan and aliased retained data is an integrity failure, never a dropped row.
	orphanID, err := id.NewPrefixed("tea")
	if err != nil {
		t.Fatal(err)
	}
	relationID, err := id.NewPrefixed("tmm")
	if err != nil {
		t.Fatal(err)
	}
	orphan := entity.TeamMembership{ID: relationID, TeamID: orphanID, UserID: subject.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}
	// Released foreign keys reject new orphan rows on both supported drivers.
	// Service-level corrupt-history rejection remains covered by the pure
	// hydration tests; do not disable a real constraint to manufacture it here.
	if err := db.Create(&orphan).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("orphan membership was not rejected by the released foreign key", err)
	}
	var orphanCount int64
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", orphan.ID).Count(&orphanCount).Error; err != nil {
		t.Fatal(err)
	}
	if orphanCount != 0 || len(get(bothCookie, endpoint).Items) != 2 {
		t.Fatal("rejected orphan changed the persisted target list", orphanCount)
	}
	// A closed journal preserves stored metadata, with unavailable usage rather
	// than a zero balance or an enforcement claim.
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	outage := find(get(bothCookie, endpoint), teamB.ID)
	if !outage.Limits.PolicyRecorded || outage.Limits.Stored.MoneyMonth == nil || outage.Limits.UsageStatus != "unavailable" || outage.Limits.Usage != nil || outage.Limits.ActiveReservations != nil || outage.Limits.RuntimeApplied {
		t.Fatal("journal outage invented facts or erased saved policy", outage)
	}
	// Independent permission is checked again on every page, with no owner fallback.
	role, err := svc.SaveRole(ctx, admin.User.ID, "", "Member-only read", []string{"members.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, both.User.ID, []string{role.Role.ID}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", endpoint+"?cursor="+*first.NextCursor, "", bothCookie, ""), 403)
	testMemberTeamsOffboardingWriters(t, db, svc, admin.User.ID)
}

func testMemberTeamsOffboardingWriters(t *testing.T, db *gorm.DB, svc *service.Service, adminID string) {
	t.Helper()
	ctx := context.Background()
	for _, mode := range []string{"historical_null", "recorded", "new"} {
		departing, err := svc.CreateMember(ctx, adminID, "teams-departing-"+mode+"@example.invalid", "test-only-teams-password", "Departing", "member")
		if err != nil {
			t.Fatal(err)
		}
		successor, err := svc.CreateMember(ctx, adminID, "teams-successor-"+mode+"@example.invalid", "test-only-teams-password", "Successor", "member")
		if err != nil {
			t.Fatal(err)
		}
		team, err := svc.CreateResource(ctx, adminID, service.TeamResource, "Continuity "+mode, "", []string{departing.User.ID})
		if err != nil {
			t.Fatal(err)
		}
		var before entity.TeamMembership
		if mode != "new" {
			if _, err := svc.SetTeamMembers(ctx, adminID, team.ID, []service.TeamMemberInput{{UserID: departing.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: successor.User.ID, Role: entity.TeamMember, Status: entity.ResourceDisabled}}); err != nil {
				t.Fatal(err)
			}
			if err := db.Where("team_id = ? AND user_id = ?", team.ID, successor.User.ID).First(&before).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "historical_null" {
				if err := db.Model(&memberTeamsJoinedAtFixture{}).Where("id = ?", before.ID).Update("joined_at", nil).Error; err != nil {
					t.Fatal(err)
				}
				before.JoinedAt = nil
			}
		}
		inventory, err := svc.OffboardingInventory(ctx, adminID, departing.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := svc.CreateOffboardingPlan(ctx, adminID, departing.User.ID, service.OffboardingPlanInput{RequestID: "req_teams_" + mode, InventoryVersion: inventory.InventoryVersion, PlannedAt: time.Now().UTC().Add(time.Hour), Reason: "Controlled continuity", OffboardingAssignments: service.OffboardingAssignments{Teams: []service.OffboardingTeamAssignment{{TeamID: team.ID, OwnerUserIDs: []string{successor.User.ID}, AddMemberUserIDs: []string{successor.User.ID}}}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CompleteOffboarding(ctx, adminID, departing.User.ID, plan.ID); err != nil {
			t.Fatal(err)
		}
		var after entity.TeamMembership
		if err := db.Where("team_id = ? AND user_id = ?", team.ID, successor.User.ID).First(&after).Error; err != nil {
			t.Fatal(err)
		}
		if after.Role != entity.TeamOwner || after.Status != entity.ResourceActive {
			t.Fatal("successor not actually enabled/promoted", after)
		}
		if mode == "new" {
			if after.ID == "" || after.JoinedAt == nil || after.JoinedAt.IsZero() {
				t.Fatal("continuity addition omitted explicit new join", after)
			}
		} else {
			if after.ID != before.ID || !reflect.DeepEqual(after.JoinedAt, before.JoinedAt) {
				t.Fatal("continuity enable/promotion reset retained ID/date/null", before, after)
			}
		}
	}
}
func TestMemberTeamsLifecycleFixtureIsHarnessOwned(t *testing.T) {
	if reflect.ValueOf(testMemberTeamsLifecycle).IsNil() {
		t.Fatal("missing lifecycle fixture")
	}
}
