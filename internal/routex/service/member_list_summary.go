package service

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const memberListRoleBudget = 10000
const memberListTeamBudget = 1000

var memberListRoleOverflow = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "member list role query budget exceeded; select a smaller page"}

type MemberListPage struct {
	ActorUserID      string
	ObservedAt       time.Time
	PlatformCurrency string
	Members          []MemberListSummary
	NextCursor       string
}
type MemberListSummary struct {
	MemberRecord
	TotalPersonalKeys    string
	PersonalPolicyStored bool
	Personal             MemberOverviewMonthlyAccount
	Teams                MemberListTeams
}
type MemberListTeams struct {
	Status string           `json:"status"`
	Items  []MemberListTeam `json:"items"`
}
type MemberListTeam struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	MembershipStatus string `json:"membership_status"`
	MembershipRole   string `json:"membership_role"`
}
type memberListKeyCount struct {
	UserID string
	Count  int64
}

func normalizeMemberListFilter(actorID string, filter MemberFilter) (MemberFilter, error) {
	if !safeTeamSessionID(actorID) {
		return filter, apperrors.ErrUnauthorized
	}
	if filter.Limit == 0 {
		filter.Limit = 40
	}
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Query) > 200 || !utf8.ValidString(filter.Query) || filter.Status != "" && filter.Status != "active" && filter.Status != "disabled" || filter.Role != "" && filter.Role != entity.RoleAdmin && filter.Role != entity.RoleMember || filter.Cursor != "" && !safeTeamSessionID(filter.Cursor) {
		return filter, apperrors.ErrBadRequest
	}
	return filter, nil
}
func memberListUserQuery(tx *gorm.DB, filter MemberFilter) *gorm.DB {
	q := tx.Session(&gorm.Session{}).Model(&entity.User{}).Select("ID", "Email", "Name", "Role", "Disabled", "OffboardedAt", "CreatedAt", "UpdatedAt", "LastLoginAt")
	if filter.Query != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(filter.Query))
		pattern := "%" + escaped + "%"
		q = q.Where("LOWER(email) LIKE ? ESCAPE '!' OR LOWER(name) LIKE ? ESCAPE '!'", pattern, pattern)
	}
	if filter.Status != "" {
		q = q.Where("disabled = ?", filter.Status == "disabled")
	}
	if filter.Role != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "role"}, filter.Role))
	}
	col := clause.Column{Name: "id"}
	if filter.Cursor != "" {
		q = q.Where(database.ByteAfter(tx, col, filter.Cursor))
	}
	return q.Clauses(clause.OrderBy{Expression: database.ByteOrder(tx, col)}).Limit(filter.Limit + 1)
}
func memberListIDs(tx *gorm.DB, col clause.Column, ids []string) clause.Expression {
	terms := make([]clause.Expression, 0, len(ids))
	for _, id := range ids {
		terms = append(terms, database.ExactText(tx, col, id))
	}
	return clause.Or(terms...)
}
func memberListKeyCountQuery(tx *gorm.DB, ids []string) *gorm.DB {
	// Group on retained primary identities, never a collation-folded owner value.
	return tx.Session(&gorm.Session{}).Table("api_keys AS retained_key").Select("subject.id AS user_id, COUNT(*) AS count").
		Joins("JOIN users AS subject ON ?", database.ExactTextColumns(tx, clause.Column{Table: "subject", Name: "id"}, clause.Column{Table: "retained_key", Name: "user_id"})).
		Where(memberListIDs(tx, clause.Column{Table: "subject", Name: "id"}, ids)).Group("subject.id")
}
func memberListRoles(ids []string, rows []entity.UserRole) (map[string][]string, error) {
	if len(rows) > memberListRoleBudget {
		return nil, memberListRoleOverflow
	}
	out := map[string][]string{}
	for _, id := range ids {
		out[id] = []string{}
	}
	seen := map[entity.UserRole]bool{}
	for _, row := range rows {
		if _, ok := out[row.UserID]; !ok || !safeTeamSessionID(row.RoleID) || seen[row] {
			return nil, apperrors.ErrInternal
		}
		seen[row] = true
		out[row.UserID] = append(out[row.UserID], row.RoleID)
	}
	for _, roles := range out {
		slices.Sort(roles)
	}
	return out, nil
}
func memberListCounts(ids []string, rows []memberListKeyCount) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		out[id] = "0"
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if _, ok := out[row.UserID]; !ok || row.Count < 0 || seen[row.UserID] {
			return nil, apperrors.ErrInternal
		}
		seen[row.UserID] = true
		out[row.UserID] = strconv.FormatInt(row.Count, 10)
	}
	return out, nil
}
func memberListTeamProjection(ids []string, memberships []entity.TeamMembership, teams []entity.Team) (map[string]MemberListTeams, error) {
	out := map[string]MemberListTeams{}
	for _, id := range ids {
		out[id] = MemberListTeams{Status: "available", Items: []MemberListTeam{}}
	}
	if len(memberships) > memberListTeamBudget {
		for _, id := range ids {
			out[id] = MemberListTeams{Status: "overflow"}
		}
		return out, nil
	}
	byTeam := map[string]entity.Team{}
	for _, team := range teams {
		if !safeTeamSessionID(team.ID) || !validCatalogLabel(team.Name) || team.Status != entity.ResourceActive && team.Status != entity.ResourceDisabled && team.Status != entity.ResourceArchived {
			return nil, apperrors.ErrInternal
		}
		if _, ok := byTeam[team.ID]; ok {
			return nil, apperrors.ErrInternal
		}
		byTeam[team.ID] = team
	}
	seenID, seenPair, referenced := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, m := range memberships {
		summary, ok := out[m.UserID]
		team, found := byTeam[m.TeamID]
		pair := m.UserID + "|" + m.TeamID
		if !ok || !found || !safeTeamSessionID(m.ID) || !safeTeamSessionID(m.TeamID) || seenID[m.ID] || seenPair[pair] || m.Status != entity.ResourceActive && m.Status != entity.ResourceDisabled || m.Role != entity.TeamOwner && m.Role != entity.TeamMember {
			return nil, apperrors.ErrInternal
		}
		seenID[m.ID], seenPair[pair], referenced[m.TeamID] = true, true, true
		summary.Items = append(summary.Items, MemberListTeam{ID: team.ID, Name: team.Name, Status: team.Status, MembershipRole: m.Role, MembershipStatus: m.Status})
		out[m.UserID] = summary
	}
	if len(referenced) != len(byTeam) {
		return nil, apperrors.ErrInternal
	}
	for id, summary := range out {
		slices.SortFunc(summary.Items, func(a, b MemberListTeam) int { return strings.Compare(a.ID, b.ID) })
		out[id] = summary
	}
	return out, nil
}
func readMemberListTeams(tx *gorm.DB, ids []string) (map[string]MemberListTeams, error) {
	var memberships []entity.TeamMembership
	// Retain collated candidates for explicit identity validation; an aliased
	// relationship must fail the full response rather than borrow a subject.
	if err := tx.Session(&gorm.Session{}).Select("ID", "TeamID", "UserID", "Role", "Status").Where("user_id IN ?", ids).Limit(memberListTeamBudget + 1).Find(&memberships).Error; err != nil {
		return nil, err
	}
	if len(memberships) > memberListTeamBudget {
		return memberListTeamProjection(ids, memberships, nil)
	}
	teamIDs := []string{}
	for _, m := range memberships {
		teamIDs = append(teamIDs, m.TeamID)
	}
	slices.Sort(teamIDs)
	teamIDs = slices.Compact(teamIDs)
	var teams []entity.Team
	if len(teamIDs) > 0 {
		if err := tx.Session(&gorm.Session{}).Select("ID", "Name", "Status").Where("id IN ?", teamIDs).Limit(memberListTeamBudget + 1).Find(&teams).Error; err != nil {
			return nil, err
		}
	}
	return memberListTeamProjection(ids, memberships, teams)
}

