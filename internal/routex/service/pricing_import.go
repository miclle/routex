package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

type PriceImportChange struct {
	Row             int           `json:"row"`
	ProviderModelID string        `json:"provider_model_id"`
	UpstreamName    string        `json:"upstream_name"`
	Action          string        `json:"action"`
	Before          *pricing.Rate `json:"before"`
	After           pricing.Rate  `json:"after"`
	ThresholdBefore int64         `json:"threshold_before"`
	ThresholdAfter  int64         `json:"threshold_after"`
	StopsFollowing  bool          `json:"stops_following"`
}
type PriceImportPreview struct {
	ETag    string              `json:"etag"`
	Digest  string              `json:"preview_digest"`
	Valid   bool                `json:"valid"`
	Items   []PriceInput        `json:"items"`
	Changes []PriceImportChange `json:"changes"`
	Errors  []PriceImportError  `json:"errors"`
	Sheet   string              `json:"sheet,omitempty"`
}
type PriceImportCommit struct {
	Preview   *PriceImportPreview `json:"preview"`
	Catalogue *PricePage          `json:"catalogue,omitempty"`
}

func priceImportDigest(etag string, items []PriceInput) string {
	raw, _ := json.Marshal(struct {
		ETag  string
		Items []PriceInput
	}{etag, items})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func validatePriceImport(tx *gorm.DB, parsed parsedPriceCSV, setting entity.PricingSetting) (*PriceImportPreview, error) {
	result := &PriceImportPreview{ETag: setting.ETag, Sheet: parsed.Sheet, Items: parsed.Items, Changes: []PriceImportChange{}, Errors: slices.Clone(parsed.Errors)}
	if result.Errors == nil {
		result.Errors = []PriceImportError{}
	}
	fx, err := pricingFX(tx, setting)
	if err != nil {
		return nil, err
	}
	add := func(row priceImportRow, column, code, message string) {
		result.Errors = append(result.Errors, PriceImportError{Row: row.Line, Column: column, Code: code, Message: message, Sheet: parsed.Sheet, Cell: parsed.cell(row.Line, column)})
	}
	auditBefore, auditAfter := []PriceRecord{}, []PriceRecord{}
	for _, item := range parsed.Items {
		rows := []priceImportRow{}
		for _, row := range parsed.Rows {
			if row.ModelID == item.ProviderModelID {
				rows = append(rows, row)
			}
		}
		var pm entity.ProviderModel
		err := tx.First(&pm, "id = ?", item.ProviderModelID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			for _, row := range rows {
				add(row, "provider_model_id", "unknown_model", "The provider model does not exist.")
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		var connection entity.ProviderConnection
		if err := tx.First(&connection, "id = ?", pm.ConnectionID).Error; err != nil {
			return nil, err
		}
		if !entity.SupportedNativeProtocol(connection.Protocol) {
			for _, row := range rows {
				add(row, "provider_model_id", "unsupported_protocol", "The provider model does not use supported text pricing.")
			}
			continue
		}
		var aggregate entity.ModelPrice
		err = tx.First(&aggregate, "provider_model_id = ?", pm.ID).Error
		before := PriceRecord{ProviderModelID: pm.ID, Protocol: connection.Protocol, Rates: []pricing.Rate{}}
		if err == nil {
			before, err = loadPrice(tx, aggregate)
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
		if err != nil {
			return nil, err
		}
		merged := priceSchedule(before)
		merged.Rates = slices.Clone(merged.Rates)
		if item.ContextThreshold != nil {
			merged.ContextThreshold = *item.ContextThreshold
		}
		for _, row := range rows {
			after := row.Rate
			var old *pricing.Rate
			for index, rate := range merged.Rates {
				if rate.Metric == after.Metric && rate.Tier == after.Tier {
					copy := rate
					old = &copy
					after.ID = rate.ID
					merged.Rates[index] = after
					break
				}
			}
			if old == nil {
				merged.Rates = append(merged.Rates, after)
			}
			action := "added"
			if old != nil {
				action = "updated"
				if *old == after && before.ContextThreshold == merged.ContextThreshold && !before.FollowRepository && before.UpdateSource == parsed.Source {
					action = "unchanged"
				}
			}
			result.Changes = append(result.Changes, PriceImportChange{Row: row.Line, ProviderModelID: pm.ID, UpstreamName: pm.UpstreamName, Action: action, Before: old, After: after, ThresholdBefore: before.ContextThreshold, ThresholdAfter: merged.ContextThreshold, StopsFollowing: before.FollowRepository})
		}
		if pricing.ValidateSchedule(merged) != nil {
			for _, row := range rows {
				add(row, "context_threshold", "invalid_schedule", "The resulting schedule is invalid; disabling a threshold requires disabling retained long-context rates.")
			}
		}
		if before.ID != "" {
			auditBefore = append(auditBefore, before)
		}
		after := before
		after.ProviderID, after.UpstreamName = connection.ProviderID, pm.UpstreamName
		after.ContextThreshold, after.UpdateSource, after.FollowRepository = merged.ContextThreshold, parsed.Source, false
		if after.ID == "" {
			after.ID = "prc_" + strings.Repeat("0", 26)
		}
		after.Rates = slices.Clone(merged.Rates)
		for index := range after.Rates {
			if after.Rates[index].ID == "" {
				after.Rates[index].ID = "rat_" + strings.Repeat("0", 26)
			}
		}
		auditAfter = append(auditAfter, after)
		for _, rate := range merged.Rates {
			if rate.Enabled && rate.Currency != fx.PlatformCurrency {
				if _, ok := fx.Rates[rate.Currency]; !ok {
					for _, row := range rows {
						if row.Rate.Currency == rate.Currency && row.Rate.Enabled {
							add(row, "currency", "missing_exchange_rate", "Configure the currency conversion before enabling this price.")
						}
					}
				}
			}
		}
	}
	if len(result.Errors) == 0 {
		type auditState struct {
			ETag  string        `json:"etag"`
			Items []PriceRecord `json:"items"`
		}
		encoded, err := json.Marshal(struct {
			Source string     `json:"source"`
			Before auditState `json:"before"`
			After  auditState `json:"after"`
		}{parsed.Source, auditState{setting.ETag, auditBefore}, auditState{strings.Repeat("x", 32), auditAfter}})
		if err != nil {
			return nil, err
		}
		if len(encoded) > 60*1024 {
			result.Errors = append(result.Errors, PriceImportError{Row: 0, Code: "audit_too_large", Message: "The normalized changes exceed the audit limit; split the file into smaller imports."})
		}
	}
	slices.SortStableFunc(result.Errors, func(a, b PriceImportError) int { return a.Row - b.Row })
	slices.SortFunc(result.Changes, func(a, b PriceImportChange) int { return a.Row - b.Row })
	result.Valid = len(result.Errors) == 0
	if result.Valid {
		result.Digest = parsed.digest(setting.ETag)
	}
	return result, nil
}
func (s *Service) PreviewPriceImport(ctx context.Context, actorID, raw string) (*PriceImportPreview, error) {
	return s.previewPriceDocument(ctx, actorID, parsePriceCSV(raw))
}
func (s *Service) previewPriceDocument(ctx context.Context, actorID string, parsed parsedPriceCSV) (*PriceImportPreview, error) {
	var result *PriceImportPreview
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "prices.read"); err != nil {
			return err
		}
		var setting entity.PricingSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		var err error
		result, err = validatePriceImport(tx, parsed, setting)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, pricingError(err)
}
func (s *Service) CommitPriceImport(ctx context.Context, actorID, raw, etag, digest string) (*PriceImportCommit, error) {
	return s.commitPriceDocument(ctx, actorID, parsePriceCSV(raw), etag, digest)
}
func (s *Service) commitPriceDocument(ctx context.Context, actorID string, parsed parsedPriceCSV, etag, digest string) (*PriceImportCommit, error) {
	if etag == "" || len(etag) > 64 || len(digest) != 64 {
		return nil, apperrors.ErrBadRequest
	}
	result := &PriceImportCommit{}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "prices.write"); err != nil {
			return err
		}
		setting, err := lockPricing(tx)
		if err != nil {
			return err
		}
		if setting.ETag != etag {
			return pricingStale
		}
		result.Preview, err = validatePriceImport(tx, parsed, setting)
		if err != nil {
			return err
		}
		if !result.Preview.Valid {
			return nil
		}
		if result.Preview.Digest != digest {
			return apperrors.ErrBadRequest
		}
		fx, err := pricingFX(tx, setting)
		if err != nil {
			return err
		}
		result.Catalogue, err = applyPriceBatch(tx, actorID, setting, fx, parsed.Items, parsed.Source)
		return err
	})
	if err == nil && result.Catalogue != nil {
		err = s.refreshAfterMutation(ctx, nil)
	}
	return result, pricingError(err)
}
