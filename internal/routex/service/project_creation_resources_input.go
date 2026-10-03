package service

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

type ProjectInitialResources struct {
	ModelIDs    []string `json:"model_ids,omitempty"`
	TokensMonth *int64   `json:"tokens_month,omitempty"`
	MoneyMonth  *string  `json:"money_month,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	RPM         *int64   `json:"rpm,omitempty"`
	TPM         *int64   `json:"tpm,omitempty"`
	Concurrency *int64   `json:"concurrency,omitempty"`
	Reason      string   `json:"reason"`
}

func projectCreationStringIDs(raw []byte, allowEmpty bool) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' {
		return nil, apperrors.ErrBadRequest
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || !allowEmpty && len(values) == 0 {
		return nil, apperrors.ErrBadRequest
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		var id string
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &id) != nil {
			return nil, apperrors.ErrBadRequest
		}
		result = append(result, id)
	}
	return result, nil
}

func (input *ProjectInitialResources) UnmarshalJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return apperrors.ErrBadRequest
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	var result ProjectInitialResources
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		field, ok := token.(string)
		if err != nil || !ok || seen[field] {
			return apperrors.ErrBadRequest
		}
		seen[field] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
		var target any
		switch field {
		case "model_ids":
			result.ModelIDs, err = projectCreationStringIDs(value, true)
			if err != nil {
				return err
			}
			continue
		case "tokens_month":
			target = &result.TokensMonth
		case "money_month":
			target = &result.MoneyMonth
		case "currency":
			target = &result.Currency
		case "rpm":
			target = &result.RPM
		case "tpm":
			target = &result.TPM
		case "concurrency":
			target = &result.Concurrency
		case "reason":
			target = &result.Reason
		default:
			return apperrors.ErrBadRequest
		}
		if json.Unmarshal(value, target) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if _, err := d.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if _, err := d.Token(); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	*input = result
	return nil
}

func (input ProjectInitialResources) hasQuota() bool {
	return input.TokensMonth != nil || input.MoneyMonth != nil
}
func (input ProjectInitialResources) hasRates() bool {
	return input.RPM != nil || input.TPM != nil || input.Concurrency != nil
}
func (input ProjectInitialResources) hasLimits() bool { return input.hasQuota() || input.hasRates() }
func (input ProjectInitialResources) policy() (limits.Policy, error) {
	return limits.Normalize(limits.Policy{TokensMonth: input.TokensMonth, MoneyMonth: input.MoneyMonth, Currency: input.Currency, RPM: input.RPM, TPM: input.TPM, Concurrency: input.Concurrency, IPMode: "none"})
}

func normalizeProjectInitialResources(input ProjectInitialResources) (ProjectInitialResources, error) {
	if len(input.ModelIDs) == 0 && !input.hasLimits() || len(input.ModelIDs) > 1000 || !validResourceDescription(input.Reason) || len(strings.TrimSpace(input.Reason)) == 0 {
		return input, apperrors.ErrBadRequest
	}
	input.ModelIDs = slices.Clone(input.ModelIDs)
	slices.Sort(input.ModelIDs)
	for i, model := range input.ModelIDs {
		if !strings.HasPrefix(model, "mdl_") || !safeTeamSessionID(model) || i > 0 && model == input.ModelIDs[i-1] {
			return input, apperrors.ErrBadRequest
		}
	}
	if input.MoneyMonth == nil && input.Currency != "" {
		return input, apperrors.ErrBadRequest
	}
	policy, err := input.policy()
	if err != nil {
		return input, apperrors.ErrBadRequest
	}
	input.MoneyMonth, input.Currency = policy.MoneyMonth, policy.Currency
	return input, nil
}

func normalizeProjectCreationResources(input ProjectCreationInput) (ProjectCreationInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if !credentialReplacementRequestID.MatchString(input.CreationID) || !personalModelETag(input.ReviewETag) || !validCatalogLabel(input.Name) || !validResourceDescription(input.Description) || input.InitialResources != nil && input.InitialRequest != nil {
		return input, apperrors.ErrBadRequest
	}
	if input.ManagerIDs != nil {
		selected, err := projectCreationManagers("", true, input.ManagerIDs)
		if err != nil {
			return input, err
		}
		input.ManagerIDs = &selected
	}
	for _, target := range []**ProjectInitialResources{&input.InitialResources, &input.InitialRequest} {
		if *target != nil {
			normalized, err := normalizeProjectInitialResources(**target)
			if err != nil {
				return input, err
			}
			*target = &normalized
		}
	}
	return input, nil
}
