package service

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type runtimeArchiveRoutingState struct {
	Providers      []entity.Provider
	Connections    []entity.ProviderConnection
	Models         []entity.Model
	ProviderModels []entity.ProviderModel
	Bindings       []entity.ModelProviderBinding
	Egresses       []entity.Egress
	Setting        entity.EgressSetting
}
type runtimeArchiveRollbackState struct {
	Header             entity.RuntimeConfigurationArchive
	Current            runtimeArchiveRoutingState
	Desired            runtimeArchiveRoutingState
	Preview            *RuntimeArchiveRollbackPreview
	TransportChanged   map[string]bool
	DeclarationChanged map[string]bool
	WeightModels       map[string]bool
	DesiredDigest      string
}

func runtimeArchiveConnectionFields(c entity.ProviderConnection) RuntimeArchiveConnectionFields {
	return RuntimeArchiveConnectionFields{c.BaseURL, c.Protocol, entity.ConnectionAdapter(c), c.APIVersion, c.EgressMode, c.EgressID}
}
func runtimeArchiveDeclarations(pm entity.ProviderModel) RuntimeArchiveDeclarationFields {
	return RuntimeArchiveDeclarationFields{pm.SupportsImageInput, pm.SupportsPDFInput}
}

// This digest contains only allowed fields, stable topology and current lifecycle
// flags. It cannot resurrect an authorization, price, secret or verification.
func runtimeArchiveRoutingDigest(st runtimeArchiveRoutingState) string {
	rows := [][]any{}
	for _, p := range st.Providers {
		rows = append(rows, []any{"provider", p.ID, p.CreatedAt.UTC(), p.Enabled})
	}
	for _, c := range st.Connections {
		rows = append(rows, []any{"connection", c.ID, c.ProviderID, c.CreatedAt.UTC(), c.Enabled, runtimeArchiveConnectionFields(c)})
	}
	for _, p := range st.ProviderModels {
		rows = append(rows, []any{"provider_model", p.ID, p.ConnectionID, p.UpstreamName, p.CreatedAt.UTC(), p.Disabled, runtimeArchiveDeclarations(p)})
	}
	for _, m := range st.Models {
		rows = append(rows, []any{"model", m.ID, m.CreatedAt.UTC(), m.Status})
	}
	for _, b := range st.Bindings {
		rows = append(rows, []any{"binding", b.ID, b.ModelID, b.ProviderModelID, b.CreatedAt.UTC(), b.Weight})
	}
	return connectionMetadataHash(rows)
}

// Archive decoding has a wider whole-source bound. The routing review applies
// its own relation bound before any cross-relation planning work.
func runtimeArchiveRollbackSourceWithinBound(source runtimeConfigurationSourceV1) bool {
	for _, count := range []int{len(source.Providers), len(source.Connections), len(source.ProviderModels), len(source.Models), len(source.Bindings), len(source.Egresses)} {
		if count > runtimeArchiveRollbackBound {
			return false
		}
	}
	return true
}

func loadRuntimeArchiveRoutingState(tx *gorm.DB) (runtimeArchiveRoutingState, error) {
	var st runtimeArchiveRoutingState
	// A single bounded batch per relation, with a deterministic lock order. No
	// per-row catalog/readiness queries or whole authorization-source allocation.
	for _, target := range []any{&st.Providers, &st.Connections, &st.ProviderModels, &st.Models, &st.Bindings, &st.Egresses} {
		if err := tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Limit(runtimeArchiveRollbackBound + 1).Find(target).Error; err != nil {
			return st, err
		}
	}
	for _, count := range []int{len(st.Providers), len(st.Connections), len(st.ProviderModels), len(st.Models), len(st.Bindings), len(st.Egresses)} {
		if count > runtimeArchiveRollbackBound {
			return st, runtimeArchiveRollbackUnavailable
		}
	}
	if err := tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).Take(&st.Setting).Error; err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) runtimeArchiveRollbackActor(tx *gorm.DB, auth *Authentication, write bool) (entity.User, error) {
	if err := lockGovernance(tx); err != nil {
		return entity.User{}, err
	}
	actor, err := s.runtimePublicationActor(tx, auth)
	if err != nil {
		return actor, err
	}
	perms := []string{"system.read", "providers.read", "models.read_all"}
	if write {
		perms = append(perms, "system.write", "providers.write", "models.write")
	}
	for _, permission := range perms {
		allowed, e := exactGovernancePermissionForAdmittedActor(tx, actor, permission)
		if e != nil {
			return actor, e
		}
		if !allowed {
			return actor, apperrors.ErrForbidden
		}
	}
	return actor, tx.Statement.Context.Err()
}

