package service

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type modelWeightState struct {
	Model             entity.Model
	Rows              []ModelWeightRow
	Models            []entity.ProviderModel
	Connections       []entity.ProviderConnection
	Providers         []entity.Provider
	Supplies          map[string]routingSupplyState
	Egress            runtimeData
	EgressRevisions   map[string]string
	EgressReady       map[string]bool
	SourceDenialProof string
	Credentials       []entity.ProviderCredential
	Names             []entity.ModelName
}

func modelWeightBirth(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t.UTC()
	return &v
}
func modelWeightRetainedID(value, prefix string) bool {
	return strings.HasPrefix(value, prefix+"_") && safeTeamSessionID(value)
}

// Hydrate only the complete selected topology, in bounded batches. No grants,
// directory, prices or remote credential operations belong in this review.
func loadModelWeightState(tx *gorm.DB, modelID string, locked, eligibility bool) (*modelWeightState, error) {
	st := &modelWeightState{Rows: []ModelWeightRow{}, EgressRevisions: map[string]string{}, EgressReady: map[string]bool{}}
	q := personalExact(modelCreationDB(tx), "id", modelID)
	if locked {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&st.Model).Error; err != nil {
		return nil, err
	}
	if st.Model.ID != modelID {
		return nil, apperrors.ErrNotFound
	}
	var bindings []entity.ModelProviderBinding
	if err := personalExact(modelCreationDB(tx), "model_id", modelID).Order("id").Limit(modelWeightBindingBudget + 1).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) > modelWeightBindingBudget {
		return nil, modelWeightUnavailable
	}
	pmIDs := []string{}
	for _, b := range bindings {
		if b.ModelID != modelID || !modelWeightRetainedID(b.ID, "bnd") || !modelWeightRetainedID(b.ProviderModelID, "pmd") {
			return nil, modelWeightUnavailable
		}
		pmIDs = append(pmIDs, b.ProviderModelID)
	}
	slices.Sort(pmIDs)
	pmIDs = slices.Compact(pmIDs)
	if len(pmIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", pmIDs)).Order("id").Limit(1001).Find(&st.Models).Error; err != nil {
			return nil, err
		}
	}
	if len(st.Models) != len(pmIDs) {
		return nil, modelWeightUnavailable
	}
	pms := map[string]entity.ProviderModel{}
	connectionIDs := []string{}
	for _, pm := range st.Models {
		if !slices.Contains(pmIDs, pm.ID) || !modelWeightRetainedID(pm.ConnectionID, "con") {
			return nil, modelWeightUnavailable
		}
		pms[pm.ID] = pm
		connectionIDs = append(connectionIDs, pm.ConnectionID)
	}
	slices.Sort(connectionIDs)
	connectionIDs = slices.Compact(connectionIDs)
	if len(connectionIDs) > 0 {
		q = modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", connectionIDs)).Order("id").Limit(1001)
		if locked {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := q.Find(&st.Connections).Error; err != nil {
			return nil, err
		}
	}
	if len(st.Connections) != len(connectionIDs) {
		return nil, modelWeightUnavailable
	}
	cs := map[string]entity.ProviderConnection{}
	providerIDs := []string{}
	for _, c := range st.Connections {
		if !slices.Contains(connectionIDs, c.ID) || !modelWeightRetainedID(c.ProviderID, "prv") {
			return nil, modelWeightUnavailable
		}
		cs[c.ID] = c
		providerIDs = append(providerIDs, c.ProviderID)
	}
	slices.Sort(providerIDs)
	providerIDs = slices.Compact(providerIDs)
	if len(providerIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", providerIDs)).Order("id").Limit(1001).Find(&st.Providers).Error; err != nil {
			return nil, err
		}
	}
	if len(st.Providers) != len(providerIDs) {
		return nil, modelWeightUnavailable
	}
	ps := map[string]entity.Provider{}
	for _, p := range st.Providers {
		if !slices.Contains(providerIDs, p.ID) {
			return nil, modelWeightUnavailable
		}
		ps[p.ID] = p
	}
	for _, b := range bindings {
		pm, ok := pms[b.ProviderModelID]
		if !ok {
			return nil, modelWeightUnavailable
		}
		c, ok := cs[pm.ConnectionID]
		if !ok {
			return nil, modelWeightUnavailable
		}
		p, ok := ps[c.ProviderID]
		if !ok || !entity.SupportedNativeProtocol(c.Protocol) {
			return nil, modelWeightUnavailable
		}
		st.Rows = append(st.Rows, ModelWeightRow{b.ID, modelWeightBirth(b.CreatedAt), pm.ID, modelWeightBirth(pm.CreatedAt), c.ID, modelWeightBirth(c.CreatedAt), p.ID, modelWeightBirth(p.CreatedAt), c.Protocol, b.Weight})
	}
	slices.SortFunc(st.Rows, func(a, b ModelWeightRow) int { return strings.Compare(a.BindingID, b.BindingID) })
	if !eligibility {
		return st, nil
	}
	if err := personalExact(modelCreationDB(tx), "current_model_id", modelID).Order("name").Limit(2).Find(&st.Names).Error; err != nil {
		return nil, err
	}
	if len(st.Names) > 1 {
		return nil, modelWeightUnavailable
	}
	for _, n := range st.Names {
		if n.ModelID != modelID || n.CurrentModelID == nil || *n.CurrentModelID != modelID {
			return nil, modelWeightUnavailable
		}
	}

	var err error
	st.Supplies, err = routingSupplyStates(tx, st.Connections, st.Models)
	if err != nil {
		return nil, err
	}
	if err := modelWeightSourceDenials(tx, st); err != nil {
		return nil, err
	}
	if err := modelCreationDB(tx).First(&st.Egress.EgressSetting, 1).Error; err != nil {
		return nil, err
	}
	egressIDs := []string{}
	for _, c := range st.Connections {
		target := c.EgressID
		if c.EgressMode == "" || c.EgressMode == "default" {
			target = st.Egress.EgressSetting.DefaultEgressID
		}
		if target != nil {
			egressIDs = append(egressIDs, *target)
		}
	}
	slices.Sort(egressIDs)
	egressIDs = slices.Compact(egressIDs)
	if len(egressIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", egressIDs)).Order("id").Limit(1001).Find(&st.Egress.Egresses).Error; err != nil {
			return nil, err
		}
	}
	if len(st.Egress.Egresses) != len(egressIDs) {
		return nil, modelWeightUnavailable
	}
	for _, c := range st.Connections {
		_, revision, ready := runtimeEgressSelection(&st.Egress, c)
		st.EgressRevisions[c.ID] = revision
		st.EgressReady[c.ID] = ready
	}
	return st, nil
}
func modelWeightSnapshot(rows []ModelWeightRow) ([]byte, string, error) {
	if len(rows) > modelWeightBindingBudget {
		return nil, "", modelWeightUnavailable
	}
	raw, err := json.Marshal(rows)
	if err != nil || len(raw) > modelWeightSnapshotBudget {
		return nil, "", modelWeightUnavailable
	}
	return raw, memberModelsDigest(rows), nil
}
func modelWeightRowsValid(rows []ModelWeightRow) bool {
	if len(rows) == 0 || len(rows) > modelWeightBindingBudget {
		return false
	}
	sums := map[string]int{}
	for i, row := range rows {
		if i > 0 && rows[i-1].BindingID >= row.BindingID || !modelWeightRetainedID(row.BindingID, "bnd") || !modelWeightRetainedID(row.ProviderModelID, "pmd") || !modelWeightRetainedID(row.ConnectionID, "con") || !modelWeightRetainedID(row.ProviderID, "prv") || !entity.SupportedNativeProtocol(row.Protocol) || row.Weight < 0 || row.Weight > 100 || row.BindingCreatedAt == nil || row.ProviderModelCreatedAt == nil || row.ConnectionCreatedAt == nil || row.ProviderCreatedAt == nil {
			return false
		}
		sums[row.Protocol] += row.Weight
	}
	for _, sum := range sums {
		if sum != 100 {
			return false
		}
	}
	return true
}
func modelWeightTopologyEqual(a, b []ModelWeightRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		x.Weight = 0
		y.Weight = 0
		if memberModelsDigest(x) != memberModelsDigest(y) {
			return false
		}
	}
	return true
}
func (st *modelWeightState) executable(rows []ModelWeightRow) bool {
	if st.Model.Status != entity.ResourceActive || !modelWeightRowsValid(rows) {
		return false
	}
	cs := map[string]entity.ProviderConnection{}
	pms := map[string]entity.ProviderModel{}
	providers := map[string]entity.Provider{}
	for _, p := range st.Providers {
		providers[p.ID] = p
	}
	for _, c := range st.Connections {
		cs[c.ID] = c
	}
	for _, pm := range st.Models {
		pms[pm.ID] = pm
	}
	for _, row := range rows {
		if row.Weight == 0 {
			continue
		}
		c, cok := cs[row.ConnectionID]
		pm, pok := pms[row.ProviderModelID]
		sup, ok := st.Supplies[row.ProviderModelID]
		p, providerOK := providers[row.ProviderID]
		if !providerOK || !p.Enabled || p.ETag == "" || !connectionMetadataBirth(p.CreatedAt) || row.ProviderCreatedAt == nil || !p.CreatedAt.Equal(*row.ProviderCreatedAt) || !cok || !pok || !ok || c.ProviderID != p.ID || pm.ConnectionID != c.ID || !c.Enabled || pm.Disabled || !capabilityTransportCurrent(pm, c) || !sup.Covered || !sup.SourceAvailable || !st.EgressReady[c.ID] || st.EgressRevisions[c.ID] == "" {
			return false
		}
	}
	return true
}
func modelWeightAuthority(tx *gorm.DB, actorID string, readRequired, writeRequired bool) (entity.User, bool, error) {
	actor, err := exactEnabledActor(modelCreationDB(tx), actorID)
	if err != nil {
		return actor, false, err
	}
	read, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "models.read_all")
	if err != nil {
		return actor, false, err
	}
	if readRequired && !read {
		return actor, false, apperrors.ErrForbidden
	}
	write, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "models.write")
	if err != nil {
		return actor, false, err
	}
	if writeRequired && !write {
		return actor, false, apperrors.ErrForbidden
	}
	return actor, write, nil
}

