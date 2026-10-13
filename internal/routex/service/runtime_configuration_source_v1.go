package service

import "time"

// These private structs freeze codec v1. Do not replace them with entity aliases,
// embed current entities, or add fields in place; a changed schema needs a new codec.
type runtimeConfigurationAPIKeyV1 struct {
	DeliveryMode      string
	LifecycleRevision string
	ID                string
	UserID            string
	Name              string
	Prefix            string
	TokenHash         string
	Status            string
	ExpiresAt         *time.Time
	DeliveryExpiresAt *time.Time
	ReplacesKeyID     *string
	ActivateOnConfirm bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type runtimeConfigurationAPIKeyModelV1 struct {
	KeyID   string
	ModelID string
}

type runtimeConfigurationProjectV1 struct {
	ID          string
	Name        string
	Description string
	Status      string
	CreatorID   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type runtimeConfigurationProjectKeyV1 struct {
	ID                string
	ProjectID         string
	CreatorID         string
	Name              string
	Prefix            string
	TokenHash         string
	Status            string
	DeliveryMode      string
	ExpiresAt         *time.Time
	DeliveryExpiresAt *time.Time
	ReplacesKeyID     *string
	ActivateOnConfirm bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type runtimeConfigurationProjectManagerV1 struct {
	ID        string
	ProjectID string
	UserID    string
}

type runtimeConfigurationProjectModelGrantV1 struct {
	ProjectID string
	ModelID   string
}

type runtimeConfigurationProjectKeyModelV1 struct {
	KeyID   string
	ModelID string
}

type runtimeConfigurationTeamV1 struct {
	ID          string
	Name        string
	Description string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type runtimeConfigurationTeamMembershipV1 struct {
	JoinedAt *time.Time
	ID       string
	TeamID   string
	UserID   string
	Role     string
	Status   string
}

type runtimeConfigurationTeamModelGrantV1 struct {
	SourceCreationReceiptID *string
	SourceRequestID         *string
	TeamID                  string
	ModelID                 string
}

type runtimeConfigurationCredentialDeploymentAttestationV1 struct {
	CredentialID    string
	ProviderModelID string
	Identity        string
}

type runtimeConfigurationProviderV1 struct {
	Enabled   bool
	ETag      string
	ID        string
	Name      string
	CreatedAt time.Time
}

type runtimeConfigurationProviderConnectionV1 struct {
	TransportGeneration string
	Adapter             string
	APIVersion          *string
	Enabled             bool
	EgressMode          string
	EgressID            *string
	ETag                string
	ID                  string
	ProviderID          string
	Name                string
	BaseURL             string
	Protocol            string
	CreatedAt           time.Time
}

type runtimeConfigurationProviderCredentialV1 struct {
	VerifiedTransportGeneration  string
	VerificationTransportCurrent bool
	CoverageRevision             int64
	CoverageReviewETag           string
	CoverageIntentSHA256         string
	StorageSource                string
	VaultReference               *runtimeConfigurationCredentialVaultReferenceV1
	ReplacesCredentialID         *string
	ID                           string
	ConnectionID                 string
	Name                         string
	Priority                     int
	Enabled                      bool
	VerificationStatus           string
	VerifiedAt                   *time.Time
	CreatedAt                    time.Time
	SourceProof                  string
}

type runtimeConfigurationProviderModelV1 struct {
	CapabilityTransportGeneration string
	CapabilitiesTransportCurrent  bool
	CapabilityReviewETag          string
	ID                            string
	ConnectionID                  string
	UpstreamName                  string
	Disabled                      bool
	SupportsImageInput            bool
	SupportsPDFInput              bool
	ETag                          string
	CreatedAt                     time.Time
}

type runtimeConfigurationModelV1 struct {
	ID        string
	Status    string
	CreatedAt time.Time
}

type runtimeConfigurationModelNameV1 struct {
	Name           string
	ModelID        string
	CurrentModelID *string
	ExpiresAt      *time.Time
	CreatedAt      time.Time
}

type runtimeConfigurationModelProviderBindingV1 struct {
	ID              string
	ModelID         string
	ProviderModelID string
	Weight          int
	CreatedAt       time.Time
}

type runtimeConfigurationUserModelGrantV1 struct {
	SourceRequestID *string
	UserID          string
	ModelID         string
	CreatedAt       time.Time
}

type runtimeConfigurationCredentialModelAccessV1 struct {
	CredentialID    string
	ProviderModelID string
}

type runtimeConfigurationEgressV1 struct {
	ID               string
	Name             string
	Kind             string
	Host             string
	Port             int
	Enabled          bool
	ETag             string
	SecretGeneration string
	CreatedAt        time.Time
	AuthProof        string
}

type runtimeConfigurationEgressSettingV1 struct {
	ID              int
	DefaultEgressID *string
	ETag            string
	UpdatedAt       time.Time
}

type runtimeConfigurationPricingSettingV1 struct {
	ID               int
	ETag             string
	PlatformCurrency string
}

type runtimeConfigurationPricingExchangeRateV1 struct {
	Currency string
	Rate     string
}

type runtimeConfigurationModelPriceV1 struct {
	ID                     string
	ProviderModelID        string
	ContextThreshold       int64
	UpdateSource           string
	FollowRepository       bool
	RepositoryThresholdKey *string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type runtimeConfigurationPriceRateV1 struct {
	ID                 string
	ModelPriceID       string
	Metric             string
	Tier               string
	Unit               string
	Currency           string
	Amount             string
	Enabled            bool
	RepositoryModelKey *string
	RepositoryRateKey  *string
}

type runtimeConfigurationResourceLimitV1 struct {
	ScopeKind           string
	ScopeID             string
	ETag                string
	PreviousETag        string
	AppliedDefaultETag  *string
	DefaultResetETag    *string
	ActorID             string
	Reason              string
	Tokens5H            *int64
	Tokens7D            *int64
	TokensMonthBehavior string
	MoneyMonthBehavior  string
	TokensMonth         *int64
	TPM                 *int64
	MoneyMonth          *string
	Currency            string
	RPM                 *int64
	Concurrency         *int64
	IPMode              string
	IPRangesJSON        string
	UpdatedAt           time.Time
}

type runtimeConfigurationQuotaSettingV1 struct {
	AccountingStarted bool
	ID                int
	TimeZone          string
	ETag              string
	PreviousETag      string
	ActorID           string
	Reason            string
	UpdatedAt         time.Time
}

type runtimeConfigurationReservationBoundV1 struct {
	TransportGeneration string
	ProviderModelID     string
	Protocol            string
	MaxInputTokens      int64
	MaxOutputTokens     int64
	Evidence            string
	ETag                string
	PreviousETag        string
	ActorID             string
	Reason              string
	UpdatedAt           time.Time
}

type runtimeConfigurationSessionV1 struct {
	EmergencyPolicyRevision       string
	EmergencyUserCreatedAt        *time.Time
	EmergencyMFAGeneration        string
	EmergencyPasswordDigest       string
	EmergencyFactorCompletedAt    *time.Time
	NamedIdentityProviderID       string
	NamedIdentityProfileID        string
	NamedIdentityBindingID        string
	NamedIdentityBindingCreatedAt *time.Time
	NamedIdentityConfigRevision   string
	NamedIdentityPolicyRevision   string
	NamedIdentityUserCreatedAt    *time.Time
	SAMLBindingID                 string
	SAMLBindingCreatedAt          *time.Time
	SAMLConfigRevision            string
	SAMLPolicyRevision            string
	SAMLUserCreatedAt             *time.Time
	LDAPBindingID                 string
	LDAPBindingCreatedAt          *time.Time
	LDAPConfigRevision            string
	LDAPPolicyRevision            string
	LDAPUserCreatedAt             *time.Time
	OAuthBindingID                string
	OAuthBindingCreatedAt         *time.Time
	OAuthConfigRevision           string
	OAuthPolicyRevision           string
	OAuthUserCreatedAt            *time.Time
	PrimaryMethod                 string
	OIDCBindingID                 string
	OIDCBindingCreatedAt          *time.Time
	OIDCConfigRevision            string
	OIDCPolicyRevision            string
	OIDCUserCreatedAt             *time.Time
	ID                            string
	UserID                        string
	TokenHash                     string
	ExpiresAt                     time.Time
	CreatedAt                     time.Time
}

type runtimeConfigurationOIDCBindingV1 struct {
	ID             string
	UserID         string
	UserCreatedAt  time.Time
	ConfigRevision string
	Subject        string
	SubjectDigest  string
	CreatedAt      time.Time
}

type runtimeConfigurationOAuthBindingV1 struct {
	ProviderID     string
	SubjectKind    string
	ID             string
	UserID         string
	UserCreatedAt  time.Time
	ConfigRevision string
	Subject        string
	SubjectDigest  string
	CreatedAt      time.Time
}

type runtimeConfigurationLDAPBindingV1 struct {
	ProviderID        string
	IdentityAttribute string
	ID                string
	UserID            string
	UserCreatedAt     time.Time
	ConfigRevision    string
	Subject           string
	SubjectDigest     string
	CreatedAt         time.Time
}

type runtimeConfigurationSAMLBindingV1 struct {
	ID             string
	ProviderID     string
	UserID         string
	UserCreatedAt  time.Time
	ConfigRevision string
	Issuer         string
	Subject        string
	SubjectDigest  string
	CreatedAt      time.Time
}

type runtimeConfigurationNamedIdentityBindingV1 struct {
	ProfileID      string
	IdentityIssuer string
	ProviderID     string
	SubjectKind    string
	ID             string
	UserID         string
	UserCreatedAt  time.Time
	ConfigRevision string
	Subject        string
	SubjectDigest  string
	CreatedAt      time.Time
}

type runtimeConfigurationUserV1 struct {
	ApprovalApplicationID *string
	PersonalGrantRevision string
	ID                    string
	Disabled              bool
	OffboardedAt          *time.Time
	CreatedAt             time.Time
}

type runtimeConfigurationRegistrationApprovalApplicationV1 struct {
	ID              string
	UserID          string
	UserCreatedAt   time.Time
	CreatedAt       time.Time
	State           string
	Revision        string
	DecidedAt       *time.Time
	DecisionActorID *string
	DecisionReason  *string
}

type runtimeConfigurationCredentialVaultReferenceV1 struct {
	PhysicalObject       string
	ReaderMethod         string
	SourceContext        string
	CredentialID         string
	CredentialBirth      time.Time
	ReferenceID          string
	ExpectedMarkerSHA256 string
	DescriptorSHA256     string
	IntegrationID        string
	IntegrationBirth     time.Time
	RevisionID           string
	ReaderGeneration     string
}

type runtimeConfigurationOIDCProviderV1 struct {
	ID                       string
	ConfigRevision           string
	PolicyRevision           string
	Issuer                   string
	Enabled                  bool
	VerifiedConfigRevision   string
	VerifiedBy               string
	VerifiedUserCreatedAt    *time.Time
	VerifiedBindingID        string
	VerifiedBindingCreatedAt *time.Time
}

type runtimeConfigurationOAuthProviderV1 struct {
	ID                       string
	ConfigRevision           string
	PolicyRevision           string
	AuthorizationURL         string
	TokenURL                 string
	UserInfoURL              string
	Enabled                  bool
	VerifiedConfigRevision   string
	VerifiedBy               string
	VerifiedUserCreatedAt    *time.Time
	VerifiedBindingID        string
	VerifiedBindingCreatedAt *time.Time
}

type runtimeConfigurationLDAPProviderV1 struct {
	ID                       string
	ConfigRevision           string
	PolicyRevision           string
	Endpoint                 string
	IdentityAttribute        string
	Enabled                  bool
	VerifiedConfigRevision   string
	VerifiedBy               string
	VerifiedUserCreatedAt    *time.Time
	VerifiedBindingID        string
	VerifiedBindingCreatedAt *time.Time
}

type runtimeConfigurationSAMLProviderV1 struct {
	ID                       string
	ConfigRevision           string
	PolicyRevision           string
	IDPIssuer                string
	Enabled                  bool
	VerifiedConfigRevision   string
	VerifiedBy               string
	VerifiedUserCreatedAt    *time.Time
	VerifiedBindingID        string
	VerifiedBindingCreatedAt *time.Time
}

type runtimeConfigurationNamedIdentityProviderV1 struct {
	ProfileID                string
	IdentityIssuer           string
	ID                       string
	ConfigRevision           string
	PolicyRevision           string
	Enabled                  bool
	VerifiedConfigRevision   string
	VerifiedBy               string
	VerifiedUserCreatedAt    *time.Time
	VerifiedBindingID        string
	VerifiedBindingCreatedAt *time.Time
}

type runtimeConfigurationAuthenticationPolicyV1 struct {
	Enforced                bool
	Revision                string
	EmergencyAdminID        string
	EmergencyAdminCreatedAt *time.Time
}

type runtimeConfigurationPolicyV1 struct {
	Tokens5H            *int64
	Tokens7D            *int64
	TokensMonthBehavior string
	MoneyMonthBehavior  string
	TokensMonth         *int64
	TPM                 *int64
	MoneyMonth          *string
	Currency            string
	RPM                 *int64
	Concurrency         *int64
	IPMode              string
	IPRanges            []string
}

type runtimeConfigurationV1 struct {
	Format                        string
	SourceDigest                  string
	AuthorizationProjectionDigest string
	NetworkPolicyDigest           string
	Source                        runtimeConfigurationSourceV1
	PricingObservation            *runtimeConfigurationPricingV1
	PricingObservationDigest      string
}

type runtimeConfigurationSourceV1 struct {
	ApprovalApplications []runtimeConfigurationRegistrationApprovalApplicationV1
	Quota                *runtimeConfigurationQuotaV1
	Egresses             []runtimeConfigurationEgressV1
	EgressSetting        runtimeConfigurationEgressSettingV1
	Limits               []runtimeConfigurationResourceLimitV1
	LimitPolicies        map[string]runtimeConfigurationPolicyV1
	LimitRoots           map[string]string
	ProjectData          *runtimeConfigurationProjectDataV1
	TeamSessionData      *runtimeConfigurationTeamSessionDataV1
	Users                []runtimeConfigurationUserV1
	Keys                 []runtimeConfigurationAPIKeyV1
	Scopes               []runtimeConfigurationAPIKeyModelV1
	Grants               []runtimeConfigurationUserModelGrantV1
	Models               []runtimeConfigurationModelV1
	Names                []runtimeConfigurationModelNameV1
	Providers            []runtimeConfigurationProviderV1
	Connections          []runtimeConfigurationProviderConnectionV1
	Credentials          []runtimeConfigurationProviderCredentialV1
	ProviderModels       []runtimeConfigurationProviderModelV1
	Access               []runtimeConfigurationCredentialModelAccessV1
	Attestations         []runtimeConfigurationCredentialDeploymentAttestationV1
	Bindings             []runtimeConfigurationModelProviderBindingV1
}

type runtimeConfigurationQuotaV1 struct {
	Setting   runtimeConfigurationQuotaSettingV1
	Bounds    map[string]runtimeConfigurationReservationBoundV1
	Created   map[string]time.Time
	Revisions map[string]string
	Currency  string
}

type runtimeConfigurationProjectDataV1 struct {
	Projects []runtimeConfigurationProjectV1
	Managers []runtimeConfigurationProjectManagerV1
	Keys     []runtimeConfigurationProjectKeyV1
	Scopes   []runtimeConfigurationProjectKeyModelV1
	Grants   []runtimeConfigurationProjectModelGrantV1
}

type runtimeConfigurationEmergencyUserV1 struct {
	ID                     string
	CreatedAt              time.Time
	Enabled                bool
	IntrinsicAdministrator bool
	PasswordDigest         string
}

type runtimeConfigurationEmergencyMFAV1 struct {
	UserID     string
	Enabled    bool
	Generation string
}

type runtimeConfigurationTeamSessionDataV1 struct {
	AuthenticationPolicy   runtimeConfigurationAuthenticationPolicyV1
	EmergencyUsers         map[string]runtimeConfigurationEmergencyUserV1
	EmergencyMFA           map[string]runtimeConfigurationEmergencyMFAV1
	Sessions               []runtimeConfigurationSessionV1
	Teams                  []runtimeConfigurationTeamV1
	Memberships            []runtimeConfigurationTeamMembershipV1
	Grants                 []runtimeConfigurationTeamModelGrantV1
	OIDCProvider           *runtimeConfigurationOIDCProviderV1
	OIDCBindings           []runtimeConfigurationOIDCBindingV1
	OAuthProvider          *runtimeConfigurationOAuthProviderV1
	OAuthBindings          []runtimeConfigurationOAuthBindingV1
	LDAPProvider           *runtimeConfigurationLDAPProviderV1
	LDAPBindings           []runtimeConfigurationLDAPBindingV1
	SAMLProvider           *runtimeConfigurationSAMLProviderV1
	SAMLBindings           []runtimeConfigurationSAMLBindingV1
	NamedIdentityProviders map[string]*runtimeConfigurationNamedIdentityProviderV1
	NamedIdentityBindings  []runtimeConfigurationNamedIdentityBindingV1
}

type runtimeConfigurationPricingV1 struct {
	Setting runtimeConfigurationPricingSettingV1
	FX      []runtimeConfigurationPricingExchangeRateV1
	Prices  []runtimeConfigurationModelPriceV1
	Rates   []runtimeConfigurationPriceRateV1
}

// A reconstructed record is private historical source, never an executable
// authorization snapshot. Dependencies require separate current-authority review.
type runtimeConfigurationFactsV1 struct {
	Document          runtimeConfigurationV1
	UnresolvedSecrets []runtimeConfigurationSecretDependencyV1
}

type runtimeConfigurationSecretDependencyV1 struct {
	Kind        string
	ID          string
	Birth       time.Time
	SourceProof string
}
