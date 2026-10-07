package service

import (
	"bytes"
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"time"
)

type ProviderCredentialOrphanView struct {
	CreationRequestID string               `json:"creation_request_id"`
	Kind              string               `json:"kind"`
	ProviderID        string               `json:"provider_id"`
	ConnectionID      string               `json:"connection_id"`
	CredentialID      string               `json:"credential_id"`
	IntegrationID     string               `json:"integration_id"`
	RevisionID        string               `json:"revision_id"`
	CreatedAt         time.Time            `json:"created_at"`
	State             string               `json:"state"`
	Write             VaultObservationView `json:"write"`
	Read              VaultObservationView `json:"read"`
	OwnershipRecorded bool                 `json:"ownership_recorded"`
	Eligible          bool                 `json:"eligible"`
	BlockerCodes      []string             `json:"blocker_codes"`
	CanCleanup        bool                 `json:"can_cleanup"`
	ReviewETag        string               `json:"review_etag"`
}
type ProviderCredentialOrphanPage struct {
	Items      []ProviderCredentialOrphanView `json:"items"`
	NextCursor *string                        `json:"next_cursor"`
}
type ProviderCredentialCleanupReceipt struct {
	CreationRequestID string               `json:"creation_request_id"`
	RequestID         string               `json:"request_id"`
	IntegrationID     string               `json:"integration_id"`
	RevisionID        string               `json:"revision_id"`
	State             string               `json:"state"`
	Ownership         VaultObservationView `json:"ownership"`
	Cleanup           VaultCleanupView     `json:"cleanup"`
	StartedAt         time.Time            `json:"started_at"`
	FinishedAt        *time.Time           `json:"finished_at"`
}
type ProviderCredentialCleanupResult struct {
	Receipt ProviderCredentialCleanupReceipt `json:"receipt"`
	Running bool                             `json:"running"`
}
type ProviderCredentialCleanupInput struct {
	RequestID    string `json:"request_id"`
	Reason       string `json:"reason"`
	cleanupToken string
}

func (ProviderCredentialCleanupInput) String() string     { return "Provider cleanup input (redacted)" }
func (v ProviderCredentialCleanupInput) GoString() string { return v.String() }
func (v *ProviderCredentialCleanupInput) UnmarshalJSON(raw []byte) error {
	fields, err := modelCreationObject(raw, "request_id", "reason", "cleanup_token")
	if err != nil {
		return err
	}
	var n ProviderCredentialCleanupInput
	for k, d := range fields {
		if bytes.Equal(bytes.TrimSpace(d), []byte("null")) {
			return apperrors.ErrBadRequest
		}
		var target *string
		switch k {
		case "request_id":
			target = &n.RequestID
		case "reason":
			target = &n.Reason
		case "cleanup_token":
			target = &n.cleanupToken
		}
		if json.Unmarshal(d, target) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if !credentialReplacementRequestID.MatchString(n.RequestID) || !rootReason(n.Reason) || (n.cleanupToken != "" && !vaultToken(n.cleanupToken)) {
		return apperrors.ErrBadRequest
	}
	if _, ok := fields["request_id"]; !ok {
		return apperrors.ErrBadRequest
	}
	if _, ok := fields["reason"]; !ok {
		return apperrors.ErrBadRequest
	}
	*v = n
	return nil
}
func cleanupReceipt(row entity.ProviderCredentialCleanup, now time.Time) (ProviderCredentialCleanupReceipt, error) {
	v := ProviderCredentialCleanupReceipt{CreationRequestID: row.CreationRequestID, RequestID: row.RequestID, IntegrationID: row.IntegrationID, RevisionID: row.RevisionID, State: row.State, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt}
	if !credentialReplacementRequestID.MatchString(row.CreationRequestID) || !credentialReplacementRequestID.MatchString(row.RequestID) || !vaultIntegrationID.MatchString(row.IntegrationID) || !vaultRevisionID.MatchString(row.RevisionID) || !connectionMetadataBirth(row.StartedAt) || !row.Deadline.After(row.StartedAt) {
		return v, vaultUnavailable
	}
	if !vaultDecodeObservation(row.OwnershipJSON, &v.Ownership) || !vaultDecodeCleanup(row.CleanupJSON, &v.Cleanup) {
		return v, vaultUnavailable
	}
	if (row.State == "pending") != (row.FinishedAt == nil) {
		return v, vaultUnavailable
	}
	switch row.State {
	case "pending":
		if !now.Before(row.Deadline) {
			v.State = "unknown"
		}
	case "unknown", "failed", "acknowledged":
	default:
		return v, vaultUnavailable
	}
	if row.State == "acknowledged" && (!v.Ownership.Succeeded || v.Cleanup.State != "acknowledged" || !v.Cleanup.Observation.Succeeded || row.FinishedAt == nil) {
		return v, vaultUnavailable
	}
	if row.FinishedAt != nil && (row.FinishedAt.Before(row.StartedAt) || !connectionMetadataBirth(*row.FinishedAt)) {
		return v, vaultUnavailable
	}
	return v, nil
}
