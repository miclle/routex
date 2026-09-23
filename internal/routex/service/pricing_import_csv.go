package service

import (
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/miclle/routex/pkg/pricing"
)

const priceImportBytes = 32 << 10
const priceImportRows = 160
const priceImportModels = 20

var priceCSVColumns = []string{"provider_model_id", "metric", "tier", "unit", "currency", "amount", "enabled", "context_threshold"}
var priceCSVIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{1,30}$`)

type PriceImportError struct {
	Row     int    `json:"row"`
	Column  string `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type priceImportRow struct {
	Line      int
	ModelID   string
	Threshold *int64
	Rate      pricing.Rate
}
type parsedPriceCSV struct {
	Rows   []priceImportRow
	Items  []PriceInput
	Errors []PriceImportError
}

func (p *parsedPriceCSV) add(row int, column, code, message string) {
	p.Errors = append(p.Errors, PriceImportError{row, column, code, message})
}

func parsePriceCSV(raw string) parsedPriceCSV {
	result := parsedPriceCSV{Rows: []priceImportRow{}, Items: []PriceInput{}, Errors: []PriceImportError{}}
	if len(raw) > priceImportBytes {
		result.add(0, "", "file_too_large", "CSV must not exceed 32 KiB.")
		return result
	}
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		result.add(0, "", "invalid_encoding", "CSV must be UTF-8 text without NUL bytes.")
		return result
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(raw, "\ufeff")))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		result.add(1, "", "invalid_header", "A valid CSV header is required.")
		return result
	}
	positions := map[string]int{}
	for index, name := range header {
		if _, exists := positions[name]; exists {
			result.add(1, name, "duplicate_column", "A column may appear only once.")
			continue
		}
		positions[name] = index
		if !slices.Contains(priceCSVColumns, name) && name != "upstream_name" {
			result.add(1, "", "unsupported_column", "The header contains an unsupported column.")
		}
	}
	for _, name := range priceCSVColumns {
		if _, ok := positions[name]; !ok {
			result.add(1, name, "missing_column", "A required column is missing.")
		}
	}
	if len(result.Errors) > 0 {
		return result
	}
	seen := map[string]int{}
	models := map[string]*PriceInput{}
	order := []string{}
	for count := 0; ; count++ {
		cells, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line := count + 2
			var parse *csv.ParseError
			if errors.As(err, &parse) {
				line = parse.Line
			}
			result.add(line, "", "invalid_csv", "Malformed CSV cannot be parsed safely.")
			break
		}
		line, _ := reader.FieldPos(0)
		if count >= priceImportRows {
			result.add(line, "", "too_many_rows", "CSV must contain at most 160 rate rows.")
			break
		}
		if len(cells) != len(header) {
			result.add(line, "", "column_count", "The row must have the same number of columns as the header.")
			continue
		}
		get := func(name string) string { return cells[positions[name]] }
		row := priceImportRow{Line: line, ModelID: get("provider_model_id"), Rate: pricing.Rate{Metric: get("metric"), Tier: get("tier"), Unit: get("unit"), Currency: get("currency"), Amount: get("amount")}}
		before := len(result.Errors)
		if !priceCSVIdentity.MatchString(row.ModelID) {
			result.add(line, "provider_model_id", "invalid_identity", "A stable provider model ID is required.")
		}
		if !slices.Contains([]string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite}, row.Rate.Metric) {
			result.add(line, "metric", "unsupported_metric", "Use one of the four supported text-token metrics.")
		}
		if row.Rate.Tier != pricing.Base && row.Rate.Tier != pricing.Long {
			result.add(line, "tier", "unsupported_tier", "Use base or long_context.")
		}
		if row.Rate.Unit != pricing.Unit {
			result.add(line, "unit", "unsupported_unit", "The supported unit is 1M_TOKEN.")
		}
		if !pricing.Currency(row.Rate.Currency) {
			result.add(line, "currency", "unsupported_currency", "The currency is not supported.")
		}
		amount, amountErr := pricing.Decimal(row.Rate.Amount)
		if amountErr != nil {
			result.add(line, "amount", "invalid_decimal", "Amount must be a nonnegative plain decimal with at most 18 integer and 18 fractional digits.")
		} else {
			row.Rate.Amount = amount
		}
		switch get("enabled") {
		case "true":
			row.Rate.Enabled = true
		case "false":
		default:
			result.add(line, "enabled", "invalid_boolean", "Enabled must explicitly be true or false.")
		}
		if rawThreshold := get("context_threshold"); rawThreshold != "" {
			value, err := strconv.ParseInt(rawThreshold, 10, 64)
			if err != nil || (rawThreshold != "0" && rawThreshold != "128000" && rawThreshold != "200000") {
				result.add(line, "context_threshold", "unsupported_threshold", "Use blank, 0, 128000, or 200000.")
			} else {
				row.Threshold = &value
			}
		}
		key := row.ModelID + "/" + row.Rate.Metric + "/" + row.Rate.Tier
		if _, ok := seen[key]; ok {
			result.add(line, "metric", "duplicate_rate", "The provider model, metric, and tier combination is duplicated.")
		} else {
			seen[key] = line
		}
		if len(result.Errors) != before {
			continue
		}
		item, ok := models[row.ModelID]
		if !ok {
			item = &PriceInput{ProviderModelID: row.ModelID, ContextThreshold: row.Threshold, Rates: []pricing.Rate{}}
			models[row.ModelID] = item
			order = append(order, row.ModelID)
		} else if !sameImportThreshold(item.ContextThreshold, row.Threshold) {
			result.add(line, "context_threshold", "inconsistent_threshold", "Every row for a provider model must use the same threshold, including blank.")
			continue
		}
		item.Rates = append(item.Rates, row.Rate)
		result.Rows = append(result.Rows, row)
	}
	if len(result.Rows) == 0 && len(result.Errors) == 0 {
		result.add(2, "", "empty_file", "CSV must contain at least one rate row.")
	}
	if len(models) > priceImportModels {
		result.add(0, "provider_model_id", "too_many_models", "CSV must contain at most 20 provider models.")
	}
	slices.Sort(order)
	for _, modelID := range order {
		item := models[modelID]
		slices.SortFunc(item.Rates, func(a, b pricing.Rate) int { return strings.Compare(a.Metric+"/"+a.Tier, b.Metric+"/"+b.Tier) })
		result.Items = append(result.Items, *item)
	}
	return result
}
func sameImportThreshold(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
