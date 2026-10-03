package service

import (
	"bytes"
	"encoding/json"
	"io"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func decodeTeamQuotaStrings(raw []byte, required, optional []string) (map[string]string, error) {
	allowed := map[string]bool{}
	for _, field := range required {
		allowed[field] = true
	}
	for _, field := range optional {
		allowed[field] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, apperrors.ErrBadRequest
	}
	result := map[string]string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, apperrors.ErrBadRequest
		}
		field, ok := token.(string)
		if !ok || !allowed[field] {
			return nil, apperrors.ErrBadRequest
		}
		if _, present := result[field]; present {
			return nil, apperrors.ErrBadRequest
		}
		token, err = decoder.Token()
		if err != nil {
			return nil, apperrors.ErrBadRequest
		}
		value, ok := token.(string)
		if !ok {
			return nil, apperrors.ErrBadRequest
		}
		result[field] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, apperrors.ErrBadRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, apperrors.ErrBadRequest
	}
	for _, field := range required {
		if _, present := result[field]; !present {
			return nil, apperrors.ErrBadRequest
		}
	}
	return result, nil
}
func (input *TeamQuotaRequestInput) UnmarshalJSON(raw []byte) error {
	fields, err := decodeTeamQuotaStrings(raw, []string{"request_id", "dimension", "target_value", "reason"}, nil)
	if err != nil {
		return err
	}
	*input = TeamQuotaRequestInput{RequestID: fields["request_id"], Dimension: fields["dimension"], TargetValue: fields["target_value"], Reason: fields["reason"]}
	return nil
}
func (input *TeamQuotaDecisionInput) UnmarshalJSON(raw []byte) error {
	fields, err := decodeTeamQuotaStrings(raw, []string{"decision_id", "step_id", "action"}, []string{"reason"})
	if err != nil {
		return err
	}
	*input = TeamQuotaDecisionInput{DecisionID: fields["decision_id"], StepID: fields["step_id"], Action: fields["action"], Reason: fields["reason"]}
	return nil
}
