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
)

func memberRoleID(id string) bool { return safeTeamSessionID(id) && strings.HasPrefix(id, "rol_") }
func validateMemberRolesInput(input MemberRolesInput) error {
	if input.RoleIDs == nil || input.RoleDefinitions == nil || len(input.RoleIDs) > memberRolesWriteBudget || len(input.RoleIDs) != len(input.RoleDefinitions) || !validMemberRoleDigest(input.BuiltinDefinitionETag) || !utf8.ValidString(input.Reason) || input.Reason == "" || len(input.Reason) > 1024 || strings.TrimSpace(input.Reason) != input.Reason || strings.ContainsFunc(input.Reason, unicode.IsControl) {
		return apperrors.ErrBadRequest
	}
	if !slices.IsSorted(input.RoleIDs) {
		return apperrors.ErrBadRequest
	}
	for i, id := range input.RoleIDs {
		if !memberRoleID(id) || id == "rol_admin" || id == "rol_member" || i > 0 && input.RoleIDs[i-1] == id || input.RoleDefinitions[i].ID != id || !validMemberRoleDigest(input.RoleDefinitions[i].ETag) {
			return apperrors.ErrBadRequest
		}
	}
	return nil
}
func (input *MemberRolesInput) UnmarshalJSON(raw []byte) error {
	// Preserve ambiguous/duplicate fields and malformed Unicode as rejection, not normalized intent.
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := memberRolesObject(raw)
	if err != nil {
		return err
	}
	if len(fields) != 4 {
		return apperrors.ErrBadRequest
	}
	var decoded MemberRolesInput
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
		switch name {
		case "role_ids":
			err = json.Unmarshal(value, &decoded.RoleIDs)
		case "builtin_definition_etag":
			err = json.Unmarshal(value, &decoded.BuiltinDefinitionETag)
		case "reason":
			err = json.Unmarshal(value, &decoded.Reason)
		case "role_definitions":
			var rows []json.RawMessage
			err = json.Unmarshal(value, &rows)
			if err == nil {
				decoded.RoleDefinitions = []MemberRoleDefinitionProof{}
				for _, row := range rows {
					proof, e := memberRolesObject(row)
					if e != nil || len(proof) != 2 {
						return apperrors.ErrBadRequest
					}
					var item MemberRoleDefinitionProof
					for key, v := range proof {
						if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
							return apperrors.ErrBadRequest
						}
						switch key {
						case "id":
							e = json.Unmarshal(v, &item.ID)
						case "etag":
							e = json.Unmarshal(v, &item.ETag)
						default:
							return apperrors.ErrBadRequest
						}
						if e != nil {
							return apperrors.ErrBadRequest
						}
					}
					decoded.RoleDefinitions = append(decoded.RoleDefinitions, item)
				}
			}
		default:
			return apperrors.ErrBadRequest
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
	}
	if err := validateMemberRolesInput(decoded); err != nil {
		return err
	}
	*input = decoded
	return nil
}
func memberRolesObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, apperrors.ErrBadRequest
	}
	result := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return nil, apperrors.ErrBadRequest
		}
		if _, dup := result[name]; dup {
			return nil, apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, apperrors.ErrBadRequest
		}
		result[name] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, apperrors.ErrBadRequest
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, apperrors.ErrBadRequest
	}
	return result, nil
}
