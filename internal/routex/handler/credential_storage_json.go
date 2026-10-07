package handler

import (
	"bytes"
	"encoding/json"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"io"
	"regexp"
)

var credentialCreationUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var credentialCreationReview = regexp.MustCompile(`^[0-9a-f]{64}$`)

func decodeCredentialCreationJSON(raw []byte, target any) error {
	fields := map[string]json.RawMessage{}
	scan := json.NewDecoder(bytes.NewReader(raw))
	token, err := scan.Token()
	if err != nil || token != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	for scan.More() {
		key, err := scan.Token()
		if err != nil {
			return apperrors.ErrBadRequest
		}
		name, ok := key.(string)
		if !ok {
			return apperrors.ErrBadRequest
		}
		if _, exists := fields[name]; exists {
			return apperrors.ErrBadRequest
		}
		var value json.RawMessage
		if scan.Decode(&value) != nil {
			return apperrors.ErrBadRequest
		}
		fields[name] = value
	}
	if _, err := scan.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if scan.Decode(new(any)) != io.EOF {
		return apperrors.ErrBadRequest
	}
	for k, rule := range map[string]*regexp.Regexp{"request_id": credentialCreationUUID, "storage_policy_etag": credentialCreationReview} {
		if v, ok := fields[k]; ok {
			var text string
			if json.Unmarshal(v, &text) != nil || !rule.MatchString(text) {
				return apperrors.ErrBadRequest
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return apperrors.ErrBadRequest
	}
	if decoder.Decode(new(any)) != io.EOF {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (v *CreateProviderRequest) UnmarshalJSON(raw []byte) error {
	type plain CreateProviderRequest
	var n plain
	if e := decodeCredentialCreationJSON(raw, &n); e != nil {
		return e
	}
	*v = CreateProviderRequest(n)
	return nil
}
func (v *CreateConnectionRequest) UnmarshalJSON(raw []byte) error {
	type plain CreateConnectionRequest
	var n plain
	if e := decodeCredentialCreationJSON(raw, &n); e != nil {
		return e
	}
	*v = CreateConnectionRequest(n)
	return nil
}
func (v *CreateCredentialRequest) UnmarshalJSON(raw []byte) error {
	type plain CreateCredentialRequest
	var n plain
	if e := decodeCredentialCreationJSON(raw, &n); e != nil {
		return e
	}
	*v = CreateCredentialRequest(n)
	return nil
}
