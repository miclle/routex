package service

import (
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type personalKeyQuotaWarningInboxRow struct {
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
	Observation          entity.PersonalKeyQuotaWarningObservation `gorm:"embedded;embeddedPrefix:warning_"`
}

const personalKeyQuotaWarningInboxSelect = `personal_key_quota_warning_inboxes.id, personal_key_quota_warning_inboxes.recipient_id, personal_key_quota_warning_inboxes.observation_id, personal_key_quota_warning_inboxes.read_at, personal_key_quota_warning_inboxes.created_at, personal_key_quota_warning_inboxes.recipient_created_at, root.id AS current_root_key_id, root.created_at AS current_root_created_at, root.user_id AS current_root_user_id, root.replaces_key_id AS current_root_parent,
 warning.id AS warning_id, warning.root_key_id AS warning_root_key_id, warning.owner_id AS warning_owner_id, warning.owner_created_at AS warning_owner_created_at, warning.root_key_name AS warning_root_key_name, warning.dimension AS warning_dimension, warning.policy_revision AS warning_policy_revision,
 warning.month_start AS warning_month_start, warning.month_end AS warning_month_end, warning.time_zone AS warning_time_zone, warning.as_of AS warning_as_of,
 warning.limit_value AS warning_limit_value, warning.settled_value AS warning_settled_value, warning.currency AS warning_currency, warning.level AS warning_level,
 warning.threshold AS warning_threshold, warning.threshold_generation AS warning_threshold_generation, warning.coverage_start AS warning_coverage_start, warning.resource_created_at AS warning_resource_created_at`

func personalKeyQuotaWarningRootJoin(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return tx.Joins("JOIN api_keys AS root ON root.id = warning.root_key_id").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "root", Name: "id"}, clause.Column{Table: "warning", Name: "root_key_id"})).
		Where("root.created_at = warning.resource_created_at AND root.replaces_key_id IS NULL").
		Where("root.user_id = ?", access.ActorID).Where(database.ExactText(tx, clause.Column{Table: "root", Name: "user_id"}, access.ActorID)).
		Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "owner_id"}, access.ActorID)).
		Where("warning.owner_created_at = ?", access.ActorCreatedAt)
}
func personalKeyQuotaWarningScope(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	if access.ActorCreatedAt.IsZero() {
		return tx.Where("1 = 0")
	}
	return tx.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "threshold_generation"}, personalKeyQuotaWarningGeneration)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "tokens"), database.ExactText(tx, clause.Column{Table: "warning", Name: "dimension"}, "money"))).
		Where(clause.Or(clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "near"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 80}), clause.And(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, "critical"), clause.Eq{Column: clause.Column{Table: "warning", Name: "threshold"}, Value: 90})))
}
func personalKeyQuotaWarningInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	return personalKeyQuotaWarningRootJoin(tx.Table("personal_key_quota_warning_inboxes").Joins("JOIN personal_key_quota_warning_observations AS warning ON warning.id = personal_key_quota_warning_inboxes.observation_id"), access).Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "personal_key_quota_warning_inboxes", Name: "observation_id"})).Where(database.ExactText(tx, clause.Column{Table: "personal_key_quota_warning_inboxes", Name: "recipient_id"}, access.ActorID)).Where("personal_key_quota_warning_inboxes.recipient_created_at = ?", access.ActorCreatedAt).Where(personalKeyQuotaWarningScope(tx, access))
}
func personalKeyQuotaWarningMutationQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	observations := personalKeyQuotaWarningRootJoin(tx.Table("personal_key_quota_warning_observations AS warning"), access).Select("1").Where(database.ExactTextColumns(tx, clause.Column{Table: "warning", Name: "id"}, clause.Column{Table: "personal_key_quota_warning_inboxes", Name: "observation_id"})).Where(personalKeyQuotaWarningScope(tx, access))
	return tx.Model(&entity.PersonalKeyQuotaWarningInbox{}).Where("recipient_created_at = ?", access.ActorCreatedAt).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where("EXISTS (?)", observations)
}
func validPersonalKeyQuotaWarningInbox(row personalKeyQuotaWarningInboxRow, access quotaInboxAccess) bool {
	v := row.Observation
	level, threshold := quotaWarningLevel(v.Settled, v.Limit)
	return !access.ActorCreatedAt.IsZero() && row.RecipientCreatedAt.Equal(access.ActorCreatedAt) && !v.ResourceCreatedAt.IsZero() && row.CurrentRootKeyID == v.RootKeyID && row.CurrentRootUserID == access.ActorID && row.CurrentRootParent == nil && row.CurrentRootCreatedAt.Equal(v.ResourceCreatedAt) && v.OwnerID == access.ActorID && v.OwnerCreatedAt.Equal(access.ActorCreatedAt) && memberKeyID.MatchString(v.RootKeyID) && validCatalogLabel(v.RootKeyName) && row.RecipientID == access.ActorID && !v.MonthStart.IsZero() && v.MonthEnd.After(v.MonthStart) && !v.AsOf.Before(v.MonthStart) && v.AsOf.Before(v.MonthEnd) && !v.CoverageStart.IsZero() && !v.CoverageStart.After(v.AsOf) && !v.ResourceCreatedAt.After(v.AsOf) && !v.OwnerCreatedAt.After(v.ResourceCreatedAt) && v.PolicyRevision != "" && len(v.PolicyRevision) <= 64 && safeCallID.MatchString(v.PolicyRevision) && v.TimeZone != "" && v.TimeZone != "Local" && len(v.TimeZone) <= 100 && row.ObservationID == v.ID && safeTeamSessionID(row.ID) && strings.HasPrefix(row.ID, "kwi_") && safeTeamSessionID(v.ID) && strings.HasPrefix(v.ID, "kwo_") && v.ThresholdGeneration == personalKeyQuotaWarningGeneration && (v.Dimension == "tokens" && v.Currency == "" || v.Dimension == "money" && pricing.Currency(v.Currency)) && level == v.Level && threshold == v.Threshold && level != ""
}
func personalKeyQuotaWarningRecord(row personalKeyQuotaWarningInboxRow) NotificationRecord {
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
	return NotificationRecord{ID: row.ID, QuotaWarningObservationID: v.ID, QuotaWarning: &QuotaWarningSnapshot{ScopeKind: "personal_key", ScopeID: v.RootKeyID, Dimension: v.Dimension, PolicyRevision: v.PolicyRevision, MonthStart: v.MonthStart, MonthEnd: v.MonthEnd, TimeZone: v.TimeZone, AsOf: v.AsOf, Limit: v.Limit, Settled: v.Settled, Currency: currency, Level: v.Level, Threshold: v.Threshold, ThresholdGeneration: v.ThresholdGeneration}, Kind: "monthly_quota_warning", Severity: severity, DetailCode: v.Dimension + "_month_" + v.Level, SubjectType: "personal_key", SubjectID: v.RootKeyID, SubjectName: v.RootKeyName, OccurrenceCount: 1, Read: row.ReadAt != nil, ReadAt: row.ReadAt, FirstSeenAt: v.AsOf, LastSeenAt: row.CreatedAt}
}
func personalKeyQuotaWarningPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := personalKeyQuotaWarningInboxQuery(tx, access)
	if filter.UnreadOnly {
		query = query.Where("personal_key_quota_warning_inboxes.read_at IS NULL")
	}
	if filter.Severity != "" {
		level := "near"
		if filter.Severity == "high" {
			level = "critical"
		}
		query = query.Where(database.ExactText(tx, clause.Column{Table: "warning", Name: "level"}, level))
	}
	if filter.Cursor != "" {
		query = query.Where("personal_key_quota_warning_inboxes.created_at < ? OR (personal_key_quota_warning_inboxes.created_at = ? AND personal_key_quota_warning_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []personalKeyQuotaWarningInboxRow
	if err := query.Select(personalKeyQuotaWarningInboxSelect).Order("personal_key_quota_warning_inboxes.created_at DESC, personal_key_quota_warning_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	records := []NotificationRecord{}
	for _, row := range rows {
		if validPersonalKeyQuotaWarningInbox(row, access) {
			records = append(records, personalKeyQuotaWarningRecord(row))
		}
	}
	var unread int64
	err := personalKeyQuotaWarningInboxQuery(tx, access).Where("personal_key_quota_warning_inboxes.read_at IS NULL").Count(&unread).Error
	return records, unread, err
}
func markPersonalKeyQuotaWarningRead(tx *gorm.DB, access quotaInboxAccess, noticeID string) (NotificationRecord, bool, error) {
	var row personalKeyQuotaWarningInboxRow
	err := personalKeyQuotaWarningInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "personal_key_quota_warning_inboxes", Name: "id"}, noticeID)).Select(personalKeyQuotaWarningInboxSelect).Take(&row).Error
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !validPersonalKeyQuotaWarningInbox(row, access) {
		return NotificationRecord{}, false, nil
	}
	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err := personalKeyQuotaWarningMutationQuery(tx, access).Where(database.ExactText(tx, clause.Column{Name: "id"}, noticeID)).Update("read_at", now).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.ReadAt = &now
	}
	return personalKeyQuotaWarningRecord(row), true, nil
}
