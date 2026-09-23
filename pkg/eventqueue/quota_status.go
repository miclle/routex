package eventqueue

import (
	"errors"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

type QuotaStatus struct {
	Active        bool       `json:"active"`
	TimeZone      string     `json:"time_zone"`
	CoverageStart *time.Time `json:"coverage_start"`
}

func (q *Queue) QuotaStatus() (*QuotaStatus, error) {
	result := &QuotaStatus{}
	err := q.db.View(func(tx *bolt.Tx) error {
		metadata, err := readQuotaMetadata(tx)
		if errors.Is(err, ErrQuotaInactive) {
			return nil
		}
		if err != nil {
			return err
		}
		instant := time.Unix(0, metadata.CoverageStart).UTC()
		result.Active = true
		result.TimeZone = metadata.TimeZone
		result.CoverageStart = &instant
		return nil
	})
	return result, err
}

// HasLiveMoneyHolds protects currency reconfiguration. Unknown unbounded money
// is also a hold; changing the display currency cannot make it free.
func (q *Queue) HasLiveMoneyHolds(now time.Time) (bool, error) {
	held := false
	err := q.db.Update(func(tx *bolt.Tx) error {
		if _, err := readQuotaMetadata(tx); errors.Is(err, ErrQuotaInactive) {
			return nil
		} else if err != nil {
			return err
		}
		instant, err := logicalLimitTime(tx, now)
		if err != nil {
			return err
		}
		if err := pruneQuota(tx, instant); err != nil {
			return err
		}
		return tx.Bucket(quotaCounterBucket).ForEach(func(key, value []byte) error {
			account, window, ok := strings.Cut(string(key), "\x00")
			if !ok {
				return ErrInvalid
			}
			if window != "active" && !strings.HasPrefix(window, "month_") {
				return nil
			}
			counter, err := readQuotaCounter(tx, account, window)
			if err != nil {
				return err
			}
			held = held || counter.MoneyUnknown > 0 || len(counter.MoneyHeld) > 0
			return nil
		})
	})
	return held, err
}
