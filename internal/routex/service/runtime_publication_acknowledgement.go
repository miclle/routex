package service

import (
	"context"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This is immutable evidence of one successful local installation seam. It says
// nothing about present serving state, other nodes or the original return value.
type RuntimePublicationAcknowledgementReceipt struct {
	Scope             string    `json:"scope"`
	InstallationID    string    `json:"installation_id"`
	InstanceID        string    `json:"instance_id"`
	InstanceStartedAt time.Time `json:"instance_started_at"`
	SnapshotID        string    `json:"snapshot_id"`
	RoutesPublishedAt time.Time `json:"routes_published_at"`
	AcknowledgedAt    time.Time `json:"acknowledged_at"`
}

func runtimePublicationSameClaim(a, b entity.RuntimePublicationCommand) bool {
	return runtimePublicationValid(a) && runtimePublicationValid(b) && a.RequestID == b.RequestID && a.ActorID == b.ActorID && a.ActorCreatedAt.Equal(b.ActorCreatedAt) && a.Reason == b.Reason && a.ClaimInstanceID == b.ClaimInstanceID && a.ClaimInstanceStartedAt.Equal(b.ClaimInstanceStartedAt) && a.ClaimedAt.Equal(b.ClaimedAt)
}

func runtimePublicationAcknowledgementValid(row entity.RuntimePublicationAcknowledgement, command entity.RuntimePublicationCommand, installation entity.RuntimeInstallationObservation) bool {
	return runtimePublicationValid(command) && row.RequestID == command.RequestID && row.InstallationID == installation.ID && validRuntimeInstallation(installation) && installation.ProjectionVersion == runtimeInstallationProjectionVersion && installation.InstanceID == command.ClaimInstanceID && installation.InstanceStartedAt.Equal(command.ClaimInstanceStartedAt) && runtimeInstallationMicrosecond(row.AcknowledgedAt) && !row.AcknowledgedAt.Before(command.ClaimedAt) && !row.AcknowledgedAt.Before(installation.FirstObservedAt)
}

// Caller holds publication admission and runtime.mu at the successful refresh
// seam. All checks/writes share the original installation evidence deadline.
func (s *Service) recordRuntimePublicationAcknowledgement(ctx context.Context, ticket *runtimePublicationTicket, routes *runtimeRoutes, auth *runtimeAuthorization, epoch uint64) error {
	if ticket == nil || ticket.lease == nil || !runtimePublicationValid(ticket.row) || ticket.row.Outcome != "claimed" || ticket.row.ClaimInstanceID != ticket.lease.id || !ticket.row.ClaimInstanceStartedAt.Equal(ticket.lease.startedAt) || !runtimeInstallationEvidenceDeadline(ctx, auth) || s.runtime == nil {
		return runtimePublicationUnavailable
	}
	r := s.runtime
	status := r.status.Load()
	source, known := runtimeInstallationSource(routes, auth)
	current := func() bool {
		value, exists := s.runtimePublicationCommands.Load(ticket.row.RequestID)
		return known && exists && value == ticket && ticket.ctx != nil && ticket.ctx.Err() == nil && s.runtime == r && s.runtimeInstallationCaptureCurrent(ctx, routes, auth, epoch, status) && s.runtimeInstallationLeaseCurrent(ticket.lease)
	}
	if !current() {
		return runtimePublicationUnavailable
	}
	var instance entity.SystemInstance
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, ticket.lease.id)).Take(&instance).Error; err != nil {
			return err
		}
		live := func() bool { return current() && runtimeApplicationInstanceMatches(instance, ticket.lease, time.Now()) }
		if !live() {
			return runtimePublicationUnavailable
		}
		command, err := runtimePublicationRead(tx, ticket.row.RequestID)
		if err != nil {
			return err
		}
		if command.Outcome != "claimed" || !runtimePublicationSameClaim(command, ticket.row) {
			return runtimePublicationUnavailable
		}
		var rows []entity.RuntimeInstallationObservation
		if err := tx.Where("instance_id = ? AND source_digest = ?", ticket.lease.id, source).Limit(2).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return runtimePublicationUnavailable
		}
		installation := rows[0]
		row := entity.RuntimePublicationAcknowledgement{RequestID: command.RequestID, InstallationID: installation.ID, AcknowledgedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if !runtimePublicationAcknowledgementValid(row, command, installation) || installation.SourceDigest != source || installation.SnapshotID != routes.ID || !installation.RoutesPublishedAt.Equal(routes.PublishedAt.UTC().Truncate(time.Microsecond)) || !live() {
			return runtimePublicationUnavailable
		}
		result := tx.Omit(clause.Associations).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 || !live() {
			return runtimePublicationUnavailable
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Unknown commit acknowledgement is never an in-memory positive proof. A
	// genuinely committed immutable row can still be recovered by an authorized GET.
	if !current() || !runtimeApplicationInstanceMatches(instance, ticket.lease, time.Now()) {
		return runtimePublicationUnavailable
	}
	return nil
}

func (s *Service) runtimePublicationReceiptWithAcknowledgement(tx *gorm.DB, command entity.RuntimePublicationCommand) (*RuntimePublicationCommandReceipt, error) {
	receipt := s.runtimePublicationReceipt(command)
	var row entity.RuntimePublicationAcknowledgement
	err := tx.Where(database.ExactText(tx, clause.Column{Name: "request_id"}, command.RequestID)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return receipt, nil
	}
	if err != nil {
		return nil, err
	}
	var installation entity.RuntimeInstallationObservation
	if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, row.InstallationID)).Take(&installation).Error; err != nil {
		return nil, err
	}
	if !runtimePublicationAcknowledgementValid(row, command, installation) {
		return nil, runtimePublicationUnavailable
	}
	receipt.Acknowledgement = &RuntimePublicationAcknowledgementReceipt{Scope: "single_process_gateway_admission", InstallationID: installation.ID, InstanceID: installation.InstanceID, InstanceStartedAt: installation.InstanceStartedAt.UTC(), SnapshotID: installation.SnapshotID, RoutesPublishedAt: installation.RoutesPublishedAt.UTC(), AcknowledgedAt: row.AcknowledgedAt.UTC()}
	return receipt, nil
}
