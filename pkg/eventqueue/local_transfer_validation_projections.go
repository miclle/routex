package eventqueue

import (
	"context"
	"encoding/binary"
	"maps"
	"math"
	"strings"
	"time"
)

func (state *localTransferSemanticState) validateQuotaProjections(ctx context.Context) error {
	stored, live := map[string]QuotaCounter{}, map[string]QuotaCounter{}
	references := map[string]struct{}{}
	for _, raw := range state.buckets["quota-expiry-v2"].Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(raw.Key) < 10 || len(raw.Value) != 0 {
			return ErrInvalid
		}
		end := binary.BigEndian.Uint64(raw.Key[:8])
		if end == 0 || end > math.MaxInt64 {
			return ErrInvalid
		}
		if err := state.reference(); err != nil {
			return err
		}
		switch raw.Key[8] {
		case 'c':
			parts := strings.Split(string(raw.Key[9:]), "\x00")
			if len(parts) != 3 {
				return ErrInvalid
			}
			receipt, exists := state.receipts[parts[2]]
			if !exists || !localTransferReceiptAccount(receipt, parts[0]) || quotaWindowEnds(receipt)[parts[1]] != int64(end) {
				return ErrInvalid
			}
			key := string(counterKey(parts[0], parts[1]))
			if err := localTransferAddContribution(stored, key, receipt); err != nil {
				return err
			}
			if int64(end) > state.result.CutoverUnixNano {
				if err := localTransferAddContribution(live, key, receipt); err != nil {
					return err
				}
			}
		case 'r':
			receipt, exists := state.receipts[string(raw.Key[9:])]
			if !exists || localTransferReceiptExpiry(receipt) != int64(end) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		references[string(raw.Key)] = struct{}{}
	}
	for _, receipt := range state.receipts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, exists := references[string(expiryKey(localTransferReceiptExpiry(receipt), 'r', receipt.RequestID))]; !exists {
			return ErrInvalid
		}
		for window, end := range quotaWindowEnds(receipt) {
			// A terminal write can omit already-expired windows; pruning can
			// remove expired references. Unexpired original refs are mandatory.
			if end <= *state.result.Header.ClockHighWater {
				continue
			}
			for _, account := range receipt.Accounts {
				key := expiryKey(end, 'c', account.Account+"\x00"+window+"\x00"+receipt.RequestID)
				if _, exists := references[string(key)]; !exists {
					return ErrInvalid
				}
			}
		}
	}
	localTransferRemoveEmptyCounters(stored)
	localTransferRemoveEmptyCounters(live)
	actual := map[string]QuotaCounter{}
	for _, raw := range state.buckets["quota-counters-v2"].Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		var counter QuotaCounter
		if localTransferDecodeJSON(raw.Value, &counter) != nil {
			return ErrInvalid
		}
		expected, exists := stored[string(raw.Key)]
		if !exists || !localTransferCountersEqual(expected, counter) || ValidateQuotaCounter(counter) != nil {
			return ErrInvalid
		}
		actual[string(raw.Key)] = counter
	}
	if len(actual) != len(stored) {
		return ErrInvalid
	}
	state.result.StoredQuota = localTransferQuotaRows(stored)
	state.result.LiveQuota = localTransferQuotaRows(live)
	return nil
}

