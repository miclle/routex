package eventqueue

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func quotaCounterRulesEmpty() QuotaCounter {
	return QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
}

func TestQuotaCounterRulesIndependentTerminalDimensions(t *testing.T) {
	tokens, amount, actualTokens, actualAmount := int64(100), "1.25", int64(4), "0.000000000000000001"
	bound := QuotaBound{Tokens: &tokens, Money: &amount, Currency: "USD"}
	for _, test := range []struct {
		name   string
		actual QuotaSettlement
		want   QuotaCounter
	}{
		{"unknown", QuotaSettlement{}, QuotaCounter{TokensHeld: 100, MoneyHeld: map[string]string{"USD": "1250000000000000000"}, MoneyUsed: map[string]string{}}},
		{"tokens", QuotaSettlement{Tokens: &actualTokens}, QuotaCounter{TokensUsed: 4, MoneyHeld: map[string]string{"USD": "1250000000000000000"}, MoneyUsed: map[string]string{}}},
		{"money", QuotaSettlement{Money: &actualAmount, Currency: "USD"}, QuotaCounter{TokensHeld: 100, MoneyHeld: map[string]string{}, MoneyUsed: map[string]string{"USD": "1"}}},
		{"both", QuotaSettlement{Tokens: &actualTokens, Money: &actualAmount, Currency: "USD"}, QuotaCounter{TokensUsed: 4, MoneyHeld: map[string]string{}, MoneyUsed: map[string]string{"USD": "1"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := quotaCounterRulesEmpty()
			active := QuotaCounterContribution{Bound: bound}
			reserved, err := QuotaCounterDelta(original, active, 1)
			if err != nil {
				t.Fatal(err)
			}
			removed, err := QuotaCounterDelta(reserved, active, -1)
			if err != nil || !reflect.DeepEqual(removed, original) {
				t.Fatalf("remove original hold: %+v, %v", removed, err)
			}
			terminal := QuotaCounterContribution{Bound: bound, Actual: test.actual, Settled: true}
			settled, err := QuotaCounterDelta(removed, terminal, 1)
			if err != nil || !reflect.DeepEqual(settled, test.want) {
				t.Fatalf("terminal contribution: %+v, %v; want %+v", settled, err, test.want)
			}
			pruned, err := QuotaCounterDelta(settled, terminal, -1)
			if err != nil || !reflect.DeepEqual(pruned, original) {
				t.Fatalf("expire terminal: %+v, %v", pruned, err)
			}
			if len(original.MoneyUsed) != 0 || len(original.MoneyHeld) != 0 || reserved.MoneyHeld["USD"] != "1250000000000000000" {
				t.Fatal("a later delta mutated an earlier snapshot")
			}
		})
	}
}

func TestQuotaCounterRulesUnknownAndKnownZero(t *testing.T) {
	original := quotaCounterRulesEmpty()
	unknown := QuotaCounterContribution{}
	active, err := QuotaCounterDelta(original, unknown, 1)
	if err != nil || active.TokensUnknown != 1 || active.MoneyUnknown != 1 {
		t.Fatalf("unknown reserve: %+v, %v", active, err)
	}
	zero, amount := int64(0), "0"
	known := QuotaCounterContribution{Actual: QuotaSettlement{Tokens: &zero, Money: &amount, Currency: "USD"}, Settled: true}
	removed, err := QuotaCounterDelta(active, unknown, -1)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := QuotaCounterDelta(removed, known, 1)
	if err != nil || !reflect.DeepEqual(settled, original) {
		t.Fatalf("known zero leaves no economic hold: %+v, %v", settled, err)
	}
	stillUnknown, err := QuotaCounterDelta(original, QuotaCounterContribution{Settled: true}, 1)
	if err != nil || stillUnknown.TokensUnknown != 1 || stillUnknown.MoneyUnknown != 1 {
		t.Fatalf("terminal status must not invent zero: %+v, %v", stillUnknown, err)
	}
}

func TestQuotaCounterRulesFailureDoesNotMutate(t *testing.T) {
	zero, one, amount := int64(0), int64(1), "1"
	for _, test := range []struct {
		name         string
		counter      QuotaCounter
		contribution QuotaCounterContribution
		sign         int64
	}{
		{"token_overflow", QuotaCounter{TokensHeld: math.MaxInt64, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, QuotaCounterContribution{Bound: QuotaBound{Tokens: &one}}, 1},
		{"unknown_overflow", QuotaCounter{TokensUnknown: math.MaxInt64, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, QuotaCounterContribution{}, 1},
		{"money_underflow_after_tokens", QuotaCounter{TokensHeld: 1, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "1"}}, QuotaCounterContribution{Bound: QuotaBound{Tokens: &one, Money: &amount, Currency: "USD"}}, -1},
		{"unknown_underflow", quotaCounterRulesEmpty(), QuotaCounterContribution{}, -1},
		{"invalid_sign", quotaCounterRulesEmpty(), QuotaCounterContribution{Bound: QuotaBound{Tokens: &zero}}, 0},
		{"invalid_units", QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "01"}}, QuotaCounterContribution{}, 1},
		{"negative_units", QuotaCounter{MoneyUsed: map[string]string{"USD": "-1"}, MoneyHeld: map[string]string{}}, QuotaCounterContribution{}, 1},
		{"missing_map", QuotaCounter{}, QuotaCounterContribution{}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := test.counter
			before.MoneyUsed = copyQuotaCounterMoney(test.counter.MoneyUsed)
			before.MoneyHeld = copyQuotaCounterMoney(test.counter.MoneyHeld)
			if test.counter.MoneyUsed == nil {
				before.MoneyUsed = nil
			}
			if test.counter.MoneyHeld == nil {
				before.MoneyHeld = nil
			}
			got, err := QuotaCounterDelta(test.counter, test.contribution, test.sign)
			if err == nil || !reflect.DeepEqual(got, before) || !reflect.DeepEqual(test.counter, before) {
				t.Fatalf("failure changed input/result: %+v, %v; before %+v", got, err, before)
			}
		})
	}
}

func TestQuotaCounterRulesAccumulatedMoneyPrecision(t *testing.T) {
	units := "1" + strings.Repeat("0", 100)
	original := QuotaCounter{MoneyUsed: map[string]string{"USD": units}, MoneyHeld: map[string]string{}}
	tokens, amount := int64(0), "0.000000000000000001"
	contribution := QuotaCounterContribution{Actual: QuotaSettlement{Tokens: &tokens, Money: &amount, Currency: "USD"}, Settled: true}
	got, err := QuotaCounterDelta(original, contribution, 1)
	if err != nil || got.MoneyUsed["USD"] != "1"+strings.Repeat("0", 99)+"1" || original.MoneyUsed["USD"] != units {
		t.Fatalf("exact unbounded accumulator: %+v, %v", got, err)
	}
	back, err := QuotaCounterDelta(got, contribution, -1)
	if err != nil || !reflect.DeepEqual(back, original) {
		t.Fatalf("exact removal: %+v, %v", back, err)
	}
}
