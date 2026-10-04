package service

import (
	"encoding/json"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/prices"
	"gorm.io/gorm"
)

type RepositoryPriceAudit struct {
	RequestID    string                        `json:"request_id"`
	SourceDigest string                        `json:"source_digest"`
	Reason       string                        `json:"reason"`
	Mode         string                        `json:"mode"`
	Enabled      *bool                         `json:"enabled,omitempty"`
	Mappings     []RepositoryPriceMappingInput `json:"mappings,omitempty"`
	Before       []PriceRecord                 `json:"before,omitempty"`
	After        []PriceRecord                 `json:"after,omitempty"`
}

func appendRepositoryAudit(tx *gorm.DB, actorID, action, resourceID, digest, reason string, record RepositoryPriceAudit) error {
	record.SourceDigest = digest
	record.Reason = reason
	return appendPricingSourceAudit(tx, actorID, action, resourceID, prices.SourceID, nil, record)
}

// Repository audit deliberately reads only this bounded recorded schema. It
// never fetches mutable model names or exposes arbitrary stored audit JSON.
func repositoryPriceAuditProjection(row entity.AuditEvent) (RepositoryPriceAudit, bool) {
	var envelope struct {
		Source string               `json:"source"`
		After  RepositoryPriceAudit `json:"after"`
	}
	if row.ResourceType != "pricing" || row.DetailsJSON == nil || len(*row.DetailsJSON) > 60*1024 || json.Unmarshal([]byte(*row.DetailsJSON), &envelope) != nil {
		return RepositoryPriceAudit{}, false
	}
	record := envelope.After
	if envelope.Source != prices.SourceID || !credentialReplacementRequestID.MatchString(record.RequestID) || !personalModelETag(record.SourceDigest) || !repositoryReason(record.Reason) {
		return RepositoryPriceAudit{}, false
	}
	switch row.Action {
	case "prices.repository.config":
		if record.Mode != "configure" || record.Enabled == nil || len(record.Mappings) > repositoryMaxMappings || len(record.Before) != 0 || len(record.After) != 0 {
			return RepositoryPriceAudit{}, false
		}
		for _, m := range record.Mappings {
			if !safeTeamSessionID(m.ProviderModelID) || !repositorySourceKey.MatchString(m.SourceModelKey) {
				return RepositoryPriceAudit{}, false
			}
		}
	case "prices.repository.apply":
		if (record.Mode != "sync" && record.Mode != "restore") || record.Enabled != nil || len(record.Mappings) != 0 || len(record.Before) > 20 || len(record.After) > 20 {
			return RepositoryPriceAudit{}, false
		}
	default:
		return RepositoryPriceAudit{}, false
	}
	return record, true
}
