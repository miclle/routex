package service

import (
	"net/http"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const runtimeArchiveRollbackBound = 1000

var runtimeArchiveRollbackConflict = &apperrors.Error{Code: http.StatusConflict, Message: "archive routing rollback review or intent conflicts"}
var runtimeArchiveRollbackUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "archive routing rollback temporarily unavailable"}

// Only this closed allowlist is eligible for restoration. Identity, availability,
// verification, capacity, credentials, authorization and prices are never copied.
type RuntimeArchiveConnectionFields struct {
	BaseURL    string  `json:"base_url"`
	Protocol   string  `json:"protocol"`
	Adapter    string  `json:"adapter"`
	APIVersion *string `json:"api_version"`
	EgressMode string  `json:"egress_mode"`
	EgressID   *string `json:"egress_id"`
}
type RuntimeArchiveConnectionChange struct {
	ConnectionID         string                         `json:"connection_id"`
	Name                 string                         `json:"name"`
	Enabled              bool                           `json:"enabled"`
	Before               RuntimeArchiveConnectionFields `json:"before"`
	After                RuntimeArchiveConnectionFields `json:"after"`
	RequiresVerification bool                           `json:"requires_verification"`
}
type RuntimeArchiveDeclarationFields struct {
	Image bool `json:"image_input"`
	PDF   bool `json:"pdf_input"`
}
type RuntimeArchiveDeclarationChange struct {
	ProviderModelID string                          `json:"provider_model_id"`
	Before          RuntimeArchiveDeclarationFields `json:"before"`
	After           RuntimeArchiveDeclarationFields `json:"after"`
}
type RuntimeArchiveWeightChange struct {
	BindingID       string `json:"binding_id"`
	ModelID         string `json:"model_id"`
	ProviderModelID string `json:"provider_model_id"`
	Protocol        string `json:"protocol"`
	Before          int    `json:"before"`
	After           int    `json:"after"`
}
type RuntimeArchiveRollbackPreview struct {
	Scope            string                            `json:"scope"`
	ArchiveID        string                            `json:"archive_id"`
	ArchiveCreatedAt time.Time                         `json:"archive_created_at"`
	ObservedAt       time.Time                         `json:"observed_at"`
	CanApply         bool                              `json:"can_apply"`
	BlockerCodes     []string                          `json:"blocker_codes"`
	Connections      []RuntimeArchiveConnectionChange  `json:"connections"`
	Declarations     []RuntimeArchiveDeclarationChange `json:"declarations"`
	Weights          []RuntimeArchiveWeightChange      `json:"weights"`
	ETag             string                            `json:"-"`
}
type RuntimeArchiveRollbackInput struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}
type RuntimeArchiveRollbackReceipt struct {
	Scope            string                            `json:"scope"`
	RequestID        string                            `json:"request_id"`
	ArchiveID        string                            `json:"archive_id"`
	ArchiveCreatedAt time.Time                         `json:"archive_created_at"`
	AppliedAt        time.Time                         `json:"applied_at"`
	Changed          bool                              `json:"changed"`
	Reason           string                            `json:"reason"`
	Publication      *RuntimePublicationCommandReceipt `json:"publication"`
	// This is a fresh same-node current-config proof, separate from the original
	// publication function outcome. Neither is a fleet acknowledgement.
	RuntimeApplied bool `json:"runtime_applied"`
	Superseded     bool `json:"superseded"`
}
