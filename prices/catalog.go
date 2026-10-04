// Package prices reads the versioned RouteX repository price source.
package prices

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/pkg/pricing"
)

const (
	SourceID  = "routex-repository"
	MaxBytes  = 256 << 10
	MaxModels = 1000
)

var ErrInvalidCatalogue = errors.New("invalid repository price catalogue")
var sourceKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)

//go:embed catalog.json
var embedded []byte

// Rate associates an exact source identity with a supported pricing rule.
type Rate struct {
	Key   string
	Value pricing.Rate
}

// Model contains source metadata, never a deployment-specific model identity.
type Model struct {
	Key              string
	ProviderKey      string
	Name             string
	Protocol         string
	ContextThreshold int64
	Rates            []Rate
}

// Snapshot retains only validated data and the digest of the complete source.
// Accessors return copies so callers cannot change a reviewed snapshot.
type Snapshot struct {
	digest string
	models []Model
}

func Embedded() (*Snapshot, error) { return Parse(embedded) }
func (s *Snapshot) Digest() string { return s.digest }
func (s *Snapshot) Models() []Model {
	result := slices.Clone(s.models)
	for i := range result {
		result[i].Rates = slices.Clone(result[i].Rates)
	}
	return result
}
func (s *Snapshot) Lookup(key string) (Model, bool) {
	for _, model := range s.models {
		if model.Key == key {
			model.Rates = slices.Clone(model.Rates)
			return model, true
		}
	}
	return Model{}, false
}

// Parse accepts one bounded complete source. It never resolves names to local
// models, fetches a URL, evaluates code or changes a current price catalogue.
func Parse(raw []byte) (*Snapshot, error) {
	if len(raw) == 0 || len(raw) > MaxBytes || !utf8.Valid(raw) || bytes.ContainsRune(raw, 0) {
		return nil, ErrInvalidCatalogue
	}
	fields, err := object(raw, "schema_version", "models")
	if err != nil {
		return nil, err
	}
	var version int
	if json.Unmarshal(fields["schema_version"], &version) != nil || version != 1 {
		return nil, ErrInvalidCatalogue
	}
	var items []json.RawMessage
	if json.Unmarshal(fields["models"], &items) != nil || len(items) > MaxModels {
		return nil, ErrInvalidCatalogue
	}
	result := &Snapshot{models: make([]Model, 0, len(items))}
	modelKeys, rateKeys := map[string]bool{}, map[string]bool{}
	for _, item := range items {
		model, err := parseModel(item)
		if err != nil || modelKeys[model.Key] {
			return nil, ErrInvalidCatalogue
		}
		modelKeys[model.Key] = true
		for _, rate := range model.Rates {
			if rateKeys[rate.Key] {
				return nil, ErrInvalidCatalogue
			}
			rateKeys[rate.Key] = true
		}
		result.models = append(result.models, model)
	}
	digest := sha256.Sum256(raw)
	result.digest = hex.EncodeToString(digest[:])
	return result, nil
}

func parseModel(raw []byte) (Model, error) {
	fields, err := object(raw, "key", "provider_key", "model", "protocol", "context_threshold", "rates")
	if err != nil {
		return Model{}, err
	}
	var model Model
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"key", &model.Key}, {"provider_key", &model.ProviderKey},
		{"model", &model.Name}, {"protocol", &model.Protocol},
	} {
		if json.Unmarshal(fields[field.name], field.value) != nil {
			return Model{}, ErrInvalidCatalogue
		}
	}
	if !sourceKey.MatchString(model.Key) || !sourceKey.MatchString(model.ProviderKey) || !safeName(model.Name) {
		return Model{}, ErrInvalidCatalogue
	}
	if json.Unmarshal(fields["context_threshold"], &model.ContextThreshold) != nil {
		return Model{}, ErrInvalidCatalogue
	}
	var items []json.RawMessage
	if json.Unmarshal(fields["rates"], &items) != nil || len(items) == 0 || len(items) > pricing.MaxRates {
		return Model{}, ErrInvalidCatalogue
	}
	model.Rates = make([]Rate, 0, len(items))
	schedule := pricing.Schedule{Protocol: model.Protocol, ContextThreshold: model.ContextThreshold, Rates: make([]pricing.Rate, 0, len(items))}
	for _, item := range items {
		rate, err := parseRate(item)
		if err != nil {
			return Model{}, err
		}
		model.Rates = append(model.Rates, rate)
		schedule.Rates = append(schedule.Rates, rate.Value)
	}
	if pricing.ValidateSchedule(schedule) != nil {
		return Model{}, ErrInvalidCatalogue
	}
	return model, nil
}

func parseRate(raw []byte) (Rate, error) {
	fields, err := object(raw, "key", "metric", "tier", "unit", "currency", "amount", "enabled")
	if err != nil {
		return Rate{}, err
	}
	var rate Rate
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"key", &rate.Key}, {"metric", &rate.Value.Metric}, {"tier", &rate.Value.Tier},
		{"unit", &rate.Value.Unit}, {"currency", &rate.Value.Currency}, {"amount", &rate.Value.Amount},
	} {
		if json.Unmarshal(fields[field.name], field.value) != nil {
			return Rate{}, ErrInvalidCatalogue
		}
	}
	if !sourceKey.MatchString(rate.Key) || json.Unmarshal(fields["enabled"], &rate.Value.Enabled) != nil || pricing.ValidateRate(rate.Value) != nil {
		return Rate{}, ErrInvalidCatalogue
	}
	// Normalize with the same exact-decimal rules as ordinary price maintenance.
	rate.Value.Amount, _ = pricing.Decimal(rate.Value.Amount)
	return rate, nil
}

func safeName(value string) bool {
	return value != "" && len(value) <= 256 && value == strings.TrimSpace(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// object requires exact field spelling and presence. encoding/json's struct
// matching alone permits case aliases, repeated keys and omitted zero values.
func object(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrInvalidCatalogue
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !slices.Contains(allowed, key) || fields[key] != nil {
			return nil, ErrInvalidCatalogue
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrInvalidCatalogue
		}
		fields[key] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(fields) != len(allowed) {
		return nil, ErrInvalidCatalogue
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidCatalogue
	}
	return fields, nil
}
