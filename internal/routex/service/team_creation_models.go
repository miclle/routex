package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TeamCreationModelFilter struct {
	Query, Cursor string
	Limit         int
}
type TeamCreationModelCandidate struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Providers []string `json:"providers"`
	Protocols []string `json:"protocols"`
}
type TeamCreationModelPage struct {
	Items      []TeamCreationModelCandidate `json:"items"`
	NextCursor *string                      `json:"next_cursor"`
}
type TeamCreationModelReviewInput struct {
	ModelIDs []string `json:"model_ids"`
}
type TeamCreationModelReview struct {
	ModelIDs         []string `json:"model_ids"`
	ModelReviewToken string   `json:"model_review_token"`
}

func (input *TeamCreationModelReviewInput) UnmarshalJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return apperrors.ErrBadRequest
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	if !d.More() {
		return apperrors.ErrBadRequest
	}
	key, err := d.Token()
	if err != nil || key != "model_ids" {
		return apperrors.ErrBadRequest
	}
	var value json.RawMessage
	if d.Decode(&value) != nil {
		return apperrors.ErrBadRequest
	}
	ids, err := projectCreationStringIDs(value, true)
	if err != nil {
		return apperrors.ErrBadRequest
	}
	ids, err = normalizeTeamCreationModelIDs(ids, false)
	if err != nil || d.More() {
		return apperrors.ErrBadRequest
	}
	if _, err = d.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if _, err = d.Token(); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	input.ModelIDs = ids
	return nil
}
func normalizeTeamCreationModelIDs(ids []string, empty bool) ([]string, error) {
	if len(ids) > 1000 || !empty && len(ids) == 0 {
		return nil, apperrors.ErrBadRequest
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	for i, id := range ids {
		if !validAdminModelTarget(id) || i > 0 && id == ids[i-1] {
			return nil, apperrors.ErrBadRequest
		}
	}
	return ids, nil
}
func teamCreationModelPermissions(tx *gorm.DB, actor entity.User) error {
	for _, p := range []string{"teams.write", "teams.models.write"} {
		ok, err := exactGovernancePermissionForAdmittedActor(tx, actor, p)
		if err != nil {
			return err
		}
		if !ok {
			return apperrors.ErrForbidden
		}
	}
	return nil
}

// Read only the explicit selected set and its bounded reachable metadata. This
// does not call the global member workspace or load a candidate catalogue.
func readTeamCreationModels(tx *gorm.DB, ids []string, lock, providers bool) (*memberModelsData, error) {
	data := &memberModelsData{ProvidersRead: providers}
	for offset := 0; offset < len(ids); offset += 500 {
		end := min(offset+500, len(ids))
		batch := ids[offset:end]
		q := modelCreationDB(tx).Where("id IN ?", batch).Where(memberModelsExactIDs(tx, "id", batch)).Order("id").Limit(len(batch) + 1)
		if lock {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var rows []entity.Model
		if err := q.Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) != len(batch) {
			return nil, catalogConflict
		}
		for _, m := range rows {
			if !slices.Contains(batch, m.ID) || m.Status != entity.ResourceActive || m.CreatedAt.IsZero() {
				return nil, catalogConflict
			}
			data.Models = append(data.Models, m)
		}
	}
	slices.SortFunc(data.Models, func(a, b entity.Model) int { return strings.Compare(a.ID, b.ID) })
	if err := readMemberModelMetadata(tx, data, ids); err != nil {
		return nil, err
	}
	for i, m := range data.Models {
		if m.ID != ids[i] {
			return nil, catalogConflict
		}
	}
	return data, nil
}
func (s *Service) teamCreationModelProof(data *memberModelsData) ([]TeamCreationModelCandidate, []string, error) {
	if s.runtime == nil {
		return nil, nil, runtimeUnavailable
	}
	auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if auth == nil || routes == nil || !time.Now().Before(auth.ValidUntil) || auth.SourceDigest == "" || auth.SourceDigest != routes.Digest {
		return nil, nil, runtimeUnavailable
	}
	index := memberModelsIndex(data)
	hashes, err := readMemberEffectiveCipherHashesForCreation(data)
	if err != nil {
		return nil, nil, err
	}
	rows := make([]TeamCreationModelCandidate, 0, len(data.Models))
	proofs := make([]string, 0, len(data.Models))
	providerNames := map[string]string{}
	for _, p := range data.Providers {
		providerNames[p.ID] = p.Name
	}
	for _, m := range data.Models {
		protocols, eligible, state := s.memberModelProtocols(m, index, auth, routes)
		if state == "unknown" || !memberEffectiveModelCipherProof(m.ID, hashes, routes) {
			return nil, nil, runtimeUnavailable
		}
		if state != "ready" {
			rows = append(rows, TeamCreationModelCandidate{ID: m.ID, Protocols: []string{}})
			proofs = append(proofs, "")
			continue
		}
		row := TeamCreationModelCandidate{ID: m.ID, Name: index.names[m.ID].Name, Protocols: protocols}
		if data.ProvidersRead {
			row.Providers = []string{}
			for _, pmID := range eligible {
				pm := index.models[pmID]
				connection := index.connections[pm.ConnectionID]
				if name := providerNames[connection.ProviderID]; name != "" {
					row.Providers = append(row.Providers, name)
				}
			}
			slices.Sort(row.Providers)
			row.Providers = slices.Compact(row.Providers)
		}
		rows = append(rows, row)
		proofs = append(proofs, personalHash(struct {
			Configured string
			Protocols  []string
		}{index.hash(m), protocols}))
	}
	if s.runtime.auth.Load() != auth || s.runtime.routes.Load() != routes || !time.Now().Before(auth.ValidUntil) {
		return nil, nil, runtimeUnavailable
	}
	return rows, proofs, nil
}

// Ciphertexts remain private; their hashes only prove current source continuity.
// The query is performed by the selected-set reader, never by this pure projection.
func readMemberEffectiveCipherHashesForCreation(data *memberModelsData) (map[string]string, error) {
	hashes := map[string]string{}
	for _, c := range data.Credentials {
		if credentialSourceProof(c) == "" {
			return nil, runtimeUnavailable
		}
		hashes[c.ID] = credentialSourceProof(c)
	}
	return hashes, nil
}
func readTeamCreationCiphertexts(tx *gorm.DB, data *memberModelsData) error {
	hashes := map[string]entity.ProviderCredential{}
	ids := []string{}
	for _, c := range data.Credentials {
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	for offset := 0; offset < len(ids); offset += 500 {
		batch := ids[offset:min(offset+500, len(ids))]
		var rows []entity.ProviderCredential
		if err := modelCreationDB(tx).Select("id", "ciphertext", "storage_source", "created_at").Where("id IN ?", batch).Where(memberModelsExactIDs(tx, "id", batch)).Limit(len(batch) + 1).Find(&rows).Error; err != nil {
			return err
		}
		if err := attachCredentialSources(tx, rows); err != nil {
			return err
		}
		if len(rows) != len(batch) {
			return runtimeUnavailable
		}
		for _, c := range rows {
			if !slices.Contains(batch, c.ID) || hashes[c.ID].ID != "" {
				return runtimeUnavailable
			}
			hashes[c.ID] = c
		}
	}
	for i, c := range data.Credentials {
		data.Credentials[i].Ciphertext = hashes[c.ID].Ciphertext
		data.Credentials[i].StorageSource = hashes[c.ID].StorageSource
		data.Credentials[i].VaultReference = hashes[c.ID].VaultReference
	}
	return nil
}
func teamCreationModelReviewHash(review *teamCreationReview, models []entity.Model, proofs []string) string {
	models = slices.Clone(models)
	for i := range models {
		models[i].CreatedAt = models[i].CreatedAt.UTC()
	}
	return personalHash(struct {
		Domain, Actor, Context string
		Born                   time.Time
		Models                 []entity.Model
		Proofs                 []string
	}{"routex.team-creation.model-review.v1", review.Actor.ID, review.Public.ReviewETag, review.Actor.CreatedAt.UTC(), models, proofs})
}
func (s *Service) ReviewTeamCreationModels(ctx context.Context, actorID, etag string, input TeamCreationModelReviewInput) (*TeamCreationModelReview, error) {
	ids, err := normalizeTeamCreationModelIDs(input.ModelIDs, false)
	if err != nil || !teamSessionDigest.MatchString(etag) {
		return nil, apperrors.ErrBadRequest
	}
	var result *TeamCreationModelReview
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		review, err := loadTeamCreationReview(tx, actor, false)
		if err != nil {
			return err
		}
		if !review.Public.CanSetModels {
			return apperrors.ErrForbidden
		}
		if review.Public.ReviewETag != etag {
			return catalogConflict
		}
		data, err := readTeamCreationModels(tx, ids, false, false)
		if err != nil {
			return err
		}
		if err = readTeamCreationCiphertexts(tx, data); err != nil {
			return err
		}
		_, proofs, err := s.teamCreationModelProof(data)
		if err != nil {
			return err
		}
		if slices.Contains(proofs, "") {
			return catalogConflict
		}
		result = &TeamCreationModelReview{ModelIDs: ids, ModelReviewToken: teamCreationModelReviewHash(review, data.Models, proofs)}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) ListTeamCreationModelCandidates(ctx context.Context, actorID string, filter TeamCreationModelFilter) (*TeamCreationModelPage, error) {
	pattern, err := candidatePattern(filter.Query)
	if err != nil {
		return nil, err
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 50 || filter.Cursor != "" && !validAdminModelTarget(filter.Cursor) {
		return nil, apperrors.ErrBadRequest
	}
	result := &TeamCreationModelPage{Items: []TeamCreationModelCandidate{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		if err = teamCreationModelPermissions(tx, actor); err != nil {
			return err
		}
		providers, err := exactGovernancePermissionForAdmittedActor(tx, actor, "providers.read")
		if err != nil {
			return err
		}
		ids := []string{}
		col := clause.Column{Table: "m", Name: "id"}
		q := tx.Table("models AS m").Select("m.id").Joins("JOIN model_names AS n ON ?", database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "current_model_id"}, col)).Where(database.ExactTextColumns(tx, clause.Column{Table: "n", Name: "model_id"}, col)).Where(database.ExactText(tx, clause.Column{Table: "m", Name: "status"}, entity.ResourceActive)).Where("LOWER(n.name) LIKE ? ESCAPE '!'", pattern)
		if filter.Cursor != "" {
			q = q.Where(database.ByteAfter(tx, col, filter.Cursor))
		}
		if err := q.Clauses(clause.OrderBy{Expression: database.ByteOrder(tx, col)}).Limit(filter.Limit + 1).Scan(&ids).Error; err != nil {
			return err
		}
		if len(ids) > filter.Limit {
			cursor := ids[filter.Limit-1]
			result.NextCursor = &cursor
			ids = ids[:filter.Limit]
		}
		slices.Sort(ids)
		data, err := readTeamCreationModels(tx, ids, false, providers)
		if err != nil {
			return err
		}
		if err = readTeamCreationCiphertexts(tx, data); err != nil {
			return err
		}
		rows, proofs, err := s.teamCreationModelProof(data)
		if err != nil {
			return err
		}
		for i, row := range rows {
			if proofs[i] != "" {
				result.Items = append(result.Items, row)
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
