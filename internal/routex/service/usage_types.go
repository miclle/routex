package service

import "time"

// UsageFilter selects persisted canonical facts, never live quota counters.
type UsageFilter struct {
	Period, Granularity, Timezone                    string
	From, To                                         *time.Time
	Compare                                          bool
	ModelID, KeyID, Status, Protocol                 string
	Stream                                           *bool
	UserID, ProjectID, ProviderModelID, ConnectionID string
}
type UsageCount struct {
	Value        *string `json:"value"`
	Known        string  `json:"known"`
	UnknownCalls int64   `json:"unknown_calls"`
}
type UsageTokens struct {
	Input  UsageCount `json:"input"`
	Output UsageCount `json:"output"`
	Total  UsageCount `json:"total"`
}
type UsageAmount struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
	Calls    int64  `json:"calls"`
}
type UsageStats struct {
	Requests           int64            `json:"requests"`
	Successes          int64            `json:"successes"`
	Errors             int64            `json:"errors"`
	Canceled           int64            `json:"canceled"`
	SuccessRate        *float64         `json:"success_rate"`
	AverageDurationMS  *float64         `json:"average_duration_ms"`
	Tokens             UsageTokens      `json:"tokens"`
	Amounts            []UsageAmount    `json:"amounts"`
	UnknownAmountCalls int64            `json:"unknown_amount_calls"`
	PricingStatuses    map[string]int64 `json:"pricing_statuses"`
}
type UsageBucket struct {
	Start time.Time  `json:"start"`
	End   time.Time  `json:"end"`
	Stats UsageStats `json:"stats"`
}
type UsageGroup struct {
	ID      string     `json:"id"`
	Name    string     `json:"name,omitempty"`
	Unknown bool       `json:"unknown"`
	Stats   UsageStats `json:"stats"`
}
type UsagePeriod struct {
	From           time.Time     `json:"from"`
	To             time.Time     `json:"to"`
	Summary        UsageStats    `json:"summary"`
	Trend          []UsageBucket `json:"trend"`
	Models         []UsageGroup  `json:"models"`
	Keys           []UsageGroup  `json:"keys"`
	ProviderModels []UsageGroup  `json:"provider_models,omitempty"`
	Connections    []UsageGroup  `json:"connections,omitempty"`
}
type UsageReport struct {
	Timezone            string       `json:"timezone"`
	Granularity         string       `json:"granularity"`
	QueriedAt           time.Time    `json:"queried_at"`
	LatestCompletedAt   *time.Time   `json:"latest_completed_at"`
	Source              string       `json:"source"`
	MayLag              bool         `json:"may_lag"`
	Current             UsagePeriod  `json:"current"`
	Previous            *UsagePeriod `json:"previous,omitempty"`
	AvailableDimensions []string     `json:"available_dimensions"`
}
