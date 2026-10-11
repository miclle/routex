package service

import (
	"context"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
	"time"
)

type apiKeyDeliveryTuple struct {
	ProfileID                      string    `json:"profile_id"`
	OwnerKind                      string    `json:"owner_kind"`
	OwnerID                        string    `json:"owner_id"`
	OwnerBirth                     time.Time `json:"owner_birth"`
	OwnerDigest                    string    `json:"owner_digest"`
	ConsumerID                     string    `json:"consumer_id"`
	Environment                    string    `json:"environment"`
	ProfileConfigRevision          string    `json:"profile_config_revision"`
	ProfileSecurityRevision        string    `json:"profile_security_revision"`
	IntegrationID                  string    `json:"integration_id"`
	IntegrationBirth               time.Time `json:"integration_birth"`
	IntegrationRevision            string    `json:"integration_revision"`
	VaultRevisionID                string    `json:"vault_revision_id"`
	VaultRevisionBirth             time.Time `json:"vault_revision_birth"`
	InfrastructureSecurityRevision string    `json:"infrastructure_security_revision"`
	WriterIdentityID               string    `json:"writer_identity_id"`
	WriterIdentityBirth            time.Time `json:"writer_identity_birth"`
	WriterGeneration               string    `json:"writer_generation"`
	ProvisionerIdentityID          string    `json:"provisioner_identity_id"`
	ProvisionerIdentityBirth       time.Time `json:"provisioner_identity_birth"`
	ProvisionerGeneration          string    `json:"provisioner_generation"`
	TemplateID                     string    `json:"template_id"`
	TemplateBirth                  time.Time `json:"template_birth"`
	TemplateSecurityRevision       string    `json:"template_security_revision"`
	AuthMount                      string    `json:"auth_mount"`
	Issuer                         string    `json:"issuer"`
	JWKSURL                        string    `json:"jwks_url"`
	Audience                       string    `json:"audience"`
	TrustSecurityRevision          string    `json:"trust_security_revision"`
	ProfileBirth                   time.Time `json:"profile_birth"`
	PrincipalID                    string    `json:"principal_id"`
	PrincipalBirth                 time.Time `json:"principal_birth"`
	PrincipalSecurityRevision      string    `json:"principal_security_revision"`
	Method                         string    `json:"method"`
	Subject                        string    `json:"subject"`
}