func (s *Service) runtimeArchiveRollbackState(ctx context.Context, tx *gorm.DB, actor entity.User, archiveID string) (*runtimeArchiveRollbackState, error) {
	// This order is also used by cleanup and root-envelope rewrap. An original
	// rollback command creates its protected archive reference while holding it.
	if _, err := lockedSecretPolicy(tx); err != nil {
		return nil, err
	}
	if _, err := lockRuntimeArchiveRetention(tx); err != nil {
		return nil, err
	}
	var header entity.RuntimeConfigurationArchive
	if err := personalExact(tx, "id", archiveID).Take(&header).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.ErrNotFound
		}
		return nil, err
	}
	if header.ID != archiveID {
		return nil, errRuntimeArchive
	}
	facts, err := s.readRuntimeArchiveTx(ctx, tx, header)
	if err != nil {
		return nil, err
	}
	if !runtimeArchiveRollbackSourceWithinBound(facts.Document.Source) {
		return nil, runtimeArchiveRollbackUnavailable
	}
	current, err := loadRuntimeArchiveRoutingState(tx)
	if err != nil {
		return nil, err
	}
	supplies, err := routingSupplyStates(tx, current.Connections, current.ProviderModels)
	if err != nil {
		return nil, err
	}
	writeAllowed := true
	for _, permission := range []string{"system.write", "providers.write", "models.write"} {
		allowed, e := exactGovernancePermissionForAdmittedActor(tx, actor, permission)
		if e != nil {
			return nil, e
		}
		writeAllowed = writeAllowed && allowed
	}
	st := s.planRuntimeArchiveRollback(actor, header, current, facts.Document.Source, supplies, writeAllowed)
	return st, ctx.Err()
}

