package eventqueue

import (
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var (
	ErrQuotaInactive = errors.New("quota accounting is not active")
	ErrQuotaRequired = errors.New("quota-aware admission is required")
	ErrQuotaCoverage = errors.New("quota history coverage is incomplete")
	ErrQuotaUnknown  = errors.New("quota history contains unknown usage")
	ErrQuotaBound    = errors.New("a proven reservation bound is unavailable")
	ErrQuotaTokens   = errors.New("token quota exceeded")
	ErrQuotaMoney    = errors.New("monetary quota exceeded")
	ErrQuotaCurrency = errors.New("quota currencies are incompatible")
	ErrQuotaConflict = errors.New("quota settlement conflicts with accepted receipt")
)

const quotaHistoryCapacity = 1000000

var (
	quotaEntryBucket        = []byte("quota-entries-v2")
	quotaCounterBucket      = []byte("quota-counters-v2")
	quotaExpiryBucket       = []byte("quota-expiry-v2")
	quotaAccountBucket      = []byte("quota-accounts-v2")
	quotaInvalidBoundBucket = []byte("quota-invalid-bounds-v2")
)
var quotaBuckets = [][]byte{quotaEntryBucket, quotaCounterBucket, quotaExpiryBucket, quotaAccountBucket, quotaInvalidBoundBucket}

type QuotaLimit struct {
	Limit
	Revision string
	// CreatedAt is trusted resource creation metadata, never a caller override.
	// Zero means unknown prehistory. Rotation uses the original Key account date.
	CreatedAt                            time.Time
	Tokens5H, Tokens7D, TokensMonth, TPM *int64
	MoneyMonth                           *string
	Currency                             string
}
type QuotaBound struct {
	Tokens        *int64  `json:"tokens"`
	Money         *string `json:"money"`
	Currency      string  `json:"currency"`
	Revision      string  `json:"revision"`
	PriceRevision string  `json:"price_revision"`
	BasisDigest   string  `json:"basis_digest"`
}
type QuotaSettlement struct {
	// Nil means unproven, not zero. The service establishes completeness separately
	// for each dimension, independent of HTTP success or cancellation.
	Tokens   *int64  `json:"tokens"`
	Money    *string `json:"money"`
	Currency string  `json:"currency"`
}
type QuotaReceipt struct {
	RequestID        string          `json:"request_id"`
	AdmittedAt       int64           `json:"admitted_at"`
	MonthStart       int64           `json:"month_start"`
	MonthEnd         int64           `json:"month_end"`
	Accounts         []quotaAccount  `json:"accounts"`
	Bound            QuotaBound      `json:"bound"`
	State            string          `json:"state"`
	Actual           QuotaSettlement `json:"actual"`
	SettlementDigest string          `json:"settlement_digest"`
	Overrun          bool            `json:"overrun"`
}
type quotaAccount struct{ Account, Revision string }
type quotaMetadata struct {
	TimeZone      string
	CoverageStart int64
}
type quotaCounter struct {
	TokensUsed    int64             `json:"tokens_used"`
	TokensHeld    int64             `json:"tokens_held"`
	TokensUnknown int64             `json:"tokens_unknown"`
	MoneyUsed     map[string]string `json:"money_used"`
	MoneyHeld     map[string]string `json:"money_held"`
	MoneyUnknown  int64             `json:"money_unknown"`
}

// QuotaUsage keeps settled facts distinct from conservative holds. Values are
// decimal strings; unknown counts identify unbounded holds, not free requests.
type QuotaUsage struct {
	TokensUsed, TokensHeld, TokensUnknown int64
	MoneyUsed, MoneyHeld                  map[string]string
	MoneyUnknown                          int64
}
type AccountQuotaUsage struct {
	CoverageStart                       time.Time
	TimeZone                            string
	Active                              QuotaUsage
	Minute, FiveHours, SevenDays, Month QuotaUsage
}

var quotaDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,59})(\.[0-9]{1,18})?$`)

func quotaUnits(value string) (*big.Int, error) {
	if !quotaDecimal.MatchString(value) {
		return nil, ErrInvalid
	}
	whole, fraction, _ := strings.Cut(value, ".")
	digits := whole + fraction + strings.Repeat("0", 18-len(fraction))
	number, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, ErrInvalid
	}
	return number, nil
}
func quotaDecimalValue(units *big.Int) string {
	value := units.String()
	if len(value) <= 18 {
		value = strings.Repeat("0", 19-len(value)) + value
	}
	return strings.TrimRight(strings.TrimRight(value[:len(value)-18]+"."+value[len(value)-18:], "0"), ".")
}
func cloneReceipt(receipt QuotaReceipt) QuotaReceipt {
	raw, _ := json.Marshal(receipt)
	var copy QuotaReceipt
	_ = json.Unmarshal(raw, &copy)
	return copy
}
func quotaMonth(instant int64, zone string) (int64, int64, error) {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return 0, 0, ErrInvalid
	}
	now := time.Unix(0, instant).In(location)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	end := start.AddDate(0, 1, 0)
	if start.UnixNano() < 0 || end.UnixNano() <= instant {
		return 0, 0, ErrInvalid
	}
	return start.UnixNano(), end.UnixNano(), nil
}
