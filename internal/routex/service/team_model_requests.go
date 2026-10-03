package service

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

var errTeamModelRequestConflict = &apperrors.Error{Code: http.StatusConflict, Message: "Team model request or reviewed access changed"}
var errTeamModelRequestOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "Team model request workspace exceeds supported bounds"}

type TeamModelRequestInput struct {
	RequestID string `json:"request_id"`
	TeamID    string `json:"team_id"`
	ModelID   string `json:"model_id"`
	Reason    string `json:"reason"`
}
type TeamModelDecisionInput struct {
	DecisionID string `json:"decision_id"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
}

func (input *TeamModelRequestInput) UnmarshalJSON(data []byte) error {
	fields, err := personalModelStringObject(data, []string{"request_id", "team_id", "model_id", "reason"}, nil)
	if err == nil {
		*input = TeamModelRequestInput{RequestID: fields["request_id"], TeamID: fields["team_id"], ModelID: fields["model_id"], Reason: fields["reason"]}
	}
	return err
}
func (input *TeamModelDecisionInput) UnmarshalJSON(data []byte) error {
	fields, err := personalModelStringObject(data, []string{"decision_id", "action"}, []string{"reason"})
	if err == nil {
		*input = TeamModelDecisionInput{DecisionID: fields["decision_id"], Action: fields["action"], Reason: fields["reason"]}
	}
	return err
}

type TeamModelRequestRecord struct {
	ID                    string                 `json:"id"`
	RequestID             string                 `json:"request_id"`
	TeamID                string                 `json:"team_id"`
	TeamName              string                 `json:"team_name"`
	ApplicantUserID       string                 `json:"applicant_user_id"`
	ApplicantName         string                 `json:"applicant_name"`
	ApplicantMembershipID string                 `json:"applicant_membership_id"`
	ModelID               string                 `json:"model_id"`
	ModelName             string                 `json:"model_name"`
	Reason                string                 `json:"reason"`
	Status                string                 `json:"status"`
	CreatedAt             time.Time              `json:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
	ResolvedAt            *time.Time             `json:"resolved_at"`
	CancelledReason       *string                `json:"cancelled_reason"`
	Decision              *PersonalModelDecision `json:"decision"`
}
type TeamModelCurrentTeam struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
type TeamModelRequestDetail struct {
	TeamModelRequestRecord
	CurrentTeam              *TeamModelCurrentTeam      `json:"current_team"`
	CurrentModel             *PersonalModelCurrentModel `json:"current_model"`
	CurrentMembershipMatches *bool                      `json:"current_membership_matches"`
	CurrentGranted           *bool                      `json:"current_granted"`
	ReviewETag               string                     `json:"review_etag"`
	AllowedActions           []string                   `json:"allowed_actions"`
	RuntimeApplied           *bool                      `json:"runtime_applied"`
	ApplicationStatus        string                     `json:"application_status"`
}
type TeamModelDecisionRecord struct {
	DecisionID               string                 `json:"decision_id"`
	Committed                bool                   `json:"committed"`
	SavedRequest             TeamModelRequestRecord `json:"saved_request"`
	CurrentGranted           *bool                  `json:"current_granted"`
	CurrentMembershipMatches *bool                  `json:"current_membership_matches"`
	RuntimeApplied           *bool                  `json:"runtime_applied"`
	ApplicationStatus        string                 `json:"application_status"`
}
type TeamModelRequestFilter struct {
	Status, Cursor, TeamID, ModelID string
	Limit                           int
}
type TeamModelRequestPage struct {
	Items      []TeamModelRequestRecord `json:"items"`
	Total      int64                    `json:"total"`
	NextCursor *string                  `json:"next_cursor"`
}
type TeamModelRequestWorkspace struct {
	TeamID            string                      `json:"team_id"`
	Name              string                      `json:"name"`
	Status            string                      `json:"status"`
	Models            []PersonalModelCurrentModel `json:"models"`
	ModelCount        int                         `json:"model_count"`
	CanReviewRequests bool                        `json:"can_review_requests"`
}

func teamModelRequestRecord(row entity.TeamModelRequest) TeamModelRequestRecord {
	result := TeamModelRequestRecord{ID: row.ID, RequestID: row.RequestID, TeamID: row.TeamID, TeamName: row.TeamName, ApplicantUserID: row.ApplicantUserID, ApplicantName: row.ApplicantName, ApplicantMembershipID: row.ApplicantMembershipID, ModelID: row.ModelID, ModelName: row.ModelName, Reason: row.Reason, Status: row.Status, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), ResolvedAt: row.ResolvedAt}
	if row.CancelledReason != "" {
		reason := row.CancelledReason
		result.CancelledReason = &reason
	}
	if row.DecisionID != nil && row.DecisionActorID != nil && row.DecisionActorName != nil && row.DecidedAt != nil {
		result.Decision = &PersonalModelDecision{DecisionID: *row.DecisionID, ActorID: *row.DecisionActorID, ActorName: *row.DecisionActorName, Action: row.DecisionAction, Reason: row.DecisionReason, DecidedAt: row.DecidedAt.UTC()}
	}
	return result
}
func teamModelCreationHash(actorID, etag string, input TeamModelRequestInput) string {
	return personalHash(struct {
		ActorID, ETag string
		Input         TeamModelRequestInput
	}{actorID, etag, input})
}
func teamModelDecisionHash(actorID, requestID, etag string, input TeamModelDecisionInput) string {
	return personalHash(struct {
		ActorID, RequestID, ETag string
		Input                    TeamModelDecisionInput
	}{actorID, requestID, etag, input})
}

type teamModelRequestCursor struct {
	Scope     string
	CreatedAt time.Time
	ID        string
}

func teamModelCursorScope(actorID, teamID string, filter TeamModelRequestFilter, reviewer bool) string {
	return personalHash(struct {
		ActorID, TeamID string
		Filter          TeamModelRequestFilter
		Reviewer        bool
	}{actorID, teamID, TeamModelRequestFilter{Status: filter.Status, TeamID: filter.TeamID, ModelID: filter.ModelID}, reviewer})
}
func encodeTeamModelRequestCursor(value teamModelRequestCursor) string {
	data, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(data)
}
func decodeTeamModelRequestCursor(raw, scope string) (*teamModelRequestCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 512 {
		return nil, apperrors.ErrBadRequest
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	var cursor teamModelRequestCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Scope != scope || cursor.CreatedAt.IsZero() || !personalModelID(cursor.ID, "tmr") {
		return nil, apperrors.ErrBadRequest
	}
	return &cursor, nil
}
