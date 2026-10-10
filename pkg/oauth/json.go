package oauth

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	maxJSONDepth       = 32
	maxJSONNodes       = 8192
	maxObjectMembers   = 256
	maxArrayElements   = 1024
	maxJSONStringBytes = 64 << 10
)

// Strict parsing rejects duplicate decoded keys at every depth. UseNumber keeps
// the exact subject lexeme rather than coercing identities through a float.
func object(data []byte) (map[string]any, error) {
	if !utf8.Valid(data) || !validStringEscapes(data) {
		return nil, ErrProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	value, err := jsonValue(decoder, 0, &nodes)
	if err != nil {
		return nil, ErrProtocol
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrProtocol
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, ErrProtocol
	}
	return result, nil
}

func jsonValue(decoder *json.Decoder, depth int, nodes *int) (any, error) {
	(*nodes)++
	if depth > maxJSONDepth || *nodes > maxJSONNodes {
		return nil, ErrProtocol
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, ErrProtocol
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			result := make(map[string]any)
			count := 0
			for decoder.More() {
				count++
				if count > maxObjectMembers {
					return nil, ErrProtocol
				}
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok || len(key) > maxJSONStringBytes {
					return nil, ErrProtocol
				}
				if _, exists := result[key]; exists {
					return nil, ErrProtocol
				}
				child, err := jsonValue(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				result[key] = child
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return nil, ErrProtocol
			}
			return result, nil
		case '[':
			result := make([]any, 0)
			for decoder.More() {
				if len(result) >= maxArrayElements {
					return nil, ErrProtocol
				}
				child, err := jsonValue(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				result = append(result, child)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return nil, ErrProtocol
			}
			return result, nil
		default:
			return nil, ErrProtocol
		}
	case string:
		if len(value) > maxJSONStringBytes {
			return nil, ErrProtocol
		}
		return value, nil
	case json.Number, bool, nil:
		return value, nil
	default:
		return nil, ErrProtocol
	}
}

// encoding/json replaces unpaired surrogate escapes with U+FFFD. Reject those
// before parsing so different malformed byte strings cannot alias an identity.
// This validates escapes only; encoding/json remains the JSON grammar parser.
func validStringEscapes(data []byte) bool {
	quoted := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		unit, ok := hexUnit(data[i+1 : i+5])
		if !ok {
			return false
		}
		i += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return false
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, ok := hexUnit(data[i+3 : i+7])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !quoted
}

func hexUnit(data []byte) (uint16, bool) {
	var value uint16
	for _, c := range data {
		value <<= 4
		switch {
		case c >= '0' && c <= '9':
			value += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			value += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			value += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return value, true
}

func accessToken(data []byte) (string, error) {
	fields, err := object(data)
	if err != nil {
		return "", err
	}
	if _, exists := fields["error"]; exists {
		return "", ErrProtocol
	}
	token, ok := fields["access_token"].(string)
	kind, kindOK := fields["token_type"].(string)
	if !ok || !kindOK || !strings.EqualFold(kind, "Bearer") || !bearer(token) {
		return "", ErrProtocol
	}
	return token, nil
}

// RFC 6750 b64token: one or more alphabet characters, then optional '=' padding.
func bearer(value string) bool {
	if len(value) == 0 || len(value) > 4096 {
		return false
	}
	padding := false
	alphabet := 0
	for _, c := range []byte(value) {
		if c == '=' {
			padding = true
			continue
		}
		valid := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+/", rune(c))
		if padding || !valid {
			return false
		}
		alphabet++
	}
	return alphabet > 0
}

func subject(data []byte, segments []string) (Identity, error) {
	fields, err := object(data)
	if err != nil {
		return Identity{}, err
	}
	var value any = fields
	for _, segment := range segments {
		parent, ok := value.(map[string]any)
		if !ok {
			return Identity{}, ErrProtocol
		}
		var exists bool
		value, exists = parent[segment]
		if !exists {
			return Identity{}, ErrProtocol
		}
	}
	switch stable := value.(type) {
	case string:
		if !text(stable, maxSubjectBytes) {
			return Identity{}, ErrProtocol
		}
		return Identity{Kind: SubjectString, Subject: stable}, nil
	case json.Number:
		raw := string(stable)
		if len(raw) == 0 || len(raw) > maxSubjectBytes || (len(raw) > 1 && raw[0] == '0') {
			return Identity{}, ErrProtocol
		}
		for _, c := range []byte(raw) {
			if c < '0' || c > '9' {
				return Identity{}, ErrProtocol
			}
		}
		return Identity{Kind: SubjectInteger, Subject: raw}, nil
	default:
		return Identity{}, ErrProtocol
	}
}
