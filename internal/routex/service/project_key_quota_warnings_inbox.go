package service

import (
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type projectKeyQuotaWarningInboxRow struct {
	RecipientCreatedAt      time.Time
	CurrentProjectID        string
	CurrentProjectCreatedAt time.Time
	CurrentProjectStatus    string
	CurrentRootKeyID        string
	CurrentRootCreatedAt    time.Time
	CurrentRootProjectID    string
	CurrentRootParent       *string
	ID                      string
	RecipientID             string
	ObservationID           string
	ReadAt                  *time.Time
	CreatedAt               time.Time
	Observation             entity.ProjectKeyQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const projectKeyQuotaWarningInboxSelect = `project_key_quota_warning_inboxes.id, project_key_quota_warning_inboxes.recipient_id, project_key_quota_warning_inboxes.observation_id, project_key_quota_warning_inboxes.read_at, project_key_quota_warning_inboxes.created_at, project_key_quota_warning_inboxes.recipient_created_at, root.id AS current_root_key_id, root.created_at AS current_root_created_at, root.project_id AS current_root_project_id, root.replaces_key_id AS current_root_parent, project.id AS current_project_id, project.created_at AS current_project_created_at, project.status AS current_project_status,
 warning.id AS warning_id, warning.root_key_id AS warning_root_key_id, warning.project_id AS warning_project_id, warning.project_created_at AS warning_project_created_at, warning.root_key_name AS warning_root_key_name, warning.dimension AS warning_dimension, warning.policy_revision AS warning_policy_revision,
 warning.month_start AS warning_month_start, warning.month_end AS warning_month_end, warning.time_zone AS warning_time_zone, warning.as_of AS warning_as_of,
 warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.currency AS warning_currency, warning.level AS warning_level,
 warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation, warning.coverage_start AS warning_coverage_start, warning.resource_created_at AS warning_resource_created_at`

func projectKeyQuotaWarningRootJoin(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Joins("JOIN project_api_keys AS root ON root.id = warning.root_key_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "root", Name: "id"}, clause.Column{Table: "warning", Name: "root_key_id"})).
		Where(database.ExactTextColumns(tx, clause.Column{Table: "root", Name: "project_id"}, clause.Column{Table: "warning", Name: "project_id"})).
		Where("root.created_at = warning.resource_created_at AND root.replaces_key_id IS NULL").
		Joins("JOIN projects AS project ON project.id = warning.project_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "project", Name: "id"}, clause.Column{Table: "warning", Name: "project_id"})).
		Where("project.created_at = warning.project_created_at").
		Where(database.ExactText(tx, clause.Column{Table: "project", Name: "status"}, entity.ResourceActive))
}
func projectKeyQuotaWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	ids := []clause.Expression{}
	for _, projectID := range access.ProjectIDs {
		ids = append(ids, database.ExactText(tx, clause.Column{Table: "warning", Name: "project_id"}, projectID))
	}
	if access.ActorCreatedAt.IsZero() || len(ids) == 0 {
		return tx.Where("1 = 0")
	}
	return tx.Where(clause.Or(ids...)).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, projectKeyQuotaWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "tokens"), database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "money"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func projectKeyQuotaWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return projectKeyQuotaWarningRootJoin(tx.Table("project_key_quota_warning_inboxes").Joins("JOIN project_key_quota_warning_observations AS warning ON warning.id = project_key_quota_warning_inboxes.observation_id"), access).Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_key_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "project_key_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where("project_key_quota_warning_inboxes.recipient_created_at = ?", access.ActorCreatedAt).Where(projectKeyQuotaWarningScope(tx, access))
}
func projectKeyQuotaWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := projectKeyQuotaWarningRootJoin(tx.Table("project_key_quota_warning_observations AS warning"), access).Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_key_quota_warning_inboxes", Name: "observation_id"})).Where(projectKeyQuotaWarningScope(tx, access))
	return tx.Model(&entity.ProjectKeyQuotaWarningInbox{}).Where("recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validProjectKeyQuotaWarningInbox(row projectKeyQuotaWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	level, threshold := quotaWarningLevel(v.Settled, v.Limit)
	return !access.ActorCreatedAt.IsZero() && row.RecipientCreatedAt.Equal(access.ActorCreatedAt) && !v.ResourceCreatedAt.IsZero() && row.CurrentRootKeyID == v.RootKeyID && row.CurrentRootProjectID == v.ProjectID && row.CurrentRootParent == nil && row.CurrentRootCreatedAt.Equal(v.ResourceCreatedAt) && slices.Contains(access.ProjectIDs, v.ProjectID) && !v.ProjectCreatedAt.IsZero() && row.CurrentProjectID == v.ProjectID && row.CurrentProjectStatus == entity.ResourceActive && row.CurrentProjectCreatedAt.Equal(v.ProjectCreatedAt) && projectWarningKeyID(v.RootKeyID) && validCatalogLabel(v.RootKeyName) && row.RecipientID == access.ActorID && !v.MonthStart.IsZero() && v.MonthEnd.After(v.MonthStart) && !v.AsOf.Before(v.MonthStart) && v.AsOf.Before(v.MonthEnd) && !v.CoverageStart.IsZero() && !v.CoverageStart.After(v.AsOf) && !v.ResourceCreatedAt.After(v.AsOf) && !v.ProjectCreatedAt.After(v.ResourceCreatedAt) && v.PolicyRevision != "" && len(v.PolicyRevision) <= 64 && safeCallID.MatchString(v.PolicyRevision) && v.TimeZone != "" && v.TimeZone != "Local" && len(v.TimeZone) <= 100 && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && strings.HasPrefix(row.ID, "jwi_") && safeTeamSessionID(v.ID) && strings.HasPrefix(v.ID, "jwo_") && v.ThresholdGeneration == projectKeyQuotaWarningGeneration && (v.Dimension == "tokens" && v.Currency == "" || v.Dimension == "money" && pricing.Currency(v.Currency)) && level == v.Level && threshold == v.Threshold && level != ""
}
func projectKeyQuotaWarningRecord(row projectKeyQuotaWarningInboxRow) NotificationRecord {
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
	return NotificationRecord{ID: row.ID, QuotaWarningObservationID: v.ID, QuotaWarning: &QuotaWarningSnapshot{ScopeKind: "project_key", ScopeID: v.RootKeyID, Dimension: v.Dimension, PolicyRevision: v.PolicyRevision, MonthStart: v.MonthStart, MonthEnd: v.MonthEnd, TimeZone: v.TimeZone, AsOf: v.AsOf, Limit: v.Limit, Settled: v.Settled, Currency: currency, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}, Kind: "monthly_quota_warning", Severity: severity, DetailCode: v.Dimension + "_month_" + v.Level, SubjectType: "project_key", SubjectID: v.RootKeyID, SubjectName: v.RootKeyName, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func projectKeyQuotaWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := projectKeyQuotaWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("project_key_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("project_key_quota_warning_inboxes.created_at < ? OR (project_key_quota_warning_inboxes.created_at = ? AND project_key_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []projectKeyQuotaWarningInboxRow
	if err := query.Select(projectKeyQuotaWarningInboxSelect).Order("project_key_quota_warning_inboxes.created_at DESC, project_key_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validProjectKeyQuotaWarningInbox(row, access) {
			records = append(records, projectKeyQuotaWarningRecord(row))
		}
	}
	var unread int64
	err := projectKeyQuotaWarningInboxQuery(tx, access).Where("project_key_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markProjectKeyQuotaWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row projectKeyQuotaWarningInboxRow
	err := projectKeyQuotaWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "project_key_quota_warning_inboxes", Name: "id"}, noticeID)).Select(projectKeyQuotaWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validProjectKeyQuotaWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := projectKeyQuotaWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return projectKeyQuotaWarningRecord(row), true, nil
}