func (state *localTransferSemanticState) validateRPMProjections(ctx context.Context) error {
	bucket := state.buckets["rpm-v1"]
	if bucket.Sequence != uint64(len(bucket.Entries)) || bucket.Sequence > limitHistoryCapacity {
		return ErrInvalid
	}
	stored, live := map[string]int64{}, map[string]int64{}
	events := map[string]struct{}{}
	for _, raw := range bucket.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(raw.Key) < 11 {
			return ErrInvalid
		}
		instant := binary.BigEndian.Uint64(raw.Key[:8])
		parts := strings.Split(string(raw.Key[8:]), ":")
		if instant == 0 || instant > uint64(*state.result.Header.ClockHighWater) || len(parts) != 2 || !validKey.MatchString(parts[0]) || parts[1] != string(raw.Value) {
			return ErrInvalid
		}
		if err := state.account(parts[1]); err != nil {
			return err
		}
		if err := state.reference(); err != nil {
			return err
		}
		// EnableQuota can reuse a legacy logical timestamp after equal time
		// or clock rollback. Equality cannot identify an event's incarnation.
		if int64(instant) > state.result.Header.Quota.CoverageStart {
			receipt, exists := state.receipts[parts[0]]
			if exists && (receipt.AdmittedAt != int64(instant) || !localTransferReceiptAccount(receipt, parts[1])) {
				return ErrInvalid
			}
			if !exists {
				retirement, err := localTransferEconomicRetirement(int64(instant), state.result.Header.Quota.TimeZone)
				if err != nil || retirement > *state.result.Header.ClockHighWater {
					return ErrInvalid
				}
			}
		}
		events[string(raw.Key)] = struct{}{}
		stored[parts[1]]++
		// Filtering does not mutate the original retained RPM events/counts.
		if int64(instant) > state.result.CutoverUnixNano-int64(time.Minute) {
			live[parts[1]]++
		}
	}
	counts := state.buckets["rpm-counts-v1"].Entries
	if len(counts) != len(stored) {
		return ErrInvalid
	}
	for _, raw := range counts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(raw.Value) != 8 || !validKey.Match(raw.Key) {
			return ErrInvalid
		}
		value := binary.BigEndian.Uint64(raw.Value)
		if value == 0 || value > math.MaxInt64 || int64(value) != stored[string(raw.Key)] {
			return ErrInvalid
		}
	}
	for _, receipt := range state.receipts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if receipt.AdmittedAt <= *state.result.Header.ClockHighWater-int64(time.Minute) {
			continue
		}
		for _, account := range receipt.Accounts {
			key := make([]byte, 8)
			binary.BigEndian.PutUint64(key, uint64(receipt.AdmittedAt))
			key = append(key, []byte(receipt.RequestID+":"+account.Account)...)
			if _, exists := events[string(key)]; !exists {
				return ErrInvalid
			}
		}
	}
	for _, account := range localTransferSortedKeys(stored) {
		state.result.RPM = append(state.result.RPM, LocalTransferRPMProjection{Account: account, Stored: stored[account], Live: live[account]})
	}
	return nil
}

func localTransferReceiptAccount(receipt QuotaReceipt, name string) bool {
	for _, account := range receipt.Accounts {
		if account.Account == name {
			return true
		}
	}
	return false
}

func localTransferReceiptExpiry(receipt QuotaReceipt) int64 {
	latest := receipt.MonthEnd
	for _, end := range quotaWindowEnds(receipt) {
		latest = max(latest, end)
	}
	return latest
}

// RPM expiry does not authorize economic-history pruning. A strictly
// post-boundary admission retains its receipt through every original window.
func localTransferEconomicRetirement(admitted int64, zone string) (int64, error) {
	if admitted <= 0 || admitted > math.MaxInt64-int64(7*24*time.Hour) {
		return 0, ErrInvalid
	}
	_, monthEnd, err := QuotaMonthBounds(admitted, zone)
	if err != nil {
		return 0, err
	}
	return max(monthEnd, admitted+int64(7*24*time.Hour)), nil
}

func localTransferEmptyCounter() QuotaCounter {
	return QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
}

func localTransferAddContribution(counters map[string]QuotaCounter, key string, receipt QuotaReceipt) error {
	counter, exists := counters[key]
	if !exists {
		counter = localTransferEmptyCounter()
	}
	next, err := QuotaCounterDelta(counter, QuotaCounterContribution{Bound: receipt.Bound, Actual: receipt.Actual, Settled: true}, 1)
	if err != nil {
		return err
	}
	counters[key] = next
	return nil
}

func localTransferCountersEqual(left, right QuotaCounter) bool {
	return left.TokensUsed == right.TokensUsed && left.TokensHeld == right.TokensHeld && left.TokensUnknown == right.TokensUnknown && left.MoneyUnknown == right.MoneyUnknown && maps.Equal(left.MoneyUsed, right.MoneyUsed) && maps.Equal(left.MoneyHeld, right.MoneyHeld)
}

func localTransferRemoveEmptyCounters(counters map[string]QuotaCounter) {
	for key, counter := range counters {
		if localTransferCountersEqual(counter, localTransferEmptyCounter()) {
			delete(counters, key)
		}
	}
}

func localTransferQuotaRows(counters map[string]QuotaCounter) []LocalTransferQuotaProjection {
	rows := make([]LocalTransferQuotaProjection, 0, len(counters))
	for _, key := range localTransferSortedKeys(counters) {
		parts := strings.SplitN(key, "\x00", 2)
		rows = append(rows, LocalTransferQuotaProjection{Account: parts[0], Window: parts[1], Counter: counters[key]})
	}
	return rows
}
