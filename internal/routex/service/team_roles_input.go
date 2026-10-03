package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

type TeamRoleInput struct {
	RoleIDs []string `json:"role_ids"`
	Reason  string   `json:"reason"`
}

func (input *TeamRoleInput) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	result := TeamRoleInput{RoleIDs: []string{}}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return apperrors.ErrBadRequest
		}
		field, ok := token.(string)
		if !ok || seen[field] || field != "role_ids" && field != "reason" {
			return apperrors.ErrBadRequest
		}
		seen[field] = true
		switch field {
		case "reason":
			token, err = decoder.Token()
			if err != nil {
				return apperrors.ErrBadRequest
			}
			value, ok := token.(string)
			if !ok {
				return apperrors.ErrBadRequest
			}
			result.Reason = value
		case "role_ids":
			token, err = decoder.Token()
			if err != nil || token != json.Delim('[') {
				return apperrors.ErrBadRequest
			}
			for decoder.More() {
				token, err = decoder.Token()
				if err != nil {
					return apperrors.ErrBadRequest
				}
				value, ok := token.(string)
				if !ok || len(result.RoleIDs) == 100 {
					return apperrors.ErrBadRequest
				}
				result.RoleIDs = append(result.RoleIDs, value)
			}
			if _, err = decoder.Token(); err != nil {
				return apperrors.ErrBadRequest
			}
		}
	}
	if _, err = decoder.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if _, err = decoder.Token(); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	if !seen["role_ids"] || !seen["reason"] {
		return apperrors.ErrBadRequest
	}
	*input = result
	return nil
}
func teamRoleCursorScope(actorID, teamID, query, etag string) string {
	hash, _ := teamQuotaHash([]string{actorID, teamID, query, etag})
	return hash
}
func encodeTeamRoleCursor(after, actorID, teamID, query, etag string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(after + "|" + teamRoleCursorScope(actorID, teamID, query, etag)))
}
func decodeTeamRoleCursor(cursor, actorID, teamID, query, etag string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", apperrors.ErrBadRequest
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 || !safeTeamSessionID(parts[0]) || !strings.HasPrefix(parts[0], "rol_") {
		return "", apperrors.ErrBadRequest
	}
	if parts[1] != teamRoleCursorScope(actorID, teamID, query, etag) {
		return "", catalogConflict
	}
	return parts[0], nil
}
