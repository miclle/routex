package service

import "time"

type MemberEffectiveModelSource struct {
	Kind         string   `json:"kind"`
	TeamID       *string  `json:"team_id"`
	TeamName     *string  `json:"team_name"`
	Protocols    []string `json:"protocols"`
	Availability string   `json:"availability"`
}
type MemberEffectiveModelRow struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Status       string                       `json:"status"`
	Type         *string                      `json:"type"`
	Providers    []string                     `json:"providers"`
	Protocols    []string                     `json:"protocols"`
	Availability string                       `json:"availability"`
	InputPrice   MemberModelPriceCell         `json:"input_price"`
	OutputPrice  MemberModelPriceCell         `json:"output_price"`
	CreatedAt    time.Time                    `json:"created_at"`
	UpdatedAt    *time.Time                   `json:"updated_at"`
	Sources      []MemberEffectiveModelSource `json:"sources"`
}
type MemberEffectiveModelsPage struct {
	UserID            string                    `json:"user_id"`
	ObservedAt        time.Time                 `json:"observed_at"`
	SubjectStatus     string                    `json:"subject_status"`
	TeamEnrichment    string                    `json:"team_enrichment"`
	UnionCompleteness string                    `json:"union_completeness"`
	Items             []MemberEffectiveModelRow `json:"items"`
}
