package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
)

func memberModelsDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func memberModelsPrice(metric string, authorized bool, eligible []string, data *memberModelsData) MemberModelPriceCell {
	result := MemberModelPriceCell{State: "unauthorized"}
	if !authorized {
		return result
	}
	result.State = "unavailable"
	if len(eligible) == 0 {
		return result
	}
	var first *MemberModelBaseRate
	firstEnabled := false
	missing, present := false, false
	for _, pm := range eligible {
		schedules := []entity.ModelPrice{}
		for _, p := range data.Prices {
			if p.ProviderModelID == pm {
				schedules = append(schedules, p)
			}
		}
		if len(schedules) == 0 {
			missing = true
			continue
		}
		for _, p := range schedules {
			var chosen *entity.PriceRate
			for i := range data.Rates {
				r := &data.Rates[i]
				if r.ModelPriceID == p.ID && r.Metric == metric && r.Tier == pricing.Base {
					if chosen != nil {
						result.State = "heterogeneous"
						return result
					}
					chosen = r
				}
			}
			if chosen == nil {
				missing = true
				continue
			}
			amount, err := pricing.Decimal(chosen.Amount)
			if err != nil || !pricing.Currency(chosen.Currency) || chosen.Unit != pricing.Unit {
				result.State = "heterogeneous"
				return result
			}
			rate := MemberModelBaseRate{Amount: amount, Unit: pricing.Unit, Currency: chosen.Currency}
			if first != nil && (*first != rate || firstEnabled != chosen.Enabled) {
				result.State = "heterogeneous"
				return result
			}
			first = &rate
			firstEnabled = chosen.Enabled
			present = true
		}
	}
	if !present {
		result.State = "missing"
		return result
	}
	if missing {
		result.State = "heterogeneous"
		return result
	}
	result.Rate = first
	result.State = "disabled"
	if firstEnabled {
		result.State = "priced"
	}
	return result
}
func (s *Service) memberModelProtocols(model entity.Model, index memberModelsEligibilityIndex, auth *runtimeAuthorization, routes *runtimeRoutes) ([]string, []string, string) {
	protocols, eligible := []string{}, []string{}
	if model.Status != entity.ResourceActive {
		return protocols, eligible, "unavailable"
	}
	if s.runtime == nil || auth == nil || routes == nil || !time.Now().Before(auth.ValidUntil) || s.runtime.auth.Load() != auth || s.runtime.routes.Load() != routes || auth.SourceDigest == "" || auth.SourceDigest != routes.Digest {
		return protocols, eligible, "unknown"
	}
	hash := index.hash(model)
	if hash == "" || auth.ModelEligibilityHashes[model.ID] != hash || !auth.Models[model.ID] || !auth.ModelCreated[model.ID].Equal(model.CreatedAt) || runtimeDenied(&s.runtime.deniedModels, model.ID) {
		return protocols, eligible, "unknown"
	}
	totals := map[string]int{}
	for _, binding := range index.bindings[model.ID] {
		pm := index.models[binding.ProviderModelID]
		connection := index.connections[pm.ConnectionID]
		totals[connection.Protocol] += binding.Weight
	}
	for _, candidate := range routes.Models[model.ID] {
		route := candidate.Route
		pm, exists := index.models[route.ProviderModelID]
		if !exists || pm.Disabled || !index.covered(pm) || !entity.SupportedNativeProtocol(route.Protocol) || totals[route.Protocol] != 100 || !s.runtimeRouteReadyForDiscovery(auth, candidate) || route.EgressRevision == "" || route.EgressRevision != auth.ConnectionRevisions[route.ConnectionID] || route.EgressGeneration != s.egressGeneration.Load() {
			continue
		}
		protocols = append(protocols, route.Protocol)
		eligible = append(eligible, pm.ID)
	}
	slices.Sort(protocols)
	protocols = slices.Compact(protocols)
	slices.Sort(eligible)
	eligible = slices.Compact(eligible)
	if len(protocols) > 0 {
		return protocols, eligible, "ready"
	}
	return protocols, eligible, "unavailable"
}
func memberModelsETag(actorID string, data *memberModelsData, rows []MemberModelRow, index memberModelsEligibilityIndex) string {
	type reviewedModel struct {
		ID, Name, Status, Configuration string
		CreatedAt                       time.Time
	}
	models := make([]reviewedModel, 0, len(rows))
	for _, row := range rows {
		models = append(models, reviewedModel{row.ID, row.Name, row.Status, index.hash(entity.Model{ID: row.ID, Status: row.Status, CreatedAt: row.CreatedAt}), row.CreatedAt})
	}
	return memberModelsDigest(struct {
		ActorID, UserID, Role, Revision, GrantHash string
		CreatedAt                                  time.Time
		Disabled                                   bool
		OffboardedAt                               *time.Time
		CanEdit                                    bool
		TeamBasis                                  []memberModelsTeamBasis
		Models                                     []reviewedModel
	}{actorID, data.Subject.ID, data.Subject.Role, data.Subject.PersonalGrantRevision, personalGrantHash(data.Grants), data.Subject.CreatedAt.UTC(), data.Subject.Disabled, data.Subject.OffboardedAt, data.CanEdit, data.TeamBasis, models})
}
func (s *Service) projectMemberModels(actorID string, data *memberModelsData) *MemberModelsWorkspace {
	result := &MemberModelsWorkspace{UserID: data.Subject.ID, ObservedAt: time.Now().UTC(), CanEdit: data.CanEdit, PersonalModels: []MemberModelRow{}, AvailableModels: []MemberModelRow{}}
	index := memberModelsIndex(data)
	personal := map[string]bool{}
	for _, g := range data.Grants {
		personal[g.ModelID] = true
	}
	var auth *runtimeAuthorization
	var routes *runtimeRoutes
	if s.runtime != nil {
		if s.runtime.mu.TryLock() {
			defer s.runtime.mu.Unlock()
			auth = s.runtime.auth.Load()
			routes = s.runtime.routes.Load()
		} else {
			result.runtimeBusy = true
		}
	}
	providerNames := map[string]string{}
	for _, p := range data.Providers {
		providerNames[p.ID] = p.Name
	}
	reviewed := []MemberModelRow{}
	for _, model := range data.Models {
		if !data.CanEdit && !personal[model.ID] {
			continue
		}
		row := MemberModelRow{ID: model.ID, Name: index.names[model.ID].Name, Status: model.Status, Protocols: []string{}, CreatedAt: model.CreatedAt.UTC()}
		var eligible []string
		row.Protocols, eligible, row.Availability = s.memberModelProtocols(model, index, auth, routes)
		if data.ProvidersRead {
			row.Providers = []string{}
			for _, binding := range index.bindings[model.ID] {
				if binding.Weight > 0 {
					pm := index.models[binding.ProviderModelID]
					c := index.connections[pm.ConnectionID]
					if name, exists := providerNames[c.ProviderID]; exists {
						row.Providers = append(row.Providers, name)
					}
				}
			}
			slices.Sort(row.Providers)
			row.Providers = slices.Compact(row.Providers)
		}
		row.InputPrice = memberModelsPrice(pricing.Input, data.PricesRead, eligible, data)
		row.OutputPrice = memberModelsPrice(pricing.Output, data.PricesRead, eligible, data)
		row.Selectable = !personal[model.ID] && data.CanEdit && row.Availability == "ready"
		reviewed = append(reviewed, row)
		if personal[model.ID] {
			result.PersonalModels = append(result.PersonalModels, row)
		} else {
			result.AvailableModels = append(result.AvailableModels, row)
		}
	}
	slices.SortFunc(result.PersonalModels, func(a, b MemberModelRow) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(result.AvailableModels, func(a, b MemberModelRow) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(reviewed, func(a, b MemberModelRow) int { return strings.Compare(a.ID, b.ID) })
	result.ETag = memberModelsETag(actorID, data, reviewed, index)
	result.ApplicationStatus, result.RuntimeApplied = s.memberModelsApplication(data.Subject, data.Grants, auth, data.Applications)
	return result
}
