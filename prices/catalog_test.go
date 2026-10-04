package prices

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/miclle/routex/pkg/pricing"
)

const fixture = `{"schema_version":1,"models":[{"key":"example/model","provider_key":"example","model":"Native model","protocol":"openai_chat","context_threshold":0,"rates":[{"key":"example/model/input","metric":"INPUT_TOKEN","tier":"base","unit":"1M_TOKEN","currency":"USD","amount":"1.00","enabled":true}]}]}`

func TestEmbeddedSourceStartsWithoutInventedRates(t *testing.T) {
	source, err := Embedded()
	if err != nil || source == nil || len(source.Models()) != 0 || len(source.Digest()) != 64 {
		t.Fatalf("empty source: %v %v", source, err)
	}
	if SourceID != "routex-repository" {
		t.Fatal("source identity changed")
	}
}

func TestSourceDigestAndCopiesPreserveReviewedSnapshot(t *testing.T) {
	raw := []byte(fixture)
	source, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if source.Digest() != hex.EncodeToString(digest[:]) {
		t.Fatal("digest is not complete source bytes")
	}
	other, err := Parse(append([]byte(" \n"), raw...))
	if err != nil || source.Digest() == other.Digest() {
		t.Fatal("source byte changes must invalidate a preview")
	}
	raw[0] = '!'
	model, ok := source.Lookup("example/model")
	if !ok || model.Name != "Native model" || model.Rates[0].Value.Amount != "1" || model.Rates[0].Value.ID != "" {
		t.Fatal("source metadata or normalization changed", model)
	}
	model.Rates[0].Value.Amount = "999"
	models := source.Models()
	models[0].Key = "changed"
	models[0].Rates[0].Key = "changed"
	models[0].Rates[0].Value.Enabled = false
	retained, _ := source.Lookup("example/model")
	if retained.Rates[0].Value.Amount != "1" || !retained.Rates[0].Value.Enabled || retained.Rates[0].Key != "example/model/input" {
		t.Fatal("caller changed the immutable snapshot", retained)
	}
	if _, ok := source.Lookup("EXAMPLE/model"); ok {
		t.Fatal("source identities must be exact")
	}
	if _, ok := source.Lookup("Native model"); ok {
		t.Fatal("display name must not infer a mapping")
	}
}

func TestStrictCompleteSourceRejectsAmbiguity(t *testing.T) {
	for _, test := range []struct{ name, raw string }{
		{"empty", ""}, {"invalid UTF8", string([]byte{0xff})},
		{"NUL", fixture + "\x00"}, {"trailing", fixture + "{}"},
		{"array root", "[]"}, {"null root", "null"},
		{"version missing", `{"models":[]}`},
		{"version null", `{"schema_version":null,"models":[]}`},
		{"version string", `{"schema_version":"1","models":[]}`},
		{"unknown version", `{"schema_version":2,"models":[]}`},
		{"version decimal", `{"schema_version":1.0,"models":[]}`},
		{"duplicate root", `{"schema_version":1,"schema_version":1,"models":[]}`},
		{"case alias", `{"Schema_version":1,"models":[]}`},
		{"escaped duplicate", `{"schema_version":1,"schema_versio\u006e":1,"models":[]}`},
		{"unknown root", `{"schema_version":1,"models":[],"exchange_rates":{}}`},
		{"models null", `{"schema_version":1,"models":null}`},
		{"null model", `{"schema_version":1,"models":[null]}`},
		{"model duplicate", strings.Replace(fixture, `"provider_key":"example"`, `"provider_key":"example","provider_key":"example"`, 1)},
		{"model missing", strings.Replace(fixture, `"provider_key":"example",`, ``, 1)},
		{"model unknown", strings.Replace(fixture, `"provider_key":"example"`, `"provider_key":"example","capabilities":{}`, 1)},
		{"null name", strings.Replace(fixture, `"model":"Native model"`, `"model":null`, 1)},
		{"empty name", strings.Replace(fixture, `"Native model"`, `""`, 1)},
		{"padded name", strings.Replace(fixture, `"Native model"`, `" Native model"`, 1)},
		{"control name", strings.Replace(fixture, `"Native model"`, `"Native\nmodel"`, 1)},
		{"unsafe key", strings.Replace(fixture, `"key":"example/model"`, `"key":" example/model"`, 1)},
		{"protocol unsupported", strings.Replace(fixture, `"openai_chat"`, `"batch"`, 1)},
		{"threshold unsupported", strings.Replace(fixture, `"context_threshold":0`, `"context_threshold":1`, 1)},
		{"threshold string", strings.Replace(fixture, `"context_threshold":0`, `"context_threshold":"0"`, 1)},
		{"rates empty", strings.Replace(fixture, `[{"key":"example/model/input","metric":"INPUT_TOKEN","tier":"base","unit":"1M_TOKEN","currency":"USD","amount":"1.00","enabled":true}]`, `[]`, 1)},
		{"rate duplicate", strings.Replace(fixture, `"enabled":true`, `"enabled":true,"enabled":true`, 1)},
		{"rate missing", strings.Replace(fixture, `,"enabled":true`, ``, 1)},
		{"rate id forbidden", strings.Replace(fixture, `"enabled":true`, `"enabled":true,"id":"rat_local"`, 1)},
		{"enabled null", strings.Replace(fixture, `"enabled":true`, `"enabled":null`, 1)},
		{"enabled string", strings.Replace(fixture, `"enabled":true`, `"enabled":"true"`, 1)},
		{"metric unsupported", strings.Replace(fixture, `"INPUT_TOKEN"`, `"AUDIO_SECOND"`, 1)},
		{"unit unsupported", strings.Replace(fixture, `"1M_TOKEN"`, `"1_TOKEN"`, 1)},
		{"currency unsupported", strings.Replace(fixture, `"USD"`, `"XXX"`, 1)},
		{"amount numeric", strings.Replace(fixture, `"amount":"1.00"`, `"amount":1`, 1)},
		{"amount exponent", strings.Replace(fixture, `"1.00"`, `"1e-5"`, 1)},
		{"amount negative", strings.Replace(fixture, `"1.00"`, `"-1"`, 1)},
		{"long without threshold", strings.Replace(fixture, `"base"`, `"long_context"`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if source, err := Parse([]byte(test.raw)); !errors.Is(err, ErrInvalidCatalogue) || source != nil {
				t.Fatalf("ambiguous source accepted: source=%v err=%v", source, err)
			}
		})
	}
}

