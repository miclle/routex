package service

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

func (s *Service) writeRuntimeArchiveRouting(tx *gorm.DB, actor entity.User, st *runtimeArchiveRollbackState, now time.Time) error {
	affected := map[string]bool{}
	for i, c := range st.Desired.Connections {
		if !st.TransportChanged[c.ID] {
			continue
		}
		generation, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		revision, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		// Never copy historical transport/capability/verification generations. Fresh
		// generations make every old discovery/capacity/declaration proof stale.
		c.TransportGeneration, c.ETag = generation, revision
		st.Desired.Connections[i] = c
		result := personalExact(tx.Model(&entity.ProviderConnection{}), "id", c.ID).
			Select("BaseURL", "Adapter", "APIVersion", "EgressMode", "EgressID", "TransportGeneration", "ETag").
			Updates(&c)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtimeArchiveRollbackUnavailable
		}
	}
	for _, pm := range st.Desired.ProviderModels {
		if !st.DeclarationChanged[pm.ID] {
			continue
		}
		revision, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		pm.ETag = revision
		fields := []string{"SupportsImageInput", "SupportsPDFInput", "ETag"}
		// On an unchanged transport this explicit reviewed declaration is current.
		// Changed transport keeps the old proof stale until separate real Verify and
		// a fresh declaration review; the archive itself never grants eligibility.
		if !st.TransportChanged[pm.ConnectionID] {
			ci := slices.IndexFunc(st.Current.Connections, func(c entity.ProviderConnection) bool { return c.ID == pm.ConnectionID })
			if ci < 0 {
				return runtimeArchiveRollbackConflict
			}
			pm.CapabilityTransportGeneration = st.Current.Connections[ci].TransportGeneration
			fields = append(fields, "CapabilityTransportGeneration")
		}
		result := personalExact(tx.Model(&entity.ProviderModel{}), "id", pm.ID).Select(fields).Updates(&pm)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return runtimeArchiveRollbackUnavailable
		}
	}
	for _, m := range st.Current.Models {
		if !st.WeightModels[m.ID] {
			continue
		}
		before := runtimeArchiveWeightRows(st.Current, m.ID)
		after := runtimeArchiveWeightRows(st.Desired, m.ID)
		if !modelWeightRowsValid(after) || !modelWeightTopologyEqual(before, after) {
			return runtimeArchiveRollbackConflict
		}
		weightState := &modelWeightState{Model: m, Rows: before}
		// The existing complete-set editor journal retains before/after configuration
		// versions. The archive command/audit separately identifies this rollback;
		// there is no fabricated weight-version rollback target.
		if _, _, err := journalModelWeightChange(tx, actor.ID, weightState, after, "legacy_editor", nil, nil, now); err != nil {
			return err
		}
		for _, b := range st.Desired.Bindings {
			if b.ModelID != m.ID {
				continue
			}
			bi := slices.IndexFunc(st.Current.Bindings, func(row entity.ModelProviderBinding) bool { return row.ID == b.ID })
			if bi < 0 {
				return runtimeArchiveRollbackConflict
			}
			if st.Current.Bindings[bi].Weight == b.Weight {
				continue
			}
			result := personalExact(tx.Model(&entity.ModelProviderBinding{}), "id", b.ID).Update("weight", b.Weight)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return runtimeArchiveRollbackUnavailable
			}
		}
		affected[m.ID] = true
	}
	for _, b := range st.Current.Bindings {
		pi := slices.IndexFunc(st.Current.ProviderModels, func(p entity.ProviderModel) bool { return p.ID == b.ProviderModelID })
		if pi < 0 {
			return runtimeArchiveRollbackConflict
		}
		if st.TransportChanged[st.Current.ProviderModels[pi].ConnectionID] || st.DeclarationChanged[b.ProviderModelID] {
			affected[b.ModelID] = true
		}
	}
	for _, m := range st.Current.Models {
		if affected[m.ID] {
			if err := stampModelConfiguration(tx, m.ID, now); err != nil {
				return err
			}
		}
	}
	return nil
}
func runtimeArchiveRollbackAudit(tx *gorm.DB, row entity.RuntimeArchiveRollback, preview *RuntimeArchiveRollbackPreview) error {
	detail, err := json.Marshal(struct {
		RequestID        string `json:"request_id"`
		ArchiveID        string `json:"archive_id"`
		Scope            string `json:"scope"`
		Changed          bool   `json:"changed"`
		ConnectionCount  int    `json:"connection_count"`
		DeclarationCount int    `json:"declaration_count"`
		WeightCount      int    `json:"weight_count"`
		Reason           string `json:"reason"`
	}{row.RequestID, row.ArchiveID, "gateway_routing_configuration", row.Changed, len(preview.Connections), len(preview.Declarations), len(preview.Weights), row.Reason})
	if err != nil {
		return err
	}
	identity, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	text := string(detail)
	return tx.Create(&entity.AuditEvent{ID: identity, ActorID: row.ActorID, Action: "runtime.archive.routing.rollback", ResourceType: "runtime_configuration_archive", ResourceID: row.ArchiveID, DetailsJSON: &text}).Error
}
