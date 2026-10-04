package service

import (
	"errors"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type quotaInboxAccess struct {
	ActorID     string
	Operational bool
	ProjectIDs  []string
	TeamIDs     []string
}

const quotaInboxManagerLimit = 1000

type quotaManagerIdentity struct {
	ManagerUserID    string
	ManagerProjectID string
	UserID           string
	ProjectID        string
	ProjectStatus    string
	Disabled         bool
	Offboarded       bool
}

func validQuotaManager(row quotaManagerIdentity, actorID, projectID string) bool {
	return row.ManagerUserID == row.UserID && row.ManagerProjectID == row.ProjectID &&
		(actorID == "" || row.UserID == actorID) && (projectID == "" || row.ProjectID == projectID) &&
		row.ProjectStatus == entity.ResourceActive && !row.Disabled && !row.Offboarded
}

func quotaManagerQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("project_managers AS manager").
		Select("manager.user_id AS manager_user_id, manager.project_id AS manager_project_id, actor.id AS user_id, project.id AS project_id, project.status AS project_status, actor.disabled, actor.offboarded_at IS NOT NULL AS offboarded").
		Joins("JOIN users AS actor ON actor.id = manager.user_id").
		Joins("JOIN projects AS project ON project.id = manager.project_id").
		Where("actor.disabled = ? AND actor.offboarded_at IS NULL", false).
		Where(database.ExactText(tx, clause.Column{Table: "project", Name: "status"}, entity.ResourceActive)).
		Where(database.ExactTextColumns(tx, clause.Column{Table: "actor", Name: "id"}, clause.Column{Table: "manager", Name: "user_id"})).
		Where(database.ExactTextColumns(tx, clause.Column{Table: "project", Name: "id"}, clause.Column{Table: "manager", Name: "project_id"}))
}

type quotaTeamIdentity struct {
	MembershipID     string
	MembershipUserID string
	MembershipTeamID string
	UserID           string
	TeamID           string
	TeamStatus       string
	MembershipStatus string
	Role             string
	Disabled         bool
	Offboarded       bool
}

func validQuotaTeamMember(row quotaTeamIdentity, actorID, teamID string) bool {
	return safeTeamSessionID(row.MembershipID) && safeTeamSessionID(row.UserID) && safeTeamSessionID(row.TeamID) &&
		row.MembershipUserID == row.UserID && row.MembershipTeamID == row.TeamID &&
		(actorID == "" || row.UserID == actorID) && (teamID == "" || row.TeamID == teamID) &&
		row.TeamStatus == entity.ResourceActive && row.MembershipStatus == entity.ResourceActive &&
		(row.Role == entity.TeamOwner || row.Role == entity.TeamMember) && !row.Disabled && !row.Offboarded
}

func quotaTeamQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("team_memberships AS member").
		Select("member.id AS membership_id, member.user_id AS membership_user_id, member.team_id AS membership_team_id, actor.id AS user_id, team.id AS team_id, team.status AS team_status, member.status AS membership_status, member.role, actor.disabled, actor.offboarded_at IS NOT NULL AS offboarded").
		Joins("JOIN users AS actor ON actor.id = member.user_id").
		Joins("JOIN teams AS team ON team.id = member.team_id").
		Where("actor.disabled = ? AND actor.offboarded_at IS NULL", false).
		Where(database.ExactText(tx, clause.Column{Table: "team", Name: "status"}, entity.ResourceActive)).
		Where(database.ExactText(tx, clause.Column{Table: "member", Name: "status"}, entity.ResourceActive)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "member", Name: "role"}, entity.TeamOwner), database.ExactText(tx, clause.Column{Table: "member", Name: "role"}, entity.TeamMember))).
		Where(database.ExactTextColumns(tx, clause.Column{Table: "actor", Name: "id"}, clause.Column{Table: "member", Name: "user_id"})).
		Where(database.ExactTextColumns(tx, clause.Column{Table: "team", Name: "id"}, clause.Column{Table: "member", Name: "team_id"}))
}

