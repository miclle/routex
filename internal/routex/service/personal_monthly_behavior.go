package service

import (
	"bytes"
	"encoding/json"
	"io"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

func userMonthlyBehaviorWire(policy limits.Policy) limits.Policy {
	policy.TokensMonthBehavior = limits.StoredMonthlyBehavior(policy.TokensMonthBehavior)
	policy.MoneyMonthBehavior = limits.StoredMonthlyBehavior(policy.MoneyMonthBehavior)
	return policy
}

// UnmarshalJSON retains legacy full-policy defaults while rejecting duplicate or
// malformed modes. Explicit behavior presence cannot reach a non-User write.
func (input *LimitInput) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return apperrors.ErrBadRequest
		}
		key, ok := token.(string)
		if !ok {
			return apperrors.ErrBadRequest
		}
		if _, exists := fields[key]; exists {
			return apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return apperrors.ErrBadRequest
		}
		fields[key] = value
	}
	if _, err = decoder.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if _, err = decoder.Token(); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	type decodedInput struct {
		limits.Policy
		Reason string `json:"reason"`
	}
	var decoded decodedInput
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if strict.Decode(&decoded) != nil {
		return apperrors.ErrBadRequest
	}
	present := false
	for _, key := range []string{"tokens_month_behavior", "money_month_behavior"} {
		if value, exists := fields[key]; exists {
			present = true
			var mode string
			if json.Unmarshal(value, &mode) != nil || (mode != limits.MonthlyBehaviorStop && mode != limits.MonthlyBehaviorAlertOnly) {
				return apperrors.ErrBadRequest
			}
		}
	}
	*input = LimitInput{Policy: decoded.Policy, Reason: decoded.Reason, monthlyBehaviorPresent: present}
	return nil
}

// Historical blank modes are stop; malformed or foreign soft audit facts stay unknown.
func validMonthlyBehaviorAudit(kind string, policy limits.Policy) bool {
	tokens, err := limits.CanonicalMonthlyBehavior(policy.TokensMonthBehavior)
	if err != nil {
		return false
	}
	money, err := limits.CanonicalMonthlyBehavior(policy.MoneyMonthBehavior)
	return err == nil && (kind == "user" || kind == "team" || kind == "project" || tokens == "" && money == "")
}