func apiKeyDeliveryTargetTuple(p entity.VaultAccessProfile, b entity.VaultPrincipalBinding) apiKeyDeliveryTuple {
	return apiKeyDeliveryTuple{
		ProfileID:                      p.ID,
		OwnerKind:                      p.OwnerKind,
		OwnerID:                        p.OwnerID,
		OwnerBirth:                     p.OwnerBirth.UTC(),
		OwnerDigest:                    p.OwnerDigest,
		ConsumerID:                     p.ConsumerID,
		Environment:                    p.Environment,
		ProfileConfigRevision:          p.ConfigRevision,
		ProfileSecurityRevision:        p.SecurityRevision,
		IntegrationID:                  p.IntegrationID,
		IntegrationBirth:               p.IntegrationBirth.UTC(),
		IntegrationRevision:            p.IntegrationRevision,
		VaultRevisionID:                p.VaultRevisionID,
		VaultRevisionBirth:             p.VaultRevisionBirth.UTC(),
		InfrastructureSecurityRevision: p.InfrastructureSecurityRevision,
		WriterIdentityID:               p.WriterIdentityID,
		WriterIdentityBirth:            p.WriterIdentityBirth.UTC(),
		WriterGeneration:               p.WriterGeneration,
		ProvisionerIdentityID:          p.ProvisionerIdentityID,
		ProvisionerIdentityBirth:       p.ProvisionerIdentityBirth.UTC(),
		ProvisionerGeneration:          p.ProvisionerGeneration,
		TemplateID:                     p.TemplateID,
		TemplateBirth:                  p.TemplateBirth.UTC(),
		TemplateSecurityRevision:       p.TemplateSecurityRevision,
		AuthMount:                      p.AuthMount,
		Issuer:                         p.Issuer,
		JWKSURL:                        p.JWKSURL,
		Audience:                       p.Audience,
		TrustSecurityRevision:          p.TrustSecurityRevision,
		ProfileBirth:                   p.CreatedAt.UTC(),
		PrincipalID:                    b.ID,
		PrincipalBirth:                 b.CreatedAt.UTC(),
		PrincipalSecurityRevision:      b.SecurityRevision,
		Method:                         b.Method,
		Subject:                        b.Subject,
	}
}
func apiKeyDeliveryAttestationTuple(a entity.VaultPrincipalIsolationAttestation) apiKeyDeliveryTuple {
	return apiKeyDeliveryTuple{
		ProfileID:                      a.ProfileID,
		OwnerKind:                      a.OwnerKind,
		OwnerID:                        a.OwnerID,
		OwnerBirth:                     a.OwnerBirth.UTC(),
		OwnerDigest:                    a.OwnerDigest,
		ConsumerID:                     a.ConsumerID,
		Environment:                    a.Environment,
		ProfileConfigRevision:          a.ProfileConfigRevision,
		ProfileSecurityRevision:        a.ProfileSecurityRevision,
		IntegrationID:                  a.IntegrationID,
		IntegrationBirth:               a.IntegrationBirth.UTC(),
		IntegrationRevision:            a.IntegrationRevision,
		VaultRevisionID:                a.VaultRevisionID,
		VaultRevisionBirth:             a.VaultRevisionBirth.UTC(),
		InfrastructureSecurityRevision: a.InfrastructureSecurityRevision,
		WriterIdentityID:               a.WriterIdentityID,
		WriterIdentityBirth:            a.WriterIdentityBirth.UTC(),
		WriterGeneration:               a.WriterGeneration,
		ProvisionerIdentityID:          a.ProvisionerIdentityID,
		ProvisionerIdentityBirth:       a.ProvisionerIdentityBirth.UTC(),
		ProvisionerGeneration:          a.ProvisionerGeneration,
		TemplateID:                     a.TemplateID,
		TemplateBirth:                  a.TemplateBirth.UTC(),
		TemplateSecurityRevision:       a.TemplateSecurityRevision,
		AuthMount:                      a.AuthMount,
		Issuer:                         a.Issuer,
		JWKSURL:                        a.JWKSURL,
		Audience:                       a.Audience,
		TrustSecurityRevision:          a.TrustSecurityRevision,
		ProfileBirth:                   a.ProfileBirth.UTC(),
		PrincipalID:                    a.PrincipalID,
		PrincipalBirth:                 a.PrincipalBirth.UTC(),
		PrincipalSecurityRevision:      a.PrincipalSecurityRevision,
		Method:                         a.Method,
		Subject:                        a.Subject,
	}
}
func apiKeyDeliveryNewAttestation(p entity.VaultAccessProfile, b entity.VaultPrincipalBinding) entity.VaultPrincipalIsolationAttestation {
	return entity.VaultPrincipalIsolationAttestation{
		ProfileID:                      p.ID,
		OwnerKind:                      p.OwnerKind,
		OwnerID:                        p.OwnerID,
		OwnerBirth:                     p.OwnerBirth,
		OwnerDigest:                    p.OwnerDigest,
		ConsumerID:                     p.ConsumerID,
		Environment:                    p.Environment,
		ProfileConfigRevision:          p.ConfigRevision,
		ProfileSecurityRevision:        p.SecurityRevision,
		IntegrationID:                  p.IntegrationID,
		IntegrationBirth:               p.IntegrationBirth,
		IntegrationRevision:            p.IntegrationRevision,
		VaultRevisionID:                p.VaultRevisionID,
		VaultRevisionBirth:             p.VaultRevisionBirth,
		InfrastructureSecurityRevision: p.InfrastructureSecurityRevision,
		WriterIdentityID:               p.WriterIdentityID,
		WriterIdentityBirth:            p.WriterIdentityBirth,
		WriterGeneration:               p.WriterGeneration,
		ProvisionerIdentityID:          p.ProvisionerIdentityID,
		ProvisionerIdentityBirth:       p.ProvisionerIdentityBirth,
		ProvisionerGeneration:          p.ProvisionerGeneration,
		TemplateID:                     p.TemplateID,
		TemplateBirth:                  p.TemplateBirth,
		TemplateSecurityRevision:       p.TemplateSecurityRevision,
		AuthMount:                      p.AuthMount,
		Issuer:                         p.Issuer,
		JWKSURL:                        p.JWKSURL,
		Audience:                       p.Audience,
		TrustSecurityRevision:          p.TrustSecurityRevision,
		ProfileBirth:                   p.CreatedAt,
		PrincipalID:                    b.ID,
		PrincipalBirth:                 b.CreatedAt,
		PrincipalSecurityRevision:      b.SecurityRevision,
		Method:                         b.Method,
		Subject:                        b.Subject,
	}
}
func apiKeyDeliveryTupleDigest(t apiKeyDeliveryTuple) string {
	return apiKeyDeliveryDigest("api-key-delivery:security-tuple:v1", t)
}
func apiKeyDeliveryIsolation(tx *gorm.DB, p entity.VaultAccessProfile, b entity.VaultPrincipalBinding) (entity.VaultPrincipalIsolationAttestation, string, error) {
	var a entity.VaultPrincipalIsolationAttestation
	if p.CurrentIsolationRevision == "" {
		return a, "isolation_attestation_missing", nil
	}
	if e := personalExact(vaultDB(tx), "revision", p.CurrentIsolationRevision).Take(&a).Error; e != nil {
		return a, "isolation_attestation_stale", e
	}
	if a.Revision != p.CurrentIsolationRevision || a.ProfileID != p.ID || !a.ProfileBirth.Equal(p.CreatedAt) || a.PrincipalID != b.ID || !a.PrincipalBirth.Equal(b.CreatedAt) {
		return a, "isolation_attestation_stale", nil
	}
	if a.State == "revoked" || p.IsolationState == "revoked" {
		return a, "isolation_attestation_revoked", nil
	}
	want := apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, b))
	if a.State != "operator_attested" || p.IsolationState != "operator_attested" || a.TupleDigest != want || apiKeyDeliveryTupleDigest(apiKeyDeliveryAttestationTuple(a)) != want {
		return a, "isolation_attestation_stale", nil
	}
	return a, "", nil
}
func apiKeyDeliveryApplySnapshot(p *entity.VaultAccessProfile, x apiKeyDeliverySnapshot) {
	p.IntegrationID, p.IntegrationBirth, p.IntegrationRevision = x.Integration.ID, x.Integration.CreatedAt, x.Policy.IntegrationRevision
	p.VaultRevisionID, p.VaultRevisionBirth = x.Revision.ID, x.Revision.CreatedAt
	p.InfrastructureSecurityRevision = x.Policy.InfrastructureSecurityRevision
	p.WriterIdentityID, p.WriterIdentityBirth, p.WriterGeneration = x.Writer.ID, x.Writer.CreatedAt, x.Writer.SecretGeneration
	p.ProvisionerIdentityID, p.ProvisionerIdentityBirth, p.ProvisionerGeneration = x.Provisioner.ID, x.Provisioner.CreatedAt, x.Provisioner.SecretGeneration
	p.TemplateID, p.TemplateBirth, p.TemplateSecurityRevision = x.Template.ID, x.Template.CreatedAt, x.Template.SecurityRevision
	p.AuthMount, p.Issuer, p.JWKSURL, p.Audience, p.TrustSecurityRevision = x.Template.AuthMount, x.Template.Issuer, x.Template.JWKSURL, x.Template.Audience, x.Template.TrustSecurityRevision
}
func apiKeyDeliveryTargetCurrent(p entity.VaultAccessProfile, b entity.VaultPrincipalBinding, x apiKeyDeliverySnapshot) bool {
	expected := p
	apiKeyDeliveryApplySnapshot(&expected, x)
	return apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(expected, b)) == apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, b)) && b.Issuer == p.Issuer && b.Audience == p.Audience && b.SubjectDigest == apiKeyDeliveryDigest("api-key-delivery:subject:v1", struct{ Issuer, Audience, Subject string }{b.Issuer, b.Audience, b.Subject}) && apiKeyDeliverySubjectAllowed(x.Template, b.Subject)
}
func (s *Service) apiKeyDeliveryTargetView(tx *gorm.DB, p entity.VaultAccessProfile, b entity.VaultPrincipalBinding, a *Authentication) (APIKeyDeliveryTargetView, error) {
	att, code, e := apiKeyDeliveryIsolation(tx, p, b)
	if e != nil {
		return APIKeyDeliveryTargetView{}, e
	}
	iv := APIKeyDeliveryIsolationView{Status: p.IsolationState}
	if p.CurrentIsolationRevision != "" {
		iv.Revision = &att.Revision
		iv.RecordedAt = &att.RecordedAt
	}
	if code == "isolation_attestation_stale" {
		iv.Status = "stale"
	}
	if p.CurrentIsolationRevision == "" {
		iv.Status = "missing"
	}
	status := p.Status
	if status == "ready" {
		policy, err := apiKeyDeliveryPolicy(tx, false)
		if err != nil {
			return APIKeyDeliveryTargetView{}, err
		}
		x, err := s.apiKeyDeliverySnapshot(tx, policy, true)
		if err != nil || s.apiKeyDeliveryValidator == nil || !apiKeyDeliveryTargetCurrent(p, b, x) || code != "" || !p.Enabled || !b.Enabled || p.ReadinessAt == nil || p.ReadinessDigest != apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, b)) || b.ReconciledRevision != p.ReadinessDigest {
			status = "pending"
			if code == "" {
				code = "dependency_validation_unavailable"
			}
		}
	}
	return APIKeyDeliveryTargetView{ID: p.ID, ConsumerID: p.ConsumerID, ApplicationName: p.ApplicationName, Environment: p.Environment, Status: status, ControlVerified: b.Enabled && b.ApprovedAt != nil, Isolation: iv, FailureCode: apiKeyDeliveryFailure(code), ReviewETag: apiKeyDeliveryTargetReview(p, b, a)}, nil
}
func (s *Service) APIKeyDeliveryContext(parent context.Context, a *Authentication, project string) (*APIKeyDeliveryContextView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	var result APIKeyDeliveryContextView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, false)
		if e != nil {
			return e
		}
		v := s.apiKeyDeliveryPolicyView(tx, p, a)
		result = v.APIKeyDeliveryContextView
		result.ReviewETag = apiKeyDeliveryOwnerReview(p, a, o)
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
func apiKeyDeliveryResolveIntent(in APIKeyDeliveryResolveInput) string {
	return apiKeyDeliveryDigest("api-key-delivery:resolve-intent:v1", in)
}
func apiKeyDeliveryAllocateConsumer(requested string) (string, error) {
	if requested != "" {
		if !apiKeyDeliveryCode(requested) {
			return "", apperrors.ErrBadRequest
		}
		return requested, nil
	}
	return newAPIKeyDeliveryID("kdc")
}

