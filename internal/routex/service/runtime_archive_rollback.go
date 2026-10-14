package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

func runtimeArchiveRollbackError(err error) error {
	for _, expected := range []error{apperrors.ErrBadRequest, apperrors.ErrUnauthorized, apperrors.ErrForbidden, apperrors.ErrNotFound, runtimeArchiveRollbackConflict} {
		if errors.Is(err, expected) {
			return expected
		}
	}
	return runtimeArchiveRollbackUnavailable
}
func runtimeArchiveRollbackIntent(actor entity.User, archiveID, etag string, input RuntimeArchiveRollbackInput) string {
	return connectionMetadataHash([]any{"routex.archive-routing-intent.v1", actor.ID, actor.CreatedAt.UTC(), archiveID, etag, input.RequestID, input.Reason})
}
func runtimeArchiveRollbackValid(row entity.RuntimeArchiveRollback) bool {
	actor := entity.User{ID: row.ActorID, CreatedAt: row.ActorCreatedAt}
	return runtimePublicationRequestID.MatchString(row.RequestID) && modelCreationID(row.ActorID, "usr") && runtimeArchiveID.MatchString(row.ArchiveID) && runtimeRouteDigest.MatchString(row.ReviewETag) && runtimeRouteDigest.MatchString(row.DesiredDigest) && runtimePublicationReason(row.Reason) && runtimeInstallationMicrosecond(row.ActorCreatedAt) && runtimeInstallationMicrosecond(row.ArchiveCreatedAt) && runtimeInstallationMicrosecond(row.AppliedAt) && !row.AppliedAt.Before(row.ActorCreatedAt) && !row.AppliedAt.Before(row.ArchiveCreatedAt) && row.InputDigest == runtimeArchiveRollbackIntent(actor, row.ArchiveID, row.ReviewETag, RuntimeArchiveRollbackInput{row.RequestID, row.Reason})
}
func (s *Service) PreviewRuntimeArchiveRollback(ctx context.Context, auth *Authentication, archiveID string) (*RuntimeArchiveRollbackPreview, error) {
	if !runtimeArchiveID.MatchString(archiveID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var result *RuntimeArchiveRollbackPreview
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := s.runtimeArchiveRollbackActor(tx, auth, false)
		if err != nil {
			return err
		}
		st, err := s.runtimeArchiveRollbackState(ctx, tx, actor, archiveID)
		if err != nil {
			return err
		}
		if _, err = s.runtimeArchiveRollbackActor(tx, auth, false); err != nil {
			return err
		}
		result = st.Preview
		return ctx.Err()
	})
	if err != nil {
		return nil, runtimeArchiveRollbackError(err)
	}
	if ctx.Err() != nil {
		return nil, runtimeArchiveRollbackUnavailable
	}
	return result, nil
}

func (s *Service) GetRuntimeArchiveRollback(ctx context.Context, auth *Authentication, requestID string) (*RuntimeArchiveRollbackReceipt, error) {
	if !runtimePublicationRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var result *RuntimeArchiveRollbackReceipt
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := s.runtimeArchiveRollbackActor(tx, auth, false)
		if err != nil {
			return err
		}
		var row entity.RuntimeArchiveRollback
		if err = personalExact(tx, "request_id", requestID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrNotFound
			}
			return err
		}
		if row.RequestID != requestID || !runtimeArchiveRollbackValid(row) {
			return runtimeArchiveRollbackUnavailable
		}
		if row.ActorID != actor.ID || !row.ActorCreatedAt.Equal(actor.CreatedAt) {
			return apperrors.ErrForbidden
		}
		var archive entity.RuntimeConfigurationArchive
		if err = personalExact(tx.Select("id", "created_at"), "id", row.ArchiveID).Take(&archive).Error; err != nil {
			return err
		}
		if archive.ID != row.ArchiveID || !archive.CreatedAt.Equal(row.ArchiveCreatedAt) {
			return runtimeArchiveRollbackUnavailable
		}
		publication, err := runtimePublicationRead(tx, requestID)
		if err != nil {
			return err
		}
		if runtimePublicationIntent(publication, actor, &row.Reason) != nil {
			return runtimeArchiveRollbackUnavailable
		}
		current, err := loadRuntimeArchiveRoutingState(tx)
		if err != nil {
			return err
		}
		superseded := runtimeArchiveRoutingDigest(current) != row.DesiredDigest
		applied := false
		if !superseded {
			data, err := s.loadRuntimeDataTx(tx)
			if err != nil {
				return err
			}
			digest, err := runtimeDigest(data)
			if err != nil {
				return err
			}
			applied = s.connectionMetadataRuntimeApplied(digest, data.EgressGeneration)
		}
		if _, err = s.runtimeArchiveRollbackActor(tx, auth, false); err != nil {
			return err
		}
		result = &RuntimeArchiveRollbackReceipt{Scope: "gateway_routing_configuration", RequestID: row.RequestID, ArchiveID: row.ArchiveID, ArchiveCreatedAt: row.ArchiveCreatedAt.UTC(), AppliedAt: row.AppliedAt.UTC(), Changed: row.Changed, Reason: row.Reason, Publication: s.runtimePublicationReceipt(publication), RuntimeApplied: applied, Superseded: superseded}
		return ctx.Err()
	})
	if err != nil {
		return nil, runtimeArchiveRollbackError(err)
	}
	if ctx.Err() != nil {
		return nil, runtimeArchiveRollbackUnavailable
	}
	return result, nil
}

