package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type credentialCreationIntent struct {
	PolicyETag     string
	Kind           string
	Target         string
	ProviderName   string
	Connection     CreateConnectionInput
	CredentialName string
	Priority       int
	SourceETag     string
	Reason         string
}

// created is a transaction observation, not a retained receipt field.
type credentialCreationResult struct {
	entity.CredentialStorageOperation
	created bool
}

func credentialCreationJSON(i credentialCreationIntent) string {
	i.Connection.Secret = ""
	i.Connection.RequestID = ""
	raw, _ := json.Marshal(i)
	return string(raw)
}
func operationReference(o entity.CredentialStorageOperation) entity.CredentialVaultReference {
	if o.IntegrationBirth == nil {
		return entity.CredentialVaultReference{}
	}
	return entity.CredentialVaultReference{CredentialID: o.CredentialID, CredentialBirth: o.CredentialBirth, ReferenceID: o.ReferenceID, ExpectedMarkerSHA256: o.ExpectedMarkerSHA256, DescriptorSHA256: o.DescriptorSHA256, IntegrationID: o.IntegrationID, IntegrationBirth: *o.IntegrationBirth, RevisionID: o.RevisionID, ReaderGeneration: o.ReaderGeneration}
}

// Retries claim only an ownership Read, never another Write. Claims expiring
// after process/response loss remain unknown until a fresh authorized retry.
func (s *Service) createVaultCredential(ctx context.Context, actorID, requestID, secret string, intent credentialCreationIntent) (*credentialCreationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if !credentialReplacementRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	if !utf8.ValidString(secret) || secret == "" || len(secret) > 2048 || strings.ContainsAny(secret, "\r\n") {
		return nil, apperrors.ErrBadRequest
	}
	release, e := s.vaultReadLease()
	if e != nil {
		return nil, e
	}
	defer release()
	commandEpoch := s.secretEpoch()
	var op entity.CredentialStorageOperation
	var prepared *vault.PreparedCredential
	var client *vault.Client
	var writer entity.VaultWriterAuth
	var reader entity.VaultReaderAuth
	var proposedProvider entity.Provider
	var proposedConnection entity.ProviderConnection
	var proposedCredential entity.ProviderCredential
	dispatchWrite := false
	claim, e := id.NewPrefixed("ccl")
	if e != nil {
		return nil, e
	}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		if e = exactCatalogPermission(tx, actorID, "providers.write"); e != nil {
			return e
		}
		prior := personalExact(vaultDB(tx), "request_id", requestID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&op).Error
		if prior == nil {
			if op.RequestID != requestID || op.ActorID != actor.ID || !op.ActorBirth.Equal(actor.CreatedAt) || op.IntentJSON != credentialCreationJSON(intent) {
				return catalogConflict
			}
			if op.StorageSource != "vault" || op.ClaimedUntil.After(s.secretNow()) {
				return catalogConflict
			}
			// Preserve original source even after a future-write policy change.
			op.Claim = claim
			op.ClaimedUntil = s.secretNow().Add(30 * time.Second)
			return vaultDB(tx).Model(&entity.CredentialStorageOperation{}).Where("request_id = ?", requestID).Select("claim", "claimed_until").Updates(&op).Error
		}
		if !errors.Is(prior, gorm.ErrRecordNotFound) {
			return prior
		}
		policy, e := credentialStoragePolicy(tx, true)
		if e != nil {
			return e
		}
		if policy.Mode != "vault" || credentialStorageContext(actor, policy).ETag != intent.PolicyETag {
			return catalogConflict
		}
		row, rev, w, r, e := vaultSnapshot(tx, *policy.IntegrationID, true)
		if e != nil {
			return e
		}
		if rev.ID != *policy.RevisionID || !row.CreatedAt.Equal(*policy.IntegrationBirth) || w.AuthCiphertext == "" || r.AuthCiphertext == "" || !vaultStoredMethod(w.Method) || !vaultStoredMethod(r.Method) {
			return catalogConflict
		}
		writer, reader = w, r
		client, e = vaultClient(rev, s.allowPrivateUpstream)
		if e != nil {
			return e
		}
		prepared, e = client.PrepareCredential()
		if e != nil {
			return vaultUnavailable
		}
		proposedProvider, proposedConnection, proposedCredential, e = s.credentialCreationObjects(tx, intent)
		if e != nil {
			return e
		}
		targetBirth, e := credentialCreationTargetBirth(tx, intent, proposedProvider, proposedConnection)
		if e != nil {
			return e
		}
		plan := prepared.Plan()
		now := s.secretNow()
		op = entity.CredentialStorageOperation{StorageSource: "vault", RequestID: requestID, ActorID: actor.ID, ActorBirth: actor.CreatedAt, Kind: intent.Kind, TargetID: intent.Target, TargetBirth: targetBirth, PolicyGeneration: policy.Generation, IntentJSON: credentialCreationJSON(intent), CredentialID: proposedCredential.ID, CredentialBirth: now, ProviderID: proposedProvider.ID, ProviderBirth: proposedProvider.CreatedAt, ConnectionID: proposedConnection.ID, ConnectionBirth: proposedConnection.CreatedAt, ReferenceID: plan.ReferenceID, ExpectedMarkerSHA256: plan.ExpectedMarkerSHA256, DescriptorSHA256: plan.DescriptorSHA256, IntegrationID: row.ID, IntegrationBirth: &row.CreatedAt, RevisionID: rev.ID, WriterGeneration: w.SecretGeneration, ReaderGeneration: r.SecretGeneration, RootEpoch: s.secretEpoch(), State: "writing", Claim: claim, ClaimedUntil: now.Add(30 * time.Second), WriteJSON: vaultJSON(vaultEmptyObservation()), ReadJSON: vaultJSON(vaultEmptyObservation()), CreatedAt: now}
		if e = vaultDB(tx).Create(&op).Error; e != nil {
			return e
		}
		dispatchWrite = true
		return nil
	})
	if prepared != nil {
		defer prepared.Close()
	}
	if client != nil {
		defer client.Close()
	}
	if e != nil {
		return nil, catalogError(e)
	}
	ref := operationReference(op)
	if !dispatchWrite {
		client, writer, reader, e = s.credentialReferenceClient(ctx, ref)
		if e != nil {
			return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
		}
		defer client.Close()
	}
	wt, rt, e := s.vaultOpen(writer, reader)
	if e != nil || wt == "" || rt == "" {
		return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
	}
	if dispatchWrite {
		token, closeToken, login, loginErr := vaultCommandToken(ctx, client, writer.Method, wt)
		if loginErr != nil {
			op.WriteJSON, op.State = vaultJSON(vaultLoginFailure(login)), "unknown"
			_ = s.persistCredentialStage(ctx, op, claim, map[string]any{"write_json": op.WriteJSON, "state": op.State})
			return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
		}
		transient := []byte(secret)
		result, writeErr := client.WriteCredential(ctx, token, prepared, transient)
		closeToken()
		clear(transient)
		observation := vaultJSON(vaultObservation(result.Write))
		state := "awaiting_read"
		if writeErr != nil {
			state = "unknown"
		}
		if e = s.persistCredentialStage(ctx, op, claim, map[string]any{"write_json": observation, "state": state}); e != nil {
			return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
		}
		op.WriteJSON, op.State = observation, state
		if writeErr != nil {
			return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
		}
	}
	token, closeToken, login, loginErr := vaultCommandToken(ctx, client, reader.Method, rt)
	if loginErr != nil {
		op.ReadJSON, op.State = vaultJSON(vaultLoginFailure(login)), credentialUnresolvedState(op, "unknown")
		_ = s.persistCredentialStage(ctx, op, claim, map[string]any{"read_json": op.ReadJSON, "state": op.State})
		return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
	}
	value, read, e := client.ReadCredential(ctx, token, credentialPlan(ref))
	closeToken()
	if e != nil {
		_ = s.persistCredentialStage(ctx, op, claim, map[string]any{"read_json": vaultJSON(vaultObservation(read.Read)), "state": credentialUnresolvedState(op, "unknown")})
		return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
	}
	defer value.Close()
	bytes := value.Bytes()
	defer clear(bytes)
	if !sameCredentialReplacementSecret(string(bytes), secret) {
		_ = s.persistCredentialStage(ctx, op, claim, map[string]any{"state": credentialUnresolvedState(op, "unknown"), "claimed_until": s.secretNow()})
		return &credentialCreationResult{op, dispatchWrite}, catalogConflict
	}
	state := "owned"
	if op.State == "committed" {
		state = "committed"
	}
	if e = s.persistCredentialStage(ctx, op, claim, map[string]any{"read_json": vaultJSON(vaultObservation(read.Read)), "state": state}); e != nil {
		return &credentialCreationResult{op, dispatchWrite}, vaultUnavailable
	}
	e = s.commitVaultCredential(ctx, actorID, intent, &op, claim, dispatchWrite, commandEpoch)
	if e != nil {
		_ = s.persistCredentialStage(ctx, op, claim, map[string]any{"state": credentialUnresolvedState(op, "orphan")})
		return &credentialCreationResult{op, dispatchWrite}, catalogError(e)
	}
	return &credentialCreationResult{op, dispatchWrite}, nil
}
func (s *Service) persistCredentialStage(ctx context.Context, o entity.CredentialStorageOperation, claim string, fields map[string]any) error {
	// Request cancellation never creates detached cleanup or a background retry.
	if fields["state"] == "unknown" || fields["state"] == "orphan" || fields["state"] == "committed" {
		fields["claimed_until"] = s.secretNow()
	}
	q := s.authDB(ctx).Model(&entity.CredentialStorageOperation{}).Where("request_id = ? AND claim = ?", o.RequestID, claim).Updates(fields)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return catalogConflict
	}
	return nil
}
func (s *Service) credentialCreationObjects(tx *gorm.DB, i credentialCreationIntent) (entity.Provider, entity.ProviderConnection, entity.ProviderCredential, error) {
	var p entity.Provider
	var c entity.ProviderConnection
	var result entity.ProviderCredential
	var e error
	now := s.secretNow()
	if i.Kind == "provider" {
		p.ID, e = id.NewPrefixed("prv")
		p.Name = strings.TrimSpace(i.ProviderName)
		p.CreatedAt = now
		if e != nil || !validCatalogLabel(p.Name) {
			return p, c, result, apperrors.ErrBadRequest
		}
	} else {
		if i.Kind == "connection" {
			if e = personalExact(vaultDB(tx), "id", i.Target).Take(&p).Error; e != nil {
				return p, c, result, e
			}
			if p.ID != i.Target {
				return p, c, result, catalogConflict
			}
		} else {
			target := i.Target
			if i.Kind == "replacement" {
				var original entity.ProviderCredential
				if e = personalExact(vaultDB(tx), "id", i.Target).Take(&original).Error; e != nil {
					return p, c, result, e
				}
				if original.ID != i.Target || credentialMetadataRecord(original).ETag != i.SourceETag {
					return p, c, result, catalogConflict
				}
				target = original.ConnectionID
				i.Priority = original.Priority
			}
			if e = personalExact(vaultDB(tx), "id", target).Take(&c).Error; e != nil {
				return p, c, result, e
			}
			if c.ID != target || !connectionMetadataBirth(c.CreatedAt) {
				return p, c, result, catalogConflict
			}
			if e = personalExact(vaultDB(tx), "id", c.ProviderID).Take(&p).Error; e != nil {
				return p, c, result, e
			}
			if p.ID != c.ProviderID || !connectionMetadataBirth(p.CreatedAt) {
				return p, c, result, catalogConflict
			}
		}
	}
	if i.Kind == "provider" || i.Kind == "connection" {
		c, e = s.prepareConnectionMetadata(p.ID, i.Connection)
		if e != nil {
			return p, c, result, e
		}
		c.CreatedAt = now
	}
	result, e = prepareCredentialMetadata(c.ID, i.CredentialName, i.Priority)
	if e != nil {
		return p, c, result, e
	}
	result.CreatedAt = now
	result.StorageSource = "vault"
	if i.Kind == "replacement" {
		target := i.Target
		result.ReplacesCredentialID = &target
	}
	if i.Kind == "credential" || i.Kind == "replacement" {
		if e = checkCredentialName(tx, c.ID, "", result.Name); e != nil {
			return p, c, result, e
		}
	}
	if _, _, _, e = s.resolveConnectionEgress(tx, c); e != nil {
		return p, c, result, e
	}
	return p, c, result, nil
}
func (s *Service) commitVaultCredential(ctx context.Context, actorID string, i credentialCreationIntent, o *entity.CredentialStorageOperation, claim string, initialWrite bool, commandEpoch uint64) error {
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		if e = exactCatalogPermission(tx, actorID, "providers.write"); e != nil {
			return e
		}
		if !actor.CreatedAt.Equal(o.ActorBirth) {
			return catalogConflict
		}
		var live entity.CredentialStorageOperation
		if e = personalExact(vaultDB(tx), "request_id", o.RequestID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&live).Error; e != nil {
			return e
		}
		if live.Claim != claim {
			return catalogConflict
		}
		if live.State == "committed" {
			var saved entity.ProviderCredential
			if e = personalExact(vaultDB(tx), "id", o.CredentialID).Take(&saved).Error; e != nil {
				return e
			}
			if saved.ID != o.CredentialID || !saved.CreatedAt.Equal(o.CredentialBirth) || saved.StorageSource != "vault" {
				return catalogConflict
			}
			rows := []entity.ProviderCredential{saved}
			if e = attachCredentialSources(tx, rows); e != nil || credentialSourceProof(rows[0]) == "" {
				return vaultUnavailable
			}
			ref := rows[0].VaultReference
			if saved.ConnectionID != o.ConnectionID || ref == nil || ref.ReferenceID != o.ReferenceID || ref.DescriptorSHA256 != o.DescriptorSHA256 || ref.ReaderGeneration != o.ReaderGeneration {
				return catalogConflict
			}
			var conn entity.ProviderConnection
			if e = personalExact(vaultDB(tx), "id", o.ConnectionID).Take(&conn).Error; e != nil {
				return e
			}
			if conn.ID != o.ConnectionID || !conn.CreatedAt.Equal(o.ConnectionBirth) || conn.ProviderID != o.ProviderID {
				return catalogConflict
			}
			if e = vaultDB(tx).Model(&live).Updates(map[string]any{"claimed_until": s.secretNow()}).Error; e != nil {
				return e
			}
			o.State = "committed"
			return nil
		}
		if initialWrite {
			policy, e := credentialStoragePolicy(tx, true)
			if e != nil {
				return e
			}
			if policy.Generation != o.PolicyGeneration {
				return catalogConflict
			}
		}
		if s.secretEpoch() != commandEpoch || initialWrite && commandEpoch != o.RootEpoch {
			return catalogConflict
		}
		// Recovery keeps the original descriptor/auth generations. Current
		// configuration presence still revokes admission; it never repoints a plan.
		row, _, cw, cr, e := vaultSnapshot(tx, o.IntegrationID, true)
		if e != nil {
			return e
		}
		if initialWrite && row.RevisionID != o.RevisionID {
			return catalogConflict
		}
		var rev entity.VaultRevision
		var w entity.VaultWriterAuth
		var r entity.VaultReaderAuth
		for _, value := range []any{&rev, &w, &r} {
			if e := personalExact(vaultDB(tx), "id", o.RevisionID).Take(value).Error; e != nil {
				return e
			}
		}
		if o.IntegrationBirth == nil || !row.CreatedAt.Equal(*o.IntegrationBirth) || cw.AuthCiphertext == "" || cr.AuthCiphertext == "" || rev.ID != o.RevisionID || rev.IntegrationID != o.IntegrationID || !rev.IntegrationBirth.Equal(*o.IntegrationBirth) || w.ID != o.RevisionID || r.ID != o.RevisionID || w.SecretGeneration != o.WriterGeneration || r.SecretGeneration != o.ReaderGeneration || w.AuthCiphertext == "" || r.AuthCiphertext == "" {
			return catalogConflict
		}
		if !vaultStoredMethod(cw.Method) || !vaultStoredMethod(cr.Method) {
			return catalogConflict
		}
		if _, _, e = s.vaultOpen(w, r); e != nil {
			return catalogConflict
		}
		p, c, credential, e := s.credentialCreationObjects(tx, i)
		if e != nil {
			return e
		}
		if i.Kind == "provider" || i.Kind == "connection" {
			c.ID = o.ConnectionID
			c.CreatedAt = o.ConnectionBirth
			p.ID = o.ProviderID
			p.CreatedAt = o.ProviderBirth
			c.ProviderID = p.ID
		}
		if i.Kind == "credential" || i.Kind == "replacement" {
			if c.ID != o.ConnectionID || !c.CreatedAt.Equal(o.ConnectionBirth) {
				return catalogConflict
			}
		}
		if i.Kind != "provider" && (p.ID != o.ProviderID || !p.CreatedAt.Equal(o.ProviderBirth)) {
			return catalogConflict
		}
		if i.Kind == "replacement" {
			birth, err := credentialCreationTargetBirth(tx, i, p, c)
			if err != nil {
				return err
			}
			if !birth.Equal(o.TargetBirth) {
				return catalogConflict
			}
		}
		credential.ID = o.CredentialID
		credential.CreatedAt = o.CredentialBirth
		credential.ConnectionID = c.ID
		if i.Kind == "provider" {
			if e = vaultDB(tx).Create(&p).Error; e != nil {
				return e
			}
		}
		if i.Kind == "provider" || i.Kind == "connection" {
			if e = vaultDB(tx).Create(&c).Error; e != nil {
				return e
			}
		}
		if e = vaultDB(tx).Create(&credential).Error; e != nil {
			return e
		}
		ref := operationReference(*o)
		if e = vaultDB(tx).Create(&ref).Error; e != nil {
			return e
		}
		if i.Kind == "replacement" {
			receipt := entity.CredentialReplacementReceipt{RequestID: o.RequestID, ActorID: actorID, SourceCredentialID: i.Target, ConnectionID: c.ID, ResultCredentialID: credential.ID, RequestHash: credentialReplacementHash(actorID, i.Target, i.SourceETag, CredentialReplacementInput{RequestID: o.RequestID, Name: i.CredentialName, Reason: i.Reason, StoragePolicyETag: i.PolicyETag})}
			if e = vaultDB(tx).Create(&receipt).Error; e != nil {
				return e
			}
			if e = appendCredentialReplacementAudit(tx, actorID, i.Target, credential, i.Reason); e != nil {
				return e
			}
		} else if e = appendAudit(tx, actorID, "credential.create", "credential", credential.ID); e != nil {
			return e
		}
		if e = vaultDB(tx).Model(&live).Updates(map[string]any{"state": "committed", "claimed_until": s.secretNow()}).Error; e != nil {
			return e
		}
		o.State = "committed"
		return nil
	})
}

