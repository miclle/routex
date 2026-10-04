package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/prices"
)

const repositoryMaxMappings = 100

var repositorySourceKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)

type PriceRateSource struct {
	Kind           string  `json:"kind"`
	SourceModelKey *string `json:"source_model_key"`
	SourceRateKey  *string `json:"source_rate_key"`
}
type PriceThresholdSource struct {
	Kind           string  `json:"kind"`
	SourceModelKey *string `json:"source_model_key"`
}
type RepositoryPriceMappingInput struct {
	ProviderModelID string `json:"provider_model_id"`
	SourceModelKey  string `json:"source_model_key"`
}
type RepositoryPriceConfigInput struct {
	RequestID string                        `json:"request_id"`
	Enabled   bool                          `json:"enabled"`
	Mappings  []RepositoryPriceMappingInput `json:"mappings"`
	Reason    string                        `json:"reason"`
}
type RepositorySourceModel struct {
	Key              string `json:"key"`
	ProviderKey      string `json:"provider_key"`
	Model            string `json:"model"`
	Protocol         string `json:"protocol"`
	ContextThreshold int64  `json:"context_threshold"`
}
type RepositoryPriceSourceView struct {
	ID         string                  `json:"id"`
	Digest     string                  `json:"digest"`
	ModelCount int                     `json:"model_count"`
	Models     []RepositorySourceModel `json:"models"`
}
type RepositoryPriceConfigView struct {
	ReviewETag    string                        `json:"review_etag"`
	Enabled       bool                          `json:"enabled"`
	CanWrite      bool                          `json:"can_write"`
	Source        RepositoryPriceSourceView     `json:"source"`
	Mappings      []RepositoryPriceMappingInput `json:"mappings"`
	LastAttemptAt *time.Time                    `json:"last_attempt_at"`
	LastSuccessAt *time.Time                    `json:"last_success_at"`
	LastResult    *string                       `json:"last_result"`
}
type RepositoryPriceCandidateFilter struct {
	Query, Cursor string
	Limit         int
}
type RepositoryPriceCandidate struct {
	ProviderModelID string `json:"provider_model_id"`
	UpstreamName    string `json:"upstream_name"`
	Protocol        string `json:"protocol"`
}
type RepositoryPriceCandidatePage struct {
	Items      []RepositoryPriceCandidate `json:"items"`
	NextCursor string                     `json:"next_cursor"`
}
type RepositoryPriceSelection struct {
	Mode             string   `json:"mode"`
	ProviderModelIDs []string `json:"provider_model_ids"`
	RateIDs          []string `json:"rate_ids"`
}
type RepositoryPriceApplyInput struct {
	RequestID     string                   `json:"request_id"`
	PreviewDigest string                   `json:"preview_digest"`
	Selection     RepositoryPriceSelection `json:"selection"`
	Reason        string                   `json:"reason"`
}
type RepositoryPriceIssue struct {
	ProviderModelID string  `json:"provider_model_id"`
	RateID          *string `json:"rate_id"`
	Code            string  `json:"code"`
	Message         string  `json:"message"`
}
type RepositoryPriceChange struct {
	ProviderModelID string          `json:"provider_model_id"`
	RateID          *string         `json:"rate_id"`
	SourceModelKey  string          `json:"source_model_key"`
	SourceRateKey   string          `json:"source_rate_key"`
	Action          string          `json:"action"`
	Before          *pricing.Rate   `json:"before"`
	After           *pricing.Rate   `json:"after"`
	BeforeSource    PriceRateSource `json:"before_source"`
	AfterSource     PriceRateSource `json:"after_source"`
	ThresholdBefore int64           `json:"threshold_before"`
	ThresholdAfter  int64           `json:"threshold_after"`
}
type RepositoryPricePreview struct {
	ReviewETag    string                  `json:"review_etag"`
	SourceDigest  string                  `json:"source_digest"`
	PreviewDigest string                  `json:"preview_digest"`
	Mode          string                  `json:"mode"`
	Valid         bool                    `json:"valid"`
	Changes       []RepositoryPriceChange `json:"changes"`
	Errors        []RepositoryPriceIssue  `json:"errors"`
	Warnings      []RepositoryPriceIssue  `json:"warnings"`
}
type RepositoryPriceReceiptView struct {
	RequestID    string    `json:"request_id"`
	SourceDigest string    `json:"source_digest"`
	Mode         string    `json:"mode"`
	CreatedAt    time.Time `json:"created_at"`
}
type RepositoryPriceResult struct {
	Receipt              RepositoryPriceReceiptView `json:"receipt"`
	Committed            bool                       `json:"committed"`
	ConfigurationApplied bool                       `json:"configuration_applied"`
	Configuration        *RepositoryPriceConfigView `json:"configuration"`
	RuntimeApplied       bool                       `json:"runtime_applied"`
	ApplicationStatus    string                     `json:"application_status"`
	Created              bool                       `json:"-"`
}

func WithRepositoryPriceSource(source *prices.Snapshot) Option {
	return func(s *Service) { s.repositorySource = source }
}
func (s *Service) repositoryPriceSnapshot() (*prices.Snapshot, error) {
	if s.repositorySource != nil {
		return s.repositorySource, nil
	}
	return prices.Embedded()
}
func repositoryHash(value any) string {
	raw, _ := json.Marshal(value)
	return secret.SHA256Hex(string(raw))
}
func repositoryReason(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.TrimSpace(value) == value && utf8.RuneCountInString(value) <= 1000 && !strings.ContainsFunc(value, unicode.IsControl)
}
func repositoryRateSource(rate entity.PriceRate) PriceRateSource {
	result := PriceRateSource{Kind: "custom"}
	if rate.RepositoryModelKey != nil && rate.RepositoryRateKey != nil {
		result.Kind = "repository"
		result.SourceModelKey = rate.RepositoryModelKey
		result.SourceRateKey = rate.RepositoryRateKey
	}
	return result
}
func repositoryThresholdSource(price entity.ModelPrice) PriceThresholdSource {
	result := PriceThresholdSource{Kind: "custom"}
	if price.RepositoryThresholdKey != nil {
		result.Kind = "repository"
		result.SourceModelKey = price.RepositoryThresholdKey
	}
	return result
}
func repositoryRateValue(rate entity.PriceRate) pricing.Rate {
	return pricing.Rate{ID: rate.ID, Metric: rate.Metric, Tier: rate.Tier, Unit: rate.Unit, Currency: rate.Currency, Amount: rate.Amount, Enabled: rate.Enabled}
}
func (s *Service) GetRepositoryPriceReceipt(ctx context.Context, actorID, requestID string) (*RepositoryPriceResult, error) {
	if !credentialReplacementRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	return s.repositoryReceipt(ctx, actorID, requestID)
}
