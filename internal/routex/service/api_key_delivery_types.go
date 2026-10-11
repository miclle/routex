package service

import (
	"context"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/vault"
	"time"
)

type APIKeyDeliveryPolicyInput struct {
	AllowedModes          []string `json:"allowed_modes"`
	DefaultMode           string   `json:"default_mode"`
	IntegrationID         string   `json:"integration_id"`
	WriterIdentityID      string   `json:"writer_identity_id"`
	ProvisionerIdentityID string   `json:"provisioner_identity_id"`
	Reason                string   `json:"reason"`
}
type APIKeyDeliveryIdentityInput struct {
	Purpose       string `json:"purpose"`
	IntegrationID string `json:"integration_id"`
	Token         string `json:"token"`
	Reason        string `json:"reason"`
}

func (APIKeyDeliveryIdentityInput) String() string {
	return "API-Key delivery identity input (redacted)"
}
func (v APIKeyDeliveryIdentityInput) GoString() string           { return v.String() }
func (APIKeyDeliveryIdentityInput) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

type APIKeyCallerTemplateInput struct {
	AuthMount          string   `json:"auth_mount"`
	Issuer             string   `json:"issuer"`
	JWKSURL            string   `json:"jwks_url"`
	Audience           string   `json:"audience"`
	AllowedSubjects    []string `json:"allowed_subjects"`
	TokenTTLSeconds    uint64   `json:"token_ttl_seconds"`
	TokenMaxTTLSeconds uint64   `json:"token_max_ttl_seconds"`
	Reason             string   `json:"reason"`
}
type APIKeyDeliveryContextView struct {
	AllowedModes     []string `json:"allowed_modes"`
	DefaultMode      string   `json:"default_mode"`
	ManagedAvailable bool     `json:"managed_available"`
	FailureCode      *string  `json:"failure_code"`
	ReviewETag       string   `json:"review_etag"`
}
type APIKeyDeliveryPolicyView struct {
	APIKeyDeliveryContextView
	IntegrationID         string `json:"integration_id"`
	WriterIdentityID      string `json:"writer_identity_id"`
	ProvisionerIdentityID string `json:"provisioner_identity_id"`
	TemplateConfigured    bool   `json:"template_configured"`
}
type APIKeyDeliveryIdentityView struct {
	ID         string `json:"id"`
	Purpose    string `json:"purpose"`
	Configured bool   `json:"configured"`
	ReviewETag string `json:"review_etag"`
}
type APIKeyCallerTemplateView struct {
	AuthMount          string   `json:"auth_mount"`
	Issuer             string   `json:"issuer"`
	JWKSURL            string   `json:"jwks_url"`
	Audience           string   `json:"audience"`
	AllowedSubjects    []string `json:"allowed_subjects"`
	TokenTTLSeconds    uint64   `json:"token_ttl_seconds"`
	TokenMaxTTLSeconds uint64   `json:"token_max_ttl_seconds"`
	Verified           bool     `json:"verified"`
	ReviewETag         string   `json:"review_etag"`
}
type APIKeyDeliverySelection struct {
	ConsumerID   string                       `json:"consumer_id"`
	Environment  string                       `json:"environment"`
	Principal    APIKeyDeliveryPrincipalInput `json:"principal"`
	TargetReview string                       `json:"target_review"`
}
type APIKeyDeliveryPrincipalInput struct {
	Method  string `json:"method"`
	Subject string `json:"subject"`
}
type APIKeyDeliveryResolveInput struct {
	RequestID       string                       `json:"request_id"`
	ConsumerID      string                       `json:"consumer_id"`
	ApplicationName string                       `json:"application_name"`
	Environment     string                       `json:"environment"`
	Principal       APIKeyDeliveryPrincipalInput `json:"principal"`
	Reason          string                       `json:"reason"`
}
type APIKeyDeliveryApprovalInput struct {
	Approved        bool   `json:"approved"`
	ControlVerified bool   `json:"control_verified"`
	Reason          string `json:"reason"`
}
type APIKeyDeliveryIsolationInput struct {
	Isolated bool   `json:"isolated"`
	Reason   string `json:"reason"`
}
type APIKeyDeliveryReconcileInput struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}
type APIKeyDeliveryIsolationView struct {
	Status     string     `json:"status"`
	Revision   *string    `json:"revision"`
	RecordedAt *time.Time `json:"recorded_at"`
}
type APIKeyDeliveryTargetView struct {
	ID              string                      `json:"id"`
	ConsumerID      string                      `json:"consumer_id"`
	ApplicationName string                      `json:"application_name"`
	Environment     string                      `json:"environment"`
	Status          string                      `json:"status"`
	ControlVerified bool                        `json:"control_verified"`
	Isolation       APIKeyDeliveryIsolationView `json:"isolation"`
	FailureCode     *string                     `json:"failure_code"`
	ReviewETag      string                      `json:"review_etag"`
}
type ManagedAPIKeyInput struct {
	Name      string                  `json:"name"`
	ModelIDs  []string                `json:"model_ids"`
	ExpiresAt *time.Time              `json:"expires_at"`
	RequestID string                  `json:"request_id"`
	Reason    string                  `json:"reason"`
	Delivery  APIKeyDeliverySelection `json:"delivery"`
}
type APIKeyDeliveryDescriptorView struct {
	SchemaVersion      int    `json:"schema_version"`
	KeyID              string `json:"key_id"`
	ProfileID          string `json:"profile_id"`
	PrincipalID        string `json:"principal_id"`
	SlotID             string `json:"slot_id"`
	DescriptorRevision uint64 `json:"descriptor_revision"`
	Endpoint           string `json:"endpoint"`
	Namespace          string `json:"namespace"`
	Mount              string `json:"mount"`
	Path               string `json:"path"`
	DataField          string `json:"data_field"`
	Version            uint64 `json:"version"`
	Method             string `json:"method"`
	AuthMount          string `json:"auth_mount"`
	Role               string `json:"role"`
	Coordinator        string `json:"coordinator"`
	ApplicationStatus  string `json:"application_status"`
}
type APIKeyDeliveryReceiptView struct {
	RequestID      string                        `json:"request_id"`
	KeyID          string                        `json:"key_id"`
	Mode           string                        `json:"mode"`
	DeliveryStatus string                        `json:"delivery_status"`
	KeyStatus      string                        `json:"key_status"`
	RuntimeStatus  string                        `json:"runtime_status"`
	FailureCode    *string                       `json:"failure_code"`
	Descriptor     *APIKeyDeliveryDescriptorView `json:"descriptor"`
	ReviewETag     string                        `json:"review_etag"`
}
type ManagedAPIKeyResult struct {
	Personal   *KeyRecord                `json:"-"`
	Project    *ProjectKeyRecord         `json:"-"`
	Delivery   APIKeyDeliveryReceiptView `json:"delivery"`
	HTTPStatus int                       `json:"-"`
}

