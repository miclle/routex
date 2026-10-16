package eventqueue

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQuotaRulesTokenDeltaBoundaries(t *testing.T) {
	for _, test := range []struct {
		name                string
		total, amount, sign int64
		want                int64
		err                 error
	}{
		{"known_zero", 0, 0, 1, 0, nil},
		{"maximum", math.MaxInt64 - 1, 1, 1, math.MaxInt64, nil},
		{"overflow", math.MaxInt64, 1, 1, math.MaxInt64, ErrInvalid},
		{"release_exact", 7, 7, -1, 0, nil},
		{"underflow", 7, 8, -1, 7, ErrInvalid},
		{"negative_total", -1, 1, 1, -1, ErrInvalid},
		{"negative_amount", 7, -1, 1, 7, ErrInvalid},
		{"invalid_sign", 7, 1, 0, 7, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := QuotaTokenDelta(test.total, test.amount, test.sign)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatalf("delta = %d, %v; want %d, %v", got, err, test.want, test.err)
			}
		})
	}
}

func TestQuotaRulesMoneyPrecisionAndAccumulation(t *testing.T) {
	amount := strings.Repeat("9", 60) + ".999999999999999999"
	units, err := QuotaMoneyDelta("0", "USD", amount, 1)
	if err != nil || units != strings.Repeat("9", 78) {
		t.Fatalf("full precision amount = %q, %v", units, err)
	}
	second, err := QuotaMoneyDelta(units, "USD", amount, 1)
	want := "1" + strings.Repeat("9", 60) + ".999999999999999998"
	if value, renderErr := QuotaMoneyValue(second); err != nil || renderErr != nil || value != want {
		t.Fatalf("accumulated amount = %q, %v, %v; want %q", value, err, renderErr, want)
	}
	remaining, err := QuotaMoneyDelta(second, "USD", amount, -1)
	if err != nil || remaining != units {
		t.Fatalf("exact release = %q, %v", remaining, err)
	}
	zero, err := QuotaMoneyDelta(remaining, "USD", amount, -1)
	if value, renderErr := QuotaMoneyValue(zero); err != nil || renderErr != nil || value != "0" {
		t.Fatalf("zero = %q, %v, %v", value, err, renderErr)
	}
	if smallest, err := QuotaMoneyDelta("0", "USD", "0.000000000000000001", 1); err != nil || smallest != "1" {
		t.Fatalf("smallest amount = %q, %v", smallest, err)
	}
	if value, err := QuotaMoneyValue("100000000000000000"); err != nil || value != "0.1" {
		t.Fatalf("fraction rendering = %q, %v", value, err)
	}
}

func TestQuotaRulesMoneyFailurePreservesAccumulator(t *testing.T) {
	for _, test := range []struct {
		name, units, currency, amount string
		sign                          int64
	}{
		{"underflow", "1", "USD", "0.000000000000000002", -1},
		{"negative_amount", "1", "USD", "-1", 1},
		{"leading_zero_amount", "1", "USD", "01", 1},
		{"too_many_integer_digits", "1", "USD", "1" + strings.Repeat("0", 60), 1},
		{"too_many_fraction_digits", "1", "USD", "0." + strings.Repeat("1", 19), 1},
		{"fraction_units", "1.0", "USD", "1", 1},
		{"noncanonical_units", "01", "USD", "1", 1},
		{"negative_units", "-1", "USD", "1", 1},
		{"empty_units", "", "USD", "1", 1},
		{"lowercase_currency", "1", "usd", "1", 1},
		{"invalid_sign", "1", "USD", "1", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := QuotaMoneyDelta(test.units, test.currency, test.amount, test.sign)
			if got != test.units || !errors.Is(err, ErrInvalid) {
				t.Fatalf("failed delta changed accumulator: %q, %v", got, err)
			}
		})
	}
	for _, raw := range []string{"", "-1", "01", "+1", "1.0"} {
		if _, err := QuotaMoneyValue(raw); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted noncanonical stored units %q: %v", raw, err)
		}
	}
}

