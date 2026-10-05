package service

import (
	"context"

	"github.com/miclle/routex/internal/routex/entity"
)

// Caller holds publication admission while capturing SQL subject and this proof.
// This proves only the published account lifecycle gate, not native eligibility.
func (s *Service) memberStateRuntimeApplied(ctx context.Context, subject entity.User) bool {
	rt := s.runtime
	if rt == nil || rt.done == nil || ctx.Err() != nil || subject.CreatedAt.IsZero() {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
	}
	auth := rt.auth.Load()
	if auth == nil || !s.gatewayAttemptClock().Before(auth.ValidUntil) {
		return false
	}
	proof, ok := auth.UserProofs[subject.ID]
	if !ok || !proof.CreatedAt.Equal(subject.CreatedAt) || proof.Enabled != (!subject.Disabled && subject.OffboardedAt == nil) || runtimeDenied(&rt.deniedUsers, subject.ID) || runtimeDenied(&rt.deniedSessionUsers, subject.ID) {
		return false
	}
	now := s.gatewayAttemptClock()
	if s.runtime != rt || rt.auth.Load() != auth || !now.Before(auth.ValidUntil) || ctx.Err() != nil || runtimeDenied(&rt.deniedUsers, subject.ID) || runtimeDenied(&rt.deniedSessionUsers, subject.ID) {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
		return true
	}
}