func (s *Service) ResolveAPIKeyDeliveryTarget(parent context.Context, a *Authentication, project, etag string, in APIKeyDeliveryResolveInput) (*APIKeyDeliveryTargetView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, false)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !personalModelETag(etag) || !credentialReplacementRequestID.MatchString(in.RequestID) || !validCatalogLabel(in.ApplicationName) || in.ConsumerID != "" && !apiKeyDeliveryCode(in.ConsumerID) || !apiKeyDeliveryCode(in.Environment) || in.Principal.Method != "jwt" || !apiKeyDeliveryText(in.Principal.Subject, 256) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	intent := apiKeyDeliveryResolveIntent(in)
	var result APIKeyDeliveryTargetView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		var old entity.APIKeyDeliveryCommand
		e = personalExact(vaultDB(tx), "owner_digest", o.Digest).Where("request_id = ?", in.RequestID).Take(&old).Error
		if e == nil {
			if old.RequestID != in.RequestID || old.Purpose != "resolve" || old.ActorID != a.User.ID || !old.ActorBirth.Equal(a.User.CreatedAt) || old.IntentDigest != intent || old.ReviewRevision != etag {
				return apiKeyDeliveryConflict
			}
			p, b, e := apiKeyDeliveryProfile(tx, old.ProfileID)
			if e != nil {
				return e
			}
			if !apiKeyDeliveryOwnerMatches(p, o) {
				return apperrors.ErrNotFound
			}
			result, e = s.apiKeyDeliveryTargetView(tx, p, b, a)
			return e
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if apiKeyDeliveryOwnerReview(policy, a, o) != etag {
			return apiKeyDeliveryConflict
		}
		x, e := s.apiKeyDeliverySnapshot(tx, policy, true)
		if e != nil {
			return e
		}
		if !apiKeyDeliverySubjectAllowed(x.Template, in.Principal.Subject) {
			return apperrors.ErrBadRequest
		}
		consumerID, e := apiKeyDeliveryAllocateConsumer(in.ConsumerID)
		if e != nil {
			return e
		}
		if in.ConsumerID != "" {
			var consumer entity.APIKeyDeliveryConsumer
			if e = personalExact(vaultDB(tx), "owner_digest", o.Digest).Where("consumer_id = ?", consumerID).Take(&consumer).Error; e != nil {
				return e
			}
			if consumer.OwnerKind != o.Kind || consumer.OwnerID != o.ID || !consumer.OwnerBirth.Equal(o.Birth) || consumer.OwnerDigest != o.Digest || consumer.ConsumerID != consumerID || consumer.ApplicationName != in.ApplicationName {
				return apiKeyDeliveryConflict
			}
		}
		target := apiKeyDeliveryDigest("api-key-delivery:target:v1", struct{ Owner, Consumer, Environment string }{o.Digest, consumerID, in.Environment})
		var p entity.VaultAccessProfile
		var b entity.VaultPrincipalBinding
		e = personalExact(vaultDB(tx), "target_digest", target).Take(&p).Error
		if e == nil {
			p, b, e = apiKeyDeliveryProfile(tx, p.ID)
			if e != nil {
				return e
			}
			if !apiKeyDeliveryOwnerMatches(p, o) || p.ConsumerID != consumerID || p.Environment != in.Environment || b.Subject != in.Principal.Subject || p.ApplicationName != in.ApplicationName {
				return apiKeyDeliveryConflict
			}
		} else {
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			now := s.secretNow()
			pid, e := newAPIKeyDeliveryID("vap")
			if e != nil {
				return e
			}
			bid, e := newAPIKeyDeliveryID("vpb")
			if e != nil {
				return e
			}
			revision, e := apiKeyDeliveryRevision()
			if e != nil {
				return e
			}
			p = entity.VaultAccessProfile{ID: pid, OwnerKind: o.Kind, OwnerID: o.ID, OwnerBirth: o.Birth, OwnerDigest: o.Digest, ConsumerID: consumerID, Environment: in.Environment, TargetDigest: target, ApplicationName: in.ApplicationName, ReviewRevision: revision, ConfigRevision: revision, SecurityRevision: revision, Status: "pending", Enabled: true, GeneratedPolicy: "routex-api-key-" + pid, IsolationState: "missing", CreatedAt: now, UpdatedAt: now}
			apiKeyDeliveryApplySnapshot(&p, x)
			b = entity.VaultPrincipalBinding{ID: bid, ProfileID: p.ID, ProfileBirth: now, Method: "jwt", SecurityRevision: revision, Issuer: p.Issuer, Audience: p.Audience, Subject: in.Principal.Subject, GeneratedRole: "routex-api-key-" + bid, CreatedAt: now, UpdatedAt: now}
			b.SubjectDigest = apiKeyDeliveryDigest("api-key-delivery:subject:v1", struct{ Issuer, Audience, Subject string }{b.Issuer, b.Audience, b.Subject})
			var consumer entity.APIKeyDeliveryConsumer
			e = personalExact(vaultDB(tx), "owner_digest", o.Digest).Where("consumer_id = ?", consumerID).Take(&consumer).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				cid, e := newAPIKeyDeliveryID("kdc")
				if e != nil {
					return e
				}
				consumer = entity.APIKeyDeliveryConsumer{ID: cid, OwnerKind: o.Kind, OwnerID: o.ID, OwnerBirth: o.Birth, OwnerDigest: o.Digest, ConsumerID: consumerID, ApplicationName: in.ApplicationName, CreatedAt: now, UpdatedAt: now}
				if e = vaultDB(tx).Create(&consumer).Error; e != nil {
					return e
				}
			} else if e != nil {
				return e
			} else if consumer.OwnerDigest != o.Digest || consumer.ConsumerID != consumerID || consumer.ApplicationName != in.ApplicationName {
				return apiKeyDeliveryConflict
			}
			if e = vaultDB(tx).Create(&p).Error; e != nil {
				return e
			}
			if e = vaultDB(tx).Create(&b).Error; e != nil {
				return e
			}
		}
		cid, e := newAPIKeyDeliveryID("kdo")
		if e != nil {
			return e
		}
		command := entity.APIKeyDeliveryCommand{ID: cid, OwnerKind: o.Kind, OwnerID: o.ID, OwnerBirth: o.Birth, OwnerDigest: o.Digest, RequestID: in.RequestID, ActorID: a.User.ID, ActorBirth: a.User.CreatedAt, SessionID: a.Session.ID, SessionBirth: a.Session.CreatedAt, Purpose: "resolve", IntentDigest: intent, ReviewRevision: etag, ProfileID: p.ID, ProfileBirth: p.CreatedAt, PrincipalID: b.ID, PrincipalBirth: b.CreatedAt, PolicySecurityRevision: policy.SecurityRevision, DeliveryStatus: "not_attempted", RuntimeStatus: "pending", Reason: in.Reason, CreatedAt: s.secretNow(), UpdatedAt: s.secretNow()}
		if e = vaultDB(tx).Create(&command).Error; e != nil {
			return e
		}
		if e = apiKeyDeliveryAudit(tx, a.User.ID, "resolve", p.ID, in.RequestID, in.Reason); e != nil {
			return e
		}
		result, e = s.apiKeyDeliveryTargetView(tx, p, b, a)
		return e
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
func (s *Service) GetAPIKeyDeliveryTarget(parent context.Context, a *Authentication, project, target string) (*APIKeyDeliveryTargetView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	var result APIKeyDeliveryTargetView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		o, e := apiKeyDeliveryOwnerCurrent(tx, a, project)
		if e != nil {
			return e
		}
		p, b, e := apiKeyDeliveryProfile(tx, target)
		if e != nil {
			return e
		}
		if !apiKeyDeliveryOwnerMatches(p, o) {
			return apperrors.ErrNotFound
		}
		result, e = s.apiKeyDeliveryTargetView(tx, p, b, a)
		return e
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
func (s *Service) ApproveAPIKeyDeliveryPrincipal(parent context.Context, a *Authentication, target, etag string, in APIKeyDeliveryApprovalInput) (*APIKeyDeliveryTargetView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	if !validRegistrationReason(in.Reason) || !personalModelETag(etag) || in.Approved && !in.ControlVerified || !in.Approved && in.ControlVerified {
		return nil, apperrors.ErrBadRequest
	}
	return s.apiKeyDeliveryAttestAction(parent, a, target, etag, in.Reason, func(tx *gorm.DB, p *entity.VaultAccessProfile, b *entity.VaultPrincipalBinding, u entity.User) error {
		rev, e := apiKeyDeliveryRevision()
		if e != nil {
			return e
		}
		now := s.secretNow()
		b.Enabled = in.Approved
		b.ApprovalRevision = rev
		b.ApprovalActorID = u.ID
		b.ApprovalActorBirth = &u.CreatedAt
		b.ApprovedAt = &now
		b.ApprovalReason = in.Reason
		b.SecurityRevision = rev
		p.SecurityRevision = rev
		return vaultDB(tx).Save(b).Error
	})
}
func (s *Service) AttestAPIKeyDeliveryIsolation(parent context.Context, a *Authentication, target, etag string, in APIKeyDeliveryIsolationInput) (*APIKeyDeliveryTargetView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	if !validRegistrationReason(in.Reason) || !personalModelETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	return s.apiKeyDeliveryAttestAction(parent, a, target, etag, in.Reason, func(tx *gorm.DB, p *entity.VaultAccessProfile, b *entity.VaultPrincipalBinding, u entity.User) error {
		revision, e := apiKeyDeliveryRevision()
		if e != nil {
			return e
		}
		att := apiKeyDeliveryNewAttestation(*p, *b)
		att.Revision = revision
		att.TupleDigest = apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(*p, *b))
		att.State = "revoked"
		if in.Isolated {
			att.State = "operator_attested"
		}
		att.OperatorID, att.OperatorBirth, att.Reason, att.RecordedAt = u.ID, u.CreatedAt, in.Reason, s.secretNow()
		if e = vaultDB(tx).Create(&att).Error; e != nil {
			return e
		}
		p.CurrentIsolationRevision, p.IsolationState = revision, att.State
		return nil
	})
}
func (s *Service) apiKeyDeliveryAttestAction(parent context.Context, a *Authentication, target, etag, reason string, apply func(*gorm.DB, *entity.VaultAccessProfile, *entity.VaultPrincipalBinding, entity.User) error) (*APIKeyDeliveryTargetView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, false)
	if e != nil {
		return nil, e
	}
	defer cancel()
	var result APIKeyDeliveryTargetView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", false)
		if e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		x, e := s.apiKeyDeliverySnapshot(tx, policy, false)
		if e != nil {
			return e
		}
		p, b, e := apiKeyDeliveryProfile(tx, target)
		if e != nil {
			return e
		}
		if apiKeyDeliveryTargetReview(p, b, a) != etag || !apiKeyDeliveryTargetCurrent(p, b, x) {
			return apiKeyDeliveryConflict
		}
		if e = apply(tx, &p, &b, u); e != nil {
			return e
		}
		p.Status, p.ReadinessDigest, p.ReadinessAt = "pending", "", nil
		p.ReviewRevision, e = apiKeyDeliveryRevision()
		if e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&p).Error; e != nil {
			return e
		}
		if e = apiKeyDeliveryAudit(tx, a.User.ID, "attest", p.ID, "", reason); e != nil {
			return e
		}
		result, e = s.apiKeyDeliveryTargetView(tx, p, b, a)
		return e
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
func (s *Service) ReconcileAPIKeyDeliveryTarget(parent context.Context, a *Authentication, target, etag string, in APIKeyDeliveryReconcileInput) (*APIKeyDeliveryTargetView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, false)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !validRegistrationReason(in.Reason) || !personalModelETag(etag) || !credentialReplacementRequestID.MatchString(in.RequestID) {
		return nil, apperrors.ErrBadRequest
	}
	ticket, e := s.credentialSources.processAcquire()
	if e != nil {
		return nil, e
	}
	defer ticket.release()
	var x apiKeyDeliverySnapshot
	var p entity.VaultAccessProfile
	var b entity.VaultPrincipalBinding
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", true); e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		x, e = s.apiKeyDeliverySnapshot(tx, policy, true)
		if e != nil {
			return e
		}
		p, b, e = apiKeyDeliveryProfile(tx, target)
		if e != nil {
			return e
		}
		if apiKeyDeliveryTargetReview(p, b, a) != etag || !apiKeyDeliveryTargetCurrent(p, b, x) || !p.Enabled || !b.Enabled || b.ApprovedAt == nil {
			return apiKeyDeliveryConflict
		}
		_, code, e := apiKeyDeliveryIsolation(tx, p, b)
		if e != nil {
			return e
		}
		if code != "" {
			return apiKeyDeliveryUnavailable
		}
		p.ReviewRevision, e = apiKeyDeliveryRevision()
		if e != nil {
			return e
		}
		p.Status, p.ReadinessDigest, p.ReadinessAt = "pending", "", nil
		if e = vaultDB(tx).Save(&p).Error; e != nil {
			return e
		}
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	sample, e := newAPIKeyDeliveryID("key")
	if e != nil {
		return nil, e
	}
	if _, e = s.apiKeyDeliveryValidateMachines(ctx, x, p.ID, b.ID, sample); e != nil {
		return nil, e
	}
	client, e := vault.NewAPIKeyProvisioner(apiKeyDeliveryProvisionerConfig(x), s.allowPrivateUpstream)
	if e != nil {
		return nil, apiKeyDeliveryUnavailable
	}
	defer client.Close()
	prepared, e := client.Prepare(apiKeyDeliveryProvisionerInput(p, b, x.Template))
	if e != nil {
		return nil, apiKeyDeliveryUnavailable
	}
	defer prepared.Close()
	ref, e := apiKeyDeliveryAuthReference(x.Provisioner)
	if e != nil {
		return nil, apiKeyDeliveryUnavailable
	}
	token, e := s.openSecret(ref, x.Provisioner.AuthCiphertext)
	if e != nil {
		return nil, apiKeyDeliveryUnavailable
	}
	observed, e := client.Reconcile(ctx, token, prepared)
	state := client.ResponseCloseState()
	if state.Failed || state.Pending != 0 {
		ticket.poison()
	}
	if e != nil || !observed.DesiredGrantsReconciled || observed.Outcome != vault.APIKeyProvisionerReconciled || ctx.Err() != nil || state.Failed || state.Pending != 0 {
		return nil, apiKeyDeliveryUnavailable
	}
	var result APIKeyDeliveryTargetView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", true); e != nil {
			return e
		}
		policy, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		current, e := s.apiKeyDeliverySnapshot(tx, policy, true)
		if e != nil {
			return e
		}
		cp, cb, e := apiKeyDeliveryProfile(tx, target)
		if e != nil {
			return e
		}
		if cp.ReviewRevision != p.ReviewRevision || !apiKeyDeliveryTargetCurrent(cp, cb, current) || !cp.Enabled || !cb.Enabled || cb.ApprovedAt == nil {
			return apiKeyDeliveryConflict
		}
		_, code, e := apiKeyDeliveryIsolation(tx, cp, cb)
		if e != nil {
			return e
		}
		if code != "" {
			return apiKeyDeliveryUnavailable
		}
		now := s.secretNow()
		cp.Status = "ready"
		cp.ReadinessDigest = apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(cp, cb))
		cp.ReadinessAt = &now
		cb.ReconciledRevision = cp.ReadinessDigest
		cp.ReviewRevision, e = apiKeyDeliveryRevision()
		if e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&cb).Error; e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&cp).Error; e != nil {
			return e
		}
		if e = apiKeyDeliveryAudit(tx, a.User.ID, "reconcile", cp.ID, in.RequestID, in.Reason); e != nil {
			return e
		}
		result, e = s.apiKeyDeliveryTargetView(tx, cp, cb, a)
		return e
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}

