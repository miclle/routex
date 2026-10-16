package eventqueue

import (
	"errors"
	"reflect"
	"testing"
)

func TestSharedQuotaCounterUsageExactDecimalAndIndependentDimensions(t *testing.T) {
	counter := QuotaCounter{
		TokensUsed: 7, TokensHeld: 11, TokensUnknown: 2, MoneyUnknown: 3,
		MoneyUsed: map[string]string{
			"USD": "900719925474099312345678901234567890",
			"EUR": "1",
			"CNY": "0",
		},
		MoneyHeld: map[string]string{"USD": "1000000000000000001", "JPY": "1000000000000000000000"},
	}
	want := QuotaUsage{
		TokensUsed: 7, TokensHeld: 11, TokensUnknown: 2, MoneyUnknown: 3,
		MoneyUsed: map[string]string{"USD": "900719925474099312.34567890123456789", "EUR": "0.000000000000000001", "CNY": "0"},
		MoneyHeld: map[string]string{"USD": "1.000000000000000001", "JPY": "1000"},
	}
	got, err := QuotaCounterUsage(counter)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("exact independent usage: got=%+v err=%v", got, err)
	}
	got.MoneyUsed["USD"] = "changed"
	got.MoneyHeld["JPY"] = "changed"
	if counter.MoneyUsed["USD"] != "900719925474099312345678901234567890" || counter.MoneyHeld["JPY"] != "1000000000000000000000" {
		t.Fatal("public usage aliases persisted integer units")
	}
	counter.MoneyUsed["EUR"] = "999"
	counter.MoneyHeld["USD"] = "999"
	if got.MoneyUsed["EUR"] != want.MoneyUsed["EUR"] || got.MoneyHeld["USD"] != want.MoneyHeld["USD"] {
		t.Fatal("counter mutation rewrites the returned snapshot")
	}
}

func TestSharedQuotaCounterUsageRejectsMalformedWithoutPartialValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*QuotaCounter)
	}{
		{"nil_used", func(c *QuotaCounter) { c.MoneyUsed = nil }},
		{"nil_held", func(c *QuotaCounter) { c.MoneyHeld = nil }},
		{"negative_tokens", func(c *QuotaCounter) { c.TokensUsed = -1 }},
		{"negative_token_holds", func(c *QuotaCounter) { c.TokensHeld = -1 }},
		{"negative_token_unknown", func(c *QuotaCounter) { c.TokensUnknown = -1 }},
		{"negative_money_unknown", func(c *QuotaCounter) { c.MoneyUnknown = -1 }},
		{"decimal_units", func(c *QuotaCounter) { c.MoneyUsed["USD"] = "1.2" }},
		{"negative_units", func(c *QuotaCounter) { c.MoneyHeld["USD"] = "-1" }},
		{"padded_units", func(c *QuotaCounter) { c.MoneyUsed["USD"] = "01" }},
		{"signed_units", func(c *QuotaCounter) { c.MoneyHeld["USD"] = "+1" }},
		{"lowercase_currency", func(c *QuotaCounter) { c.MoneyUsed["usd"] = "1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			counter := QuotaCounter{TokensUsed: 7, MoneyUsed: map[string]string{"USD": "1"}, MoneyHeld: map[string]string{"USD": "2"}}
			test.change(&counter)
			if got, err := QuotaCounterUsage(counter); !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(got, QuotaUsage{}) {
				t.Fatalf("invalid counter exposed partial usage: got=%+v err=%v", got, err)
			}
		})
	}
}

func TestSharedQuotaCounterUsageKnownZeroAndUnknownRemainSeparate(t *testing.T) {
	counter := QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
	zero, err := QuotaCounterUsage(counter)
	if err != nil || zero.MoneyUsed == nil || zero.MoneyHeld == nil || zero.TokensUnknown != 0 || zero.MoneyUnknown != 0 {
		t.Fatal("valid recorded zero lost its empty maps", err)
	}
	counter.TokensUnknown, counter.MoneyUnknown = 4, 5
	unknown, err := QuotaCounterUsage(counter)
	if err != nil || unknown.TokensUnknown != 4 || unknown.MoneyUnknown != 5 || unknown.TokensUsed != 0 || unknown.TokensHeld != 0 || len(unknown.MoneyUsed) != 0 || len(unknown.MoneyHeld) != 0 {
		t.Fatal("unknown dimensions were inferred as consumption or bounded holds", err)
	}
}
