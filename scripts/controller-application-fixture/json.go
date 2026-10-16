package main

import (
	"bytes"
	"encoding/json"
	"io"
)

// Duplicate keys can make two parsers disagree about the fixture's native
// outcome or configuration. Reject them before projecting either structure.
func validJSONObject(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}
	if !consumeObject(d, 0) {
		return false
	}
	_, err = d.Token()
	return err == io.EOF
}
func consumeObject(d *json.Decoder, depth int) bool {
	if depth > 12 {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		s, ok := key.(string)
		if err != nil || !ok || seen[s] {
			return false
		}
		seen[s] = true
		if !consumeValue(d, depth+1) {
			return false
		}
	}
	last, err := d.Token()
	return err == nil && last == json.Delim('}')
}
func consumeValue(d *json.Decoder, depth int) bool {
	if depth > 12 {
		return false
	}
	value, err := d.Token()
	if err != nil {
		return false
	}
	delimiter, ok := value.(json.Delim)
	if !ok {
		return true
	}
	switch delimiter {
	case '{':
		return consumeObject(d, depth+1)
	case '[':
		for d.More() {
			if !consumeValue(d, depth+1) {
				return false
			}
		}
		last, err := d.Token()
		return err == nil && last == json.Delim(']')
	default:
		return false
	}
}