// Separate administrator read exposes only the reviewed public security tuple.
type APIKeyDeliveryTargetReviewView struct {
	APIKeyDeliveryTargetView
	Tuple apiKeyDeliveryTuple `json:"security_tuple"`
}

func (s *Service) ReviewAPIKeyDeliveryTarget(parent context.Context, a *Authentication, target string) (*APIKeyDeliveryTargetReviewView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	var result APIKeyDeliveryTargetReviewView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.read", false); e != nil {
			return e
		}
		p, b, e := apiKeyDeliveryProfile(tx, target)
		if e != nil {
			return e
		}
		result.APIKeyDeliveryTargetView, e = s.apiKeyDeliveryTargetView(tx, p, b, a)
		if e != nil {
			return e
		}
		result.Tuple = apiKeyDeliveryTargetTuple(p, b)
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}

type APIKeyDeliveryTargetSummary struct {
	APIKeyDeliveryTargetView
	OwnerKind string `json:"owner_kind"`
	OwnerID   string `json:"owner_id"`
}
type APIKeyDeliveryTargetPage struct {
	Items      []APIKeyDeliveryTargetSummary `json:"items"`
	NextCursor *string                       `json:"next_cursor"`
}

func (s *Service) ListAPIKeyDeliveryTargets(parent context.Context, a *Authentication, cursor string, limit int) (*APIKeyDeliveryTargetPage, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if limit < 1 || limit > 100 || cursor != "" && !rootSafeIdentity(cursor, 30) {
		return nil, apperrors.ErrBadRequest
	}
	result := APIKeyDeliveryTargetPage{Items: []APIKeyDeliveryTargetSummary{}}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.read", false); e != nil {
			return e
		}
		if cursor != "" {
			var anchor entity.VaultAccessProfile
			if e := personalExact(vaultDB(tx), "id", cursor).Take(&anchor).Error; e != nil {
				return e
			}
			if anchor.ID != cursor {
				return apperrors.ErrBadRequest
			}
		}
		var profiles []entity.VaultAccessProfile
		q := vaultDB(tx).Order("id").Limit(limit + 1)
		if cursor != "" {
			q = q.Where("id > ?", cursor)
		}
		if e := q.Find(&profiles).Error; e != nil {
			return e
		}
		more := len(profiles) > limit
		if more {
			profiles = profiles[:limit]
		}
		for _, row := range profiles {
			p, b, e := apiKeyDeliveryProfile(tx, row.ID)
			if e != nil {
				return e
			}
			v, e := s.apiKeyDeliveryTargetView(tx, p, b, a)
			if e != nil {
				return e
			}
			result.Items = append(result.Items, APIKeyDeliveryTargetSummary{APIKeyDeliveryTargetView: v, OwnerKind: p.OwnerKind, OwnerID: p.OwnerID})
		}
		if more {
			next := profiles[len(profiles)-1].ID
			result.NextCursor = &next
		}
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