// The reviewed SQL, original publication claim and protected archive reference
// commit atomically. Unknown commit acknowledgements never authorize dispatch.
// Exact retries do not reapply desired SQL or dispatch an original claim again.
func (s *Service) RollbackRuntimeArchive(ctx context.Context, auth *Authentication, archiveID, etag string, input RuntimeArchiveRollbackInput) (*RuntimeArchiveRollbackReceipt, error) {
	if !runtimeArchiveID.MatchString(archiveID) || !runtimeRouteDigest.MatchString(etag) || !runtimePublicationRequestID.MatchString(input.RequestID) || !runtimePublicationReason(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	release, err := s.pinModelLifecycle(writeCtx)
	if err != nil {
		cancel()
		return nil, runtimeArchiveRollbackUnavailable
	}
	var row entity.RuntimePublicationCommand
	var lease *systemInstanceLease
	var state *runtimeArchiveRollbackState
	created := false
	writeAttempted := false
	err = s.authDB(writeCtx).Transaction(func(tx *gorm.DB) error {
		actor, err := s.runtimeArchiveRollbackActor(tx, auth, true)
		if err != nil {
			return err
		}
		var saved entity.RuntimeArchiveRollback
		err = personalExact(tx, "request_id", input.RequestID).Take(&saved).Error
		if err == nil {
			if !runtimeArchiveRollbackValid(saved) || saved.RequestID != input.RequestID || saved.ActorID != actor.ID || !saved.ActorCreatedAt.Equal(actor.CreatedAt) || saved.ArchiveID != archiveID || saved.ReviewETag != etag || saved.Reason != input.Reason || saved.InputDigest != runtimeArchiveRollbackIntent(actor, archiveID, etag, input) {
				return runtimeArchiveRollbackConflict
			}
			row, err = runtimePublicationRead(tx, input.RequestID)
			if err != nil {
				return err
			}
			return runtimePublicationIntent(row, actor, &input.Reason)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// A UUID used by the generic publication API cannot be repurposed as rollback.
		_, err = runtimePublicationRead(tx, input.RequestID)
		if err == nil {
			return runtimeArchiveRollbackConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		state, err = s.runtimeArchiveRollbackState(writeCtx, tx, actor, archiveID)
		if err != nil {
			return err
		}
		if state.Preview.ETag != etag || !state.Preview.CanApply {
			return runtimeArchiveRollbackConflict
		}
		lease = s.runtimePublicationLease()
		if err = s.runtimePublicationProcess(tx, lease); err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		row = entity.RuntimePublicationCommand{RequestID: input.RequestID, ActorID: actor.ID, ActorCreatedAt: actor.CreatedAt, Reason: input.Reason, ClaimInstanceID: lease.id, ClaimInstanceStartedAt: lease.startedAt, ClaimedAt: now, Outcome: "claimed"}
		if !runtimePublicationValid(row) {
			return runtimeArchiveRollbackUnavailable
		}
		if result := tx.Create(&row); result.Error != nil {
			return result.Error
		} else if result.RowsAffected != 1 {
			return runtimeArchiveRollbackUnavailable
		}
		if err = runtimePublicationAudit(tx, row, "runtime.publication.claim"); err != nil {
			return err
		}
		writeAttempted = true
		if err = s.writeRuntimeArchiveRouting(tx, actor, state, now); err != nil {
			return err
		}
		changed := len(state.Preview.Connections)+len(state.Preview.Declarations)+len(state.Preview.Weights) > 0
		command := entity.RuntimeArchiveRollback{RequestID: input.RequestID, ActorID: actor.ID, ActorCreatedAt: actor.CreatedAt, ArchiveID: archiveID, ArchiveCreatedAt: state.Header.CreatedAt, ReviewETag: etag, InputDigest: runtimeArchiveRollbackIntent(actor, archiveID, etag, input), DesiredDigest: state.DesiredDigest, Reason: input.Reason, Changed: changed, AppliedAt: now}
		if !runtimeArchiveRollbackValid(command) {
			return runtimeArchiveRollbackUnavailable
		}
		if result := tx.Create(&command); result.Error != nil {
			return result.Error
		} else if result.RowsAffected != 1 {
			return runtimeArchiveRollbackUnavailable
		}
		if err = runtimeArchiveRollbackAudit(tx, command, state.Preview); err != nil {
			return err
		}
		if _, err = s.runtimeArchiveRollbackActor(tx, auth, true); err != nil {
			return err
		}
		created = true
		return writeCtx.Err()
	})
	// Even an uncertain SQL acknowledgement must close admission to obsolete
	// routes. Over-denial on rollback is safe; only a fresh coherent publication
	// can clear the tombstone. Already dispatched calls retain their old attempts.
	if state != nil && writeAttempted {
		s.invalidateRuntimeArchiveRouting(state)
	}
	if err == nil && writeCtx.Err() != nil {
		err = writeCtx.Err()
	}
	release()
	cancel()
	if err != nil {
		return nil, runtimeArchiveRollbackError(err)
	}
	if created {
		publishCtx, publishCancel := context.WithTimeout(ctx, 3*time.Second)
		_, err = s.dispatchRuntimePublication(publishCtx, auth, row, lease, func(tx *gorm.DB) error {
			actor, e := s.runtimeArchiveRollbackActor(tx, auth, true)
			if e != nil {
				return e
			}
			current, e := loadRuntimeArchiveRoutingState(tx)
			if e != nil {
				return e
			}
			if actor.ID != row.ActorID || !actor.CreatedAt.Equal(row.ActorCreatedAt) || runtimeArchiveRoutingDigest(current) != state.DesiredDigest {
				return runtimeArchiveRollbackConflict
			}
			return publishCtx.Err()
		}, true)
		publishCancel()
		if err != nil {
			return nil, runtimeArchiveRollbackError(err)
		}
	}
	return s.GetRuntimeArchiveRollback(ctx, auth, input.RequestID)
}

func (s *Service) invalidateRuntimeArchiveRouting(st *runtimeArchiveRollbackState) {
	if s.runtime == nil {
		return
	}
	for identity := range st.TransportChanged {
		s.runtime.deniedConnections.Store(identity, s.runtime.epoch.Add(1))
	}
	for identity := range st.DeclarationChanged {
		s.runtime.deniedProviderModels.Store(identity, s.runtime.epoch.Add(1))
	}
	for identity := range st.WeightModels {
		s.InvalidateRuntimeModel(identity)
	}
}

func runtimeArchiveWeightRows(st runtimeArchiveRoutingState, modelID string) []ModelWeightRow {
	rows := []ModelWeightRow{}
	for _, b := range st.Bindings {
		if b.ModelID != modelID {
			continue
		}
		pi := slices.IndexFunc(st.ProviderModels, func(p entity.ProviderModel) bool { return p.ID == b.ProviderModelID })
		if pi < 0 {
			return nil
		}
		pm := st.ProviderModels[pi]
		ci := slices.IndexFunc(st.Connections, func(c entity.ProviderConnection) bool { return c.ID == pm.ConnectionID })
		if ci < 0 {
			return nil
		}
		c := st.Connections[ci]
		ri := slices.IndexFunc(st.Providers, func(p entity.Provider) bool { return p.ID == c.ProviderID })
		if ri < 0 {
			return nil
		}
		p := st.Providers[ri]
		rows = append(rows, ModelWeightRow{b.ID, modelWeightBirth(b.CreatedAt), pm.ID, modelWeightBirth(pm.CreatedAt), c.ID, modelWeightBirth(c.CreatedAt), p.ID, modelWeightBirth(p.CreatedAt), c.Protocol, b.Weight})
	}
	return rows
}
