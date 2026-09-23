package eventqueue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"regexp"
	"time"

	bolt "go.etcd.io/bbolt"
)

var quotaCurrency = regexp.MustCompile(`^[A-Z]{3}$`)
var quotaDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// EnableQuota is an explicit deployment boundary. Opening an old journal alone
// never asserts complete coverage while callers still use legacy admissions.
func (q *Queue) EnableQuota(zone string, now time.Time) error {
	if zone == "" {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return ErrInvalid
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		if string(tx.Bucket(limitMetaBucket).Get([]byte("version"))) == "2" {
			metadata, err := readQuotaMetadata(tx)
			if err != nil {
				return err
			}
			if metadata.TimeZone != zone {
				return ErrQuotaConflict
			}
			return nil
		}
		instant, err := logicalLimitTime(tx, now)
		if err != nil {
			return err
		}
		for _, name := range quotaBuckets {
			if tx.Bucket(name) != nil {
				return ErrInvalid
			}
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		metadata, _ := json.Marshal(quotaMetadata{TimeZone: zone, CoverageStart: instant})
		if err := tx.Bucket(limitMetaBucket).Put([]byte("quota"), metadata); err != nil {
			return err
		}
		return tx.Bucket(limitMetaBucket).Put([]byte("version"), []byte("2"))
	})
}
func readQuotaMetadata(tx *bolt.Tx) (quotaMetadata, error) {
	metadata := quotaMetadata{}
	if string(tx.Bucket(limitMetaBucket).Get([]byte("version"))) != "2" {
		return metadata, ErrQuotaInactive
	}
	if json.Unmarshal(tx.Bucket(limitMetaBucket).Get([]byte("quota")), &metadata) != nil || metadata.CoverageStart <= 0 || metadata.TimeZone == "" {
		return metadata, ErrInvalid
	}
	if _, err := time.LoadLocation(metadata.TimeZone); err != nil {
		return metadata, ErrInvalid
	}
	return metadata, nil
}
func readQuotaReceipt(tx *bolt.Tx, id string) (QuotaReceipt, error) {
	entry := QuotaReceipt{}
	raw := tx.Bucket(quotaEntryBucket).Get([]byte(id))
	if raw == nil {
		return entry, ErrMissing
	}
	if json.Unmarshal(raw, &entry) != nil || entry.RequestID != id || !validKey.MatchString(id) || entry.AdmittedAt <= 0 || entry.AdmittedAt > math.MaxInt64-int64(7*24*time.Hour) || entry.MonthStart < 0 || entry.MonthEnd <= entry.AdmittedAt || len(entry.Accounts) == 0 || len(entry.Accounts) > 8 {
		return entry, ErrInvalid
	}
	if err := validQuotaBound(entry.Bound); err != nil {
		return entry, err
	}
	if err := validQuotaSettlement(entry.Actual); err != nil {
		return entry, err
	}
	seen := map[string]bool{}
	for _, account := range entry.Accounts {
		if !validKey.MatchString(account.Account) || !validKey.MatchString(account.Revision) || seen[account.Account] {
			return entry, ErrInvalid
		}
		seen[account.Account] = true
	}
	switch entry.State {
	case "active":
		if entry.SettlementDigest != "" || entry.Actual.Tokens != nil || entry.Actual.Money != nil {
			return entry, ErrInvalid
		}
	case "complete", "interrupted":
		if entry.State == "interrupted" && (entry.Actual.Tokens != nil || entry.Actual.Money != nil) {
			return entry, ErrInvalid
		}
		if !quotaDigest.MatchString(entry.SettlementDigest) {
			return entry, ErrInvalid
		}
	default:
		return entry, ErrInvalid
	}
	return entry, nil
}
func validQuotaBound(bound QuotaBound) error {
	if bound.Tokens != nil && *bound.Tokens < 0 {
		return ErrInvalid
	}
	if bound.Money != nil {
		if _, err := quotaUnits(*bound.Money); err != nil {
			return err
		}
		if !quotaCurrency.MatchString(bound.Currency) {
			return ErrInvalid
		}
	}
	if bound.Revision != "" && !validKey.MatchString(bound.Revision) || len(bound.PriceRevision) > 64 || bound.BasisDigest != "" && !quotaDigest.MatchString(bound.BasisDigest) {
		return ErrInvalid
	}
	return nil
}
func validQuotaSettlement(actual QuotaSettlement) error {
	if actual.Tokens != nil && *actual.Tokens < 0 {
		return ErrInvalid
	}
	if actual.Money != nil {
		if _, err := quotaUnits(*actual.Money); err != nil {
			return err
		}
		if !quotaCurrency.MatchString(actual.Currency) {
			return ErrInvalid
		}
	}
	return nil
}
func storeQuotaReceipt(tx *bolt.Tx, entry QuotaReceipt) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return ErrInvalid
	}
	return tx.Bucket(quotaEntryBucket).Put([]byte(entry.RequestID), raw)
}

