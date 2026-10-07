package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
)

func vaultIntent(v VaultConfigInput) string {
	v.WriterAuth.Token = ""
	v.ReaderAuth.Token = ""
	b, _ := json.Marshal(v)
	return string(b)
}
func vaultTextEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func vaultStrong(v string) bool {
	return len(v) == 129 && v[64] == '.' && personalModelETag(v[:64]) && personalModelETag(v[65:])
}
func (s *Service) vaultOpen(w entity.VaultWriterAuth, r entity.VaultReaderAuth) (string, string, error) {
	var wt, rt string
	var err error
	if w.AuthCiphertext != "" {
		wt, err = s.openSecret(rootReference("vault_writer_auth", w.ID, w.SecretGeneration), w.AuthCiphertext)
		if err != nil || !vaultToken(wt) {
			return "", "", vaultUnavailable
		}
	}
	if r.AuthCiphertext != "" {
		rt, err = s.openSecret(rootReference("vault_reader_auth", r.ID, r.SecretGeneration), r.AuthCiphertext)
		if err != nil || !vaultToken(rt) {
			return "", "", vaultUnavailable
		}
	}
	if wt != "" && rt != "" && vaultTextEqual(wt, rt) {
		return "", "", vaultUnavailable
	}
	return wt, rt, nil
}
func vaultAuthValue(v VaultAuthInput, old string, creation bool) (string, error) {
	switch v.Action {
	case "replace":
		if !vaultToken(v.Token) {
			return "", apperrors.ErrBadRequest
		}
		return v.Token, nil
	case "keep":
		if creation || old == "" || v.Token != "" {
			return "", apperrors.ErrBadRequest
		}
		return old, nil
	case "remove":
		if v.Token != "" {
			return "", apperrors.ErrBadRequest
		}
		return "", nil
	default:
		return "", apperrors.ErrBadRequest
	}
}
func (s *Service) vaultReceiptMatch(tx *gorm.DB, actor entity.User, target, etag string, input VaultConfigInput) (*VaultConfigResult, error) {
	var receipt entity.VaultConfigReceipt
	err := personalExact(vaultDB(tx), "request_id", input.RequestID).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if receipt.ActorID != actor.ID || !receipt.ActorBirth.Equal(actor.CreatedAt) || receipt.ReviewETag != etag || receipt.IntentJSON != vaultIntent(input) || target != "" && receipt.IntegrationID != target {
		return nil, catalogConflict
	}
	row, _, _, _, err := vaultSnapshot(tx, receipt.IntegrationID, false)
	if err != nil {
		return nil, err
	}
	if !row.CreatedAt.Equal(receipt.IntegrationBirth) {
		return nil, catalogConflict
	}
	var original entity.VaultRevision
	if err = personalExact(vaultDB(tx), "id", receipt.RevisionID).Take(&original).Error; err != nil {
		return nil, err
	}
	if original.ID != receipt.RevisionID || original.IntegrationID != receipt.IntegrationID || !original.IntegrationBirth.Equal(receipt.IntegrationBirth) {
		return nil, vaultUnavailable
	}
	var w entity.VaultWriterAuth
	var r entity.VaultReaderAuth
	if err = personalExact(vaultDB(tx), "id", receipt.RevisionID).Take(&w).Error; err != nil {
		return nil, err
	}
	if err = personalExact(vaultDB(tx), "id", receipt.RevisionID).Take(&r).Error; err != nil {
		return nil, err
	}
	if w.ID != receipt.RevisionID || r.ID != receipt.RevisionID {
		return nil, vaultUnavailable
	}
	wt, rt, err := s.vaultOpen(w, r)
	if err != nil {
		return nil, err
	}
	if input.WriterAuth.Action == "replace" && !vaultTextEqual(wt, input.WriterAuth.Token) || input.ReaderAuth.Action == "replace" && !vaultTextEqual(rt, input.ReaderAuth.Token) {
		return nil, catalogConflict
	}
	return &VaultConfigResult{receipt.RequestID, receipt.IntegrationID, receipt.RevisionID, true, receipt.Changed}, nil
}
func (s *Service) SaveVaultIntegration(ctx context.Context, actorID, target, etag string, input VaultConfigInput) (*VaultConfigResult, error) {
	raw, _ := json.Marshal(input)
	var checked VaultConfigInput
	if checked.UnmarshalJSON(raw) != nil || !vaultStrong(etag) || target != "" && !vaultIntegrationID.MatchString(target) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, err := vault.New(input.Descriptor.client(), s.allowPrivateUpstream)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	client.Close()
	var actor entity.User
	var row entity.VaultIntegration
	var original entity.VaultRevision
	var ow entity.VaultWriterAuth
	var or entity.VaultReaderAuth
	var replay *VaultConfigResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		actor, e = vaultAuthorize(tx, actorID, "secrets.write")
		if e != nil {
			return e
		}
		replay, e = s.vaultReceiptMatch(tx, actor, target, etag, input)
		if e != nil || replay != nil {
			return e
		}
		if target != "" {
			row, original, ow, or, e = vaultSnapshot(tx, target, false)
			if e != nil {
				return e
			}
			if strings.Split(etag, ".")[0] != vaultIdentity(actor, row) {
				return catalogConflict
			}
		}
		return nil
	})
	if err != nil {
		return nil, vaultError(err)
	}
	if replay != nil {
		return replay, nil
	}
	oldW, oldR, err := s.vaultOpen(ow, or)
	if err != nil {
		return nil, err
	}
	wt, err := vaultAuthValue(input.WriterAuth, oldW, target == "")
	if err != nil {
		return nil, err
	}
	rt, err := vaultAuthValue(input.ReaderAuth, oldR, target == "")
	if err != nil {
		return nil, err
	}
	if wt != "" && rt != "" && vaultTextEqual(wt, rt) {
		return nil, apperrors.ErrBadRequest
	}
	revisionID, err := id.NewPrefixed("vlr")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	wg, err := id.NewPrefixed("vag")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	rg, err := id.NewPrefixed("vag")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	epoch := s.secretEpoch()
	w := entity.VaultWriterAuth{ID: revisionID, SecretGeneration: wg}
	r := entity.VaultReaderAuth{ID: revisionID, SecretGeneration: rg}
	if wt != "" {
		w.AuthCiphertext, err = s.sealSecret(rootReference("vault_writer_auth", revisionID, wg), wt)
		if err != nil {
			return nil, err
		}
	}
	if rt != "" {
		r.AuthCiphertext, err = s.sealSecret(rootReference("vault_reader_auth", revisionID, rg), rt)
		if err != nil {
			return nil, err
		}
	}
	now := vaultNow()
	if target == "" {
		target, err = id.NewPrefixed("vlt")
		if err != nil {
			return nil, apperrors.ErrInternal
		}
		row = entity.VaultIntegration{ID: target, CreatedAt: now}
	}
	result := &VaultConfigResult{RequestID: input.RequestID, IntegrationID: target, Committed: true}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		currentActor, e := vaultAuthorize(tx, actorID, "secrets.write")
		if e != nil {
			return e
		}
		if !currentActor.CreatedAt.Equal(actor.CreatedAt) {
			return catalogConflict
		}
		cat, e := vaultCatalogue(tx, true)
		if e != nil {
			return e
		}
		receipt, e := s.vaultReceiptMatch(tx, currentActor, func() string {
			if original.ID != "" {
				return target
			}
			return ""
		}(), etag, input)
		if e != nil {
			return e
		}
		if receipt != nil {
			*result = *receipt
			return nil
		}
		changed := true
		if original.ID == "" {
			if vaultCreationReview(currentActor, cat) != etag {
				return catalogConflict
			}
		} else {
			current, rev, cw, cr, e := vaultSnapshot(tx, target, true)
			if e != nil {
				return e
			}
			if !current.CreatedAt.Equal(row.CreatedAt) {
				return catalogConflict
			}
			if rev.ID != original.ID {
				return catalogConflict
			}
			changed = rev.Name != input.Name || vaultDescriptor(rev) != input.Descriptor || !vaultTextEqual(oldW, wt) || !vaultTextEqual(oldR, rt)
			if changed && vaultReview(currentActor, current, rev) != etag {
				return catalogConflict
			}
			row = current
			if cw.AuthCiphertext != ow.AuthCiphertext || cr.AuthCiphertext != or.AuthCiphertext { // Rewrap may change bytes without changing the logical revision; new writes still use the epoch fence.
				if cw.SecretGeneration != ow.SecretGeneration || cr.SecretGeneration != or.SecretGeneration {
					return catalogConflict
				}
			}
		}
		result.Changed = changed
		result.RevisionID = row.RevisionID
		if changed {
			if e = s.guardSecretWrite(tx, epoch, rootReference("vault_writer_auth", revisionID, wg), w.AuthCiphertext); e != nil {
				return e
			}
			if e = s.guardSecretWrite(tx, epoch, rootReference("vault_reader_auth", revisionID, rg), r.AuthCiphertext); e != nil {
				return e
			}
			row.Name = input.Name
			row.RevisionID = revisionID
			row.UpdatedAt = now
			rev := entity.VaultRevision{ID: revisionID, IntegrationID: row.ID, IntegrationBirth: row.CreatedAt, Name: input.Name, Endpoint: input.Descriptor.Endpoint, Namespace: input.Descriptor.Namespace, Mount: input.Descriptor.Mount, Prefix: input.Descriptor.Prefix, DataField: input.Descriptor.DataField, CreatedAt: now}
			if original.ID == "" {
				if e = tx.Create(&row).Error; e != nil {
					return e
				}
			} else {
				if e = personalExact(vaultDB(tx).Model(&entity.VaultIntegration{}), "id", row.ID).Select("Name", "RevisionID", "UpdatedAt").Updates(row).Error; e != nil {
					return e
				}
			}
			for _, record := range []any{&rev, &w, &r} {
				if e = tx.Create(record).Error; e != nil {
					return e
				}
			}
			result.RevisionID = revisionID
			cat.Generation = rootHash("vault.catalogue:" + revisionID)
			if e = tx.Model(&entity.VaultCatalogue{}).Where("id = ?", 1).Update("Generation", cat.Generation).Error; e != nil {
				return e
			}
		}
		receiptRow := entity.VaultConfigReceipt{RequestID: input.RequestID, ActorID: actor.ID, ActorBirth: actor.CreatedAt, IntegrationID: row.ID, IntegrationBirth: row.CreatedAt, RevisionID: result.RevisionID, ReviewETag: etag, IntentJSON: vaultIntent(input), Changed: changed, CreatedAt: now}
		if e = tx.Create(&receiptRow).Error; e != nil {
			return e
		}
		return vaultAudit(tx, actor.ID, row.ID, "configure", input.RequestID, input.Reason, result.RevisionID, changed)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
