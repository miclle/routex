package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"strings"
	"testing"
)

func TestTeamMemberQuotaWarningBoundedScopeFairWrapAndCancellation(t *testing.T) {
	candidates := make([]quotaTeamIdentity, 33)
	policies := []entity.ResourceLimit{}
	for i := range candidates {
		user := fmt.Sprintf("usr_%03d", i)
		candidates[i] = quotaTeamIdentity{MembershipID: fmt.Sprintf("tmm_%03d", i), MembershipUserID: user, MembershipTeamID: "tem_warning", UserID: user, TeamID: "tem_warning", TeamStatus: entity.ResourceActive, MembershipStatus: entity.ResourceActive, Role: entity.TeamMember}
		if i != 5 {
			policies = append(policies, entity.ResourceLimit{ScopeKind: "team_member", ScopeID: teamMemberLimitScopeID("tem_warning", user), TokensMonth: limitNumber(100)})
		}
	}
	// Aggregate and another pair never authorize a private child observation.
	policies = append(policies, entity.ResourceLimit{ScopeKind: "team", ScopeID: "tem_warning", TokensMonth: limitNumber(1)})
	failure := errors.New("controlled warning failure")
	cursor := ""
	seen := map[string]bool{}
	observe := func(_ context.Context, target teamMemberQuotaTarget) error {
		if !validTeamMemberQuotaTarget(target) || target.TeamID != "tem_warning" || seen[target.UserID] {
			t.Fatal("scope or duplicate target in bounded cycle")
		}
		seen[target.UserID] = true
		if target.UserID == "usr_000" {
			return failure
		}
		return nil
	}
	done, err := reconcileTeamMemberQuotaNotificationRows(context.Background(), candidates[:32], policies, &cursor, observe)
	if done || !errors.Is(err, failure) || cursor != "tmm_031" || len(seen) != 31 || seen["usr_005"] {
		t.Fatal("failed or unlimited pair starved scanner")
	}
	done, err = reconcileTeamMemberQuotaNotificationRows(context.Background(), candidates[32:], policies, &cursor, observe)
	if done || err != nil || cursor != "tmm_032" || len(seen) != 32 {
		t.Fatal("later pair omitted")
	}
	done, err = reconcileTeamMemberQuotaNotificationRows(context.Background(), nil, nil, &cursor, observe)
	if !done || err != nil || cursor != "" {
		t.Fatal("EOF did not wrap independent cursor")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reconcileTeamMemberQuotaNotificationRows(ctx, candidates, policies, &cursor, observe)
	if !errors.Is(err, context.Canceled) || len(seen) != 32 || cursor != "" {
		t.Fatal("cancel observed or advanced member")
	}
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
		var rows []quotaTeamIdentity
		q := teamMemberQuotaCandidatesQuery(db, "tmm_031").Find(&rows)
		sql := q.Statement.SQL.String()
		if q.Error != nil || !strings.Contains(sql, "member.id >") || !strings.Contains(sql, "ORDER BY member.id") || !strings.Contains(sql, "LIMIT") {
			t.Fatal("unbounded member warning candidates")
		}
		var limits []entity.ResourceLimit
		q = teamMemberQuotaPoliciesQuery(db, candidates[:32]).Find(&limits)
		if q.Error != nil || !strings.Contains(q.Statement.SQL.String(), "scope_kind") || !strings.Contains(q.Statement.SQL.String(), "scope_id") {
			t.Fatal("private policy batch lost exact pair")
		}
	}
}
