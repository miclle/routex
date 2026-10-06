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
	if err := validateQuotaUsageBatchAccounts(accounts); err != nil {
		return nil, err
	}
	result := &QuotaUsageBatch{Accounts: make(map[string]AccountQuotaUsage, len(accounts))}
	err := q.db.Update(func(tx *bolt.Tx) error {
		frame, err := sampleQuotaUsageFrame(tx, now)
		if err != nil || !frame.active {
			return err
		}
		result.Active = true
		result.AsOf = frame.asOf
		result.CoverageStart = frame.coverageStart
		result.TimeZone = frame.timeZone
		for _, account := range accounts {
			usage, err := sampleAccountQuotaUsage(tx, account, frame)
			if err != nil {
				return err
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

func validateQuotaUsageBatchAccounts(accounts []string) error {
	if len(accounts) == 0 || len(accounts) > MaxQuotaUsageBatchAccounts {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		if !validKey.MatchString(account) || seen[account] {
			return ErrInvalid
		}
		seen[account] = true
	}
	return nil
}

// quotaUsageFrame is private transaction-local sampling metadata. Legacy usage
// reads share it without reading the account registration bucket.
type quotaUsageFrame struct {
	active              bool
	asOf, coverageStart time.Time
	timeZone            string
	monthStart          int64
}

func sampleQuotaUsageFrame(tx *bolt.Tx, now time.Time) (quotaUsageFrame, error) {
	frame := quotaUsageFrame{}
	metadata, err := readQuotaMetadata(tx)
	if errors.Is(err, ErrQuotaInactive) {
		return frame, nil
	}
	if err != nil {
		return frame, err
	}
	instant, err := logicalLimitTime(tx, now)
	if err != nil {
		return frame, err
	}
	if err := pruneQuota(tx, instant); err != nil {
		return frame, err
	}
	start, _, err := quotaMonth(instant, metadata.TimeZone)
	if err != nil {
		return frame, err
	}
	return quotaUsageFrame{
		active:        true,
		asOf:          time.Unix(0, instant).UTC(),
		coverageStart: time.Unix(0, metadata.CoverageStart).UTC(),
		timeZone:      metadata.TimeZone,
		monthStart:    start,
	}, nil
}

func sampleAccountQuotaUsage(tx *bolt.Tx, account string, frame quotaUsageFrame) (AccountQuotaUsage, error) {
	usage := AccountQuotaUsage{AsOf: frame.asOf, CoverageStart: frame.coverageStart, TimeZone: frame.timeZone}
	for _, window := range []struct {
		name   string
		target *QuotaUsage
	}{{"active", &usage.Active}, {"minute", &usage.Minute}, {"five_hours", &usage.FiveHours}, {"seven_days", &usage.SevenDays}, {monthWindow(frame.monthStart), &usage.Month}} {
		counter, err := readQuotaCounter(tx, account, window.name)
		if err != nil {
			return AccountQuotaUsage{}, err
		}
		*window.target = quotaCounterView(counter)
	}
	return usage, nil
}
