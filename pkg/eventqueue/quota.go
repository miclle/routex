package eventqueue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"regexp"
	"strings"

	"github.com/miclle/routex/pkg/limits"
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
	if err := validQuotaSettlement(entry.Recovery); err != nil {
		return entry, err
	}
	if err := validQuotaRecovery(entry.Bound, entry.Recovery); err != nil {
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
		if entry.Recovery.Tokens != nil || entry.Recovery.Money != nil || entry.Recovery.Currency != "" {
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
	return q.ReserveWithQuotaRecovery(id, payload, policies, bound, QuotaSettlement{}, now)
}

// ReserveWithQuotaRecovery also stores the settlement that is safe if the
// process stops before the first dispatch checkpoint. Admission, fallback,
// economic holds, and recovery evidence are committed atomically.
func (q *Queue) ReserveWithQuotaRecovery(id string, payload []byte, policies []QuotaLimit, bound QuotaBound, recovery QuotaSettlement, now time.Time) error {
	if !q.valid(id, payload) || len(policies) == 0 || len(policies) > 8 {
		return ErrInvalid
	}
	if err := validQuotaBound(bound); err != nil {
		return err
	}
	if err := validQuotaSettlement(recovery); err != nil {
		return err
	}
	if err := validQuotaRecovery(bound, recovery); err != nil {
		return err
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		entry, limitAccounts, err := q.checkQuotaAdmission(tx, id, policies, bound, "", now, true)
		if err != nil {
			return err
		}
		if err := q.applyLimitAdmission(tx, id, payload, entry.AdmittedAt, limitAccounts); err != nil {
			return err
		}
		for _, account := range entry.Accounts {
			if err := applyQuotaCounter(tx, account.Account, "active", *entry, 1); err != nil {
				return err
			}
		}
		entry.Recovery = recovery
		if err := storeQuotaReceipt(tx, *entry); err != nil {
			return err
		}
		return tx.Bucket(quotaEntryBucket).SetSequence(tx.Bucket(quotaEntryBucket).Sequence() + 1)
	})
}

// UpdatePendingQuota replaces the crash fallback and the settlement that is
// safe only if the active process stops before its next checkpoint. It never
// completes the admission or changes its counters while the process is alive.
func (q *Queue) UpdatePendingQuota(id string, fallback []byte, interrupted QuotaSettlement) error {
	if !q.valid(id, fallback) {
		return ErrInvalid
	}
	if err := validQuotaSettlement(interrupted); err != nil {
		return err
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		pending, ready := tx.Bucket(pendingBucket), tx.Bucket(readyBucket)
		key := []byte(id)
		if ready.Get(key) != nil || pending.Get(key) == nil {
			return ErrMissing
		}
		entry, err := readQuotaReceipt(tx, id)
		if err != nil {
			return err
		}
		if entry.State != "active" {
			return ErrQuotaConflict
		}
		if err := validQuotaRecovery(entry.Bound, interrupted); err != nil {
			return err
		}
		entry.Recovery = interrupted
		if err := storeQuotaReceipt(tx, entry); err != nil {
			return err
		}
		return pending.Put(key, fallback)
	})
}

func validQuotaRecovery(bound QuotaBound, recovery QuotaSettlement) error {
	if recovery.Tokens != nil && bound.Tokens != nil && *recovery.Tokens > *bound.Tokens {
		return ErrQuotaBound
	}
	if recovery.Money == nil || bound.Money == nil {
		return nil
	}
	if recovery.Currency != bound.Currency {
		return ErrQuotaCurrency
	}
	observed, _ := quotaUnits(*recovery.Money)
	reserved, _ := quotaUnits(*bound.Money)
	if observed.Cmp(reserved) > 0 {
		return ErrQuotaBound
	}
	return nil
}