func (s *Service) useVaultCreation(ctx context.Context, actorID, requestID, etag string) (bool, error) {
	if requestID != "" && !credentialReplacementRequestID.MatchString(requestID) || etag != "" && !personalModelETag(etag) {
		return false, apperrors.ErrBadRequest
	}
	var mode bool
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		if e = exactCatalogPermission(tx, actorID, "providers.write"); e != nil {
			return e
		}
		if requestID != "" {
			var op entity.CredentialStorageOperation
			e := personalExact(vaultDB(tx), "request_id", requestID).Take(&op).Error
			if e == nil {
				if op.StorageSource != "vault" && op.StorageSource != "inline" {
					return vaultUnavailable
				}
				mode = op.StorageSource == "vault"
				return nil
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
		}
		policy, e := credentialStoragePolicy(tx, false)
		if e != nil {
			return e
		}
		mode = policy.Mode == "vault"
		if mode && (requestID == "" || etag == "") {
			return apperrors.ErrBadRequest
		}
		if etag != "" && credentialStorageContext(actor, policy).ETag != etag {
			return catalogConflict
		}
		return nil
	})
	return mode, catalogError(e)
}

func requireInlineCredentialPolicy(tx *gorm.DB) error {
	p, e := credentialStoragePolicy(tx, true)
	if e != nil {
		return e
	}
	if p.Mode != "inline" {
		return catalogConflict
	}
	return nil
}

