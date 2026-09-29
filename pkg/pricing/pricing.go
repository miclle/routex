package pricing

import "math/big"

const (
	TextAdapter       = "routex_text_v1"
	MultimodalAdapter = "routex_multimodal_v1"

	Input      = "INPUT_TOKEN"
	Output     = "OUTPUT_TOKEN"
	CacheRead  = "CACHE_READ_TOKEN"
	CacheWrite = "CACHE_WRITE_TOKEN"
	ImageInput = "IMAGE_INPUT"
	PDFInput   = "PDF_INPUT"

	Unit      = "1M_TOKEN"
	ImageUnit = "1_IMAGE"
	PDFUnit   = "1_PDF"

	Base = "base"
	Long = "long_context"

	MaxRates = 10
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

// Usage is normalized billable usage. InputTokens INCLUDES both cache
// categories; adapters for native protocols must establish that invariant
// before quoting. All counts must be known, including explicit zero counts.
type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	ImageInputs      int64 `json:"image_inputs"`
	PDFInputs        int64 `json:"pdf_inputs"`
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
		if r.Unit != Unit || (r.Tier != Base && r.Tier != Long) {
			return ErrInvalid
		}
	case ImageInput:
		if r.Unit != ImageUnit || r.Tier != Base {
			return ErrInvalid
		}
	case PDFInput:
		if r.Unit != PDFUnit || r.Tier != Base {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if !Currency(r.Currency) {
		return ErrInvalid
	}
	_, err := Decimal(r.Amount)
	return err
}

// ScheduleAdapter returns the deterministic adapter required by a schedule.
// The presence of a media row is an explicit catalogue declaration even when
// that row is disabled or has a zero amount.
func ScheduleAdapter(s Schedule) string {
	for _, rate := range s.Rates {
		if rate.Metric == ImageInput || rate.Metric == PDFInput {
			return MultimodalAdapter
		}
	}
	return TextAdapter
}
func ValidateSchedule(s Schedule) error {
	if (s.Protocol != "openai_chat" && s.Protocol != "openai_responses" && s.Protocol != "anthropic_messages" && s.Protocol != "gemini_generate_content") || (s.ContextThreshold != 0 && s.ContextThreshold != 128000 && s.ContextThreshold != 200000) || len(s.Rates) > MaxRates {
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

// Calculate applies the finite RouteX pricing rules. Token rates are per million
// tokens. Image and PDF rates are additive per forwarded occurrence and always
// use the base tier. A strict input threshold selects the entire token request's
// tier, including output and caches; there is no tier fallback.
func Calculate(s Schedule, f FX, u Usage) (*Quote, error) {
	if err := ValidateSchedule(s); err != nil {
		return nil, err
	}
	if err := ValidateFX(f); err != nil {
		return nil, err
	}
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 || u.ImageInputs < 0 || u.PDFInputs < 0 || u.CacheReadTokens > u.InputTokens || u.CacheWriteTokens > u.InputTokens-u.CacheReadTokens {
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
		if r.Tier == tier || (r.Tier == Base && (r.Metric == ImageInput || r.Metric == PDFInput)) {
			rates[r.Metric] = r
		}
	}
	// Known zero usage does not turn an absent/fully disabled catalogue into a
	// configured free model. Individual zero quantities need no enabled rate.
	if !configured {
		return nil, ErrUnpriced
	}
	result := &Quote{Adapter: ScheduleAdapter(s), PriceID: s.PriceID, ProviderModelID: s.ProviderModelID, Usage: u, Tier: tier, ContextThreshold: s.ContextThreshold, Currency: f.PlatformCurrency, Components: []Component{}}
	total := new(big.Rat)
	for _, item := range []struct {
		metric   string
		quantity int64
		divisor  int64
	}{
		{Input, u.InputTokens - u.CacheReadTokens - u.CacheWriteTokens, 1000000},
		{Output, u.OutputTokens, 1000000},
		{CacheRead, u.CacheReadTokens, 1000000},
		{CacheWrite, u.CacheWriteTokens, 1000000},
		{ImageInput, u.ImageInputs, 1},
		{PDFInput, u.PDFInputs, 1},
	} {
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
		charge := new(big.Rat).Mul(amount, big.NewRat(item.quantity, item.divisor))
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