// Physical denials are permanent across credential and auth revisions. Read
// only the selected Vault sources, under the same reviewed DB snapshot; do not
// decrypt, expose or acquire a native/SDK holder for this advisory predicate.
func modelWeightSourceDenials(tx *gorm.DB, st *modelWeightState) error {
	ids := []string{}
	for _, c := range st.Connections {
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var credentials []entity.ProviderCredential
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "connection_id", ids)).Limit(1001).Find(&credentials).Error; err != nil {
		return err
	}
	if len(credentials) > 1000 {
		return modelWeightUnavailable
	}
	if err := attachCredentialSources(tx, credentials); err != nil {
		return err
	}
	// A database enumeration is not a canonical review identity. Sort exact
	// retained IDs by bytes so unchanged selected facts keep the same ETag.
	slices.SortFunc(credentials, func(a, b entity.ProviderCredential) int {
		return strings.Compare(a.ID, b.ID)
	})
	st.Credentials = credentials
	objects := []string{}
	byCredential := map[string]string{}
	for _, c := range credentials {
		if c.VaultReference == nil || c.VaultReference.PhysicalObject == "" {
			continue
		}
		object := c.VaultReference.PhysicalObject
		objects = append(objects, object)
		byCredential[c.ID] = object
	}
	slices.Sort(objects)
	objects = slices.Compact(objects)
	var denials []entity.CredentialSourceDenial
	if len(objects) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "physical_object", objects)).Order("physical_object").Limit(len(objects) + 1).Find(&denials).Error; err != nil {
			return err
		}
	}
	if len(denials) > len(objects) {
		return modelWeightUnavailable
	}
	denied := map[string]bool{}
	proofs := []string{}
	for _, d := range denials {
		if !slices.Contains(objects, d.PhysicalObject) || !sourceDenialValid(d) {
			return modelWeightUnavailable
		}
		denied[d.PhysicalObject] = true
		proofs = append(proofs, d.PhysicalObject)
	}
	st.SourceDenialProof = memberModelsDigest(proofs)
	for pmID, supply := range st.Supplies {
		for _, c := range supply.Credentials {
			if c.Enabled && c.VerificationStatus == "verified" && denied[byCredential[c.ID]] {
				supply.SourceAvailable = false
			}
		}
		st.Supplies[pmID] = supply
	}
	return nil
}

