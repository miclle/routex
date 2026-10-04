package service

import (
	"fmt"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (s *Service) rootCurrentProof(tx *gorm.DB, p entity.SecretWritePolicy) (entity.SecretProcessVerification, error) {
	return s.rootCurrentProofAdmission(tx, p, false)
}
func (s *Service) rootCurrentProofAdmission(tx *gorm.DB, p entity.SecretWritePolicy, allowClosed bool) (entity.SecretProcessVerification, error) {
	var proof entity.SecretProcessVerification
	view := s.rootPolicy.Load()
	if !p.Initialized || view == nil || (view.closed.Load() && !allowClosed) || view.epoch != p.Epoch || p.WriteKeyID == nil || view.writeID != *p.WriteKeyID || s.runtime == nil {
		return proof, secretStoreUnavailable
	}
	s.instanceMu.RLock()
	lease := s.instance
	s.instanceMu.RUnlock()
	if lease == nil {
		return proof, secretStoreUnavailable
	}
	var instances []entity.SystemInstance
	if err := tx.Where("stopped_at IS NULL AND retired_at IS NULL AND lease_expires_at > ?", s.instanceNow().UTC()).Find(&instances).Error; err != nil {
		return proof, err
	}
	if len(instances) != 1 || instances[0].ID != lease.id || instances[0].LeaseToken != lease.token || instances[0].Role != "combined" {
		return proof, secretStoreUnavailable
	}
	auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if auth == nil || routes == nil || !time.Now().Before(auth.ValidUntil) || auth.SourceDigest == "" || auth.SourceDigest != routes.Digest {
		return proof, secretStoreUnavailable
	}
	data, err := s.loadRuntimeDataTx(tx)
	if err != nil {
		return proof, err
	}
	digest, err := runtimeDigest(data)
	if err != nil || digest != routes.Digest {
		return proof, secretStoreUnavailable
	}
	var keys []entity.SecretRootKey
	if err := tx.Order("key_id").Find(&keys).Error; err != nil {
		return proof, err
	}
	manifest := ""
	for _, key := range keys {
		manifest += key.KeyID + ":" + key.State + ";"
		if key.State == "retired" {
			continue
		}
		plain, err := view.store.Open(rootProofReference(key.KeyID), key.ProofCiphertext)
		if err != nil || plain != rootProofPlaintext {
			return proof, secretStoreUnavailable
		}
	}
	proof = entity.SecretProcessVerification{ProcessID: lease.id, PolicyEpoch: p.Epoch, KeyManifestDigest: rootHash(manifest), CryptoVersion: 2, LeaseToken: lease.token, RuntimeSnapshotID: routes.ID, RuntimeSourceDigest: routes.Digest, VerifiedAt: s.secretNow()}
	return proof, nil
}
func (s *Service) rootPersistProof(tx *gorm.DB, p entity.SecretWritePolicy) (entity.SecretProcessVerification, error) {
	proof, err := s.rootCurrentProof(tx, p)
	if err != nil {
		return proof, err
	}
	err = tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&proof).Error
	return proof, err
}
func rootJobETag(job entity.SecretRotationJob) string {
	return rootHash(fmt.Sprintf("%s:%s:%s:%d:%d:%s:%s:%s:%v", job.ID, job.Status, job.Phase, job.CutoverEpoch, job.ScanGeneration, job.Cursor, job.CountsJSON, job.BlockerCode, job.ObservationStartedAt))
}