func (s *Service) createInlineCredential(ctx context.Context, actorID, requestID, secret string, i credentialCreationIntent) (*credentialCreationResult, error) {
	if secret == "" || len(secret) > 2048 || strings.ContainsAny(secret, "\r\n") {
		return nil, apperrors.ErrBadRequest
	}
	var op entity.CredentialStorageOperation
	created := false
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		if e = exactCatalogPermission(tx, actorID, "providers.write"); e != nil {
			return e
		}
		prior := personalExact(vaultDB(tx), "request_id", requestID).Take(&op).Error
		if prior == nil {
			if op.StorageSource != "inline" || op.RequestID != requestID || op.ActorID != actor.ID || !op.ActorBirth.Equal(actor.CreatedAt) || op.IntentJSON != credentialCreationJSON(i) || op.State != "committed" {
				return catalogConflict
			}
			var c entity.ProviderCredential
			if e = personalExact(vaultDB(tx), "id", op.CredentialID).Take(&c).Error; e != nil {
				return e
			}
			if c.ID != op.CredentialID || !c.CreatedAt.Equal(op.CredentialBirth) || c.StorageSource != "inline" || c.ConnectionID != op.ConnectionID {
				return catalogConflict
			}
			original, e := s.openSecret(c.ID, c.Ciphertext)
			if e != nil {
				return e
			}
			if !sameCredentialReplacementSecret(original, secret) {
				return catalogConflict
			}
			return nil
		}
		if !errors.Is(prior, gorm.ErrRecordNotFound) {
			return prior
		}
		policy, e := credentialStoragePolicy(tx, true)
		if e != nil {
			return e
		}
		if policy.Mode != "inline" || i.PolicyETag != "" && credentialStorageContext(actor, policy).ETag != i.PolicyETag {
			return catalogConflict
		}
		provider, connection, c, e := s.credentialCreationObjects(tx, i)
		if e != nil {
			return e
		}
		c.StorageSource = "inline"
		epoch := s.secretEpoch()
		c.Ciphertext, e = s.sealSecret(c.ID, secret)
		if e != nil {
			return e
		}
		if e = s.guardSecretWrite(tx, epoch, c.ID, c.Ciphertext); e != nil {
			return e
		}
		if i.Kind == "provider" {
			if e = vaultDB(tx).Create(&provider).Error; e != nil {
				return e
			}
		}
		if i.Kind == "provider" || i.Kind == "connection" {
			if e = vaultDB(tx).Create(&connection).Error; e != nil {
				return e
			}
		}
		if e = vaultDB(tx).Create(&c).Error; e != nil {
			return e
		}
		if i.Kind == "replacement" {
			receipt := entity.CredentialReplacementReceipt{RequestID: requestID, ActorID: actorID, SourceCredentialID: i.Target, ConnectionID: c.ConnectionID, ResultCredentialID: c.ID, RequestHash: credentialReplacementHash(actorID, i.Target, i.SourceETag, CredentialReplacementInput{RequestID: requestID, Name: i.CredentialName, Reason: i.Reason, StoragePolicyETag: i.PolicyETag})}
			if e = vaultDB(tx).Create(&receipt).Error; e != nil {
				return e
			}
			if e = appendCredentialReplacementAudit(tx, actorID, i.Target, c, i.Reason); e != nil {
				return e
			}
		} else if e = appendAudit(tx, actorID, "credential.create", "credential", c.ID); e != nil {
			return e
		}
		targetBirth, err := credentialCreationTargetBirth(tx, i, provider, connection)
		if err != nil {
			return err
		}
		now := s.secretNow()
		op = entity.CredentialStorageOperation{StorageSource: "inline", RequestID: requestID, ActorID: actor.ID, ActorBirth: actor.CreatedAt, Kind: i.Kind, TargetID: i.Target, TargetBirth: targetBirth, PolicyGeneration: policy.Generation, IntentJSON: credentialCreationJSON(i), CredentialID: c.ID, CredentialBirth: c.CreatedAt, ProviderID: provider.ID, ProviderBirth: provider.CreatedAt, ConnectionID: connection.ID, ConnectionBirth: connection.CreatedAt, ReferenceID: strings.ReplaceAll(requestID, "-", ""), RootEpoch: epoch, State: "committed", WriteJSON: vaultJSON(vaultEmptyObservation()), ReadJSON: vaultJSON(vaultEmptyObservation()), ClaimedUntil: now, CreatedAt: now}
		if err := vaultDB(tx).Create(&op).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return &credentialCreationResult{op, created}, catalogError(e)
}

