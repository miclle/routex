package service

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	notificationSourceModelDiscovery = "model_discovery"
	notificationKindModelDiscovery   = "models_first_recorded"
	modelDiscoveryCountLimit         = 2000
)

func validDiscoveryCount(kind string, count int) bool {
	if kind == notificationKindModelDiscovery {
		return count > 0 && count <= modelDiscoveryCountLimit
	}
	return count == 0
}

// The caller holds the exact Connection lock and commits the audit, new catalog
// rows and this source together. Manual entry and Azure authentication never call
// this helper. Counts describe first recording here, not upstream release time.
func saveModelDiscoveryNotice(tx *gorm.DB, auditID string, connection entity.ProviderConnection, provider entity.Provider, count int, at time.Time) error {
	name := provider.Name
	if !validNotificationSubject("provider", provider.ID, name) {
		name = provider.ID
	}
	row := entity.ModelDiscoveryNotice{AuditID: auditID, ConnectionID: connection.ID, ProviderID: provider.ID, ProviderName: name, ModelCount: count, RecordedAt: at.UTC()}
	if !validModelDiscoveryNotice(row) || connection.ProviderID != provider.ID {
		return fmt.Errorf("invalid model discovery source")
	}
	return tx.Create(&row).Error
}

func validModelDiscoveryNotice(row entity.ModelDiscoveryNotice) bool {
	return safeCallID.MatchString(row.AuditID) && len(row.AuditID) <= 30 && row.AuditID != "" &&
		safeCallID.MatchString(row.ConnectionID) && row.ConnectionID != "" && len(row.ConnectionID) <= 30 &&
		validNotificationSubject("provider", row.ProviderID, row.ProviderName) && len(row.ProviderID) <= 30 && utf8.RuneCountInString(row.ProviderName) <= 100 &&
		validDiscoveryCount(notificationKindModelDiscovery, row.ModelCount) && !row.RecordedAt.IsZero()
}

// Existing notification reconciliation owns the bounded retry loop. Immutable
// sources, rather than mutable current discovery coverage, recover missed inboxes.
func (s *Service) reconcileModelDiscoveryNotices(tx *gorm.DB) error {
	var sources []entity.ModelDiscoveryNotice
	err := tx.Model(&entity.ModelDiscoveryNotice{}).
		Where("NOT EXISTS (SELECT 1 FROM operational_alert_occurrences o WHERE o.source_type = ? AND o.source_id = model_discovery_notices.audit_id)", notificationSourceModelDiscovery).
		Order("recorded_at, audit_id").Limit(100).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&sources).Error
	if err != nil {
		return err
	}
	for _, source := range sources {
		if !validModelDiscoveryNotice(source) {
			return fmt.Errorf("invalid retained model discovery source")
		}
		// The source references an exact successful verification audit. Never borrow
		// a case-folded alias or a failure/manual audit as discovery provenance.
		var audit entity.AuditEvent
		err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, source.AuditID)).Take(&audit).Error
		if err != nil {
			return err
		}
		if audit.ID != source.AuditID || audit.Action != "credential.verify" || audit.ResourceType != "credential" {
			return fmt.Errorf("invalid model discovery provenance")
		}
		if err := s.recordOperationalAlertOccurrenceWithModelCount(tx, notificationSourceModelDiscovery, source.AuditID,
			"model_discovery:"+source.ConnectionID, notificationKindModelDiscovery, "low", "first_recorded",
			"provider", source.ProviderID, source.ProviderName, source.RecordedAt, source.ModelCount); err != nil {
			return err
		}
	}
	return nil
}
