package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"net/http"
	"strconv"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

const priceExportModels = 500
const priceExportBytes = 2 << 20

var priceExportTooLarge = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "price catalogue exceeds the CSV export limit"}

type PriceCSVExport struct {
	ETag string
	CSV  []byte
}

func safePriceCSVName(value string) string { return safeCSVCell(value) }
func (s *Service) ExportPriceCSV(ctx context.Context, actorID string) (*PriceCSVExport, error) {
	result := &PriceCSVExport{}
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	header := append(append([]string{}, priceCSVColumns...), "upstream_name")
	if err := writer.Write(header); err != nil {
		return result, err
	}
	etag, err := s.exportPriceRows(ctx, actorID, priceExportTooLarge, func(row []string) error {
		row[8] = safePriceCSVName(row[8])
		if err := writer.Write(row); err != nil {
			return err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}
		if buffer.Len() > priceExportBytes {
			return priceExportTooLarge
		}
		return nil
	})
	result.ETag = etag
	if err != nil {
		return result, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return result, pricingError(err)
	}
	result.CSV = bytes.Clone(buffer.Bytes())
	return result, nil
}

// Both formats consume the same authorized, complete current snapshot. Raw
// names reach the serializer so CSV protection never changes XLSX text cells.
func (s *Service) exportPriceRows(ctx context.Context, actorID string, tooLarge error, consume func([]string) error) (string, error) {
	etag := ""
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "prices.read"); err != nil {
			return err
		}
		var setting entity.PricingSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		etag = setting.ETag
		var models []entity.ModelPrice
		if err := tx.Order("id").Limit(priceExportModels + 1).Find(&models).Error; err != nil {
			return err
		}
		if len(models) > priceExportModels {
			return tooLarge
		}
		rows := 0
		for _, model := range models {
			record, err := loadPrice(tx, model)
			if err != nil {
				return err
			}
			for _, rate := range record.Rates {
				rows++
				if rows > priceExportModels*pricing.MaxRates {
					return tooLarge
				}
				if err := consume([]string{record.ProviderModelID, rate.Metric, rate.Tier, rate.Unit, rate.Currency, rate.Amount, strconv.FormatBool(rate.Enabled), strconv.FormatInt(record.ContextThreshold, 10), record.UpstreamName}); err != nil {
					return err
				}
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return etag, pricingError(err)
}
