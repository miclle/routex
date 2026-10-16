package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var runtimePublicationRequestID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var runtimePublicationUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "runtime publication command temporarily unavailable"}
var runtimePublicationConflict = &apperrors.Error{Code: http.StatusConflict, Message: "runtime publication request conflicts with retained intent"}

type RuntimePublicationCommandInput struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}

// The original function outcome and optional command-bound local installation
// acknowledgement are independent. Neither attributes a later runtime state.
type RuntimePublicationCommandReceipt struct {
	RequestID              string                                    `json:"request_id"`
	ActorID                string                                    `json:"actor_id"`
	Reason                 string                                    `json:"reason"`
	ClaimInstanceID        string                                    `json:"claim_instance_id"`
	ClaimInstanceStartedAt time.Time                                 `json:"claim_instance_started_at"`
	ClaimedAt              time.Time                                 `json:"claimed_at"`
	FinalizedAt            *time.Time                                `json:"finalized_at"`
	Status                 string                                    `json:"status"`
	FailureCode            *string                                   `json:"failure_code"`
	Acknowledgement        *RuntimePublicationAcknowledgementReceipt `json:"acknowledgement"`
}

type runtimePublicationTicket struct {
	row   entity.RuntimePublicationCommand
	lease *systemInstanceLease
	ctx   context.Context
}

func runtimePublicationReason(reason string) bool {
	return rootReason(reason)
}

func runtimePublicationValid(row entity.RuntimePublicationCommand) bool {
	if !runtimePublicationRequestID.MatchString(row.RequestID) || !safeTeamSessionID(row.ActorID) || !strings.HasPrefix(row.ActorID, "usr_") || !runtimePublicationReason(row.Reason) || !systemInstanceIDPattern.MatchString(row.ClaimInstanceID) || !runtimeInstallationMicrosecond(row.ActorCreatedAt) || !runtimeInstallationMicrosecond(row.ClaimInstanceStartedAt) || !runtimeInstallationMicrosecond(row.ClaimedAt) || row.ClaimedAt.Before(row.ActorCreatedAt) || row.ClaimedAt.Before(row.ClaimInstanceStartedAt) {
		return false
	}
	switch row.Outcome {
	case "claimed":
		return row.FinalizedAt == nil && row.FailureCode == nil
	case "refresh_returned_nil", "refresh_returned_error":
		if row.FinalizedAt == nil || !runtimeInstallationMicrosecond(*row.FinalizedAt) || row.FinalizedAt.Before(row.ClaimedAt) {
			return false
		}
		return row.Outcome == "refresh_returned_nil" && row.FailureCode == nil || row.Outcome == "refresh_returned_error" && row.FailureCode != nil && *row.FailureCode == "runtime_refresh_failed"
	default:
		return false
	}
}

func runtimePublicationError(err error) error {
	for _, expected := range []error{apperrors.ErrBadRequest, apperrors.ErrUnauthorized, apperrors.ErrForbidden, apperrors.ErrNotFound, runtimePublicationConflict} {
		if errors.Is(err, expected) {
			return expected
		}
	}
	return runtimePublicationUnavailable
}

// Call with the governance lock held. Both the persisted actor birth and the
// current Session/primary-method proof are checked, including after claim commit.
func (s *Service) runtimePublicationActor(tx *gorm.DB, auth *Authentication) (entity.User, error) {
	if auth == nil {
		return entity.User{}, apperrors.ErrUnauthorized
	}
	actor, err := registrationAdmittedUser(tx, auth.User.ID, true)
	if err != nil {
		return actor, err
	}
	if actor.Role != entity.RoleAdmin {
		return actor, apperrors.ErrForbidden
	}
	if actor.CreatedAt.IsZero() || !actor.CreatedAt.Equal(auth.User.CreatedAt) {
		return actor, apperrors.ErrUnauthorized
	}
	var session entity.Session
	if auth.Session.ID == "" || auth.Session.CreatedAt.IsZero() {
		return actor, apperrors.ErrUnauthorized
	}
	err = tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, auth.Session.ID)).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return actor, apperrors.ErrUnauthorized
	}
	if err != nil {
		return actor, err
	}
	if session.ID != auth.Session.ID || session.UserID != actor.ID || !session.CreatedAt.Equal(auth.Session.CreatedAt) || session.TokenHash != secret.SHA256Hex(auth.Token) || !session.ExpiresAt.After(time.Now()) {
		return actor, apperrors.ErrUnauthorized
	}
	// Use the Service entry point so later policy enforcement composes here.
	if err := s.primaryValidateSession(tx, session); err != nil {
		return actor, err
	}
	if !session.ExpiresAt.After(time.Now()) {
		return actor, apperrors.ErrUnauthorized
	}
	return actor, tx.Statement.Context.Err()
}

func (s *Service) runtimePublicationLease() *systemInstanceLease {
	if !s.instanceMu.TryRLock() {
		return nil
	}
	lease := s.instance
	s.instanceMu.RUnlock()
	return lease
}

