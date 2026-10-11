package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

// PersonalSessionIdentity is a real request-local Session subject. Private
// proofs are never returned to callers or persisted in call facts. ModelIDs is
// advisory; dispatch checks the private captured scope and current publication.
type PersonalSessionIdentity struct {
	SessionID string
	UserID    string
	ModelIDs  []string

	session       runtimeTeamSession
	user          runtimeUserProof
	admission     runtimeAdmissionProof
	grants        runtimePersonalGrantState
	models        []string
	csrfProofHash string
}

// RuntimeAuthenticatePersonalSession uses the published neutral Session index.
// Its historical TeamSessions name does not impose a Team or membership context.
func (s *Service) RuntimeAuthenticatePersonalSession(ctx context.Context, rawCookie string) (*PersonalSessionIdentity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !teamSessionCookie.MatchString(rawCookie) {
		return nil, teamSessionInvalid()
	}
	auth, err := s.teamSessionAuthorization()
	if err != nil {
		return nil, err
	}
	hash := secret.SHA256Hex(rawCookie)
	session, exists := auth.TeamSessions[hash]
	if !exists || session.TokenHash != hash || !s.runtimeSessionActive(auth, session) || !personalSessionUserPublished(auth, session.UserID) || runtimeDenied(&s.runtime.deniedPersonalGrants, session.UserID) {
		return nil, teamSessionInvalid()
	}
	models := []string{}
	for modelID, granted := range auth.PersonalModels[session.UserID] {
		if granted && auth.Models[modelID] && !runtimeDenied(&s.runtime.deniedModels, modelID) {
			models = append(models, modelID)
		}
	}
	sort.Strings(models)
	return &PersonalSessionIdentity{SessionID: session.ID, UserID: session.UserID, ModelIDs: slices.Clone(models), session: session, user: auth.UserProofs[session.UserID], admission: auth.UserAdmissions[session.UserID], grants: auth.PersonalGrantStates[session.UserID], models: models, csrfProofHash: secret.SHA256Hex(secret.SHA256Hex("routex-csrf:" + rawCookie))}, nil
}

func ValidatePersonalSessionCSRF(identity *PersonalSessionIdentity, rawCSRF string) bool {
	return identity != nil && teamSessionDigest.MatchString(rawCSRF) && subtle.ConstantTimeCompare([]byte(identity.csrfProofHash), []byte(secret.SHA256Hex(rawCSRF))) == 1
}

func personalSessionUserPublished(auth *runtimeAuthorization, userID string) bool {
	user, userExists := auth.UserProofs[userID]
	admission, admissionExists := auth.UserAdmissions[userID]
	grants, grantsExist := auth.PersonalGrantStates[userID]
	return safeTeamSessionID(userID) && userExists && admissionExists && grantsExist && user.Enabled && admission.Eligible && grants.Enabled && !user.CreatedAt.IsZero() && user.CreatedAt.Equal(admission.CreatedAt) && user.CreatedAt.Equal(grants.CreatedAt) && personalModelETag(grants.Revision) && personalModelETag(grants.Hash)
}

