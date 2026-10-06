package vault

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// Bound duplicate-key/depth validation before projection prevents aliases or
// repeated metadata fields from converting an ambiguous response into proof.
func validJSON(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	if !jsonObject(decoder, 1) {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}
func jsonValue(d *json.Decoder, depth int) bool {
	if depth > 16 {
		return false
	}
	token, err := d.Token()
	if err != nil {
		return false
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			return jsonObject(d, depth)
		case '[':
			for d.More() {
				if !jsonValue(d, depth+1) {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	return true
}
func jsonObject(d *json.Decoder, depth int) bool {
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return false
		}
		seen[name] = true
		if !jsonValue(d, depth+1) {
			return false
		}
	}
	end, err := d.Token()
	return err == nil && end == json.Delim('}')
}
func object(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var value map[string]json.RawMessage
	err := json.Unmarshal(raw, &value)
	return value, err == nil && value != nil
}
func metadata(raw json.RawMessage, expected int64) bool {
	value, ok := object(raw)
	if !ok {
		return false
	}
	var version *int64
	var destroyed *bool
	var deleted *string
	// Missing/null fields cannot establish a live immutable version.
	return json.Unmarshal(value["version"], &version) == nil && version != nil && *version == expected && json.Unmarshal(value["destroyed"], &destroyed) == nil && destroyed != nil && !*destroyed && json.Unmarshal(value["deletion_time"], &deleted) == nil && deleted != nil && *deleted == ""

}
func writeVersion(raw []byte) (int64, error) {
	root, ok := object(raw)
	if !ok {
		return 0, io.ErrUnexpectedEOF
	}
	// CAS=0 on a fresh generated path must return its first version. Never clean
	// an unproven existing key merely because an arbitrary version was returned.
	if !metadata(root["data"], 1) {
		return 0, io.ErrUnexpectedEOF
	}
	return 1, nil
}
func matchesRead(raw []byte, field, value string, version int64) bool {
	root, ok := object(raw)
	if !ok {
		return false
	}
	data, ok := object(root["data"])
	if !ok || !metadata(data["metadata"], version) {
		return false
	}
	fields, ok := object(data["data"])
	if !ok || len(fields) != 1 {
		return false
	}
	var recorded string
	return json.Unmarshal(fields[field], &recorded) == nil && subtle.ConstantTimeCompare([]byte(recorded), []byte(value)) == 1
}

func noResponseErrors(raw []byte) bool {
	root, ok := object(raw)
	if !ok {
		return false
	}
	value, exists := root["errors"]
	if !exists {
		return true
	}
	var errors []string
	return json.Unmarshal(value, &errors) == nil && len(errors) == 0
}

// Destroy returns 204. Vault may replace it with a 200 warnings envelope; no
// secret, lease, authentication or wrapping result belongs to this operation.
// Warning text is validated for bounded shape only and never returned or logged.
func cleanupAcknowledged(raw []byte) bool {
	root, ok := object(raw)
	if !ok {
		return false
	}
	var warnings []string
	if json.Unmarshal(root["warnings"], &warnings) != nil || len(warnings) == 0 || len(warnings) > 32 {
		return false
	}
	for _, warning := range warnings {
		if len(warning) == 0 || len(warning) > 1024 {
			return false
		}
	}
	for key, field := range root {
		switch key {
		case "warnings":
		case "data", "auth", "wrap_info":
			if !bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
				return false
			}
		case "errors":
			var errors []string
			if json.Unmarshal(field, &errors) != nil || errors == nil || len(errors) != 0 {
				return false
			}
		case "request_id":
			var value string
			if json.Unmarshal(field, &value) != nil || len(value) == 0 || len(value) > 128 {
				return false
			}
		case "mount_type":
			var value string
			if json.Unmarshal(field, &value) != nil || value != "kv" {
				return false
			}
		case "lease_id":
			var value *string
			if json.Unmarshal(field, &value) != nil || value == nil || *value != "" {
				return false
			}
		case "lease_duration":
			var value *int64
			if json.Unmarshal(field, &value) != nil || value == nil || *value != 0 {
				return false
			}
		case "renewable":
			var value *bool
			if json.Unmarshal(field, &value) != nil || value == nil || *value {
				return false
			}
		default:
			return false
		}
	}
	return true
}