func (s *Service) runtimePublicationProcess(tx *gorm.DB, lease *systemInstanceLease) error {
	if lease == nil || s.runtime == nil || !s.runtimeInstallationLeaseCurrent(lease) {
		return runtimePublicationUnavailable
	}
	select {
	case <-s.runtime.done:
		return runtimePublicationUnavailable
	default:
	}
	var row entity.SystemInstance
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, lease.id)).Take(&row).Error; err != nil {
		return err
	}
	if !runtimeApplicationInstanceMatches(row, lease, time.Now()) || !s.runtimeInstallationLeaseCurrent(lease) || tx.Statement.Context.Err() != nil {
		return runtimePublicationUnavailable
	}
	select {
	case <-s.runtime.done:
		return runtimePublicationUnavailable
	default:
	}
	return nil
}

func runtimePublicationRead(tx *gorm.DB, requestID string) (entity.RuntimePublicationCommand, error) {
	var row entity.RuntimePublicationCommand
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "request_id"}, requestID)).Take(&row).Error
	if err == nil && (row.RequestID != requestID || !runtimePublicationValid(row)) {
		return row, runtimePublicationUnavailable
	}
	return row, err
}

func runtimePublicationIntent(row entity.RuntimePublicationCommand, actor entity.User, reason *string) error {
	if row.ActorID != actor.ID || !row.ActorCreatedAt.Equal(actor.CreatedAt) || reason != nil && row.Reason != *reason {
		return runtimePublicationConflict
	}
	return nil
}

func runtimePublicationAudit(tx *gorm.DB, row entity.RuntimePublicationCommand, action string) error {
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	detail, err := json.Marshal(struct {
		RequestID string `json:"request_id"`
		Reason    string `json:"reason"`
		Outcome   string `json:"outcome"`
	}{row.RequestID, row.Reason, row.Outcome})
	if err != nil {
		return err
	}
	text := string(detail)
	// Audit resource IDs are size30; the UUID remains in the closed detail.
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: row.ActorID, Action: action, ResourceType: "system_instance", ResourceID: row.ClaimInstanceID, DetailsJSON: &text}).Error
}

func (s *Service) runtimePublicationRunning() bool {
	if s.runtime == nil {
		return false
	}
	select {
	case <-s.runtime.done:
		return false
	default:
		return true
	}
}

func (s *Service) runtimePublicationReceipt(row entity.RuntimePublicationCommand) *RuntimePublicationCommandReceipt {
	status := row.Outcome
	if status == "claimed" {
		status = "unknown"
		if value, ok := s.runtimePublicationCommands.Load(row.RequestID); ok {
			ticket, valid := value.(*runtimePublicationTicket)
			if valid && ticket.ctx.Err() == nil && ticket.row.RequestID == row.RequestID && ticket.row.ActorID == row.ActorID && ticket.row.ActorCreatedAt.Equal(row.ActorCreatedAt) && ticket.row.Reason == row.Reason && ticket.row.ClaimedAt.Equal(row.ClaimedAt) && ticket.row.ClaimInstanceID == row.ClaimInstanceID && ticket.row.ClaimInstanceStartedAt.Equal(row.ClaimInstanceStartedAt) && s.runtimeInstallationLeaseCurrent(ticket.lease) && s.runtimePublicationRunning() {
				status = "pending"
			}
		}
	}
	receipt := &RuntimePublicationCommandReceipt{RequestID: row.RequestID, ActorID: row.ActorID, Reason: row.Reason, ClaimInstanceID: row.ClaimInstanceID, ClaimInstanceStartedAt: row.ClaimInstanceStartedAt.UTC(), ClaimedAt: row.ClaimedAt.UTC(), Status: status}
	if row.FinalizedAt != nil {
		value := row.FinalizedAt.UTC()
		receipt.FinalizedAt = &value
	}
	if row.FailureCode != nil {
		value := *row.FailureCode
		receipt.FailureCode = &value
	}
	return receipt
}

// PublishRuntimeCommand makes at most one dispatch after a positively committed
// claim. Claimed history is never resumed, even by a new Service in this process.
func (s *Service) PublishRuntimeCommand(ctx context.Context, auth *Authentication, input RuntimePublicationCommandInput) (*RuntimePublicationCommandReceipt, error) {
	if !runtimePublicationRequestID.MatchString(input.RequestID) || !runtimePublicationReason(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeRefreshTimeout)
	defer cancel()
	var row entity.RuntimePublicationCommand
	var lease *systemInstanceLease
	created := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := s.runtimePublicationActor(tx, auth)
		if err != nil {
			return err
		}
		row, err = runtimePublicationRead(tx, input.RequestID)
		if err == nil {
			return runtimePublicationIntent(row, actor, &input.Reason)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		lease = s.runtimePublicationLease()
		if err := s.runtimePublicationProcess(tx, lease); err != nil {
			return err
		}
		row = entity.RuntimePublicationCommand{RequestID: input.RequestID, ActorID: actor.ID, ActorCreatedAt: actor.CreatedAt, Reason: input.Reason, ClaimInstanceID: lease.id, ClaimInstanceStartedAt: lease.startedAt, ClaimedAt: time.Now().UTC().Truncate(time.Microsecond), Outcome: "claimed"}
		if !runtimePublicationValid(row) {
			return runtimePublicationUnavailable
		}
		createdRow := tx.Create(&row)
		if createdRow.Error != nil {
			return createdRow.Error
		}
		if createdRow.RowsAffected != 1 {
			return runtimePublicationUnavailable
		}
		if err := runtimePublicationAudit(tx, row, "runtime.publication.claim"); err != nil {
			return err
		}
		created = true
		return ctx.Err()
	})
	// Even if the database actually committed, an uncertain acknowledgement does
	// not grant dispatch. A later read can only return retained unknown history.
	if err != nil || ctx.Err() != nil {
		if err == nil {
			err = ctx.Err()
		}
		return nil, runtimePublicationError(err)
	}
	if !created {
		return s.getRuntimePublicationCommand(ctx, auth, row.RequestID)
	}
	return s.dispatchRuntimePublication(ctx, auth, row, lease, nil, false)
}

