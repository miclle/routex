package eventqueue

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaStatusAndLiveCurrencyHoldBoundary(t *testing.T) {
	q, err := Open(filepath.Join(t.TempDir(), "status.db"), 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	status, err := q.QuotaStatus()
	if err != nil || status.Active || status.CoverageStart != nil {
		t.Fatal("opening legacy journal invented coverage", status, err)
	}
	if held, err := q.HasLiveMoneyHolds(quotaTestTime); err != nil || held {
		t.Fatal(held, err)
	}
	if err := q.EnableQuota("UTC", quotaTestTime); err != nil {
		t.Fatal(err)
	}
	status, err = q.QuotaStatus()
	if err != nil || !status.Active || status.CoverageStart == nil || !status.CoverageStart.Equal(quotaTestTime) {
		t.Fatal(status, err)
	}
	policy := quotaPolicy("account")
	bound := tokenBound(5)
	bound.Money = moneyPtr("1")
	bound.Currency = "USD"
	requireReserve(t, q, "active", []QuotaLimit{policy}, bound, quotaTestTime)
	nextMonth := quotaTestTime.AddDate(0, 1, 0)
	if held, err := q.HasLiveMoneyHolds(nextMonth); err != nil || !held {
		t.Fatal("active hold disappeared at month rollover", held, err)
	}
	requireComplete(t, q, "active", QuotaSettlement{Tokens: limitPtr(2)}, nextMonth)
	if held, err := q.HasLiveMoneyHolds(nextMonth); err != nil || held {
		t.Fatal("terminal prior-month hold migrated into new month", held, err)
	}
	usage := requireUsage(t, q, policy.Account, quotaTestTime)
	if !usage.AsOf.Equal(nextMonth) {
		t.Fatal("reported wall clock instead of persisted accounting clock", usage.AsOf)
	}
	requireReserve(t, q, "unbounded", []QuotaLimit{policy}, QuotaBound{}, nextMonth)
	requireComplete(t, q, "unbounded", QuotaSettlement{Tokens: limitPtr(1)}, nextMonth)
	if held, err := q.HasLiveMoneyHolds(nextMonth.Add(time.Hour)); err != nil || !held {
		t.Fatal("unknown monetary exposure treated as zero", held, err)
	}
}
