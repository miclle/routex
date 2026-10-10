package service

import (
	"context"
	"crypto/subtle"
	"regexp"
	"sort"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// TeamSessionIdentity is an exact request-local Team invocation subject. Its
// private proofs must never be projected into a response or durable call fact.
type TeamSessionIdentity struct {
	SessionID          string
	UserID             string
	TeamID             string
	TeamMembershipID   string
	ModelIDs           []string
	tokenHash          string
	csrfProofHash      string
	expiresAt          time.Time
	teamCreatedAt      time.Time
	oidcPolicyRevision string
	oidcBindingKey     string
}

type runtimeTeamSession struct {
	ID, UserID, TokenHash string
	ExpiresAt             time.Time
	OIDCPolicyRevision    string
	OIDCBindingKey        string
}

type runtimeTeam struct {
	CreatedAt time.Time
	Members   map[string]string
	Models    map[string]bool
}

type teamSessionRuntimeData struct {
	Sessions     []entity.Session
	Teams        []entity.Team
	Memberships  []entity.TeamMembership
	Grants       []entity.TeamModelGrant
	OIDCProvider *entity.OIDCProvider
	OIDCBindings []entity.OIDCBinding
}

var teamSessionCookie = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var teamSessionDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func loadTeamSessionRuntimeData(tx *gorm.DB) (*teamSessionRuntimeData, error) {
	data := &teamSessionRuntimeData{}
	if err := tx.Select("id", "user_id", "token_hash", "expires_at", "primary_method", "oidc_binding_id", "oidc_binding_created_at", "oidc_config_revision", "oidc_policy_revision", "oidc_user_created_at").Where("expires_at > ?", time.Now().UTC()).Find(&data.Sessions).Error; err != nil {
		return nil, err
	}
	if err := loadOIDCSessionRuntimeData(tx, data); err != nil {
		return nil, err
	}
	for _, target := range []any{&data.Teams, &data.Memberships, &data.Grants} {
		if err := tx.Find(target).Error; err != nil {
			return nil, err
		}
	}
	return data, nil
}

func addTeamSessionRuntimeAuthorization(auth *runtimeAuthorization, data *teamSessionRuntimeData, users []entity.User) {
	if data == nil {
		return
	}
	enabledUsers := map[string]bool{}
	for _, user := range users {
		if safeTeamSessionID(user.ID) && auth.UserAdmissions[user.ID].Eligible {
			enabledUsers[user.ID] = true
		}
	}
	usersByID := map[string]entity.User{}
	for _, u := range users {
		usersByID[u.ID] = u
	}
	bindings := map[string]entity.OIDCBinding{}
	for _, b := range data.OIDCBindings {
		bindings[b.ID] = b
	}
	for _, session := range data.Sessions {
		if !oidcRuntimePrimary(session, data.OIDCProvider, bindings, usersByID) {
			continue
		}
		if safeTeamSessionID(session.ID) && enabledUsers[session.UserID] && teamSessionDigest.MatchString(session.TokenHash) {
			auth.TeamSessions[session.TokenHash] = runtimeTeamSession{ID: session.ID, UserID: session.UserID, TokenHash: session.TokenHash, ExpiresAt: session.ExpiresAt, OIDCPolicyRevision: session.OIDCPolicyRevision, OIDCBindingKey: oidcRuntimeBindingKey(session.OIDCBindingID, session.OIDCBindingCreatedAt)}
		}
	}
	auth.TeamCreationGrants = map[string]map[string]runtimeTeamCreationGrant{}
	for _, team := range data.Teams {
		if safeTeamSessionID(team.ID) && team.Status == entity.ResourceActive {
			auth.TeamCreationGrants[team.ID] = map[string]runtimeTeamCreationGrant{}
			auth.Teams[team.ID] = runtimeTeam{CreatedAt: team.CreatedAt, Members: map[string]string{}, Models: map[string]bool{}}
		}
	}
	for _, membership := range data.Memberships {
		team, exists := auth.Teams[membership.TeamID]
		if exists && safeTeamSessionID(membership.ID) && enabledUsers[membership.UserID] && membership.Status == entity.ResourceActive && (membership.Role == entity.TeamOwner || membership.Role == entity.TeamMember) {
			team.Members[membership.UserID] = membership.ID
		}
	}
	for _, grant := range data.Grants {
		if stored, exists := auth.TeamCreationGrants[grant.TeamID]; exists {
			source := ""
			if grant.SourceCreationReceiptID != nil {
				source = *grant.SourceCreationReceiptID
			}
			request := ""
			if grant.SourceRequestID != nil {
				request = *grant.SourceRequestID
			}
			stored[grant.ModelID] = runtimeTeamCreationGrant{ModelCreatedAt: auth.ModelCreated[grant.ModelID], CreationID: source, RequestID: request}
		}
		team, exists := auth.Teams[grant.TeamID]
		if exists && auth.Models[grant.ModelID] {
			team.Models[grant.ModelID] = true
			if grant.SourceRequestID != nil {
				if auth.TeamGrantSources == nil {
					auth.TeamGrantSources = map[string]map[string]string{}
				}
				if auth.TeamGrantSources[grant.TeamID] == nil {
					auth.TeamGrantSources[grant.TeamID] = map[string]string{}
				}
				auth.TeamGrantSources[grant.TeamID][grant.ModelID] = *grant.SourceRequestID
			}
		}
	}
}

func safeTeamSessionID(value string) bool {
	return len(value) <= 30 && safeCallID.MatchString(value)
}

// RuntimeAuthenticateTeamSession authenticates the existing HttpOnly cookie
// against leased private state, without reading Control Plane tables per call.
func (s *Service) RuntimeAuthenticateTeamSession(ctx context.Context, rawCookie, teamID string) (*TeamSessionIdentity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !teamSessionCookie.MatchString(rawCookie) {
		return nil, teamSessionInvalid()
	}
	if !safeTeamSessionID(teamID) {
		return nil, gatewayError(400, "invalid_request", "The Team context is invalid.")
	}
	auth, err := s.teamSessionAuthorization()
	if err != nil {
		return nil, err
	}
	hash := secret.SHA256Hex(rawCookie)
	session, exists := auth.TeamSessions[hash]
	if !exists || session.TokenHash != hash || !s.gatewayAttemptClock().Before(session.ExpiresAt) || runtimeDenied(&s.runtime.deniedSessions, session.ID) || runtimeDenied(&s.runtime.deniedSessionUsers, session.UserID) || runtimeDenied(&s.runtime.deniedUsers, session.UserID) || s.runtimeOIDCSessionDenied(session) {
		return nil, teamSessionInvalid()
	}
	team, exists := auth.Teams[teamID]
	if !exists || runtimeDenied(&s.runtime.deniedTeams, teamID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(teamID, session.UserID)) || team.Members[session.UserID] == "" {
		return nil, teamSessionDenied()
	}
	models := make([]string, 0, len(team.Models))
	for modelID := range team.Models {
		if auth.Models[modelID] && !runtimeDenied(&s.runtime.deniedModels, modelID) {
			models = append(models, modelID)
		}
	}
	sort.Strings(models)
	return &TeamSessionIdentity{SessionID: session.ID, UserID: session.UserID, TeamID: teamID, TeamMembershipID: team.Members[session.UserID], ModelIDs: models, tokenHash: hash, csrfProofHash: secret.SHA256Hex(secret.SHA256Hex("routex-csrf:" + rawCookie)), expiresAt: session.ExpiresAt, teamCreatedAt: team.CreatedAt, oidcPolicyRevision: session.OIDCPolicyRevision, oidcBindingKey: session.OIDCBindingKey}, nil
}

// ValidateTeamSessionCSRF preserves the existing cookie-derived CSRF contract.
// The proof is derived transiently at authentication, never stored or published.
func ValidateTeamSessionCSRF(identity *TeamSessionIdentity, rawCSRF string) bool {
	return identity != nil && teamSessionDigest.MatchString(rawCSRF) && subtle.ConstantTimeCompare([]byte(identity.csrfProofHash), []byte(secret.SHA256Hex(rawCSRF))) == 1
}

// ReauthorizeTeamSession rechecks the exact captured identity and selected
// model at each dispatch checkpoint. Route eligibility is checked separately.
func (s *Service) ReauthorizeTeamSession(ctx context.Context, identity *TeamSessionIdentity, modelID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if identity == nil {
		return teamSessionInvalid()
	}
	auth, err := s.teamSessionAuthorization()
	if err != nil {
		return err
	}
	session, exists := auth.TeamSessions[identity.tokenHash]
	if !exists || session.ID != identity.SessionID || session.UserID != identity.UserID || session.TokenHash != identity.tokenHash || session.OIDCPolicyRevision != identity.oidcPolicyRevision || session.OIDCBindingKey != identity.oidcBindingKey || !session.ExpiresAt.Equal(identity.expiresAt) || !s.gatewayAttemptClock().Before(session.ExpiresAt) || runtimeDenied(&s.runtime.deniedSessions, session.ID) || runtimeDenied(&s.runtime.deniedSessionUsers, session.UserID) || runtimeDenied(&s.runtime.deniedUsers, session.UserID) || s.runtimeOIDCSessionDenied(session) {
		return teamSessionInvalid()
	}
	team, exists := auth.Teams[identity.TeamID]
	if !exists || team.Members[identity.UserID] != identity.TeamMembershipID || identity.TeamMembershipID == "" || runtimeDenied(&s.runtime.deniedTeams, identity.TeamID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(identity.TeamID, identity.UserID)) {
		return teamSessionDenied()
	}
	if modelID != "" && (!team.Models[modelID] || !auth.Models[modelID] || runtimeDenied(&s.runtime.deniedModels, modelID)) {
		return gatewayError(404, "model_not_found", "The model is not available in this context.")
	}
	return nil
}

func (s *Service) teamSessionAuthorization() (*runtimeAuthorization, error) {
	if s.runtime == nil {
		return nil, teamSessionUnavailable()
	}
	auth := s.runtime.auth.Load()
	if auth == nil || !s.gatewayAttemptClock().Before(auth.ValidUntil) {
		return nil, teamSessionUnavailable()
	}
	return auth, nil
}

func teamSessionInvalid() error {
	return gatewayError(401, "invalid_session", "The session is not authorized.")
}

func teamSessionDenied() error {
	return gatewayError(403, "team_access_denied", "This Team context is not authorized.")
}

func teamSessionUnavailable() error {
	return gatewayError(503, "service_unavailable", "Runtime authorization is temporarily unavailable.")
}

func teamMemberRuntimeKey(teamID, userID string) string { return teamID + "\x00" + userID }

func (s *Service) invalidateRuntimeSession(sessionID string) {
	if s.runtime != nil {
		s.runtime.deniedSessions.Store(sessionID, s.runtime.epoch.Add(1))
	}
}

func (s *Service) invalidateRuntimeSessionUser(userID string) {
	if s.runtime != nil {
		s.runtime.deniedSessionUsers.Store(userID, s.runtime.epoch.Add(1))
	}
}

func (s *Service) invalidateRuntimeTeam(teamID string) {
	if s.runtime != nil {
		s.runtime.deniedTeams.Store(teamID, s.runtime.epoch.Add(1))
	}
}

func (s *Service) invalidateRuntimeTeamMember(teamID, userID string) {
	if s.runtime != nil {
		s.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey(teamID, userID), s.runtime.epoch.Add(1))
	}
}

func (s *Service) publishSessionMutation(ctx context.Context) {
	// Existing identity success confirms the durable identity operation. Runtime
	// failure does not change that contract; native admission still requires a
	// fresh lease and the local revocation tombstones remain effective.
	_ = s.RefreshRuntime(ctx)
}