// ReserveWithQuota commits all parent/child checks, economic holds, RPM, leases
// and the fallback fact in one synchronous transaction. No partial debit occurs.
func (q *Queue) ReserveWithQuota(id string, payload []byte, policies []QuotaLimit, bound QuotaBound, now time.Time) error {
	if !q.valid(id, payload) || len(policies) == 0 || len(policies) > 8 {
		return ErrInvalid
	}
	if err := validQuotaBound(bound); err != nil {
		return err
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		metadata, err := readQuotaMetadata(tx)
		if err != nil {
			return err
		}
		if tx.Bucket(quotaEntryBucket).Get([]byte(id)) != nil {
			return ErrQuotaConflict
		}
		legacy := make([]Limit, len(policies))
		for index, policy := range policies {
			legacy[index] = policy.Limit
		}
		if err := q.reserveWithLimits(tx, id, payload, legacy, now); err != nil {
			return err
		}
		instant, err := logicalLimitTime(tx, now)
		if err != nil {
			return err
		}
		if err := pruneQuota(tx, instant); err != nil {
			return err
		}
		if tx.Bucket(quotaEntryBucket).Sequence() >= uint64(q.quotaCapacity) {
			return ErrFull
		}
		start, end, err := quotaMonth(instant, metadata.TimeZone)
		if err != nil {
			return err
		}
		if instant > math.MaxInt64-int64(7*24*time.Hour) {
			return ErrInvalid
		}
		entry := QuotaReceipt{RequestID: id, AdmittedAt: instant, MonthStart: start, MonthEnd: end, Bound: bound, State: "active"}
		for _, policy := range policies {
			if err := checkQuota(tx, policy, bound, metadata, instant, start); err != nil {
				return err
			}
			entry.Accounts = append(entry.Accounts, quotaAccount{Account: policy.Account, Revision: policy.Revision})
		}
		for _, account := range entry.Accounts {
			if err := applyQuotaCounter(tx, account.Account, "active", entry, 1); err != nil {
				return err
			}
		}
		if err := storeQuotaReceipt(tx, entry); err != nil {
			return err
		}
		return tx.Bucket(quotaEntryBucket).SetSequence(tx.Bucket(quotaEntryBucket).Sequence() + 1)
	})
}
func checkQuota(tx *bolt.Tx, policy QuotaLimit, bound QuotaBound, metadata quotaMetadata, instant, monthStart int64) error {
	if !validKey.MatchString(policy.Revision) {
		return ErrInvalid
	}
	created := int64(0)
	if !policy.CreatedAt.IsZero() {
		created = policy.CreatedAt.UnixNano()
		if created <= 0 || created > instant {
			return ErrInvalid
		}
	}
	accountBucket := tx.Bucket(quotaAccountBucket)
	var prior int64
	if raw := accountBucket.Get([]byte(policy.Account)); raw != nil {
		if json.Unmarshal(raw, &prior) != nil || prior != created {
			return ErrQuotaConflict
		}
	} else {
		raw, _ := json.Marshal(created)
		if err := accountBucket.Put([]byte(policy.Account), raw); err != nil {
			return err
		}
	}
	active, err := readQuotaCounter(tx, policy.Account, "active")
	if err != nil {
		return err
	}
	constrained := false
	for _, window := range []struct {
		name    string
		start   int64
		ceiling *int64
	}{{"minute", instant - int64(time.Minute), policy.TPM}, {"five_hours", instant - int64(5*time.Hour), policy.Tokens5H}, {"seven_days", instant - int64(7*24*time.Hour), policy.Tokens7D}, {monthWindow(monthStart), monthStart, policy.TokensMonth}} {
		if window.ceiling == nil {
			continue
		}
		constrained = true
		if *window.ceiling < 0 {
			return ErrInvalid
		}
		if *window.ceiling == 0 {
			return ErrQuotaTokens
		}
		if window.start < metadata.CoverageStart && created < metadata.CoverageStart {
			return ErrQuotaCoverage
		}
		if bound.Tokens == nil {
			return ErrQuotaBound
		}
		used, err := readQuotaCounter(tx, policy.Account, window.name)
		if err != nil {
			return err
		}
		if used.TokensUnknown > 0 || active.TokensUnknown > 0 {
			return ErrQuotaUnknown
		}
		remaining := *window.ceiling
		for _, amount := range []int64{used.TokensUsed, used.TokensHeld, active.TokensUsed, active.TokensHeld, *bound.Tokens} {
			if amount > remaining {
				return ErrQuotaTokens
			}
			remaining -= amount
		}
	}
	if policy.MoneyMonth != nil {
		constrained = true
		maximum, err := quotaUnits(*policy.MoneyMonth)
		if err != nil {
			return err
		}
		if maximum.Sign() == 0 {
			return ErrQuotaMoney
		}
		if !quotaCurrency.MatchString(policy.Currency) {
			return ErrInvalid
		}
		if monthStart < metadata.CoverageStart && created < metadata.CoverageStart {
			return ErrQuotaCoverage
		}
		if bound.Money == nil {
			return ErrQuotaBound
		}
		if bound.Currency != policy.Currency {
			return ErrQuotaCurrency
		}
		used, err := readQuotaCounter(tx, policy.Account, monthWindow(monthStart))
		if err != nil {
			return err
		}
		if used.MoneyUnknown > 0 || active.MoneyUnknown > 0 {
			return ErrQuotaUnknown
		}
		for _, totals := range []map[string]string{used.MoneyUsed, used.MoneyHeld, active.MoneyUsed, active.MoneyHeld} {
			for currency, raw := range totals {
				if currency != policy.Currency {
					return ErrQuotaCurrency
				}
				amount, ok := new(big.Int).SetString(raw, 10)
				if !ok {
					return ErrInvalid
				}
				maximum.Sub(maximum, amount)
			}
		}
		reservation, _ := quotaUnits(*bound.Money)
		if maximum.Cmp(reservation) < 0 {
			return ErrQuotaMoney
		}
	}
	if constrained {
		if bound.Revision == "" || tx.Bucket(quotaInvalidBoundBucket).Get([]byte(bound.Revision)) != nil {
			return ErrQuotaBound
		}
	}
	return nil
}

