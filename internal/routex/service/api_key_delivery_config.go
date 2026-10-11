package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
	"slices"
)

func (s *Service) apiKeyDeliveryPolicyView(tx *gorm.DB, p entity.APIKeyDeliveryPolicy, a *Authentication) APIKeyDeliveryPolicyView {
	a = apiKeyDeliveryCopyAuthentication(a)
	modes := []string{}
	if p.AllowManual {
		modes = append(modes, "manual")
	}
	if p.AllowManagedVault {
		modes = append(modes, "managed_vault")
	}
	available := s.apiKeyDeliveryValidator != nil && p.VerifiedRevision == p.SecurityRevision && p.VerifiedAt != nil
	if available {
		_, e := s.apiKeyDeliverySnapshot(tx, p, true)
		available = e == nil
	}
	code := ""
	if !available {
		code = "dependency_validation_unavailable"
	}
	return APIKeyDeliveryPolicyView{APIKeyDeliveryContextView: APIKeyDeliveryContextView{AllowedModes: modes, DefaultMode: p.DefaultMode, ManagedAvailable: available, FailureCode: apiKeyDeliveryFailure(code), ReviewETag: apiKeyDeliveryPolicyReview(p, a)}, IntegrationID: p.IntegrationID, WriterIdentityID: p.WriterIdentityID, ProvisionerIdentityID: p.ProvisionerIdentityID, TemplateConfigured: p.TemplateID == "jwt"}
}
func (s *Service) GetAPIKeyDeliveryPolicy(parent context.Context, a *Authentication) (*APIKeyDeliveryPolicyView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	var v APIKeyDeliveryPolicyView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.read", false); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, false)
		if e != nil {
			return e
		}
		v = s.apiKeyDeliveryPolicyView(tx, p, a)
		return ctx.Err()
	})
	return &v, apiKeyDeliveryError(e)
}
func (s *Service) SaveAPIKeyDeliveryPolicy(parent context.Context, a *Authentication, etag string, in APIKeyDeliveryPolicyInput) (*APIKeyDeliveryPolicyView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	in.AllowedModes = slices.Clone(in.AllowedModes)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !personalModelETag(etag) || !validRegistrationReason(in.Reason) || len(in.AllowedModes) < 1 || len(in.AllowedModes) > 2 {
		return nil, apperrors.ErrBadRequest
	}
	manual, managed := false, false
	for _, m := range in.AllowedModes {
		switch m {
		case "manual":
			if manual {
				return nil, apperrors.ErrBadRequest
			}
			manual = true
		case "managed_vault":
			if managed {
				return nil, apperrors.ErrBadRequest
			}
			managed = true
		default:
			return nil, apperrors.ErrBadRequest
		}
	}
	if in.DefaultMode == "manual" && !manual || in.DefaultMode == "managed_vault" && !managed || in.DefaultMode != "manual" && in.DefaultMode != "managed_vault" {
		return nil, apperrors.ErrBadRequest
	}
	var v APIKeyDeliveryPolicyView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", false); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		if apiKeyDeliveryPolicyReview(p, a) != etag {
			return apiKeyDeliveryConflict
		}
		old := p
		if in.IntegrationID != "" {
			var integration entity.VaultIntegration
			var revision entity.VaultRevision
			var template entity.APIKeyCallerTemplate
			if e = personalExact(vaultDB(tx), "id", in.IntegrationID).Take(&integration).Error; e != nil {
				return e
			}
			if integration.ID != in.IntegrationID {
				return apperrors.ErrNotFound
			}
			if e = personalExact(vaultDB(tx), "id", integration.RevisionID).Take(&revision).Error; e != nil {
				return e
			}
			if revision.IntegrationID != integration.ID || !revision.IntegrationBirth.Equal(integration.CreatedAt) {
				return apiKeyDeliveryConflict
			}
			if e = vaultDB(tx).Take(&template, "id = ?", "jwt").Error; e != nil {
				return e
			}
			p.IntegrationID, p.IntegrationBirth, p.VaultRevisionID, p.VaultRevisionBirth = integration.ID, &integration.CreatedAt, revision.ID, &revision.CreatedAt
			p.IntegrationRevision = s.apiKeyDeliveryInfrastructure(revision)
			p.InfrastructureSecurityRevision = p.IntegrationRevision
			p.TemplateID, p.TemplateSecurityRevision = template.ID, template.SecurityRevision
			p.WriterIdentityID, p.ProvisionerIdentityID = in.WriterIdentityID, in.ProvisionerIdentityID
			if _, e = s.apiKeyDeliverySnapshot(tx, p, false); e != nil {
				return e
			}
		} else if in.WriterIdentityID != "" || in.ProvisionerIdentityID != "" {
			return apperrors.ErrBadRequest
		} else {
			p.IntegrationID = ""
			p.IntegrationBirth = nil
			p.VaultRevisionID = ""
			p.VaultRevisionBirth = nil
			p.IntegrationRevision = ""
			p.InfrastructureSecurityRevision = ""
			p.WriterIdentityID = ""
			p.ProvisionerIdentityID = ""
			p.TemplateID = ""
			p.TemplateSecurityRevision = ""
		}
		securityChanged := old.IntegrationID != p.IntegrationID || old.IntegrationRevision != p.IntegrationRevision || old.WriterIdentityID != p.WriterIdentityID || old.ProvisionerIdentityID != p.ProvisionerIdentityID || old.TemplateSecurityRevision != p.TemplateSecurityRevision
		if securityChanged {
			p.SecurityRevision, e = apiKeyDeliveryRevision()
			if e != nil {
				return e
			}
		}
		if managed && (s.apiKeyDeliveryValidator == nil || p.VerifiedRevision != p.SecurityRevision || p.VerifiedAt == nil) {
			return apiKeyDeliveryUnavailable
		}
		if old.AllowManual != manual || old.AllowManagedVault != managed || old.DefaultMode != in.DefaultMode || securityChanged {
			p.ReviewRevision, e = apiKeyDeliveryRevision()
			if e != nil {
				return e
			}
		}
		p.AllowManual, p.AllowManagedVault, p.DefaultMode = manual, managed, in.DefaultMode
		p.UpdatedAt = s.secretNow()
		if e = vaultDB(tx).Save(&p).Error; e != nil {
			return e
		}
		if e = apiKeyDeliveryAudit(tx, a.User.ID, "policy", "policy", "", in.Reason); e != nil {
			return e
		}
		v = s.apiKeyDeliveryPolicyView(tx, p, a)
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &v, nil
}
func (s *Service) SaveAPIKeyDeliveryIdentity(parent context.Context, a *Authentication, etag string, in APIKeyDeliveryIdentityInput) (*APIKeyDeliveryIdentityView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !personalModelETag(etag) || !validRegistrationReason(in.Reason) || !vaultToken(in.Token) || in.Purpose != "writer" && in.Purpose != "provisioner" {
		return nil, apperrors.ErrBadRequest
	}
	var auth entity.APIKeyDeliveryAuth
	var row entity.VaultIntegration
	var rev entity.VaultRevision
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", false); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		if apiKeyDeliveryPolicyReview(p, a) != etag {
			return apiKeyDeliveryConflict
		}
		if e = personalExact(vaultDB(tx), "id", in.IntegrationID).Take(&row).Error; e != nil {
			return e
		}
		if row.ID != in.IntegrationID {
			return apperrors.ErrNotFound
		}
		return personalExact(vaultDB(tx), "id", row.RevisionID).Take(&rev).Error
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	auth.ID, e = newAPIKeyDeliveryID("kda")
	if e != nil {
		return nil, e
	}
	auth.SecretGeneration, e = newAPIKeyDeliveryID("kdg")
	if e != nil {
		return nil, e
	}
	auth.Purpose, auth.Method = in.Purpose, "token"
	auth.IntegrationID, auth.IntegrationBirth = row.ID, row.CreatedAt
	auth.VaultRevisionID, auth.VaultRevisionBirth = rev.ID, rev.CreatedAt
	auth.IntegrationRevision = s.apiKeyDeliveryInfrastructure(rev)
	auth.CreatedAt, auth.UpdatedAt = s.secretNow(), s.secretNow()
	reference, e := apiKeyDeliveryAuthReference(auth)
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	epoch := s.secretEpoch()
	auth.AuthCiphertext, e = s.sealSecret(reference, in.Token)
	in.Token = ""
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	var result APIKeyDeliveryIdentityView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", false); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		if apiKeyDeliveryPolicyReview(p, a) != etag {
			return apiKeyDeliveryConflict
		}
		var current entity.VaultIntegration
		if e = personalExact(vaultDB(tx), "id", row.ID).Take(&current).Error; e != nil {
			return e
		}
		if current.ID != row.ID || !current.CreatedAt.Equal(row.CreatedAt) || current.RevisionID != rev.ID {
			return apiKeyDeliveryConflict
		}
		if e = s.guardSecretWrite(tx, epoch, reference, auth.AuthCiphertext); e != nil {
			return e
		}
		if e = vaultDB(tx).Create(&auth).Error; e != nil {
			return e
		}
		p.ReviewRevision, e = apiKeyDeliveryRevision()
		if e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&p).Error; e != nil {
			return e
		}
		if e = apiKeyDeliveryAudit(tx, a.User.ID, "identity", auth.ID, "", in.Reason); e != nil {
			return e
		}
		result = APIKeyDeliveryIdentityView{ID: auth.ID, Purpose: auth.Purpose, Configured: true, ReviewETag: apiKeyDeliveryPolicyReview(p, a)}
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
func (s *Service) SaveAPIKeyCallerTemplate(parent context.Context, a *Authentication, etag string, in APIKeyCallerTemplateInput) (*APIKeyCallerTemplateView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !personalModelETag(etag) || !validRegistrationReason(in.Reason) || len(in.AllowedSubjects) < 1 || len(in.AllowedSubjects) > 128 || !apiKeyDeliveryText(in.Audience, 256) || in.TokenTTLSeconds < 1 || in.TokenTTLSeconds > in.TokenMaxTTLSeconds || in.TokenMaxTTLSeconds > 3600 {
		return nil, apperrors.ErrBadRequest
	}
	subjects := slices.Clone(in.AllowedSubjects)
	slices.Sort(subjects)
	if len(slices.Compact(slices.Clone(subjects))) != len(subjects) {
		return nil, apperrors.ErrBadRequest
	}
	for _, v := range subjects {
		if !apiKeyDeliveryText(v, 256) {
			return nil, apperrors.ErrBadRequest
		}
	}
	// SDK constructor validates exact trust syntax without I/O, using a harmless
	// fixed local descriptor only for its unrelated KV syntax parameters.
	client, e := vault.NewAPIKeyProvisioner(vault.APIKeyProvisionerConfig{Endpoint: "https://vault.invalid", KVMount: "kv", KeyPrefix: "api-keys", AuthMount: in.AuthMount, Issuer: in.Issuer, JWKSURL: in.JWKSURL}, false)
	if e != nil {
		return nil, apperrors.ErrBadRequest
	}
	client.Close()
	raw, e := json.Marshal(subjects)
	if e != nil {
		return nil, apperrors.ErrBadRequest
	}
	var view APIKeyCallerTemplateView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", false); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		if apiKeyDeliveryPolicyReview(p, a) != etag {
			return apiKeyDeliveryConflict
		}
		var old entity.APIKeyCallerTemplate
		e = vaultDB(tx).Take(&old, "id = ?", "jwt").Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		t := old
		t.ID = "jwt"
		t.AuthMount, t.Issuer, t.JWKSURL, t.Audience, t.AllowedSubjectsJSON = in.AuthMount, in.Issuer, in.JWKSURL, in.Audience, string(raw)
		t.TokenTTLSeconds, t.TokenMaxTTLSeconds = in.TokenTTLSeconds, in.TokenMaxTTLSeconds
		changed := old.ID == "" || old.AuthMount != t.AuthMount || old.Issuer != t.Issuer || old.JWKSURL != t.JWKSURL || old.Audience != t.Audience || old.AllowedSubjectsJSON != t.AllowedSubjectsJSON || old.TokenTTLSeconds != t.TokenTTLSeconds || old.TokenMaxTTLSeconds != t.TokenMaxTTLSeconds
		if changed {
			t.SecurityRevision, e = apiKeyDeliveryRevision()
			if e != nil {
				return e
			}
			t.TrustSecurityRevision = t.SecurityRevision
			t.ReviewRevision = t.SecurityRevision
			p.SecurityRevision, e = apiKeyDeliveryRevision()
			if e != nil {
				return e
			}
			p.TemplateSecurityRevision = t.SecurityRevision
			p.ReviewRevision, e = apiKeyDeliveryRevision()
			if e != nil {
				return e
			}
		}
		t.UpdatedAt = s.secretNow()
		if t.CreatedAt.IsZero() {
			t.CreatedAt = t.UpdatedAt
		}
		if e = vaultDB(tx).Save(&t).Error; e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&p).Error; e != nil {
			return e
		}
		if e = apiKeyDeliveryAudit(tx, a.User.ID, "template", "jwt", "", in.Reason); e != nil {
			return e
		}
		view = APIKeyCallerTemplateView{AuthMount: t.AuthMount, Issuer: t.Issuer, JWKSURL: t.JWKSURL, Audience: t.Audience, AllowedSubjects: slices.Clone(subjects), TokenTTLSeconds: t.TokenTTLSeconds, TokenMaxTTLSeconds: t.TokenMaxTTLSeconds, Verified: t.VerifiedAt != nil && t.VerifiedRevision == t.SecurityRevision, ReviewETag: apiKeyDeliveryPolicyReview(p, a)}
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &view, nil
}
func (s *Service) VerifyAPIKeyDeliveryDependencies(parent context.Context, a *Authentication, etag, reason string) (*APIKeyDeliveryPolicyView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, false)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !personalModelETag(etag) || !validRegistrationReason(reason) {
		return nil, apperrors.ErrBadRequest
	}
	ticket, e := s.credentialSources.processAcquire()
	if e != nil {
		return nil, e
	}
	defer ticket.release()
	var x apiKeyDeliverySnapshot
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", true); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		if apiKeyDeliveryPolicyReview(p, a) != etag {
			return apiKeyDeliveryConflict
		}
		x, e = s.apiKeyDeliverySnapshot(tx, p, false)
		return e
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	profile, e := newAPIKeyDeliveryID("vap")
	if e != nil {
		return nil, e
	}
	principal, e := newAPIKeyDeliveryID("vpb")
	if e != nil {
		return nil, e
	}
	resource, e := newAPIKeyDeliveryID("key")
	if e != nil {
		return nil, e
	}
	if _, e = s.apiKeyDeliveryValidateMachines(ctx, x, profile, principal, resource); e != nil {
		return nil, e
	}
	var v APIKeyDeliveryPolicyView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.write", true); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, true)
		if e != nil {
			return e
		}
		if apiKeyDeliveryPolicyReview(p, a) != etag || p.SecurityRevision != x.Policy.SecurityRevision {
			return apiKeyDeliveryConflict
		}
		current, e := s.apiKeyDeliverySnapshot(tx, p, false)
		if e != nil {
			return e
		}
		if current.Writer.ID != x.Writer.ID || current.Provisioner.ID != x.Provisioner.ID || current.Template.SecurityRevision != x.Template.SecurityRevision {
			return apiKeyDeliveryConflict
		}
		now := s.secretNow()
		p.VerifiedRevision, p.VerifiedAt = p.SecurityRevision, &now
		t := current.Template
		t.VerifiedRevision, t.VerifiedAt = t.SecurityRevision, &now
		p.ReviewRevision, e = apiKeyDeliveryRevision()
		if e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&t).Error; e != nil {
			return e
		}
		if e = vaultDB(tx).Save(&p).Error; e != nil {
			return e
		}
		if e = apiKeyDeliveryAudit(tx, a.User.ID, "verify", "policy", "", reason); e != nil {
			return e
		}
		v = s.apiKeyDeliveryPolicyView(tx, p, a)
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &v, nil
}

