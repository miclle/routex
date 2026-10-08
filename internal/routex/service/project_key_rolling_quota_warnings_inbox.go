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

type ProjectKeyRollingQuotaWarningSnapshot struct {
	PersonalRollingQuotaWarningSnapshot
	ProjectID        string    `json:"project_id"`
	ProjectCreatedAt time.Time `json:"project_created_at"`
}
type projectKeyRollingWarningInboxRow struct {
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
	Observation             entity.ProjectKeyRollingQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const projectKeyRollingWarningInboxSelect = `project_key_rolling_quota_warning_inboxes.id, project_key_rolling_quota_warning_inboxes.recipient_id, project_key_rolling_quota_warning_inboxes.observation_id, project_key_rolling_quota_warning_inboxes.read_at, project_key_rolling_quota_warning_inboxes.created_at, project_key_rolling_quota_warning_inboxes.recipient_created_at, root.id AS current_root_key_id, root.created_at AS current_root_created_at, root.project_id AS current_root_project_id, root.replaces_key_id AS current_root_parent, project.id AS current_project_id, project.created_at AS current_project_created_at, project.status AS current_project_status,
 warning.id AS warning_id, warning.root_key_id AS warning_root_key_id, warning.root_key_name AS warning_root_key_name, warning.project_created_at AS warning_project_created_at, warning.project_id AS warning_project_id, warning.resource_created_at AS warning_resource_created_at,
 warning.window_kind AS warning_window_kind, warning.episode_id AS warning_episode_id, warning.policy_revision AS warning_policy_revision,
 warning.window_start AS warning_window_start, warning.window_end AS warning_window_end, warning.as_of AS warning_as_of, warning.coverage_start AS warning_coverage_start,
 warning.time_zone AS warning_time_zone, warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.level AS warning_level, warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation`

func projectKeyRollingWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	ids := []clause.Expression{}
	for _, id := range access.ProjectIDs {
		ids = append(ids, database.ExactText(tx, clause.Column{Table: "warning", Name: "project_id"}, id))
	}
	if access.ActorCreatedAt.IsZero() || len(ids) == 0 {
		return tx.Where("1 = 0")
	}
	return tx.Where(clause.Or(ids...)).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, projectKeyRollingWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "5h"), database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "7d"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func projectKeyRollingWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return projectKeyQuotaWarningRootJoin(tx.Table("project_key_rolling_quota_warning_inboxes").Joins("JOIN project_key_rolling_quota_warning_observations AS warning ON warning.id = project_key_rolling_quota_warning_inboxes.observation_id"), access).Where("project_key_rolling_quota_warning_inboxes.recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_key_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "project_key_rolling_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where(projectKeyRollingWarningScope(tx, access))
}
func projectKeyRollingWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := projectKeyQuotaWarningRootJoin(tx.Table("project_key_rolling_quota_warning_observations AS warning"), access).Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "project_key_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(projectKeyRollingWarningScope(tx, access))
	return tx.Model(&entity.ProjectKeyRollingQuotaWarningInbox{}).Where("recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validProjectKeyRollingWarningInbox(row projectKeyRollingWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	duration := personalRollingDuration(v.WindowKind)
	level, threshold := personalRollingWarningLevel(v.Settled, v.Limit)
	coveredFrom := v.WindowStart
	if v.ResourceCreatedAt.After(coveredFrom) {
		coveredFrom = v.ResourceCreatedAt
	}
	_, zoneErr := time.LoadLocation(v.TimeZone)
	return !access.ActorCreatedAt.IsZero() && row.RecipientCreatedAt.Equal(access.ActorCreatedAt) && !row.RecipientCreatedAt.After(v.AsOf) && !v.ProjectCreatedAt.IsZero() && row.CurrentProjectID == v.ProjectID && row.CurrentProjectStatus == entity.ResourceActive && row.CurrentProjectCreatedAt.Equal(v.ProjectCreatedAt) && slices.Contains(access.ProjectIDs, v.ProjectID) && !v.ResourceCreatedAt.Before(v.ProjectCreatedAt) && row.CurrentRootKeyID == v.RootKeyID && row.CurrentRootProjectID == v.ProjectID && row.CurrentRootParent == nil && row.CurrentRootCreatedAt.Equal(v.ResourceCreatedAt) && projectWarningKeyID(v.RootKeyID) && validCatalogLabel(v.RootKeyName) && row.RecipientID == access.ActorID && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && strings.HasPrefix(row.ID, "qri_") && safeTeamSessionID(v.ID) && strings.HasPrefix(v.ID, "qro_") && safeTeamSessionID(v.EpisodeID) && safeCallID.MatchString(v.PolicyRevision) && len(v.PolicyRevision) <= 64 && v.ThresholdGeneration == projectKeyRollingWarningGeneration && duration > 0 && v.WindowEnd.Equal(v.AsOf) && v.WindowStart.Equal(v.AsOf.Add(-duration)) && !v.ResourceCreatedAt.After(v.AsOf) && !v.CoverageStart.IsZero() && !coveredFrom.Before(v.CoverageStart) && zoneErr == nil && v.TimeZone != "Local" && len(v.TimeZone) <= 100 && level != "" && level == v.Level && threshold == v.Threshold
}
func projectKeyRollingWarningRecord(row projectKeyRollingWarningInboxRow) NotificationRecord {
	v := row.Observation
	severity := "medium"
	if v.Level == "critical" {
		severity = "high"
	}
	return NotificationRecord{ID: row.ID, RollingQuotaWarningObservationID: v.ID, ProjectKeyRollingQuotaWarning: &ProjectKeyRollingQuotaWarningSnapshot{ProjectID: v.ProjectID, ProjectCreatedAt: v.ProjectCreatedAt, PersonalRollingQuotaWarningSnapshot: PersonalRollingQuotaWarningSnapshot{ScopeKind: "project_key", ScopeID: v.RootKeyID, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, ResourceCreatedAt: v.ResourceCreatedAt, TimeZone: v.TimeZone, Limit: strconv.FormatInt(v.Limit, 10), Settled: strconv.FormatInt(v.Settled, 10), Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}}, Kind: "project_key_rolling_quota_warning", Severity: severity, DetailCode: "tokens_" + v.WindowKind + "_" + v.Level, SubjectType: "project_key", SubjectID: v.RootKeyID, SubjectName: v.RootKeyName, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func projectKeyRollingWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := projectKeyRollingWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("project_key_rolling_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("project_key_rolling_quota_warning_inboxes.created_at < ? OR (project_key_rolling_quota_warning_inboxes.created_at = ? AND project_key_rolling_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []projectKeyRollingWarningInboxRow
	if err := query.Select(projectKeyRollingWarningInboxSelect).Order("project_key_rolling_quota_warning_inboxes.created_at DESC, project_key_rolling_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validProjectKeyRollingWarningInbox(row, access) {
			records = append(records, projectKeyRollingWarningRecord(row))
		}
	}
	var unread int64
	err := projectKeyRollingWarningInboxQuery(tx, access).Where("project_key_rolling_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markProjectKeyRollingWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row projectKeyRollingWarningInboxRow
	err := projectKeyRollingWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "project_key_rolling_quota_warning_inboxes", Name: "id"}, noticeID)).Select(projectKeyRollingWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validProjectKeyRollingWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := projectKeyRollingWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return projectKeyRollingWarningRecord(row), true, nil
}
