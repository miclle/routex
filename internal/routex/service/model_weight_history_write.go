package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type modelWeightAudit struct {
	SourceVersionID *string `json:"source_version_id"`
	SavedVersionID  *string `json:"saved_version_id"`
	TargetVersionID *string `json:"target_version_id"`
	RequestID       *string `json:"request_id"`
	BeforeDigest    string  `json:"before_digest"`
	AfterDigest     string  `json:"after_digest"`
	BindingCount    int     `json:"binding_count"`
	Effect          string  `json:"effect"`
	Reason          *string `json:"reason"`
}

func appendModelWeightAudit(tx *gorm.DB, actor, modelID, action string, details modelWeightAudit) error {
	raw, err := json.Marshal(details)
	if err != nil || len(raw) > 8*1024 {
		return modelWeightUnavailable
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	detailsJSON := string(raw)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actor, Action: action, ResourceType: "model", ResourceID: modelID, DetailsJSON: &detailsJSON}).Error
}
func createModelWeightVersion(tx *gorm.DB, actor string, st *modelWeightState, rows []ModelWeightRow, source string, parent, rollback *string, reason *string, sequence uint64, now time.Time) (*entity.ModelWeightVersion, error) {
	raw, digest, err := modelWeightSnapshot(rows)
	if err != nil {
		return nil, err
	}
	versionID, err := id.NewPrefixed("mwv")
	if err != nil {
		return nil, err
	}
	if sequence == 0 {
		return nil, modelWeightUnavailable
	}
	v := &entity.ModelWeightVersion{ID: versionID, ModelID: st.Model.ID, ModelBirth: modelWeightBirth(st.Model.CreatedAt), Sequence: sequence, CapturedAt: now, Source: source, ParentVersionID: parent, RollbackVersionID: rollback, ActorID: actor, Reason: reason, BindingCount: len(rows), ValidWeightSet: !st.Model.CreatedAt.IsZero() && modelWeightRowsValid(rows), Snapshot: raw, SnapshotDigest: digest}
	return v, tx.Create(v).Error
}

