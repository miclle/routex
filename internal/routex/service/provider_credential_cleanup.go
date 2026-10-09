package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"sync"
	"time"
)

// Creation/recovery holders protect business commit. Published cleanup also
// requires the durable physical source barrier and complete process drain.
func (s *Service) credentialCreationLease(request string) (func(), error) {
	s.credentialCleanupMu.Lock()
	defer s.credentialCleanupMu.Unlock()
	if s.credentialCleanupHolders[request] {
		return nil, catalogConflict
	}
	if s.credentialCreationHolders == nil {
		s.credentialCreationHolders = map[string]int{}
	}
	s.credentialCreationHolders[request]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.credentialCleanupMu.Lock()
			defer s.credentialCleanupMu.Unlock()
			s.credentialCreationHolders[request]--
			if s.credentialCreationHolders[request] == 0 {
				delete(s.credentialCreationHolders, request)
			}
		})
	}, nil
}
func (s *Service) credentialCleanupLease(request string) (func(), error) {
	s.credentialCleanupMu.Lock()
	defer s.credentialCleanupMu.Unlock()
	if s.credentialCreationHolders[request] != 0 || s.credentialCleanupHolders[request] {
		return nil, catalogConflict
	}
	if s.credentialCleanupHolders == nil {
		s.credentialCleanupHolders = map[string]bool{}
	}
	s.credentialCleanupHolders[request] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			s.credentialCleanupMu.Lock()
			defer s.credentialCleanupMu.Unlock()
			delete(s.credentialCleanupHolders, request)
		})
	}, nil
}
func (s *Service) credentialCreationHeld(request string) bool {
	s.credentialCleanupMu.Lock()
	defer s.credentialCleanupMu.Unlock()
	return s.credentialCreationHolders[request] != 0
}
func credentialCleanupAdmission(tx *gorm.DB, request string) error {
	var rows []entity.ProviderCredentialCleanup
	if err := personalExact(vaultDB(tx), "creation_request_id", request).Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != 0 {
		return catalogConflict
	}
	return nil
}
func cleanupOperationProof(op entity.CredentialStorageOperation) string {
	// Rewrap changes ciphertext, not this immutable logical ownership identity.
	return connectionMetadataHash(struct {
		Request, Actor, Source, Kind, Target, Credential, Provider, Connection, Reference, Marker, Descriptor, Integration, Revision, Writer, Reader string
		ActorBirth, TargetBirth, CredentialBirth, ProviderBirth, ConnectionBirth                                                                     time.Time
		IntegrationBirth                                                                                                                             *time.Time
	}{op.RequestID, op.ActorID, op.StorageSource, op.Kind, op.TargetID, op.CredentialID, op.ProviderID, op.ConnectionID, op.ReferenceID, op.ExpectedMarkerSHA256, op.DescriptorSHA256, op.IntegrationID, op.RevisionID, op.WriterGeneration, op.ReaderGeneration, op.ActorBirth, op.TargetBirth, op.CredentialBirth, op.ProviderBirth, op.ConnectionBirth, op.IntegrationBirth})
}
func (s *Service) cleanupSoleInstance(tx *gorm.DB) (bool, error) {
	s.instanceMu.RLock()
	lease := s.instance
	s.instanceMu.RUnlock()
	if lease == nil {
		return false, nil
	}
	var rows []entity.SystemInstance
	if err := vaultDB(tx).Find(&rows).Error; err != nil {
		return false, err
	}
	return len(rows) == 1 && rows[0].ID == lease.id && rows[0].LeaseToken == lease.token && rows[0].Role == "combined" && rows[0].StoppedAt == nil && rows[0].RetiredAt == nil && rows[0].LeaseExpiresAt.After(s.instanceNow().UTC()), nil
}
func cleanupAuthorize(tx *gorm.DB, actorID string, write bool) (entity.User, error) {
	permission := "secrets.read"
	if write {
		permission = "secrets.write"
	}
	actor, err := vaultAuthorize(tx, actorID, permission)
	if err != nil {
		return actor, err
	}
	if write {
		err = exactCatalogPermission(tx, actor.ID, "providers.write")
	}
	return actor, err
}
func cleanupOperation(tx *gorm.DB, row entity.VaultIntegration, request string) (entity.CredentialStorageOperation, error) {
	var op entity.CredentialStorageOperation
	err := personalExact(vaultDB(tx), "request_id", request).Take(&op).Error
	if err == nil && (op.RequestID != request || op.StorageSource != "vault" || op.IntegrationID != row.ID || op.IntegrationBirth == nil || !op.IntegrationBirth.Equal(row.CreatedAt)) {
		err = apperrors.ErrNotFound
	}
	return op, err
}
func cleanupTarget(ctx context.Context, s *Service, actorID, target, request string, write bool) (entity.User, entity.VaultIntegration, entity.CredentialStorageOperation, error) {
	var actor entity.User
	var row entity.VaultIntegration
	var op entity.CredentialStorageOperation
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		actor, e = cleanupAuthorize(tx, actorID, write)
		if e != nil {
			return e
		}
		row, _, _, _, e = vaultSnapshot(tx, target, false)
		if e != nil {
			return e
		}
		op, e = cleanupOperation(tx, row, request)
		return e
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return actor, row, op, err
}
func cleanupOriginalConfirmed(op entity.CredentialStorageOperation) (VaultObservationView, VaultObservationView, bool) {
	var w, r VaultObservationView
	ok := vaultDecodeObservation(op.WriteJSON, &w) && vaultDecodeObservation(op.ReadJSON, &r) && w.Attempted && w.Succeeded && w.Failure == nil && r.Attempted && r.Succeeded && r.Failure == nil
	return w, r, ok
}
func (s *Service) cleanupView(tx *gorm.DB, actor entity.User, row entity.VaultIntegration, op entity.CredentialStorageOperation, claiming bool) (ProviderCredentialOrphanView, error) {
	w, r, confirmed := cleanupOriginalConfirmed(op)
	if !vaultDecodeObservation(op.WriteJSON, &w) || !vaultDecodeObservation(op.ReadJSON, &r) {
		return ProviderCredentialOrphanView{}, vaultUnavailable
	}
	codes := []string{}
	if !confirmed {
		codes = append(codes, "unconfirmed_ownership")
	}
	if op.State != "orphan" && op.State != "committed" {
		codes = append(codes, "creation_unresolved")
	}

	if op.ClaimedUntil.After(s.secretNow()) || s.credentialCreationHeld(op.RequestID) {
		codes = append(codes, "active_creation")
	}
	var live int64
	if e := personalExact(vaultDB(tx).Model(&entity.ProviderCredential{}), "id", op.CredentialID).Count(&live).Error; e != nil {
		return ProviderCredentialOrphanView{}, e
	}
	if live != 0 {
		codes = append(codes, "live_credential")
	}
	if op.State != "committed" {
		var refs int64
		if e := vaultDB(tx).Model(&entity.CredentialVaultReference{}).Where("credential_id = ? OR reference_id = ?", op.CredentialID, op.ReferenceID).Count(&refs).Error; e != nil {
			return ProviderCredentialOrphanView{}, e
		}
		if refs != 0 {
			codes = append(codes, "retained_reference")
		}
	}
	var dispositions []entity.ProviderCredentialCleanup
	if e := personalExact(vaultDB(tx), "creation_request_id", op.RequestID).Limit(1).Find(&dispositions).Error; e != nil {
		return ProviderCredentialOrphanView{}, e
	}
	if len(dispositions) != 0 && !claiming {
		codes = append(codes, "cleanup_claimed")
	}
	if _, w, _, e := s.credentialReferenceClientTx(tx, operationReference(op)); e != nil || w.SecretGeneration != op.WriterGeneration {
		codes = append(codes, "source_unavailable")
	}
	var publishedProof string
	if op.State == "committed" {
		_, proof, code, err := s.publishedCleanupPreview(tx, op, claiming)
		if err != nil {
			return ProviderCredentialOrphanView{}, err
		}
		publishedProof = proof
		if code != "" {
			codes = append(codes, code)
		}
	} else {
		processOwned, e := s.cleanupProcessOwned(tx, op.RequestID)
		if e != nil {
			return ProviderCredentialOrphanView{}, e
		}
		if !processOwned {
			codes = append(codes, "process_ownership_unknown")
		}
		sole, e := s.cleanupSoleInstance(tx)
		if e != nil {
			return ProviderCredentialOrphanView{}, e
		}
		if !sole {
			codes = append(codes, "fleet_ambiguous")
		}

	}
	can, e := exactGovernancePermission(tx, actor, "secrets.write")
	if e != nil {
		return ProviderCredentialOrphanView{}, e
	}
	providerWrite, e := exactGovernancePermission(tx, actor, "providers.write")
	if e != nil {
		return ProviderCredentialOrphanView{}, e
	}
	state := op.State
	if state == "writing" && !s.secretNow().Before(op.ClaimedUntil) {
		state = "unknown"
	}
	v := ProviderCredentialOrphanView{op.RequestID, op.Kind, op.ProviderID, op.ConnectionID, op.CredentialID, op.IntegrationID, op.RevisionID, op.CreatedAt, state, w, r, confirmed, len(codes) == 0, codes, can && providerWrite, ""}
	content := connectionMetadataHash(struct {
		View            ProviderCredentialOrphanView
		Operation       string
		Claim           string
		Until           time.Time
		CurrentRevision string
		Disposition     []entity.ProviderCredentialCleanup
		Epoch           uint64
		PublishedProof  string
	}{v, cleanupOperationProof(op), op.Claim, op.ClaimedUntil, row.RevisionID, dispositions, s.secretEpoch(), publishedProof})
	// Entity JSON intentionally hides private ledger fields, so bind disposition
	// changes independently rather than relying on its public serialization.
	if len(dispositions) != 0 {
		content = rootHash(content + ":" + dispositions[0].RequestID + ":" + dispositions[0].State)
	}
	v.ReviewETag = vaultIdentity(actor, row) + "." + content
	return v, nil
}

