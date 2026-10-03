package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
)

const teamQuotaReadAll = "teams.quota_requests.read_all"

var errTeamQuotaOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "Team quota requests exceed supported bounds"}

var errTeamQuotaSubject = errors.New("team quota request subject is unavailable")

type TeamQuotaRequestInput struct {
	RequestID   string `json:"request_id"`
	Dimension   string `json:"dimension"`
	TargetValue string `json:"target_value"`
	Reason      string `json:"reason"`
}
type TeamQuotaDecisionInput struct {
	DecisionID string `json:"decision_id"`
	StepID     string `json:"step_id"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
}
type TeamQuotaRequestContext struct {
	TeamID            string            `json:"team_id"`
	Dimension         string            `json:"dimension"`
	Currency          *string           `json:"currency"`
	PlatformCurrency  string            `json:"platform_currency"`
	MemberStored      *string           `json:"member_stored"`
	MemberEffective   *string           `json:"member_effective"`
	TeamStored        *string           `json:"team_stored"`
	TeamEffective     *string           `json:"team_effective"`
	MemberPolicyETag  string            `json:"member_policy_etag"`
	TeamPolicyETag    string            `json:"team_policy_etag"`
	MemberUsage       *QuotaUsageRecord `json:"member_usage"`
	TeamUsage         *QuotaUsageRecord `json:"team_usage"`
	TimeZone          string            `json:"time_zone"`
	MonthStart        time.Time         `json:"month_start"`
	MonthEnd          time.Time         `json:"month_end"`
	CalendarETag      string            `json:"calendar_etag"`
	PricingETag       string            `json:"pricing_etag"`
	ResourceCreatedAt time.Time         `json:"resource_created_at"`
	ETag              string            `json:"etag"`
	Eligible          bool              `json:"eligible"`
	Blockers          []string          `json:"blockers"`
}
type TeamQuotaStepRecord struct {
	ID        string     `json:"id"`
	Stage     string     `json:"stage"`
	Status    string     `json:"status"`
	EnteredAt time.Time  `json:"entered_at"`
	ActedAt   *time.Time `json:"acted_at"`
	ActorID   *string    `json:"actor_id"`
	ActorName *string    `json:"actor_name"`
	Reason    string     `json:"reason"`
}
type TeamQuotaApplication struct {
	RuntimeApplied    bool   `json:"runtime_applied"`
	ApplicationStatus string `json:"application_status"`
}
type TeamQuotaRequestRecord struct {
	ID                string                  `json:"id"`
	RequestID         string                  `json:"request_id"`
	TeamID            string                  `json:"team_id"`
	TeamName          string                  `json:"team_name"`
	ApplicantUserID   string                  `json:"applicant_user_id"`
	ApplicantName     string                  `json:"applicant_name"`
	Dimension         string                  `json:"dimension"`
	TargetValue       string                  `json:"target_value"`
	Currency          *string                 `json:"currency"`
	Reason            string                  `json:"reason"`
	Status            string                  `json:"status"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
	ResolvedAt        *time.Time              `json:"resolved_at"`
	SubmittedSnapshot TeamQuotaRequestContext `json:"submitted_snapshot"`
	Steps             []TeamQuotaStepRecord   `json:"steps"`
	CurrentStepID     *string                 `json:"current_step_id"`
	EscalationReason  *string                 `json:"escalation_reason"`
}
type TeamQuotaApprovalPreview struct {
	MemberBefore *string `json:"member_before"`
	MemberAfter  string  `json:"member_after"`
	TeamBefore   *string `json:"team_before"`
	TeamAfter    *string `json:"team_after"`
	Escalates    bool    `json:"escalates"`
}
type TeamQuotaRequestDetail struct {
	TeamQuotaRequestRecord
	CurrentContext     *TeamQuotaRequestContext  `json:"current_context"`
	AllowedActions     []string                  `json:"allowed_actions"`
	ETag               string                    `json:"etag"`
	Application        *TeamQuotaApplication     `json:"application"`
	ApprovalPreview    *TeamQuotaApprovalPreview `json:"approval_preview"`
	WorkspaceAvailable bool                      `json:"workspace_available"`
}
type TeamQuotaDecisionRecord struct {
	DecisionID string                  `json:"decision_id"`
	StepID     string                  `json:"step_id"`
	Action     string                  `json:"action"`
	Committed  bool                    `json:"committed"`
	SavedStep  TeamQuotaStepRecord     `json:"saved_step"`
	Request    *TeamQuotaRequestDetail `json:"request"`
}

type teamQuotaSubject struct {
	Team                     entity.Team
	User                     entity.User
	Member                   entity.TeamMembership
	TeamRow, MemberRow       entity.ResourceLimit
	TeamPolicy, MemberPolicy limits.Policy
	Pricing                  entity.PricingSetting
	Calendar                 entity.QuotaSetting
	Owners                   []entity.TeamMembership
	Context                  *TeamQuotaRequestContext
}

