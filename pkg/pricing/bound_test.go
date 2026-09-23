package pricing

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"
)

func boundSchedule() Schedule {
	result := Schedule{ProviderModelID: "pmd_bound", Protocol: "openai_chat", ContextThreshold: 128000}
	for _, tier := range []string{Base, Long} {
		for index, metric := range []string{Input, Output, CacheRead, CacheWrite} {
			result.Rates = append(result.Rates, Rate{Metric: metric, Tier: tier, Unit: Unit, Currency: "USD", Amount: []string{"1", "2", "0.25", "3"}[index], Enabled: true})
		}
	}
	return result
}
func TestReservationBoundDominatesRoundedActual(t *testing.T) {
	random := rand.New(rand.NewPCG(19, 37))
	for iteration := 0; iteration < 500; iteration++ {
		schedule := boundSchedule()
		for index := range schedule.Rates {
			schedule.Rates[index].Amount = fmt.Sprintf("%d.%018d", random.Int64N(8), random.Int64N(1000000000000000000))
		}
		fx := FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "7.123456789123456789", "CNY": "1"}}
		cap := Capacity{Input: 200000 + random.Int64N(1000000), Output: random.Int64N(1000000), CacheRead: true, CacheWrite: true}
		bound, err := ReserveBound(schedule, fx, cap)
		if err != nil {
			t.Fatal(err)
		}
		maximum, _ := new(big.Rat).SetString(bound.Amount)
		for sample := 0; sample < 15; sample++ {
			input := random.Int64N(cap.Input + 1)
			read := random.Int64N(input + 1)
			write := random.Int64N(input - read + 1)
			usage := Usage{InputTokens: input, OutputTokens: random.Int64N(cap.Output + 1), CacheReadTokens: read, CacheWriteTokens: write}
			actual, err := Calculate(schedule, fx, usage)
			if err != nil {
				t.Fatal(err)
			}
			amount, _ := new(big.Rat).SetString(actual.Total)
			if amount.Cmp(maximum) > 0 {
				t.Fatalf("actual %s exceeds %s for %+v", actual.Total, bound.Amount, usage)
			}
		}
	}
}
func TestReservationBoundMissingTiersCacheAndExplicitZero(t *testing.T) {
	schedule := boundSchedule()
	fx := FX{PlatformCurrency: "USD", Rates: map[string]string{"USD": "1"}}
	cap := Capacity{Input: 128000, Output: 100, CacheRead: true, CacheWrite: true}
	schedule.Rates = schedule.Rates[:4]
	if _, err := ReserveBound(schedule, fx, cap); err != nil {
		t.Fatal("unreachable long tier required", err)
	}
	cap.Input++
	if _, err := ReserveBound(schedule, fx, cap); !errors.Is(err, ErrUnpriced) {
		t.Fatal("reachable missing tier accepted")
	}
	cap.Input = 100
	cap.CacheWrite = false
	schedule.Rates = schedule.Rates[:3]
	if _, err := ReserveBound(schedule, fx, cap); err != nil {
		t.Fatal("inapplicable write required", err)
	}
	cap.CacheWrite = true
	if _, err := ReserveBound(schedule, fx, cap); !errors.Is(err, ErrUnpriced) {
		t.Fatal("possible missing write accepted")
	}
	schedule = boundSchedule()
	for index := range schedule.Rates {
		schedule.Rates[index].Amount = "0"
	}
	quote, err := ReserveBound(schedule, fx, cap)
	if err != nil || quote.Amount != "0" {
		t.Fatal("explicit free rate acquired rounding charge", err)
	}
	for index := range schedule.Rates {
		schedule.Rates[index].Enabled = false
	}
	if _, err := ReserveBound(schedule, fx, Capacity{}); !errors.Is(err, ErrUnpriced) {
		t.Fatal("missing configuration became free")
	}
}