// Compare the complete neutral proof, including newly added primary-auth fields.
// Expiry compares instants rather than the internal representation of time.Time.
func samePersonalSessionProof(a, b runtimeTeamSession) bool {
	if !a.ExpiresAt.Equal(b.ExpiresAt) {
		return false
	}
	a.ExpiresAt, b.ExpiresAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

func samePersonalAdmission(a, b runtimeAdmissionProof) bool {
	return a.CreatedAt.Equal(b.CreatedAt) && a.ApplicationID == b.ApplicationID && a.ApplicationCreatedAt.Equal(b.ApplicationCreatedAt) && a.Revision == b.Revision && a.State == b.State && a.Eligible == b.Eligible
}

// ReauthorizePersonalSession preserves the exact Session, User birth/admission
// and complete personal grant generation at every native dispatch checkpoint.
func (s *Service) ReauthorizePersonalSession(ctx context.Context, identity *PersonalSessionIdentity, modelID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if identity == nil || identity.SessionID != identity.session.ID || identity.UserID != identity.session.UserID {
		return teamSessionInvalid()
	}
	auth, err := s.teamSessionAuthorization()
	if err != nil {
		return err
	}
	session, exists := auth.TeamSessions[identity.session.TokenHash]
	if !exists || !samePersonalSessionProof(session, identity.session) || !s.runtimeSessionActive(auth, session) || !personalSessionUserPublished(auth, identity.UserID) {
		return teamSessionInvalid()
	}
	user := auth.UserProofs[identity.UserID]
	if !user.CreatedAt.Equal(identity.user.CreatedAt) || user.Enabled != identity.user.Enabled || !samePersonalAdmission(auth.UserAdmissions[identity.UserID], identity.admission) {
		return teamSessionInvalid()
	}
	grants := auth.PersonalGrantStates[identity.UserID]
	if runtimeDenied(&s.runtime.deniedPersonalGrants, identity.UserID) || !grants.CreatedAt.Equal(identity.grants.CreatedAt) || grants.Enabled != identity.grants.Enabled || grants.Revision != identity.grants.Revision || grants.Hash != identity.grants.Hash {
		return gatewayError(403, "model_forbidden", "Personal model access must be reviewed again.")
	}
	if modelID != "" && (!slices.Contains(identity.models, modelID) || !auth.PersonalModels[identity.UserID][modelID] || !auth.Models[modelID] || runtimeDenied(&s.runtime.deniedModels, modelID)) {
		return gatewayError(404, "model_not_found", "The selected model is unavailable.")
	}
	return nil
}

// PersonalSessionGatewayModels exposes only exact personal grants with an
// eligible native Chat route. It does not expose Team-only or Key-scoped access.
func (s *Service) PersonalSessionGatewayModels(ctx context.Context, identity *PersonalSessionIdentity) ([]GatewayModel, error) {
	if err := s.ReauthorizePersonalSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	auth := s.runtime.auth.Load()
	output := []GatewayModel{}
	for _, name := range auth.Names {
		if name.CurrentModelID == nil || *name.CurrentModelID != name.ModelID {
			continue
		}
		if err := s.ReauthorizePersonalSession(ctx, identity, name.ModelID); err != nil {
			continue
		}
		plan, err := s.gatewayAttemptPlan(name.ModelID, entity.ProtocolOpenAIChat)
		if err != nil {
			continue
		}
		plan.personal, plan.userID = identity, identity.UserID
		available := false
		for _, candidate := range plan.Candidates() {
			eligible, checkErr := s.gatewayAttemptEligible(ctx, plan, candidate.attempt)
			if checkErr != nil {
				plan.releaseSources()
				return nil, gatewayPublicAttemptError(checkErr)
			}
			available = available || eligible
		}
		plan.releaseSources()
		if available {
			output = append(output, GatewayModel{ID: name.Name, ModelID: name.ModelID, Object: "model", Created: auth.ModelCreated[name.ModelID].Unix(), OwnedBy: "routex", Protocols: []string{entity.ProtocolOpenAIChat}, InputCapabilities: map[string][]string{entity.ProtocolOpenAIChat: {}}, AttachmentScope: entity.StorageOwnerUser, PersonalAttachments: false})
		}
	}
	if err := s.ReauthorizePersonalSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	if s.runtime.auth.Load() != auth {
		return nil, teamSessionUnavailable()
	}
	sort.Slice(output, func(i, j int) bool { return output[i].ID < output[j].ID })
	return output, nil
}

// PersonalSessionGatewayChat is internal control-plane analysis dispatch, not
// a generic personal inference endpoint. The caller owns response parsing and
// native usage/completion persistence even on failure or cancellation.
func (s *Service) PersonalSessionGatewayChat(ctx context.Context, identity *PersonalSessionIdentity, selectedModelID string, body []byte, requestID string) (*GatewayResult, error) {
	if selectedModelID == "" {
		return nil, gatewayError(400, "invalid_request_error", "An exact selected model is required.")
	}
	if err := s.ReauthorizePersonalSession(ctx, identity, selectedModelID); err != nil {
		return nil, err
	}
	if len(body) > 64<<10 {
		return nil, personalSessionInputUnsupported()
	}
	return s.gatewayNativeIdentity(ctx, gatewayIdentity{personal: identity, selectedModelID: selectedModelID}, body, requestID, entity.ProtocolOpenAIChat)
}

func personalSessionInputUnsupported() error {
	return gatewayError(400, "unsupported_input", "Personal Session analysis requires bounded nonstream text Chat input.")
}

func validatePersonalSessionChat(payload map[string]json.RawMessage) error {
	for field := range payload {
		if !slices.Contains([]string{"model", "messages", "max_completion_tokens", "stream"}, field) {
			return personalSessionInputUnsupported()
		}
	}
	var maximum int64
	if json.Unmarshal(payload["max_completion_tokens"], &maximum) != nil || maximum <= 0 || maximum > 4096 {
		return personalSessionInputUnsupported()
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(payload["messages"], &messages) != nil || len(messages) != 2 {
		return personalSessionInputUnsupported()
	}
	for index, message := range messages {
		if len(message) != 2 {
			return personalSessionInputUnsupported()
		}
		var role, content string
		if json.Unmarshal(message["role"], &role) != nil || role != []string{"system", "user"}[index] || json.Unmarshal(message["content"], &content) != nil || content == "" {
			return personalSessionInputUnsupported()
		}
	}
	return nil
}