// Dispatch is shared only after an acknowledged atomic claim. A retained claim
// is never resumed; the optional authority check belongs to the new consumer.
func (s *Service) dispatchRuntimePublication(ctx context.Context, auth *Authentication, row entity.RuntimePublicationCommand, lease *systemInstanceLease, authority func(*gorm.DB) error, tryAdmission bool) (*RuntimePublicationCommandReceipt, error) {
	ticket := &runtimePublicationTicket{row: row, lease: lease, ctx: ctx}
	if _, loaded := s.runtimePublicationCommands.LoadOrStore(row.RequestID, ticket); loaded {
		return nil, runtimePublicationUnavailable
	}
	defer s.runtimePublicationCommands.CompareAndDelete(row.RequestID, ticket)
	// A claim is not authority. Serialize a fresh actor/Session/process check after
	// claim commit; losing authority leaves an immutable claimed/unknown receipt.
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if _, err := s.runtimePublicationActor(tx, auth); err != nil {
			return err
		}
		if err := s.runtimePublicationProcess(tx, lease); err != nil {
			return err
		}
		if authority != nil {
			if err := authority(tx); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	if err != nil || ctx.Err() != nil {
		if err == nil {
			err = ctx.Err()
		}
		return nil, runtimePublicationError(err)
	}
	refreshErr := s.refreshRuntimeWithPublication(ctx, tryAdmission, ticket)
	finalized := time.Now().UTC().Truncate(time.Microsecond)
	outcome := "refresh_returned_nil"
	var code *string
	if refreshErr != nil {
		outcome = "refresh_returned_error"
		value := "runtime_refresh_failed"
		code = &value
	}
	terminal := row
	terminal.Outcome, terminal.FinalizedAt, terminal.FailureCode = outcome, &finalized, code
	if !runtimePublicationValid(terminal) {
		return nil, runtimePublicationUnavailable
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		stored, err := runtimePublicationRead(tx, row.RequestID)
		if err != nil {
			return err
		}
		if stored.Outcome != "claimed" || stored.ActorID != row.ActorID || !stored.ActorCreatedAt.Equal(row.ActorCreatedAt) || stored.Reason != row.Reason || stored.ClaimInstanceID != row.ClaimInstanceID || !stored.ClaimInstanceStartedAt.Equal(row.ClaimInstanceStartedAt) || !stored.ClaimedAt.Equal(row.ClaimedAt) {
			return runtimePublicationUnavailable
		}
		result := tx.Model(&entity.RuntimePublicationCommand{}).Where(database.ExactText(tx, clause.Column{Name: "request_id"}, row.RequestID)).Where("outcome = ?", "claimed").Updates(map[string]any{"outcome": outcome, "finalized_at": finalized, "failure_code": code})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtimePublicationUnavailable
		}
		if err := runtimePublicationAudit(tx, terminal, "runtime.publication.finalize"); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil || ctx.Err() != nil {
		return nil, runtimePublicationUnavailable
	}
	// Recording an objective original outcome does not depend on the actor still
	// being eligible. Returning private history does require a fresh recheck.
	return s.getRuntimePublicationCommand(ctx, auth, row.RequestID)
}

func (s *Service) GetRuntimePublicationCommand(ctx context.Context, auth *Authentication, requestID string) (*RuntimePublicationCommandReceipt, error) {
	if !runtimePublicationRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeRefreshTimeout)
	defer cancel()
	return s.getRuntimePublicationCommand(ctx, auth, requestID)
}

func (s *Service) getRuntimePublicationCommand(ctx context.Context, auth *Authentication, requestID string) (*RuntimePublicationCommandReceipt, error) {
	var row entity.RuntimePublicationCommand
	var receipt *RuntimePublicationCommandReceipt
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := s.runtimePublicationActor(tx, auth)
		if err != nil {
			return err
		}
		row, err = runtimePublicationRead(tx, requestID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := runtimePublicationIntent(row, actor, nil); err != nil {
			return err
		}
		receipt, err = s.runtimePublicationReceiptWithAcknowledgement(tx, row)
		if err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, runtimePublicationError(err)
	}
	if ctx.Err() != nil {
		return nil, runtimePublicationUnavailable
	}
	return receipt, nil
}
