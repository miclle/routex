package pricing

import "math/big"

// Capacity describes proven billable token limits for a supported request. Input
// includes caches; output includes every billed output token, including thoughts.
// The caller must establish these semantics before using this arithmetic.
type Capacity struct {
	Input, Output         int64
	CacheRead, CacheWrite bool
}

type BoundQuote struct {
	Amount     string   `json:"amount"`
	Currency   string   `json:"currency"`
	Capacity   Capacity `json:"capacity"`
	Components int      `json:"rounding_components"`
}

// ReserveBound conservatively dominates Calculate for every usage within cap.
// It covers all reachable tiers and cache classifications, then includes the
// maximum half-unit rounding error of each separately rounded actual component.
// This is a reservation, never an actual charge or a replacement pricing engine.
func ReserveBound(schedule Schedule, fx FX, cap Capacity) (*BoundQuote, error) {
	if cap.Input < 0 || cap.Output < 0 || ValidateSchedule(schedule) != nil || ValidateFX(fx) != nil {
		return nil, ErrInvalid
	}
	configured := false
	for _, rate := range schedule.Rates {
		configured = configured || rate.Enabled
	}
	if !configured {
		return nil, ErrUnpriced
	}
	tiers := []string{Base}
	if schedule.ContextThreshold > 0 && cap.Input > schedule.ContextThreshold {
		tiers = append(tiers, Long)
	}
	metrics := []string{Input}
	if cap.CacheRead {
		metrics = append(metrics, CacheRead)
	}
	if cap.CacheWrite {
		metrics = append(metrics, CacheWrite)
	}
	maxInput, maxOutput := new(big.Rat), new(big.Rat)
	components := 0
	if cap.Input > 0 {
		components = len(metrics)
	}
	if cap.Output > 0 {
		components++
	}
	for _, tier := range tiers {
		for _, metric := range append(append([]string{}, metrics...), Output) {
			if metric == Output && cap.Output == 0 || metric != Output && cap.Input == 0 {
				continue
			}
			var selected *Rate
			for index := range schedule.Rates {
				rate := &schedule.Rates[index]
				if rate.Tier == tier && rate.Metric == metric && rate.Enabled {
					selected = rate
					break
				}
			}
			if selected == nil {
				return nil, ErrUnpriced
			}
			exchange := "1"
			if selected.Currency != fx.PlatformCurrency {
				var ok bool
				exchange, ok = fx.Rates[selected.Currency]
				if !ok {
					return nil, ErrUnpriced
				}
			}
			rate, err := rational(selected.Amount)
			if err != nil {
				return nil, err
			}
			conversion, err := rational(exchange)
			if err != nil {
				return nil, err
			}
			rate.Mul(rate, conversion)
			target := maxInput
			if metric == Output {
				target = maxOutput
			}
			if rate.Cmp(target) > 0 {
				target.Set(rate)
			}
		}
	}
	exact := new(big.Rat).Mul(maxInput, big.NewRat(cap.Input, 1000000))
	exact.Add(exact, new(big.Rat).Mul(maxOutput, big.NewRat(cap.Output, 1000000)))
	result := &BoundQuote{Amount: "0", Currency: fx.PlatformCurrency, Capacity: cap, Components: components}
	if exact.Sign() == 0 {
		return result, nil
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	units := new(big.Rat).Mul(exact, new(big.Rat).SetInt(scale))
	units.Add(units, big.NewRat(int64(components), 2))
	roundedUp, remainder := new(big.Int), new(big.Int)
	roundedUp.QuoRem(units.Num(), units.Denom(), remainder)
	if remainder.Sign() != 0 {
		roundedUp.Add(roundedUp, big.NewInt(1))
	}
	result.Amount = rounded(new(big.Rat).SetFrac(roundedUp, scale))
	return result, nil
}