// This transaction helper never decrypts or performs HTTP. It binds current
// configuration presence to the original retained descriptor/reader generation.
func (s *Service) credentialReferenceClientTx(tx *gorm.DB, ref entity.CredentialVaultReference) (entity.VaultRevision, entity.VaultWriterAuth, entity.VaultReaderAuth, error) {
	var rev entity.VaultRevision
	var w entity.VaultWriterAuth
	var r entity.VaultReaderAuth
	row, _, cw, cr, e := vaultSnapshot(tx, ref.IntegrationID, false)
	if e != nil {
		return rev, w, r, e
	}
	if !row.CreatedAt.Equal(ref.IntegrationBirth) || cw.AuthCiphertext == "" || cr.AuthCiphertext == "" {
		return rev, w, r, vaultUnavailable
	}
	for _, v := range []any{&rev, &w, &r} {
		if e = personalExact(vaultDB(tx), "id", ref.RevisionID).Take(v).Error; e != nil {
			return rev, w, r, e
		}
	}
	if rev.ID != ref.RevisionID || rev.IntegrationID != row.ID || !rev.IntegrationBirth.Equal(row.CreatedAt) || w.ID != rev.ID || r.ID != rev.ID || r.SecretGeneration != ref.ReaderGeneration || w.AuthCiphertext == "" || r.AuthCiphertext == "" || !vaultStoredMethod(w.Method) || !vaultStoredMethod(r.Method) {
		return rev, w, r, vaultUnavailable
	}
	client, e := vaultClient(rev, s.allowPrivateUpstream)
	if e != nil {
		return rev, w, r, e
	}
	defer client.Close()
	if !vaultProbeID.MatchString(ref.ReferenceID) || !personalModelETag(ref.ExpectedMarkerSHA256) || !personalModelETag(ref.DescriptorSHA256) {
		return rev, w, r, vaultUnavailable
	}
	prepared, e := client.PrepareCredential()
	if e != nil {
		return rev, w, r, vaultUnavailable
	}
	defer prepared.Close()
	if prepared.Plan().DescriptorSHA256 != ref.DescriptorSHA256 {
		return rev, w, r, vaultUnavailable
	}
	return rev, w, r, nil
}
func cleanupCursor(actor entity.User, row entity.VaultIntegration, request string) string {
	return request + "." + rootHash(vaultIdentity(actor, row)+":"+request)
}
func (s *Service) ListProviderCredentialOrphans(ctx context.Context, actorID, target, cursor string, limit int) (*ProviderCredentialOrphanPage, error) {
	if !vaultIntegrationID.MatchString(target) || limit < 1 || limit > 50 {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	page := &ProviderCredentialOrphanPage{Items: []ProviderCredentialOrphanView{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := cleanupAuthorize(tx, actorID, false)
		if e != nil {
			return e
		}
		row, _, _, _, e := vaultSnapshot(tx, target, false)
		if e != nil {
			return e
		}
		after := ""
		if cursor != "" {
			parts := strings.Split(cursor, ".")
			if len(parts) != 2 || !credentialReplacementRequestID.MatchString(parts[0]) || cleanupCursor(actor, row, parts[0]) != cursor {
				return apperrors.ErrBadRequest
			}
			after = parts[0]
		}
		q := personalExact(vaultDB(tx), "integration_id", target).Where("storage_source = ? AND integration_birth = ?", "vault", row.CreatedAt)
		col := clause.Column{Name: "request_id"}
		if after != "" {
			q = q.Where(database.ByteAfter(tx, col, after))
		}
		var ops []entity.CredentialStorageOperation
		if e = q.Clauses(clause.OrderBy{Expression: database.ByteOrder(tx, col)}).Limit(limit + 1).Find(&ops).Error; e != nil {
			return e
		}
		if len(ops) > limit {
			next := cleanupCursor(actor, row, ops[limit-1].RequestID)
			page.NextCursor = &next
			ops = ops[:limit]
		}
		for _, op := range ops {
			if op.IntegrationID != row.ID || op.IntegrationBirth == nil || !op.IntegrationBirth.Equal(row.CreatedAt) {
				return vaultUnavailable
			}
			v, e := s.cleanupView(tx, actor, row, op, false)
			if e != nil {
				return e
			}
			page.Items = append(page.Items, v)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return page, vaultError(err)
}
func (s *Service) GetProviderCredentialOrphan(ctx context.Context, actorID, target, request string) (*ProviderCredentialOrphanView, error) {
	if !vaultIntegrationID.MatchString(target) || !credentialReplacementRequestID.MatchString(request) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var v ProviderCredentialOrphanView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := cleanupAuthorize(tx, actorID, false)
		if e != nil {
			return e
		}
		row, _, _, _, e := vaultSnapshot(tx, target, false)
		if e != nil {
			return e
		}
		op, e := cleanupOperation(tx, row, request)
		if e != nil {
			return e
		}
		v, e = s.cleanupView(tx, actor, row, op, false)
		return e
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return &v, vaultError(err)
}
func (s *Service) GetProviderCredentialCleanup(ctx context.Context, actorID, target, request, command string) (*ProviderCredentialCleanupReceipt, error) {
	if !credentialReplacementRequestID.MatchString(command) || !credentialReplacementRequestID.MatchString(request) || !vaultIntegrationID.MatchString(target) {
		return nil, apperrors.ErrBadRequest
	}
	var v ProviderCredentialCleanupReceipt
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, row, op, e := cleanupTargetTx(tx, actorID, target, request, false)
		if e != nil {
			return e
		}
		ledger, _, e := cleanupCommandTx(tx, command)
		if e != nil {
			return e
		}
		if ledger.CreationRequestID != request || ledger.RequestID != command || ledger.IntegrationID != row.ID || !ledger.IntegrationBirth.Equal(row.CreatedAt) || ledger.OperationProof != cleanupOperationProof(op) {
			return apperrors.ErrNotFound
		}
		v, e = cleanupReceipt(ledger, s.secretNow())
		return e
	})
	return &v, vaultError(err)
}
func cleanupTargetTx(tx *gorm.DB, actorID, target, request string, write bool) (entity.User, entity.VaultIntegration, entity.CredentialStorageOperation, error) {
	actor, e := cleanupAuthorize(tx, actorID, write)
	if e != nil {
		return actor, entity.VaultIntegration{}, entity.CredentialStorageOperation{}, e
	}
	row, _, _, _, e := vaultSnapshot(tx, target, true)
	if e != nil {
		return actor, row, entity.CredentialStorageOperation{}, e
	}
	op, e := cleanupOperation(tx, row, request)
	return actor, row, op, e
}
func cleanupReplay(ledger entity.ProviderCredentialCleanup, actor entity.User, op entity.CredentialStorageOperation, review string, input ProviderCredentialCleanupInput) bool {
	return ledger.CreationRequestID == op.RequestID && ledger.RequestID == input.RequestID && ledger.ActorID == actor.ID && ledger.ActorBirth.Equal(actor.CreatedAt) && ledger.IntegrationID == op.IntegrationID && op.IntegrationBirth != nil && ledger.IntegrationBirth.Equal(*op.IntegrationBirth) && ledger.OperationProof == cleanupOperationProof(op) && ledger.ReviewedETag == review && ledger.Reason == input.Reason
}
func (s *Service) CleanupProviderCredentialOrphan(ctx context.Context, actorID, target, request, review string, input ProviderCredentialCleanupInput) (*ProviderCredentialCleanupResult, error) {
	defer func() { input.cleanupToken = "" }()
	if !vaultIntegrationID.MatchString(target) || !credentialReplacementRequestID.MatchString(request) || !credentialReplacementRequestID.MatchString(input.RequestID) || input.RequestID == request || !vaultStrong(review) || !rootReason(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	release, e := s.vaultReadLease()
	if e != nil {
		return nil, e
	}
	defer release()
	var op entity.CredentialStorageOperation
	var ledger entity.ProviderCredentialCleanup
	var replay bool
	var denial entity.CredentialSourceDenial
	var releaseLocal func()
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		actor, row, loaded, e := cleanupTargetTx(tx, actorID, target, request, true)
		if e != nil {
			return e
		}
		op = loaded
		var published bool
		ledger, published, e = cleanupCommandTx(tx, input.RequestID)
		if e == nil {
			if published != (op.State == "committed") || !cleanupReplay(ledger, actor, op, review, input) {
				return catalogConflict
			}
			replay = true
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		// V80 remains a singleton per creation, including its old immutable
		// failed receipts. Only V88 published commands support safe new intents.
		var old []entity.ProviderCredentialCleanup
		if e = personalExact(vaultDB(tx), "creation_request_id", request).Limit(1).Find(&old).Error; e != nil {
			return e
		}
		if len(old) != 0 {
			return catalogConflict
		}
		if !vaultToken(input.cleanupToken) {
			return apperrors.ErrBadRequest
		}
		releaseLocal, e = s.credentialCleanupLease(request)
		if e != nil {
			return e
		}
		v, e := s.cleanupView(tx, actor, row, op, false)
		if e != nil {
			return e
		}
		if v.ReviewETag != review || !v.Eligible || !v.CanCleanup {
			return catalogConflict
		}
		now := s.secretNow().UTC().Truncate(time.Microsecond)
		ledger = entity.ProviderCredentialCleanup{CreationRequestID: request, RequestID: input.RequestID, ActorID: actor.ID, ActorBirth: actor.CreatedAt, IntegrationID: row.ID, IntegrationBirth: row.CreatedAt, RevisionID: op.RevisionID, OperationProof: cleanupOperationProof(op), ReviewedETag: review, Reason: input.Reason, State: "pending", OwnershipJSON: vaultJSON(vaultEmptyObservation()), CleanupJSON: vaultJSON(VaultCleanupView{"not_attempted", vaultEmptyObservation()}), RootEpoch: s.secretEpoch(), StartedAt: now, Deadline: now.Add(30 * time.Second)}
		if op.State == "committed" {
			denial, e = s.claimPublishedCleanup(tx, op, ledger)
			if e != nil {
				return e
			}
			command := publishedCommand(ledger, denial.PhysicalObject)
			e = vaultDB(tx).Create(&command).Error
		} else {
			e = vaultDB(tx).Create(&ledger).Error
		}
		if e != nil {
			if errors.Is(e, gorm.ErrDuplicatedKey) {
				return catalogConflict
			}
			return e
		}
		return appendProviderCleanupAudit(tx, actor.ID, "credential.orphan.cleanup.claim", op, input.RequestID)
	})
	if releaseLocal != nil {
		defer releaseLocal()
	}
	if err != nil {
		return nil, vaultError(err)
	}
	if replay {
		v, e := cleanupReceipt(ledger, s.secretNow())
		return &ProviderCredentialCleanupResult{v, v.State == "pending"}, e
	}
	result := vault.CredentialCleanupResult{Cleanup: vault.Cleanup{State: "not_attempted"}}
	// This private positive control-flow fact is never inferred from a timeout,
	// HTTP response, or empty observations. Once the remote claim path is entered,
	// any uncertainty permanently blocks a fresh command for this object.
	remoteBoundaryEntered := false
	// Recheck fresh actor/source/instance after the durable command claim. Root
	// retirement cannot advance while this reader lease remains held.
	if op.State == "committed" {
		err = s.joinPublishedCleanup(ctx, denial, op)
	}
	if err == nil {
		err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
			if op.State == "committed" {
				if err := lockGovernance(tx); err != nil {
					return err
				}
			}
			_, _, live, e := cleanupTargetTx(tx, actorID, target, request, true)
			if e != nil {
				return e
			}
			if cleanupOperationProof(live) != ledger.OperationProof || s.secretEpoch() != ledger.RootEpoch {
				return catalogConflict
			}
			if op.State == "committed" {
				object, _, code, e := s.publishedCleanupPreview(tx, live, true)
				if e != nil {
					return e
				}
				if code != "" || object != denial.PhysicalObject || !s.credentialSources.joined(object) {
					return vaultUnavailable
				}
				return nil
			}
			sole, e := s.cleanupSoleInstance(tx)
			if e != nil {
				return e
			}
			if !sole {
				return vaultUnavailable
			}
			owned, e := s.cleanupProcessOwned(tx, request)
			if e != nil {
				return e
			}
			if !owned {
				return catalogConflict
			}
			return nil
		})
	}
	if err == nil {
		rev, w, r, e := s.credentialReferenceRevision(ctx, operationReference(op))
		if e == nil {
			var operation *credentialFiniteOperation
			var operationErr error
			if op.State == "committed" {
				remoteBoundaryEntered = true
				operation, operationErr = s.publishedCleanupOperation(ctx, rev, operationReference(op), r.Method, op, ledger)
			} else {
				operation, operationErr = s.credentialFiniteOperation(rev, operationReference(op), r.Method, "cleanup")
			}
			if operationErr != nil {
				err = operationErr
			} else {
				defer operation.close()
				wt, rt, openErr := s.vaultOpen(w, r)
				if openErr == nil && (w.Method != "token" || wt != input.cleanupToken) {
					token, closeToken, login, loginErr := operation.token(ctx, r.Method, rt)
					if loginErr == nil {
						result, err = operation.cleanup(ctx, token, input.cleanupToken, credentialPlan(operationReference(op)))
						closeToken()
						if op.State == "committed" && err != nil && result.Cleanup.State == "acknowledged" {
							result.Cleanup.State = "unknown"
						}
					} else {
						result.Ownership = vault.Observation{Attempted: false, Failure: &vault.Failure{Stage: "prepare", Code: "invalid_auth"}}
						_ = login
						err = loginErr
					}
				} else {
					err = vaultUnavailable
				}
			}
		} else {
			err = e
		}
	}
	// A committed remote observation is saved even when the caller cancels or
	// authority changes. No detached retry; failure to persist remains unknown.
	state := "failed"
	switch result.Cleanup.State {
	case "acknowledged":
		state = "acknowledged"
	case "unknown":
		state = "unknown"
	}
	ledger.State = state
	ledger.OwnershipJSON = vaultJSON(vaultObservation(result.Ownership))
	ledger.CleanupJSON = vaultJSON(VaultCleanupView{result.Cleanup.State, vaultObservation(result.Cleanup.Observation)})
	now := s.secretNow().UTC().Truncate(time.Microsecond)
	ledger.FinishedAt = &now
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer finishCancel()
	saveErr := s.authDB(finishCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		if op.State == "committed" {
			if e := s.finishPublishedCleanup(tx, ledger, denial.PhysicalObject, !remoteBoundaryEntered); e != nil {
				return e
			}
		} else {
			updated := vaultDB(tx).Model(&entity.ProviderCredentialCleanup{}).Where("creation_request_id = ? AND request_id = ? AND state = ?", request, input.RequestID, "pending").Updates(map[string]any{"state": ledger.State, "ownership_json": ledger.OwnershipJSON, "cleanup_json": ledger.CleanupJSON, "finished_at": ledger.FinishedAt})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return catalogConflict
			}
		}
		return appendProviderCleanupAudit(tx, actorID, "credential.orphan.cleanup.observe", op, input.RequestID)
	})
	if saveErr != nil {
		return nil, vaultUnavailable
	}
	// A known recorded failed/unknown result remains a receipt, never success.
	_ = err
	v, e := cleanupReceipt(ledger, s.secretNow())
	if e != nil {
		return nil, e
	}
	if _, _, _, e = cleanupTarget(ctx, s, actorID, target, request, true); e != nil {
		return nil, vaultError(e)
	}
	return &ProviderCredentialCleanupResult{v, false}, nil
}