func TestSupportedRulesUseExistingExactPricingSemantics(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "openai_responses", "anthropic_messages", "gemini_generate_content"} {
		t.Run(protocol, func(t *testing.T) {
			source, err := Parse([]byte(strings.Replace(fixture, "openai_chat", protocol, 1)))
			if err != nil {
				t.Fatal(err)
			}
			model, _ := source.Lookup("example/model")
			if model.Protocol != protocol {
				t.Fatal(model.Protocol)
			}
		})
	}
	for _, amount := range []string{"0", "9007199254740993.123456789012345678", "0.000000000000000001"} {
		source, err := Parse([]byte(strings.Replace(fixture, "1.00", amount, 1)))
		if err != nil {
			t.Fatal(err)
		}
		model, _ := source.Lookup("example/model")
		if model.Rates[0].Value.Amount != amount {
			t.Fatal("decimal precision lost", model.Rates[0].Value.Amount)
		}
	}
	source, err := Parse([]byte(strings.Replace(fixture, `"enabled":true`, `"enabled":false`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	model, _ := source.Lookup("example/model")
	if model.Rates[0].Value.Enabled {
		t.Fatal("disabled price changed")
	}
	for _, item := range []struct{ metric, unit string }{
		{pricing.Output, pricing.Unit}, {pricing.CacheRead, pricing.Unit}, {pricing.CacheWrite, pricing.Unit},
		{pricing.ImageInput, pricing.ImageUnit}, {pricing.PDFInput, pricing.PDFUnit},
	} {
		raw := strings.Replace(strings.Replace(fixture, "INPUT_TOKEN", item.metric, 1), "1M_TOKEN", item.unit, 1)
		if _, err := Parse([]byte(raw)); err != nil {
			t.Fatal(item, err)
		}
	}
	for _, threshold := range []string{"128000", "200000"} {
		raw := strings.Replace(strings.Replace(fixture, `"context_threshold":0`, `"context_threshold":`+threshold, 1), `"base"`, `"long_context"`, 1)
		if _, err := Parse([]byte(raw)); err != nil {
			t.Fatal(threshold, err)
		}
	}
}

func TestDuplicateSourceAndBillingKeysRejectWholeSnapshot(t *testing.T) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixture), &envelope); err != nil {
		t.Fatal(err)
	}
	var models []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["models"], &models); err != nil {
		t.Fatal(err)
	}
	model := models[0]
	encode := func(models []map[string]json.RawMessage) []byte {
		data, err := json.Marshal(map[string]any{"schema_version": 1, "models": models})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if _, err := Parse(encode([]map[string]json.RawMessage{model, model})); err == nil {
		t.Fatal("duplicate model accepted")
	}
	other := make(map[string]json.RawMessage)
	for key, value := range model {
		other[key] = value
	}
	other["key"] = json.RawMessage(`"example/other"`)
	if _, err := Parse(encode([]map[string]json.RawMessage{model, other})); err == nil {
		t.Fatal("duplicate global rate key accepted")
	}
	var rates []map[string]json.RawMessage
	if err := json.Unmarshal(model["rates"], &rates); err != nil {
		t.Fatal(err)
	}
	duplicate := make(map[string]json.RawMessage)
	for key, value := range rates[0] {
		duplicate[key] = value
	}
	duplicate["key"] = json.RawMessage(`"example/other/input"`)
	model["rates"], _ = json.Marshal([]map[string]json.RawMessage{rates[0], duplicate})
	if _, err := Parse(encode([]map[string]json.RawMessage{model})); err == nil {
		t.Fatal("duplicate metric/tier accepted")
	}
}

