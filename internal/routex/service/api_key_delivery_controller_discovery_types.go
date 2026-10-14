package service

import "time"

// Discovery summaries contain retained facts only. They never grant a reviewed
// write token or infer current configuration, output, application or fleet state.
type APIKeyDeliverySlotSummary struct {
	ID                  string    `json:"id"`
	Birth               time.Time `json:"birth"`
	ProfileID           string    `json:"profile_id"`
	OwnerKind           string    `json:"owner_kind"`
	OwnerID             string    `json:"owner_id"`
	DescriptorRevision  string    `json:"descriptor_revision"`
	ConfigurationStatus string    `json:"configuration_status"`
}

type APIKeyDeliverySlotList struct {
	Items      []APIKeyDeliverySlotSummary `json:"items"`
	NextCursor string                      `json:"next_cursor"`
}

type APIKeyDeliveryControllerSummary struct {
	ID                  string     `json:"id"`
	Birth               time.Time  `json:"birth"`
	Name                string     `json:"name"`
	SlotID              string     `json:"slot_id"`
	ExpiresAt           time.Time  `json:"expires_at"`
	RevokedAt           *time.Time `json:"revoked_at"`
	LifecycleStatus     string     `json:"lifecycle_status"`
	ConfigurationStatus string     `json:"configuration_status"`
}

type APIKeyDeliveryControllerList struct {
	Items      []APIKeyDeliveryControllerSummary `json:"items"`
	NextCursor string                            `json:"next_cursor"`
}

// The original actor may locate an enrollment without recovering its bearer.
// Only an exact retained intent digest/review match can reconcile that intent;
// the summary is not a reviewed current configuration or mutation receipt.
type APIKeyDeliveryControllerEnrollmentReceipt struct {
	RequestID        string                          `json:"request_id"`
	IntentDigest     string                          `json:"intent_digest"`
	EnrollmentReview string                          `json:"enrollment_review"`
	Controller       APIKeyDeliveryControllerSummary `json:"controller"`
	Bearer           *string                         `json:"bearer"`
	SecretStatus     string                          `json:"secret_status"`
}
