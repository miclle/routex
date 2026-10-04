package service

import (
	"context"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func sameRuntimeAliasName(a, b entity.ModelName) bool {
	if a.Name != b.Name || a.ModelID != b.ModelID || !a.CreatedAt.Equal(b.CreatedAt) || (a.CurrentModelID == nil) != (b.CurrentModelID == nil) || (a.ExpiresAt == nil) != (b.ExpiresAt == nil) {
		return false
	}
	return (a.CurrentModelID == nil || *a.CurrentModelID == *b.CurrentModelID) && (a.ExpiresAt == nil || a.ExpiresAt.Equal(*b.ExpiresAt))
}

// Configuration proof is separate from route readiness and alias callability.
// A missing or expired authorization snapshot cannot prove retirement.
func (s *Service) runtimeModelAliasApplied(subject modelAliasSubject) bool {
	if s.runtime == nil {
		return false
	}
	s.runtime.publication.RLock()
	defer s.runtime.publication.RUnlock()
	auth := s.runtime.auth.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) || runtimeDenied(&s.runtime.deniedModels, subject.Model.ID) || !auth.ModelCreated[subject.Model.ID].Equal(subject.Model.CreatedAt) {
		return false
	}
	active, exists := auth.Models[subject.Model.ID]
	if !exists || active != (subject.Model.Status == "active") {
		return false
	}
	current, currentExists := auth.Names[subject.Current.Name]
	selected, selectedExists := auth.Names[subject.Selected.Name]
	return currentExists && selectedExists && sameRuntimeAliasName(current, subject.Current) && sameRuntimeAliasName(selected, subject.Selected)
}

// A prepared route retains the requested public name. Model access alone does
// not keep a compatibility name callable after its published deadline changes.
func (s *Service) reauthorizeGatewayPublicName(ctx context.Context, result *GatewayResult) error {
	if result.identity.team != nil {
		if err := s.ReauthorizeTeamSession(ctx, result.identity.team, result.ModelID); err != nil {
			return err
		}
	}
	if s.runtime != nil {
		auth := s.runtime.auth.Load()
		if auth == nil || !time.Now().Before(auth.ValidUntil) {
			return gatewayPublicAttemptError(runtimeUnavailable)
		}
		if result.identity.key != nil && result.ProjectID == "" && !s.personalPreparedGrantAllowed(auth, result.UserID, result.KeyID, result.ModelID, result.identity.personalGrantRevision) {
			return gatewayError(403, "model_forbidden", "The requested model is unavailable.")
		}
		name, exists := s.runtimeModelName(result.ModelName)
		if !exists || name.Name != result.ModelName || name.ModelID != result.ModelID {
			return gatewayError(404, "model_not_found", "The requested model is unavailable.")
		}
	}
	return nil
}