// ListMemberSummaries captures one authorized SQL page and one independent,
// coherent Personal journal batch. It reads no per-subject endpoint or directory.
func (s *Service) ListMemberSummaries(ctx context.Context, actorID string, filter MemberFilter) (*MemberListPage, error) {
	filter, err := normalizeMemberListFilter(actorID, filter)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &MemberListPage{ActorUserID: actorID, Members: []MemberListSummary{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var actor entity.User
		if err := tx.Select("ID", "Role", "Disabled", "OffboardedAt").Where(database.ExactText(tx, clause.Column{Name: "id"}, actorID)).Where("disabled = ? AND offboarded_at IS NULL", false).First(&actor).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return apperrors.ErrUnauthorized
			}
			return err
		}
		if actor.ID != actorID {
			return apperrors.ErrUnauthorized
		}
		allowed, err := exactGovernancePermission(tx, actor, "members.read")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		teamRead, err := exactGovernancePermission(tx, actor, "teams.read_all")
		if err != nil {
			return err
		}
		result.ObservedAt = time.Now().UTC()
		var users []entity.User
		if err := memberListUserQuery(tx, filter).Find(&users).Error; err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, user := range users {
			if !safeTeamSessionID(user.ID) || user.CreatedAt.IsZero() || seen[user.ID] || user.Role != entity.RoleAdmin && user.Role != entity.RoleMember {
				return apperrors.ErrInternal
			}
			seen[user.ID] = true
		}
		if len(users) > filter.Limit {
			users = users[:filter.Limit]
			result.NextCursor = users[len(users)-1].ID
		}
		ids := make([]string, len(users))
		targets := make([]overviewAccountTarget, len(users))
		for i, u := range users {
			if _, err := memberRecentLoginProjection(u.LastLoginAt); err != nil {
				return err
			}
			ids[i] = u.ID
			targets[i] = overviewAccountTarget{kind: "user", id: u.ID, created: u.CreatedAt}
		}
		roles := map[string][]string{}
		counts := map[string]string{}
		stored := map[string]bool{}
		teamSummaries := map[string]MemberListTeams{}
		if len(ids) > 0 {
			var roleRows []entity.UserRole
			if err := tx.Session(&gorm.Session{}).Where(memberListIDs(tx, clause.Column{Name: "user_id"}, ids)).Limit(memberListRoleBudget + 1).Find(&roleRows).Error; err != nil {
				return err
			}
			roles, err = memberListRoles(ids, roleRows)
			if err != nil {
				return err
			}
			var countRows []memberListKeyCount
			if err := memberListKeyCountQuery(tx, ids).Scan(&countRows).Error; err != nil {
				return err
			}
			counts, err = memberListCounts(ids, countRows)
			if err != nil {
				return err
			}
			var policies []entity.ResourceLimit
			if err := memberOverviewPolicyQuery(tx, targets).Find(&policies).Error; err != nil {
				return err
			}
			if err := memberOverviewPolicies(targets, policies); err != nil {
				return err
			}
			for _, row := range policies {
				stored[row.ScopeID] = true
			}
			if teamRead {
				teamSummaries, err = readMemberListTeams(tx, ids)
				if err != nil {
					return err
				}
			}
		}
		var currency entity.PricingSetting
		if err := tx.Select("PlatformCurrency").Take(&currency, 1).Error; err != nil {
			return err
		}
		if !pricing.Currency(currency.PlatformCurrency) {
			return apperrors.ErrInternal
		}
		result.PlatformCurrency = currency.PlatformCurrency
		var setting entity.QuotaSetting
		if err := memberOverviewCalendarQuery(tx).Take(&setting, 1).Error; err != nil {
			return err
		}
		var auth *runtimeAuthorization
		if s.runtime != nil {
			auth = s.runtime.auth.Load()
		}
		var batch *eventqueue.QuotaUsageBatch
		if s.recorder != nil && len(ids) > 0 {
			accounts := make([]string, len(ids))
			for i, id := range ids {
				accounts[i] = limitAccount("user", id)
			}
			batch, _ = s.recorder.queue.AccountQuotaUsageBatch(accounts, result.ObservedAt)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, u := range users {
			teams := MemberListTeams{Status: "not_authorized"}
			if teamRead {
				teams = teamSummaries[u.ID]
			}
			personal := s.memberOverviewMonthlyAccount(targets[i], targets, batch, auth, u.ID, setting, result.PlatformCurrency)
			personal.RuntimeApplied = personal.RuntimeApplied && s.memberOverviewSubjectApplied(auth, u, targets[i], setting, result.PlatformCurrency)
			result.Members = append(result.Members, MemberListSummary{MemberRecord: MemberRecord{User: u, RoleIDs: roles[u.ID]}, TotalPersonalKeys: counts[u.ID], PersonalPolicyStored: stored[u.ID], Personal: personal, Teams: teams})
		}
		// Never claim application from a generation replaced while projecting a page.
		for i := range result.Members {
			item := &result.Members[i]
			item.Personal.RuntimeApplied = item.Personal.RuntimeApplied && s.memberOverviewSubjectApplied(auth, item.User, targets[i], setting, result.PlatformCurrency)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
