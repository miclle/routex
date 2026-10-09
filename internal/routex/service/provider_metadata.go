package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type ProviderMetadataInput struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
type ProviderMetadataRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	ETag    string `json:"etag"`
	CanEdit bool   `json:"can_edit"`
}
type ProviderMetadataWriteResult struct {
	Provider       ProviderMetadataRecord `json:"provider"`
	RuntimeApplied bool                   `json:"runtime_applied"`
	Changed        bool                   `json:"changed"`
}

var providerMetadataUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "provider metadata confirmation unavailable"}

func validProviderMetadataInput(input ProviderMetadataInput) bool {
	return validCatalogLabel(input.Name) && validCredentialMetadataReason(input.Reason) && strings.TrimSpace(input.Reason) == input.Reason
}
func (input *ProviderMetadataInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"name", "reason"})
	if err != nil {
		return err
	}
	var next ProviderMetadataInput
	for name, target := range map[string]*string{"name": &next.Name, "reason": &next.Reason} {
		if json.Unmarshal(fields[name], target) != nil || string(fields[name]) == "null" {
			return apperrors.ErrBadRequest
		}
	}
	if !validProviderMetadataInput(next) {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}
func providerMetadataIDs(actorID, providerID string) error {
	if !safeTeamSessionID(actorID) || !strings.HasPrefix(actorID, "usr_") {
		return apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(providerID) || !strings.HasPrefix(providerID, "prv_") {
		return apperrors.ErrBadRequest
	}
	return nil
}
func providerMetadataRecord(actor entity.User, row entity.Provider, write bool) (ProviderMetadataRecord, error) {
	if providerMetadataIDs(actor.ID, row.ID) != nil || !connectionMetadataBirth(actor.CreatedAt) || !connectionMetadataBirth(row.CreatedAt) || !validCatalogLabel(row.Name) {
		return ProviderMetadataRecord{}, providerMetadataUnavailable
	}
	identity := connectionMetadataHash(struct {
		Version, ActorID, ProviderID string
		ActorBirth, ProviderBirth    time.Time
	}{"provider.metadata.identity.v1", actor.ID, row.ID, actor.CreatedAt.UTC(), row.CreatedAt.UTC()})
	content := connectionMetadataHash(struct {
		Version, Name, Revision string
		Enabled                 bool
	}{"provider.metadata.review.v2", row.Name, row.ETag, row.Enabled})
	return ProviderMetadataRecord{row.ID, row.Name, identity + "." + content, write}, nil
}
func providerMetadataQuery(tx *gorm.DB, providerID string) *gorm.DB {
	return memberRolesExact(memberRolesDB(tx).Model(&entity.Provider{}), "id", providerID)
}
func providerMetadataSnapshot(tx *gorm.DB, actorID, providerID string, writeOnly, lock bool) (entity.User, entity.Provider, ProviderMetadataRecord, error) {
	var row entity.Provider
	var record ProviderMetadataRecord
	actor, err := registrationAdmittedUser(memberRolesDB(tx), actorID, lock)
	if err != nil {
		return actor, row, record, err
	}
	if actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
		return actor, row, record, apperrors.ErrUnauthorized
	}
	permission := "providers.read"
	if writeOnly {
		permission = "providers.write"
	}
	allowed, err := exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, permission)
	if err != nil {
		return actor, row, record, err
	}
	if !allowed {
		return actor, row, record, apperrors.ErrForbidden
	}
	write := writeOnly
	if !writeOnly {
		write, err = exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, "providers.write")
		if err != nil {
			return actor, row, record, err
		}
	}
	q := providerMetadataQuery(tx, providerID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err = q.First(&row).Error; err != nil {
		return actor, row, record, err
	}
	if row.ID != providerID {
		return actor, row, record, apperrors.ErrNotFound
	}
	record, err = providerMetadataRecord(actor, row, write)
	return actor, row, record, err
}
func providerMetadataReadError(err error) error {
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return providerMetadataUnavailable
}
func (s *Service) GetProviderMetadata(ctx context.Context, actorID, providerID string) (*ProviderMetadataRecord, error) {
	if err := providerMetadataIDs(actorID, providerID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var record ProviderMetadataRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, next, err := providerMetadataSnapshot(tx, actorID, providerID, false, false)
		record = next
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, providerMetadataReadError(err)
	}
	return &record, nil
}
func providerMetadataReview(record ProviderMetadataRecord, etag string, input ProviderMetadataInput) error {
	if !validConnectionMetadataETag(etag) {
		return apperrors.ErrBadRequest
	}
	if record.ETag[:64] != etag[:64] {
		return catalogConflict
	}
	// Current content may return to the same value. Equality proves neither a
	// historical operation nor the absence of intervening edits.
	if record.Name != input.Name && record.ETag != etag {
		return catalogConflict
	}
	return nil
}
