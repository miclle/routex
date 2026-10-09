package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type capabilityTransportAuditValues struct {
	Enabled    bool   `json:"enabled"`
	Image      bool   `json:"image"`
	PDF        bool   `json:"pdf"`
	Generation string `json:"generation"`
}
type capabilityTransportAudit struct {
	Version string                         `json:"version"`
	Before  capabilityTransportAuditValues `json:"before"`
	After   capabilityTransportAuditValues `json:"after"`
}

func capabilityTransportAuditValue(pm entity.ProviderModel) capabilityTransportAuditValues {
	return capabilityTransportAuditValues{!pm.Disabled, pm.SupportsImageInput, pm.SupportsPDFInput, pm.CapabilityTransportGeneration}
}
func appendCapabilityTransportAudit(tx *gorm.DB, actor string, before, after entity.ProviderModel) error {
	raw, err := json.Marshal(capabilityTransportAudit{"provider_model.capability.v1", capabilityTransportAuditValue(before), capabilityTransportAuditValue(after)})
	if err != nil {
		return err
	}
	aid, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	text := string(raw)
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: actor, Action: "provider_model.capability.review", ResourceType: "provider_model", ResourceID: after.ID, DetailsJSON: &text}).Error
}
func capabilityTransportAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.ResourceType != "provider_model" || !modelWeightRetainedID(row.ResourceID, "pmd") || !modelWeightRetainedID(row.ActorID, "usr") || row.DetailsJSON == nil || len(*row.DetailsJSON) > 4096 {
		return nil, false
	}
	raw := []byte(*row.DetailsJSON)
	fields, e := decodeDefaultLimitObject(raw, []string{"version", "before", "after"})
	if e != nil {
		return nil, false
	}
	for _, key := range []string{"before", "after"} {
		v, e := decodeDefaultLimitObject(fields[key], []string{"enabled", "image", "pdf", "generation"})
		if e != nil {
			return nil, false
		}
		for _, field := range []string{"enabled", "image", "pdf"} {
			if string(v[field]) != "true" && string(v[field]) != "false" {
				return nil, false
			}
		}
	}
	var d capabilityTransportAudit
	if json.Unmarshal(raw, &d) != nil || d.Version != "provider_model.capability.v1" || !validTransportGeneration(d.Before.Generation) || !validTransportGeneration(d.After.Generation) || d.Before == d.After {
		return nil, false
	}
	return d, true
}
