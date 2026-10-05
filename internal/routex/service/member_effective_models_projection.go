package service

import (
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
)

func (s *Service) projectMemberEffectiveModels(data *memberEffectiveModelsData) *MemberEffectiveModelsPage {
	meta := data.Metadata
	result := &MemberEffectiveModelsPage{UserID: meta.Subject.ID, ObservedAt: time.Now().UTC(), SubjectStatus: "active", TeamEnrichment: "not_authorized", UnionCompleteness: "unknown", Items: []MemberEffectiveModelRow{}}
	if meta.Subject.Disabled {
		result.SubjectStatus = "disabled"
	}
	if meta.Subject.OffboardedAt != nil {
		result.SubjectStatus = "offboarded"
	}
	if data.TeamsRead {
		result.TeamEnrichment = "included"
		result.UnionCompleteness = "complete"
	}
	var auth *runtimeAuthorization
	var routes *runtimeRoutes
	if s.runtime != nil && s.runtime.publication.TryRLock() {
		defer s.runtime.publication.RUnlock()
		if s.runtime.mu.TryLock() {
			defer s.runtime.mu.Unlock()
			auth = s.runtime.auth.Load()
			routes = s.runtime.routes.Load()
		}
	}

	index := memberModelsIndex(meta)
	personal := map[string]bool{}
	for _, g := range meta.Grants {
		personal[g.ModelID] = true
	}
	providers := map[string]string{}
	for _, p := range meta.Providers {
		providers[p.ID] = p.Name
	}
	teamByID := map[string]memberEffectiveTeam{}
	teamModels := map[string][]string{}
	teamProofs := map[string]bool{}
	if data.TeamsRead {
		for _, team := range data.Teams {
			teamByID[team.ID] = team
			teamProofs[team.ID] = s.memberEffectiveTeamProof(data, team, auth, routes)
		}
		for _, grant := range data.TeamGrants {
			if _, exists := teamByID[grant.TeamID]; exists {
				teamModels[grant.ModelID] = append(teamModels[grant.ModelID], grant.TeamID)
			}
		}
		for id, teams := range teamModels {
			slices.Sort(teams)
			teamModels[id] = slices.Compact(teams)
		}
	}
	personalProof := s.memberEffectivePersonalProof(meta, auth, routes)
	for _, model := range meta.Models {
		row := MemberEffectiveModelRow{ID: model.ID, Name: index.names[model.ID].Name, Status: model.Status, CreatedAt: model.CreatedAt.UTC(), Protocols: []string{}, Sources: []MemberEffectiveModelSource{}, Availability: "unavailable"}
		protocols, eligible, routeState := s.memberModelProtocols(model, index, auth, routes)
		if model.Status == entity.ResourceActive && !memberEffectiveModelCipherProof(model.ID, data.CipherHashes, routes) {
			protocols = []string{}
			eligible = nil
			routeState = "unknown"
		}
		add := func(kind string, team *memberEffectiveTeam, proven bool) {
			source := MemberEffectiveModelSource{Kind: kind, Protocols: []string{}, Availability: routeState}
			if team != nil {
				id, name := team.ID, team.Name
				source.TeamID = &id
				source.TeamName = &name
			}
			switch {
			case result.SubjectStatus != "active" || model.Status != entity.ResourceActive:
				source.Availability = "unavailable"
			case !proven:
				source.Availability = "unknown"
			case routeState == "ready":
				source.Protocols = slices.Clone(protocols)
			}
			row.Sources = append(row.Sources, source)
			if source.Availability == "ready" {
				row.Availability = "ready"
				row.Protocols = append(row.Protocols, source.Protocols...)
			} else if source.Availability == "unknown" && row.Availability != "ready" {
				row.Availability = "unknown"
			}
		}
		if personal[model.ID] {
			add("personal", nil, personalProof)
		}
		for _, teamID := range teamModels[model.ID] {
			team := teamByID[teamID]
			add("team", &team, teamProofs[teamID])
		}

		if len(row.Sources) == 0 {
			continue
		}
		slices.Sort(row.Protocols)
		row.Protocols = slices.Compact(row.Protocols)
		if meta.ProvidersRead {
			row.Providers = []string{}
			for _, b := range index.bindings[model.ID] {
				if b.Weight > 0 {
					if name, ok := providers[index.connections[index.models[b.ProviderModelID].ConnectionID].ProviderID]; ok {
						row.Providers = append(row.Providers, name)
					}
				}
			}
			slices.Sort(row.Providers)
			row.Providers = slices.Compact(row.Providers)
		}
		if row.Availability != "ready" {
			eligible = nil
		}
		row.InputPrice = memberModelsPrice(pricing.Input, meta.PricesRead, eligible, meta)
		row.OutputPrice = memberModelsPrice(pricing.Output, meta.PricesRead, eligible, meta)
		result.Items = append(result.Items, row)
	}
	// Repeat source/lease checks at the return boundary. A late reduction may
	// hide readiness without removing the independently authorized SQL facts.
	personalProof = s.memberEffectivePersonalProof(meta, auth, routes)
	for id, team := range teamByID {
		teamProofs[id] = s.memberEffectiveTeamProof(data, team, auth, routes)
	}
	for i := range result.Items {
		row := &result.Items[i]
		if result.SubjectStatus != "active" || row.Status != entity.ResourceActive {
			continue
		}
		row.Availability = "unavailable"
		row.Protocols = []string{}
		for j := range row.Sources {
			source := &row.Sources[j]
			proof := personalProof
			if source.Kind == "team" {
				proof = teamProofs[*source.TeamID]
			}
			if !proof || !memberEffectiveModelCipherProof(row.ID, data.CipherHashes, routes) || s.runtime != nil && runtimeDenied(&s.runtime.deniedModels, row.ID) {
				source.Availability = "unknown"
				source.Protocols = []string{}
			}
			if source.Availability == "ready" {
				row.Availability = "ready"
				row.Protocols = append(row.Protocols, source.Protocols...)
			} else if source.Availability == "unknown" && row.Availability != "ready" {
				row.Availability = "unknown"
			}
		}
		slices.Sort(row.Protocols)
		row.Protocols = slices.Compact(row.Protocols)
		if row.Availability != "ready" {
			row.InputPrice = memberModelsPrice(pricing.Input, meta.PricesRead, nil, meta)
			row.OutputPrice = memberModelsPrice(pricing.Output, meta.PricesRead, nil, meta)
		}
	}
	slices.SortFunc(result.Items, func(a, b MemberEffectiveModelRow) int { return strings.Compare(a.ID, b.ID) })
	return result
}
