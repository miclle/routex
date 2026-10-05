package service

import (
	"context"
	"github.com/miclle/routex/internal/routex/entity"
)

func runtimeRegistrationAdmissions(data *runtimeData) map[string]runtimeAdmissionProof {
	apps := map[string]entity.RegistrationApprovalApplication{}
	for _, a := range data.ApprovalApplications {
		apps[a.ID] = a
	}
	r := map[string]runtimeAdmissionProof{}
	for _, u := range data.Users {
		_, p := registrationAdmission(u, apps)
		r[u.ID] = p
	}
	return r
}
func (s *Service) registrationAdmissionPublished(ctx context.Context, userID string, wanted runtimeAdmissionProof) bool {
	rt := s.runtime
	if rt == nil || rt.done == nil || ctx.Err() != nil || wanted.CreatedAt.IsZero() {
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
	p, ok := auth.UserAdmissions[userID]
	if !ok || !registrationAdmissionProofEqual(p, wanted) || runtimeDenied(&rt.deniedUsers, userID) || runtimeDenied(&rt.deniedSessionUsers, userID) {
		return false
	}
	if ctx.Err() != nil || s.runtime != rt || rt.auth.Load() != auth || !s.gatewayAttemptClock().Before(auth.ValidUntil) || runtimeDenied(&rt.deniedUsers, userID) || runtimeDenied(&rt.deniedSessionUsers, userID) {
		return false
	}
	select {
	case <-rt.done:
		return false
	default:
		return true
	}
}
func registrationAdmissionProofEqual(a, b runtimeAdmissionProof) bool {
	return a.CreatedAt.Equal(b.CreatedAt) && a.ApplicationID == b.ApplicationID && a.ApplicationCreatedAt.Equal(b.ApplicationCreatedAt) && a.Revision == b.Revision && a.State == b.State && a.Eligible == b.Eligible
}

// Read-only advisories follow the current leased Gateway snapshot, including a
// synchronous refresh after the background publisher stops. Approval mutation
// confirmation separately requires the active publisher above.
func (s *Service) registrationAdvisoryPublished(auth *runtimeAuthorization, subject entity.User, applications map[string]entity.RegistrationApprovalApplication) bool {
	rt := s.runtime
	_, wanted := registrationAdmission(subject, applications)
	if !wanted.Eligible || wanted.CreatedAt.IsZero() || rt == nil || auth == nil || rt.auth.Load() != auth || !s.gatewayAttemptClock().Before(auth.ValidUntil) {
		return false
	}
	published, exists := auth.UserAdmissions[subject.ID]
	if !exists || !registrationAdmissionProofEqual(published, wanted) || runtimeDenied(&rt.deniedUsers, subject.ID) || runtimeDenied(&rt.deniedSessionUsers, subject.ID) {
		return false
	}
	return s.gatewayAttemptClock().Before(auth.ValidUntil) && s.runtime == rt && rt.auth.Load() == auth && !runtimeDenied(&rt.deniedUsers, subject.ID) && !runtimeDenied(&rt.deniedSessionUsers, subject.ID)
}
