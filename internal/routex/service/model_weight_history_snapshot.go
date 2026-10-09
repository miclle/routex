package service

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

// Typed canonical reserialization makes logical snapshot integrity portable.
// Array order and every recorded field remain significant; object key order
// and whitespace do not. Missing/extra/duplicate fields never become defaults.
func decodeModelWeightSnapshot(raw []byte, count int, digest string) ([]ModelWeightRow, []byte, error) {
	if len(raw) > modelWeightSnapshotBudget || count < 0 || count > modelWeightBindingBudget {
		return nil, nil, modelWeightUnavailable
	}
	var objects []json.RawMessage
	if json.Unmarshal(raw, &objects) != nil || objects == nil || len(objects) != count {
		return nil, nil, modelWeightUnavailable
	}
	rows := make([]ModelWeightRow, 0, len(objects))
	names := []string{"binding_id", "binding_created_at", "provider_model_id", "provider_model_created_at", "connection_id", "connection_created_at", "provider_id", "provider_created_at", "protocol", "weight"}
	for _, object := range objects {
		dec := json.NewDecoder(bytes.NewReader(object))
		tok, err := dec.Token()
		if err != nil || tok != json.Delim('{') {
			return nil, nil, modelWeightUnavailable
		}
		fields := map[string]json.RawMessage{}
		for dec.More() {
			tok, err = dec.Token()
			key, ok := tok.(string)
			if err != nil || !ok || !slices.Contains(names, key) {
				return nil, nil, modelWeightUnavailable
			}
			if _, exists := fields[key]; exists {
				return nil, nil, modelWeightUnavailable
			}
			var value json.RawMessage
			if dec.Decode(&value) != nil {
				return nil, nil, modelWeightUnavailable
			}
			fields[key] = value
		}
		if _, err = dec.Token(); err != nil || dec.Decode(new(any)) != io.EOF || len(fields) != len(names) {
			return nil, nil, modelWeightUnavailable
		}
		for key, value := range fields {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !slices.Contains([]string{"binding_created_at", "provider_model_created_at", "connection_created_at", "provider_created_at"}, key) {
				return nil, nil, modelWeightUnavailable
			}
		}
		var row ModelWeightRow
		if json.Unmarshal(object, &row) != nil {
			return nil, nil, modelWeightUnavailable
		}
		for _, birth := range []*time.Time{row.BindingCreatedAt, row.ProviderModelCreatedAt, row.ConnectionCreatedAt, row.ProviderCreatedAt} {
			if birth != nil {
				if birth.IsZero() {
					return nil, nil, modelWeightUnavailable
				}
				*birth = birth.UTC()
			}
		}
		if len(rows) > 0 && rows[len(rows)-1].BindingID >= row.BindingID || !modelWeightRetainedID(row.BindingID, "bnd") || !modelWeightRetainedID(row.ProviderModelID, "pmd") || !modelWeightRetainedID(row.ConnectionID, "con") || !modelWeightRetainedID(row.ProviderID, "prv") || !entity.SupportedNativeProtocol(row.Protocol) || row.Weight < 0 || row.Weight > 100 {
			return nil, nil, modelWeightUnavailable
		}
		rows = append(rows, row)
	}
	canonical, canonicalDigest, err := modelWeightSnapshot(rows)
	if err != nil || canonicalDigest != digest {
		return nil, nil, modelWeightUnavailable
	}
	return rows, canonical, nil
}