func (s *Service) GetAPIKeyDeliveryIdentity(parent context.Context, a *Authentication, target string) (*APIKeyDeliveryIdentityView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	if !rootSafeIdentity(target, 30) {
		return nil, apperrors.ErrBadRequest
	}
	var result APIKeyDeliveryIdentityView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.read", false); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, false)
		if e != nil {
			return e
		}
		var auth entity.APIKeyDeliveryAuth
		if e = personalExact(vaultDB(tx), "id", target).Take(&auth).Error; e != nil {
			return e
		}
		if auth.ID != target {
			return apperrors.ErrNotFound
		}
		if _, e = apiKeyDeliveryAuthReference(auth); e != nil {
			return e
		}
		result = APIKeyDeliveryIdentityView{ID: auth.ID, Purpose: auth.Purpose, Configured: auth.AuthCiphertext != "", ReviewETag: apiKeyDeliveryPolicyReview(p, a)}
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}
func (s *Service) GetAPIKeyCallerTemplate(parent context.Context, a *Authentication) (*APIKeyCallerTemplateView, error) {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, e := apiKeyDeliveryContext(parent, true)
	if e != nil {
		return nil, e
	}
	defer cancel()
	var result APIKeyCallerTemplateView
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := apiKeyDeliveryAdmin(tx, a, "secrets.read", false); e != nil {
			return e
		}
		p, e := apiKeyDeliveryPolicy(tx, false)
		if e != nil {
			return e
		}
		var t entity.APIKeyCallerTemplate
		if e = vaultDB(tx).Take(&t, "id = ?", "jwt").Error; errors.Is(e, gorm.ErrRecordNotFound) {
			result = apiKeyDeliveryUnconfiguredTemplate(apiKeyDeliveryPolicyReview(p, a))
			return ctx.Err()
		} else if e != nil {
			return e
		}
		var subjects []string
		if len(t.AllowedSubjectsJSON) > 32768 || json.Unmarshal([]byte(t.AllowedSubjectsJSON), &subjects) != nil || len(subjects) > 128 {
			return apiKeyDeliveryUnavailable
		}
		if subjects == nil {
			subjects = []string{}
		}
		result = APIKeyCallerTemplateView{AuthMount: t.AuthMount, Issuer: t.Issuer, JWKSURL: t.JWKSURL, Audience: t.Audience, AllowedSubjects: slices.Clone(subjects), TokenTTLSeconds: t.TokenTTLSeconds, TokenMaxTTLSeconds: t.TokenMaxTTLSeconds, Verified: t.VerifiedAt != nil && t.VerifiedRevision == t.SecurityRevision, ReviewETag: apiKeyDeliveryPolicyReview(p, a)}
		return ctx.Err()
	})
	if e != nil {
		return nil, apiKeyDeliveryError(e)
	}
	return &result, nil
}

func apiKeyDeliveryUnconfiguredTemplate(review string) APIKeyCallerTemplateView {
	return APIKeyCallerTemplateView{AllowedSubjects: []string{}, ReviewETag: review}
}