type CredentialStorageOperationView struct {
	RequestID     string               `json:"request_id"`
	StorageSource string               `json:"storage_source"`
	Kind          string               `json:"kind"`
	TargetID      string               `json:"target_id"`
	CredentialID  string               `json:"credential_id"`
	State         string               `json:"state"`
	Write         VaultObservationView `json:"write"`
	Read          VaultObservationView `json:"read"`
}

func (s *Service) GetCredentialStorageOperation(ctx context.Context, actorID, requestID string) (*CredentialStorageOperationView, error) {
	if !credentialReplacementRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	var v *CredentialStorageOperationView
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		if e = exactCatalogPermission(tx, actorID, "providers.write"); e != nil {
			return e
		}
		var op entity.CredentialStorageOperation
		if e = personalExact(vaultDB(tx), "request_id", requestID).Take(&op).Error; e != nil {
			return e
		}
		if op.ActorID != actor.ID || !op.ActorBirth.Equal(actor.CreatedAt) || op.RequestID != requestID {
			return apperrors.ErrNotFound
		}
		var write, read VaultObservationView
		if !vaultDecodeObservation(op.WriteJSON, &write) || !vaultDecodeObservation(op.ReadJSON, &read) {
			return vaultUnavailable
		}
		state := op.State
		if state == "writing" && s.secretNow().After(op.ClaimedUntil) {
			state = "unknown"
		}
		v = &CredentialStorageOperationView{op.RequestID, op.StorageSource, op.Kind, op.TargetID, op.CredentialID, state, write, read}
		return nil
	})
	return v, catalogError(e)
}