func TestQuotaRulesBoundSettlementAndRecovery(t *testing.T) {
	bound := QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("1.25"), Currency: "USD", Revision: "bound_v1"}
	for _, test := range []struct {
		name     string
		recovery QuotaSettlement
		err      error
	}{
		{"unknown", QuotaSettlement{}, nil},
		{"known_zero", QuotaSettlement{Tokens: limitPtr(0), Money: moneyPtr("0"), Currency: "USD"}, nil},
		{"token_only", QuotaSettlement{Tokens: limitPtr(10)}, nil},
		{"money_only", QuotaSettlement{Money: moneyPtr("1.25"), Currency: "USD"}, nil},
		{"token_overrun", QuotaSettlement{Tokens: limitPtr(11)}, ErrQuotaBound},
		{"money_overrun", QuotaSettlement{Money: moneyPtr("1.250000000000000001"), Currency: "USD"}, ErrQuotaBound},
		{"other_currency", QuotaSettlement{Money: moneyPtr("1"), Currency: "EUR"}, ErrQuotaCurrency},
		{"negative_tokens", QuotaSettlement{Tokens: limitPtr(-1)}, ErrInvalid},
		{"invalid_money", QuotaSettlement{Money: moneyPtr("bad"), Currency: "USD"}, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := cloneReceipt(QuotaReceipt{Bound: bound, Recovery: test.recovery})
			if err := ValidateQuotaRecovery(bound, test.recovery); !errors.Is(err, test.err) {
				t.Fatalf("recovery error = %v; want %v", err, test.err)
			}
			if !reflect.DeepEqual(before.Bound, bound) || !reflect.DeepEqual(before.Recovery, test.recovery) {
				t.Fatal("validation mutated caller proof")
			}
		})
	}
	for _, invalid := range []QuotaBound{
		{Tokens: limitPtr(-1)},
		{Money: moneyPtr("bad"), Currency: "USD"},
		{Money: moneyPtr("0"), Currency: "usd"},
		{Revision: "not a key"},
		{PriceRevision: strings.Repeat("a", 65)},
		{BasisDigest: strings.Repeat("A", 64)},
	} {
		if err := ValidateQuotaBound(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid bound accepted: %+v, %v", invalid, err)
		}
		if err := ValidateQuotaRecovery(invalid, QuotaSettlement{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("recovery accepted invalid bound: %+v, %v", invalid, err)
		}
	}
	unknown := QuotaSettlement{}
	if err := ValidateQuotaSettlement(unknown); err != nil || unknown.Tokens != nil || unknown.Money != nil {
		t.Fatal("unknown settlement became known", err)
	}
	// A structurally valid unknown bound does not establish finite eligibility.
	if err := ValidateQuotaBound(QuotaBound{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuotaRecovery(QuotaBound{}, QuotaSettlement{Tokens: limitPtr(1)}); err != nil {
		t.Fatal("unknown bound was invented as zero", err)
	}
}

func TestQuotaRulesCalendarMonthBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, zone string
		instant    time.Time
		start, end time.Time
	}{
		{"spring_dst", "America/New_York", time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 5, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 4, 0, 0, 0, time.UTC)},
		{"fall_dst", "America/New_York", time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)},
		{"leap_month", "UTC", time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC), time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"exact_next_month", "UTC", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(test.name, func(t *testing.T) {
			start, end, err := QuotaMonthBounds(test.instant.UnixNano(), test.zone)
			if err != nil || start != test.start.UnixNano() || end != test.end.UnixNano() {
				t.Fatalf("month = %d..%d, %v; want %s..%s", start, end, err, test.start, test.end)
			}
		})
	}
	if _, _, err := QuotaMonthBounds(quotaTestTime.UnixNano(), "Not/A_Zone"); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid timezone accepted", err)
	}
	if _, _, err := QuotaMonthBounds(time.Date(1969, 12, 15, 0, 0, 0, 0, time.UTC).UnixNano(), "UTC"); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative calendar month accepted", err)
	}
}

