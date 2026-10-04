package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemberOverviewAccountsFilter struct {
	Cursor string
	Limit  int
}

type MemberOverviewAccountsPage struct {
	ActorUserID      string                       `json:"actor_user_id"`
	ObservedAt       time.Time                    `json:"observed_at"`
	PlatformCurrency string                       `json:"platform_currency"`
	Personal         MemberOverviewMonthlyAccount `json:"personal"`
	Teams            []MemberOverviewTeamAccount  `json:"teams"`
	NextCursor       *string                      `json:"next_cursor"`
}

type MemberOverviewTeamAccount struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	MembershipID string                       `json:"membership_id"`
	Aggregate    MemberOverviewMonthlyAccount `json:"aggregate"`
	Member       MemberOverviewMonthlyAccount `json:"member"`
}

// Each account reports its stored monthly policy. A Team aggregate restriction
// and a member restriction apply independently; neither usage nor caps are added.
type MemberOverviewMonthlyAccount struct {
	AccountID          string                      `json:"account_id"`
	PolicyETag         string                      `json:"policy_etag"`
	TokensMonth        *string                     `json:"tokens_month"`
	MoneyMonth         *string                     `json:"money_month"`
	Currency           *string                     `json:"currency"`
	RuntimeApplied     bool                        `json:"runtime_applied"`
	UsageStatus        string                      `json:"usage_status"`
	Usage              *OverviewAccountUsage       `json:"usage"`
	ActiveReservations *OverviewActiveReservations `json:"active_reservations"`
}

// Live reservations share Usage.AsOf but remain separate from monthly facts.
type OverviewActiveReservations struct {
	TokensHeld string            `json:"tokens_held"`
	MoneyHeld  map[string]string `json:"money_held"`
}

// All journal counters retain their exact int64 representation across JSON.
type OverviewAccountUsage struct {
	AsOf          time.Time         `json:"as_of"`
	TimeZone      string            `json:"time_zone"`
	MonthStart    time.Time         `json:"month_start"`
	MonthEnd      time.Time         `json:"month_end"`
	Covered       bool              `json:"covered"`
	TokensUsed    string            `json:"tokens_used"`
	TokensHeld    string            `json:"tokens_held"`
	TokensUnknown string            `json:"tokens_unknown"`
	MoneyUsed     map[string]string `json:"money_used"`
	MoneyHeld     map[string]string `json:"money_held"`
	MoneyUnknown  string            `json:"money_unknown"`
}

type overviewTeamIdentity struct {
	ID, Name, MembershipID, MembershipTeamID, MembershipUserID string
	TeamStatus, MembershipStatus, Role                         string
	CreatedAt                                                  time.Time
}

type overviewAccountTarget struct {
	kind, id, teamID, membershipID string
	created                        time.Time
	row                            entity.ResourceLimit
	policy                         limits.Policy
}

func normalizeMemberOverviewFilter(actorID string, filter MemberOverviewAccountsFilter) (MemberOverviewAccountsFilter, string, error) {
	if !safeTeamSessionID(actorID) {
		return filter, "", apperrors.ErrUnauthorized
	}
	if filter.Limit == 0 {
		filter.Limit = 10
	}
	if filter.Limit < 1 || filter.Limit > 50 || len(filter.Cursor) > 128 {
		return filter, "", apperrors.ErrBadRequest
	}
	if filter.Cursor == "" {
		return filter, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
	owner, lastID, found := strings.Cut(string(raw), "|")
	if err != nil || !found || owner != actorID || !safeTeamSessionID(lastID) || base64.RawURLEncoding.EncodeToString(raw) != filter.Cursor {
		return filter, "", apperrors.ErrBadRequest
	}
	return filter, lastID, nil
}

func memberOverviewCursor(actorID, teamID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(actorID + "|" + teamID))
}

