package service

import (
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type teamMemberQuotaWarningInboxRow struct {
	RecipientCreatedAt   time.Time
	CurrentTeamID        string
	CurrentTeamCreatedAt time.Time
	CurrentTeamStatus    string
	ID                   string
	RecipientID          string
	ObservationID        string
	ReadAt               *time.Time
	CreatedAt            time.Time
	Observation          entity.TeamMemberQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const teamMemberQuotaWarningInboxSelect = `team_member_quota_warning_inboxes.id, team_member_quota_warning_inboxes.recipient_id, team_member_quota_warning_inboxes.observation_id, team_member_quota_warning_inboxes.read_at, team_member_quota_warning_inboxes.created_at, team_member_quota_warning_inboxes.recipient_created_at, team.id AS current_team_id, team.created_at AS current_team_created_at, team.status AS current_team_status,
 warning.id AS warning_id, warning.scope_id AS warning_scope_id, warning.team_id AS warning_team_id, warning.member_user_id AS warning_member_user_id, warning.user_created_at AS warning_user_created_at, warning.team_name AS warning_team_name, warning.dimension AS warning_dimension, warning.policy_revision AS warning_policy_revision,
 warning.month_start AS warning_month_start, warning.month_end AS warning_month_end, warning.time_zone AS warning_time_zone, warning.as_of AS warning_as_of,
 warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.currency AS warning_currency, warning.level AS warning_level,
 warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation, warning.coverage_start AS warning_coverage_start, warning.resource_created_at AS warning_resource_created_at`

func teamMemberQuotaWarningTeamJoin(tx *gorm.DB) *gorm.DB {
	return tx.Joins("JOIN teams AS team ON team.id = warning.team_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "team", Name: "id"}, clause.Column{Table: "warning", Name: "team_id"})).
		Where("team.created_at = warning.resource_created_at").
		Where(database.ExactText(tx, clause.Column{Table: "team", Name: "status"}, entity.ResourceActive))
}
func teamMemberQuotaWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	ids := []clause.Expression{}
	for _, id := range access.TeamIDs {
		ids = append(ids, clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "team_id"}, id), database.ExactText(tx, clause.Column{Table: "warning", Name: "scope_id"}, teamMemberLimitScopeID(id, access.ActorID))))
	}
	if len(ids) == 0 || access.ActorCreatedAt.IsZero() {
		return tx.Where("1 = 0")
	}
	return tx.Where(clause.Or(ids...)).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "member_user_id"}, access.ActorID)).Where("warning.user_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, teamMemberQuotaWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "tokens"), database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "money"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func teamMemberQuotaWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return teamMemberQuotaWarningTeamJoin(tx.Table("team_member_quota_warning_inboxes").Joins("JOIN team_member_quota_warning_observations AS warning ON warning.id = team_member_quota_warning_inboxes.observation_id")).Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "team_member_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "team_member_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where("team_member_quota_warning_inboxes.recipient_created_at = ?", access.ActorCreatedAt).Where(teamMemberQuotaWarningScope(tx, access))
}
func teamMemberQuotaWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := teamMemberQuotaWarningTeamJoin(tx.Table("team_member_quota_warning_observations AS warning")).Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "team_member_quota_warning_inboxes", Name: "observation_id"})).Where(teamMemberQuotaWarningScope(tx, access))
	return tx.Model(&entity.TeamMemberQuotaWarningInbox{}).Where("recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validTeamMemberQuotaWarningInbox(row teamMemberQuotaWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	return validTeamMemberQuotaWarningObservation(v) && !access.ActorCreatedAt.IsZero() && row.RecipientCreatedAt.Equal(access.ActorCreatedAt) && v.UserCreatedAt.Equal(access.ActorCreatedAt) && v.MemberUserID == access.ActorID && row.RecipientID == access.ActorID && row.CurrentTeamID == v.TeamID && row.CurrentTeamCreatedAt.Equal(v.ResourceCreatedAt) && row.CurrentTeamStatus == entity.ResourceActive && slices.Contains(access.TeamIDs, v.TeamID) && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && safeTeamSessionID(v.ID)
}
func teamMemberQuotaWarningRecord(row teamMemberQuotaWarningInboxRow) NotificationRecord {
	v := row.Observation
	var currency *string
	if v.Dimension == "money" {
		value := v.Currency
		currency = &value
	}
	severity := "medium"
	if v.Level == "critical" {
		severity = "high"
	}
	return NotificationRecord{ID: row.ID, QuotaWarningObservationID: v.ID, QuotaWarning: &QuotaWarningSnapshot{ScopeKind: "team_member", ScopeID: v.ScopeID, Dimension: v.Dimension, PolicyRevision: v.PolicyRevision, MonthStart: v.MonthStart, MonthEnd: v.MonthEnd, TimeZone: v.TimeZone, AsOf: v.AsOf, Limit: v.Limit, Settled: v.Settled, Currency: currency, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}, Kind: "monthly_quota_warning", Severity: severity, DetailCode: v.Dimension + "_month_" + v.Level, SubjectType: "team_member", SubjectID: v.ScopeID, SubjectName: v.TeamName, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func teamMemberQuotaWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := teamMemberQuotaWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("team_member_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("team_member_quota_warning_inboxes.created_at < ? OR (team_member_quota_warning_inboxes.created_at = ? AND team_member_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []teamMemberQuotaWarningInboxRow
	if err := query.Select(teamMemberQuotaWarningInboxSelect).Order("team_member_quota_warning_inboxes.created_at DESC, team_member_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validTeamMemberQuotaWarningInbox(row, access) {
			records = append(records, teamMemberQuotaWarningRecord(row))
		}
	}
	var unread int64
	err := teamMemberQuotaWarningInboxQuery(tx, access).Where("team_member_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markTeamMemberQuotaWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row teamMemberQuotaWarningInboxRow
	err := teamMemberQuotaWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "team_member_quota_warning_inboxes", Name: "id"}, noticeID)).Select(teamMemberQuotaWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validTeamMemberQuotaWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := teamMemberQuotaWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return teamMemberQuotaWarningRecord(row), true, nil
}
