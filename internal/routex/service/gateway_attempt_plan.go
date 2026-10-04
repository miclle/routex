package service

import (
	"context"

	"github.com/miclle/routex/pkg/routeattempt"
)

type gatewayAttemptCandidate struct {
	attempt    routeattempt.Attempt
	route      gatewayRoute
	credential string
	priority   int
}

type gatewayAttemptPlan struct {
	personalGrantRevision string
	Plan                  *routeattempt.Plan
	modelID               string
	protocol              string
	snapshotID            string
	userID                string
	projectID             string
	keyID                 string
	team                  *TeamSessionIdentity
	candidates            []gatewayAttemptCandidate
	draw                  func(int) (int, error)
	allowed               map[string]bool
}

func gatewayAttemptKey(attempt routeattempt.Attempt) string {
	return attempt.TargetID + "\x00" + attempt.CredentialID
}

func (s *Service) gatewayAttemptPlan(modelID, protocol string) (*gatewayAttemptPlan, error) {
	return s.gatewayAttemptPlanWithDraw(modelID, protocol, nil)
}

func (s *Service) gatewayAttemptPlanWithDraw(modelID, protocol string, draw func(int) (int, error)) (*gatewayAttemptPlan, error) {
	runtime := s.runtime
	if runtime == nil {
		return nil, runtimeUnavailable
	}
	now := s.gatewayAttemptClock()
	auth, routes := runtime.auth.Load(), runtime.routes.Load()
	if auth == nil || !now.Before(auth.ValidUntil) || routes == nil || !auth.Models[modelID] || runtimeDenied(&runtime.deniedModels, modelID) {
		return nil, runtimeUnavailable
	}
	targets := []routeattempt.Target{}
	detached := []gatewayAttemptCandidate{}
	for _, candidate := range routes.Models[modelID] {
		if candidate.Route.Protocol != protocol || candidate.Route.Weight == 0 {
			continue
		}
		credentials := make([]routeattempt.Credential, 0, len(candidate.Credentials))
		for _, credential := range candidate.Credentials {
			credentials = append(credentials, routeattempt.Credential{ID: credential.ID, Priority: credential.Priority})
			route := candidate.Route
			route.PriceBasis = clonePriceBasis(candidate.Route.PriceBasis)
			route.CredentialID = credential.ID
			route.SnapshotID = routes.ID
			detached = append(detached, gatewayAttemptCandidate{
				attempt:    routeattempt.Attempt{TargetID: route.BindingID, ConnectionID: route.ConnectionID, CredentialID: credential.ID, Protocol: protocol},
				route:      route,
				credential: credential.Plaintext,
				priority:   credential.Priority,
			})
		}
		targets = append(targets, routeattempt.Target{ID: candidate.Route.BindingID, ConnectionID: candidate.Route.ConnectionID, Protocol: protocol, Weight: candidate.Route.Weight, Credentials: credentials})
	}
	if len(detached) == 0 {
		return nil, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
	}
	plan, err := routeattempt.New(protocol, targets, routeattempt.Options{MaxAttempts: routeattempt.MaxAttempts, Draw: draw})
	if err != nil {
		return nil, runtimeUnavailable
	}
	return &gatewayAttemptPlan{Plan: plan, modelID: modelID, protocol: protocol, snapshotID: routes.ID, candidates: detached, draw: draw}, nil
}

func (p *gatewayAttemptPlan) Candidates() []gatewayAttemptCandidate {
	if p == nil {
		return nil
	}
	result := append([]gatewayAttemptCandidate(nil), p.candidates...)
	for index := range result {
		result[index].route.PriceBasis = clonePriceBasis(result[index].route.PriceBasis)
	}
	return result
}

func (p *gatewayAttemptPlan) Candidate(attempt routeattempt.Attempt) (*gatewayRoute, string, bool) {
	if p == nil || attempt.Protocol != p.protocol {
		return nil, "", false
	}
	key := gatewayAttemptKey(attempt)
	for _, candidate := range p.candidates {
		if gatewayAttemptKey(candidate.attempt) == key && candidate.attempt.ConnectionID == attempt.ConnectionID {
			route := candidate.route
			route.PriceBasis = clonePriceBasis(candidate.route.PriceBasis)
			return &route, candidate.credential, true
		}
	}
	return nil, "", false
}

