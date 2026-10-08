package service

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProjectRollingQuotaWarningSnapshot struct {
	PersonalRollingQuotaWarningSnapshot
}
type projectRollingWarningInboxRow struct {
	RecipientCreatedAt      time.Time
	CurrentProjectID        string
	CurrentProjectCreatedAt time.Time
	CurrentProjectStatus    string
	ID                      string
	RecipientID             string
	ObservationID           string
	ReadAt                  *time.Time
	CreatedAt               time.Time
	Observation             entity.ProjectRollingQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const projectRollingWarningInboxSelect = `project_rolling_quota_warning_inboxes.id, project_rolling_quota_warning_inboxes.recipient_id, project_rolling_quota_warning_inboxes.observation_id, project_rolling_quota_warning_inboxes.read_at, project_rolling_quota_warning_inboxes.created_at, project_rolling_quota_warning_inboxes.recipient_created_at, project.id AS current_project_id, project.created_at AS current_project_created_at, project.status AS current_project_status,
 warning.id AS warning_id, warning.project_id AS warning_project_id, warning.project_name AS warning_project_name, warning.resource_created_at AS warning_resource_created_at,
 warning.window_kind AS warning_window_kind, warning.episode_id AS warning_episode_id, warning.policy_revision AS warning_policy_revision,
 warning.window_start AS warning_window_start, warning.window_end AS warning_window_end, warning.as_of AS warning_as_of, warning.coverage_start AS warning_coverage_start,
 warning.time_zone AS warning_time_zone, warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.level AS warning_level, warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation`

func projectRollingWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	ids := []clause.Expression{}
	for _, id := range access.ProjectIDs {
		ids = append(ids, database.ExactText(tx, clause.Column{Table: "warning", Name: "project_id"}, id))
	}
	if len(ids) == 0 || access.ActorCreatedAt.IsZero() {
		return tx.Where("1 = 0")
	}
	return tx.Where(clause.Or(ids...)).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, projectRollingWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "5h"), database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "7d"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func projectRollingWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return projectQuotaWarningProjectJoin(tx.Table("project_rolling_quota_warning_inboxes").Joins("JOIN project_rolling_quota_warning_observations AS warning ON warning.id = project_rolling_quota_warning_inboxes.observation_id")).Where("project_rolling_quota_warning_inboxes.recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "project_rolling_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where(projectRollingWarningScope(tx, access))
}
func projectRollingWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := projectQuotaWarningProjectJoin(tx.Table("project_rolling_quota_warning_observations AS warning")).Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(projectRollingWarningScope(tx, access))
	return tx.Model(&entity.ProjectRollingQuotaWarningInbox{}).Where("recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validProjectRollingWarningInbox(row projectRollingWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	duration := personalRollingDuration(v.WindowKind)
	level, threshold := personalRollingWarningLevel(v.Settled, v.Limit)
	coveredFrom := v.WindowStart
	if v.ResourceCreatedAt.After(coveredFrom) {
		coveredFrom = v.ResourceCreatedAt
	}
	_, zoneErr := time.LoadLocation(v.TimeZone)
	return !v.ResourceCreatedAt.IsZero() && !row.RecipientCreatedAt.After(v.AsOf) && !access.ActorCreatedAt.IsZero() && row.RecipientCreatedAt.Equal(access.ActorCreatedAt) && row.CurrentProjectID == v.ProjectID && row.CurrentProjectStatus == entity.ResourceActive && slices.Contains(access.ProjectIDs, v.ProjectID) && row.CurrentProjectCreatedAt.Equal(v.ResourceCreatedAt) && projectMonthlyID(v.ProjectID) && validCatalogLabel(v.ProjectName) && row.RecipientID == access.ActorID && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && strings.HasPrefix(row.ID, "jri_") && safeTeamSessionID(v.ID) && strings.HasPrefix(v.ID, "jro_") && safeTeamSessionID(v.EpisodeID) && safeCallID.MatchString(v.PolicyRevision) && len(v.PolicyRevision) <= 64 && v.ThresholdGeneration == projectRollingWarningGeneration && duration > 0 && v.WindowEnd.Equal(v.AsOf) && v.WindowStart.Equal(v.AsOf.Add(-duration)) && !v.ResourceCreatedAt.After(v.AsOf) && !v.CoverageStart.IsZero() && !coveredFrom.Before(v.CoverageStart) && zoneErr == nil && v.TimeZone != "Local" && len(v.TimeZone) <= 100 && level != "" && level == v.Level && threshold == v.Threshold
}
func projectRollingWarningRecord(row projectRollingWarningInboxRow) NotificationRecord {
	v := row.Observation
	severity := "medium"
	if v.Level == "critical" {
		severity = "high"
	}
	return NotificationRecord{ID: row.ID, RollingQuotaWarningObservationID: v.ID, ProjectRollingQuotaWarning: &ProjectRollingQuotaWarningSnapshot{PersonalRollingQuotaWarningSnapshot: PersonalRollingQuotaWarningSnapshot{ScopeKind: "project", ScopeID: v.ProjectID, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, ResourceCreatedAt: v.ResourceCreatedAt, TimeZone: v.TimeZone, Limit: strconv.FormatInt(v.Limit, 10), Settled: strconv.FormatInt(v.Settled, 10), Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}}, Kind: "project_rolling_quota_warning", Severity: severity, DetailCode: "tokens_" + v.WindowKind + "_" + v.Level, SubjectType: "project", SubjectID: v.ProjectID, SubjectName: v.ProjectName, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func projectRollingWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := projectRollingWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("project_rolling_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("project_rolling_quota_warning_inboxes.created_at < ? OR (project_rolling_quota_warning_inboxes.created_at = ? AND project_rolling_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []projectRollingWarningInboxRow
	if err := query.Select(projectRollingWarningInboxSelect).Order("project_rolling_quota_warning_inboxes.created_at DESC, project_rolling_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validProjectRollingWarningInbox(row, access) {
			records = append(records, projectRollingWarningRecord(row))
		}
	}
	var unread int64
	err := projectRollingWarningInboxQuery(tx, access).Where("project_rolling_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markProjectRollingWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row projectRollingWarningInboxRow
	err := projectRollingWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "project_rolling_quota_warning_inboxes", Name: "id"}, noticeID)).Select(projectRollingWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validProjectRollingWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := projectRollingWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return projectRollingWarningRecord(row), true, nil
}