func (st *modelWeightState) currentEligibilityHash() string {
	bindings := []entity.ModelProviderBinding{}
	access := []entity.CredentialModelAccess{}
	seen := map[string]bool{}
	for _, r := range st.Rows {
		b := entity.ModelProviderBinding{ID: r.BindingID, ModelID: st.Model.ID, ProviderModelID: r.ProviderModelID, Weight: r.Weight}
		if r.BindingCreatedAt != nil {
			b.CreatedAt = *r.BindingCreatedAt
		}
		bindings = append(bindings, b)
	}
	for pmID, supply := range st.Supplies {
		for _, c := range supply.Credentials {
			if containsModelCreationAccess(c.Access, pmID) && !seen[c.ID+"|"+pmID] {
				seen[c.ID+"|"+pmID] = true
				access = append(access, entity.CredentialModelAccess{CredentialID: c.ID, ProviderModelID: pmID})
			}
		}
	}
	index := memberModelsIndex(&memberModelsData{EgressSetting: st.Egress.EgressSetting, Egresses: st.Egress.Egresses, Models: []entity.Model{st.Model}, Names: st.Names, Providers: st.Providers, Bindings: bindings, ProviderModels: st.Models, Connections: st.Connections, Credentials: st.Credentials, Access: access})
	return index.hash(st.Model)
}