func (s *Service) planRuntimeArchiveRollback(actor entity.User, header entity.RuntimeConfigurationArchive, current runtimeArchiveRoutingState, source runtimeConfigurationSourceV1, supplies map[string]routingSupplyState, writeAllowed bool) *runtimeArchiveRollbackState {
	st := &runtimeArchiveRollbackState{Header: header, Current: current, TransportChanged: map[string]bool{}, DeclarationChanged: map[string]bool{}, WeightModels: map[string]bool{}}
	st.Desired = current
	st.Desired.Connections = slices.Clone(current.Connections)
	st.Desired.ProviderModels = slices.Clone(current.ProviderModels)
	st.Desired.Bindings = slices.Clone(current.Bindings)
	p := &RuntimeArchiveRollbackPreview{Scope: "gateway_routing_configuration", ArchiveID: header.ID, ArchiveCreatedAt: header.CreatedAt.UTC(), ObservedAt: time.Now().UTC(), BlockerCodes: []string{}, Connections: []RuntimeArchiveConnectionChange{}, Declarations: []RuntimeArchiveDeclarationChange{}, Weights: []RuntimeArchiveWeightChange{}}
	st.Preview = p
	block := func(code string) {
		if !slices.Contains(p.BlockerCodes, code) {
			p.BlockerCodes = append(p.BlockerCodes, code)
		}
	}
	// Frozen decoder has already validated exact references inside the archive.
	// Current topology must match every identity AND birth; new/deleted/reborn or
	// rebound resources cannot be silently adopted by a historical configuration.
	if len(source.Providers) != len(current.Providers) || len(source.Connections) != len(current.Connections) || len(source.ProviderModels) != len(current.ProviderModels) || len(source.Models) != len(current.Models) || len(source.Bindings) != len(current.Bindings) {
		block("topology_changed")
	}
	for _, a := range source.Providers {
		i := slices.IndexFunc(current.Providers, func(p entity.Provider) bool { return p.ID == a.ID })
		if i < 0 || !current.Providers[i].CreatedAt.Equal(a.CreatedAt) {
			block("topology_changed")
		}
	}
	for _, a := range source.Models {
		i := slices.IndexFunc(current.Models, func(m entity.Model) bool { return m.ID == a.ID })
		if i < 0 || !current.Models[i].CreatedAt.Equal(a.CreatedAt) {
			block("topology_changed")
		}
	}
	for _, a := range source.Connections {
		i := slices.IndexFunc(current.Connections, func(c entity.ProviderConnection) bool { return c.ID == a.ID })
		if i < 0 || current.Connections[i].ProviderID != a.ProviderID || !current.Connections[i].CreatedAt.Equal(a.CreatedAt) {
			block("topology_changed")
			continue
		}
		before := current.Connections[i]
		// Protocol belongs to current topology, including empty disabled
		// Connections. An archive cannot change it through transport restoration.
		if a.Protocol != before.Protocol {
			block("transport_topology_locked")
			continue
		}
		after := before
		after.BaseURL, after.Adapter, after.APIVersion, after.EgressMode, after.EgressID = a.BaseURL, a.Adapter, a.APIVersion, a.EgressMode, a.EgressID
		canonical, e := s.canonicalConnectionTransport(ConnectionTransportInput{BaseURL: after.BaseURL, Protocol: after.Protocol, Adapter: after.Adapter, APIVersion: after.APIVersion})
		if e != nil || canonical.BaseURL != after.BaseURL || canonical.Adapter != entity.ConnectionAdapter(after) || !reflect.DeepEqual(canonical.APIVersion, after.APIVersion) {
			block("transport_policy_changed")
		}
		// Existing discovered Connections also lock adapter. Empty ones retain
		// the ordinary adapter validation for their authoritative current protocol.
		discovered := slices.ContainsFunc(current.ProviderModels, func(pm entity.ProviderModel) bool { return pm.ConnectionID == before.ID })
		if discovered && entity.ConnectionAdapter(after) != entity.ConnectionAdapter(before) {
			block("transport_topology_locked")
		}
		_, _, ready := runtimeEgressSelection(&runtimeData{Egresses: current.Egresses, EgressSetting: current.Setting}, after)
		if !ready {
			block("egress_unavailable")
		}
		selected := after.EgressID
		if after.EgressMode == "default" || after.EgressMode == "" {
			selected = current.Setting.DefaultEgressID
		}
		if selected != nil {
			old := slices.IndexFunc(source.Egresses, func(e runtimeConfigurationEgressV1) bool { return e.ID == *selected })
			now := slices.IndexFunc(current.Egresses, func(e entity.Egress) bool { return e.ID == *selected })
			if old < 0 || now < 0 || !source.Egresses[old].CreatedAt.Equal(current.Egresses[now].CreatedAt) {
				block("egress_topology_changed")
			}
		}
		changed := !reflect.DeepEqual(runtimeArchiveConnectionFields(before), runtimeArchiveConnectionFields(after))
		if changed {
			st.TransportChanged[before.ID] = true
			if before.Enabled {
				block("changed_transport_enabled")
			}
			p.Connections = append(p.Connections, RuntimeArchiveConnectionChange{before.ID, before.Name, before.Enabled, runtimeArchiveConnectionFields(before), runtimeArchiveConnectionFields(after), true})
		}
		st.Desired.Connections[i] = after
	}
	for _, a := range source.ProviderModels {
		i := slices.IndexFunc(current.ProviderModels, func(pm entity.ProviderModel) bool { return pm.ID == a.ID })
		if i < 0 || current.ProviderModels[i].ConnectionID != a.ConnectionID || current.ProviderModels[i].UpstreamName != a.UpstreamName || !current.ProviderModels[i].CreatedAt.Equal(a.CreatedAt) {
			block("topology_changed")
			continue
		}
		before := current.ProviderModels[i]
		after := before
		after.SupportsImageInput, after.SupportsPDFInput = a.SupportsImageInput, a.SupportsPDFInput
		if runtimeArchiveDeclarations(before) != runtimeArchiveDeclarations(after) {
			st.DeclarationChanged[before.ID] = true
			p.Declarations = append(p.Declarations, RuntimeArchiveDeclarationChange{before.ID, runtimeArchiveDeclarations(before), runtimeArchiveDeclarations(after)})
		}
		st.Desired.ProviderModels[i] = after
	}
	for _, a := range source.Bindings {
		i := slices.IndexFunc(current.Bindings, func(b entity.ModelProviderBinding) bool { return b.ID == a.ID })
		if i < 0 || current.Bindings[i].ModelID != a.ModelID || current.Bindings[i].ProviderModelID != a.ProviderModelID || !current.Bindings[i].CreatedAt.Equal(a.CreatedAt) {
			block("topology_changed")
			continue
		}
		before := current.Bindings[i]
		st.Desired.Bindings[i].Weight = a.Weight
		if before.Weight != a.Weight {
			st.WeightModels[before.ModelID] = true
		}
	}
	for modelID := range st.WeightModels {
		mi := slices.IndexFunc(current.Models, func(m entity.Model) bool { return m.ID == modelID })
		if mi < 0 || current.Models[mi].Status != entity.ResourceActive {
			block("model_not_active")
		}
		totals := map[string]int{}
		for i, b := range st.Desired.Bindings {
			if b.ModelID != modelID {
				continue
			}
			pi := slices.IndexFunc(st.Desired.ProviderModels, func(pm entity.ProviderModel) bool { return pm.ID == b.ProviderModelID })
			if pi < 0 {
				block("topology_changed")
				continue
			}
			pm := st.Desired.ProviderModels[pi]
			ci := slices.IndexFunc(st.Desired.Connections, func(c entity.ProviderConnection) bool { return c.ID == pm.ConnectionID })
			if ci < 0 {
				block("topology_changed")
				continue
			}
			c := st.Desired.Connections[ci]
			if b.Weight < 0 || b.Weight > 100 {
				block("invalid_weights")
			}
			totals[c.Protocol] += b.Weight
			if b.Weight > 0 {
				if st.TransportChanged[c.ID] {
					if b.Weight != current.Bindings[i].Weight {
						block("changed_transport_weight_assignment")
					}
				} else {
					provider := slices.IndexFunc(current.Providers, func(p entity.Provider) bool { return p.ID == c.ProviderID })
					supply := supplies[pm.ID]
					_, _, egressReady := runtimeEgressSelection(&runtimeData{Egresses: current.Egresses, EgressSetting: current.Setting}, c)
					if provider < 0 || !current.Providers[provider].Enabled || !c.Enabled || pm.Disabled || !supply.Covered || !supply.SourceAvailable || !egressReady {
						block("positive_route_not_ready")
					}
				}
			}
			if b.Weight != current.Bindings[i].Weight {
				p.Weights = append(p.Weights, RuntimeArchiveWeightChange{b.ID, b.ModelID, b.ProviderModelID, c.Protocol, current.Bindings[i].Weight, b.Weight})
			}
		}
		for _, total := range totals {
			if total != 100 {
				block("invalid_weights")
			}
		}
	}
	// A terminal Model remains read-only, including declaration changes affecting
	// its bound routes. A disabled Connection's desired transport may be prepared.
	for _, b := range current.Bindings {
		m := slices.IndexFunc(current.Models, func(m entity.Model) bool { return m.ID == b.ModelID })
		pm := slices.IndexFunc(current.ProviderModels, func(pm entity.ProviderModel) bool { return pm.ID == b.ProviderModelID })
		if m >= 0 && pm >= 0 && current.Models[m].Status == entity.ResourceArchived && (st.TransportChanged[current.ProviderModels[pm].ConnectionID] || st.DeclarationChanged[b.ProviderModelID] || st.WeightModels[b.ModelID]) {
			block("model_archived")
		}
	}
	// Review proofs use only nonsecret current revisions and readiness booleans.
	revisions := [][]any{}
	for _, row := range current.Providers {
		revisions = append(revisions, []any{row.ID, row.ETag})
	}
	for _, row := range current.Connections {
		revisions = append(revisions, []any{row.ID, row.ETag, row.TransportGeneration})
	}
	for _, row := range current.ProviderModels {
		q := supplies[row.ID]
		revisions = append(revisions, []any{row.ID, row.ETag, row.CapabilityTransportGeneration, q.Covered, q.SourceAvailable})
	}
	for _, row := range current.Egresses {
		revisions = append(revisions, []any{row.ID, row.CreatedAt.UTC(), row.Enabled, row.ETag, row.SecretGeneration})
	}
	revisions = append(revisions, []any{current.Setting.ID, current.Setting.ETag, current.Setting.DefaultEgressID})
	if !writeAllowed {
		block("write_authority_required")
	}
	slices.Sort(p.BlockerCodes)
	slices.SortFunc(p.Weights, func(a, b RuntimeArchiveWeightChange) int {
		if a.BindingID < b.BindingID {
			return -1
		}
		if a.BindingID > b.BindingID {
			return 1
		}
		return 0
	})
	st.DesiredDigest = runtimeArchiveRoutingDigest(st.Desired)
	p.ETag = connectionMetadataHash([]any{"routex.routing-archive-rollback.v1", actor.ID, actor.CreatedAt.UTC(), header.ID, header.CreatedAt.UTC(), header.ContentDigest, runtimeArchiveRoutingDigest(current), st.DesiredDigest, revisions, p.BlockerCodes})
	p.CanApply = len(p.BlockerCodes) == 0
	return st
}
