package oidc

import (
	"bytes"
	"encoding/json"
	"io"
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
func googleJSONObject(data []byte) (map[string]any, error) {
	if !utf8.Valid(data) || !googleValidStringEscapes(data) {
		return nil, ErrProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	value, err := googleJSONValue(decoder, 0, &nodes)
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

func googleJSONValue(decoder *json.Decoder, depth int, nodes *int) (any, error) {
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
				child, err := googleJSONValue(decoder, depth+1, nodes)
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
				child, err := googleJSONValue(decoder, depth+1, nodes)
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
func googleValidStringEscapes(data []byte) bool {
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
		unit, ok := googleHexUnit(data[i+1 : i+5])
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
			low, ok := googleHexUnit(data[i+3 : i+7])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !quoted
}

func googleHexUnit(data []byte) (uint16, bool) {
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