func TestQuotaRulesLocalQueueParity(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	policy := quotaPolicy("key_rules")
	policy.Tokens5H = limitPtr(10)
	policy.MoneyMonth = moneyPtr("5")
	policy.Currency = "USD"
	bound := QuotaBound{Tokens: limitPtr(4), Money: moneyPtr("2.5"), Currency: "USD", Revision: "bound_rules"}
	if err := ValidateQuotaBound(bound); err != nil {
		t.Fatal(err)
	}
	requireReserve(t, q, "rules", []QuotaLimit{policy}, bound, quotaTestTime)
	wrongCurrency := QuotaSettlement{Money: moneyPtr("1"), Currency: "EUR"}
	if err := ValidateQuotaRecovery(bound, wrongCurrency); !errors.Is(err, ErrQuotaCurrency) {
		t.Fatal(err)
	}
	if err := q.UpdatePendingQuota("rules", []byte("wrong-currency"), wrongCurrency); !errors.Is(err, ErrQuotaCurrency) {
		t.Fatal("local recovery diverged from pure rule", err)
	}
	recovery := QuotaSettlement{Tokens: limitPtr(1), Money: moneyPtr("0.25"), Currency: "USD"}
	if err := ValidateQuotaRecovery(bound, recovery); err != nil {
		t.Fatal(err)
	}
	if err := q.UpdatePendingQuota("rules", []byte("checkpoint"), recovery); err != nil {
		t.Fatal(err)
	}
	actual := QuotaSettlement{Tokens: limitPtr(1)}
	if err := ValidateQuotaSettlement(actual); err != nil {
		t.Fatal(err)
	}
	receipt := requireComplete(t, q, "rules", actual, quotaTestTime)
	start, end, err := QuotaMonthBounds(quotaTestTime.UnixNano(), "UTC")
	if err != nil || receipt.MonthStart != start || receipt.MonthEnd != end {
		t.Fatal("local calendar attribution diverged", receipt, err)
	}
	usage := requireUsage(t, q, policy.Account, quotaTestTime)
	if usage.Active.TokensHeld != 0 || usage.Active.MoneyHeld["USD"] != "" || usage.FiveHours.TokensUsed != 1 || usage.FiveHours.TokensUnknown != 0 || usage.Month.MoneyHeld["USD"] != "2.5" || usage.Month.MoneyUnknown != 0 {
		t.Fatalf("independent settlement changed: %+v", usage)
	}
	units, err := QuotaMoneyDelta("0", "USD", *bound.Money, 1)
	if value, renderErr := QuotaMoneyValue(units); err != nil || renderErr != nil || value != usage.Month.MoneyHeld["USD"] {
		t.Fatal("local money projection diverged", value, err, renderErr)
	}
}

func TestQuotaRulesLocalUnknownAndZeroRemainDistinct(t *testing.T) {
	for _, known := range []bool{false, true} {
		name := "unknown"
		if known {
			name = "known_zero"
		}
		t.Run(name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			policy := quotaPolicy("key_unknown_rules")
			requireReserve(t, q, "unknown_rules", []QuotaLimit{policy}, QuotaBound{}, quotaTestTime)
			actual := QuotaSettlement{}
			if known {
				actual = QuotaSettlement{Tokens: limitPtr(0), Money: moneyPtr("0"), Currency: "USD"}
			}
			if err := ValidateQuotaSettlement(actual); err != nil {
				t.Fatal(err)
			}
			requireComplete(t, q, "unknown_rules", actual, quotaTestTime)
			usage := requireUsage(t, q, policy.Account, quotaTestTime)
			wantUnknown := int64(1)
			if known {
				wantUnknown = 0
			}
			if usage.FiveHours.TokensUsed != 0 || usage.FiveHours.TokensHeld != 0 || usage.FiveHours.TokensUnknown != wantUnknown || usage.Month.MoneyUnknown != wantUnknown {
				t.Fatalf("unknown and known zero conflated: %+v", usage)
			}
		})
	}
}
