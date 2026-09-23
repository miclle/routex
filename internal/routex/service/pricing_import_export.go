package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

const priceExportModels = 500
const priceExportBytes = 2 << 20

var priceExportTooLarge = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "price catalogue exceeds the CSV export limit"}

type PriceCSVExport struct {
	ETag string
	CSV  []byte
}

func safePriceCSVName(value string) string {
	visible := strings.TrimLeftFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	if visible != "" && strings.ContainsRune("=+-@", rune(visible[0])) {
		return "'" + value
	}
	return value
}
func (s *Service) ExportPriceCSV(ctx context.Context, actorID string) (*PriceCSVExport, error) {
	result := &PriceCSVExport{}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "prices.read"); err != nil {
			return err
		}
		var setting entity.PricingSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		result.ETag = setting.ETag
		var models []entity.ModelPrice
		if err := tx.Order("id").Limit(priceExportModels + 1).Find(&models).Error; err != nil {
			return err
		}
		if len(models) > priceExportModels {
			return priceExportTooLarge
		}
		var buffer bytes.Buffer
		writer := csv.NewWriter(&buffer)
		header := append(append([]string{}, priceCSVColumns...), "upstream_name")
		if err := writer.Write(header); err != nil {
			return err
		}
		rows := 0
		for _, model := range models {
			record, err := loadPrice(tx, model)
			if err != nil {
				return err
			}
			for _, rate := range record.Rates {
				rows++
				if rows > priceExportModels*8 {
					return priceExportTooLarge
				}
				if err := writer.Write([]string{record.ProviderModelID, rate.Metric, rate.Tier, rate.Unit, rate.Currency, rate.Amount, strconv.FormatBool(rate.Enabled), strconv.FormatInt(record.ContextThreshold, 10), safePriceCSVName(record.UpstreamName)}); err != nil {
					return err
				}
				writer.Flush()
				if err := writer.Error(); err != nil {
					return err
				}
				if buffer.Len() > priceExportBytes {
					return priceExportTooLarge
				}
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return err
		}
		result.CSV = bytes.Clone(buffer.Bytes())
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, pricingError(err)
}