// Business persistence remains committed after a failed current ownership read.
func credentialUnresolvedState(o entity.CredentialStorageOperation, unresolved string) string {
	if o.State == "committed" {
		return "committed"
	}
	return unresolved
}
func credentialCreationTargetBirth(tx *gorm.DB, i credentialCreationIntent, p entity.Provider, c entity.ProviderConnection) (time.Time, error) {
	if i.Kind == "replacement" {
		var original entity.ProviderCredential
		if err := personalExact(vaultDB(tx), "id", i.Target).Take(&original).Error; err != nil {
			return time.Time{}, err
		}
		if original.ID != i.Target || !connectionMetadataBirth(original.CreatedAt) {
			return time.Time{}, catalogConflict
		}
		return original.CreatedAt, nil
	}
	if i.Kind == "provider" || i.Kind == "connection" {
		return p.CreatedAt, nil
	}
	return c.CreatedAt, nil

}

// UUID responses identify only the creation-owned bootstrap rows. Later rows
// must not move the response's credential position or imply historical state.
func (s *Service) createdConnectionCatalog(ctx context.Context, o entity.CredentialStorageOperation) (*ConnectionCatalog, error) {
	result := &ConnectionCatalog{Credentials: []entity.ProviderCredential{}, Models: []entity.ProviderModel{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := personalExact(vaultDB(tx), "id", o.ConnectionID).Take(&result.Connection).Error; err != nil {
			return err
		}
		if result.Connection.ID != o.ConnectionID || result.Connection.ProviderID != o.ProviderID || !result.Connection.CreatedAt.Equal(o.ConnectionBirth) {
			return catalogConflict
		}
		var c entity.ProviderCredential
		if err := personalExact(vaultDB(tx).Omit("ciphertext"), "id", o.CredentialID).Take(&c).Error; err != nil {
			return err
		}
		if c.ID != o.CredentialID || !c.CreatedAt.Equal(o.CredentialBirth) || c.ConnectionID != o.ConnectionID || c.StorageSource != o.StorageSource {
			return catalogConflict
		}
		result.Credentials = append(result.Credentials, c)
		return nil
	})
	return result, err
}
func (s *Service) createdProviderCatalog(ctx context.Context, o entity.CredentialStorageOperation) (*ProviderCatalog, error) {
	connection, err := s.createdConnectionCatalog(ctx, o)
	if err != nil {
		return nil, err
	}
	result := &ProviderCatalog{Connections: []ConnectionCatalog{*connection}}
	if err = personalExact(s.authDB(ctx), "id", o.ProviderID).Take(&result.Provider).Error; err != nil {
		return nil, err
	}
	if result.Provider.ID != o.ProviderID || !result.Provider.CreatedAt.Equal(o.ProviderBirth) {
		return nil, catalogConflict
	}
	return result, nil
}