// Entity records are internal handler-projection inputs, not JSON values.
func (ManagedAPIKeyResult) String() string     { return "Managed API-Key result (redacted)" }
func (v ManagedAPIKeyResult) GoString() string { return v.String() }

type APIKeyDeliveryCapability struct {
	Path    string
	Actions []string
}
type APIKeyDeliveryValidationPlan struct {
	Purpose, AuthID, Generation, IntegrationID, IntegrationRevision, VaultRevisionID string
	AuthBirth, IntegrationBirth, VaultRevisionBirth                                  time.Time
	Endpoint, Namespace                                                              string
	KVMount, KeyPrefix, AuthMount, ProfileID, PrincipalID, ResourceID                string
	Capabilities                                                                     []APIKeyDeliveryCapability
	SHA256                                                                           string
}
type APIKeyDeliveryValidationFacts struct {
	PlanSHA256                string
	OwnIdentityObserved       bool
	TTLSeconds                uint64
	MachineIdentityCommitment string
	TokenType                 string
	ExpiresAt                 time.Time
	Capabilities              []APIKeyDeliveryCapability
	CloseState                vault.ResponseCloseState
}
type APIKeyDeliveryValidator interface {
	Validate(context.Context, APIKeyDeliveryValidationPlan, string) (APIKeyDeliveryValidationFacts, error)
}

func WithAPIKeyDeliveryValidator(v APIKeyDeliveryValidator) Option {
	return func(s *Service) { s.apiKeyDeliveryValidator = v }
}

type apiKeyDeliveryOwner struct {
	Kind, ID, Digest string
	Birth            time.Time
}
type apiKeyDeliverySnapshot struct {
	Policy              entity.APIKeyDeliveryPolicy
	Template            entity.APIKeyCallerTemplate
	Integration         entity.VaultIntegration
	Revision            entity.VaultRevision
	Writer, Provisioner entity.APIKeyDeliveryAuth
}
