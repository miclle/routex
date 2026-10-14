package service

import (
	"context"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type apiKeyDeliveryKeyRecord struct {
	Key     entity.APIKey
	Models  []string
	Project *ProjectKeyRecord
}

func apiKeyDeliveryLoadKey(tx *gorm.DB, o apiKeyDeliveryOwner, c entity.APIKeyDeliveryCommand, lock bool) (apiKeyDeliveryKeyRecord, error) {
	var r apiKeyDeliveryKeyRecord
	if c.KeyBirth == nil || c.KeyID == "" {
		return r, apiKeyDeliveryUnavailable
	}
	if o.Kind == "personal" {
		var k entity.APIKey
		q := personalExact(vaultDB(tx), "id", c.KeyID).Where("user_id = ?", o.ID)
		if lock {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if e := q.Take(&k).Error; e != nil {
			return r, e
		}
		if k.ID != c.KeyID || k.UserID != o.ID || !k.CreatedAt.Equal(*c.KeyBirth) {
			return r, apperrors.ErrNotFound
		}
		models, e := keyModelIDs(tx, k.ID)
		r.Key, r.Models = k, models
		return r, e
	}
	v, e := projectKeyRecord(tx, o.ID, c.KeyID, lock)
	if e != nil {
		return r, e
	}
	if !v.Key.CreatedAt.Equal(*c.KeyBirth) {
		return r, apperrors.ErrNotFound
	}
	r.Key, r.Models, r.Project = commonProjectKeyMetadata(v.Key), v.ModelIDs, v
	return r, nil
}
func apiKeyDeliveryRevokeKey(tx *gorm.DB, o apiKeyDeliveryOwner, c entity.APIKeyDeliveryCommand) error {
	r, e := apiKeyDeliveryLoadKey(tx, o, c, true)
	if e != nil {
		return e
	}
	if r.Key.Status == entity.KeyRevoked {
		return nil
	}
	if o.Kind == "personal" {
		r.Key.Status = entity.KeyRevoked
		r.Key.DeliveryExpiresAt = nil
		if e = advancePersonalKeyRevision(&r.Key); e != nil {
			return e
		}
		return vaultDB(tx).Save(&r.Key).Error
	}
	r.Project.Key.Status = entity.KeyRevoked
	r.Project.Key.DeliveryExpiresAt = nil
	return vaultDB(tx).Save(&r.Project.Key).Error
}

// Historical outcome persistence does not require an actor who may have been
// disabled during I/O. It serializes with exact owner/offboarding locks, never
// activates a Key and never overwrites a prior revocation/tombstone.
func apiKeyDeliveryOutcomeOwner(tx *gorm.DB, c entity.APIKeyDeliveryCommand) (apiKeyDeliveryOwner, error) {
	o := apiKeyDeliveryOwner{Kind: c.OwnerKind, ID: c.OwnerID, Birth: c.OwnerBirth, Digest: c.OwnerDigest}
	if e := lockGovernance(tx); e != nil {
		return o, e
	}
	switch o.Kind {
	case "personal":
		var u entity.User
		if e := personalExact(vaultDB(tx), "id", o.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&u).Error; e != nil {
			return o, e
		}
		if u.ID != o.ID || !u.CreatedAt.Equal(o.Birth) {
			return o, apiKeyDeliveryConflict
		}
	case "project":
		var p entity.Project
		if e := personalExact(vaultDB(tx), "id", o.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&p).Error; e != nil {
			return o, e
		}
		if p.ID != o.ID || !p.CreatedAt.Equal(o.Birth) {
			return o, apiKeyDeliveryConflict
		}
	default:
		return o, apiKeyDeliveryUnavailable
	}
	return o, nil
}

// An acknowledgement can be historical without being an activation authority.
// Only the exact issuing actor/Session may establish eligibility when I/O returns.
func apiKeyDeliveryOriginalIssuer(c entity.APIKeyDeliveryCommand, a *Authentication) bool {
	return a != nil && c.ActorID == a.User.ID && c.ActorBirth.Equal(a.User.CreatedAt) && c.SessionID == a.Session.ID && c.SessionBirth.Equal(a.Session.CreatedAt)
}
func apiKeyDeliveryApplyAcknowledgement(c *entity.APIKeyDeliveryCommand, now time.Time, eligible bool) {
	c.DeliveryStatus, c.AcknowledgedAt, c.FailureCode = "acknowledged", &now, ""
	if !eligible {
		c.RuntimeStatus, c.FailureCode = "unknown", "issuing_authority_changed"
		c.TerminalAt, c.RevokedAt = &now, &now
	}
}
func apiKeyDeliveryKnownStateLoss(e error) bool {
	if errors.Is(e, gorm.ErrRecordNotFound) || errors.Is(e, apiKeyDeliveryUnavailable) || errors.Is(e, apiKeyDeliveryConflict) {
		return true
	}
	var app *apperrors.Error
	return errors.As(e, &app) && app.Code >= 400 && app.Code < 500
}
func (s *Service) apiKeyDeliveryPostWriteCurrent(tx *gorm.DB, c entity.APIKeyDeliveryCommand, a *Authentication) (bool, error) {
	if !apiKeyDeliveryOriginalIssuer(c, a) {
		return false, nil
	}
	project := ""
	if c.OwnerKind == "project" {
		project = c.OwnerID
	}
	o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
	if e != nil {
		if apiKeyDeliveryKnownStateLoss(e) {
			return false, nil
		}
		return false, e
	}
	if o.Kind != c.OwnerKind || o.ID != c.OwnerID || !o.Birth.Equal(c.OwnerBirth) || o.Digest != c.OwnerDigest {
		return false, nil
	}
	policy, e := apiKeyDeliveryPolicy(tx, true)
	if e != nil {
		if apiKeyDeliveryKnownStateLoss(e) {
			return false, nil
		}
		return false, e
	}
	if !policy.AllowManagedVault || policy.SecurityRevision != c.PolicySecurityRevision || apiKeyDeliveryOwnerReview(policy, a, o) != c.ReviewRevision {
		return false, nil
	}
	x, e := s.apiKeyDeliverySnapshot(tx, policy, true)
	if e != nil {
		if apiKeyDeliveryKnownStateLoss(e) {
			return false, nil
		}
		return false, e
	}
	p, b, e := apiKeyDeliveryProfile(tx, c.ProfileID)
	if e != nil {
		if apiKeyDeliveryKnownStateLoss(e) {
			return false, nil
		}
		return false, e
	}
	att, e := apiKeyDeliveryReady(tx, p, b, x)
	if e != nil {
		if apiKeyDeliveryKnownStateLoss(e) {
			return false, nil
		}
		return false, e
	}
	var binding entity.APIKeyVaultDeliveryBinding
	if e = personalExact(vaultDB(tx), "id", c.BindingID).Take(&binding).Error; e != nil {
		if apiKeyDeliveryKnownStateLoss(e) {
			return false, nil
		}
		return false, e
	}
	if !apiKeyDeliveryBindingCurrent(binding, p, b, att) || att.Revision != c.IsolationRevision {
		return false, nil
	}
	key, e := apiKeyDeliveryLoadKey(tx, o, c, false)
	if e != nil {
		if apiKeyDeliveryKnownStateLoss(e) {
			return false, nil
		}
		return false, e
	}
	return key.Key.Status == entity.KeyPending, nil
}

func (s *Service) apiKeyDeliveryRecordOutcome(ctx context.Context, expected entity.APIKeyDeliveryCommand, ack bool, code string, issuer *Authentication) error {
	release := s.pinPersonalKeyMutation()
	defer release()
	revokeRuntime := !ack
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		eligible := true
		if ack {
			var e error
			eligible, e = s.apiKeyDeliveryPostWriteCurrent(tx, expected, issuer)
			if e != nil {
				return e
			}
		}
		o, e := apiKeyDeliveryOutcomeOwner(tx, expected)
		if e != nil {
			return e
		}
		c, e := apiKeyDeliveryCommand(tx, o, expected.RequestID, true)
		if e != nil {
			return e
		}
		if c.ID != expected.ID || c.KeyID != expected.KeyID || c.BindingID != expected.BindingID || c.IntentDigest != expected.IntentDigest {
			return apiKeyDeliveryConflict
		}
		if c.RevokedAt != nil || c.DeliveryStatus != "pending" {
			return nil
		}
		if !ack && expected.ClaimedAt == nil && c.ClaimedAt != nil {
			return nil
		} // A duplicate observed pre-claim; the original won dispatch admission.

		now := s.secretNow()
		if ack {
			if c.ClaimedAt == nil || expected.ClaimedAt == nil || c.ClaimProcessID != expected.ClaimProcessID || c.ClaimProcessBirth == nil || expected.ClaimProcessBirth == nil || !c.ClaimProcessBirth.Equal(*expected.ClaimProcessBirth) {
				return apiKeyDeliveryConflict
			}
			var b entity.APIKeyVaultDeliveryBinding
			if e = personalExact(vaultDB(tx), "id", c.BindingID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&b).Error; e != nil {
				return e
			}
			if c.KeyBirth == nil || b.ID != c.BindingID || b.KeyID != c.KeyID || !b.KeyBirth.Equal(*c.KeyBirth) || b.Version != 0 || b.AcknowledgedAt != nil {
				return apiKeyDeliveryConflict
			}
			b.Version, b.AcknowledgedAt = 1, &now
			if e = vaultDB(tx).Save(&b).Error; e != nil {
				return e
			}
			apiKeyDeliveryApplyAcknowledgement(&c, now, eligible)
			if !eligible {
				revokeRuntime = true
				if e = apiKeyDeliveryRevokeKey(tx, o, c); e != nil {
					return e
				}
			}
		} else {
			if c.ClaimedAt != nil {
				c.DeliveryStatus = "unknown"
			} else {
				c.DeliveryStatus = "not_attempted"
			}
			c.RuntimeStatus, c.FailureCode = "unknown", code
			c.TerminalAt, c.RevokedAt = &now, &now
			if e = apiKeyDeliveryRevokeKey(tx, o, c); e != nil {
				return e
			}
		}
		if e = vaultDB(tx).Save(&c).Error; e != nil {
			return e
		}
		return ctx.Err()
	})
	if e == nil && revokeRuntime {
		s.InvalidateRuntimeKey(expected.KeyID)
	}
	return e
}
func (s *Service) apiKeyDeliveryFinalizeAbandoned(ctx context.Context, a *Authentication, project string, expected entity.APIKeyDeliveryCommand) error {
	// Same-process duplicates observe pending state; they cannot stop an active
	// original stack. A new process only revokes; it never claims closure or Do.
	process, e := s.currentSourceProcess()
	if e == nil && expected.ClaimProcessID == process.ProcessID && expected.ClaimProcessBirth != nil && expected.ClaimProcessBirth.Equal(process.Birth) {
		return nil
	}
	var current entity.APIKeyDeliveryCommand
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		current, e = apiKeyDeliveryCommand(tx, o, expected.RequestID, false)
		if e != nil {
			return e
		}
		if current.ID != expected.ID {
			return apiKeyDeliveryConflict
		}
		return nil
	})
	if e != nil {
		return e
	}
	return s.apiKeyDeliveryRecordOutcome(ctx, current, false, "original_attempt_unrecoverable", nil)
}
func apiKeyDeliveryReceiptReview(c entity.APIKeyDeliveryCommand, k entity.APIKey, a *Authentication) string {
	return apiKeyDeliveryDigest("api-key-delivery:receipt-review:v1", struct {
		ID, Intent, Delivery, Runtime, Status, Lifecycle, Failure, Actor, Session string
		Claim, Ack, Published, Revoked, Terminal                                  *time.Time
		ActorBirth, SessionBirth                                                  time.Time
	}{c.ID, c.IntentDigest, c.DeliveryStatus, c.RuntimeStatus, k.Status, k.LifecycleRevision, c.FailureCode, a.User.ID, a.Session.ID, c.ClaimedAt, c.AcknowledgedAt, c.PublishedAt, c.RevokedAt, c.TerminalAt, a.User.CreatedAt.UTC(), a.Session.CreatedAt.UTC()})
}
func (s *Service) apiKeyDeliveryDescriptor(tx *gorm.DB, c entity.APIKeyDeliveryCommand, b entity.APIKeyVaultDeliveryBinding) (*APIKeyDeliveryDescriptorView, error) {
	if c.DeliveryStatus != "acknowledged" || c.RevokedAt != nil || b.Version != 1 || b.AcknowledgedAt == nil {
		return nil, apiKeyDeliveryConflict
	}
	var rev entity.VaultRevision
	if e := personalExact(vaultDB(tx), "id", b.VaultRevisionID).Take(&rev).Error; e != nil {
		return nil, e
	}
	if rev.ID != b.VaultRevisionID || !rev.CreatedAt.Equal(b.VaultRevisionBirth) || rev.IntegrationID != b.IntegrationID || !rev.IntegrationBirth.Equal(b.IntegrationBirth) || s.apiKeyDeliveryInfrastructure(rev) != b.IntegrationRevision || rev.Mount != b.Mount || rev.DataField != b.DataField || b.Path != rev.Prefix+"/"+b.ProfileID+"/routex-api-key-"+b.KeyID {
		return nil, apiKeyDeliveryUnavailable
	}
	return &APIKeyDeliveryDescriptorView{SchemaVersion: 1, KeyID: b.KeyID, ProfileID: b.ProfileID, PrincipalID: b.PrincipalID, SlotID: b.SlotID, DescriptorRevision: b.DescriptorRevision, Endpoint: rev.Endpoint, Namespace: rev.Namespace, Mount: b.Mount, Path: b.Path, DataField: b.DataField, Version: 1, Method: "jwt", AuthMount: b.AuthMount, Role: "routex-api-key-" + b.PrincipalID, Coordinator: "not_configured", ApplicationStatus: "unknown"}, nil
}
func apiKeyDeliveryReplacementCallStatus(tx *gorm.DB, owner apiKeyDeliveryOwner, purpose string, key entity.APIKey) (string, error) {
	if purpose == "create" {
		return "not_applicable", nil
	}
	if purpose != "rotate" {
		return "", apiKeyDeliveryUnavailable
	}
	userID, projectID := "", ""
	switch owner.Kind {
	case "personal":
		userID = owner.ID
	case "project":
		projectID = owner.ID
	default:
		return "", apiKeyDeliveryUnavailable
	}
	completed, err := hasCompletedReplacementKeyCall(tx, userID, projectID, key.ID, key.CreatedAt)
	if err != nil {
		return "", err
	}
	if completed {
		return "completed", nil
	}
	return "not_recorded", nil
}

