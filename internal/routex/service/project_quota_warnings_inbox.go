package service

import (
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type projectQuotaWarningInboxRow struct {
	RecipientCreatedAt      time.Time
	CurrentProjectID        string
	CurrentProjectCreatedAt time.Time
	CurrentProjectStatus    string
	ID                      string
	RecipientID             string
	ObservationID           string
	ReadAt                  *time.Time
	CreatedAt               time.Time
	Observation             entity.ProjectQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const projectQuotaWarningInboxSelect = `project_quota_warning_inboxes.id, project_quota_warning_inboxes.recipient_id, project_quota_warning_inboxes.observation_id, project_quota_warning_inboxes.read_at, project_quota_warning_inboxes.created_at, project_quota_warning_inboxes.recipient_created_at, project.id AS current_project_id, project.created_at AS current_project_created_at, project.status AS current_project_status,
 warning.id AS warning_id, warning.project_id AS warning_project_id, warning.project_name AS warning_project_name, warning.dimension AS warning_dimension, warning.policy_revision AS warning_policy_revision,
 warning.month_start AS warning_month_start, warning.month_end AS warning_month_end, warning.time_zone AS warning_time_zone, warning.as_of AS warning_as_of,
 warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.currency AS warning_currency, warning.level AS warning_level,
 warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation, warning.coverage_start AS warning_coverage_start, warning.resource_created_at AS warning_resource_created_at`

func projectQuotaWarningProjectJoin(tx *gorm.DB) *gorm.DB {
	return tx.Joins("JOIN projects AS project ON project.id = warning.project_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "project", Name: "id"}, clause.Column{Table: "warning", Name: "project_id"})).
		Where("project.created_at = warning.resource_created_at").
		Where(database.ExactText(tx, clause.Column{Table: "project", Name: "status"}, entity.ResourceActive))
}
func projectQuotaWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	ids := []clause.Expression{}
	for _, id := range access.ProjectIDs {
		ids = append(ids, database.ExactText(tx, clause.Column{Table: "warning", Name: "project_id"}, id))
	}
	if len(ids) == 0 || access.ActorCreatedAt.IsZero() {
		return tx.Where("1 = 0")
	}
	return tx.Where(clause.Or(ids...)).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, projectQuotaWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "tokens"), database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "money"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func projectQuotaWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return projectQuotaWarningProjectJoin(tx.Table("project_quota_warning_inboxes").Joins("JOIN project_quota_warning_observations AS warning ON warning.id = project_quota_warning_inboxes.observation_id")).Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "project_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where("project_quota_warning_inboxes.recipient_created_at = ?", access.ActorCreatedAt).Where(projectQuotaWarningScope(tx, access))
}
func projectQuotaWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := projectQuotaWarningProjectJoin(tx.Table("project_quota_warning_observations AS warning")).Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_quota_warning_inboxes", Name: "observation_id"})).Where(projectQuotaWarningScope(tx, access))
	return tx.Model(&entity.ProjectQuotaWarningInbox{}).Where("recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validProjectQuotaWarningInbox(row projectQuotaWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	level, threshold := quotaWarningLevel(v.Settled, v.Limit)
	return !access.ActorCreatedAt.IsZero() && row.RecipientCreatedAt.Equal(access.ActorCreatedAt) && !v.ResourceCreatedAt.IsZero() && row.CurrentProjectID == v.ProjectID && row.CurrentProjectCreatedAt.Equal(v.ResourceCreatedAt) && row.CurrentProjectStatus == entity.ResourceActive && slices.Contains(access.ProjectIDs, v.ProjectID) && row.RecipientID == access.ActorID && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && safeTeamSessionID(v.ID) && v.ThresholdGeneration == projectQuotaWarningGeneration && (v.Dimension == "tokens" && v.Currency == "" || v.Dimension == "money" && len(v.Currency) == 3) && level == v.Level && threshold == v.Threshold && level != ""
}
func projectQuotaWarningRecord(row projectQuotaWarningInboxRow) NotificationRecord {
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
	return NotificationRecord{ID: row.ID, QuotaWarningObservationID: v.ID, QuotaWarning: &QuotaWarningSnapshot{ScopeKind: "project", ScopeID: v.ProjectID, Dimension: v.Dimension, PolicyRevision: v.PolicyRevision, MonthStart: v.MonthStart, MonthEnd: v.MonthEnd, TimeZone: v.TimeZone, AsOf: v.AsOf, Limit: v.Limit, Settled: v.Settled, Currency: currency, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}, Kind: "monthly_quota_warning", Severity: severity, DetailCode: v.Dimension + "_month_" + v.Level, SubjectType: "project", SubjectID: v.ProjectID, SubjectName: v.ProjectName, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func projectQuotaWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := projectQuotaWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("project_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("project_quota_warning_inboxes.created_at < ? OR (project_quota_warning_inboxes.created_at = ? AND project_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []projectQuotaWarningInboxRow
	if err := query.Select(projectQuotaWarningInboxSelect).Order("project_quota_warning_inboxes.created_at DESC, project_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validProjectQuotaWarningInbox(row, access) {
			records = append(records, projectQuotaWarningRecord(row))
		}
	}
	var unread int64
	err := projectQuotaWarningInboxQuery(tx, access).Where("project_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markProjectQuotaWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row projectQuotaWarningInboxRow
	err := projectQuotaWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "project_quota_warning_inboxes", Name: "id"}, noticeID)).Select(projectQuotaWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validProjectQuotaWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := projectQuotaWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return projectQuotaWarningRecord(row), true, nil
}