func quotaTeamRecipients(tx *gorm.DB, scopeID string) ([]string, error) {
	var rows []quotaTeamIdentity
	if err := quotaTeamQuery(tx).Where(database.ExactText(tx, clause.Column{Table: "team", Name: "id"}, scopeID)).
		Limit(quotaInboxManagerLimit + 1).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > quotaInboxManagerLimit {
		return nil, apperrors.ErrBadRequest
	}
	recipients := make([]string, 0, len(rows))
	for _, row := range rows {
		if validQuotaTeamMember(row, "", scopeID) {
			recipients = append(recipients, row.UserID)
		}
	}
	slices.Sort(recipients)
	return slices.Compact(recipients), nil
}

// quotaNotificationRecipients is called inside the observer's governance-locked
// transaction. Recorded recipients never expand when an observation is replayed.
func quotaNotificationRecipients(tx *gorm.DB, scopeKind, scopeID string) ([]string, error) {
	if scopeKind == "user" {
		var user entity.User
		err := tx.Where("id = ? AND disabled = ? AND offboarded_at IS NULL", scopeID, false).First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if user.ID != scopeID {
			return nil, nil
		}
		return []string{user.ID}, nil
	}
	if scopeKind == "team" {
		return quotaTeamRecipients(tx, scopeID)
	}
	if scopeKind != "project" {
		return nil, nil
	}
	var rows []quotaManagerIdentity
	if err := quotaManagerQuery(tx).Where(database.ExactText(tx, clause.Column{Table: "project", Name: "id"}, scopeID)).
		Limit(quotaInboxManagerLimit + 1).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > quotaInboxManagerLimit {
		return nil, apperrors.ErrBadRequest
	}
	recipients := make([]string, 0, len(rows))
	for _, row := range rows {
		if validQuotaManager(row, "", scopeID) {
			recipients = append(recipients, row.UserID)
		}
	}
	slices.Sort(recipients)
	return slices.Compact(recipients), nil
}

func loadQuotaInboxAccess(tx *gorm.DB, actorID string) (quotaInboxAccess, error) {
	access := quotaInboxAccess{ActorID: actorID}
	var actor entity.User
	err := tx.Where("id = ? AND disabled = ? AND offboarded_at IS NULL", actorID, false).First(&actor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && actor.ID != actorID {
		return access, apperrors.ErrUnauthorized
	}
	if err != nil {
		return access, err
	}
	operational, err := quotaInboxOperationalAuthority(tx, actor)
	if err != nil {
		return access, err
	}
	access.Operational = operational
	var rows []quotaManagerIdentity
	if err := quotaManagerQuery(tx).Where(database.ExactText(tx, clause.Column{Table: "actor", Name: "id"}, actorID)).
		Limit(quotaInboxManagerLimit + 1).Scan(&rows).Error; err != nil {
		return access, err
	}
	if len(rows) > quotaInboxManagerLimit {
		return access, apperrors.ErrBadRequest
	}
	for _, row := range rows {
		if validQuotaManager(row, actorID, "") {
			access.ProjectIDs = append(access.ProjectIDs, row.ProjectID)
		}
	}
	slices.Sort(access.ProjectIDs)
	access.ProjectIDs = slices.Compact(access.ProjectIDs)
	// Aggregate history follows current Team read authority. Rejoining restores
	// only the original user's recorded inbox rows and preserves their read state.
	// A membership generation is an invocation proof, not an inbox entitlement.
	var memberships []quotaTeamIdentity
	if err := quotaTeamQuery(tx).Where(database.ExactText(tx, clause.Column{Table: "actor", Name: "id"}, actorID)).
		Limit(quotaInboxManagerLimit + 1).Scan(&memberships).Error; err != nil {
		return access, err
	}
	if len(memberships) > quotaInboxManagerLimit {
		return access, apperrors.ErrBadRequest
	}
	for _, membership := range memberships {
		if validQuotaTeamMember(membership, actorID, "") {
			access.TeamIDs = append(access.TeamIDs, membership.TeamID)
		}
	}
	slices.Sort(access.TeamIDs)
	access.TeamIDs = slices.Compact(access.TeamIDs)
	return access, nil
}

type quotaInboxRow struct {
	ID                 string
	RecipientID        string
	ReadAt             *time.Time
	CreatedAt          time.Time
	InboxObservationID string                              `gorm:"column:inbox_observation_id"`
	Observation        entity.QuotaNotificationObservation `gorm:"embedded;embeddedPrefix:observation_"`
}

const quotaInboxSelect = `quota_notification_inboxes.id, quota_notification_inboxes.observation_id AS inbox_observation_id,
 quota_notification_inboxes.recipient_id, quota_notification_inboxes.read_at, quota_notification_inboxes.created_at,
 observation.id AS observation_id, observation.scope_kind AS observation_scope_kind,
 observation.scope_id AS observation_scope_id, observation.scope_name AS observation_scope_name,
 observation.dimension AS observation_dimension, observation.policy_revision AS observation_policy_revision,
 observation.month_start AS observation_month_start, observation.month_end AS observation_month_end,
 observation.time_zone AS observation_time_zone, observation.as_of AS observation_as_of,
 observation.limit_value AS observation_limit_value, observation.settled_value AS observation_settled_value,
 observation.currency AS observation_currency`

func quotaObservationScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	personal := tx.Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_kind"}, "user")).
		Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_id"}, access.ActorID))
	scope := tx.Where(personal)
	for _, projectID := range access.ProjectIDs {
		scope = scope.Or(tx.Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_kind"}, "project")).
			Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_id"}, projectID)))
	}
	for _, teamID := range access.TeamIDs {
		scope = scope.Or(tx.Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_kind"}, "team")).
			Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_id"}, teamID)))
	}
	dimensions := tx.Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "dimension"}, "tokens")).
		Or(database.ExactText(tx, clause.Column{Table: "observation", Name: "dimension"}, "money"))
	return scope.Where(dimensions)
}

func quotaInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Table("quota_notification_inboxes").
		Joins("JOIN quota_notification_observations AS observation ON observation.id = quota_notification_inboxes.observation_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "observation", Name: "id"}, clause.Column{Table: "quota_notification_inboxes", Name: "observation_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "quota_notification_inboxes", Name: "recipient_id"}, access.ActorID)).
		Where(quotaObservationScope(tx, access))
}

func quotaInboxMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := tx.Table("quota_notification_observations AS observation").Select("1").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "observation", Name: "id"}, clause.Column{Table: "quota_notification_inboxes", Name: "observation_id"})).
		Where(quotaObservationScope(tx, access))
	return tx.Model(&entity.QuotaNotificationInbox{}).
		Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).
		Where("EXISTS (?)", observations)
}

func validQuotaInboxRow(row quotaInboxRow, access quotaInboxAccess) bool {
	observation := row.Observation
	return row.RecipientID == access.ActorID && row.InboxObservationID == observation.ID &&
		(observation.Dimension == "tokens" || observation.Dimension == "money") &&
		(observation.ScopeKind == "user" && observation.ScopeID == access.ActorID ||
			observation.ScopeKind == "project" && slices.Contains(access.ProjectIDs, observation.ScopeID) ||
			observation.ScopeKind == "team" && slices.Contains(access.TeamIDs, observation.ScopeID))
}

func quotaNotificationRecord(row quotaInboxRow) NotificationRecord {
	observation := row.Observation
	var currency *string
	if observation.Dimension == "money" {
		value := observation.Currency
		currency = &value
	}
	snapshot := &QuotaNotificationSnapshot{
		ScopeKind: observation.ScopeKind, ScopeID: observation.ScopeID, Dimension: observation.Dimension,
		PolicyRevision: observation.PolicyRevision, MonthStart: observation.MonthStart, MonthEnd: observation.MonthEnd,
		TimeZone: observation.TimeZone, AsOf: observation.AsOf, Limit: observation.Limit, Settled: observation.Settled, Currency: currency,
	}
	return NotificationRecord{
		ID: row.ID, QuotaObservationID: observation.ID, Quota: snapshot,
		Kind: "monthly_quota_exhausted", Severity: "high", DetailCode: observation.Dimension + "_month_exhausted",
		SubjectType: observation.ScopeKind, SubjectID: observation.ScopeID, SubjectName: observation.ScopeName,
		OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: observation.AsOf, LastSeenAt: row.CreatedAt,
	}
}

// Operational history retains its current permission boundary.
func quotaInboxOperationalAuthority(tx *gorm.DB, actor entity.User) (bool, error) {
	return exactGovernancePermission(tx, actor, "system.read")
}
