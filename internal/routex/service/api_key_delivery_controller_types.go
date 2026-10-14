package service

import "time"

// APIKeyDeliveryControllerConfiguration contains only reviewed non-secret facts.
// The configuration reads a separately supplied workload JWT and writes one
// exact-slot API Key file; it contains neither credential nor executable hooks.
type APIKeyDeliveryControllerConfiguration struct {
	SchemaVersion       int    `json:"schema_version"`
	SlotID              string `json:"slot_id"`
	DescriptorRevision  string `json:"descriptor_revision"`
	KeyID               string `json:"key_id"`
	ConfigurationDigest string `json:"configuration_digest"`
	AgentConfiguration  string `json:"agent_configuration"`
	JWTFile             string `json:"jwt_file"`
	KeyFile             string `json:"key_file"`
}

type APIKeyDeliveryControllerReviewView struct {
	Configuration APIKeyDeliveryControllerConfiguration `json:"configuration"`
	ReviewETag    string                                `json:"review_etag"`
}

type APIKeyDeliveryControllerEnrollmentInput struct {
	RequestID           string    `json:"request_id"`
	Name                string    `json:"name"`
	ExpiresAt           time.Time `json:"expires_at"`
	OptIn               bool      `json:"opt_in"`
	DescriptorRevision  string    `json:"descriptor_revision"`
	ConfigurationDigest string    `json:"configuration_digest"`
	Reason              string    `json:"reason"`
}

type APIKeyDeliveryControllerAssignmentInput struct {
	RequestID           string `json:"request_id"`
	OptIn               bool   `json:"opt_in"`
	DescriptorRevision  string `json:"descriptor_revision"`
	ConfigurationDigest string `json:"configuration_digest"`
	Reason              string `json:"reason"`
}

type APIKeyDeliveryControllerAcknowledgementInput struct {
	RequestID           string `json:"request_id"`
	DescriptorRevision  string `json:"descriptor_revision"`
	ConfigurationDigest string `json:"configuration_digest"`
	AgentState          string `json:"agent_state"`
	OutputState         string `json:"output_state"`
}

type APIKeyDeliveryControllerAcknowledgementView struct {
	RequestID           string    `json:"request_id"`
	DescriptorRevision  string    `json:"descriptor_revision"`
	ConfigurationDigest string    `json:"configuration_digest"`
	AgentState          string    `json:"agent_state"`
	OutputState         string    `json:"output_state"`
	RecordedAt          time.Time `json:"recorded_at"`
	EvidenceKind        string    `json:"evidence_kind"`
}

type APIKeyDeliveryControllerView struct {
	ID                          string                                       `json:"id"`
	Name                        string                                       `json:"name"`
	SlotID                      string                                       `json:"slot_id"`
	OwnerKind                   string                                       `json:"owner_kind"`
	OwnerID                     string                                       `json:"owner_id"`
	ExpiresAt                   time.Time                                    `json:"expires_at"`
	RevokedAt                   *time.Time                                   `json:"revoked_at"`
	AssignedDescriptorRevision  string                                       `json:"assigned_descriptor_revision"`
	AssignedConfigurationDigest string                                       `json:"assigned_configuration_digest"`
	CurrentConfiguration        *APIKeyDeliveryControllerConfiguration       `json:"current_configuration"`
	ConfigurationStatus         string                                       `json:"configuration_status"`
	LastAcknowledgement         *APIKeyDeliveryControllerAcknowledgementView `json:"last_acknowledgement"`
	ReviewETag                  string                                       `json:"review_etag"`
}

// EnrollmentResult never serializes its one-time bearer through generic logging
// or JSON. Only the closed handler response may explicitly display it once.
type APIKeyDeliveryControllerEnrollmentResult struct {
	Controller APIKeyDeliveryControllerView `json:"-"`
	Bearer     string                       `json:"-"`
	Created    bool                         `json:"-"`
}

func (APIKeyDeliveryControllerEnrollmentResult) String() string {
	return "API-Key delivery controller enrollment (redacted)"
}
func (v APIKeyDeliveryControllerEnrollmentResult) GoString() string { return v.String() }

type apiKeyDeliveryControllerConfigurationFacts struct {
	View                                                            APIKeyDeliveryControllerConfiguration
	Owner                                                           apiKeyDeliveryOwner
	SlotBirth, ProfileBirth, PrincipalBirth, BindingBirth, KeyBirth time.Time
	ProfileID, PrincipalID, BindingID, SecurityDigest               string
}
