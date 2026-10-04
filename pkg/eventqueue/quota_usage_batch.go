package eventqueue

import (
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
)

const MaxQuotaUsageBatchAccounts = 101

// QuotaUsageBatch samples all selected accounts at one logical instant. An
// inactive journal has no usage snapshot; absence never means known zero usage.
type QuotaUsageBatch struct {
	Active        bool
	AsOf          time.Time
	CoverageStart time.Time
	TimeZone      string
	Accounts      map[string]AccountQuotaUsage
}

// AccountQuotaUsageBatch applies the same clock/pruning rules as a single
// account read, once for a bounded batch. It does not admit or settle any call.
func (q *Queue) AccountQuotaUsageBatch(accounts []string, now time.Time) (*QuotaUsageBatch, error) {
	if len(accounts) == 0 || len(accounts) > MaxQuotaUsageBatchAccounts {
		return nil, ErrInvalid
	}
	seen := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		if !validKey.MatchString(account) || seen[account] {
			return nil, ErrInvalid
		}
		seen[account] = true
	}
	result := &QuotaUsageBatch{Accounts: make(map[string]AccountQuotaUsage, len(accounts))}
	err := q.db.Update(func(tx *bolt.Tx) error {
		metadata, err := readQuotaMetadata(tx)
		if errors.Is(err, ErrQuotaInactive) {
			return nil
		}
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
		result.Active = true
		result.AsOf = time.Unix(0, instant).UTC()
		result.CoverageStart = time.Unix(0, metadata.CoverageStart).UTC()
		result.TimeZone = metadata.TimeZone
		for _, account := range accounts {
			usage := AccountQuotaUsage{AsOf: result.AsOf, CoverageStart: result.CoverageStart, TimeZone: result.TimeZone}
			for _, window := range []struct {
				name   string
				target *QuotaUsage
			}{{"active", &usage.Active}, {"minute", &usage.Minute}, {"five_hours", &usage.FiveHours}, {"seven_days", &usage.SevenDays}, {monthWindow(start), &usage.Month}} {
				counter, err := readQuotaCounter(tx, account, window.name)
				if err != nil {
					return err
				}
				*window.target = quotaCounterView(counter)
			}
			result.Accounts[account] = usage
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
