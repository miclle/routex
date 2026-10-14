package entity

import "time"

// APIKeyDeliveryControllerGrant is an independently enrolled machine identity.
// Its verifier and immutable scope never borrow a Session, inference Key or
// Vault credential. Configuration assignments carry separate reviewed facts.
type APIKeyDeliveryControllerGrant struct {
	ID                         string     `gorm:"column:id;primaryKey;size:30" json:"-"`
	EnrollmentRequestID        string     `gorm:"column:enrollment_request_id;size:36;not null;uniqueIndex:idx_delivery_controller_enrollment,priority:2;check:ck_delivery_controller_enrollment,OCTET_LENGTH(enrollment_request_id) = 36 AND OCTET_LENGTH(enrollment_intent_digest) = 64 AND OCTET_LENGTH(enrollment_review) = 64" json:"-"`
	EnrollmentIntentDigest     string     `gorm:"column:enrollment_intent_digest;size:64;not null" json:"-"`
	EnrollmentReview           string     `gorm:"column:enrollment_review;size:64;not null" json:"-"`
	Name                       string     `gorm:"column:name;size:100;not null" json:"-"`
	CredentialDigest           string     `gorm:"column:credential_digest;size:64;not null;uniqueIndex:idx_delivery_controller_verifier;check:ck_delivery_controller_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(credential_digest) = 64 AND OCTET_LENGTH(review_revision) = 64 AND OCTET_LENGTH(owner_digest) = 64 AND OCTET_LENGTH(owner_id) = 30 AND OCTET_LENGTH(slot_id) = 30 AND OCTET_LENGTH(profile_id) = 30 AND OCTET_LENGTH(principal_id) = 30 AND OCTET_LENGTH(created_by) = 30 AND OCTET_LENGTH(configuration_review_actor_id) = 30 AND OCTET_LENGTH(creation_reason) BETWEEN 1 AND 1000 AND OCTET_LENGTH(name) BETWEEN 1 AND 100" json:"-"`
	OwnerKind                  string     `gorm:"column:owner_kind;size:20;not null;check:ck_delivery_controller_owner,(OCTET_LENGTH(owner_kind) = 8 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 101 AND ASCII(SUBSTRING(owner_kind,3,1)) = 114 AND ASCII(SUBSTRING(owner_kind,4,1)) = 115 AND ASCII(SUBSTRING(owner_kind,5,1)) = 111 AND ASCII(SUBSTRING(owner_kind,6,1)) = 110 AND ASCII(SUBSTRING(owner_kind,7,1)) = 97 AND ASCII(SUBSTRING(owner_kind,8,1)) = 108) OR (OCTET_LENGTH(owner_kind) = 7 AND ASCII(SUBSTRING(owner_kind,1,1)) = 112 AND ASCII(SUBSTRING(owner_kind,2,1)) = 114 AND ASCII(SUBSTRING(owner_kind,3,1)) = 111 AND ASCII(SUBSTRING(owner_kind,4,1)) = 106 AND ASCII(SUBSTRING(owner_kind,5,1)) = 101 AND ASCII(SUBSTRING(owner_kind,6,1)) = 99 AND ASCII(SUBSTRING(owner_kind,7,1)) = 116)" json:"-"`
	OwnerID                    string     `gorm:"column:owner_id;size:30;not null" json:"-"`
	OwnerBirth                 time.Time  `gorm:"column:owner_birth;precision:6;not null" json:"-"`
	OwnerDigest                string     `gorm:"column:owner_digest;size:64;not null" json:"-"`
	SlotID                     string     `gorm:"column:slot_id;size:30;not null;uniqueIndex:idx_delivery_controller_enrollment,priority:1;index:idx_delivery_controller_slot,priority:1" json:"-"`
	SlotBirth                  time.Time  `gorm:"column:slot_birth;precision:6;not null;index:idx_delivery_controller_slot,priority:2" json:"-"`
	ProfileID                  string     `gorm:"column:profile_id;size:30;not null" json:"-"`
	ProfileBirth               time.Time  `gorm:"column:profile_birth;precision:6;not null" json:"-"`
	PrincipalID                string     `gorm:"column:principal_id;size:30;not null" json:"-"`
	PrincipalBirth             time.Time  `gorm:"column:principal_birth;precision:6;not null" json:"-"`
	ExpiresAt                  time.Time  `gorm:"column:expires_at;precision:6;not null;check:ck_delivery_controller_lifetime,created_at > '0001-01-01 00:00:00' AND owner_birth > '0001-01-01 00:00:00' AND slot_birth > '0001-01-01 00:00:00' AND profile_birth > '0001-01-01 00:00:00' AND principal_birth > '0001-01-01 00:00:00' AND created_by_birth > '0001-01-01 00:00:00' AND expires_at > created_at AND ((revoked_at IS NULL AND OCTET_LENGTH(revocation_reason) = 0 AND OCTET_LENGTH(revocation_review) = 0 AND OCTET_LENGTH(revocation_actor_id) = 0 AND revocation_actor_birth IS NULL) OR (revoked_at >= created_at AND OCTET_LENGTH(revocation_review) = 64 AND OCTET_LENGTH(revocation_actor_id) = 30 AND revocation_actor_birth IS NOT NULL AND OCTET_LENGTH(revocation_reason) BETWEEN 1 AND 1000))" json:"-"`
	RevokedAt                  *time.Time `gorm:"column:revoked_at;precision:6" json:"-"`
	RevocationReason           string     `gorm:"column:revocation_reason;size:1000;not null" json:"-"`
	AssignmentRequestID        string     `gorm:"column:assignment_request_id;size:36;not null;check:ck_delivery_controller_assignment_intent,OCTET_LENGTH(assignment_request_id) = 36 AND OCTET_LENGTH(assignment_intent_digest) = 64 AND OCTET_LENGTH(assignment_review) = 64" json:"-"`
	AssignmentIntentDigest     string     `gorm:"column:assignment_intent_digest;size:64;not null" json:"-"`
	AssignmentReview           string     `gorm:"column:assignment_review;size:64;not null" json:"-"`
	RevocationActorID          string     `gorm:"column:revocation_actor_id;size:30;not null" json:"-"`
	RevocationActorBirth       *time.Time `gorm:"column:revocation_actor_birth;precision:6" json:"-"`
	RevocationReview           string     `gorm:"column:revocation_review;size:64;not null" json:"-"`
	ReviewRevision             string     `gorm:"column:review_revision;size:64;not null" json:"-"`
	ReviewedDescriptorRevision uint64     `gorm:"column:reviewed_descriptor_revision;not null;check:ck_delivery_controller_assignment,reviewed_descriptor_revision BETWEEN 1 AND 9223372036854775807 AND OCTET_LENGTH(configuration_digest) = 64 AND OCTET_LENGTH(reviewed_binding_id) = 30 AND OCTET_LENGTH(reviewed_key_id) = 30 AND OCTET_LENGTH(configuration_review_reason) BETWEEN 1 AND 1000 AND reviewed_binding_birth > '0001-01-01 00:00:00' AND reviewed_key_birth > '0001-01-01 00:00:00' AND configuration_review_birth > '0001-01-01 00:00:00' AND configuration_reviewed_at >= created_at" json:"-"`
	ReviewedBindingID          string     `gorm:"column:reviewed_binding_id;size:30;not null" json:"-"`
	ReviewedBindingBirth       time.Time  `gorm:"column:reviewed_binding_birth;precision:6;not null" json:"-"`
	ReviewedKeyID              string     `gorm:"column:reviewed_key_id;size:30;not null" json:"-"`
	ReviewedKeyBirth           time.Time  `gorm:"column:reviewed_key_birth;precision:6;not null" json:"-"`
	ConfigurationDigest        string     `gorm:"column:configuration_digest;size:64;not null" json:"-"`
	ConfigurationReviewedAt    time.Time  `gorm:"column:configuration_reviewed_at;precision:6;not null" json:"-"`
	ConfigurationReviewActorID string     `gorm:"column:configuration_review_actor_id;size:30;not null" json:"-"`
	ConfigurationReviewBirth   time.Time  `gorm:"column:configuration_review_birth;precision:6;not null" json:"-"`
	ConfigurationReviewReason  string     `gorm:"column:configuration_review_reason;size:1000;not null" json:"-"`
	CreatedBy                  string     `gorm:"column:created_by;size:30;not null" json:"-"`
	CreatedByBirth             time.Time  `gorm:"column:created_by_birth;precision:6;not null" json:"-"`
	CreationReason             string     `gorm:"column:creation_reason;size:1000;not null" json:"-"`
	CreatedAt                  time.Time  `gorm:"column:created_at;precision:6;not null;autoCreateTime:false" json:"-"`
	UpdatedAt                  time.Time  `gorm:"column:updated_at;precision:6;not null;autoUpdateTime:false" json:"-"`
}

