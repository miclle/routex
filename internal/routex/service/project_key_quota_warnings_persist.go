package service

import (
	"errors"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func sameProjectKeyWarningIdentity(a, b entity.ProjectKeyQuotaWarningObservation) bool {
	return a.RootKeyID == b.RootKeyID && a.ProjectID == b.ProjectID && a.ProjectCreatedAt.Equal(b.ProjectCreatedAt) && a.ResourceCreatedAt.Equal(b.ResourceCreatedAt) && a.Dimension == b.Dimension && a.MonthStart.Equal(b.MonthStart) && a.PolicyRevision == b.PolicyRevision && a.Currency == b.Currency && a.Level == b.Level && a.Threshold == b.Threshold && a.ThresholdGeneration == b.ThresholdGeneration
}
func projectKeyWarningIdentityQuery(tx *gorm.DB, v entity.ProjectKeyQuotaWarningObservation) *gorm.DB {
	query := tx.Model(&entity.ProjectKeyQuotaWarningObservation{}).Where("month_start = ? AND project_created_at = ? AND resource_created_at = ?", v.MonthStart, v.ProjectCreatedAt, v.ResourceCreatedAt)
	for _, field := range []struct{ name, value string }{{"root_key_id", v.RootKeyID}, {"project_id", v.ProjectID}, {"dimension", v.Dimension}, {"policy_revision", v.PolicyRevision}, {"currency", v.Currency}, {"level", v.Level}, {"threshold_generation", v.ThresholdGeneration}} {
		query = query.Where(database.ExactText(tx, clause.Column{Name: field.name}, field.value))
	}
	return query
}
func persistProjectKeyQuotaWarning(tx *gorm.DB, v entity.ProjectKeyQuotaWarningObservation, recipients []projectQuotaWarningRecipient) error {
	var old entity.ProjectKeyQuotaWarningObservation
	err := projectKeyWarningIdentityQuery(tx, v).Take(&old).Error
	if err == nil {
		if !sameProjectKeyWarningIdentity(old, v) {
			return errQuotaNotificationIdentity
		}
		return nil
	} // Preserve the original snapshot, recipients and read state.
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}
	if len(recipients) > quotaInboxManagerLimit {
		return errQuotaNotificationIdentity
	}
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if !safeTeamSessionID(recipient.ID) || recipient.CreatedAt.IsZero() || recipient.CreatedAt.After(v.AsOf) || seen[recipient.ID] {
			return errQuotaNotificationIdentity
		}
		seen[recipient.ID] = true
	}
	v.ID, err = id.NewPrefixed("jwo")
	if err != nil {
		return err
	}
	if err = tx.Create(&v).Error; err != nil {
		return err
	}
	inboxes := make([]entity.ProjectKeyQuotaWarningInbox, 0, len(recipients))
	for _, recipient := range recipients {
		inboxID, err := id.NewPrefixed("jwi")
		if err != nil {
			return err
		}
		inboxes = append(inboxes, entity.ProjectKeyQuotaWarningInbox{ID: inboxID, ObservationID: v.ID, RecipientID: recipient.ID, RecipientCreatedAt: recipient.CreatedAt.UTC(), CreatedAt: v.AsOf})
	}
	return tx.CreateInBatches(inboxes, 250).Error
}