func validTeamQuotaDimension(value string) bool { return value == "tokens" || value == "money" }
func teamQuotaReason(reason string, required bool) bool {
	return (!required || reason != "") && len(reason) <= 2000 && utf8.ValidString(reason) && !strings.ContainsFunc(reason, unicode.IsControl)
}
func canonicalTeamQuotaTarget(dimension, value string) (string, error) {
	if dimension == "money" {
		value, err := pricing.Decimal(value)
		if err != nil {
			return "", apperrors.ErrBadRequest
		}
		return value, nil
	}
	if dimension != "tokens" || value == "" {
		return "", apperrors.ErrBadRequest
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 0 || number > limits.MaxInteger || strconv.FormatInt(number, 10) != value {
		return "", apperrors.ErrBadRequest
	}
	return value, nil
}
func teamQuotaLimit(policy limits.Policy, dimension string) *string {
	if dimension == "money" {
		if policy.MoneyMonth == nil {
			return nil
		}
		value := *policy.MoneyMonth
		return &value
	}
	if policy.TokensMonth == nil {
		return nil
	}
	value := strconv.FormatInt(*policy.TokensMonth, 10)
	return &value
}
func teamQuotaGreater(left string, right *string) bool {
	if right == nil {
		return false
	}
	a, ok := new(big.Rat).SetString(left)
	if !ok {
		return false
	}
	b, ok := new(big.Rat).SetString(*right)
	return ok && a.Cmp(b) > 0
}
func teamQuotaHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return secret.SHA256Hex(string(raw)), nil
}
func quotaExact(tx *gorm.DB, column, value string) *gorm.DB {
	return tx.Where(database.ExactText(tx, clause.Column{Name: column}, value))
}
func optionalTeamQuotaString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func teamQuotaOwners(tx *gorm.DB, teamID, applicantID string) ([]entity.TeamMembership, error) {
	var rows []entity.TeamMembership
	query := tx.Table("team_memberships AS tm").Select("tm.*").Joins("JOIN users AS u ON ?", database.ExactTextColumns(tx, clause.Column{Table: "tm", Name: "user_id"}, clause.Column{Table: "u", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "team_id"}, teamID)).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "role"}, entity.TeamOwner)).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "status"}, entity.ResourceActive)).Where("u.disabled = ? AND u.offboarded_at IS NULL", false).Order("tm.id").Limit(1001)
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 1000 {
		return nil, errTeamQuotaOverflow
	}
	result := []entity.TeamMembership{}
	for _, row := range rows {
		if row.TeamID != teamID || row.Role != entity.TeamOwner || row.Status != entity.ResourceActive || !safeTeamSessionID(row.ID) || !safeTeamSessionID(row.UserID) {
			return nil, apperrors.ErrInternal
		}
		if row.UserID != applicantID {
			result = append(result, row)
		}
	}
	return result, nil
}

