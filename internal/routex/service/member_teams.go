package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var memberTeamID = regexp.MustCompile(`^tea_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var memberTeamMembershipID = regexp.MustCompile(`^tmm_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

type MemberTeamsFilter struct {
	Cursor string
	Limit  int
}
type MemberTeamsPage struct {
	UserID           string             `json:"user_id"`
	ObservedAt       time.Time          `json:"observed_at"`
	PlatformCurrency string             `json:"platform_currency"`
	Items            []MemberTeamRecord `json:"items"`
	NextCursor       *string            `json:"next_cursor"`
}
type MemberTeamRecord struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Status           string           `json:"status"`
	MembershipID     string           `json:"membership_id"`
	MembershipRole   string           `json:"membership_role"`
	MembershipStatus string           `json:"membership_status"`
	JoinedAt         *time.Time       `json:"joined_at"`
	Limits           MemberTeamLimits `json:"limits"`
}
type memberTeamIdentity struct {
	ID, ActualTeamID, Name, Status                                   string
	MembershipID, MembershipUserID, MembershipRole, MembershipStatus string
	CreatedAt                                                        time.Time
	JoinedAt                                                         *time.Time
}

func memberTeamsCursor(actorID, userID, teamID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(actorID + "|" + userID + "|" + teamID))
}
func normalizeMemberTeamsFilter(actorID, userID string, filter MemberTeamsFilter) (MemberTeamsFilter, string, error) {
	if !memberKeyUserID.MatchString(actorID) || !memberKeyUserID.MatchString(userID) {
		return filter, "", apperrors.ErrNotFound
	}
	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit < 1 || filter.Limit > 50 || len(filter.Cursor) > 128 {
		return filter, "", apperrors.ErrBadRequest
	}
	if filter.Cursor == "" {
		return filter, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
	parts := strings.Split(string(raw), "|")
	if err != nil || len(parts) != 3 || parts[0] != actorID || parts[1] != userID || !memberTeamID.MatchString(parts[2]) || base64.RawURLEncoding.EncodeToString(raw) != filter.Cursor {
		return filter, "", apperrors.ErrBadRequest
	}
	return filter, parts[2], nil
}

func memberTeamsPeople(tx *gorm.DB, actorID, userID string) (entity.User, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return entity.User{}, err
	}
	permissions, err := memberKeyPermissions(tx, actor)
	if err != nil {
		return entity.User{}, err
	}
	if !slices.Contains(permissions, "members.read") || !slices.Contains(permissions, "teams.read_all") {
		return entity.User{}, apperrors.ErrForbidden
	}
	var subject entity.User
	err = tx.Select("id", "role", "disabled", "offboarded_at", "created_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, userID)).First(&subject).Error
	if err != nil {
		return subject, err
	}
	if subject.ID != userID || subject.CreatedAt.IsZero() || subject.Role != entity.RoleAdmin && subject.Role != entity.RoleMember {
		return subject, apperrors.ErrNotFound
	}
	return subject, nil
}

func memberTeamsQuery(tx *gorm.DB, userID, after string, limit int) *gorm.DB {
	query := tx.Table("team_memberships AS member").Select("member.team_id AS id, member.id AS membership_id, member.user_id AS membership_user_id, member.role AS membership_role, member.status AS membership_status, member.joined_at").Where(database.ExactText(tx, clause.Column{Table: "member", Name: "user_id"}, userID))
	if after != "" {
		query = query.Where("member.team_id > ?", after)
	}
	return query.Order("member.team_id").Limit(limit + 1)
}

func validateMemberTeamRelations(rows []memberTeamIdentity, userID string) error {
	seen, relationships := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if !memberTeamID.MatchString(row.ID) || row.MembershipUserID != userID || !memberTeamMembershipID.MatchString(row.MembershipID) || row.MembershipRole != entity.TeamMember && row.MembershipRole != entity.TeamOwner || row.MembershipStatus != entity.ResourceActive && row.MembershipStatus != entity.ResourceDisabled || row.JoinedAt != nil && row.JoinedAt.IsZero() || seen[row.ID] || relationships[row.MembershipID] {
			return apperrors.ErrInternal
		}
		seen[row.ID], relationships[row.MembershipID] = true, true
	}
	return nil
}

func hydrateMemberTeams(rows []memberTeamIdentity, teams []entity.Team) error {
	selected := map[string]int{}
	for index, row := range rows {
		selected[row.ID] = index
	}
	seen := map[string]bool{}
	for _, team := range teams {
		index, exists := selected[team.ID]
		if !exists || seen[team.ID] {
			return apperrors.ErrInternal
		}
		seen[team.ID] = true
		rows[index].ActualTeamID, rows[index].Name, rows[index].Status, rows[index].CreatedAt = team.ID, team.Name, team.Status, team.CreatedAt
	}
	if len(seen) != len(rows) {
		return apperrors.ErrInternal
	}
	return nil
}
func validateMemberTeams(rows []memberTeamIdentity, userID string, now time.Time) error {
	seen, relationships := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if !memberTeamID.MatchString(row.ID) || row.ActualTeamID != row.ID || row.MembershipUserID != userID || !memberTeamMembershipID.MatchString(row.MembershipID) || !validCatalogLabel(row.Name) || row.CreatedAt.IsZero() || row.CreatedAt.After(now) || row.Status != entity.ResourceActive && row.Status != entity.ResourceDisabled && row.Status != entity.ResourceArchived || row.MembershipRole != entity.TeamMember && row.MembershipRole != entity.TeamOwner || row.MembershipStatus != entity.ResourceActive && row.MembershipStatus != entity.ResourceDisabled || row.JoinedAt != nil && (row.JoinedAt.IsZero() || row.JoinedAt.Before(row.CreatedAt) || row.JoinedAt.After(now)) || seen[row.ID] || relationships[row.MembershipID] {
			return apperrors.ErrInternal
		}
		seen[row.ID], relationships[row.MembershipID] = true, true
	}
	return nil
}