func memberOverviewTeamQuery(tx *gorm.DB, actorID, after string, limit int) *gorm.DB {
	query := tx.Table("teams AS team").
		Select("team.id, team.name, team.created_at, team.status AS team_status, tm.id AS membership_id, tm.team_id AS membership_team_id, tm.user_id AS membership_user_id, tm.status AS membership_status, tm.role").
		Joins("JOIN team_memberships AS tm ON ?", database.ExactTextColumns(tx, clause.Column{Table: "team", Name: "id"}, clause.Column{Table: "tm", Name: "team_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "user_id"}, actorID)).
		Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "status"}, entity.ResourceActive)).
		Where(database.ExactText(tx, clause.Column{Table: "team", Name: "status"}, entity.ResourceActive)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "tm", Name: "role"}, entity.TeamOwner), database.ExactText(tx, clause.Column{Table: "tm", Name: "role"}, entity.TeamMember)))
	if after != "" {
		query = query.Where("team.id > ?", after)
	}
	return query.Order("team.id").Limit(limit + 1)
}

func validOverviewTeam(row overviewTeamIdentity, actorID string) bool {
	return safeTeamSessionID(row.ID) && safeTeamSessionID(row.MembershipID) && row.MembershipTeamID == row.ID && row.MembershipUserID == actorID && row.TeamStatus == entity.ResourceActive && row.MembershipStatus == entity.ResourceActive && (row.Role == entity.TeamOwner || row.Role == entity.TeamMember) && validCatalogLabel(row.Name) && !row.CreatedAt.IsZero()
}

func memberOverviewPolicyQuery(tx *gorm.DB, targets []overviewAccountTarget) *gorm.DB {
	var scopes []clause.Expression
	for _, target := range targets {
		scopes = append(scopes, clause.And(database.ExactText(tx, clause.Column{Name: "scope_kind"}, target.kind), database.ExactText(tx, clause.Column{Name: "scope_id"}, target.id)))
	}
	return tx.Where(clause.Or(scopes...))
}

// Select Go fields so GORM preserves the released physical calendar columns.
func memberOverviewCalendarQuery(tx *gorm.DB) *gorm.DB {
	return tx.Model(&entity.QuotaSetting{}).Select("TimeZone", "ETag")
}

