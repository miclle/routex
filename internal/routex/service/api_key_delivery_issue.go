package service

import (
	"context"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
	"strings"
	"time"
)

func apiKeyDeliveryCopyAuthentication(a *Authentication) *Authentication {
	if a == nil {
		return nil
	}
	v := *a
	return &v
}
func apiKeyDeliveryIntent(in ManagedAPIKeyInput, predecessor string) string {
	models := slices.Clone(in.ModelIDs)
	slices.Sort(models)
	models = slices.Compact(models)
	var expiry *time.Time
	if in.ExpiresAt != nil {
		v := in.ExpiresAt.UTC()
		expiry = &v
	}
	return apiKeyDeliveryDigest("api-key-delivery:issuance:v1", struct {
		Name                         string
		Models                       []string
		Expiry                       *time.Time
		Request, Reason, Predecessor string
		Selection                    APIKeyDeliverySelection
	}{strings.TrimSpace(in.Name), models, expiry, in.RequestID, in.Reason, predecessor, in.Delivery})
}
func apiKeyDeliveryBindingCurrent(b entity.APIKeyVaultDeliveryBinding, p entity.VaultAccessProfile, principal entity.VaultPrincipalBinding, att entity.VaultPrincipalIsolationAttestation) bool {
	return b.ProfileID == p.ID && b.ProfileBirth.Equal(p.CreatedAt) && b.PrincipalID == principal.ID && b.PrincipalBirth.Equal(principal.CreatedAt) && b.OwnerDigest == p.OwnerDigest && b.ProfileConfigRevision == p.ConfigRevision && b.ProfileSecurityRevision == p.SecurityRevision && b.PrincipalSecurityRevision == principal.SecurityRevision && b.IsolationRevision == att.Revision && b.IsolationTupleDigest == att.TupleDigest && b.PrincipalSubject == principal.Subject && b.IntegrationID == p.IntegrationID && b.IntegrationBirth.Equal(p.IntegrationBirth) && b.IntegrationRevision == p.IntegrationRevision && b.VaultRevisionID == p.VaultRevisionID && b.VaultRevisionBirth.Equal(p.VaultRevisionBirth) && b.InfrastructureSecurityRevision == p.InfrastructureSecurityRevision && b.WriterIdentityID == p.WriterIdentityID && b.WriterIdentityBirth.Equal(p.WriterIdentityBirth) && b.WriterGeneration == p.WriterGeneration && b.ProvisionerIdentityID == p.ProvisionerIdentityID && b.ProvisionerIdentityBirth.Equal(p.ProvisionerIdentityBirth) && b.ProvisionerGeneration == p.ProvisionerGeneration && b.TemplateID == p.TemplateID && b.TemplateBirth.Equal(p.TemplateBirth) && b.TemplateSecurityRevision == p.TemplateSecurityRevision && b.AuthMount == p.AuthMount && b.Issuer == p.Issuer && b.JWKSURL == p.JWKSURL && b.Audience == p.Audience && b.TrustSecurityRevision == p.TrustSecurityRevision && b.ConsumerID == p.ConsumerID && b.Environment == p.Environment && b.OwnerKind == p.OwnerKind && b.OwnerID == p.OwnerID && b.OwnerBirth.Equal(p.OwnerBirth)
}
func apiKeyDeliveryBinding(p entity.VaultAccessProfile, b entity.VaultPrincipalBinding, att entity.VaultPrincipalIsolationAttestation, k entity.APIKey, slot entity.APIKeyDeliverySlot, plan vault.APIKeyWritePlan, rev entity.VaultRevision) (entity.APIKeyVaultDeliveryBinding, error) {
	id, e := newAPIKeyDeliveryID("kvb")
	if e != nil {
		return entity.APIKeyVaultDeliveryBinding{}, e
	}
	return entity.APIKeyVaultDeliveryBinding{ID: id, KeyKind: p.OwnerKind, KeyID: k.ID, KeyBirth: k.CreatedAt, KeyDigest: apiKeyDeliveryKeyDigest(p.OwnerKind, k.ID, k.CreatedAt), SlotID: slot.ID, DescriptorRevision: slot.DescriptorRevision, ProfileID: p.ID, ProfileBirth: p.CreatedAt, PrincipalID: b.ID, PrincipalBirth: b.CreatedAt, ProfileConfigRevision: p.ConfigRevision, ProfileSecurityRevision: p.SecurityRevision, PrincipalSecurityRevision: b.SecurityRevision, OwnerKind: p.OwnerKind, OwnerID: p.OwnerID, OwnerBirth: p.OwnerBirth, OwnerDigest: p.OwnerDigest, ConsumerID: p.ConsumerID, Environment: p.Environment, IntegrationID: p.IntegrationID, IntegrationBirth: p.IntegrationBirth, IntegrationRevision: p.IntegrationRevision, VaultRevisionID: p.VaultRevisionID, VaultRevisionBirth: p.VaultRevisionBirth, InfrastructureSecurityRevision: p.InfrastructureSecurityRevision, WriterIdentityID: p.WriterIdentityID, WriterIdentityBirth: p.WriterIdentityBirth, WriterGeneration: p.WriterGeneration, ProvisionerIdentityID: p.ProvisionerIdentityID, ProvisionerIdentityBirth: p.ProvisionerIdentityBirth, ProvisionerGeneration: p.ProvisionerGeneration, TemplateID: p.TemplateID, TemplateBirth: p.TemplateBirth, TemplateSecurityRevision: p.TemplateSecurityRevision, AuthMount: p.AuthMount, Issuer: p.Issuer, JWKSURL: p.JWKSURL, Audience: p.Audience, TrustSecurityRevision: p.TrustSecurityRevision, PrincipalSubject: b.Subject, IsolationRevision: att.Revision, IsolationTupleDigest: att.TupleDigest, Mount: rev.Mount, Path: rev.Prefix + "/" + p.ID + "/routex-api-key-" + k.ID, DataField: rev.DataField, PlanDigest: plan.DescriptorSHA256, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}, nil
}
func apiKeyDeliveryChain(tx *gorm.DB, o apiKeyDeliveryOwner, key string) (string, error) {
	seen := map[string]bool{}
	current := key
	for range 128 {
		if seen[current] {
			return "", apiKeyDeliveryUnavailable
		}
		seen[current] = true
		var parent *string
		if o.Kind == "personal" {
			k, e := lockOwnedKey(tx, o.ID, current)
			if e != nil {
				return "", e
			}
			parent = k.ReplacesKeyID
		} else {
			r, e := projectKeyRecord(tx, o.ID, current, true)
			if e != nil {
				return "", e
			}
			parent = r.Key.ReplacesKeyID
		}
		if parent == nil {
			return current, nil
		}
		current = *parent
	}
	return "", apiKeyDeliveryUnavailable
}
func apiKeyDeliverySlot(tx *gorm.DB, o apiKeyDeliveryOwner, p entity.VaultAccessProfile, chain string) (entity.APIKeyDeliverySlot, error) {
	digest := apiKeyDeliveryDigest("api-key-delivery:slot:v1", struct{ Owner, Profile, Chain string }{o.Digest, p.ID, chain})
	var slot entity.APIKeyDeliverySlot
	e := personalExact(vaultDB(tx), "slot_digest", digest).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&slot).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		id, e := newAPIKeyDeliveryID("kds")
		if e != nil {
			return slot, e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		slot = entity.APIKeyDeliverySlot{ID: id, OwnerKind: o.Kind, OwnerID: o.ID, OwnerBirth: o.Birth, OwnerDigest: o.Digest, ProfileID: p.ID, ProfileBirth: p.CreatedAt, ChainID: chain, SlotDigest: digest, DescriptorRevision: 1, CreatedAt: now, UpdatedAt: now}
		return slot, vaultDB(tx).Create(&slot).Error
	}
	if e != nil {
		return slot, e
	}
	if slot.OwnerKind != o.Kind || slot.OwnerID != o.ID || !slot.OwnerBirth.Equal(o.Birth) || slot.OwnerDigest != o.Digest || slot.ProfileID != p.ID || !slot.ProfileBirth.Equal(p.CreatedAt) || slot.ChainID != chain || slot.SlotDigest != digest || slot.DescriptorRevision >= uint64(1<<63-1) {
		return slot, apiKeyDeliveryConflict
	}
	slot.DescriptorRevision++
	return slot, vaultDB(tx).Save(&slot).Error
}
func apiKeyDeliveryReady(tx *gorm.DB, p entity.VaultAccessProfile, b entity.VaultPrincipalBinding, x apiKeyDeliverySnapshot) (entity.VaultPrincipalIsolationAttestation, error) {
	att, code, e := apiKeyDeliveryIsolation(tx, p, b)
	if e != nil {
		return att, e
	}
	if code != "" || !p.Enabled || !b.Enabled || b.ApprovedAt == nil || p.Status != "ready" || p.ReadinessAt == nil || p.ReadinessDigest != apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, b)) || b.ReconciledRevision != p.ReadinessDigest || !apiKeyDeliveryTargetCurrent(p, b, x) {
		return att, apiKeyDeliveryUnavailable
	}
	return att, nil
}
func apiKeyDeliveryCommand(tx *gorm.DB, o apiKeyDeliveryOwner, request string, lock bool) (entity.APIKeyDeliveryCommand, error) {
	var c entity.APIKeyDeliveryCommand
	q := personalExact(vaultDB(tx), "owner_digest", o.Digest).Where("request_id = ?", request)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.Take(&c).Error
	if e == nil && (c.OwnerKind != o.Kind || c.OwnerID != o.ID || !c.OwnerBirth.Equal(o.Birth) || c.OwnerDigest != o.Digest || c.RequestID != request) {
		e = apperrors.ErrNotFound
	}
	return c, e
}
func (s *Service) CreateManagedAPIKey(parent context.Context, a *Authentication, project, etag string, in ManagedAPIKeyInput) (*ManagedAPIKeyResult, error) {
	return s.issueManagedAPIKey(parent, a, project, "", etag, in)
}
func (s *Service) RotateManagedAPIKey(parent context.Context, a *Authentication, project, key, etag string, in ManagedAPIKeyInput) (*ManagedAPIKeyResult, error) {
	if !rootSafeIdentity(key, 30) || in.Name != "" || len(in.ModelIDs) != 0 || in.ExpiresAt != nil {
		return nil, apperrors.ErrBadRequest
	}
	return s.issueManagedAPIKey(parent, a, project, key, etag, in)
}
func (s *Service) issueManagedAPIKey(parent context.Context, a *Authentication, project, predecessor, etag string, in ManagedAPIKeyInput) (*ManagedAPIKeyResult, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	in.ModelIDs = slices.Clone(in.ModelIDs)
	if in.ExpiresAt != nil {
		v := *in.ExpiresAt
		in.ExpiresAt = &v
	}
	ctx, cancel, e := apiKeyDeliveryContext(parent, false)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if a == nil || !personalModelETag(etag) || !personalModelETag(in.Delivery.TargetReview) || !credentialReplacementRequestID.MatchString(in.RequestID) || !validRegistrationReason(in.Reason) || !apiKeyDeliveryCode(in.Delivery.ConsumerID) || !apiKeyDeliveryCode(in.Delivery.Environment) || in.Delivery.Principal.Method != "jwt" || !apiKeyDeliveryText(in.Delivery.Principal.Subject, 256) || len(in.ModelIDs) > 128 {
		return nil, apperrors.ErrBadRequest
	}
	if predecessor == "" && (!validCatalogLabel(strings.TrimSpace(in.Name)) || in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now())) {
		return nil, apperrors.ErrBadRequest
	}
	purpose := "create"
	if predecessor != "" {
		purpose = "rotate"
	}
	intent := apiKeyDeliveryIntent(in, predecessor)
	ticket, e := s.credentialSources.processAcquire()
	if e != nil {
		return nil, apiKeyDeliveryUnavailable
	}
	defer ticket.release()
	release := s.pinPersonalKeyMutation()
	defer release()
	var owner apiKeyDeliveryOwner
	var x apiKeyDeliverySnapshot
	var command entity.APIKeyDeliveryCommand
	var binding entity.APIKeyVaultDeliveryBinding
	var bearer []byte
	defer func() { clear(bearer) }()
	var writer *vault.APIKeyWriter
	var prepared *vault.PreparedAPIKeyWrite
	defer func() {
		if prepared != nil {
			prepared.Close()
		}
		if writer != nil {
			writer.Close()
		}
	}()
	created := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		owner, e = apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		old, e := apiKeyDeliveryCommand(tx, owner, in.RequestID, true)
		if e == nil {
			if old.Purpose != purpose || old.ActorID != a.User.ID || !old.ActorBirth.Equal(a.User.CreatedAt) || old.IntentDigest != intent || old.ReviewRevision != etag {
				return apiKeyDeliveryConflict
			}
			command = old
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if !policy.AllowManagedVault || apiKeyDeliveryOwnerReview(policy, a, owner) != etag {
			return apiKeyDeliveryConflict
		}
		x, e = s.apiKeyDeliverySnapshot(tx, policy, true)
		if e != nil {
			return e
		}
		targetDigest := apiKeyDeliveryDigest("api-key-delivery:target:v1", struct{ Owner, Consumer, Environment string }{owner.Digest, in.Delivery.ConsumerID, in.Delivery.Environment})
		var found entity.VaultAccessProfile
		if e = personalExact(vaultDB(tx), "target_digest", targetDigest).Take(&found).Error; e != nil {
			return e
		}
		p, b, e := apiKeyDeliveryProfile(tx, found.ID)
		if e != nil {
			return e
		}
		if !apiKeyDeliveryOwnerMatches(p, owner) || p.ConsumerID != in.Delivery.ConsumerID || p.Environment != in.Delivery.Environment || b.Subject != in.Delivery.Principal.Subject || apiKeyDeliveryTargetReview(p, b, a) != in.Delivery.TargetReview {
			return apiKeyDeliveryConflict
		}
		att, e := apiKeyDeliveryReady(tx, p, b, x)
		if e != nil {
			return e
		}
		var pending int64
		if e = vaultDB(tx).Model(&entity.APIKeyDeliveryCommand{}).Where("owner_digest = ? AND profile_id = ? AND delivery_status = ? AND revoked_at IS NULL", owner.Digest, p.ID, "pending").Limit(1).Count(&pending).Error; e != nil {
			return e
		}
		if pending != 0 {
			return apiKeyDeliveryConflict
		}
		name, models, expiry, activate := strings.TrimSpace(in.Name), slices.Clone(in.ModelIDs), in.ExpiresAt, true
		var replaces *string
		var predecessorBirth *time.Time
		if predecessor != "" {
			replaces = &predecessor
			if owner.Kind == "personal" {
				old, e := lockOwnedKey(tx, owner.ID, predecessor)
				if e != nil {
					return e
				}
				if old.Status != entity.KeyActive && old.Status != entity.KeyDisabled || old.ExpiresAt != nil && !old.ExpiresAt.After(time.Now()) {
					return errKeyConflict
				}
				name, expiry, activate = old.Name, old.ExpiresAt, old.Status == entity.KeyActive
				models, e = keyModelIDs(tx, old.ID)
				if e != nil {
					return e
				}
				v := old.CreatedAt
				predecessorBirth = &v
			} else {
				old, e := projectKeyRecord(tx, owner.ID, predecessor, true)
				if e != nil {
					return e
				}
				if old.Key.Status != entity.KeyActive && old.Key.Status != entity.KeyDisabled || old.Key.ExpiresAt != nil && !old.Key.ExpiresAt.After(time.Now()) {
					return errKeyConflict
				}
				name, models, expiry, activate = old.Key.Name, slices.Clone(old.ModelIDs), old.Key.ExpiresAt, old.Key.Status == entity.KeyActive
				v := old.Key.CreatedAt
				predecessorBirth = &v
			}
		}
		var k entity.APIKey
		if owner.Kind == "personal" {
			if e = validateKeyModels(tx, owner.ID, models); e != nil {
				return e
			}
			c, e := createPendingKeyWithMode(tx, owner.ID, name, models, expiry, replaces, activate, "managed_vault")
			if e != nil {
				return e
			}
			k = c.Record.Key
			bearer = []byte(c.Secret)
			c.Secret = ""
		} else {
			if e = validateProjectKeyModels(tx, owner.ID, models); e != nil {
				return e
			}
			c, e := createPendingProjectKeyWithMode(tx, a.User.ID, owner.ID, name, models, expiry, replaces, activate, "managed_vault")
			if e != nil {
				return e
			}
			k = commonProjectKeyMetadata(c.Record.Key)
			bearer = []byte(c.Secret)
			c.Secret = ""
		}
		chain := k.ID
		if predecessor != "" {
			chain, e = apiKeyDeliveryChain(tx, owner, predecessor)
			if e != nil {
				return e
			}
		}
		slot, e := apiKeyDeliverySlot(tx, owner, p, chain)
		if e != nil {
			return e
		}
		d := vaultDescriptor(x.Revision).client()
		d.Prefix += "/" + p.ID
		writer, e = vault.NewAPIKeyWriter(d, s.allowPrivateUpstream)
		if e != nil {
			return apiKeyDeliveryUnavailable
		}
		prepared, e = writer.Prepare(k.ID)
		if e != nil {
			return apiKeyDeliveryUnavailable
		}
		binding, e = apiKeyDeliveryBinding(p, b, att, k, slot, prepared.Plan(), x.Revision)
		if e != nil {
			return e
		}
		if e = vaultDB(tx).Create(&binding).Error; e != nil {
			return e
		}
		id, e := newAPIKeyDeliveryID("kdo")
		if e != nil {
			return e
		}
		now := s.secretNow()
		birth := k.CreatedAt
		command = entity.APIKeyDeliveryCommand{ID: id, OwnerKind: owner.Kind, OwnerID: owner.ID, OwnerBirth: owner.Birth, OwnerDigest: owner.Digest, RequestID: in.RequestID, ActorID: a.User.ID, ActorBirth: a.User.CreatedAt, SessionID: a.Session.ID, SessionBirth: a.Session.CreatedAt, Purpose: purpose, IntentDigest: intent, ReviewRevision: etag, TargetReview: in.Delivery.TargetReview, ProfileID: p.ID, ProfileBirth: p.CreatedAt, PrincipalID: b.ID, PrincipalBirth: b.CreatedAt, KeyID: k.ID, KeyBirth: &birth, BindingID: binding.ID, PredecessorID: predecessor, PredecessorBirth: predecessorBirth, PolicySecurityRevision: policy.SecurityRevision, IsolationRevision: att.Revision, DeliveryStatus: "pending", RuntimeStatus: "pending", Reason: in.Reason, CreatedAt: now, UpdatedAt: now}
		if e = vaultDB(tx).Create(&command).Error; e != nil {
			return e
		}
		created = true
		return apiKeyDeliveryAudit(tx, a.User.ID, "prepare", k.ID, in.RequestID, in.Reason)
	})
	release()
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	if !created {
		if command.DeliveryStatus == "pending" {
			if e = s.apiKeyDeliveryFinalizeAbandoned(ctx, a, project, command); e != nil {
				return nil, apiKeyDeliveryError(e)
			}
		}
		return s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 200)
	}
	if _, e = s.apiKeyDeliveryValidateMachines(ctx, x, binding.ProfileID, binding.PrincipalID, command.KeyID); e != nil {
		_ = s.apiKeyDeliveryRecordOutcome(ctx, command, false, "dependency_validation_unavailable", a)
		return s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 202)
	}
	process, e := s.currentSourceProcess()
	if e != nil {
		_ = s.apiKeyDeliveryRecordOutcome(ctx, command, false, "process_unavailable", a)
		return s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 202)
	}
	// This second transaction is the sole durable permission to consume the local
	// handle. A duplicate/restart never retains either bearer or prepared handle.
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		fresh, e := s.apiKeyDeliverySnapshot(tx, policy, true)
		if e != nil {
			return e
		}
		p, b, e := apiKeyDeliveryProfile(tx, command.ProfileID)
		if e != nil {
			return e
		}
		att, e := apiKeyDeliveryReady(tx, p, b, fresh)
		if e != nil {
			return e
		}
		c, e := apiKeyDeliveryCommand(tx, o, command.RequestID, true)
		if e != nil {
			return e
		}
		if !apiKeyDeliveryClaimable(c, command, a) || apiKeyDeliveryOwnerReview(policy, a, o) != c.ReviewRevision || policy.SecurityRevision != c.PolicySecurityRevision || !policy.AllowManagedVault || !apiKeyDeliveryBindingCurrent(binding, p, b, att) {
			return apiKeyDeliveryConflict
		}
		k, e := apiKeyDeliveryLoadKey(tx, o, c, true)
		if e != nil {
			return e
		}
		if k.Key.Status != entity.KeyPending {
			return errKeyConflict
		}
		if e = s.sourceProcessCurrent(tx, process); e != nil {
			return e
		}
		now := s.secretNow()
		c.ClaimProcessID = process.ProcessID
		birth := process.Birth
		c.ClaimProcessBirth = &birth
		c.ClaimedAt = &now
		if e = vaultDB(tx).Save(&c).Error; e != nil {
			return e
		}
		command = c
		return ctx.Err()
	})
	if e != nil {
		_ = s.apiKeyDeliveryRecordOutcome(ctx, command, false, "admission_changed", a)
		return s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 202)
	}
	ref, e := apiKeyDeliveryAuthReference(x.Writer)
	if e != nil {
		_ = s.apiKeyDeliveryRecordOutcome(ctx, command, false, "writer_unavailable", a)
		return s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 202)
	}
	token, e := s.openSecret(ref, x.Writer.AuthCiphertext)
	if e != nil {
		_ = s.apiKeyDeliveryRecordOutcome(ctx, command, false, "writer_unavailable", a)
		return s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 202)
	}
	observed, writeErr := writer.Write(ctx, token, prepared, bearer)
	clear(bearer)
	state := writer.ResponseCloseState()
	if state.Failed || state.Pending != 0 {
		ticket.poison()
	}
	ack := writeErr == nil && observed.Outcome == vault.APIKeyWriteAcknowledged && observed.Version == 1 && observed.Consumed && observed.Plan == prepared.Plan() && state.Observed && !state.Failed && state.Pending == 0 && ctx.Err() == nil
	code := "write_unknown"
	if ack {
		code = ""
	}
	if e = s.apiKeyDeliveryRecordOutcome(ctx, command, ack, code, a); e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	if ack {
		if _, e = s.apiKeyDeliveryPublish(ctx, a, project, command.RequestID, ""); e != nil {
			return s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 202)
		}
	}
	result, e := s.apiKeyDeliveryResult(ctx, a, project, command.RequestID, 202)
	if e == nil && result.Delivery.RuntimeStatus == "confirmed" {
		result.HTTPStatus = 201
	}
	return result, e
}
func apiKeyDeliveryClaimable(c, expected entity.APIKeyDeliveryCommand, a *Authentication) bool {
	return a != nil && c.ID == expected.ID && c.IntentDigest == expected.IntentDigest && c.KeyID == expected.KeyID && c.KeyBirth != nil && expected.KeyBirth != nil && c.KeyBirth.Equal(*expected.KeyBirth) && c.BindingID == expected.BindingID && c.ActorID == a.User.ID && c.ActorBirth.Equal(a.User.CreatedAt) && c.SessionID == a.Session.ID && c.SessionBirth.Equal(a.Session.CreatedAt) && c.DeliveryStatus == "pending" && c.ClaimedAt == nil && c.ClaimProcessID == "" && c.ClaimProcessBirth == nil && c.RevokedAt == nil && c.AcknowledgedAt == nil
}
