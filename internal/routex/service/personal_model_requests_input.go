package service

import (
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func personalModelStringObject(data []byte, required, optional []string) (map[string]string, error) {
	// encoding/json replaces malformed UTF-8, which would change the frozen intent.
	if !utf8.Valid(data) {
		return nil, apperrors.ErrBadRequest
	}
	return decodeTeamQuotaStrings(data, required, optional)
}
func (input *PersonalModelRequestInput) UnmarshalJSON(data []byte) error {
	v, err := personalModelStringObject(data, []string{"request_id", "model_id", "reason"}, nil)
	if err == nil {
		*input = PersonalModelRequestInput{RequestID: v["request_id"], ModelID: v["model_id"], Reason: v["reason"]}
	}
	return err
}
func (input *PersonalModelDecisionInput) UnmarshalJSON(data []byte) error {
	v, err := personalModelStringObject(data, []string{"decision_id", "action"}, []string{"reason"})
	if err == nil {
		*input = PersonalModelDecisionInput{DecisionID: v["decision_id"], Action: v["action"], Reason: v["reason"]}
	}
	return err
}
