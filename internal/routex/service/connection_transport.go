package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"regexp"
	"strings"
	"time"
)

var transportGenerationPattern = regexp.MustCompile(`^rev_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

func validTransportGeneration(value string) bool {
	return value == "0" || transportGenerationPattern.MatchString(value)
}
func verifiedTransportCurrent(c entity.ProviderCredential, connection entity.ProviderConnection) bool {
	return c.ConnectionID == connection.ID && connection.ID != "" && connectionMetadataBirth(c.CreatedAt) && connectionMetadataBirth(connection.CreatedAt) && c.VerificationStatus == "verified" && validTransportGeneration(connection.TransportGeneration) && validTransportGeneration(c.VerifiedTransportGeneration) && c.VerifiedTransportGeneration == connection.TransportGeneration
}
func capabilityTransportCurrent(pm entity.ProviderModel, c entity.ProviderConnection) bool {
	return pm.ConnectionID == c.ID && c.ID != "" && connectionMetadataBirth(pm.CreatedAt) && connectionMetadataBirth(c.CreatedAt) && validTransportGeneration(c.TransportGeneration) && validTransportGeneration(pm.CapabilityTransportGeneration) && pm.CapabilityTransportGeneration == c.TransportGeneration
}
func capacityTransportCurrent(b entity.ReservationBound, pm entity.ProviderModel, c entity.ProviderConnection) bool {
	return b.ProviderModelID == pm.ID && pm.ConnectionID == c.ID && b.Protocol == c.Protocol && connectionMetadataBirth(pm.CreatedAt) && connectionMetadataBirth(c.CreatedAt) && validTransportGeneration(b.TransportGeneration) && validTransportGeneration(c.TransportGeneration) && b.TransportGeneration == c.TransportGeneration
}

type ConnectionTransportInput struct {
	BaseURL    string  `json:"base_url"`
	Protocol   string  `json:"protocol"`
	Adapter    string  `json:"adapter"`
	APIVersion *string `json:"api_version"`
}

func connectionTransportTuple(c entity.ProviderConnection) ConnectionTransportInput {
	return ConnectionTransportInput{c.BaseURL, c.Protocol, entity.ConnectionAdapter(c), c.APIVersion}
}
func sameConnectionTransport(a, b ConnectionTransportInput) bool {
	return a.BaseURL == b.BaseURL && a.Protocol == b.Protocol && a.Adapter == b.Adapter && (a.APIVersion == nil && b.APIVersion == nil || a.APIVersion != nil && b.APIVersion != nil && *a.APIVersion == *b.APIVersion)
}
func (s *Service) canonicalConnectionTransport(input ConnectionTransportInput) (ConnectionTransportInput, error) {
	if !entity.SupportedNativeProtocol(input.Protocol) || input.Adapter == "" {
		return input, apperrors.ErrBadRequest
	}
	u, e := upstream.ValidateBaseURL(input.BaseURL, s.allowPrivateUpstream)
	if e != nil {
		return input, apperrors.ErrBadRequest
	}
	input.BaseURL = strings.TrimRight(u.String(), "/")
	if validateConnectionAdapter(entity.ProviderConnection{BaseURL: input.BaseURL, Protocol: input.Protocol, Adapter: input.Adapter, APIVersion: input.APIVersion}, s.allowPrivateUpstream) != nil {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func connectionTransportLocked(tx *gorm.DB, id string) (bool, error) {
	var count int64
	err := memberRolesExact(memberRolesDB(tx).Model(&entity.ProviderModel{}), "connection_id", id).Limit(1).Count(&count).Error
	return count > 0, err
}
func capabilityReviewETag(actor entity.User, p entity.Provider, c entity.ProviderConnection, pm entity.ProviderModel) string {
	if !connectionMetadataBirth(actor.CreatedAt) || !connectionMetadataBirth(p.CreatedAt) || !connectionMetadataBirth(c.CreatedAt) || !connectionMetadataBirth(pm.CreatedAt) || c.ProviderID != p.ID || pm.ConnectionID != c.ID || !validTransportGeneration(c.TransportGeneration) {
		return ""
	}
	return connectionMetadataHash(struct {
		Version, ActorID, ProviderID, ConnectionID, ModelID, Revision, Generation string
		ActorBirth, ProviderBirth, ConnectionBirth, ModelBirth                    time.Time
		Image, PDF                                                                bool
		Transport                                                                 ConnectionTransportInput
	}{"provider_model.capability.review.v1", actor.ID, p.ID, c.ID, pm.ID, pm.ETag, c.TransportGeneration, actor.CreatedAt.UTC(), p.CreatedAt.UTC(), c.CreatedAt.UTC(), pm.CreatedAt.UTC(), pm.SupportsImageInput, pm.SupportsPDFInput, connectionTransportTuple(c)})
}
func transportSubject(tx *gorm.DB, actorID, modelID, permission string, lock bool) (entity.User, entity.Provider, entity.ProviderConnection, entity.ProviderModel, error) {
	var p entity.Provider
	var c entity.ProviderConnection
	var pm entity.ProviderModel
	if !modelCreationID(actorID, "usr") {
		return entity.User{}, p, c, pm, apperrors.ErrUnauthorized
	}
	if !modelCreationID(modelID, "pmd") {
		return entity.User{}, p, c, pm, apperrors.ErrBadRequest
	}
	actor, err := registrationAdmittedUser(memberRolesDB(tx), actorID, lock)
	if err != nil {
		return actor, p, c, pm, err
	}
	allowed, err := exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, permission)
	if err != nil {
		return actor, p, c, pm, err
	}
	if !allowed {
		return actor, p, c, pm, apperrors.ErrForbidden
	}
	if err := memberRolesExact(memberRolesDB(tx), "id", modelID).Take(&pm).Error; err != nil {
		return actor, p, c, pm, err
	}
	if pm.ID != modelID {
		return actor, p, c, pm, apperrors.ErrNotFound
	}
	_, p, c, _, err = connectionMetadataSnapshot(tx, actorID, pm.ConnectionID, permission != "providers.read", lock)
	if err != nil {
		return actor, p, c, pm, err
	}
	if lock {
		if e := memberRolesExact(memberRolesDB(tx), "id", modelID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&pm).Error; e != nil {
			return actor, p, c, pm, e
		}
	}
	if pm.ConnectionID != c.ID || !connectionMetadataBirth(pm.CreatedAt) || !validTransportGeneration(c.TransportGeneration) {
		return actor, p, c, pm, connectionMetadataUnavailable
	}
	return actor, p, c, pm, nil
}

func runtimeTransportDigestProof(data *runtimeData) map[string]string {
	result := map[string]string{}
	for _, c := range data.Connections {
		if c.TransportGeneration != "0" {
			result["connection:"+c.ID] = c.TransportGeneration
		}
	}
	for _, c := range data.Credentials {
		if c.VerifiedTransportGeneration != "0" {
			result["credential:"+c.ID] = c.VerifiedTransportGeneration
		}
	}
	for _, pm := range data.ProviderModels {
		if pm.CapabilityTransportGeneration != "0" {
			result["capability:"+pm.ID] = pm.CapabilityTransportGeneration
		}
	}
	if data.Quota != nil {
		for id, b := range data.Quota.Bounds {
			if b.TransportGeneration != "0" {
				result["capacity:"+id] = b.TransportGeneration
			}
		}
	}
	return result
}

func (s *Service) projectCatalogTransport(tx *gorm.DB, actorID string, item *ProviderCatalog) error {
	actor, e := registrationAdmittedUser(memberRolesDB(tx), actorID, false)
	if e != nil {
		return e
	}
	allowed, e := exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, "providers.read")
	if e != nil {
		return e
	}
	if !allowed {
		return apperrors.ErrForbidden
	}
	if !connectionMetadataBirth(item.Provider.CreatedAt) {
		return connectionMetadataUnavailable
	}
	for i := range item.Connections {
		row := &item.Connections[i]
		c := row.Connection
		if c.ProviderID != item.Provider.ID {
			return connectionMetadataUnavailable
		}
		for j := range row.Credentials {
			cred := &row.Credentials[j]
			cred.VerificationTransportCurrent = verifiedTransportCurrent(*cred, c)
		}
		for j := range row.Models {
			pm := &row.Models[j]
			pm.CapabilitiesTransportCurrent = capabilityTransportCurrent(*pm, c)
			pm.CapabilityReviewETag = capabilityReviewETag(actor, item.Provider, c, *pm)
		}
	}
	return nil
}

func modelCreationTransportProof(state *modelCreationState) map[string]string {
	data := &runtimeData{Connections: []entity.ProviderConnection{state.Connection}, ProviderModels: state.ProviderModels}
	for _, model := range state.Models {
		for _, topology := range model.Topology {
			data.Connections = append(data.Connections, topology.Connection)
			data.ProviderModels = append(data.ProviderModels, topology.ProviderModel)
		}
	}
	result := runtimeTransportDigestProof(data)
	if len(result) == 0 {
		return nil
	}
	return result
}