// CompleteQuota settles each proven dimension separately. Missing dimensions
// retain their bound (or an explicit unbounded unknown hold). It is idempotent
// across primary-SQL acknowledgment while the quota receipt is retained.
func (q *Queue) CompleteQuota(id string, payload []byte, actual QuotaSettlement, now time.Time) (*QuotaReceipt, error) {
	if !q.valid(id, payload) {
		return nil, ErrInvalid
	}
	if err := validQuotaSettlement(actual); err != nil {
		return nil, err
	}
	digest := quotaSettlementDigest(actual, payload)
	var result QuotaReceipt
	err := q.db.Update(func(tx *bolt.Tx) error {
		if _, err := readQuotaMetadata(tx); err != nil {
			return err
		}
		entry, err := readQuotaReceipt(tx, id)
		if err != nil {
			return err
		}
		if entry.State != "active" {
			if entry.SettlementDigest != digest {
				return ErrQuotaConflict
			}
			result = entry
			return nil
		}
		if actual.Money != nil && entry.Bound.Money != nil && actual.Currency != entry.Bound.Currency {
			return ErrQuotaCurrency
		}
		instant, err := logicalLimitTime(tx, now)
		if err != nil {
			return err
		}
		for _, account := range entry.Accounts {
			if err := applyQuotaCounter(tx, account.Account, "active", entry, -1); err != nil {
				return err
			}
		}
		entry.State = "complete"
		entry.Actual = actual
		entry.SettlementDigest = digest
		if actual.Tokens != nil && entry.Bound.Tokens != nil && *actual.Tokens > *entry.Bound.Tokens {
			entry.Overrun = true
		}
		if actual.Money != nil && entry.Bound.Money != nil {
			observed, _ := quotaUnits(*actual.Money)
			reserved, _ := quotaUnits(*entry.Bound.Money)
			entry.Overrun = entry.Overrun || observed.Cmp(reserved) > 0
		}
		if entry.Overrun && entry.Bound.Revision != "" {
			if err := tx.Bucket(quotaInvalidBoundBucket).Put([]byte(entry.Bound.Revision), []byte(id)); err != nil {
				return err
			}
		}
		if err := storeQuotaReceipt(tx, entry); err != nil {
			return err
		}
		if err := projectTerminalQuota(tx, entry, instant); err != nil {
			return err
		}
		if err := q.complete(tx, id, payload); err != nil {
			return err
		}
		result = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	copy := cloneReceipt(result)
	return &copy, nil
}
func (q *Queue) QuotaReceipt(id string) (*QuotaReceipt, error) {
	var result QuotaReceipt
	err := q.db.View(func(tx *bolt.Tx) error {
		if _, err := readQuotaMetadata(tx); err != nil {
			return err
		}
		var err error
		result, err = readQuotaReceipt(tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	copy := cloneReceipt(result)
	return &copy, nil
}
func (q *Queue) AccountQuotaUsage(account string, now time.Time) (*AccountQuotaUsage, error) {
	if !validKey.MatchString(account) {
		return nil, ErrInvalid
	}
	result := &AccountQuotaUsage{}
	err := q.db.Update(func(tx *bolt.Tx) error {
		metadata, err := readQuotaMetadata(tx)
		if err != nil {
			return err
		}
		instant, err := logicalLimitTime(tx, now)
		if err != nil {
			return err
		}
		if err := pruneQuota(tx, instant); err != nil {
			return err
		}
		start, _, err := quotaMonth(instant, metadata.TimeZone)
		if err != nil {
			return err
		}
		result.TimeZone = metadata.TimeZone
		result.CoverageStart = time.Unix(0, metadata.CoverageStart).UTC()
		for _, item := range []struct {
			name   string
			target *QuotaUsage
		}{{"active", &result.Active}, {"minute", &result.Minute}, {"five_hours", &result.FiveHours}, {"seven_days", &result.SevenDays}, {monthWindow(start), &result.Month}} {
			counter, err := readQuotaCounter(tx, account, item.name)
			if err != nil {
				return err
			}
			*item.target = quotaCounterView(counter)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func quotaSettlementDigest(actual QuotaSettlement, payload []byte) string {
	actualJSON, _ := json.Marshal(actual)
	digestBytes := sha256.Sum256(append(append(append([]byte{}, actualJSON...), 0), payload...))
	return hex.EncodeToString(digestBytes[:])
}
