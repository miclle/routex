package service

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

// TeamCreationInput is the opt-in reviewed operation. The handler alone selects
// the legacy path; this entry never discards an incomplete reviewed intent.
type TeamCreationInput struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	OwnerIDs      []string        `json:"owner_ids"`
	CreationID    string          `json:"creation_id"`
	ReviewETag    string          `json:"-"`
	InitialLimits *TeamLimitInput `json:"initial_limits,omitempty"`
}

func (input *TeamCreationInput) UnmarshalJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return apperrors.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	var result TeamCreationInput
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return apperrors.ErrBadRequest
		}
		field, ok := token.(string)
		if !ok || seen[field] {
			return apperrors.ErrBadRequest
		}
		seen[field] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
		switch field {
		case "name":
			err = json.Unmarshal(value, &result.Name)
		case "description":
			err = json.Unmarshal(value, &result.Description)
		case "owner_ids":
			result.OwnerIDs, err = projectCreationStringIDs(value, false)
		case "creation_id":
			err = json.Unmarshal(value, &result.CreationID)
			if err == nil && !credentialReplacementRequestID.MatchString(result.CreationID) {
				return apperrors.ErrBadRequest
			}
		case "initial_limits":
			err = json.Unmarshal(value, &result.InitialLimits)
		default:
			return apperrors.ErrBadRequest
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
	}
	if _, err := decoder.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	*input = result
	return nil
}

// Preserve the sparse wire shape when hashing or constructing a reviewed body.
func (input TeamCreationInput) MarshalJSON() ([]byte, error) {
	body := map[string]any{"name": input.Name, "description": input.Description, "owner_ids": input.OwnerIDs, "creation_id": input.CreationID}
	if input.InitialLimits != nil {
		fields := map[string]any{"reason": input.InitialLimits.Reason}
		for name, value := range input.InitialLimits.Fields {
			fields[name] = value
		}
		body["initial_limits"] = fields
	}
	return json.Marshal(body)
}

func normalizeTeamCreationInput(input TeamCreationInput) (TeamCreationInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if !credentialReplacementRequestID.MatchString(input.CreationID) || !teamSessionDigest.MatchString(input.ReviewETag) || !validCatalogLabel(input.Name) || !validResourceDescription(input.Description) || len(input.OwnerIDs) < 1 || len(input.OwnerIDs) > 1000 {
		return input, apperrors.ErrBadRequest
	}
	input.OwnerIDs = slices.Clone(input.OwnerIDs)
	slices.Sort(input.OwnerIDs)
	for i, value := range input.OwnerIDs {
		if !strings.HasPrefix(value, "usr_") || !safeTeamSessionID(value) || i > 0 && value == input.OwnerIDs[i-1] {
			return input, apperrors.ErrBadRequest
		}
	}
	if input.InitialLimits != nil {
		value := TeamLimitInput{Reason: strings.TrimSpace(input.InitialLimits.Reason), Fields: map[string]json.RawMessage{}}
		if value.Reason == "" || len(value.Reason) > 2000 || !utf8.ValidString(value.Reason) || strings.ContainsFunc(value.Reason, unicode.IsControl) {
			return input, apperrors.ErrBadRequest
		}
		currency := ""
		for field, raw := range input.InitialLimits.Fields {
			if field != "currency" && !slices.Contains(teamLimitFields, field) || !utf8.Valid(raw) {
				return input, apperrors.ErrBadRequest
			}
			var compact bytes.Buffer
			if json.Compact(&compact, raw) != nil {
				return input, apperrors.ErrBadRequest
			}
			value.Fields[field] = slices.Clone(compact.Bytes())
			if field == "currency" && (bytes.Equal(compact.Bytes(), []byte("null")) || json.Unmarshal(raw, &currency) != nil) {
				return input, apperrors.ErrBadRequest
			}
		}
		if _, err := applyTeamLimitInput(limits.Policy{}, value, "team", teamLimitFields, currency); err != nil {
			return input, apperrors.ErrBadRequest
		}
		input.InitialLimits = &value
	}
	return input, nil
}

func teamCreationHash(actorID string, input TeamCreationInput) string {
	return personalHash(struct {
		Domain, Actor, Review string
		Input                 TeamCreationInput
	}{"routex.team-creation.intent.v1", actorID, input.ReviewETag, input})
}

func teamCreationSubmittedAllowed(input *TeamLimitInput, editable []string) bool {
	if input == nil {
		return true
	}
	for field := range input.Fields {
		if field != "currency" && !slices.Contains(editable, field) {
			return false
		}
	}
	return true
}