func vaultAudit(tx *gorm.DB, actor, target, action, request, reason, revision string, changed bool) error {
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	detail := vaultAuditDetail{RequestID: request, Reason: reason, RevisionID: revision, Changed: changed}
	b, _ := json.Marshal(detail)
	text := string(b)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: "vault.integration." + action, ResourceType: "vault_integration", ResourceID: target, DetailsJSON: &text}).Error
}

type vaultAuditDetail struct {
	RequestID  string `json:"request_id"`
	Reason     string `json:"reason"`
	RevisionID string `json:"revision_id"`
	Changed    bool   `json:"changed"`
}

func vaultAuditProjection(row entity.AuditEvent) (vaultAuditDetail, bool) {
	var v vaultAuditDetail
	if row.ResourceType != "vault_integration" || !vaultIntegrationID.MatchString(row.ResourceID) || row.DetailsJSON == nil {
		return v, false
	}
	switch row.Action {
	case "vault.integration.configure", "vault.integration.write", "vault.integration.read", "vault.integration.cleanup":
	default:
		return v, false
	}
	// Ignore unrelated historic keys, project only this explicit nonsecret type.
	if json.Unmarshal([]byte(*row.DetailsJSON), &v) != nil || !credentialReplacementRequestID.MatchString(v.RequestID) || !vaultRevisionID.MatchString(v.RevisionID) || !rootReason(v.Reason) || row.Action != "vault.integration.configure" && v.Changed {
		return v, false
	}
	return v, true
}