// Both versions share the actual capture time; monotonic per-Model sequence
// orders them without inventing a historic timestamp or publication.
func journalModelWeightChange(tx *gorm.DB, actor string, st *modelWeightState, after []ModelWeightRow, source string, target, reason *string, now time.Time) (*string, *string, error) {
	latest, err := modelWeightLatest(tx, st)
	if err != nil {
		return nil, nil, err
	}
	seq := uint64(0)
	if latest != nil {
		seq = latest.Sequence
	}
	if seq > uint64(1<<63-1)-2 {
		return nil, nil, modelWeightUnavailable
	}
	beforeID := modelWeightCurrentVersion(st, latest)
	if beforeID == nil {
		var parent *string
		if latest != nil {
			id := latest.ID
			parent = &id
		}
		baseline, err := createModelWeightVersion(tx, actor, st, st.Rows, "observed_baseline", parent, nil, nil, seq+1, now)
		if err != nil {
			return nil, nil, err
		}
		seq++
		beforeID = &baseline.ID
	}
	version, err := createModelWeightVersion(tx, actor, st, after, source, beforeID, target, reason, seq+1, now)
	if err != nil {
		return nil, nil, err
	}
	return beforeID, &version.ID, nil
}
func modelWeightIntent(actor entity.User, model entity.Model, etag string, input ModelWeightRollbackInput) string {
	return memberModelsDigest(struct {
		ActorID, ModelID, ETag string
		ActorBirth, ModelBirth time.Time
		Input                  ModelWeightRollbackInput
	}{actor.ID, model.ID, etag, actor.CreatedAt.UTC(), model.CreatedAt.UTC(), input})
}
func modelWeightCommandReceipt(c entity.ModelWeightRollbackCommand) ModelWeightRollbackReceipt {
	return ModelWeightRollbackReceipt{c.RequestID, c.ModelID, c.VersionID, c.SourceVersionID, c.SavedVersionID, c.Effect, c.Reason, c.CreatedAt.UTC()}
}
func modelWeightCommandIdentity(c entity.ModelWeightRollbackCommand, actor entity.User, model entity.Model) bool {
	return c.ActorID == actor.ID && c.ActorBirth.Equal(actor.CreatedAt) && c.ModelID == model.ID && c.ModelBirth.Equal(model.CreatedAt)
}
func (s *Service) RollbackModelWeights(ctx context.Context, actorID, modelID, etag string, input ModelWeightRollbackInput) (*ModelWeightRollbackResult, error) {
	if !validAdminModelTarget(modelID) || !modelWeightID(input.VersionID, "mwv") || !credentialReplacementRequestID.MatchString(input.RequestID) || !validRoleDefinitionReason(input.Reason) || !validMemberRoleDigest(etag) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	var command entity.ModelWeightRollbackCommand
	changed := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, _, err := modelWeightAuthority(tx, actorID, true, true)
		if err != nil {
			return err
		}
		st, err := loadModelWeightState(tx, modelID, true, true)
		if err != nil {
			return err
		}
		var previous entity.ModelWeightRollbackCommand
		err = personalExact(modelCreationDB(tx), "request_id", input.RequestID).Take(&previous).Error
		if err == nil {
			if !modelWeightCommandIdentity(previous, actor, st.Model) || previous.InputDigest != modelWeightIntent(actor, st.Model, etag, input) || previous.ReviewETag != etag {
				return modelWeightConflict
			}
			command = previous
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		target, rows, err := modelWeightVersion(tx, st, input.VersionID)
		if err != nil {
			return err
		}
		latest, err := modelWeightLatest(tx, st)
		if err != nil {
			return err
		}
		review := modelWeightReview(actor, true, st, target, rows, latest)
		if !review.Eligible || review.ReviewETag != etag {
			return modelWeightConflict
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		command = entity.ModelWeightRollbackCommand{RequestID: input.RequestID, ActorID: actor.ID, ActorBirth: actor.CreatedAt.UTC(), ModelID: st.Model.ID, ModelBirth: st.Model.CreatedAt.UTC(), VersionID: input.VersionID, SourceVersionID: review.CurrentVersionID, SavedVersionID: review.CurrentVersionID, ReviewETag: etag, InputDigest: modelWeightIntent(actor, st.Model, etag, input), DesiredDigest: memberModelsDigest(rows), Effect: "noop", Reason: input.Reason, CreatedAt: now}
		if memberModelsDigest(st.Rows) != command.DesiredDigest {
			command.Effect = "changed"
			changed = true
			beforeID, afterID, err := journalModelWeightChange(tx, actor.ID, st, rows, "rollback", &input.VersionID, &input.Reason, now)
			if err != nil {
				return err
			}
			command.SourceVersionID, command.SavedVersionID = beforeID, afterID
			for i, row := range rows {
				if st.Rows[i].Weight == row.Weight {
					continue
				}
				q := personalExact(personalExact(modelCreationDB(tx).Model(&entity.ModelProviderBinding{}), "id", row.BindingID), "model_id", modelID).Where("weight = ?", st.Rows[i].Weight).Update("weight", row.Weight)
				if q.Error != nil {
					return q.Error
				}
				if q.RowsAffected != 1 {
					return modelWeightConflict
				}
			}
			if err := stampModelConfiguration(modelCreationDB(tx), modelID, now); err != nil {
				return err
			}
		}
		if err := tx.Create(&command).Error; err != nil {
			return err
		}
		return appendModelWeightAudit(tx, actor.ID, modelID, "model.weights.rollback", modelWeightAudit{command.SourceVersionID, command.SavedVersionID, &input.VersionID, &input.RequestID, memberModelsDigest(st.Rows), command.DesiredDigest, len(rows), command.Effect, &input.Reason})
	})
	if err == nil && changed {
		// Preserve the existing commit-to-publication revocation fence. Receipt
		// replay/noop never installs another tombstone or restores old weights.
		s.InvalidateRuntimeModel(modelID)
	}
	release()
	if err != nil {
		return nil, catalogError(err)
	}
	// Publication failure cannot erase the committed receipt or repeat its write.
	// Noop/replay never reapplies a superseded retained configuration.
	_ = s.RefreshRuntime(ctx)
	return s.GetModelWeightRollbackReceipt(ctx, actorID, modelID, input.RequestID)
}
func (s *Service) GetModelWeightRollbackReceipt(ctx context.Context, actorID, modelID, requestID string) (*ModelWeightRollbackResult, error) {
	if !validAdminModelTarget(modelID) || !credentialReplacementRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var command entity.ModelWeightRollbackCommand
	var st *modelWeightState
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, _, err := modelWeightAuthority(tx, actorID, true, false)
		if err != nil {
			return err
		}
		var model entity.Model
		if err := personalExact(modelCreationDB(tx), "id", modelID).Take(&model).Error; err != nil {
			return err
		}
		if model.ID != modelID {
			return apperrors.ErrNotFound
		}
		if err := personalExact(modelCreationDB(tx), "request_id", requestID).Take(&command).Error; err != nil {
			return err
		}
		if command.RequestID != requestID || !modelWeightCommandIdentity(command, actor, model) {
			return apperrors.ErrNotFound
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	// A bounded current-eligibility outage does not erase historical receipt
	// evidence. It yields unknown application, never an invented current match.
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, _, err := modelWeightAuthority(tx, actorID, true, false)
		if err != nil {
			return err
		}
		st, err = loadModelWeightState(tx, modelID, false, true)
		if err != nil {
			return err
		}
		if !modelWeightCommandIdentity(command, actor, st.Model) {
			return apperrors.ErrNotFound
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	status := "unknown"
	if err != nil {
		var app *apperrors.Error
		if errors.As(err, &app) && (app.Code == 401 || app.Code == 403 || app.Code == 404) {
			return nil, err
		}
	} else if command.DesiredDigest != memberModelsDigest(st.Rows) {
		status = "superseded"
	} else if s.modelWeightRuntimeApplied(st) {
		status = "applied"
	} else if s.runtime != nil && s.runtime.auth.Load() != nil {
		status = "pending"
	}
	return &ModelWeightRollbackResult{modelWeightCommandReceipt(command), status, status == "applied"}, nil
}