// PreflightWithQuota durably checks whether an admission would definitely be
// rejected without consuming RPM, creating a lease/fact, or holding quota. An
// inactive quota journal is modeled at its future activation boundary; only
// final ReserveWithQuota activates accounting and establishes account state.
func (q *Queue) PreflightWithQuota(id string, policies []QuotaLimit, bound QuotaBound, zone string, now time.Time) error {
	if !validKey.MatchString(id) || len(policies) == 0 || len(policies) > 8 || zone == "" {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return ErrInvalid
	}
	if err := validQuotaBound(bound); err != nil {
		return err
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		_, _, err := q.checkQuotaAdmission(tx, id, policies, bound, zone, now, false)
		return err
	})
}

func (q *Queue) checkQuotaAdmission(tx *bolt.Tx, id string, policies []QuotaLimit, bound QuotaBound, previewZone string, now time.Time, establish bool) (*QuotaReceipt, []string, error) {
	version := string(tx.Bucket(limitMetaBucket).Get([]byte("version")))
	if version != "1" && version != "2" {
		return nil, nil, ErrInvalid
	}
	active := version == "2"
	metadata := quotaMetadata{}
	if active {
		var err error
		metadata, err = readQuotaMetadata(tx)
		if err != nil {
			return nil, nil, err
		}
		if previewZone != "" && metadata.TimeZone != previewZone {
			return nil, nil, ErrQuotaConflict
		}
	} else if previewZone == "" {
		return nil, nil, ErrQuotaInactive
	}
	if entries := tx.Bucket(quotaEntryBucket); entries != nil && entries.Get([]byte(id)) != nil {
		return nil, nil, ErrQuotaConflict
	}
	legacy := make([]Limit, len(policies))
	for index, policy := range policies {
		legacy[index] = policy.Limit
	}
	instant, limitAccounts, err := q.checkLimitAdmission(tx, id, legacy, now)
	if err != nil {
		return nil, nil, err
	}
	if active {
		if err := pruneQuota(tx, instant); err != nil {
			return nil, nil, err
		}
		if tx.Bucket(quotaEntryBucket).Sequence() >= uint64(q.quotaCapacity) {
			return nil, nil, ErrFull
		}
	} else {
		metadata = quotaMetadata{TimeZone: previewZone, CoverageStart: instant}
	}
	start, end, err := quotaMonth(instant, metadata.TimeZone)
	if err != nil {
		return nil, nil, err
	}
	if instant > math.MaxInt64-int64(7*24*time.Hour) {
		return nil, nil, ErrInvalid
	}
	entry := &QuotaReceipt{RequestID: id, AdmittedAt: instant, MonthStart: start, MonthEnd: end, Bound: bound, State: "active"}
	for _, policy := range policies {
		if err := checkQuota(tx, policy, bound, metadata, instant, start, active, establish); err != nil {
			return nil, nil, err
		}
		entry.Accounts = append(entry.Accounts, quotaAccount{Account: policy.Account, Revision: policy.Revision})
	}
	return entry, limitAccounts, nil
}

// Aggregate monthly accounts use bounded ASCII identities. Team-member pair
// digests require the separate trusted current-membership proof; an account
// prefix alone never enables their monthly modes.
func monthlyBehaviorAccount(account string) bool {
	if suffix, ok := strings.CutPrefix(account, "project_"); ok {
		return strings.HasPrefix(suffix, "prj_") && len(suffix) > len("prj_") && len(suffix) <= 30 && validKey.MatchString(suffix)
	}
	if strings.HasPrefix(account, "user_") && len(account) > len("user_") {
		return true
	}
	suffix, ok := strings.CutPrefix(account, "team_")
	return ok && !strings.HasPrefix(account, "team_member_") && len(suffix) > 0 && len(suffix) <= 30 && validKey.MatchString(suffix)
}

func projectKeyBehaviorAccount(account string) bool {
	suffix, ok := strings.CutPrefix(account, "key_")
	return ok && strings.HasPrefix(suffix, "pky_") && len(suffix) > len("pky_") && len(suffix) <= 30 && validKey.MatchString(suffix)
}

