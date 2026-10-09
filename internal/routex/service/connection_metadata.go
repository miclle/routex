package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

type ConnectionMetadataInput struct {
	Transport *ConnectionTransportInput `json:"transport,omitempty"`
	Name      string                    `json:"name"`
	Reason    string                    `json:"reason"`
}
type ConnectionMetadataRecord struct {
	TransportGeneration string  `json:"transport_generation"`
	CanEditTransport    bool    `json:"can_edit_transport"`
	TransportLocked     bool    `json:"transport_locked"`
	Adapter             string  `json:"adapter"`
	APIVersion          *string `json:"api_version"`
	ID                  string  `json:"id"`
	ProviderID          string  `json:"provider_id"`
	Name                string  `json:"name"`
	Protocol            string  `json:"protocol"`
	BaseURL             string  `json:"base_url"`
	EgressMode          string  `json:"egress_mode"`
	EgressID            *string `json:"egress_id"`
	ETag                string  `json:"etag"`
	CanEdit             bool    `json:"can_edit"`
}
type ConnectionMetadataWriteResult struct {
	Connection     ConnectionMetadataRecord `json:"connection"`
	RuntimeApplied bool                     `json:"runtime_applied"`
	Changed        bool                     `json:"changed"`
}

var connectionMetadataUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "connection metadata confirmation unavailable"}

