package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type QuotaWarningSnapshot struct {
	ScopeKind           string    `json:"scope_kind"`
	ScopeID             string    `json:"scope_id"`
	Dimension           string    `json:"dimension"`
	PolicyRevision      string    `json:"policy_revision"`
	MonthStart          time.Time `json:"month_start"`
	MonthEnd            time.Time `json:"month_end"`
	TimeZone            string    `json:"time_zone"`
	AsOf                time.Time `json:"as_of"`
	Limit               string    `json:"limit"`
	Settled             string    `json:"settled"`
	Currency            *string   `json:"currency"`
	Level               string    `json:"level"`
	Threshold           int       `json:"threshold"`
	ThresholdGeneration string    `json:"threshold_generation"`
}
type quotaWarningInboxRow struct {
	ID            string
	RecipientID   string
	ObservationID string
	ReadAt        *time.Time
	CreatedAt     time.Time
	Observation   entity.QuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const quotaWarningInboxSelect = `quota_warning_inboxes.id, quota_warning_inboxes.recipient_id, quota_warning_inboxes.observation_id, quota_warning_inboxes.read_at, quota_warning_inboxes.created_at,
 warning.id AS warning_id, warning.owner_id AS warning_owner_id, warning.dimension AS warning_dimension, warning.policy_revision AS warning_policy_revision,
 warning.month_start AS warning_month_start, warning.month_end AS warning_month_end, warning.time_zone AS warning_time_zone, warning.as_of AS warning_as_of,
 warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.currency AS warning_currency, warning.level AS warning_level,
 warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation, warning.coverage_start AS warning_coverage_start, warning.resource_created_at AS warning_resource_created_at`

func quotaWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Where("warning.resource_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "owner_id"}, access.ActorID)).Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, quotaWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "tokens"), database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "money"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func quotaWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Table("quota_warning_inboxes").Joins("JOIN quota_warning_observations AS warning ON warning.id = quota_warning_inboxes.observation_id").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where(quotaWarningScope(tx, access))
}
func quotaWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := tx.Table("quota_warning_observations AS warning").Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "quota_warning_inboxes", Name: "observation_id"})).Where(quotaWarningScope(tx, access))
	return tx.Model(&entity.QuotaWarningInbox{}).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validQuotaWarningInbox(row quotaWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	level, threshold := quotaWarningLevel(v.Settled, v.Limit)
	return !access.ActorCreatedAt.IsZero() && v.ResourceCreatedAt.Equal(access.ActorCreatedAt) && row.RecipientID == access.ActorID && v.OwnerID == access.ActorID && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && safeTeamSessionID(v.ID) && v.ThresholdGeneration == quotaWarningGeneration && (v.Dimension == "tokens" && v.Currency == "" || v.Dimension == "money" && len(v.Currency) == 3) && level == v.Level && threshold == v.Threshold && level != ""
}
func quotaWarningRecord(row quotaWarningInboxRow) NotificationRecord {
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
	return NotificationRecord{ID: row.ID, QuotaWarningObservationID: v.ID, QuotaWarning: &QuotaWarningSnapshot{ScopeKind: "user", ScopeID: v.OwnerID, Dimension: v.Dimension, PolicyRevision: v.PolicyRevision, MonthStart: v.MonthStart, MonthEnd: v.MonthEnd, TimeZone: v.TimeZone, AsOf: v.AsOf, Limit: v.Limit, Settled: v.Settled, Currency: currency, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}, Kind: "monthly_quota_warning", Severity: severity, DetailCode: v.Dimension + "_month_" + v.Level, SubjectType: "user", SubjectID: v.OwnerID, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func quotaWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := quotaWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("quota_warning_inboxes.created_at < ? OR (quota_warning_inboxes.created_at = ? AND quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []quotaWarningInboxRow
	if err := query.Select(quotaWarningInboxSelect).Order("quota_warning_inboxes.created_at DESC, quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validQuotaWarningInbox(row, access) {
			records = append(records, quotaWarningRecord(row))
		}
	}
	var unread int64
	err := quotaWarningInboxQuery(tx, access).Where("quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markQuotaWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row quotaWarningInboxRow
	err := quotaWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "quota_warning_inboxes", Name: "id"}, noticeID)).Select(quotaWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validQuotaWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := quotaWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return quotaWarningRecord(row), true, nil
}
