package pricing

import (
	"errors"
	"testing"
)

func fixtureSchedule() Schedule {
	s := Schedule{PriceID: "prc_fixture", ProviderModelID: "pmo_fixture", Protocol: "openai_chat", ContextThreshold: 200000}
	for _, tier := range []string{Base, Long} {
		for i, metric := range []string{Input, Output, CacheRead, CacheWrite} {
			amount := []string{"1", "2", "0.1", "1.25"}[i]
			if tier == Long {
				amount = []string{"2", "4", "0.2", "2.5"}[i]
			}
			s.Rates = append(s.Rates, Rate{Metric: metric, Tier: tier, Unit: Unit, Currency: "USD", Amount: amount, Enabled: true})
		}
	}
	return s
}
func TestPricingSemanticFixtures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		usage       Usage
		tier, total string
	}{
		{"ordinary", Usage{InputTokens: 1000000, OutputTokens: 100000}, Long, "2.4"},
		{"below", Usage{InputTokens: 199999}, Base, "0.199999"},
		{"equal", Usage{InputTokens: 200000}, Base, "0.2"},
		{"above with output", Usage{InputTokens: 200001, OutputTokens: 1}, Long, "0.400006"},
		{"cache inclusive threshold", Usage{InputTokens: 200001, OutputTokens: 1, CacheReadTokens: 100000, CacheWriteTokens: 100000}, Long, "0.270006"},
		{"all cache", Usage{InputTokens: 100000, CacheReadTokens: 100000}, Base, "0.01"},
		{"known zero", Usage{}, Base, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Calculate(fixtureSchedule(), FX{PlatformCurrency: "USD"}, tc.usage)
			if err != nil {
				t.Fatal(err)
			}
			if q.Total != tc.total || q.Tier != tc.tier {
				t.Fatalf("got %s %s want %s %s", q.Total, q.Tier, tc.total, tc.tier)
			}
		})
	}
}
func TestPricingMissingAndDisabled(t *testing.T) {
	s := fixtureSchedule()
	s.Rates = s.Rates[:1]
	if _, err := Calculate(s, FX{PlatformCurrency: "USD"}, Usage{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Calculate(s, FX{PlatformCurrency: "USD"}, Usage{OutputTokens: 1}); !errors.Is(err, ErrUnpriced) {
		t.Fatalf("missing rate: %v", err)
	}
	s.Rates[0].Amount = "0"
	q, err := Calculate(s, FX{PlatformCurrency: "USD"}, Usage{InputTokens: 1})
	if err != nil || q.Total != "0" {
		t.Fatalf("explicit free: %+v %v", q, err)
	}
	if _, err := Calculate(s, FX{PlatformCurrency: "CNY"}, Usage{InputTokens: 1}); !errors.Is(err, ErrUnpriced) {
		t.Fatalf("free still needs FX: %v", err)
	}
	s.Rates[0].Enabled = false
	if _, err := Calculate(s, FX{PlatformCurrency: "USD"}, Usage{}); !errors.Is(err, ErrUnpriced) {
		t.Fatalf("unconfigured zero: %v", err)
	}
}
func TestPricingExactFXAndSnapshot(t *testing.T) {
	s := fixtureSchedule()
	s.Rates = s.Rates[:1]
	s.ContextThreshold = 0
	s.Rates[0].Amount = "0.0000000000005"
	f := FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "1"}}
	q, err := Calculate(s, f, Usage{InputTokens: 1})
	if err != nil || q.Total != "0" {
		t.Fatalf("even rounding %+v %v", q, err)
	}
	s.Rates[0].Amount = "0.0000000000015"
	q, err = Calculate(s, f, Usage{InputTokens: 1})
	if err != nil || q.Total != "0.000000000000000002" {
		t.Fatalf("odd rounding %+v %v", q, err)
	}
	s.Rates[0].Amount = "100"
	f.Rates["USD"] = "7.123456789012345678"
	if q.Components[0].Rate.Amount != "0.0000000000015" || q.Components[0].ExchangeRate != "1" {
		t.Fatal("quote changed after source mutation")
	}
	q, err = Calculate(s, f, Usage{InputTokens: 1000000})
	if err != nil || q.Total != "712.3456789012345678" {
		t.Fatalf("exact FX %+v %v", q, err)
	}
}
func TestPricingRejectsUnsupportedOrAmbiguous(t *testing.T) {
	for _, value := range []string{"-1", "1e2", "01", "NaN", "1.", ".1", "1000000000000000000", "0.0000000000000000001"} {
		if _, err := Decimal(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	s := fixtureSchedule()
	f := FX{PlatformCurrency: "USD"}
	for _, u := range []Usage{{InputTokens: -1}, {InputTokens: 10, CacheReadTokens: 8, CacheWriteTokens: 3}, {OutputTokens: -1}} {
		if _, err := Calculate(s, f, u); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid usage: %v", err)
		}
	}
	s.ContextThreshold = 128000
	q, err := Calculate(s, f, Usage{InputTokens: 128001})
	if err != nil || q.Tier != Long {
		t.Fatalf("128k %+v %v", q, err)
	}
	s.Protocol = "native_audio"
	if _, err := Calculate(s, f, Usage{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("protocol %v", err)
	}
	s = fixtureSchedule()
	s.Rates[0].Unit = "REQUEST"
	if _, err := Calculate(s, f, Usage{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unit %v", err)
	}
}
