package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

type TeamQuotaRequestFilter struct {
	View, Status, Dimension, TeamID, Cursor string
	Limit                                   int
}
type TeamQuotaRequestPage struct {
	Items      []TeamQuotaRequestRecord `json:"items"`
	Total      int64                    `json:"total"`
	NextCursor *string                  `json:"next_cursor"`
}
type teamQuotaAccess struct {
	Actor                  entity.User
	ReadAll, Tokens, Money bool
	OwnedTeams             []string
}

func teamQuotaActorAccess(tx *gorm.DB, actorID string) (*teamQuotaAccess, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return nil, err
	}
	result := &teamQuotaAccess{Actor: actor, OwnedTeams: []string{}}
	result.ReadAll, err = exactGovernancePermission(tx, actor, teamQuotaReadAll)
	if err != nil {
		return nil, err
	}
	result.Tokens, err = exactGovernancePermission(tx, actor, "teams.tokens.write")
	if err != nil {
		return nil, err
	}
	result.Money, err = exactGovernancePermission(tx, actor, "teams.money.write")
	if err != nil {
		return nil, err
	}
	var members []entity.TeamMembership
	if err := tx.Table("team_memberships AS tm").Select("tm.*").Joins("JOIN teams AS t ON ?", database.ExactTextColumns(tx, clause.Column{Table: "tm", Name: "team_id"}, clause.Column{Table: "t", Name: "id"})).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "user_id"}, actorID)).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "status"}, entity.ResourceActive)).Where(database.ExactText(tx, clause.Column{Table: "tm", Name: "role"}, entity.TeamOwner)).Where(database.ExactText(tx, clause.Column{Table: "t", Name: "status"}, entity.ResourceActive)).Order("tm.team_id").Limit(1001).Find(&members).Error; err != nil {
		return nil, err
	}
	if len(members) > 1000 {
		return nil, errTeamQuotaOverflow
	}
	for _, member := range members {
		if member.UserID != actorID || member.Status != entity.ResourceActive || member.Role != entity.TeamOwner {
			return nil, apperrors.ErrInternal
		}
		result.OwnedTeams = append(result.OwnedTeams, member.TeamID)
	}
	return result, nil
}
func teamQuotaDimensionScope(tx *gorm.DB, access *teamQuotaAccess) clause.Expression {
	dimensions := []clause.Expression{}
	if access.Tokens {
		dimensions = append(dimensions, teamQuotaEquality(tx, "dimension", "tokens"))
	}
	if access.Money {
		dimensions = append(dimensions, teamQuotaEquality(tx, "dimension", "money"))
	}
	if len(dimensions) == 0 {
		return clause.Expr{SQL: "1 = 0"}
	}
	return clause.Or(dimensions...)
}
func teamQuotaVisible(tx *gorm.DB, access *teamQuotaAccess, view string, admin bool) *gorm.DB {
	if admin {
		return tx
	}
	if view == "my" {
		return quotaExact(tx, "applicant_user_id", access.Actor.ID)
	}
	var owner clause.Expression = clause.Expr{SQL: "1 = 0"}
	if len(access.OwnedTeams) > 0 {
		teams := []clause.Expression{}
		for _, teamID := range access.OwnedTeams {
			teams = append(teams, teamQuotaEquality(tx, "team_id", teamID))
		}
		owner = clause.Or(teams...)
	}
	if view == "pending" {
		return tx.Where(clause.And(clause.Not(teamQuotaEquality(tx, "applicant_user_id", access.Actor.ID)), clause.Or(clause.And(owner, teamQuotaEquality(tx, "status", entity.TeamQuotaRequestPendingOwner)), clause.And(teamQuotaDimensionScope(tx, access), teamQuotaEquality(tx, "status", entity.TeamQuotaRequestPendingAdmin)))))
	}
	// Detail scope includes the applicant, current Team owners, and current
	// dimension reviewers only when a platform stage belongs to this request.
	stage := quotaExact(tx.Session(&gorm.Session{NewDB: true}).Model(&entity.TeamQuotaRequestStep{}).Select("1"), "stage", entity.TeamQuotaStageAdmin).Where(database.ExactTextColumns(tx, clause.Column{Table: "team_quota_request_steps", Name: "request_id"}, clause.Column{Table: "team_quota_requests", Name: "id"}))
	return tx.Where(clause.Or(teamQuotaEquality(tx, "applicant_user_id", access.Actor.ID), owner, clause.And(teamQuotaDimensionScope(tx, access), clause.Expr{SQL: "EXISTS (?)", Vars: []any{stage}})))
}
func teamQuotaStepRecord(step entity.TeamQuotaRequestStep) TeamQuotaStepRecord {
	return TeamQuotaStepRecord{ID: step.ID, Stage: step.Stage, Status: step.Status, EnteredAt: step.EnteredAt.UTC(), ActedAt: step.DecidedAt, ActorID: optionalTeamQuotaString(step.ActorID), ActorName: optionalTeamQuotaString(step.ActorName), Reason: step.Reason}
}
func teamQuotaRecord(tx *gorm.DB, row entity.TeamQuotaRequest) (*TeamQuotaRequestRecord, error) {
	result := &TeamQuotaRequestRecord{ID: row.ID, RequestID: row.RequestID, TeamID: row.TeamID, TeamName: row.TeamName, ApplicantUserID: row.ApplicantUserID, ApplicantName: row.ApplicantName, Dimension: row.Dimension, TargetValue: row.TargetValue, Currency: optionalTeamQuotaString(row.Currency), Reason: row.Reason, Status: row.Status, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), ResolvedAt: row.ResolvedAt, CurrentStepID: row.CurrentStepID, EscalationReason: optionalTeamQuotaString(row.EscalationReason), Steps: []TeamQuotaStepRecord{}}
	if !validTeamQuotaDimension(row.Dimension) || !credentialReplacementRequestID.MatchString(row.RequestID) || json.Unmarshal([]byte(row.SubmittedSnapshotJSON), &result.SubmittedSnapshot) != nil || result.SubmittedSnapshot.TeamID != row.TeamID || result.SubmittedSnapshot.Dimension != row.Dimension {
		return nil, apperrors.ErrInternal
	}
	var steps []entity.TeamQuotaRequestStep
	if err := quotaExact(tx, "request_id", row.ID).Order("ordinal").Limit(3).Find(&steps).Error; err != nil {
		return nil, err
	}
	if len(steps) == 0 || len(steps) > 2 {
		return nil, apperrors.ErrInternal
	}
	for _, step := range steps {
		if !teamQuotaHistoryStep(row.ID, steps, step) {
			return nil, apperrors.ErrInternal
		}
		result.Steps = append(result.Steps, teamQuotaStepRecord(step))
	}
	return result, nil
}
func teamQuotaAllowed(access *teamQuotaAccess, row entity.TeamQuotaRequest, step entity.TeamQuotaRequestStep, current *teamQuotaSubject) []string {
	if !teamQuotaCurrentStep(row, step) || current == nil || current.Member.ID != row.ApplicantMembershipID {
		return []string{}
	}
	if row.ApplicantUserID == access.Actor.ID {
		return []string{"withdraw"}
	}
	allowed := step.Stage == entity.TeamQuotaStageOwner && slices.Contains(access.OwnedTeams, row.TeamID) || step.Stage == entity.TeamQuotaStageAdmin && (row.Dimension == "tokens" && access.Tokens || row.Dimension == "money" && access.Money)
	if !allowed {
		return []string{}
	}
	return []string{"approve", "reject"}
}
func teamQuotaDetailETag(actorID string, row entity.TeamQuotaRequest, context *TeamQuotaRequestContext, steps []TeamQuotaStepRecord) (string, error) {
	contextETag := ""
	if context != nil {
		contextETag = context.ETag
	}
	return teamQuotaHash(struct {
		ActorID, ID, RequestHash, Status, ContextETag string
		CurrentStep                                   *string
		Steps                                         []TeamQuotaStepRecord
	}{actorID, row.ID, row.RequestHash, row.Status, contextETag, row.CurrentStepID, steps})
}
func (s *Service) teamQuotaDetail(tx *gorm.DB, access *teamQuotaAccess, row entity.TeamQuotaRequest, admin bool) (*TeamQuotaRequestDetail, error) {
	record, err := teamQuotaRecord(tx, row)
	if err != nil {
		return nil, err
	}
	result := &TeamQuotaRequestDetail{TeamQuotaRequestRecord: *record, AllowedActions: []string{}}
	current, err := loadTeamQuotaSubject(tx, row.TeamID, row.ApplicantUserID, false)
	if err != nil && !errors.Is(err, errTeamQuotaSubject) {
		return nil, err
	}
	if current != nil && current.Member.ID == row.ApplicantMembershipID {
		result.CurrentContext, err = s.teamQuotaContext(tx, current, row.Dimension)
		if err != nil {
			return nil, err
		}
	}
	if row.CurrentStepID != nil {
		var step entity.TeamQuotaRequestStep
		if err := quotaExact(quotaExact(tx, "request_id", row.ID), "id", *row.CurrentStepID).First(&step).Error; err != nil {
			return nil, err
		}
		actions := teamQuotaAllowed(access, row, step, current)
		result.WorkspaceAvailable = slices.Contains(actions, "approve") || slices.Contains(actions, "reject")
		if !admin {
			result.AllowedActions = actions
			result.ApprovalPreview = teamQuotaApprovalPreview(row, step, result.CurrentContext, actions)
		}
	}
	result.ETag, err = teamQuotaDetailETag(access.Actor.ID, row, result.CurrentContext, result.Steps)
	if err != nil {
		return nil, err
	}
	if row.Status == entity.TeamQuotaRequestApproved {
		result.Application = &TeamQuotaApplication{ApplicationStatus: "pending"}
		if current == nil || current.Member.ID != row.ApplicantMembershipID {
			result.Application.ApplicationStatus = "superseded"
			return result, nil
		}
		var parent, child limits.Policy
		if json.Unmarshal([]byte(row.ApprovedTeamPolicyJSON), &parent) != nil || json.Unmarshal([]byte(row.ApprovedMemberPolicyJSON), &child) != nil {
			return nil, apperrors.ErrInternal
		}
		parent, err = limits.Normalize(parent)
		if err != nil {
			return nil, apperrors.ErrInternal
		}
		child, err = limits.Normalize(child)
		if err != nil {
			return nil, apperrors.ErrInternal
		}
		if current.TeamRow.ETag != row.ApprovedTeamPolicyETag || current.MemberRow.ETag != row.ApprovedMemberPolicyETag || !reflect.DeepEqual(current.TeamPolicy, parent) || !reflect.DeepEqual(current.MemberPolicy, child) {
			result.Application.ApplicationStatus = "superseded"
			return result, nil
		}
		result.Application.RuntimeApplied = s.teamLimitApplied(&teamLimitContext{Team: current.Team, Member: &current.Member, Resolved: resolvedLimitTarget{kind: "team_member", id: teamMemberLimitScopeID(row.TeamID, row.ApplicantUserID)}, Row: current.MemberRow, Stored: child, ParentRow: current.TeamRow, Parent: parent, Pricing: current.Pricing})
		if result.Application.RuntimeApplied {
			result.Application.ApplicationStatus = "applied"
		}
	}
	return result, nil
}
func (s *Service) GetTeamQuotaRequest(ctx context.Context, actorID, requestID string, admin bool) (*TeamQuotaRequestDetail, error) {
	if !safeTeamSessionID(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	var result *TeamQuotaRequestDetail
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		access, err := teamQuotaActorAccess(tx, actorID)
		if err != nil {
			return err
		}
		if admin && !access.ReadAll {
			return apperrors.ErrForbidden
		}
		var row entity.TeamQuotaRequest
		if err := quotaExact(teamQuotaVisible(tx.Model(&entity.TeamQuotaRequest{}), access, "detail", admin), "id", requestID).First(&row).Error; err != nil {
			return err
		}
		if row.ID != requestID {
			return apperrors.ErrNotFound
		}
		result, err = s.teamQuotaDetail(tx, access, row, admin)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func validateTeamQuotaFilter(filter TeamQuotaRequestFilter, admin bool) (TeamQuotaRequestFilter, error) {
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if filter.Limit < 1 || filter.Limit > 50 || len(filter.Cursor) > 300 || filter.Dimension != "" && !validTeamQuotaDimension(filter.Dimension) || filter.TeamID != "" && !safeTeamSessionID(filter.TeamID) {
		return filter, apperrors.ErrBadRequest
	}
	if !admin && filter.View != "my" && filter.View != "pending" || admin && filter.View != "" {
		return filter, apperrors.ErrBadRequest
	}
	if filter.Status != "" && !slices.Contains([]string{entity.TeamQuotaRequestPendingOwner, entity.TeamQuotaRequestPendingAdmin, entity.TeamQuotaRequestApproved, entity.TeamQuotaRequestRejected, entity.TeamQuotaRequestWithdrawn, entity.TeamQuotaRequestCancelled}, filter.Status) {
		return filter, apperrors.ErrBadRequest
	}
	return filter, nil
}
func teamQuotaCursorScope(actorID string, filter TeamQuotaRequestFilter, admin bool) string {
	copy := filter
	copy.Cursor = ""
	copy.Limit = 0
	value, _ := teamQuotaHash(struct {
		ActorID string
		Filter  TeamQuotaRequestFilter
		Admin   bool
	}{actorID, copy, admin})
	return value
}
func decodeTeamQuotaCursor(raw, scope string) (time.Time, string, error) {
	if raw == "" {
		return time.Time{}, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", apperrors.ErrBadRequest
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 || parts[2] != scope || !safeTeamSessionID(parts[1]) {
		return time.Time{}, "", apperrors.ErrBadRequest
	}
	instant, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || instant <= 0 {
		return time.Time{}, "", apperrors.ErrBadRequest
	}
	return time.UnixMicro(instant).UTC(), parts[1], nil
}
func (s *Service) ListTeamQuotaRequests(ctx context.Context, actorID string, filter TeamQuotaRequestFilter, admin bool) (*TeamQuotaRequestPage, error) {
	filter, err := validateTeamQuotaFilter(filter, admin)
	if err != nil {
		return nil, err
	}
	scope := teamQuotaCursorScope(actorID, filter, admin)
	instant, lastID, err := decodeTeamQuotaCursor(filter.Cursor, scope)
	if err != nil {
		return nil, err
	}
	result := &TeamQuotaRequestPage{Items: []TeamQuotaRequestRecord{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		access, err := teamQuotaActorAccess(tx, actorID)
		if err != nil {
			return err
		}
		if admin && !access.ReadAll {
			return apperrors.ErrForbidden
		}
		query := teamQuotaVisible(tx.Model(&entity.TeamQuotaRequest{}), access, filter.View, admin)
		if filter.Status != "" {
			query = quotaExact(query, "status", filter.Status)
		}
		if filter.Dimension != "" {
			query = quotaExact(query, "dimension", filter.Dimension)
		}
		if filter.TeamID != "" {
			query = quotaExact(query, "team_id", filter.TeamID)
		}
		if err := query.Session(&gorm.Session{}).Count(&result.Total).Error; err != nil {
			return err
		}
		if result.Total > limits.MaxInteger {
			return errTeamQuotaOverflow
		}
		if lastID != "" {
			query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", instant, instant, lastID)
		}
		var rows []entity.TeamQuotaRequest
		if err := query.Order("created_at DESC, id DESC").Limit(filter.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		more := len(rows) > filter.Limit
		if more {
			rows = rows[:filter.Limit]
		}
		for _, row := range rows {
			record, err := teamQuotaRecord(tx, row)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, *record)
		}
		if more {
			last := rows[len(rows)-1]
			cursor := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(last.CreatedAt.UTC().UnixMicro(), 10) + "|" + last.ID + "|" + scope))
			result.NextCursor = &cursor
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}

// The preview describes only this reviewed stage's effects, never a future
// approval. Owner escalation deliberately leaves both effective caps unchanged.
func teamQuotaApprovalPreview(row entity.TeamQuotaRequest, step entity.TeamQuotaRequestStep, current *TeamQuotaRequestContext, actions []string) *TeamQuotaApprovalPreview {
	if !slices.Contains(actions, "approve") || current == nil || !current.Eligible || current.MemberEffective == nil || current.TeamID != row.TeamID || current.Dimension != row.Dimension || !teamQuotaCurrentStep(row, step) {
		return nil
	}
	if value, err := canonicalTeamQuotaTarget(row.Dimension, row.TargetValue); err != nil || value != row.TargetValue || !teamQuotaGreater(row.TargetValue, current.MemberEffective) {
		return nil
	}
	if row.Dimension == "money" && (current.Currency == nil || *current.Currency != row.Currency || current.PlatformCurrency != row.Currency) {
		return nil
	}
	result := &TeamQuotaApprovalPreview{MemberBefore: current.MemberEffective, MemberAfter: row.TargetValue, TeamBefore: current.TeamEffective, TeamAfter: current.TeamEffective}
	overflow := teamQuotaGreater(row.TargetValue, current.TeamEffective)
	if overflow && step.Stage == entity.TeamQuotaStageOwner {
		result.Escalates = true
		result.MemberAfter = *current.MemberEffective
	} else if overflow {
		target := row.TargetValue
		result.TeamAfter = &target
	}
	return result
}

// Pending request state and the selected step must describe the same stage.
// Historical receipts are reconciled before this fresh-decision guard.
func teamQuotaCurrentStep(row entity.TeamQuotaRequest, step entity.TeamQuotaRequestStep) bool {
	if row.CurrentStepID == nil || *row.CurrentStepID != step.ID || step.RequestID != row.ID || step.Status != entity.TeamQuotaStepPending {
		return false
	}
	switch row.Status {
	case entity.TeamQuotaRequestPendingOwner:
		return step.Stage == entity.TeamQuotaStageOwner && step.Ordinal == 1
	case entity.TeamQuotaRequestPendingAdmin:
		return step.Stage == entity.TeamQuotaStageAdmin && (step.Ordinal == 1 || step.Ordinal == 2)
	default:
		return false
	}
}
func teamQuotaHistoryStep(requestID string, steps []entity.TeamQuotaRequestStep, step entity.TeamQuotaRequestStep) bool {
	if step.RequestID != requestID || step.Ordinal < 1 || step.Ordinal > len(steps) || steps[step.Ordinal-1].ID != step.ID {
		return false
	}
	if step.Ordinal == 1 {
		return step.Stage == entity.TeamQuotaStageOwner || step.Stage == entity.TeamQuotaStageAdmin
	}
	return step.Stage == entity.TeamQuotaStageAdmin && steps[0].Stage == entity.TeamQuotaStageOwner && (steps[0].Status == entity.TeamQuotaStepApproved || steps[0].Status == entity.TeamQuotaStepCancelled)
}