func (APIKeyDeliveryControllerGrant) TableName() string { return "api_key_delivery_controller_grants" }

// APIKeyDeliveryControllerAcknowledgement records a controller's exact reviewed
// configuration report. It is not server proof of Agent output, application use,
// native completion, retirement or fleet convergence.
type APIKeyDeliveryControllerAcknowledgement struct {
	ID                  string    `gorm:"column:id;primaryKey;size:30" json:"-"`
	GrantID             string    `gorm:"column:grant_id;size:30;not null;uniqueIndex:idx_delivery_controller_ack_intent,priority:1" json:"-"`
	GrantBirth          time.Time `gorm:"column:grant_birth;precision:6;not null" json:"-"`
	RequestID           string    `gorm:"column:request_id;size:36;not null;uniqueIndex:idx_delivery_controller_ack_intent,priority:2;check:ck_delivery_controller_ack_identity,OCTET_LENGTH(id) = 30 AND OCTET_LENGTH(grant_id) = 30 AND OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(intent_digest) = 64 AND OCTET_LENGTH(configuration_digest) = 64 AND descriptor_revision BETWEEN 1 AND 9223372036854775807 AND OCTET_LENGTH(binding_id) = 30 AND OCTET_LENGTH(key_id) = 30 AND grant_birth > '0001-01-01 00:00:00' AND binding_birth > '0001-01-01 00:00:00' AND key_birth > '0001-01-01 00:00:00' AND recorded_at >= grant_birth" json:"-"`
	IntentDigest        string    `gorm:"column:intent_digest;size:64;not null" json:"-"`
	DescriptorRevision  uint64    `gorm:"column:descriptor_revision;not null" json:"-"`
	ConfigurationDigest string    `gorm:"column:configuration_digest;size:64;not null" json:"-"`
	BindingID           string    `gorm:"column:binding_id;size:30;not null" json:"-"`
	BindingBirth        time.Time `gorm:"column:binding_birth;precision:6;not null" json:"-"`
	KeyID               string    `gorm:"column:key_id;size:30;not null" json:"-"`
	KeyBirth            time.Time `gorm:"column:key_birth;precision:6;not null" json:"-"`
	AgentState          string    `gorm:"column:agent_state;size:16;not null;check:ck_delivery_controller_ack_agent,(OCTET_LENGTH(agent_state) = 7 AND ASCII(SUBSTRING(agent_state,1,1)) = 117 AND ASCII(SUBSTRING(agent_state,2,1)) = 110 AND ASCII(SUBSTRING(agent_state,3,1)) = 107 AND ASCII(SUBSTRING(agent_state,4,1)) = 110 AND ASCII(SUBSTRING(agent_state,5,1)) = 111 AND ASCII(SUBSTRING(agent_state,6,1)) = 119 AND ASCII(SUBSTRING(agent_state,7,1)) = 110) OR (OCTET_LENGTH(agent_state) = 7 AND ASCII(SUBSTRING(agent_state,1,1)) = 114 AND ASCII(SUBSTRING(agent_state,2,1)) = 117 AND ASCII(SUBSTRING(agent_state,3,1)) = 110 AND ASCII(SUBSTRING(agent_state,4,1)) = 110 AND ASCII(SUBSTRING(agent_state,5,1)) = 105 AND ASCII(SUBSTRING(agent_state,6,1)) = 110 AND ASCII(SUBSTRING(agent_state,7,1)) = 103) OR (OCTET_LENGTH(agent_state) = 6 AND ASCII(SUBSTRING(agent_state,1,1)) = 102 AND ASCII(SUBSTRING(agent_state,2,1)) = 97 AND ASCII(SUBSTRING(agent_state,3,1)) = 105 AND ASCII(SUBSTRING(agent_state,4,1)) = 108 AND ASCII(SUBSTRING(agent_state,5,1)) = 101 AND ASCII(SUBSTRING(agent_state,6,1)) = 100)" json:"-"`
	OutputState         string    `gorm:"column:output_state;size:16;not null;check:ck_delivery_controller_ack_output,(OCTET_LENGTH(output_state) = 7 AND ASCII(SUBSTRING(output_state,1,1)) = 117 AND ASCII(SUBSTRING(output_state,2,1)) = 110 AND ASCII(SUBSTRING(output_state,3,1)) = 107 AND ASCII(SUBSTRING(output_state,4,1)) = 110 AND ASCII(SUBSTRING(output_state,5,1)) = 111 AND ASCII(SUBSTRING(output_state,6,1)) = 119 AND ASCII(SUBSTRING(output_state,7,1)) = 110) OR (OCTET_LENGTH(output_state) = 7 AND ASCII(SUBSTRING(output_state,1,1)) = 119 AND ASCII(SUBSTRING(output_state,2,1)) = 114 AND ASCII(SUBSTRING(output_state,3,1)) = 105 AND ASCII(SUBSTRING(output_state,4,1)) = 116 AND ASCII(SUBSTRING(output_state,5,1)) = 116 AND ASCII(SUBSTRING(output_state,6,1)) = 101 AND ASCII(SUBSTRING(output_state,7,1)) = 110) OR (OCTET_LENGTH(output_state) = 6 AND ASCII(SUBSTRING(output_state,1,1)) = 102 AND ASCII(SUBSTRING(output_state,2,1)) = 97 AND ASCII(SUBSTRING(output_state,3,1)) = 105 AND ASCII(SUBSTRING(output_state,4,1)) = 108 AND ASCII(SUBSTRING(output_state,5,1)) = 101 AND ASCII(SUBSTRING(output_state,6,1)) = 100)" json:"-"`
	RecordedAt          time.Time `gorm:"column:recorded_at;precision:6;not null" json:"-"`
}

func (APIKeyDeliveryControllerAcknowledgement) TableName() string {
	return "api_key_delivery_controller_acknowledgements"
}
