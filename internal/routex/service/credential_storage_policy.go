package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CredentialStorageChoice struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Birth      time.Time `json:"birth"`
	RevisionID string    `json:"revision_id"`
}
type CredentialStoragePolicyView struct {
	Mode          string                    `json:"mode"`
	IntegrationID *string                   `json:"integration_id"`
	RevisionID    *string                   `json:"revision_id"`
	Choices       []CredentialStorageChoice `json:"choices"`
	ETag          string                    `json:"etag"`
	CanEdit       bool                      `json:"can_edit"`
}
type CredentialStoragePolicyInput struct {
	Mode          string  `json:"mode"`
	IntegrationID *string `json:"integration_id"`
	RevisionID    *string `json:"revision_id"`
	Reason        string  `json:"reason"`
}

func (v *CredentialStoragePolicyInput) UnmarshalJSON(raw []byte) error {
	f, e := vaultStrict(raw, []string{"mode", "integration_id", "revision_id", "reason"})
	if e != nil {
		return e
	}
	var n CredentialStoragePolicyInput
	if len(f) != 4 {
		return apperrors.ErrBadRequest
	}
	for k, p := range map[string]any{"mode": &n.Mode, "integration_id": &n.IntegrationID, "revision_id": &n.RevisionID, "reason": &n.Reason} {
		if json.Unmarshal(f[k], p) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if (n.Mode != "inline" && n.Mode != "vault") || !rootReason(n.Reason) || strings.TrimSpace(n.Reason) != n.Reason {
		return apperrors.ErrBadRequest
	}
	if n.Mode == "inline" {
		if n.IntegrationID != nil || n.RevisionID != nil {
			return apperrors.ErrBadRequest
		}
	} else if n.IntegrationID == nil || n.RevisionID == nil || !rootSafeIdentity(*n.IntegrationID, 30) || !vaultRevisionID.MatchString(*n.RevisionID) {
		return apperrors.ErrBadRequest
	}
	*v = n
	return nil
}
func credentialStoragePolicy(tx *gorm.DB, lock bool) (entity.CredentialStoragePolicy, error) {
	var p entity.CredentialStoragePolicy
	q := vaultDB(tx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.Take(&p, 1).Error
	if e == nil && (p.ID != 1 || (p.Mode != "inline" && p.Mode != "vault") || p.Generation == "" || p.Mode == "inline" && (p.IntegrationID != nil || p.IntegrationBirth != nil || p.RevisionID != nil) || p.Mode == "vault" && (p.IntegrationID == nil || p.IntegrationBirth == nil || p.RevisionID == nil)) {
		e = vaultUnavailable
	}
	return p, e
}
func (s *Service) credentialStoragePolicyView(tx *gorm.DB, actor entity.User, p entity.CredentialStoragePolicy) (*CredentialStoragePolicyView, error) {
	var rows []entity.VaultIntegration
	if e := vaultDB(tx).Order("id").Limit(101).Find(&rows).Error; e != nil {
		return nil, e
	}
	if len(rows) > 100 {
		return nil, apperrors.ErrBadRequest
	}
	choices := []CredentialStorageChoice{}
	for _, row := range rows {
		exact, rev, w, r, e := vaultSnapshot(tx, row.ID, false)
		if e != nil {
			return nil, e
		}
		if w.AuthCiphertext != "" && r.AuthCiphertext != "" {
			choices = append(choices, CredentialStorageChoice{exact.ID, exact.Name, exact.CreatedAt.UTC(), rev.ID})
		}
	}
	edit, e := exactGovernancePermission(tx, actor, "secrets.write")
	if e != nil {
		return nil, e
	}
	v := &CredentialStoragePolicyView{p.Mode, p.IntegrationID, p.RevisionID, choices, "", edit}
	raw, _ := json.Marshal(struct {
		Actor   string
		Birth   time.Time
		Policy  entity.CredentialStoragePolicy
		Choices []CredentialStorageChoice
	}{actor.ID, actor.CreatedAt.UTC(), p, choices})
	v.ETag = rootHash(string(raw))
	return v, nil
}
func (s *Service) GetCredentialStoragePolicy(ctx context.Context, actorID string) (*CredentialStoragePolicyView, error) {
	var result *CredentialStoragePolicyView
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := vaultAuthorize(tx, actorID, "secrets.read")
		if e != nil {
			return e
		}
		p, e := credentialStoragePolicy(tx, false)
		if e != nil {
			return e
		}
		result, e = s.credentialStoragePolicyView(tx, actor, p)
		return e
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(e)
}
func (s *Service) SaveCredentialStoragePolicy(ctx context.Context, actorID, etag string, input CredentialStoragePolicyInput) (*CredentialStoragePolicyView, error) {
	raw, err := json.Marshal(input)
	var checked CredentialStoragePolicyInput
	if err != nil || json.Unmarshal(raw, &checked) != nil || !personalModelETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	input = checked
	var result *CredentialStoragePolicyView
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		actor, e := vaultAuthorize(tx, actorID, "secrets.write")
		if e != nil {
			return e
		}
		p, e := credentialStoragePolicy(tx, true)
		if e != nil {
			return e
		}
		view, e := s.credentialStoragePolicyView(tx, actor, p)
		if e != nil {
			return e
		}
		if etag != view.ETag {
			return catalogConflict
		}
		if input.Mode == "vault" {
			if input.IntegrationID == nil || input.RevisionID == nil {
				return apperrors.ErrBadRequest
			}
			row, rev, w, r, e := vaultSnapshot(tx, *input.IntegrationID, true)
			if e != nil {
				return e
			}
			if rev.ID != *input.RevisionID || w.AuthCiphertext == "" || r.AuthCiphertext == "" {
				return catalogConflict
			}
			birth := row.CreatedAt
			p.IntegrationBirth = &birth
		} else if input.Mode == "inline" && input.IntegrationID == nil && input.RevisionID == nil {
			p.IntegrationBirth = nil
		} else {
			return apperrors.ErrBadRequest
		}
		p.Mode, p.IntegrationID, p.RevisionID = input.Mode, input.IntegrationID, input.RevisionID
		p.Generation, e = id.NewPrefixed("csp")
		if e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&p).Error; e != nil {
			return e
		}
		if e = appendAudit(tx, actor.ID, "credential.storage.policy", "credential_storage_policy", "1"); e != nil {
			return e
		}
		result, e = s.credentialStoragePolicyView(tx, actor, p)
		return e
	})
	return result, catalogError(e)
}

type CredentialStorageContext struct {
	StorageSource string `json:"storage_source"`
	ETag          string `json:"etag"`
}

func credentialStorageContext(actor entity.User, p entity.CredentialStoragePolicy) *CredentialStorageContext {
	raw, _ := json.Marshal(struct {
		Domain, Actor, Generation string
		Birth                     time.Time
	}{"credential.storage.context.v1", actor.ID, p.Generation, actor.CreatedAt.UTC()})
	return &CredentialStorageContext{p.Mode, rootHash(string(raw))}
}
func (s *Service) GetCredentialStorageContext(ctx context.Context, actorID string) (*CredentialStorageContext, error) {
	var result *CredentialStorageContext
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		if e = exactCatalogPermission(tx, actorID, "providers.write"); e != nil {
			return e
		}
		p, e := credentialStoragePolicy(tx, false)
		if e != nil {
			return e
		}
		result = credentialStorageContext(actor, p)
		return nil
	})
	return result, catalogError(e)
}
