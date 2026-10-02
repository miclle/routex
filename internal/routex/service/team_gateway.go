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
	key  *KeyRecord
	team *TeamSessionIdentity
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
		eligible := false
		for _, protocol := range metadata[name.ModelID].Protocols {
			if protocol == entity.ProtocolOpenAIChat {
				eligible = true
			}
		}
		if !eligible {
			continue
		}
		plan, err := s.gatewayAttemptPlan(name.ModelID, entity.ProtocolOpenAIChat)
		if err != nil {
			continue
		}
		plan.team, plan.userID = identity, identity.UserID
		eligible = false
		for _, candidate := range plan.Candidates() {
			ok, err := s.gatewayAttemptEligible(ctx, plan, candidate.attempt)
			if err != nil {
				return nil, gatewayPublicAttemptError(err)
			}
			if ok {
				eligible = true
				break
			}
		}
		if !eligible {
			continue
		}
		output = append(output, GatewayModel{ID: name.Name, ModelID: name.ModelID, Object: "model", Created: auth.ModelCreated[name.ModelID].Unix(), OwnedBy: "routex", Protocols: []string{entity.ProtocolOpenAIChat}, InputCapabilities: map[string][]string{entity.ProtocolOpenAIChat: {}}, AttachmentScope: "team", PersonalAttachments: false})
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

func validateTeamChatText(payload map[string]json.RawMessage) error {
	unsupported := func() error {
		return gatewayError(400, "unsupported_input", "Team Session inference supports text input only.")
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(payload["messages"], &messages) != nil {
		return unsupported()
	}
	for _, message := range messages {
		if message["audio"] != nil {
			return unsupported()
		}
		raw := message["content"]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			continue
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(raw, &parts) != nil {
			return unsupported()
		}
		for _, part := range parts {
			var kind string
			if json.Unmarshal(part["type"], &kind) != nil || kind != "text" || json.Unmarshal(part["text"], &text) != nil {
				return unsupported()
			}
		}
	}
	if payload["audio"] != nil {
		return unsupported()
	}
	if raw := payload["modalities"]; raw != nil {
		var values []string
		if json.Unmarshal(raw, &values) != nil {
			return unsupported()
		}
		for _, value := range values {
			if value != "text" {
				return unsupported()
			}
		}
	}
	return nil
}
