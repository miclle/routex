package service

import "time"

type MemberModelBaseRate struct {
	Amount   string `json:"amount"`
	Unit     string `json:"unit"`     // exactly 1M_TOKEN
	Currency string `json:"currency"` // existing pricing.Currency allowlist
}

type MemberModelPriceCell struct {
	State string               `json:"state"` // unauthorized|unavailable|missing|heterogeneous|priced|disabled
	Rate  *MemberModelBaseRate `json:"rate"`  // nonnil only priced or disabled
}

type MemberModelRow struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Status       string               `json:"status"`       // active|disabled|archived
	Type         *string              `json:"type"`         // null in this release; no declaration exists
	Providers    []string             `json:"providers"`    // nil => null when providers.read absent
	Protocols    []string             `json:"protocols"`    // four existing native values only; never null
	Availability string               `json:"availability"` // ready|unavailable|unknown
	InputPrice   MemberModelPriceCell `json:"input_price"`
	OutputPrice  MemberModelPriceCell `json:"output_price"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    *time.Time           `json:"updated_at"` // null; no semantic timestamp exists
	Selectable   bool                 `json:"selectable"` // true only on available rows currently eligible for Add
}

type MemberModelsWorkspace struct {
	UserID            string           `json:"user_id"`
	ObservedAt        time.Time        `json:"observed_at"`
	ETag              string           `json:"etag"` // lowercase hex64, strong quoted header matches
	CanEdit           bool             `json:"can_edit"`
	PersonalModels    []MemberModelRow `json:"personal_models"`  // complete retained set
	AvailableModels   []MemberModelRow `json:"available_models"` // complete advisory candidates
	RuntimeApplied    *bool            `json:"runtime_applied"`
	ApplicationStatus string           `json:"application_status"` // applied|not_applied|unavailable
	runtimeBusy       bool             // Private retry cause; never establishes current application.
}

type MemberModelsWriteInput struct {
	ModelIDs []string `json:"model_ids"` // required nonnull complete set, <=1000
	Reason   string   `json:"reason"`    // required trim-exact UTF-8, <=1024 bytes, no controls
}

type MemberModelsWriteResult struct {
	UserID         string   `json:"user_id"`
	ModelIDs       []string `json:"model_ids"`       // exact sorted current Personal set
	ETag           string   `json:"etag"`            // strong quoted response header matches
	RuntimeApplied bool     `json:"runtime_applied"` // successful result always true
	Confirmation   string   `json:"confirmation"`    // exactly current_model_grants
}
