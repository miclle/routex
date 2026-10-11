package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

// The finite wire grammar rejects discarded intent before Service admission.
// Field semantics, owner authorization and immutable command claims remain in Service.
func apiKeyDeliveryJSONFields(kind string) map[string]string {
	switch kind {
	case "policy":
		return map[string]string{"allowed_modes": "texts", "default_mode": "text", "integration_id": "text", "writer_identity_id": "text", "provisioner_identity_id": "text", "reason": "text"}
	case "identity":
		return map[string]string{"purpose": "text", "integration_id": "text", "token": "text", "reason": "text"}
	case "template":
		return map[string]string{"auth_mount": "text", "issuer": "text", "jwks_url": "text", "audience": "text", "allowed_subjects": "texts", "token_ttl_seconds": "uint", "token_max_ttl_seconds": "uint", "reason": "text"}
	case "reason":
		return map[string]string{"reason": "text"}
	case "resolve":
		return map[string]string{"request_id": "text", "consumer_id": "text", "application_name": "text", "environment": "text", "principal": "principal", "reason": "text"}
	case "approval":
		return map[string]string{"approved": "bool", "control_verified": "bool", "reason": "text"}
	case "isolation":
		return map[string]string{"isolated": "bool", "reason": "text"}
	case "reconcile":
		return map[string]string{"request_id": "text", "reason": "text"}
	case "managed":
		return map[string]string{"name": "text", "model_ids": "texts", "expires_at": "expiry", "request_id": "text", "reason": "text", "delivery": "selection"}
	case "selection":
		return map[string]string{"consumer_id": "text", "environment": "text", "principal": "principal", "target_review": "text"}
	case "principal":
		return map[string]string{"method": "text", "subject": "text"}
	}
	return nil
}

func apiKeyDeliveryJSONShape(raw []byte, kind string) error {
	fields := apiKeyDeliveryJSONFields(kind)
	if fields == nil {
		return apperrors.ErrBadRequest
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	seen := make(map[string]bool, len(fields))
	for d.More() {
		token, e := d.Token()
		name, ok := token.(string)
		typ, allowed := fields[name]
		if e != nil || !ok || !allowed || seen[name] {
			return apperrors.ErrBadRequest
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return apperrors.ErrBadRequest
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && typ != "expiry" {
			return apperrors.ErrBadRequest
		}
		switch typ {
		case "text":
			var v string
			if json.Unmarshal(value, &v) != nil || len(v) > 4096 || name == "reason" && len(v) > 1024 {
				return apperrors.ErrBadRequest
			}
		case "texts":
			var v []json.RawMessage
			if json.Unmarshal(value, &v) != nil || v == nil || len(v) > 128 {
				return apperrors.ErrBadRequest
			}
			for _, item := range v {
				var text string
				if bytes.Equal(bytes.TrimSpace(item), []byte("null")) || json.Unmarshal(item, &text) != nil || len(text) > 4096 {
					return apperrors.ErrBadRequest
				}
			}
		case "bool":
			var v bool
			if json.Unmarshal(value, &v) != nil {
				return apperrors.ErrBadRequest
			}
		case "uint":
			var v uint64
			if json.Unmarshal(value, &v) != nil || strconv.FormatUint(v, 10) != string(bytes.TrimSpace(value)) {
				return apperrors.ErrBadRequest
			}
		case "expiry":
			if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				var text string
				if json.Unmarshal(value, &text) != nil || len(text) > 64 {
					return apperrors.ErrBadRequest
				}
			}
		default:
			if e := apiKeyDeliveryJSONShape(value, typ); e != nil {
				return e
			}
		}
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') || d.Decode(new(any)) != io.EOF {
		return apperrors.ErrBadRequest
	}
	for name := range fields {
		// Absent consumer_id requests server allocation; expiry is optional/nullable.
		if !seen[name] && name != "expires_at" && (kind != "resolve" || name != "consumer_id") {
			return apperrors.ErrBadRequest
		}
	}
	return nil
}

func decodeAPIKeyDeliveryJSON(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > 64<<10 || !utf8.Valid(raw) || !secretRotationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	kind := ""
	switch target.(type) {
	case *service.APIKeyDeliveryPolicyInput:
		kind = "policy"
	case *service.APIKeyDeliveryIdentityInput:
		kind = "identity"
	case *service.APIKeyCallerTemplateInput:
		kind = "template"
	case *apiKeyDeliveryReasonInput:
		kind = "reason"
	case *service.APIKeyDeliveryResolveInput:
		kind = "resolve"
	case *service.APIKeyDeliveryApprovalInput:
		kind = "approval"
	case *service.APIKeyDeliveryIsolationInput:
		kind = "isolation"
	case *service.APIKeyDeliveryReconcileInput:
		kind = "reconcile"
	case *service.ManagedAPIKeyInput:
		kind = "managed"
	default:
		return apperrors.ErrBadRequest
	}
	if e := apiKeyDeliveryJSONShape(raw, kind); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return apperrors.ErrBadRequest
	}
	return nil
}

func apiKeyDeliveryTargetQuery(raw string, force bool) (string, int, error) {
	if force || len(raw) > 1024 {
		return "", 0, apperrors.ErrBadRequest
	}
	q, e := url.ParseQuery(raw)
	if e != nil {
		return "", 0, apperrors.ErrBadRequest
	}
	for key, values := range q {
		if key != "cursor" && key != "limit" || len(values) != 1 || values[0] == "" {
			return "", 0, apperrors.ErrBadRequest
		}
	}
	limit := 20
	if text := q.Get("limit"); text != "" {
		n, e := strconv.Atoi(text)
		if e != nil || strconv.Itoa(n) != text || n < 1 || n > 100 {
			return "", 0, apperrors.ErrBadRequest
		}
		limit = n
	}
	cursor := q.Get("cursor")
	if len(cursor) > 30 {
		return "", 0, apperrors.ErrBadRequest
	}
	for _, r := range cursor {
		if r < 33 || r > 126 || r == ':' {
			return "", 0, apperrors.ErrBadRequest
		}
	}
	return cursor, limit, nil
}