func (s *Service) cleanupProcessGeneration() (string, string) {
	s.instanceMu.RLock()
	defer s.instanceMu.RUnlock()
	if s.instance == nil {
		return "", ""
	}
	return s.instance.id, rootHash(s.instance.id + ":" + s.instance.token)
}
func (s *Service) recordCredentialCreationUse(tx *gorm.DB, request string, initial bool) error {
	process, generation := s.cleanupProcessGeneration()
	if initial {
		return vaultDB(tx).Create(&entity.ProviderCredentialCreationUse{CreationRequestID: request, ProcessID: process, ProcessGeneration: generation, Exposed: process == ""}).Error
	}
	var rows []entity.ProviderCredentialCreationUse
	if e := personalExact(vaultDB(tx), "creation_request_id", request).Limit(1).Find(&rows).Error; e != nil {
		return e
	}
	if len(rows) == 0 {
		return nil
	} // Historical absence never becomes local ownership.
	row := rows[0]
	if row.CreationRequestID != request {
		return vaultUnavailable
	}
	if !row.Exposed && (process == "" || row.ProcessID != process || row.ProcessGeneration != generation) {
		return vaultDB(tx).Model(&entity.ProviderCredentialCreationUse{}).Where("creation_request_id = ?", request).Update("exposed", true).Error
	}
	return nil
}
func (s *Service) cleanupProcessOwned(tx *gorm.DB, request string) (bool, error) {
	process, generation := s.cleanupProcessGeneration()
	if process == "" {
		return false, nil
	}
	var rows []entity.ProviderCredentialCreationUse
	if e := personalExact(vaultDB(tx), "creation_request_id", request).Limit(1).Find(&rows).Error; e != nil {
		return false, e
	}
	return len(rows) == 1 && rows[0].CreationRequestID == request && rows[0].ProcessID == process && rows[0].ProcessGeneration == generation && !rows[0].Exposed, nil
}

// The planned canonical Credential identity fits the retained audit resource
// domain. UUID command/creation identities belong in bounded nonsecret details,
// not the released 30-character ResourceID column.
func appendProviderCleanupAudit(tx *gorm.DB, actorID, action string, op entity.CredentialStorageOperation, commandID string) error {
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	detail, err := json.Marshal(struct {
		CreationRequestID string `json:"creation_request_id"`
		RequestID         string `json:"request_id"`
	}{op.RequestID, commandID})
	if err != nil {
		return err
	}
	encoded := string(detail)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: action, ResourceType: "credential", ResourceID: op.CredentialID, DetailsJSON: &encoded}).Error
}