func loadTeamQuotaSubject(tx *gorm.DB, teamID, userID string, locked bool) (*teamQuotaSubject, error) {
	result := &teamQuotaSubject{}
	query := quotaExact(tx, "id", teamID)
	if locked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&result.Team).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errTeamQuotaSubject
		}
		return nil, err
	}
	if result.Team.ID != teamID || result.Team.Status != entity.ResourceActive {
		return nil, errTeamQuotaSubject
	}
	if err := quotaExact(tx, "id", userID).First(&result.User).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errTeamQuotaSubject
		}
		return nil, err
	}
	if result.User.ID != userID || result.User.Disabled || result.User.OffboardedAt != nil {
		return nil, errTeamQuotaSubject
	}
	if err := quotaExact(quotaExact(tx, "team_id", teamID), "user_id", userID).First(&result.Member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errTeamQuotaSubject
		}
		return nil, err
	}
	if result.Member.TeamID != teamID || result.Member.UserID != userID || result.Member.Status != entity.ResourceActive || (result.Member.Role != entity.TeamOwner && result.Member.Role != entity.TeamMember) {
		return nil, errTeamQuotaSubject
	}
	var err error
	result.TeamRow, result.TeamPolicy, err = readTeamLimitPolicy(tx, "team", teamID)
	if err != nil {
		return nil, err
	}
	result.MemberRow, result.MemberPolicy, err = readTeamLimitPolicy(tx, "team_member", teamMemberLimitScopeID(teamID, userID))
	if err != nil {
		return nil, err
	}
	if err := tx.First(&result.Pricing, 1).Error; err != nil {
		return nil, err
	}
	if err := tx.First(&result.Calendar, 1).Error; err != nil {
		return nil, err
	}
	result.Owners, err = teamQuotaOwners(tx, teamID, userID)
	return result, err
}
func (s *Service) teamQuotaContext(tx *gorm.DB, current *teamQuotaSubject, dimension string) (*TeamQuotaRequestContext, error) {
	location, err := time.LoadLocation(current.Calendar.TimeZone)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	context := &TeamQuotaRequestContext{TeamID: current.Team.ID, Dimension: dimension, PlatformCurrency: current.Pricing.PlatformCurrency, MemberStored: teamQuotaLimit(current.MemberPolicy, dimension), TeamStored: teamQuotaLimit(current.TeamPolicy, dimension), TeamEffective: teamQuotaLimit(current.TeamPolicy, dimension), MemberPolicyETag: current.MemberRow.ETag, TeamPolicyETag: current.TeamRow.ETag, TimeZone: location.String(), MonthStart: start.UTC(), MonthEnd: start.AddDate(0, 1, 0).UTC(), CalendarETag: current.Calendar.ETag, PricingETag: current.Pricing.ETag, ResourceCreatedAt: current.Team.CreatedAt.UTC(), Blockers: []string{}}
	effective := limits.Policy{TokensMonth: limits.Minimum(current.TeamPolicy.TokensMonth, current.MemberPolicy.TokensMonth), MoneyMonth: limits.MoneyMinimum(current.TeamPolicy.MoneyMonth, current.MemberPolicy.MoneyMonth)}
	context.MemberEffective = teamQuotaLimit(effective, dimension)
	if dimension == "money" {
		context.Currency = optionalTeamQuotaString(current.Pricing.PlatformCurrency)
		for _, policy := range []limits.Policy{current.TeamPolicy, current.MemberPolicy} {
			if policy.MoneyMonth != nil && policy.Currency != current.Pricing.PlatformCurrency {
				context.Blockers = append(context.Blockers, "currency_changed")
				break
			}
		}
	}
	if context.MemberEffective == nil {
		context.Blockers = append(context.Blockers, "member_unlimited")
	}
	applied := s.teamLimitApplied(&teamLimitContext{Team: current.Team, Member: &current.Member, Resolved: resolvedLimitTarget{kind: "team_member", id: teamMemberLimitScopeID(current.Team.ID, current.User.ID)}, Row: current.MemberRow, Stored: current.MemberPolicy, ParentRow: current.TeamRow, Parent: current.TeamPolicy, Pricing: current.Pricing})
	if !applied {
		context.Blockers = append(context.Blockers, "policy_unpublished")
	}
	if s.recorder != nil {
		context.MemberUsage, err = s.resourceQuotaUsage(tx, resolvedLimitTarget{kind: "team_member", id: teamMemberLimitScopeID(current.Team.ID, current.User.ID), teamID: current.Team.ID})
		if err != nil {
			return nil, err
		}
		context.TeamUsage, err = s.resourceQuotaUsage(tx, resolvedLimitTarget{kind: "team", id: current.Team.ID})
		if err != nil {
			return nil, err
		}
	}
	context.Eligible = len(context.Blockers) == 0
	context.ETag, err = teamQuotaHash(struct {
		TeamID, UserID, Dimension                                                 string
		TeamCreatedAt, TeamUpdatedAt                                              time.Time
		Member                                                                    entity.TeamMembership
		Owners                                                                    []entity.TeamMembership
		TeamRevision, MemberRevision, PricingRevision, Currency, CalendarRevision string
		TeamPolicy, MemberPolicy                                                  limits.Policy
		MonthStart                                                                time.Time
	}{current.Team.ID, current.User.ID, dimension, current.Team.CreatedAt.UTC(), current.Team.UpdatedAt.UTC(), current.Member, current.Owners, current.TeamRow.ETag, current.MemberRow.ETag, current.Pricing.ETag, current.Pricing.PlatformCurrency, current.Calendar.ETag, current.TeamPolicy, current.MemberPolicy, context.MonthStart})
	current.Context = context
	return context, err
}
func (s *Service) GetTeamQuotaRequestContext(ctx context.Context, actorID, teamID, dimension string) (*TeamQuotaRequestContext, error) {
	if !validTeamQuotaDimension(dimension) || !safeTeamSessionID(teamID) {
		return nil, apperrors.ErrBadRequest
	}
	var result *TeamQuotaRequestContext
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := exactEnabledActor(tx, actorID); err != nil {
			return err
		}
		current, err := loadTeamQuotaSubject(tx, teamID, actorID, false)
		if errors.Is(err, errTeamQuotaSubject) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return err
		}
		result, err = s.teamQuotaContext(tx, current, dimension)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

// A monthly request changes one dimension only. Other retained child overrides
// may exceed a reduced parent while their effective limits remain conjunctive.
func teamQuotaDimensionWithinParent(dimension string, parent, child limits.Policy) bool {
	if dimension == "tokens" {
		return limits.Narrower(limits.Policy{TokensMonth: parent.TokensMonth}, limits.Policy{TokensMonth: child.TokensMonth})
	}
	return limits.Narrower(limits.Policy{MoneyMonth: parent.MoneyMonth, Currency: parent.Currency}, limits.Policy{MoneyMonth: child.MoneyMonth, Currency: child.Currency})
}