func validConnectionMetadataInput(input ConnectionMetadataInput) bool {
	return validCatalogLabel(input.Name) && validCredentialMetadataReason(input.Reason) && strings.TrimSpace(input.Reason) == input.Reason
}
func (input *ConnectionMetadataInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil {
		return apperrors.ErrBadRequest
	}
	keys := []string{"name", "reason"}
	if _, ok := probe["transport"]; ok {
		keys = append(keys, "transport")
	}
	fields, err := decodeDefaultLimitObject(raw, keys)
	if err != nil {
		return err
	}
	var next ConnectionMetadataInput
	for name, target := range map[string]*string{"name": &next.Name, "reason": &next.Reason} {
		if json.Unmarshal(fields[name], target) != nil || string(fields[name]) == "null" {
			return apperrors.ErrBadRequest
		}
	}
	if value, ok := fields["transport"]; ok {
		tuple, err := decodeDefaultLimitObject(value, []string{"base_url", "protocol", "adapter", "api_version"})
		if err != nil {
			return err
		}
		var t ConnectionTransportInput
		for key, target := range map[string]*string{"base_url": &t.BaseURL, "protocol": &t.Protocol, "adapter": &t.Adapter} {
			if json.Unmarshal(tuple[key], target) != nil || string(tuple[key]) == "null" {
				return apperrors.ErrBadRequest
			}
		}
		if json.Unmarshal(tuple["api_version"], &t.APIVersion) != nil {
			return apperrors.ErrBadRequest
		}
		next.Transport = &t
	}
	if !validConnectionMetadataInput(next) {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}
func connectionMetadataIDs(actorID, connectionID string) error {
	if !safeTeamSessionID(actorID) || !strings.HasPrefix(actorID, "usr_") {
		return apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(connectionID) || !strings.HasPrefix(connectionID, "con_") {
		return apperrors.ErrBadRequest
	}
	return nil
}
func validConnectionMetadataETag(etag string) bool {
	return len(etag) == 129 && etag[64] == '.' && validMemberRoleDigest(etag[:64]) && validMemberRoleDigest(etag[65:])
}
func connectionMetadataHash(value any) string {
	// Only bounded string/bool/time configuration values enter these frozen objects.
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func connectionMetadataBirth(value time.Time) bool {
	normalized := value.UTC()
	return !normalized.IsZero() && normalized.Year() >= 1 && normalized.Year() <= 9999
}
func connectionMetadataRecord(actor entity.User, provider entity.Provider, row entity.ProviderConnection, write bool, locks ...bool) (ConnectionMetadataRecord, error) {
	if !connectionMetadataBirth(actor.CreatedAt) || !connectionMetadataBirth(provider.CreatedAt) || !connectionMetadataBirth(row.CreatedAt) || provider.ID != row.ProviderID || !safeTeamSessionID(provider.ID) || !strings.HasPrefix(provider.ID, "prv_") {
		return ConnectionMetadataRecord{}, connectionMetadataUnavailable
	}
	mode := row.EgressMode
	if mode == "" {
		mode = "default"
	}
	if mode != "default" && mode != "direct" && mode != "proxy" || row.EgressID != nil && (!safeTeamSessionID(*row.EgressID) || !strings.HasPrefix(*row.EgressID, "egr_")) || mode == "proxy" && row.EgressID == nil || mode == "direct" && row.EgressID != nil {
		return ConnectionMetadataRecord{}, connectionMetadataUnavailable
	}
	if !validTransportGeneration(row.TransportGeneration) {
		return ConnectionMetadataRecord{}, connectionMetadataUnavailable
	}
	locked := len(locks) > 0 && locks[0]
	record := ConnectionMetadataRecord{TransportGeneration: row.TransportGeneration, CanEditTransport: write && !row.Enabled, TransportLocked: locked, Adapter: entity.ConnectionAdapter(row), APIVersion: row.APIVersion, ID: row.ID, ProviderID: row.ProviderID, Name: row.Name, Protocol: row.Protocol, BaseURL: row.BaseURL, EgressMode: mode, CanEdit: write}
	if row.EgressID != nil {
		value := *row.EgressID
		record.EgressID = &value
	}
	identity := connectionMetadataHash(struct {
		Version, ActorID, ProviderID, ConnectionID string
		ActorBirth, ProviderBirth, ConnectionBirth time.Time
	}{"connection.metadata.identity.v1", actor.ID, provider.ID, row.ID, actor.CreatedAt.UTC(), provider.CreatedAt.UTC(), row.CreatedAt.UTC()})
	review := connectionMetadataHash(struct {
		Version                    string
		Record                     ConnectionMetadataRecord
		Revision, StoredEgressMode string
		Enabled                    bool
	}{"connection.metadata.review.v1", record, row.ETag, row.EgressMode, row.Enabled})
	record.ETag = identity + "." + review
	return record, nil
}
func connectionMetadataQuery(tx *gorm.DB, connectionID string) *gorm.DB {
	return memberRolesExact(memberRolesDB(tx).Model(&entity.ProviderConnection{}), "id", connectionID)
}
func connectionMetadataSnapshot(tx *gorm.DB, actorID, connectionID string, writeOnly, lock bool) (entity.User, entity.Provider, entity.ProviderConnection, ConnectionMetadataRecord, error) {
	var provider entity.Provider
	var row entity.ProviderConnection
	var record ConnectionMetadataRecord
	actor, err := registrationAdmittedUser(memberRolesDB(tx), actorID, lock)
	if err != nil {
		return actor, provider, row, record, err
	}
	if actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
		return actor, provider, row, record, apperrors.ErrUnauthorized
	}
	permission := "providers.read"
	if writeOnly {
		permission = "providers.write"
	}
	allowed, err := exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, permission)
	if err != nil {
		return actor, provider, row, record, err
	}
	if !allowed {
		return actor, provider, row, record, apperrors.ErrForbidden
	}
	write := writeOnly
	if !writeOnly {
		write, err = exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, "providers.write")
		if err != nil {
			return actor, provider, row, record, err
		}
	}
	q := connectionMetadataQuery(tx, connectionID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err = q.First(&row).Error; err != nil {
		return actor, provider, row, record, err
	}
	if row.ID != connectionID || !safeTeamSessionID(row.ProviderID) || !strings.HasPrefix(row.ProviderID, "prv_") {
		return actor, provider, row, record, apperrors.ErrNotFound
	}
	q = memberRolesExact(memberRolesDB(tx).Model(&entity.Provider{}), "id", row.ProviderID)
	// The governance lock precedes all write-side resource locks. Providers have
	// no metadata deletion path; the exact birth is rechecked after publication.
	if err = q.First(&provider).Error; err != nil {
		return actor, provider, row, record, err
	}
	if provider.ID != row.ProviderID {
		return actor, provider, row, record, apperrors.ErrNotFound
	}
	locked, e := connectionTransportLocked(tx, row.ID)
	if e != nil {
		return actor, provider, row, record, e
	}
	record, err = connectionMetadataRecord(actor, provider, row, write, locked)
	return actor, provider, row, record, err
}
func (s *Service) GetConnectionMetadata(ctx context.Context, actorID, connectionID string) (*ConnectionMetadataRecord, error) {
	if err := connectionMetadataIDs(actorID, connectionID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var record ConnectionMetadataRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, _, next, err := connectionMetadataSnapshot(tx, actorID, connectionID, false, false)
		record = next
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		var app *apperrors.Error
		if errors.As(err, &app) {
			return nil, app
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.ErrNotFound
		}
		return nil, connectionMetadataUnavailable
	}
	return &record, nil
}
func connectionMetadataReview(record ConnectionMetadataRecord, etag string, input ConnectionMetadataInput) error {
	if !validConnectionMetadataETag(etag) {
		return apperrors.ErrBadRequest
	}
	if record.ETag[:64] != etag[:64] {
		return catalogConflict
	}
	if record.Name != input.Name && record.ETag != etag {
		return catalogConflict
	}
	return nil
}