// ListMemberTeams authorizes the real actor and reads only the retained subject.
// ObservedAt is a server observation, not a persisted historical timestamp.
func (s *Service) ListMemberTeams(ctx context.Context, actorID, userID string, filter MemberTeamsFilter) (*MemberTeamsPage, error) {
	filter, after, err := normalizeMemberTeamsFilter(actorID, userID, filter)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &MemberTeamsPage{UserID: userID, Items: []MemberTeamRecord{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		subject, e := memberTeamsPeople(tx, actorID, userID)
		if e != nil {
			return e
		}
		result.ObservedAt = time.Now().UTC()
		var rows []memberTeamIdentity
		if e = memberTeamsQuery(tx, userID, after, filter.Limit).Scan(&rows).Error; e != nil {
			return e
		}
		if e = validateMemberTeamRelations(rows, userID); e != nil {
			return e
		}
		if len(rows) > filter.Limit {
			cursor := memberTeamsCursor(actorID, userID, rows[filter.Limit-1].ID)
			result.NextCursor = &cursor
			rows = rows[:filter.Limit]
		}
		if len(rows) > 0 {
			teamIDs := make([]string, len(rows))
			for index, row := range rows {
				teamIDs[index] = row.ID
			}
			var teams []entity.Team
			if e = teamRoleDefinitionQuery(tx.Model(&entity.Team{}), teamIDs).Select("id", "name", "status", "created_at").Limit(len(rows) + 1).Find(&teams).Error; e != nil {
				return e
			}
			if e = hydrateMemberTeams(rows, teams); e != nil {
				return e
			}
		}
		if e = validateMemberTeams(rows, userID, result.ObservedAt); e != nil {
			return e
		}
		targets := memberTeamsTargets(rows, userID)
		var policies []entity.ResourceLimit
		if len(targets) > 0 {
			if e = memberOverviewPolicyQuery(tx, targets).Limit(len(targets) + 1).Find(&policies).Error; e != nil {
				return e
			}
			if e = memberOverviewPolicies(targets, policies); e != nil {
				return e
			}
		}
		var currency entity.PricingSetting
		if e = tx.Select("platform_currency").Take(&currency, 1).Error; e != nil {
			return e
		}
		if !pricing.Currency(currency.PlatformCurrency) {
			return apperrors.ErrInternal
		}
		result.PlatformCurrency = currency.PlatformCurrency
		var calendar entity.QuotaSetting
		if e = memberOverviewCalendarQuery(tx).Take(&calendar, 1).Error; e != nil {
			return e
		}
		var auth *runtimeAuthorization
		if s.runtime != nil {
			auth = s.runtime.auth.Load()
		}
		var batch *eventqueue.QuotaUsageBatch
		if len(targets) > 0 && s.recorder != nil {
			accounts := make([]string, len(targets))
			for i, target := range targets {
				accounts[i] = limitAccount(target.kind, target.id)
			}
			if len(accounts) > 100 {
				return apperrors.ErrInternal
			}
			batch, _ = s.recorder.queue.AccountQuotaUsageBatch(accounts, result.ObservedAt)
		}
		present := map[string]bool{}
		for _, policy := range policies {
			present[limitAccount(policy.ScopeKind, policy.ScopeID)] = true
		}
		for i, row := range rows {
			child, parent := targets[2*i+1], targets[2*i]
			account := s.memberOverviewMonthlyAccount(child, targets, batch, auth, userID, calendar, result.PlatformCurrency)
			joinedAt := row.JoinedAt
			if joinedAt != nil {
				utc := joinedAt.UTC()
				joinedAt = &utc
			}
			record := MemberTeamRecord{
				ID: row.ID, Name: row.Name, Status: row.Status,
				MembershipID: row.MembershipID, MembershipRole: row.MembershipRole,
				MembershipStatus: row.MembershipStatus, JoinedAt: joinedAt,
				Limits: MemberTeamLimits{
					PolicyRecorded: present[limitAccount(child.kind, child.id)],
					PolicyETag:     child.row.ETag,
					Stored:         memberTeamPolicyValues(child.policy),
					ParentStored:   memberTeamPolicyValues(parent.policy),
					RuntimeApplied: account.RuntimeApplied && s.memberTeamsApplied(auth, subject, row, child, targets, calendar, result.PlatformCurrency),
					UsageStatus:    account.UsageStatus, Usage: account.Usage,
					ActiveReservations: account.ActiveReservations,
				},
			}
			result.Items = append(result.Items, record)
		}
		for i := range result.Items {
			result.Items[i].Limits.RuntimeApplied = result.Items[i].Limits.RuntimeApplied && s.memberTeamsApplied(auth, subject, rows[i], targets[2*i+1], targets, calendar, result.PlatformCurrency)
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