func (s *Service) gatewayAttemptEligible(ctx context.Context, plan *gatewayAttemptPlan, attempt routeattempt.Attempt) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if plan == nil || s.runtime == nil {
		return false, nil
	}
	route, _, exists := plan.Candidate(attempt)
	if !exists {
		return false, nil
	}
	if plan.allowed != nil && !plan.allowed[gatewayAttemptKey(attempt)] {
		return false, nil
	}
	now := s.gatewayAttemptClock()
	auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if auth == nil || !now.Before(auth.ValidUntil) || routes == nil {
		return false, runtimeUnavailable
	}
	if plan.team != nil {
		if err := s.ReauthorizeTeamSession(ctx, plan.team, plan.modelID); err != nil {
			return false, err
		}
	}
	if plan.keyID != "" {
		key, exists := auth.KeysByID[plan.keyID]
		if !exists || key.Key.UserID != plan.userID || key.ProjectID != plan.projectID || runtimeDenied(&s.runtime.deniedKeys, plan.keyID) || key.Key.ExpiresAt != nil && !now.Before(*key.Key.ExpiresAt) {
			return false, nil
		}
		if plan.projectID != "" {
			if runtimeDenied(&s.runtime.deniedProjects, plan.projectID) {
				return false, nil
			}
		} else if runtimeDenied(&s.runtime.deniedUsers, plan.userID) || !s.personalPreparedGrantAllowed(auth, plan.userID, plan.keyID, plan.modelID, plan.personalGrantRevision) {
			return false, nil
		}
		granted := false
		for _, modelID := range key.Models {
			if modelID == plan.modelID {
				granted = true
				break
			}
		}
		if !granted {
			return false, nil
		}
	}
	if routes.ID != plan.snapshotID || route.SnapshotID != plan.snapshotID || route.Protocol != plan.protocol || route.BindingID != attempt.TargetID || route.ConnectionID != attempt.ConnectionID || route.CredentialID != attempt.CredentialID {
		return false, nil
	}
	if !auth.Models[plan.modelID] || runtimeDenied(&s.runtime.deniedModels, plan.modelID) ||
		!auth.ProviderModels[route.ProviderModelID] || runtimeDenied(&s.runtime.deniedProviderModels, route.ProviderModelID) ||
		!auth.Credentials[attempt.CredentialID] || runtimeDenied(&s.runtime.deniedCredentials, attempt.CredentialID) ||
		!auth.CredentialAccess[attempt.CredentialID][route.ProviderModelID] {
		return false, nil
	}
	if route.EgressRevision == "" || route.EgressGeneration != s.egressGeneration.Load() || auth.ConnectionRevisions[route.ConnectionID] != route.EgressRevision {
		return false, nil
	}
	return s.gatewayAttemptHealthy(route.ConnectionID, attempt.CredentialID), nil
}

func (p *gatewayAttemptPlan) filtered(candidates []gatewayAttemptCandidate) (*gatewayAttemptPlan, error) {
	targetsByID := map[string]*routeattempt.Target{}
	ordered := []string{}
	for _, candidate := range candidates {
		target := targetsByID[candidate.attempt.TargetID]
		if target == nil {
			target = &routeattempt.Target{ID: candidate.attempt.TargetID, ConnectionID: candidate.attempt.ConnectionID, Protocol: candidate.attempt.Protocol, Weight: candidate.route.Weight}
			targetsByID[target.ID] = target
			ordered = append(ordered, target.ID)
		}
		target.Credentials = append(target.Credentials, routeattempt.Credential{ID: candidate.attempt.CredentialID, Priority: candidate.priority})
	}
	targets := make([]routeattempt.Target, 0, len(ordered))
	for _, id := range ordered {
		targets = append(targets, *targetsByID[id])
	}
	plan, err := routeattempt.New(p.protocol, targets, routeattempt.Options{MaxAttempts: routeattempt.MaxAttempts, Draw: p.draw})
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		allowed[gatewayAttemptKey(candidate.attempt)] = true
	}
	return &gatewayAttemptPlan{personalGrantRevision: p.personalGrantRevision, Plan: plan, modelID: p.modelID, protocol: p.protocol, snapshotID: p.snapshotID, userID: p.userID, projectID: p.projectID, keyID: p.keyID, team: p.team, candidates: append([]gatewayAttemptCandidate(nil), candidates...), draw: p.draw, allowed: allowed}, nil
}
