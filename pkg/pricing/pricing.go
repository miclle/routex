package pricing

import "math/big"

const (
	Input      = "INPUT_TOKEN"
	Output     = "OUTPUT_TOKEN"
	CacheRead  = "CACHE_READ_TOKEN"
	CacheWrite = "CACHE_WRITE_TOKEN"
	Unit       = "1M_TOKEN"
	Base       = "base"
	Long       = "long_context"
)

type Rate struct {
	ID       string `json:"id,omitempty"`
	Metric   string `json:"metric"`
	Tier     string `json:"tier"`
	Unit     string `json:"unit"`
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
	Enabled  bool   `json:"enabled"`
}
type Schedule struct {
	PriceID          string `json:"price_id"`
	ProviderModelID  string `json:"provider_model_id"`
	Protocol         string `json:"protocol"`
	ContextThreshold int64  `json:"context_threshold"`
	Rates            []Rate `json:"rates"`
}

// Usage is normalized text usage. InputTokens INCLUDES both cache categories;
// adapters for native protocols must establish that invariant before quoting.
// All counts must be known, including explicit zero cache counts.
type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}
type FX struct {
	PlatformCurrency string            `json:"platform_currency"`
	Rates            map[string]string `json:"rates"`
}
type Component struct {
	Rate         Rate   `json:"rate"`
	Quantity     int64  `json:"quantity"`
	ExchangeRate string `json:"exchange_rate"`
	Charge       string `json:"charge"`
}
type Quote struct {
	Adapter          string      `json:"adapter"`
	PriceID          string      `json:"price_id"`
	ProviderModelID  string      `json:"provider_model_id"`
	Usage            Usage       `json:"usage"`
	Tier             string      `json:"tier"`
	ContextThreshold int64       `json:"context_threshold"`
	Currency         string      `json:"currency"`
	Components       []Component `json:"components"`
	Total            string      `json:"total"`
}

func ValidateRate(r Rate) error {
	switch r.Metric {
	case Input, Output, CacheRead, CacheWrite:
	default:
		return ErrInvalid
	}
	if r.Tier != Base && r.Tier != Long {
		return ErrInvalid
	}
	if r.Unit != Unit || !Currency(r.Currency) {
		return ErrInvalid
	}
	_, err := Decimal(r.Amount)
	return err
}
func ValidateSchedule(s Schedule) error {
	if (s.Protocol != "openai_chat" && s.Protocol != "openai_responses" && s.Protocol != "anthropic_messages" && s.Protocol != "gemini_generate_content") || (s.ContextThreshold != 0 && s.ContextThreshold != 128000 && s.ContextThreshold != 200000) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, r := range s.Rates {
		if err := ValidateRate(r); err != nil {
			return err
		}
		key := r.Metric + "/" + r.Tier
		if seen[key] || (r.Tier == Long && r.Enabled && s.ContextThreshold == 0) {
			return ErrInvalid
		}
		seen[key] = true
	}
	return nil
}
func ValidateFX(f FX) error {
	if !Currency(f.PlatformCurrency) {
		return ErrInvalid
	}
	for currency, value := range f.Rates {
		if !Currency(currency) {
			return ErrInvalid
		}
		r, err := rational(value)
		if err != nil || r.Sign() <= 0 {
			return ErrInvalid
		}
		if currency == f.PlatformCurrency && r.Cmp(big.NewRat(1, 1)) != 0 {
			return ErrInvalid
		}
	}
	return nil
}

// Calculate applies the finite RouteX text pricing rules. Rates are per million
// tokens. A strict input threshold selects the
// entire request's tier, including output and caches; there is no tier fallback.
func Calculate(s Schedule, f FX, u Usage) (*Quote, error) {
	if err := ValidateSchedule(s); err != nil {
		return nil, err
	}
	if err := ValidateFX(f); err != nil {
		return nil, err
	}
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 || u.CacheReadTokens > u.InputTokens || u.CacheWriteTokens > u.InputTokens-u.CacheReadTokens {
		return nil, ErrInvalid
	}
	tier := Base
	if s.ContextThreshold > 0 && u.InputTokens > s.ContextThreshold {
		tier = Long
	}
	rates := map[string]Rate{}
	configured := false
	for _, r := range s.Rates {
		if r.Enabled {
			configured = true
		}
		if r.Tier == tier {
			rates[r.Metric] = r
		}
	}
	// Known zero usage does not turn an absent/fully disabled catalogue into a
	// configured free model. Individual zero quantities need no enabled rate.
	if !configured {
		return nil, ErrUnpriced
	}
	result := &Quote{Adapter: "routex_text_v1", PriceID: s.PriceID, ProviderModelID: s.ProviderModelID, Usage: u, Tier: tier, ContextThreshold: s.ContextThreshold, Currency: f.PlatformCurrency, Components: []Component{}}
	total := new(big.Rat)
	for _, item := range []struct {
		metric   string
		quantity int64
	}{{Input, u.InputTokens - u.CacheReadTokens - u.CacheWriteTokens}, {Output, u.OutputTokens}, {CacheRead, u.CacheReadTokens}, {CacheWrite, u.CacheWriteTokens}} {
		if item.quantity == 0 {
			continue
		}
		r, ok := rates[item.metric]
		if !ok || !r.Enabled {
			return nil, ErrUnpriced
		}
		fx := "1"
		if r.Currency != f.PlatformCurrency {
			fx, ok = f.Rates[r.Currency]
			if !ok {
				return nil, ErrUnpriced
			}
		}
		amount, err := rational(r.Amount)
		if err != nil {
			return nil, err
		}
		exchange, err := rational(fx)
		if err != nil {
			return nil, err
		}
		charge := new(big.Rat).Mul(amount, big.NewRat(item.quantity, 1000000))
		charge.Mul(charge, exchange)
		value := rounded(charge)
		exact, _ := new(big.Rat).SetString(value)
		total.Add(total, exact)
		r.Amount, _ = Decimal(r.Amount)
		fx, _ = Decimal(fx)
		result.Components = append(result.Components, Component{Rate: r, Quantity: item.quantity, ExchangeRate: fx, Charge: value})
	}
	result.Total = rounded(total)
	return result, nil
}
