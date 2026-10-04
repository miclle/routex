package service

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func normalizeMemberModels(input MemberModelsWriteInput) (MemberModelsWriteInput, error) {
	if input.ModelIDs == nil || len(input.ModelIDs) > memberCatalogModelLimit || strings.TrimSpace(input.Reason) != input.Reason || !validCredentialMetadataReason(input.Reason) || strings.TrimSpace(input.Reason) == "" {
		return input, apperrors.ErrBadRequest
	}
	input.ModelIDs = slices.Clone(input.ModelIDs)
	slices.Sort(input.ModelIDs)
	for i, id := range input.ModelIDs {
		if !validAdminModelTarget(id) || i > 0 && input.ModelIDs[i-1] == id {
			return input, apperrors.ErrBadRequest
		}
	}
	return input, nil
}
func (input *MemberModelsWriteInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"model_ids", "reason"})
	if err != nil {
		return err
	}
	var value MemberModelsWriteInput
	if string(fields["model_ids"]) == "null" || string(fields["reason"]) == "null" || json.Unmarshal(fields["model_ids"], &value.ModelIDs) != nil || json.Unmarshal(fields["reason"], &value.Reason) != nil {
		return apperrors.ErrBadRequest
	}
	value, err = normalizeMemberModels(value)
	if err != nil {
		return err
	}
	*input = value
	return nil
}