func (s *Service) apiKeyDeliveryResult(ctx context.Context, a *Authentication, project, request string, status int) (*ManagedAPIKeyResult, error) {
	return s.apiKeyDeliveryReadResult(ctx, a, project, request, status, true)
}

func (s *Service) apiKeyDeliveryReadResult(ctx context.Context, a *Authentication, project, request string, status int, replacementProof bool) (*ManagedAPIKeyResult, error) {
	return s.apiKeyDeliveryReadResultForKey(ctx, a, project, request, "", status, replacementProof)
}

// A known-Key read resolves its original issuance command inside the same
// authorized transaction as the receipt projection; no client UUID or Vault
// operation is needed. The original command and exact current Key birth remain
// mandatory even for retained revoked or unpublished history.
func (s *Service) apiKeyDeliveryReadResultForKey(ctx context.Context, a *Authentication, project, request, key string, status int, replacementProof bool) (*ManagedAPIKeyResult, error) {
	var result ManagedAPIKeyResult
	result.HTTPStatus = status
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		if key != "" {
			var originals []entity.APIKeyDeliveryCommand
			if e = personalExact(vaultDB(tx), "owner_digest", o.Digest).Where("key_id = ?", key).Limit(2).Find(&originals).Error; e != nil {
				return e
			}
			if len(originals) == 0 {
				return apperrors.ErrNotFound
			}
			if len(originals) != 1 {
				return apiKeyDeliveryUnavailable
			}
			original := originals[0]
			if original.KeyID != key || !credentialReplacementRequestID.MatchString(original.RequestID) {
				return apperrors.ErrNotFound
			}
			request = original.RequestID
		}
		c, e := apiKeyDeliveryCommand(tx, o, request, false)
		if e != nil {
			return e
		}
		if c.Purpose != "create" && c.Purpose != "rotate" {
			return apperrors.ErrNotFound
		}
		if key != "" && c.KeyID != key {
			return apperrors.ErrNotFound
		}
		r, e := apiKeyDeliveryLoadKey(tx, o, c, false)
		if e != nil {
			return e
		}
		runtimeStatus := c.RuntimeStatus
		if runtimeStatus == "confirmed" && !s.apiKeyDeliveryPublished(r.Key, project, r.Models) {
			runtimeStatus = "unknown"
		}
		replacementCallStatus := ""
		if replacementProof {
			replacementCallStatus, e = apiKeyDeliveryReplacementCallStatus(tx, o, c.Purpose, r.Key)
			if e != nil {
				return e
			}
		}
		result.Delivery = APIKeyDeliveryReceiptView{RequestID: c.RequestID, KeyID: c.KeyID, Mode: "managed_vault", DeliveryStatus: c.DeliveryStatus, KeyStatus: r.Key.Status, RuntimeStatus: runtimeStatus, ReplacementCallStatus: replacementCallStatus, FailureCode: apiKeyDeliveryFailure(c.FailureCode), ReviewETag: apiKeyDeliveryReceiptReview(c, r.Key, a)}
		if c.RevokedAt == nil && c.DeliveryStatus == "acknowledged" && runtimeStatus == "confirmed" {
			b, e := apiKeyDeliveryAcknowledged(tx, o.Kind, c.KeyID, *c.KeyBirth)
			if e != nil {
				return e
			}
			result.Delivery.Descriptor, e = s.apiKeyDeliveryDescriptor(tx, c, b)
			if e != nil {
				return e
			}
		}
		if o.Kind == "personal" {
			result.Personal = &KeyRecord{Key: r.Key, ModelIDs: r.Models}
		} else {
			result.Project = r.Project
		}
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
func (s *Service) APIKeyDeliveryReceipt(parent context.Context, a *Authentication, project, request string) (*APIKeyDeliveryReceiptView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !credentialReplacementRequestID.MatchString(request) {
		return nil, apperrors.ErrBadRequest
	}
	r, e := s.apiKeyDeliveryResult(ctx, a, project, request, 200)
	if e != nil {
		return nil, e
	}
	return &r.Delivery, nil
}

func (s *Service) APIKeyDeliveryReceiptForKey(parent context.Context, a *Authentication, project, key string) (*APIKeyDeliveryReceiptView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	if !rootSafeIdentity(key, 30) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	r, e := s.apiKeyDeliveryReadResultForKey(ctx, a, project, "", key, 200, true)
	if e != nil {
		return nil, e
	}
	return &r.Delivery, nil
}
func (s *Service) ReconcileAPIKeyDeliveryPublication(parent context.Context, a *Authentication, project, key, etag string, in APIKeyDeliveryReconcileInput) (*ManagedAPIKeyResult, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, false)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !validRegistrationReason(in.Reason) || !credentialReplacementRequestID.MatchString(in.RequestID) || !personalModelETag(etag) || !rootSafeIdentity(key, 30) {
		return nil, apperrors.ErrBadRequest
	}
	var request string
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		var c entity.APIKeyDeliveryCommand
		if e = personalExact(vaultDB(tx), "owner_digest", o.Digest).Where("key_id = ?", key).Take(&c).Error; e != nil {
			return e
		}
		if c.KeyID != key || c.RequestID != in.RequestID {
			return apperrors.ErrNotFound
		}
		r, e := apiKeyDeliveryLoadKey(tx, o, c, false)
		if e != nil {
			return e
		}
		if apiKeyDeliveryReceiptReview(c, r.Key, a) != etag {
			return apiKeyDeliveryConflict
		}
		request = c.RequestID
		return nil
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	if _, e = s.apiKeyDeliveryPublish(ctx, a, project, request, in.Reason); e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return s.apiKeyDeliveryResult(ctx, a, project, request, 200)
}
func (s *Service) apiKeyDeliveryPublish(ctx context.Context, a *Authentication, project, request, reason string) (*apiKeyDeliveryKeyRecord, error) {
	release := s.pinPersonalKeyMutation()
	defer release()
	var record apiKeyDeliveryKeyRecord
	var command entity.APIKeyDeliveryCommand
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		x, e := s.apiKeyDeliverySnapshot(tx, policy, true)
		if e != nil {
			return e
		}
		peek, e := apiKeyDeliveryCommand(tx, o, request, false)
		if e != nil {
			return e
		}
		p, b, e := apiKeyDeliveryProfile(tx, peek.ProfileID)
		if e != nil {
			return e
		}
		att, e := apiKeyDeliveryReady(tx, p, b, x)
		if e != nil {
			return e
		}
		c, e := apiKeyDeliveryCommand(tx, o, request, true)
		if e != nil {
			return e
		}
		if c.ID != peek.ID || c.DeliveryStatus != "acknowledged" || c.RevokedAt != nil || c.KeyBirth == nil || c.PolicySecurityRevision != policy.SecurityRevision || c.IsolationRevision != att.Revision {
			return apiKeyDeliveryConflict
		}
		ack, e := apiKeyDeliveryAcknowledged(tx, o.Kind, c.KeyID, *c.KeyBirth)
		if e != nil {
			return e
		}
		if !apiKeyDeliveryBindingCurrent(ack, p, b, att) {
			return apiKeyDeliveryConflict
		}
		record, e = apiKeyDeliveryLoadKey(tx, o, c, true)
		if e != nil {
			return e
		}
		if record.Key.Status == entity.KeyRevoked {
			return errKeyConflict
		}
		if record.Key.Status == entity.KeyPending {
			if o.Kind == "personal" {
				r, e := confirmAPIKeyDeliveryRecord(tx, o.ID, &record.Key, &ack)
				if e != nil {
					return e
				}
				record.Key, record.Models = r.Key, r.ModelIDs
			} else {
				r, e := confirmProjectKeyDeliveryRecord(tx, a.User.ID, o.ID, record.Project, &ack)
				if e != nil {
					return e
				}
				record.Project = r
				record.Key, record.Models = commonProjectKeyMetadata(r.Key), r.ModelIDs
			}
		}
		c.RuntimeStatus = "unknown"
		c.FailureCode = "publication_unknown"
		if e = vaultDB(tx).Save(&c).Error; e != nil {
			return e
		}
		command = c
		if reason != "" {
			if e = apiKeyDeliveryAudit(tx, a.User.ID, "publication.reconcile", c.KeyID, c.RequestID, reason); e != nil {
				return e
			}
		}
		return ctx.Err()
	})
	release()
	if e != nil {
		return nil, e
	}
	if e = s.RefreshRuntime(ctx); e != nil {
		return &record, apiKeyDeliveryUnavailable
	}
	if !s.apiKeyDeliveryPublished(record.Key, project, record.Models) {
		return &record, apiKeyDeliveryUnavailable
	}
	// Fresh authority after publication, with every security/attestation fact
	// rechecked. The serving-generation observation alone cannot renew authority.
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		x, e := s.apiKeyDeliverySnapshot(tx, policy, true)
		if e != nil {
			return e
		}
		p, b, e := apiKeyDeliveryProfile(tx, command.ProfileID)
		if e != nil {
			return e
		}
		att, e := apiKeyDeliveryReady(tx, p, b, x)
		if e != nil {
			return e
		}
		c, e := apiKeyDeliveryCommand(tx, o, request, true)
		if e != nil {
			return e
		}
		if c.ID != command.ID || c.RevokedAt != nil || c.DeliveryStatus != "acknowledged" || c.PolicySecurityRevision != policy.SecurityRevision || c.IsolationRevision != att.Revision {
			return apiKeyDeliveryConflict
		}
		r, e := apiKeyDeliveryLoadKey(tx, o, c, true)
		if e != nil {
			return e
		}
		ack, e := apiKeyDeliveryAcknowledged(tx, o.Kind, c.KeyID, *c.KeyBirth)
		if e != nil || !apiKeyDeliveryBindingCurrent(ack, p, b, att) {
			return apiKeyDeliveryConflict
		}
		if !s.apiKeyDeliveryPublished(r.Key, project, r.Models) {
			return apiKeyDeliveryUnavailable
		}
		now := s.secretNow()
		c.RuntimeStatus, c.FailureCode, c.PublishedAt = "confirmed", "", &now
		return vaultDB(tx).Save(&c).Error
	})
	return &record, e
}
func (s *Service) APIKeyDeliveryDescriptor(parent context.Context, a *Authentication, project, key string) (*APIKeyDeliveryDescriptorView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	if !rootSafeIdentity(key, 30) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	var request string
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		var c entity.APIKeyDeliveryCommand
		if e = personalExact(vaultDB(tx), "owner_digest", o.Digest).Where("key_id = ?", key).Take(&c).Error; e != nil {
			return e
		}
		if c.KeyID != key {
			return apperrors.ErrNotFound
		}
		request = c.RequestID
		return nil
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	// Descriptor-only reads retain their existing queries and do not project a receipt.
	r, e := s.apiKeyDeliveryReadResult(ctx, a, project, request, 200, false)
	if e != nil {
		return nil, e
	}
	if r.Delivery.Descriptor == nil {
		return nil, apiKeyDeliveryConflict
	}
	return r.Delivery.Descriptor, nil
}
