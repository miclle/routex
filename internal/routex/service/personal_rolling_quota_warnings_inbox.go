package service

import (
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PersonalRollingQuotaWarningSnapshot struct {
	ScopeKind           string    `json:"scope_kind"`
	ScopeID             string    `json:"scope_id"`
	WindowKind          string    `json:"window_kind"`
	EpisodeID           string    `json:"episode_id"`
	PolicyRevision      string    `json:"policy_revision"`
	WindowStart         time.Time `json:"window_start"`
	WindowEnd           time.Time `json:"window_end"`
	AsOf                time.Time `json:"as_of"`
	CoverageStart       time.Time `json:"coverage_start"`
	ResourceCreatedAt   time.Time `json:"resource_created_at"`
	TimeZone            string    `json:"time_zone"`
	Limit               string    `json:"limit"`
	Settled             string    `json:"settled"`
	Level               string    `json:"level"`
	Threshold           int       `json:"threshold"`
	ThresholdGeneration string    `json:"threshold_generation"`
}
type personalRollingWarningInboxRow struct {
	ID            string
	RecipientID   string
	ObservationID string
	ReadAt        *time.Time
	CreatedAt     time.Time
	Observation   entity.PersonalRollingQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const personalRollingWarningInboxSelect = `personal_rolling_quota_warning_inboxes.id, personal_rolling_quota_warning_inboxes.recipient_id, personal_rolling_quota_warning_inboxes.observation_id, personal_rolling_quota_warning_inboxes.read_at, personal_rolling_quota_warning_inboxes.created_at,
 warning.id AS warning_id, warning.owner_id AS warning_owner_id, warning.resource_created_at AS warning_resource_created_at,
 warning.window_kind AS warning_window_kind, warning.episode_id AS warning_episode_id, warning.policy_revision AS warning_policy_revision,
 warning.window_start AS warning_window_start, warning.window_end AS warning_window_end, warning.as_of AS warning_as_of, warning.coverage_start AS warning_coverage_start,
 warning.time_zone AS warning_time_zone, warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.level AS warning_level, warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation`

func personalRollingWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Where("warning.resource_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "owner_id"}, access.ActorID)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "5h"), database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "7d"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func personalRollingWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Table("personal_rolling_quota_warning_inboxes").Joins("JOIN personal_rolling_quota_warning_observations AS warning ON warning.id = personal_rolling_quota_warning_inboxes.observation_id").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "personal_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "personal_rolling_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where(personalRollingWarningScope(tx, access))
}
func personalRollingWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := tx.Table("personal_rolling_quota_warning_observations AS warning").Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "personal_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(personalRollingWarningScope(tx, access))
	return tx.Model(&entity.PersonalRollingQuotaWarningInbox{}).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validPersonalRollingWarningInbox(row personalRollingWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	duration := personalRollingDuration(v.WindowKind)
	level, threshold := personalRollingWarningLevel(v.Settled, v.Limit)
	coveredFrom := v.WindowStart
	if v.ResourceCreatedAt.After(coveredFrom) {
		coveredFrom = v.ResourceCreatedAt
	}
	_, zoneErr := time.LoadLocation(v.TimeZone)
	return !access.ActorCreatedAt.IsZero() && v.ResourceCreatedAt.Equal(access.ActorCreatedAt) && row.RecipientID == access.ActorID && v.OwnerID == access.ActorID && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && safeTeamSessionID(v.ID) && safeTeamSessionID(v.EpisodeID) && safeCallID.MatchString(v.PolicyRevision) && len(v.PolicyRevision) <= 64 && v.ThresholdGeneration == personalRollingWarningGeneration && duration > 0 && v.WindowEnd.Equal(v.AsOf) && v.WindowStart.Equal(v.AsOf.Add(-duration)) && !v.ResourceCreatedAt.After(v.AsOf) && !v.CoverageStart.IsZero() && !coveredFrom.Before(v.CoverageStart) && zoneErr == nil && v.TimeZone != "Local" && len(v.TimeZone) <= 100 && level != "" && level == v.Level && threshold == v.Threshold
}
func personalRollingWarningRecord(row personalRollingWarningInboxRow) NotificationRecord {
	v := row.Observation
	severity := "medium"
	if v.Level == "critical" {
		severity = "high"
	}
	return NotificationRecord{ID: row.ID, RollingQuotaWarningObservationID: v.ID, RollingQuotaWarning: &PersonalRollingQuotaWarningSnapshot{ScopeKind: "user", ScopeID: v.OwnerID, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, ResourceCreatedAt: v.ResourceCreatedAt, TimeZone: v.TimeZone, Limit: strconv.FormatInt(v.Limit, 10), Settled: strconv.FormatInt(v.Settled, 10), Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}, Kind: "personal_rolling_quota_warning", Severity: severity, DetailCode: "tokens_" + v.WindowKind + "_" + v.Level, SubjectType: "user", SubjectID: v.OwnerID, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func personalRollingWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := personalRollingWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("personal_rolling_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("personal_rolling_quota_warning_inboxes.created_at < ? OR (personal_rolling_quota_warning_inboxes.created_at = ? AND personal_rolling_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []personalRollingWarningInboxRow
	if err := query.Select(personalRollingWarningInboxSelect).Order("personal_rolling_quota_warning_inboxes.created_at DESC, personal_rolling_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validPersonalRollingWarningInbox(row, access) {
			records = append(records, personalRollingWarningRecord(row))
		}
	}
	var unread int64
	err := personalRollingWarningInboxQuery(tx, access).Where("personal_rolling_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markPersonalRollingWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row personalRollingWarningInboxRow
	err := personalRollingWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "personal_rolling_quota_warning_inboxes", Name: "id"}, noticeID)).Select(personalRollingWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validPersonalRollingWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := personalRollingWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return personalRollingWarningRecord(row), true, nil
}
