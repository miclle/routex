package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

func freezeConfigurationAPIKeyV1(v entity.APIKey) runtimeConfigurationAPIKeyV1 {
	return runtimeConfigurationAPIKeyV1{
		DeliveryMode:      v.DeliveryMode,
		LifecycleRevision: v.LifecycleRevision,
		ID:                v.ID,
		UserID:            v.UserID,
		Name:              v.Name,
		Prefix:            v.Prefix,
		TokenHash:         v.TokenHash,
		Status:            v.Status,
		ExpiresAt:         freezeConfigurationPointerV1(v.ExpiresAt, func(t time.Time) time.Time { return t.UTC() }),
		DeliveryExpiresAt: freezeConfigurationPointerV1(v.DeliveryExpiresAt, func(t time.Time) time.Time { return t.UTC() }),
		ReplacesKeyID:     freezeConfigurationPointerV1(v.ReplacesKeyID, func(s string) string { return s }),
		ActivateOnConfirm: v.ActivateOnConfirm,
		CreatedAt:         v.CreatedAt.UTC(),
		UpdatedAt:         v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationAPIKeyModelV1(v entity.APIKeyModel) runtimeConfigurationAPIKeyModelV1 {
	return runtimeConfigurationAPIKeyModelV1{
		KeyID:   v.KeyID,
		ModelID: v.ModelID,
	}
}

func freezeConfigurationProjectV1(v entity.Project) runtimeConfigurationProjectV1 {
	return runtimeConfigurationProjectV1{
		ID:          v.ID,
		Name:        v.Name,
		Description: v.Description,
		Status:      v.Status,
		CreatorID:   v.CreatorID,
		CreatedAt:   v.CreatedAt.UTC(),
		UpdatedAt:   v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationProjectKeyV1(v entity.ProjectKey) runtimeConfigurationProjectKeyV1 {
	return runtimeConfigurationProjectKeyV1{
		ID:                v.ID,
		ProjectID:         v.ProjectID,
		CreatorID:         v.CreatorID,
		Name:              v.Name,
		Prefix:            v.Prefix,
		TokenHash:         v.TokenHash,
		Status:            v.Status,
		DeliveryMode:      v.DeliveryMode,
		ExpiresAt:         freezeConfigurationPointerV1(v.ExpiresAt, func(t time.Time) time.Time { return t.UTC() }),
		DeliveryExpiresAt: freezeConfigurationPointerV1(v.DeliveryExpiresAt, func(t time.Time) time.Time { return t.UTC() }),
		ReplacesKeyID:     freezeConfigurationPointerV1(v.ReplacesKeyID, func(s string) string { return s }),
		ActivateOnConfirm: v.ActivateOnConfirm,
		CreatedAt:         v.CreatedAt.UTC(),
		UpdatedAt:         v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationProjectManagerV1(v entity.ProjectManager) runtimeConfigurationProjectManagerV1 {
	return runtimeConfigurationProjectManagerV1{
		ID:        v.ID,
		ProjectID: v.ProjectID,
		UserID:    v.UserID,
	}
}

func freezeConfigurationProjectModelGrantV1(v entity.ProjectModelGrant) runtimeConfigurationProjectModelGrantV1 {
	return runtimeConfigurationProjectModelGrantV1{
		ProjectID: v.ProjectID,
		ModelID:   v.ModelID,
	}
}

func freezeConfigurationProjectKeyModelV1(v entity.ProjectKeyModel) runtimeConfigurationProjectKeyModelV1 {
	return runtimeConfigurationProjectKeyModelV1{
		KeyID:   v.KeyID,
		ModelID: v.ModelID,
	}
}

func freezeConfigurationTeamV1(v entity.Team) runtimeConfigurationTeamV1 {
	return runtimeConfigurationTeamV1{
		ID:          v.ID,
		Name:        v.Name,
		Description: v.Description,
		Status:      v.Status,
		CreatedAt:   v.CreatedAt.UTC(),
		UpdatedAt:   v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationTeamMembershipV1(v entity.TeamMembership) runtimeConfigurationTeamMembershipV1 {
	return runtimeConfigurationTeamMembershipV1{
		JoinedAt: freezeConfigurationPointerV1(v.JoinedAt, func(t time.Time) time.Time { return t.UTC() }),
		ID:       v.ID,
		TeamID:   v.TeamID,
		UserID:   v.UserID,
		Role:     v.Role,
		Status:   v.Status,
	}
}

func freezeConfigurationTeamModelGrantV1(v entity.TeamModelGrant) runtimeConfigurationTeamModelGrantV1 {
	return runtimeConfigurationTeamModelGrantV1{
		SourceCreationReceiptID: freezeConfigurationPointerV1(v.SourceCreationReceiptID, func(s string) string { return s }),
		SourceRequestID:         freezeConfigurationPointerV1(v.SourceRequestID, func(s string) string { return s }),
		TeamID:                  v.TeamID,
		ModelID:                 v.ModelID,
	}
}

func freezeConfigurationCredentialDeploymentAttestationV1(v entity.CredentialDeploymentAttestation) runtimeConfigurationCredentialDeploymentAttestationV1 {
	return runtimeConfigurationCredentialDeploymentAttestationV1{
		CredentialID:    v.CredentialID,
		ProviderModelID: v.ProviderModelID,
		Identity:        v.Identity,
	}
}

func freezeConfigurationProviderV1(v entity.Provider) runtimeConfigurationProviderV1 {
	return runtimeConfigurationProviderV1{
		Enabled:   v.Enabled,
		ETag:      v.ETag,
		ID:        v.ID,
		Name:      v.Name,
		CreatedAt: v.CreatedAt.UTC(),
	}
}

func freezeConfigurationProviderConnectionV1(v entity.ProviderConnection) runtimeConfigurationProviderConnectionV1 {
	return runtimeConfigurationProviderConnectionV1{
		TransportGeneration: v.TransportGeneration,
		Adapter:             v.Adapter,
		APIVersion:          freezeConfigurationPointerV1(v.APIVersion, func(s string) string { return s }),
		Enabled:             v.Enabled,
		EgressMode:          v.EgressMode,
		EgressID:            freezeConfigurationPointerV1(v.EgressID, func(s string) string { return s }),
		ETag:                v.ETag,
		ID:                  v.ID,
		ProviderID:          v.ProviderID,
		Name:                v.Name,
		BaseURL:             v.BaseURL,
		Protocol:            v.Protocol,
		CreatedAt:           v.CreatedAt.UTC(),
	}
}

func freezeConfigurationProviderCredentialV1(v entity.ProviderCredential) runtimeConfigurationProviderCredentialV1 {
	return runtimeConfigurationProviderCredentialV1{
		VerifiedTransportGeneration:  v.VerifiedTransportGeneration,
		VerificationTransportCurrent: v.VerificationTransportCurrent,
		CoverageRevision:             v.CoverageRevision,
		CoverageReviewETag:           v.CoverageReviewETag,
		CoverageIntentSHA256:         v.CoverageIntentSHA256,
		StorageSource:                v.StorageSource,
		VaultReference:               freezeConfigurationPointerV1(v.VaultReference, freezeConfigurationCredentialVaultReferenceV1),
		ReplacesCredentialID:         freezeConfigurationPointerV1(v.ReplacesCredentialID, func(s string) string { return s }),
		ID:                           v.ID,
		ConnectionID:                 v.ConnectionID,
		Name:                         v.Name,
		Priority:                     v.Priority,
		Enabled:                      v.Enabled,
		VerificationStatus:           v.VerificationStatus,
		VerifiedAt:                   freezeConfigurationPointerV1(v.VerifiedAt, func(t time.Time) time.Time { return t.UTC() }),
		CreatedAt:                    v.CreatedAt.UTC(),
		SourceProof:                  credentialSourceProof(v),
	}
}

func freezeConfigurationProviderModelV1(v entity.ProviderModel) runtimeConfigurationProviderModelV1 {
	return runtimeConfigurationProviderModelV1{
		CapabilityTransportGeneration: v.CapabilityTransportGeneration,
		CapabilitiesTransportCurrent:  v.CapabilitiesTransportCurrent,
		CapabilityReviewETag:          v.CapabilityReviewETag,
		ID:                            v.ID,
		ConnectionID:                  v.ConnectionID,
		UpstreamName:                  v.UpstreamName,
		Disabled:                      v.Disabled,
		SupportsImageInput:            v.SupportsImageInput,
		SupportsPDFInput:              v.SupportsPDFInput,
		ETag:                          v.ETag,
		CreatedAt:                     v.CreatedAt.UTC(),
	}
}

func freezeConfigurationModelV1(v entity.Model) runtimeConfigurationModelV1 {
	return runtimeConfigurationModelV1{
		ID:        v.ID,
		Status:    v.Status,
		CreatedAt: v.CreatedAt.UTC(),
	}
}

func freezeConfigurationModelNameV1(v entity.ModelName) runtimeConfigurationModelNameV1 {
	return runtimeConfigurationModelNameV1{
		Name:           v.Name,
		ModelID:        v.ModelID,
		CurrentModelID: freezeConfigurationPointerV1(v.CurrentModelID, func(s string) string { return s }),
		ExpiresAt:      freezeConfigurationPointerV1(v.ExpiresAt, func(t time.Time) time.Time { return t.UTC() }),
		CreatedAt:      v.CreatedAt.UTC(),
	}
}

func freezeConfigurationModelProviderBindingV1(v entity.ModelProviderBinding) runtimeConfigurationModelProviderBindingV1 {
	return runtimeConfigurationModelProviderBindingV1{
		ID:              v.ID,
		ModelID:         v.ModelID,
		ProviderModelID: v.ProviderModelID,
		Weight:          v.Weight,
		CreatedAt:       v.CreatedAt.UTC(),
	}
}

func freezeConfigurationUserModelGrantV1(v entity.UserModelGrant) runtimeConfigurationUserModelGrantV1 {
	return runtimeConfigurationUserModelGrantV1{
		SourceRequestID: freezeConfigurationPointerV1(v.SourceRequestID, func(s string) string { return s }),
		UserID:          v.UserID,
		ModelID:         v.ModelID,
		CreatedAt:       v.CreatedAt.UTC(),
	}
}

func freezeConfigurationCredentialModelAccessV1(v entity.CredentialModelAccess) runtimeConfigurationCredentialModelAccessV1 {
	return runtimeConfigurationCredentialModelAccessV1{
		CredentialID:    v.CredentialID,
		ProviderModelID: v.ProviderModelID,
	}
}

func freezeConfigurationEgressV1(v entity.Egress) runtimeConfigurationEgressV1 {
	return runtimeConfigurationEgressV1{
		ID:               v.ID,
		Name:             v.Name,
		Kind:             v.Kind,
		Host:             v.Host,
		Port:             v.Port,
		Enabled:          v.Enabled,
		ETag:             v.ETag,
		SecretGeneration: v.SecretGeneration,
		CreatedAt:        v.CreatedAt.UTC(),
		AuthProof:        configurationCipherProofV1(v.AuthCiphertext),
	}
}

func freezeConfigurationEgressSettingV1(v entity.EgressSetting) runtimeConfigurationEgressSettingV1 {
	return runtimeConfigurationEgressSettingV1{
		ID:              v.ID,
		DefaultEgressID: freezeConfigurationPointerV1(v.DefaultEgressID, func(s string) string { return s }),
		ETag:            v.ETag,
		UpdatedAt:       v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationPricingSettingV1(v entity.PricingSetting) runtimeConfigurationPricingSettingV1 {
	return runtimeConfigurationPricingSettingV1{
		ID:               v.ID,
		ETag:             v.ETag,
		PlatformCurrency: v.PlatformCurrency,
	}
}

func freezeConfigurationPricingExchangeRateV1(v entity.PricingExchangeRate) runtimeConfigurationPricingExchangeRateV1 {
	return runtimeConfigurationPricingExchangeRateV1{
		Currency: v.Currency,
		Rate:     v.Rate,
	}
}

func freezeConfigurationModelPriceV1(v entity.ModelPrice) runtimeConfigurationModelPriceV1 {
	return runtimeConfigurationModelPriceV1{
		ID:                     v.ID,
		ProviderModelID:        v.ProviderModelID,
		ContextThreshold:       v.ContextThreshold,
		UpdateSource:           v.UpdateSource,
		FollowRepository:       v.FollowRepository,
		RepositoryThresholdKey: freezeConfigurationPointerV1(v.RepositoryThresholdKey, func(s string) string { return s }),
		CreatedAt:              v.CreatedAt.UTC(),
		UpdatedAt:              v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationPriceRateV1(v entity.PriceRate) runtimeConfigurationPriceRateV1 {
	return runtimeConfigurationPriceRateV1{
		ID:                 v.ID,
		ModelPriceID:       v.ModelPriceID,
		Metric:             v.Metric,
		Tier:               v.Tier,
		Unit:               v.Unit,
		Currency:           v.Currency,
		Amount:             v.Amount,
		Enabled:            v.Enabled,
		RepositoryModelKey: freezeConfigurationPointerV1(v.RepositoryModelKey, func(s string) string { return s }),
		RepositoryRateKey:  freezeConfigurationPointerV1(v.RepositoryRateKey, func(s string) string { return s }),
	}
}

func freezeConfigurationResourceLimitV1(v entity.ResourceLimit) runtimeConfigurationResourceLimitV1 {
	return runtimeConfigurationResourceLimitV1{
		ScopeKind:           v.ScopeKind,
		ScopeID:             v.ScopeID,
		ETag:                v.ETag,
		PreviousETag:        v.PreviousETag,
		AppliedDefaultETag:  freezeConfigurationPointerV1(v.AppliedDefaultETag, func(s string) string { return s }),
		DefaultResetETag:    freezeConfigurationPointerV1(v.DefaultResetETag, func(s string) string { return s }),
		ActorID:             v.ActorID,
		Reason:              v.Reason,
		Tokens5H:            freezeConfigurationPointerV1(v.Tokens5H, func(i int64) int64 { return i }),
		Tokens7D:            freezeConfigurationPointerV1(v.Tokens7D, func(i int64) int64 { return i }),
		TokensMonthBehavior: v.TokensMonthBehavior,
		MoneyMonthBehavior:  v.MoneyMonthBehavior,
		TokensMonth:         freezeConfigurationPointerV1(v.TokensMonth, func(i int64) int64 { return i }),
		TPM:                 freezeConfigurationPointerV1(v.TPM, func(i int64) int64 { return i }),
		MoneyMonth:          freezeConfigurationPointerV1(v.MoneyMonth, func(s string) string { return s }),
		Currency:            v.Currency,
		RPM:                 freezeConfigurationPointerV1(v.RPM, func(i int64) int64 { return i }),
		Concurrency:         freezeConfigurationPointerV1(v.Concurrency, func(i int64) int64 { return i }),
		IPMode:              v.IPMode,
		IPRangesJSON:        v.IPRangesJSON,
		UpdatedAt:           v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationQuotaSettingV1(v entity.QuotaSetting) runtimeConfigurationQuotaSettingV1 {
	return runtimeConfigurationQuotaSettingV1{
		AccountingStarted: v.AccountingStarted,
		ID:                v.ID,
		TimeZone:          v.TimeZone,
		ETag:              v.ETag,
		PreviousETag:      v.PreviousETag,
		ActorID:           v.ActorID,
		Reason:            v.Reason,
		UpdatedAt:         v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationReservationBoundV1(v entity.ReservationBound) runtimeConfigurationReservationBoundV1 {
	return runtimeConfigurationReservationBoundV1{
		TransportGeneration: v.TransportGeneration,
		ProviderModelID:     v.ProviderModelID,
		Protocol:            v.Protocol,
		MaxInputTokens:      v.MaxInputTokens,
		MaxOutputTokens:     v.MaxOutputTokens,
		Evidence:            v.Evidence,
		ETag:                v.ETag,
		PreviousETag:        v.PreviousETag,
		ActorID:             v.ActorID,
		Reason:              v.Reason,
		UpdatedAt:           v.UpdatedAt.UTC(),
	}
}

func freezeConfigurationSessionV1(v entity.Session) runtimeConfigurationSessionV1 {
	return runtimeConfigurationSessionV1{
		EmergencyPolicyRevision:       v.EmergencyPolicyRevision,
		EmergencyUserCreatedAt:        freezeConfigurationPointerV1(v.EmergencyUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		EmergencyMFAGeneration:        v.EmergencyMFAGeneration,
		EmergencyPasswordDigest:       v.EmergencyPasswordDigest,
		EmergencyFactorCompletedAt:    freezeConfigurationPointerV1(v.EmergencyFactorCompletedAt, func(t time.Time) time.Time { return t.UTC() }),
		NamedIdentityProviderID:       v.NamedIdentityProviderID,
		NamedIdentityProfileID:        v.NamedIdentityProfileID,
		NamedIdentityBindingID:        v.NamedIdentityBindingID,
		NamedIdentityBindingCreatedAt: freezeConfigurationPointerV1(v.NamedIdentityBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		NamedIdentityConfigRevision:   v.NamedIdentityConfigRevision,
		NamedIdentityPolicyRevision:   v.NamedIdentityPolicyRevision,
		NamedIdentityUserCreatedAt:    freezeConfigurationPointerV1(v.NamedIdentityUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		SAMLBindingID:                 v.SAMLBindingID,
		SAMLBindingCreatedAt:          freezeConfigurationPointerV1(v.SAMLBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		SAMLConfigRevision:            v.SAMLConfigRevision,
		SAMLPolicyRevision:            v.SAMLPolicyRevision,
		SAMLUserCreatedAt:             freezeConfigurationPointerV1(v.SAMLUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		LDAPBindingID:                 v.LDAPBindingID,
		LDAPBindingCreatedAt:          freezeConfigurationPointerV1(v.LDAPBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		LDAPConfigRevision:            v.LDAPConfigRevision,
		LDAPPolicyRevision:            v.LDAPPolicyRevision,
		LDAPUserCreatedAt:             freezeConfigurationPointerV1(v.LDAPUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		OAuthBindingID:                v.OAuthBindingID,
		OAuthBindingCreatedAt:         freezeConfigurationPointerV1(v.OAuthBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		OAuthConfigRevision:           v.OAuthConfigRevision,
		OAuthPolicyRevision:           v.OAuthPolicyRevision,
		OAuthUserCreatedAt:            freezeConfigurationPointerV1(v.OAuthUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		PrimaryMethod:                 v.PrimaryMethod,
		OIDCBindingID:                 v.OIDCBindingID,
		OIDCBindingCreatedAt:          freezeConfigurationPointerV1(v.OIDCBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		OIDCConfigRevision:            v.OIDCConfigRevision,
		OIDCPolicyRevision:            v.OIDCPolicyRevision,
		OIDCUserCreatedAt:             freezeConfigurationPointerV1(v.OIDCUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		ID:                            v.ID,
		UserID:                        v.UserID,
		TokenHash:                     v.TokenHash,
		ExpiresAt:                     v.ExpiresAt.UTC(),
		CreatedAt:                     v.CreatedAt.UTC(),
	}
}

func freezeConfigurationOIDCBindingV1(v entity.OIDCBinding) runtimeConfigurationOIDCBindingV1 {
	return runtimeConfigurationOIDCBindingV1{
		ID:             v.ID,
		UserID:         v.UserID,
		UserCreatedAt:  v.UserCreatedAt.UTC(),
		ConfigRevision: v.ConfigRevision,
		Subject:        v.Subject,
		SubjectDigest:  v.SubjectDigest,
		CreatedAt:      v.CreatedAt.UTC(),
	}
}

func freezeConfigurationOAuthBindingV1(v entity.OAuthBinding) runtimeConfigurationOAuthBindingV1 {
	return runtimeConfigurationOAuthBindingV1{
		ProviderID:     v.ProviderID,
		SubjectKind:    v.SubjectKind,
		ID:             v.ID,
		UserID:         v.UserID,
		UserCreatedAt:  v.UserCreatedAt.UTC(),
		ConfigRevision: v.ConfigRevision,
		Subject:        v.Subject,
		SubjectDigest:  v.SubjectDigest,
		CreatedAt:      v.CreatedAt.UTC(),
	}
}

func freezeConfigurationLDAPBindingV1(v entity.LDAPBinding) runtimeConfigurationLDAPBindingV1 {
	return runtimeConfigurationLDAPBindingV1{
		ProviderID:        v.ProviderID,
		IdentityAttribute: v.IdentityAttribute,
		ID:                v.ID,
		UserID:            v.UserID,
		UserCreatedAt:     v.UserCreatedAt.UTC(),
		ConfigRevision:    v.ConfigRevision,
		Subject:           v.Subject,
		SubjectDigest:     v.SubjectDigest,
		CreatedAt:         v.CreatedAt.UTC(),
	}
}

func freezeConfigurationSAMLBindingV1(v entity.SAMLBinding) runtimeConfigurationSAMLBindingV1 {
	return runtimeConfigurationSAMLBindingV1{
		ID:             v.ID,
		ProviderID:     v.ProviderID,
		UserID:         v.UserID,
		UserCreatedAt:  v.UserCreatedAt.UTC(),
		ConfigRevision: v.ConfigRevision,
		Issuer:         v.Issuer,
		Subject:        v.Subject,
		SubjectDigest:  v.SubjectDigest,
		CreatedAt:      v.CreatedAt.UTC(),
	}
}

func freezeConfigurationNamedIdentityBindingV1(v entity.NamedIdentityBinding) runtimeConfigurationNamedIdentityBindingV1 {
	return runtimeConfigurationNamedIdentityBindingV1{
		ProfileID:      v.ProfileID,
		IdentityIssuer: v.IdentityIssuer,
		ProviderID:     v.ProviderID,
		SubjectKind:    v.SubjectKind,
		ID:             v.ID,
		UserID:         v.UserID,
		UserCreatedAt:  v.UserCreatedAt.UTC(),
		ConfigRevision: v.ConfigRevision,
		Subject:        v.Subject,
		SubjectDigest:  v.SubjectDigest,
		CreatedAt:      v.CreatedAt.UTC(),
	}
}

func freezeConfigurationUserV1(v entity.User) runtimeConfigurationUserV1 {
	return runtimeConfigurationUserV1{
		ApprovalApplicationID: freezeConfigurationPointerV1(v.ApprovalApplicationID, func(s string) string { return s }),
		PersonalGrantRevision: v.PersonalGrantRevision,
		ID:                    v.ID,
		Disabled:              v.Disabled,
		OffboardedAt:          freezeConfigurationPointerV1(v.OffboardedAt, func(t time.Time) time.Time { return t.UTC() }),
		CreatedAt:             v.CreatedAt.UTC(),
	}
}

func freezeConfigurationRegistrationApprovalApplicationV1(v entity.RegistrationApprovalApplication) runtimeConfigurationRegistrationApprovalApplicationV1 {
	return runtimeConfigurationRegistrationApprovalApplicationV1{
		ID:              v.ID,
		UserID:          v.UserID,
		UserCreatedAt:   v.UserCreatedAt.UTC(),
		CreatedAt:       v.CreatedAt.UTC(),
		State:           v.State,
		Revision:        v.Revision,
		DecidedAt:       freezeConfigurationPointerV1(v.DecidedAt, func(t time.Time) time.Time { return t.UTC() }),
		DecisionActorID: freezeConfigurationPointerV1(v.DecisionActorID, func(s string) string { return s }),
		DecisionReason:  freezeConfigurationPointerV1(v.DecisionReason, func(s string) string { return s }),
	}
}

func freezeConfigurationCredentialVaultReferenceV1(v entity.CredentialVaultReference) runtimeConfigurationCredentialVaultReferenceV1 {
	return runtimeConfigurationCredentialVaultReferenceV1{
		PhysicalObject:       v.PhysicalObject,
		ReaderMethod:         v.ReaderMethod,
		SourceContext:        v.SourceContext,
		CredentialID:         v.CredentialID,
		CredentialBirth:      v.CredentialBirth.UTC(),
		ReferenceID:          v.ReferenceID,
		ExpectedMarkerSHA256: v.ExpectedMarkerSHA256,
		DescriptorSHA256:     v.DescriptorSHA256,
		IntegrationID:        v.IntegrationID,
		IntegrationBirth:     v.IntegrationBirth.UTC(),
		RevisionID:           v.RevisionID,
		ReaderGeneration:     v.ReaderGeneration,
	}
}

func freezeConfigurationOIDCProviderV1(v entity.OIDCProvider) runtimeConfigurationOIDCProviderV1 {
	return runtimeConfigurationOIDCProviderV1{
		ID:                       v.ID,
		ConfigRevision:           v.ConfigRevision,
		PolicyRevision:           v.PolicyRevision,
		Issuer:                   v.Issuer,
		Enabled:                  v.Enabled,
		VerifiedConfigRevision:   v.VerifiedConfigRevision,
		VerifiedBy:               v.VerifiedBy,
		VerifiedUserCreatedAt:    freezeConfigurationPointerV1(v.VerifiedUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		VerifiedBindingID:        v.VerifiedBindingID,
		VerifiedBindingCreatedAt: freezeConfigurationPointerV1(v.VerifiedBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
	}
}

func freezeConfigurationOAuthProviderV1(v entity.OAuthProvider) runtimeConfigurationOAuthProviderV1 {
	return runtimeConfigurationOAuthProviderV1{
		ID:                       v.ID,
		ConfigRevision:           v.ConfigRevision,
		PolicyRevision:           v.PolicyRevision,
		AuthorizationURL:         v.AuthorizationURL,
		TokenURL:                 v.TokenURL,
		UserInfoURL:              v.UserInfoURL,
		Enabled:                  v.Enabled,
		VerifiedConfigRevision:   v.VerifiedConfigRevision,
		VerifiedBy:               v.VerifiedBy,
		VerifiedUserCreatedAt:    freezeConfigurationPointerV1(v.VerifiedUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		VerifiedBindingID:        v.VerifiedBindingID,
		VerifiedBindingCreatedAt: freezeConfigurationPointerV1(v.VerifiedBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
	}
}

func freezeConfigurationLDAPProviderV1(v entity.LDAPProvider) runtimeConfigurationLDAPProviderV1 {
	return runtimeConfigurationLDAPProviderV1{
		ID:                       v.ID,
		ConfigRevision:           v.ConfigRevision,
		PolicyRevision:           v.PolicyRevision,
		Endpoint:                 v.Endpoint,
		IdentityAttribute:        v.IdentityAttribute,
		Enabled:                  v.Enabled,
		VerifiedConfigRevision:   v.VerifiedConfigRevision,
		VerifiedBy:               v.VerifiedBy,
		VerifiedUserCreatedAt:    freezeConfigurationPointerV1(v.VerifiedUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		VerifiedBindingID:        v.VerifiedBindingID,
		VerifiedBindingCreatedAt: freezeConfigurationPointerV1(v.VerifiedBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
	}
}

func freezeConfigurationSAMLProviderV1(v entity.SAMLProvider) runtimeConfigurationSAMLProviderV1 {
	return runtimeConfigurationSAMLProviderV1{
		ID:                       v.ID,
		ConfigRevision:           v.ConfigRevision,
		PolicyRevision:           v.PolicyRevision,
		IDPIssuer:                v.IDPIssuer,
		Enabled:                  v.Enabled,
		VerifiedConfigRevision:   v.VerifiedConfigRevision,
		VerifiedBy:               v.VerifiedBy,
		VerifiedUserCreatedAt:    freezeConfigurationPointerV1(v.VerifiedUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		VerifiedBindingID:        v.VerifiedBindingID,
		VerifiedBindingCreatedAt: freezeConfigurationPointerV1(v.VerifiedBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
	}
}

func freezeConfigurationNamedIdentityProviderV1(v entity.NamedIdentityProvider) runtimeConfigurationNamedIdentityProviderV1 {
	return runtimeConfigurationNamedIdentityProviderV1{
		ProfileID:                v.ProfileID,
		IdentityIssuer:           v.IdentityIssuer,
		ID:                       v.ID,
		ConfigRevision:           v.ConfigRevision,
		PolicyRevision:           v.PolicyRevision,
		Enabled:                  v.Enabled,
		VerifiedConfigRevision:   v.VerifiedConfigRevision,
		VerifiedBy:               v.VerifiedBy,
		VerifiedUserCreatedAt:    freezeConfigurationPointerV1(v.VerifiedUserCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
		VerifiedBindingID:        v.VerifiedBindingID,
		VerifiedBindingCreatedAt: freezeConfigurationPointerV1(v.VerifiedBindingCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
	}
}

func freezeConfigurationAuthenticationPolicyV1(v runtimeAuthenticationPolicy) runtimeConfigurationAuthenticationPolicyV1 {
	return runtimeConfigurationAuthenticationPolicyV1{
		Enforced:                v.Enforced,
		Revision:                v.Revision,
		EmergencyAdminID:        v.EmergencyAdminID,
		EmergencyAdminCreatedAt: freezeConfigurationPointerV1(v.EmergencyAdminCreatedAt, func(t time.Time) time.Time { return t.UTC() }),
	}
}

func freezeConfigurationPolicyV1(v limits.Policy) runtimeConfigurationPolicyV1 {
	return runtimeConfigurationPolicyV1{
		Tokens5H:            freezeConfigurationPointerV1(v.Tokens5H, func(i int64) int64 { return i }),
		Tokens7D:            freezeConfigurationPointerV1(v.Tokens7D, func(i int64) int64 { return i }),
		TokensMonthBehavior: v.TokensMonthBehavior,
		MoneyMonthBehavior:  v.MoneyMonthBehavior,
		TokensMonth:         freezeConfigurationPointerV1(v.TokensMonth, func(i int64) int64 { return i }),
		TPM:                 freezeConfigurationPointerV1(v.TPM, func(i int64) int64 { return i }),
		MoneyMonth:          freezeConfigurationPointerV1(v.MoneyMonth, func(s string) string { return s }),
		Currency:            v.Currency,
		RPM:                 freezeConfigurationPointerV1(v.RPM, func(i int64) int64 { return i }),
		Concurrency:         freezeConfigurationPointerV1(v.Concurrency, func(i int64) int64 { return i }),
		IPMode:              v.IPMode,
		IPRanges:            freezeConfigurationSliceV1(v.IPRanges, func(s string) string { return s }),
	}
}

func freezeConfigurationPointerV1[A, B any](v *A, copyValue func(A) B) *B {
	if v == nil {
		return nil
	}
	result := copyValue(*v)
	return &result
}
func freezeConfigurationSliceV1[A, B any](v []A, copyValue func(A) B) []B {
	if v == nil {
		return nil
	}
	result := make([]B, len(v))
	for i := range v {
		result[i] = copyValue(v[i])
	}
	return result
}
func freezeConfigurationMapV1[A, B any](v map[string]A, copyValue func(A) B) map[string]B {
	if v == nil {
		return nil
	}
	result := make(map[string]B, len(v))
	for k, a := range v {
		result[k] = copyValue(a)
	}
	return result
}

// freezeRuntimeConfigurationV1 is a source-only adapter. Its caller must establish
// source coherence; metadata digests are observations, never activation proof.
// It does not query, open a secret, retain a holder, publish, or persist anything.
func freezeRuntimeConfigurationV1(data *runtimeData, sourceDigest, authorizationDigest, networkDigest string) (runtimeConfigurationV1, error) {
	if data == nil {
		return runtimeConfigurationV1{}, errRuntimeConfigurationCodecV1
	}
	result := runtimeConfigurationV1{Format: runtimeConfigurationFormatV1, SourceDigest: sourceDigest, AuthorizationProjectionDigest: authorizationDigest, NetworkPolicyDigest: networkDigest}
	result.Source = runtimeConfigurationSourceV1{
		ApprovalApplications: freezeConfigurationSliceV1(data.ApprovalApplications, freezeConfigurationRegistrationApprovalApplicationV1),
		Egresses:             freezeConfigurationSliceV1(data.Egresses, freezeConfigurationEgressV1),
		Limits:               freezeConfigurationSliceV1(data.Limits, freezeConfigurationResourceLimitV1),
		Users:                freezeConfigurationSliceV1(data.Users, freezeConfigurationUserV1),
		Keys:                 freezeConfigurationSliceV1(data.Keys, freezeConfigurationAPIKeyV1),
		Scopes:               freezeConfigurationSliceV1(data.Scopes, freezeConfigurationAPIKeyModelV1),
		Grants:               freezeConfigurationSliceV1(data.Grants, freezeConfigurationUserModelGrantV1),
		Models:               freezeConfigurationSliceV1(data.Models, freezeConfigurationModelV1),
		Names:                freezeConfigurationSliceV1(data.Names, freezeConfigurationModelNameV1),
		Providers:            freezeConfigurationSliceV1(data.Providers, freezeConfigurationProviderV1),
		Connections:          freezeConfigurationSliceV1(data.Connections, freezeConfigurationProviderConnectionV1),
		Credentials:          freezeConfigurationSliceV1(data.Credentials, freezeConfigurationProviderCredentialV1),
		ProviderModels:       freezeConfigurationSliceV1(data.ProviderModels, freezeConfigurationProviderModelV1),
		Access:               freezeConfigurationSliceV1(data.Access, freezeConfigurationCredentialModelAccessV1),
		Attestations:         freezeConfigurationSliceV1(data.Attestations, freezeConfigurationCredentialDeploymentAttestationV1),
		Bindings:             freezeConfigurationSliceV1(data.Bindings, freezeConfigurationModelProviderBindingV1),
		EgressSetting:        freezeConfigurationEgressSettingV1(data.EgressSetting),
		LimitPolicies:        freezeConfigurationMapV1(data.LimitPolicies, freezeConfigurationPolicyV1),
		LimitRoots:           freezeConfigurationMapV1(data.LimitRoots, func(v string) string { return v }),
	}
	if data.Quota != nil {
		q := data.Quota
		result.Source.Quota = &runtimeConfigurationQuotaV1{Setting: freezeConfigurationQuotaSettingV1(q.Setting), Bounds: freezeConfigurationMapV1(q.Bounds, freezeConfigurationReservationBoundV1), Created: freezeConfigurationMapV1(q.Created, func(v time.Time) time.Time { return v.UTC() }), Revisions: freezeConfigurationMapV1(q.Revisions, func(v string) string { return v }), Currency: q.Currency}
	}
	if data.ProjectData != nil {
		d := data.ProjectData
		result.Source.ProjectData = &runtimeConfigurationProjectDataV1{
			Projects: freezeConfigurationSliceV1(d.Projects, freezeConfigurationProjectV1),
			Managers: freezeConfigurationSliceV1(d.Managers, freezeConfigurationProjectManagerV1),
			Keys:     freezeConfigurationSliceV1(d.Keys, freezeConfigurationProjectKeyV1),
			Scopes:   freezeConfigurationSliceV1(d.Scopes, freezeConfigurationProjectKeyModelV1),
			Grants:   freezeConfigurationSliceV1(d.Grants, freezeConfigurationProjectModelGrantV1),
		}
	}
	if data.TeamSessionData != nil {
		d := data.TeamSessionData
		result.Source.TeamSessionData = &runtimeConfigurationTeamSessionDataV1{
			AuthenticationPolicy: freezeConfigurationAuthenticationPolicyV1(runtimePolicy(d.AuthenticationPolicy)),
			EmergencyUsers: freezeConfigurationMapV1(d.EmergencyUsers, func(u entity.User) runtimeConfigurationEmergencyUserV1 {
				return runtimeConfigurationEmergencyUserV1{ID: u.ID, CreatedAt: u.CreatedAt.UTC(), Enabled: !u.Disabled && u.OffboardedAt == nil, IntrinsicAdministrator: u.Role == "admin", PasswordDigest: configurationCipherProofV1(u.PasswordHash)}
			}),
			EmergencyMFA: freezeConfigurationMapV1(d.EmergencyMFA, func(m entity.UserMFA) runtimeConfigurationEmergencyMFAV1 {
				return runtimeConfigurationEmergencyMFAV1{UserID: m.UserID, Enabled: m.Enabled, Generation: m.Generation}
			}),
			Sessions:              freezeConfigurationSliceV1(d.Sessions, freezeConfigurationSessionV1),
			Teams:                 freezeConfigurationSliceV1(d.Teams, freezeConfigurationTeamV1),
			Memberships:           freezeConfigurationSliceV1(d.Memberships, freezeConfigurationTeamMembershipV1),
			Grants:                freezeConfigurationSliceV1(d.Grants, freezeConfigurationTeamModelGrantV1),
			OIDCBindings:          freezeConfigurationSliceV1(d.OIDCBindings, freezeConfigurationOIDCBindingV1),
			OAuthBindings:         freezeConfigurationSliceV1(d.OAuthBindings, freezeConfigurationOAuthBindingV1),
			LDAPBindings:          freezeConfigurationSliceV1(d.LDAPBindings, freezeConfigurationLDAPBindingV1),
			SAMLBindings:          freezeConfigurationSliceV1(d.SAMLBindings, freezeConfigurationSAMLBindingV1),
			NamedIdentityBindings: freezeConfigurationSliceV1(d.NamedIdentityBindings, freezeConfigurationNamedIdentityBindingV1),
			OIDCProvider:          freezeConfigurationPointerV1(d.OIDCProvider, freezeConfigurationOIDCProviderV1),
			OAuthProvider:         freezeConfigurationPointerV1(d.OAuthProvider, freezeConfigurationOAuthProviderV1),
			LDAPProvider:          freezeConfigurationPointerV1(d.LDAPProvider, freezeConfigurationLDAPProviderV1),
			SAMLProvider:          freezeConfigurationPointerV1(d.SAMLProvider, freezeConfigurationSAMLProviderV1),
			NamedIdentityProviders: freezeConfigurationMapV1(d.NamedIdentityProviders, func(p *entity.NamedIdentityProvider) *runtimeConfigurationNamedIdentityProviderV1 {
				return freezeConfigurationPointerV1(p, freezeConfigurationNamedIdentityProviderV1)
			}),
		}
	}
	if data.Pricing != nil {
		d := data.Pricing
		result.PricingObservation = &runtimeConfigurationPricingV1{Setting: freezeConfigurationPricingSettingV1(d.Setting), FX: freezeConfigurationSliceV1(d.FX, freezeConfigurationPricingExchangeRateV1), Prices: freezeConfigurationSliceV1(d.Prices, freezeConfigurationModelPriceV1), Rates: freezeConfigurationSliceV1(d.Rates, freezeConfigurationPriceRateV1)}
	}
	canonicalizeRuntimeConfigurationV1(&result)
	return result, nil
}
