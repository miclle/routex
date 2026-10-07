package service

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"sort"

	"github.com/miclle/routex/internal/routex/entity"
)

// gatewayIdentity keeps real Key and Team Session subjects separate while both
// use the same native parsing, route attempts, admission and forwarding.
type gatewayIdentity struct {
	personalGrantRevision string
	key                   *KeyRecord
	team                  *TeamSessionIdentity
}

func (identity gatewayIdentity) modelIDs() []string {
	if identity.team != nil {
		return identity.team.ModelIDs
	}
	return identity.key.ModelIDs
}

func (identity gatewayIdentity) result(protocol, name string, stream bool) *GatewayResult {
	result := &GatewayResult{Protocol: protocol, ModelName: name, Stream: stream, identity: identity}
	if identity.team != nil {
		result.UserID, result.TeamID, result.TeamMembershipID = identity.team.UserID, identity.team.TeamID, identity.team.TeamMembershipID
	} else {
		result.UserID, result.ProjectID, result.KeyID = identity.key.Key.UserID, identity.key.ProjectID, identity.key.Key.ID
	}
	return result
}

// The complete SHA256 pair digest fits the existing 64-byte ASCII journal key.
// Membership replacement never creates a new accounting identity for the pair.
func teamMemberLimitAccount(teamID, userID string) string {
	raw, _ := json.Marshal([2]string{teamID, userID})
	digest := sha256.Sum256(raw)
	return "team_member_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])
}

func (s *Service) TeamGatewayModels(ctx context.Context, identity *TeamSessionIdentity) ([]GatewayModel, error) {
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	metadata, err := s.gatewayModelMetadata(ctx, identity.ModelIDs)
	if err != nil {
		return nil, gatewayError(503, "service_unavailable", "Model catalog is temporarily unavailable.")
	}
	auth := s.runtime.auth.Load()
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	output := []GatewayModel{}
	for _, name := range auth.Names {
		if name.CurrentModelID == nil || *name.CurrentModelID != name.ModelID {
			continue
		}
		if err := s.ReauthorizeTeamSession(ctx, identity, name.ModelID); err != nil {
			continue
		}
		protocols := []string{}
		capabilities := map[string][]string{}
		for _, protocol := range metadata[name.ModelID].Protocols {
			if !teamNativeProtocol(protocol) {
				continue
			}
			plan, err := s.gatewayAttemptPlan(name.ModelID, protocol)
			if err != nil {
				continue
			}
			plan.team, plan.userID = identity, identity.UserID
			intersection := inputCapabilityIntersection{}
			available := false
			for _, candidate := range plan.Candidates() {
				eligible, err := s.gatewayAttemptEligible(ctx, plan, candidate.attempt)
				if err != nil {
					plan.releaseSources()
					return nil, gatewayPublicAttemptError(err)
				}
				if eligible {
					available = true
					intersection.include(candidate.route)
				}
			}
			plan.releaseSources()
			if available {
				protocols = append(protocols, protocol)
				capabilities[protocol] = intersection.values()
			}
		}
		if len(protocols) == 0 {
			continue
		}
		output = append(output, GatewayModel{ID: name.Name, ModelID: name.ModelID, Object: "model", Created: auth.ModelCreated[name.ModelID].Unix(), OwnedBy: "routex", Protocols: protocols, InputCapabilities: capabilities, AttachmentScope: "team", AttachmentTeamID: identity.TeamID, AttachmentMembershipID: identity.TeamMembershipID, PersonalAttachments: false})
	}
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	sort.Slice(output, func(i, j int) bool { return output[i].ID < output[j].ID })
	return output, nil
}

func (s *Service) TeamGatewayChat(ctx context.Context, identity *TeamSessionIdentity, body []byte, requestID string) (*GatewayResult, error) {
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	return s.gatewayNativeIdentity(ctx, gatewayIdentity{team: identity}, body, requestID, entity.ProtocolOpenAIChat)
}

func (s *Service) TeamGatewayResponses(ctx context.Context, identity *TeamSessionIdentity, body []byte, requestID string) (*GatewayResult, error) {
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	return s.gatewayNativeIdentity(ctx, gatewayIdentity{team: identity}, body, requestID, entity.ProtocolOpenAIResponses)
}

func (s *Service) TeamGatewayMessages(ctx context.Context, identity *TeamSessionIdentity, body []byte, requestID string, headers MessagesHeaders) (*GatewayResult, error) {
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	if err := ValidateMessagesHeaders(headers); err != nil {
		return nil, err
	}
	return s.gatewayNativeIdentity(ctx, gatewayIdentity{team: identity}, body, requestID, entity.ProtocolAnthropicMessages, gatewayNativeOptions{Messages: headers})
}

func (s *Service) TeamGatewayGemini(ctx context.Context, identity *TeamSessionIdentity, body []byte, requestID, model string, stream bool) (*GatewayResult, error) {
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		return nil, err
	}
	return s.gatewayNativeIdentity(ctx, gatewayIdentity{team: identity}, body, requestID, entity.ProtocolGeminiGenerateContent, gatewayNativeOptions{GeminiModel: model, GeminiStream: stream})
}