func memberOverviewPolicies(targets []overviewAccountTarget, rows []entity.ResourceLimit) error {
	byAccount := make(map[string]int, len(targets))
	for i := range targets {
		target := &targets[i]
		byAccount[limitAccount(target.kind, target.id)] = i
		target.row = entity.ResourceLimit{ScopeKind: target.kind, ScopeID: target.id, ETag: "0"}
		var err error
		target.policy, err = policyFromRow(target.row)
		if err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, row := range rows {
		account := limitAccount(row.ScopeKind, row.ScopeID)
		i, found := byAccount[account]
		if !found || seen[account] || row.ScopeKind != targets[i].kind || row.ScopeID != targets[i].id || row.ETag == "" {
			return apperrors.ErrInternal
		}
		policy, err := policyFromRow(row)
		if err != nil || row.ScopeKind != "user" && validateTeamLimitPolicy(row.ScopeKind, policy) != nil {
			return apperrors.ErrInternal
		}
		seen[account] = true
		targets[i].row, targets[i].policy = row, policy
	}
	return nil
}

func overviewMonthlyUsage(value eventqueue.AccountQuotaUsage, created time.Time) (*OverviewAccountUsage, error) {
	if value.AsOf.IsZero() || created.IsZero() || created.After(value.AsOf) || value.CoverageStart.IsZero() || value.CoverageStart.After(value.AsOf) {
		return nil, runtimeUnavailable
	}
	location, err := time.LoadLocation(value.TimeZone)
	if err != nil || value.TimeZone == "Local" {
		return nil, runtimeUnavailable
	}
	local := value.AsOf.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	month := value.Month
	if month.TokensUsed < 0 || month.TokensHeld < 0 || month.TokensUnknown < 0 || month.MoneyUnknown < 0 {
		return nil, runtimeUnavailable
	}
	return &OverviewAccountUsage{AsOf: value.AsOf, TimeZone: value.TimeZone, MonthStart: start.UTC(), MonthEnd: start.AddDate(0, 1, 0).UTC(), Covered: !created.Before(value.CoverageStart) || !start.Before(value.CoverageStart), TokensUsed: strconv.FormatInt(month.TokensUsed, 10), TokensHeld: strconv.FormatInt(month.TokensHeld, 10), TokensUnknown: strconv.FormatInt(month.TokensUnknown, 10), MoneyUsed: month.MoneyUsed, MoneyHeld: month.MoneyHeld, MoneyUnknown: strconv.FormatInt(month.MoneyUnknown, 10)}, nil
}

func (s *Service) memberOverviewApplied(auth *runtimeAuthorization, actorID string, target overviewAccountTarget, targets []overviewAccountTarget, setting entity.QuotaSetting, currency string) bool {
	if s.runtime == nil || auth == nil || s.runtime.auth.Load() != auth || !time.Now().Before(auth.ValidUntil) || auth.Quota == nil || setting.ETag == "" || auth.Quota.Setting.ETag != setting.ETag || auth.Quota.Setting.TimeZone != setting.TimeZone || auth.Quota.Currency != currency || runtimeDenied(&s.runtime.deniedUsers, actorID) || runtimeDenied(&s.runtime.deniedLimits, "quota_settings") {
		return false
	}
	checks := []overviewAccountTarget{target}
	if target.kind != "user" {
		team, exists := auth.Teams[target.teamID]
		if !exists || !team.CreatedAt.Equal(target.created) || team.Members[actorID] != target.membershipID || runtimeDenied(&s.runtime.deniedTeams, target.teamID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(target.teamID, actorID)) {
			return false
		}
		if target.kind == "team_member" {
			for _, parent := range targets {
				if parent.kind == "team" && parent.id == target.teamID {
					checks = append(checks, parent)
					break
				}
			}
			if len(checks) != 2 {
				return false
			}
		}
	}
	for _, current := range checks {
		account := limitAccount(current.kind, current.id)
		revision := auth.Quota.Revisions[account]
		if revision == "" {
			revision = "0"
		}
		published, err := limits.Normalize(auth.LimitPolicies[account])
		if err != nil || current.created.IsZero() || revision != current.row.ETag || !auth.Quota.Created[account].Equal(current.created) || !reflect.DeepEqual(published, current.policy) || runtimeDenied(&s.runtime.deniedLimits, account) || current.policy.MoneyMonth != nil && current.policy.Currency != currency {
			return false
		}
	}
	return true
}

func (s *Service) memberOverviewMonthlyAccount(target overviewAccountTarget, all []overviewAccountTarget, batch *eventqueue.QuotaUsageBatch, auth *runtimeAuthorization, actorID string, setting entity.QuotaSetting, currency string) MemberOverviewMonthlyAccount {
	result := MemberOverviewMonthlyAccount{AccountID: limitAccount(target.kind, target.id), PolicyETag: target.row.ETag, MoneyMonth: target.policy.MoneyMonth, UsageStatus: "unavailable"}
	if target.policy.TokensMonth != nil {
		value := strconv.FormatInt(*target.policy.TokensMonth, 10)
		result.TokensMonth = &value
	}
	if target.policy.MoneyMonth != nil {
		value := target.policy.Currency
		result.Currency = &value
	}
	if batch == nil {
		return result
	}
	if !batch.Active {
		result.UsageStatus = "inactive"
		return result
	}
	usage, exists := batch.Accounts[result.AccountID]
	if !exists || !usage.AsOf.Equal(batch.AsOf) || !usage.CoverageStart.Equal(batch.CoverageStart) || usage.TimeZone != batch.TimeZone {
		return result
	}
	value, err := overviewMonthlyUsage(usage, target.created)
	if err != nil || usage.Active.TokensHeld < 0 || usage.Active.TokensUnknown < 0 || usage.Active.MoneyUnknown < 0 {
		return result
	}
	moneyHeld := make(map[string]string, len(usage.Active.MoneyHeld))
	for currency, amount := range usage.Active.MoneyHeld {
		moneyHeld[currency] = amount
	}
	result.ActiveReservations = &OverviewActiveReservations{TokensHeld: strconv.FormatInt(usage.Active.TokensHeld, 10), MoneyHeld: moneyHeld}
	result.Usage, result.UsageStatus = value, "active"
	result.RuntimeApplied = batch.TimeZone == setting.TimeZone && s.memberOverviewApplied(auth, actorID, target, all, setting, currency)
	return result
}

// MemberOverviewAccounts enumerates only the authenticated member's own current
// active accounts. Platform directory or limit-write permissions add no rows.
// Persisted facts use one repeatable snapshot; journal counters share a separate
// coherent AsOf and are never reconstructed from asynchronous call reports.
func (s *Service) MemberOverviewAccounts(ctx context.Context, actorID string, filter MemberOverviewAccountsFilter) (*MemberOverviewAccountsPage, error) {
	filter, after, err := normalizeMemberOverviewFilter(actorID, filter)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &MemberOverviewAccountsPage{ActorUserID: actorID, Teams: []MemberOverviewTeamAccount{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		result.ObservedAt = time.Now().UTC()
		var teams []overviewTeamIdentity
		if err := memberOverviewTeamQuery(tx, actorID, after, filter.Limit).Scan(&teams).Error; err != nil {
			return err
		}
		for _, team := range teams {
			if !validOverviewTeam(team, actorID) {
				return apperrors.ErrInternal
			}
		}
		if len(teams) > filter.Limit {
			cursor := memberOverviewCursor(actorID, teams[filter.Limit-1].ID)
			result.NextCursor = &cursor
			teams = teams[:filter.Limit]
		}
		targets := []overviewAccountTarget{{kind: "user", id: actorID, created: actor.CreatedAt}}
		for _, team := range teams {
			targets = append(targets, overviewAccountTarget{kind: "team", id: team.ID, teamID: team.ID, membershipID: team.MembershipID, created: team.CreatedAt}, overviewAccountTarget{kind: "team_member", id: teamMemberLimitScopeID(team.ID, actorID), teamID: team.ID, membershipID: team.MembershipID, created: team.CreatedAt})
		}
		var rows []entity.ResourceLimit
		if err := memberOverviewPolicyQuery(tx, targets).Find(&rows).Error; err != nil {
			return err
		}
		if err := memberOverviewPolicies(targets, rows); err != nil {
			return err
		}
		var pricingSetting entity.PricingSetting
		if err := tx.Select("platform_currency").Take(&pricingSetting, 1).Error; err != nil {
			return err
		}
		if !pricing.Currency(pricingSetting.PlatformCurrency) {
			return apperrors.ErrInternal
		}
		result.PlatformCurrency = pricingSetting.PlatformCurrency
		var setting entity.QuotaSetting
		if err := memberOverviewCalendarQuery(tx).Take(&setting, 1).Error; err != nil {
			return err
		}
		var auth *runtimeAuthorization
		if s.runtime != nil {
			auth = s.runtime.auth.Load()
		}
		var batch *eventqueue.QuotaUsageBatch
		if s.recorder != nil {
			accounts := make([]string, len(targets))
			for i, target := range targets {
				accounts[i] = limitAccount(target.kind, target.id)
			}
			// A journal outage leaves persisted policies visible and usage unknown.
			batch, _ = s.recorder.queue.AccountQuotaUsageBatch(accounts, result.ObservedAt)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result.Personal = s.memberOverviewMonthlyAccount(targets[0], targets, batch, auth, actorID, setting, result.PlatformCurrency)
		for i, team := range teams {
			result.Teams = append(result.Teams, MemberOverviewTeamAccount{ID: team.ID, Name: team.Name, MembershipID: team.MembershipID, Aggregate: s.memberOverviewMonthlyAccount(targets[1+2*i], targets, batch, auth, actorID, setting, result.PlatformCurrency), Member: s.memberOverviewMonthlyAccount(targets[2+2*i], targets, batch, auth, actorID, setting, result.PlatformCurrency)})
		}
		// Recheck the captured publication and tombstones after projection. A
		// newer generation must not turn the old SQL policy into a current claim.
		result.Personal.RuntimeApplied = result.Personal.RuntimeApplied && s.memberOverviewApplied(auth, actorID, targets[0], targets, setting, result.PlatformCurrency)
		for i := range result.Teams {
			result.Teams[i].Aggregate.RuntimeApplied = result.Teams[i].Aggregate.RuntimeApplied && s.memberOverviewApplied(auth, actorID, targets[1+2*i], targets, setting, result.PlatformCurrency)
			result.Teams[i].Member.RuntimeApplied = result.Teams[i].Member.RuntimeApplied && s.memberOverviewApplied(auth, actorID, targets[2+2*i], targets, setting, result.PlatformCurrency)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
