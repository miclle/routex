package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

const modelWeightSummaryColumns = "id,model_id,model_birth,sequence,captured_at,source,parent_version_id,rollback_version_id,actor_id,reason,binding_count,valid_weight_set"

func modelWeightSummary(v entity.ModelWeightVersion) ModelWeightVersionSummary {
	return ModelWeightVersionSummary{v.ID, v.ModelID, v.ModelBirth, v.CapturedAt.UTC(), v.Source, v.ParentVersionID, v.RollbackVersionID, v.ActorID, v.Reason, v.BindingCount, v.ValidWeightSet}
}
func modelWeightVersion(tx *gorm.DB, st *modelWeightState, versionID string) (entity.ModelWeightVersion, []ModelWeightRow, error) {
	var v entity.ModelWeightVersion
	err := personalExact(personalExact(modelCreationDB(tx), "id", versionID), "model_id", st.Model.ID).Take(&v).Error
	if err != nil {
		return v, nil, err
	}
	if v.ID != versionID || v.ModelID != st.Model.ID {
		return v, nil, apperrors.ErrNotFound
	}
	rows, canonical, err := decodeModelWeightSnapshot(v.Snapshot, v.BindingCount, v.SnapshotDigest)
	if err != nil {
		return v, nil, err
	}
	// Review identity hashes typed canonical rows, never storage JSON spelling.
	v.Snapshot = canonical
	return v, rows, nil
}
func modelWeightLatest(tx *gorm.DB, st *modelWeightState) (*entity.ModelWeightVersion, error) {
	var v entity.ModelWeightVersion
	err := personalExact(modelCreationDB(tx), "model_id", st.Model.ID).Order("sequence DESC").Take(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if v.ModelID != st.Model.ID {
		return nil, modelWeightUnavailable
	}
	_, canonical, err := decodeModelWeightSnapshot(v.Snapshot, v.BindingCount, v.SnapshotDigest)
	if err != nil {
		return nil, err
	}
	v.Snapshot = canonical
	return &v, nil
}
func modelWeightCurrentVersion(st *modelWeightState, v *entity.ModelWeightVersion) *string {
	if v == nil || !modelWeightSameBirth(v.ModelBirth, modelWeightBirth(st.Model.CreatedAt)) || v.SnapshotDigest != memberModelsDigest(st.Rows) {
		return nil
	}
	id := v.ID
	return &id
}
func modelWeightSameBirth(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}
func modelWeightCursorScope(actor entity.User, model entity.Model) string {
	return memberModelsDigest(struct {
		ActorID, ModelID       string
		ActorBirth, ModelBirth time.Time
	}{actor.ID, model.ID, actor.CreatedAt.UTC(), model.CreatedAt.UTC()})
}
func modelWeightCursor(sequence uint64, scope string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(sequence, 10) + "|" + scope))
}
func modelWeightCursorRead(raw, scope string) (uint64, error) {
	if len(raw) > 200 {
		return 0, apperrors.ErrBadRequest
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		return 0, apperrors.ErrBadRequest
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 2 || parts[1] != scope {
		return 0, apperrors.ErrBadRequest
	}
	n, e := strconv.ParseUint(parts[0], 10, 64)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != parts[0] || modelWeightCursor(n, scope) != raw {
		return 0, apperrors.ErrBadRequest
	}
	return n, nil
}
func (s *Service) ListModelWeightVersions(ctx context.Context, actorID, modelID string, filter ModelWeightHistoryFilter) (*ModelWeightVersionPage, error) {
	if !validAdminModelTarget(modelID) {
		return nil, apperrors.ErrBadRequest
	}
	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &ModelWeightVersionPage{ModelID: modelID, Items: []ModelWeightVersionSummary{}}
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
		q := personalExact(modelCreationDB(tx), "model_id", modelID).Select(modelWeightSummaryColumns).Order("sequence DESC").Limit(filter.Limit + 1)
		scope := modelWeightCursorScope(actor, model)
		if filter.Cursor != "" {
			seq, e := modelWeightCursorRead(filter.Cursor, scope)
			if e != nil {
				return e
			}
			q = q.Where("sequence < ?", seq)
		}
		var versions []entity.ModelWeightVersion
		if err := q.Find(&versions).Error; err != nil {
			return err
		}
		if len(versions) > filter.Limit {
			cursor := modelWeightCursor(versions[filter.Limit-1].Sequence, scope)
			result.NextCursor = &cursor
			versions = versions[:filter.Limit]
		}
		for _, v := range versions {
			if v.ModelID != modelID || !modelWeightID(v.ID, "mwv") || v.Sequence == 0 {
				return modelWeightUnavailable
			}
			result.Items = append(result.Items, modelWeightSummary(v))
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) GetModelWeightVersion(ctx context.Context, actorID, modelID, versionID string) (*ModelWeightVersionDetail, error) {
	if !validAdminModelTarget(modelID) || !modelWeightID(versionID, "mwv") {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *ModelWeightVersionDetail
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := modelWeightAuthority(tx, actorID, true, false); err != nil {
			return err
		}
		// Detail needs retained Model identity, not current credential readiness.
		var model entity.Model
		if err := personalExact(modelCreationDB(tx), "id", modelID).Take(&model).Error; err != nil {
			return err
		}
		if model.ID != modelID {
			return apperrors.ErrNotFound
		}
		v, rows, err := modelWeightVersion(tx, &modelWeightState{Model: model}, versionID)
		if err != nil {
			return err
		}
		result = &ModelWeightVersionDetail{modelWeightSummary(v), rows}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func modelWeightReview(actor entity.User, write bool, st *modelWeightState, target entity.ModelWeightVersion, rows []ModelWeightRow, latest *entity.ModelWeightVersion) *ModelWeightRollbackReview {
	blockers := []string{}
	if !write {
		blockers = append(blockers, "write_not_authorized")
	}
	if !target.ValidWeightSet || !modelWeightRowsValid(rows) || !modelWeightSameBirth(target.ModelBirth, modelWeightBirth(st.Model.CreatedAt)) || st.Model.CreatedAt.IsZero() {
		blockers = append(blockers, "version_not_restorable")
	}
	if !modelWeightTopologyEqual(st.Rows, rows) {
		blockers = append(blockers, "topology_changed")
	}
	if st.Model.Status != entity.ResourceActive {
		blockers = append(blockers, "model_inactive")
	}
	if !st.executable(rows) {
		blockers = append(blockers, "positive_route_unavailable")
	}
	// Private material binds source/egress revisions without projecting secrets.
	token := memberModelsDigest(struct {
		ActorID                string
		ActorBirth             time.Time
		Write                  bool
		Target                 entity.ModelWeightVersion
		State                  *modelWeightState
		Latest                 *entity.ModelWeightVersion
		ConfigurationUpdatedAt *time.Time
	}{actor.ID, actor.CreatedAt.UTC(), write, target, st, latest, st.Model.ConfigUpdatedAt})
	return &ModelWeightRollbackReview{st.Model.ID, target.ID, modelWeightCurrentVersion(st, latest), st.Rows, rows, len(blockers) == 0, write, blockers, token, time.Now().UTC()}
}
func (s *Service) ReviewModelWeightRollback(ctx context.Context, actorID, modelID, versionID string) (*ModelWeightRollbackReview, error) {
	if !validAdminModelTarget(modelID) || !modelWeightID(versionID, "mwv") {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *ModelWeightRollbackReview
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, write, err := modelWeightAuthority(tx, actorID, true, false)
		if err != nil {
			return err
		}
		st, err := loadModelWeightState(tx, modelID, false, true)
		if err != nil {
			return err
		}
		v, rows, err := modelWeightVersion(tx, st, versionID)
		if err != nil {
			return err
		}
		latest, err := modelWeightLatest(tx, st)
		if err != nil {
			return err
		}
		result = modelWeightReview(actor, write, st, v, rows, latest)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
