package service

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
)

const personalModelReviewPermission = "members.models.write"

var errPersonalModelConflict = &apperrors.Error{Code: http.StatusConflict, Message: "personal model request or reviewed access changed"}
var errPersonalModelOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "personal model access exceeds supported bounds"}

type PersonalModelRequestInput struct {
	RequestID string `json:"request_id"`
	ModelID   string `json:"model_id"`
	Reason    string `json:"reason"`
}
type PersonalModelDecisionInput struct {
	DecisionID string `json:"decision_id"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
}
type PersonalModelDecision struct {
	DecisionID string    `json:"decision_id"`
	ActorID    string    `json:"actor_id"`
	ActorName  string    `json:"actor_name"`
	Action     string    `json:"action"`
	Reason     string    `json:"reason"`
	DecidedAt  time.Time `json:"decided_at"`
}
type PersonalModelRequestRecord struct {
	ID              string                 `json:"id"`
	RequestID       string                 `json:"request_id"`
	ApplicantUserID string                 `json:"applicant_user_id"`
	ApplicantName   string                 `json:"applicant_name"`
	ModelID         string                 `json:"model_id"`
	ModelName       string                 `json:"model_name"`
	Reason          string                 `json:"reason"`
	Status          string                 `json:"status"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	ResolvedAt      *time.Time             `json:"resolved_at"`
	CancelledReason *string                `json:"cancelled_reason"`
	Decision        *PersonalModelDecision `json:"decision"`
}
type PersonalModelCurrentModel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
type PersonalModelRequestDetail struct {
	PersonalModelRequestRecord
	CurrentModel      *PersonalModelCurrentModel `json:"current_model"`
	CurrentGranted    bool                       `json:"current_granted"`
	ReviewETag        string                     `json:"review_etag"`
	AllowedActions    []string                   `json:"allowed_actions"`
	RuntimeApplied    bool                       `json:"runtime_applied"`
	ApplicationStatus string                     `json:"application_status"`
}
type PersonalModelDecisionRecord struct {
	DecisionID        string                     `json:"decision_id"`
	Committed         bool                       `json:"committed"`
	SavedRequest      PersonalModelRequestRecord `json:"saved_request"`
	CurrentGranted    bool                       `json:"current_granted"`
	RuntimeApplied    bool                       `json:"runtime_applied"`
	ApplicationStatus string                     `json:"application_status"`
}
type PersonalModelRequestFilter struct {
	Status, Cursor string
	Limit          int
}
type PersonalModelRequestPage struct {
	Items      []PersonalModelRequestRecord `json:"items"`
	Total      int64                        `json:"total"`
	NextCursor *string                      `json:"next_cursor"`
}
type MemberModelAccessWorkspace struct {
	UserID            string                      `json:"user_id"`
	Models            []PersonalModelCurrentModel `json:"models"`
	ModelCount        int                         `json:"model_count"`
	CanReviewRequests bool                        `json:"can_review_requests"`
}

func personalModelID(value, prefix string) bool {
	if len(value) != 30 || !strings.HasPrefix(value, prefix+"_") || value != strings.ToLower(value) {
		return false
	}
	_, err := ulid.ParseStrict(strings.ToUpper(value[4:]))
	return err == nil
}
func personalModelETag(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, b := range value {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}
func personalModelReason(value string, required bool) bool {
	return (!required || value != "") && len(value) <= 1024 && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}
func personalExact(tx *gorm.DB, column, value string) *gorm.DB {
	return tx.Where(database.ExactText(tx, clause.Column{Name: column}, value))
}
func personalHash(value any) string {
	encoded, _ := json.Marshal(value)
	return secret.SHA256Hex(string(encoded))
}
func personalCreationHash(actor, etag string, input PersonalModelRequestInput) string {
	return personalHash(struct{ Actor, ETag, RequestID, ModelID, Reason string }{actor, etag, input.RequestID, input.ModelID, input.Reason})
}
func personalDecisionHash(actor, request, etag string, input PersonalModelDecisionInput) string {
	return personalHash(struct{ Actor, Request, ETag, DecisionID, Action, Reason string }{actor, request, etag, input.DecisionID, input.Action, input.Reason})
}
func personalModelRecord(row entity.PersonalModelRequest) PersonalModelRequestRecord {
	result := PersonalModelRequestRecord{ID: row.ID, RequestID: row.RequestID, ApplicantUserID: row.ApplicantUserID, ApplicantName: row.ApplicantName, ModelID: row.ModelID, ModelName: row.ModelName, Reason: row.Reason, Status: row.Status, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), ResolvedAt: row.ResolvedAt}
	if row.CancelledReason != "" {
		value := row.CancelledReason
		result.CancelledReason = &value
	}
	if row.DecisionID != nil && row.DecisionActorID != nil && row.DecisionActorName != nil && row.DecidedAt != nil {
		result.Decision = &PersonalModelDecision{DecisionID: *row.DecisionID, ActorID: *row.DecisionActorID, ActorName: *row.DecisionActorName, Action: row.DecisionAction, Reason: row.DecisionReason, DecidedAt: row.DecidedAt.UTC()}
	}
	return result
}

type personalModelCursor struct {
	Scope     string
	CreatedAt time.Time
	ID        string
}

func encodePersonalModelCursor(value personalModelCursor) string {
	b, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodePersonalModelCursor(raw, scope string) (*personalModelCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 512 {
		return nil, apperrors.ErrBadRequest
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	var value personalModelCursor
	if json.Unmarshal(b, &value) != nil || value.Scope != scope || value.CreatedAt.IsZero() || !personalModelID(value.ID, "mar") {
		return nil, apperrors.ErrBadRequest
	}
	return &value, nil
}
