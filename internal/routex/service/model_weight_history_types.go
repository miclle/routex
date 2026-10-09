package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const modelWeightSnapshotBudget = 512 * 1024
const modelWeightBindingBudget = 1000

var modelWeightUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "routing weight history unavailable"}
var modelWeightConflict = &apperrors.Error{Code: http.StatusConflict, Message: "routing weight review changed or rollback unavailable"}
var modelWeightCanonical = regexp.MustCompile(`^[a-z]{3}_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

func modelWeightID(value, prefix string) bool {
	return len(value) == 30 && len(prefix) == 3 && value[:4] == prefix+"_" && modelWeightCanonical.MatchString(value)
}

type ModelWeightRow struct {
	BindingID              string     `json:"binding_id"`
	BindingCreatedAt       *time.Time `json:"binding_created_at"`
	ProviderModelID        string     `json:"provider_model_id"`
	ProviderModelCreatedAt *time.Time `json:"provider_model_created_at"`
	ConnectionID           string     `json:"connection_id"`
	ConnectionCreatedAt    *time.Time `json:"connection_created_at"`
	ProviderID             string     `json:"provider_id"`
	ProviderCreatedAt      *time.Time `json:"provider_created_at"`
	Protocol               string     `json:"protocol"`
	Weight                 int        `json:"weight"`
}
type ModelWeightVersionSummary struct {
	VersionID         string     `json:"version_id"`
	ModelID           string     `json:"model_id"`
	ModelCreatedAt    *time.Time `json:"model_created_at"`
	CapturedAt        time.Time  `json:"captured_at"`
	Source            string     `json:"source"`
	ParentVersionID   *string    `json:"parent_version_id"`
	RollbackVersionID *string    `json:"rollback_version_id"`
	ActorID           string     `json:"actor_id"`
	Reason            *string    `json:"reason"`
	BindingCount      int        `json:"binding_count"`
	ValidWeightSet    bool       `json:"valid_weight_set"`
}
type ModelWeightVersionDetail struct {
	Version ModelWeightVersionSummary `json:"version"`
	Weights []ModelWeightRow          `json:"weights"`
}
type ModelWeightVersionPage struct {
	ModelID    string                      `json:"model_id"`
	Items      []ModelWeightVersionSummary `json:"items"`
	NextCursor *string                     `json:"next_cursor"`
}
type ModelWeightHistoryFilter struct {
	Limit  int
	Cursor string
}
type ModelWeightRollbackReview struct {
	ModelID          string           `json:"model_id"`
	VersionID        string           `json:"version_id"`
	CurrentVersionID *string          `json:"current_version_id"`
	CurrentWeights   []ModelWeightRow `json:"current_weights"`
	ProposedWeights  []ModelWeightRow `json:"proposed_weights"`
	Eligible         bool             `json:"eligible"`
	CanRollback      bool             `json:"can_rollback"`
	BlockerCodes     []string         `json:"blocker_codes"`
	ReviewETag       string           `json:"review_etag"`
	ObservedAt       time.Time        `json:"observed_at"`
}
type ModelWeightRollbackInput struct {
	VersionID string `json:"version_id"`
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}

func (input *ModelWeightRollbackInput) UnmarshalJSON(raw []byte) error {
	fields, err := modelCreationObject(raw, "version_id", "request_id", "reason")
	if err != nil || len(fields) != 3 {
		return apperrors.ErrBadRequest
	}
	var next ModelWeightRollbackInput
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
		switch name {
		case "version_id":
			err = json.Unmarshal(value, &next.VersionID)
		case "request_id":
			err = json.Unmarshal(value, &next.RequestID)
		case "reason":
			err = json.Unmarshal(value, &next.Reason)
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
	}
	if !modelWeightID(next.VersionID, "mwv") || !credentialReplacementRequestID.MatchString(next.RequestID) || !validRoleDefinitionReason(next.Reason) {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}

type ModelWeightRollbackReceipt struct {
	RequestID       string    `json:"request_id"`
	ModelID         string    `json:"model_id"`
	VersionID       string    `json:"version_id"`
	SourceVersionID *string   `json:"source_version_id"`
	SavedVersionID  *string   `json:"saved_version_id"`
	Effect          string    `json:"effect"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
}
type ModelWeightRollbackResult struct {
	Receipt           ModelWeightRollbackReceipt `json:"receipt"`
	ApplicationStatus string                     `json:"application_status"`
	RuntimeApplied    bool                       `json:"runtime_applied"`
}