func personalKeyBehaviorAccount(account string) bool {
	suffix, ok := strings.CutPrefix(account, "key_")
	return ok && len(suffix) > 0 && len(suffix) <= 30 && validKey.MatchString(suffix)
}

func checkQuota(tx *bolt.Tx, policy QuotaLimit, bound QuotaBound, metadata quotaMetadata, instant, monthStart int64, activeJournal, establish bool) error {
	tokenBehavior, tokenErr := limits.CanonicalMonthlyBehavior(policy.TokensMonthBehavior)
	moneyBehavior, moneyErr := limits.CanonicalMonthlyBehavior(policy.MoneyMonthBehavior)
	if tokenErr != nil || moneyErr != nil || policy.PersonalKey && policy.ProjectKey || policy.TeamMember && (policy.PersonalKey || policy.ProjectKey) || (tokenBehavior != "" || moneyBehavior != "") && !monthlyBehaviorAccount(policy.Account) && (!policy.PersonalKey || !personalKeyBehaviorAccount(policy.Account)) && (!policy.ProjectKey || !projectKeyBehaviorAccount(policy.Account)) && (!policy.TeamMember || !teamMemberBehaviorAccount(policy.Account)) {
		return ErrInvalid
	}
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
	if activeJournal {
		accountBucket := tx.Bucket(quotaAccountBucket)
		var prior int64
		if raw := accountBucket.Get([]byte(policy.Account)); raw != nil {
			if json.Unmarshal(raw, &prior) != nil || prior != created {
				return ErrQuotaConflict
			}
		} else if establish {
			raw, _ := json.Marshal(created)
			if err := accountBucket.Put([]byte(policy.Account), raw); err != nil {
				return err
			}
		}
	}
	active := quotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
	if activeJournal {
		var err error
		active, err = readQuotaCounter(tx, policy.Account, "active")
		if err != nil {
			return err
		}
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
		soft := window.name == monthWindow(monthStart) && tokenBehavior == limits.MonthlyBehaviorAlertOnly
		if *window.ceiling == 0 && !soft {
			return ErrQuotaTokens
		}
		if window.start < metadata.CoverageStart && created < metadata.CoverageStart {
			return ErrQuotaCoverage
		}
		if bound.Tokens == nil {
			return ErrQuotaBound
		}
		used := quotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
		if activeJournal {
			var err error
			used, err = readQuotaCounter(tx, policy.Account, window.name)
			if err != nil {
				return err
			}
		}
		if used.TokensUnknown > 0 || active.TokensUnknown > 0 {
			return ErrQuotaUnknown
		}
		if !soft {
			remaining := *window.ceiling
			for _, amount := range []int64{used.TokensUsed, used.TokensHeld, active.TokensUsed, active.TokensHeld, *bound.Tokens} {
				if amount > remaining {
					return ErrQuotaTokens
				}
				remaining -= amount
			}
		}
	}
	if policy.MoneyMonth != nil {
		constrained = true
		maximum, err := quotaUnits(*policy.MoneyMonth)
		if err != nil {
			return err
		}
		soft := moneyBehavior == limits.MonthlyBehaviorAlertOnly
		if maximum.Sign() == 0 && !soft {
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
		used := quotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
		if activeJournal {
			var err error
			used, err = readQuotaCounter(tx, policy.Account, monthWindow(monthStart))
			if err != nil {
				return err
			}
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
		if maximum.Cmp(reservation) < 0 && !soft {
			return ErrQuotaMoney
		}
	}
	if constrained {
		invalidBound := activeJournal && tx.Bucket(quotaInvalidBoundBucket).Get([]byte(bound.Revision)) != nil
		if bound.Revision == "" || invalidBound {
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
		entry.Recovery = QuotaSettlement{}
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
		result.AsOf = time.Unix(0, instant).UTC()
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
