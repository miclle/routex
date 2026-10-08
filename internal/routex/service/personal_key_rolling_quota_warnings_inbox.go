package service

import (
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PersonalKeyRollingQuotaWarningSnapshot struct {
	PersonalRollingQuotaWarningSnapshot
	OwnerID        string    `json:"owner_id"`
	OwnerCreatedAt time.Time `json:"owner_created_at"`
}
type personalKeyRollingWarningInboxRow struct {
	RecipientCreatedAt   time.Time
	CurrentRootKeyID     string
	CurrentRootCreatedAt time.Time
	CurrentRootUserID    string
	CurrentRootParent    *string
	ID                   string
	RecipientID          string
	ObservationID        string
	ReadAt               *time.Time
	CreatedAt            time.Time
	Observation          entity.PersonalKeyRollingQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const personalKeyRollingWarningInboxSelect = `personal_key_rolling_quota_warning_inboxes.id, personal_key_rolling_quota_warning_inboxes.recipient_id, personal_key_rolling_quota_warning_inboxes.observation_id, personal_key_rolling_quota_warning_inboxes.read_at, personal_key_rolling_quota_warning_inboxes.created_at, personal_key_rolling_quota_warning_inboxes.recipient_created_at, root.id AS current_root_key_id, root.created_at AS current_root_created_at, root.user_id AS current_root_user_id, root.replaces_key_id AS current_root_parent,
 warning.id AS warning_id, warning.root_key_id AS warning_root_key_id, warning.root_key_name AS warning_root_key_name, warning.owner_created_at AS warning_owner_created_at, warning.owner_id AS warning_owner_id, warning.resource_created_at AS warning_resource_created_at,
 warning.window_kind AS warning_window_kind, warning.episode_id AS warning_episode_id, warning.policy_revision AS warning_policy_revision,
 warning.window_start AS warning_window_start, warning.window_end AS warning_window_end, warning.as_of AS warning_as_of, warning.coverage_start AS warning_coverage_start,
 warning.time_zone AS warning_time_zone, warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.level AS warning_level, warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation`

func personalKeyRollingWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, personalKeyRollingWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "5h"), database.ExactText(tx, clause.Column{Table: "warning", Name: "window_kind"}, "7d"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func personalKeyRollingWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return personalKeyQuotaWarningRootJoin(tx.Table("personal_key_rolling_quota_warning_inboxes").Joins("JOIN personal_key_rolling_quota_warning_observations AS warning ON warning.id = personal_key_rolling_quota_warning_inboxes.observation_id"), access).Where("personal_key_rolling_quota_warning_inboxes.recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "personal_key_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "personal_key_rolling_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where(personalKeyRollingWarningScope(tx, access))
}
func personalKeyRollingWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := personalKeyQuotaWarningRootJoin(tx.Table("personal_key_rolling_quota_warning_observations AS warning"), access).Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "personal_key_rolling_quota_warning_inboxes", Name: "observation_id"})).Where(personalKeyRollingWarningScope(tx, access))
	return tx.Model(&entity.PersonalKeyRollingQuotaWarningInbox{}).Where("recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validPersonalKeyRollingWarningInbox(row personalKeyRollingWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	duration := personalRollingDuration(v.WindowKind)
	level, threshold := personalRollingWarningLevel(v.Settled, v.Limit)
	coveredFrom := v.WindowStart
	if v.ResourceCreatedAt.After(coveredFrom) {
		coveredFrom = v.ResourceCreatedAt
	}
	_, zoneErr := time.LoadLocation(v.TimeZone)
	return !access.ActorCreatedAt.IsZero() && row.RecipientCreatedAt.Equal(access.ActorCreatedAt) && v.OwnerCreatedAt.Equal(access.ActorCreatedAt) && !v.ResourceCreatedAt.Before(v.OwnerCreatedAt) && row.CurrentRootKeyID == v.RootKeyID && row.CurrentRootUserID == access.ActorID && row.CurrentRootParent == nil && row.CurrentRootCreatedAt.Equal(v.ResourceCreatedAt) && memberKeyID.MatchString(v.RootKeyID) && validCatalogLabel(v.RootKeyName) && row.RecipientID == access.ActorID && v.OwnerID == access.ActorID && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && strings.HasPrefix(row.ID, "kri_") && safeTeamSessionID(v.ID) && strings.HasPrefix(v.ID, "kro_") && safeTeamSessionID(v.EpisodeID) && safeCallID.MatchString(v.PolicyRevision) && len(v.PolicyRevision) <= 64 && v.ThresholdGeneration == personalKeyRollingWarningGeneration && duration > 0 && v.WindowEnd.Equal(v.AsOf) && v.WindowStart.Equal(v.AsOf.Add(-duration)) && !v.ResourceCreatedAt.After(v.AsOf) && !v.CoverageStart.IsZero() && !coveredFrom.Before(v.CoverageStart) && zoneErr == nil && v.TimeZone != "Local" && len(v.TimeZone) <= 100 && level != "" && level == v.Level && threshold == v.Threshold
}
func personalKeyRollingWarningRecord(row personalKeyRollingWarningInboxRow) NotificationRecord {
	v := row.Observation
	severity := "medium"
	if v.Level == "critical" {
		severity = "high"
	}
	return NotificationRecord{ID: row.ID, RollingQuotaWarningObservationID: v.ID, PersonalKeyRollingQuotaWarning: &PersonalKeyRollingQuotaWarningSnapshot{OwnerID: v.OwnerID, OwnerCreatedAt: v.OwnerCreatedAt, PersonalRollingQuotaWarningSnapshot: PersonalRollingQuotaWarningSnapshot{ScopeKind: "personal_key", ScopeID: v.RootKeyID, WindowKind: v.WindowKind, EpisodeID: v.EpisodeID, PolicyRevision: v.PolicyRevision, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, AsOf: v.AsOf, CoverageStart: v.CoverageStart, ResourceCreatedAt: v.ResourceCreatedAt, TimeZone: v.TimeZone, Limit: strconv.FormatInt(v.Limit, 10), Settled: strconv.FormatInt(v.Settled, 10), Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}}, Kind: "personal_key_rolling_quota_warning", Severity: severity, DetailCode: "tokens_" + v.WindowKind + "_" + v.Level, SubjectType: "personal_key", SubjectID: v.RootKeyID, SubjectName: v.RootKeyName, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func personalKeyRollingWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := personalKeyRollingWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("personal_key_rolling_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("personal_key_rolling_quota_warning_inboxes.created_at < ? OR (personal_key_rolling_quota_warning_inboxes.created_at = ? AND personal_key_rolling_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []personalKeyRollingWarningInboxRow
	if err := query.Select(personalKeyRollingWarningInboxSelect).Order("personal_key_rolling_quota_warning_inboxes.created_at DESC, personal_key_rolling_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validPersonalKeyRollingWarningInbox(row, access) {
			records = append(records, personalKeyRollingWarningRecord(row))
		}
	}
	var unread int64
	err := personalKeyRollingWarningInboxQuery(tx, access).Where("personal_key_rolling_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markPersonalKeyRollingWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row personalKeyRollingWarningInboxRow
	err := personalKeyRollingWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "personal_key_rolling_quota_warning_inboxes", Name: "id"}, noticeID)).Select(personalKeyRollingWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validPersonalKeyRollingWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := personalKeyRollingWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return personalKeyRollingWarningRecord(row), true, nil
}