func TestCompleteSourceBounds(t *testing.T) {
	bounded := []byte(fixture + strings.Repeat(" ", MaxBytes-len(fixture)))
	if _, err := Parse(bounded); err != nil {
		t.Fatal("exact bound rejected", err)
	}
	if _, err := Parse(append(bounded, ' ')); err == nil {
		t.Fatal("byte overflow accepted")
	}
	if _, err := Parse([]byte(`{"schema_version":1,"models":` + strings.Repeat("[", 10000) + strings.Repeat("]", 10000) + `}`)); err == nil {
		t.Fatal("deeply nested malformed model list accepted")
	}
	if _, err := Parse([]byte(`{"schema_version":1,"models":[` + strings.Repeat(`{},`, MaxModels) + `{}]}`)); err == nil {
		t.Fatal("model count overflow accepted")
	}
}

func TestSourceUsesNativeCacheTierAndMediaChargeRules(t *testing.T) {
	rates := []map[string]any{}
	for _, item := range []struct{ metric, tier, unit, amount string }{
		{pricing.Input, pricing.Base, pricing.Unit, "1"},
		{pricing.Output, pricing.Base, pricing.Unit, "2"},
		{pricing.CacheRead, pricing.Base, pricing.Unit, "3"},
		{pricing.CacheWrite, pricing.Base, pricing.Unit, "4"},
		{pricing.Input, pricing.Long, pricing.Unit, "10"},
		{pricing.Output, pricing.Long, pricing.Unit, "20"},
		{pricing.CacheRead, pricing.Long, pricing.Unit, "30"},
		{pricing.CacheWrite, pricing.Long, pricing.Unit, "40"},
		{pricing.ImageInput, pricing.Base, pricing.ImageUnit, "5"},
		{pricing.PDFInput, pricing.Base, pricing.PDFUnit, "6"},
	} {
		rates = append(rates, map[string]any{"key": "example/" + item.metric + "/" + item.tier, "metric": item.metric, "tier": item.tier, "unit": item.unit, "currency": "USD", "amount": item.amount, "enabled": true})
	}
	raw, err := json.Marshal(map[string]any{"schema_version": 1, "models": []map[string]any{{"key": "example/model", "provider_key": "example", "model": "Native model", "protocol": "openai_chat", "context_threshold": 128000, "rates": rates}}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	model, _ := source.Lookup("example/model")
	schedule := pricing.Schedule{Protocol: model.Protocol, ContextThreshold: model.ContextThreshold}
	for _, rate := range model.Rates {
		schedule.Rates = append(schedule.Rates, rate.Value)
	}
	for _, test := range []struct {
		name        string
		usage       pricing.Usage
		tier, total string
	}{
		{"cache does not double charge", pricing.Usage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 2, ImageInputs: 1, PDFInputs: 1}, pricing.Base, "11.000026"},
		{"exact boundary remains base", pricing.Usage{InputTokens: 128000, OutputTokens: 2, ImageInputs: 1, PDFInputs: 1}, pricing.Base, "11.128004"},
		{"above boundary selects whole long tier", pricing.Usage{InputTokens: 128001, OutputTokens: 2, ImageInputs: 1, PDFInputs: 1}, pricing.Long, "12.28005"},
	} {
		t.Run(test.name, func(t *testing.T) {
			quote, err := pricing.Calculate(schedule, pricing.FX{PlatformCurrency: "USD", Rates: map[string]string{}}, test.usage)
			if err != nil || quote == nil || quote.Total != test.total || quote.Tier != test.tier || quote.Adapter != pricing.MultimodalAdapter {
				t.Fatalf("source changed established billing: quote=%v err=%v", quote, err)
			}
		})
	}
}
