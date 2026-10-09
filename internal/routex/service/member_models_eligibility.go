package service

import (
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type memberModelsData struct {
	EgressSetting                      entity.EgressSetting
	Egresses                           []entity.Egress
	Subject                            entity.User
	Applications                       map[string]entity.RegistrationApprovalApplication
	Grants                             []entity.UserModelGrant
	Models                             []entity.Model
	Names                              []entity.ModelName
	Bindings                           []entity.ModelProviderBinding
	ProviderModels                     []entity.ProviderModel
	Connections                        []entity.ProviderConnection
	Credentials                        []entity.ProviderCredential
	Access                             []entity.CredentialModelAccess
	Providers                          []entity.Provider
	Prices                             []entity.ModelPrice
	Rates                              []entity.PriceRate
	TeamModels                         map[string]bool
	TeamBasis                          []memberModelsTeamBasis
	CanEdit, ProvidersRead, PricesRead bool
}
type memberModelsTeamBasis struct{ TeamID, MembershipID, ModelID string }
type memberModelsEligibilityIndex struct {
	egressSetting entity.EgressSetting
	egresses      []entity.Egress
	names         map[string]entity.ModelName
	providers     map[string]runtimeProviderProof
	bindings      map[string][]entity.ModelProviderBinding
	models        map[string]entity.ProviderModel
	connections   map[string]entity.ProviderConnection
	credentials   map[string][]entity.ProviderCredential
	access        map[string]map[string]bool
}

func memberModelsIndex(data *memberModelsData) memberModelsEligibilityIndex {
	index := memberModelsEligibilityIndex{egressSetting: data.EgressSetting, egresses: data.Egresses, names: map[string]entity.ModelName{}, providers: runtimeProviderProofs(&runtimeData{Providers: data.Providers}), bindings: map[string][]entity.ModelProviderBinding{}, models: map[string]entity.ProviderModel{}, connections: map[string]entity.ProviderConnection{}, credentials: map[string][]entity.ProviderCredential{}, access: map[string]map[string]bool{}}
	for _, n := range data.Names {
		if n.CurrentModelID != nil && *n.CurrentModelID == n.ModelID {
			index.names[n.ModelID] = n
		}
	}
	for _, b := range data.Bindings {
		index.bindings[b.ModelID] = append(index.bindings[b.ModelID], b)
	}
	for _, p := range data.ProviderModels {
		index.models[p.ID] = p
	}
	for _, c := range data.Connections {
		index.connections[c.ID] = c
	}
	for _, c := range data.Credentials {
		index.credentials[c.ConnectionID] = append(index.credentials[c.ConnectionID], c)
	}
	for _, a := range data.Access {
		if index.access[a.CredentialID] == nil {
			index.access[a.CredentialID] = map[string]bool{}
		}
		index.access[a.CredentialID][a.ProviderModelID] = true
	}
	return index
}

type memberModelsCredentialProof struct {
	ID, Revision string
	Access       bool
}
type memberModelsRouteProof struct {
	Binding     entity.ModelProviderBinding
	Model       entity.ProviderModel
	Connection  entity.ProviderConnection
	Provider    runtimeProviderProof
	EgressProof string
	Credentials []memberModelsCredentialProof
}

func (index memberModelsEligibilityIndex) hash(model entity.Model) string {
	rows := []memberModelsRouteProof{}
	for _, b := range index.bindings[model.ID] {
		p, exists := index.models[b.ProviderModelID]
		if !exists {
			return ""
		}
		c, exists := index.connections[p.ConnectionID]
		if !exists {
			return ""
		}
		provider, exists := index.providers[c.ProviderID]
		if !exists {
			return ""
		}
		proofs := []memberModelsCredentialProof{}
		for _, credential := range index.credentials[c.ID] {
			proofs = append(proofs, memberModelsCredentialProof{credential.ID, credentialRuntimeRevision(credential), index.access[credential.ID][p.ID]})
		}
		slices.SortFunc(proofs, func(a, b memberModelsCredentialProof) int { return strings.Compare(a.ID, b.ID) })
		b.CreatedAt = b.CreatedAt.UTC()
		p.CreatedAt = p.CreatedAt.UTC()
		c.CreatedAt = c.CreatedAt.UTC()
		rows = append(rows, memberModelsRouteProof{Binding: b, Model: p, Connection: c, Provider: provider, Credentials: proofs, EgressProof: index.egressProof(c)})
	}
	slices.SortFunc(rows, func(a, b memberModelsRouteProof) int { return strings.Compare(a.Binding.ID, b.Binding.ID) })
	name, exists := index.names[model.ID]
	if !exists {
		return ""
	}
	model.CreatedAt = model.CreatedAt.UTC()
	name.CreatedAt = name.CreatedAt.UTC()
	return memberModelsDigest(struct {
		Model  entity.Model
		Name   entity.ModelName
		Routes []memberModelsRouteProof
	}{model, name, rows})
}
func runtimeMemberModelsEligibility(data *runtimeData) map[string]string {
	index := memberModelsIndex(&memberModelsData{EgressSetting: data.EgressSetting, Egresses: data.Egresses, Names: data.Names, Providers: data.Providers, Bindings: data.Bindings, ProviderModels: data.ProviderModels, Connections: data.Connections, Credentials: data.Credentials, Access: deploymentCoverageProjection(data.Credentials, data.Connections, data.ProviderModels, data.Access, data.Attestations)})
	result := map[string]string{}
	for _, m := range data.Models {
		result[m.ID] = index.hash(m)
	}
	return result
}
func (index memberModelsEligibilityIndex) covered(pm entity.ProviderModel) bool {
	enabled := 0
	for _, c := range index.credentials[pm.ConnectionID] {
		if c.Enabled && c.VerificationStatus == "verified" {
			enabled++
			if !index.access[c.ID][pm.ID] {
				return false
			}
		}
	}
	return enabled > 0
}

// Descriptor metadata is sufficient to compare configured egress generation;
// no proxy secret, diagnostic or unauthorized directory is projected.
func (index memberModelsEligibilityIndex) egressProof(connection entity.ProviderConnection) string {
	selected := connection.EgressID
	defaultETag := ""
	switch connection.EgressMode {
	case "", "default":
		selected = index.egressSetting.DefaultEgressID
		defaultETag = index.egressSetting.ETag
	case "direct":
		if selected != nil {
			return ""
		}
	case "proxy":
		if selected == nil {
			return ""
		}
	default:
		return ""
	}
	type descriptor struct {
		ID, ETag, SecretGeneration, Kind, Host string
		Port                                   int
		Enabled                                bool
		CreatedAt                              time.Time
	}
	var proof *descriptor
	if selected != nil {
		for _, e := range index.egresses {
			if e.ID == *selected {
				proof = &descriptor{e.ID, e.ETag, e.SecretGeneration, e.Kind, e.Host, e.Port, e.Enabled, e.CreatedAt.UTC()}
				break
			}
		}
		if proof == nil {
			return ""
		}
	}
	return memberModelsDigest(struct {
		Selected    *string
		DefaultETag string
		Descriptor  *descriptor
	}{selected, defaultETag, proof})
}
