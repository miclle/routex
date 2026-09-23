package eventqueue

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func moneyPtr(value string) *string { return &value }

var quotaTestTime = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func newQuotaTest(t *testing.T, zone string) (*Queue, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quota.db")
	q, err := Open(path, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	if err := q.EnableQuota(zone, quotaTestTime); err != nil {
		t.Fatal(err)
	}
	return q, path
}
func quotaPolicy(account string) QuotaLimit {
	return QuotaLimit{Limit: Limit{Account: account}, Revision: "policy_v1", CreatedAt: quotaTestTime}
}
func tokenBound(value int64) QuotaBound {
	return QuotaBound{Tokens: limitPtr(value), Revision: "bound_v1"}
}
func requireReserve(t *testing.T, q *Queue, id string, policy []QuotaLimit, bound QuotaBound, now time.Time) {
	t.Helper()
	if err := q.ReserveWithQuota(id, []byte("fallback"), policy, bound, now); err != nil {
		t.Fatal(err)
	}
}
func requireComplete(t *testing.T, q *Queue, id string, actual QuotaSettlement, now time.Time) *QuotaReceipt {
	t.Helper()
	receipt, err := q.CompleteQuota(id, []byte("final"), actual, now)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
func requireUsage(t *testing.T, q *Queue, account string, now time.Time) *AccountQuotaUsage {
	t.Helper()
	value, err := q.AccountQuotaUsage(account, now)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestQuotaConcurrentAggregateAtomicAdmission(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	parent, child := quotaPolicy("user_one"), quotaPolicy("key_root")
	parent.Tokens5H = limitPtr(100)
	child.Tokens5H = limitPtr(25)
	var accepted atomic.Int32
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := range 40 {
		group.Go(func() {
			<-start
			err := q.ReserveWithQuota(fmt.Sprintf("req_%d", index), []byte("fallback"), []QuotaLimit{parent, child}, tokenBound(10), quotaTestTime)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrQuotaTokens) {
				t.Error(err)
			}
		})
	}
	close(start)
	group.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("admitted %d", accepted.Load())
	}
	for _, account := range []string{parent.Account, child.Account} {
		usage := requireUsage(t, q, account, quotaTestTime)
		if usage.Active.TokensHeld != 20 {
			t.Fatalf("partial economic debit: %+v", usage)
		}
		rpm, active, err := q.AccountUsage(account, quotaTestTime)
		if err != nil || rpm != 2 || active != 2 {
			t.Fatalf("partial RPM/lease %d %d %v", rpm, active, err)
		}
	}
	unrelated := quotaPolicy("project_other")
	requireReserve(t, q, "unrelated", []QuotaLimit{unrelated}, tokenBound(10), quotaTestTime)
}
func TestQuotaSettlementIndependentDimensionsAndIdempotency(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	policy := quotaPolicy("key_root")
	policy.Tokens5H = limitPtr(100)
	policy.MoneyMonth = moneyPtr("10")
	policy.Currency = "USD"
	bound := tokenBound(50)
	bound.Money = moneyPtr("2.125")
	bound.Currency = "USD"
	requireReserve(t, q, "known_tokens", []QuotaLimit{policy}, bound, quotaTestTime)
	actual := QuotaSettlement{Tokens: limitPtr(13)}
	requireComplete(t, q, "known_tokens", actual, quotaTestTime)
	if err := q.Ack("known_tokens"); err != nil {
		t.Fatal(err)
	}
	if err := q.Reserve("known_tokens", []byte("rejected")); !errors.Is(err, ErrQuotaConflict) {
		t.Fatal("generic admission reused retained ID", err)
	}
	requireComplete(t, q, "known_tokens", actual, quotaTestTime)
	if _, err := q.CompleteQuota("known_tokens", []byte("changed"), actual, quotaTestTime); !errors.Is(err, ErrQuotaConflict) {
		t.Fatalf("conflicting fact accepted: %v", err)
	}
	usage := requireUsage(t, q, policy.Account, quotaTestTime)
	if usage.Active.TokensHeld != 0 || usage.FiveHours.TokensUsed != 13 || usage.Month.MoneyHeld["USD"] != "2.125" || usage.Month.MoneyUnknown != 0 {
		t.Fatalf("wrong independent settlement: %+v", usage)
	}
	requireReserve(t, q, "known_money", []QuotaLimit{policy}, bound, quotaTestTime)
	requireComplete(t, q, "known_money", QuotaSettlement{Money: moneyPtr("0"), Currency: "USD"}, quotaTestTime)
	usage = requireUsage(t, q, policy.Account, quotaTestTime)
	if usage.FiveHours.TokensHeld != 50 || usage.FiveHours.TokensUsed != 13 || usage.Month.MoneyHeld["USD"] != "2.125" {
		t.Fatalf("nil invented zero or zero lost: %+v", usage)
	}
	if err := q.Complete("known_tokens", []byte("bypass")); !errors.Is(err, ErrQuotaRequired) {
		t.Fatal(err)
	}
}
func TestQuotaUnknownUnboundedAndCurrencyFailClosed(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	policy := quotaPolicy("account")
	requireReserve(t, q, "unconstrained", []QuotaLimit{policy}, QuotaBound{}, quotaTestTime)
	requireComplete(t, q, "unconstrained", QuotaSettlement{Tokens: limitPtr(3)}, quotaTestTime)
	policy.Tokens5H = limitPtr(100)
	requireReserve(t, q, "tokens_still_known", []QuotaLimit{policy}, tokenBound(5), quotaTestTime)
	policy.MoneyMonth = moneyPtr("100")
	policy.Currency = "USD"
	bound := tokenBound(5)
	bound.Money = moneyPtr("1")
	bound.Currency = "USD"
	if err := q.ReserveWithQuota("money_unknown", []byte("f"), []QuotaLimit{policy}, bound, quotaTestTime); !errors.Is(err, ErrQuotaUnknown) {
		t.Fatal(err)
	}
	other := quotaPolicy("other")
	foreign := QuotaBound{Money: moneyPtr("1"), Currency: "EUR"}
	requireReserve(t, q, "foreign", []QuotaLimit{other}, foreign, quotaTestTime)
	requireComplete(t, q, "foreign", QuotaSettlement{Money: moneyPtr("0.5"), Currency: "EUR"}, quotaTestTime)
	other.MoneyMonth = moneyPtr("10")
	other.Currency = "USD"
	if err := q.ReserveWithQuota("mixed", []byte("f"), []QuotaLimit{other}, bound, quotaTestTime); !errors.Is(err, ErrQuotaCurrency) {
		t.Fatal(err)
	}
}
func TestQuotaRollingBoundaryClockAndActiveCarry(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	policy := quotaPolicy("key_root")
	policy.TPM = limitPtr(10)
	policy.Tokens5H = limitPtr(10)
	requireReserve(t, q, "first", []QuotaLimit{policy}, tokenBound(10), quotaTestTime)
	later := quotaTestTime.Add(8 * 24 * time.Hour)
	if err := q.ReserveWithQuota("active_still_blocks", []byte("f"), []QuotaLimit{policy}, tokenBound(1), later); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal(err)
	}
	// A completion is attributed to admission, even when its actual arrives later.
	requireComplete(t, q, "first", QuotaSettlement{Tokens: limitPtr(10)}, later)
	requireReserve(t, q, "second", []QuotaLimit{policy}, tokenBound(10), later)
	requireComplete(t, q, "second", QuotaSettlement{Tokens: limitPtr(10)}, later)
	if err := q.ReserveWithQuota("clock_back", []byte("f"), []QuotaLimit{policy}, tokenBound(1), quotaTestTime); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal(err)
	}
	minute := requireUsage(t, q, policy.Account, later.Add(time.Minute))
	if minute.Minute.TokensUsed != 0 || minute.FiveHours.TokensUsed != 10 {
		t.Fatalf("rolling interval incorrect: %+v", minute)
	}
	five := requireUsage(t, q, policy.Account, later.Add(5*time.Hour))
	if five.FiveHours.TokensUsed != 0 || five.SevenDays.TokensUsed != 10 {
		t.Fatal(five)
	}
	requireReserve(t, q, "boundary", []QuotaLimit{policy}, tokenBound(10), later.Add(5*time.Hour))
}
func TestQuotaCoverageAndExplicitZero(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	old := quotaPolicy("old")
	old.CreatedAt = quotaTestTime.Add(-time.Hour)
	old.Tokens7D = limitPtr(10)
	if err := q.ReserveWithQuota("missing_history", []byte("f"), []QuotaLimit{old}, tokenBound(1), quotaTestTime); !errors.Is(err, ErrQuotaCoverage) {
		t.Fatal(err)
	}
	requireReserve(t, q, "covered", []QuotaLimit{old}, tokenBound(1), quotaTestTime.Add(7*24*time.Hour))
	fresh := quotaPolicy("fresh")
	fresh.TokensMonth = limitPtr(0)
	if err := q.ReserveWithQuota("closed", []byte("f"), []QuotaLimit{fresh}, tokenBound(0), quotaTestTime.Add(7*24*time.Hour)); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal(err)
	}
	fresh.TokensMonth = nil
	fresh.MoneyMonth = moneyPtr("0")
	fresh.Currency = "USD"
	if err := q.ReserveWithQuota("closed_money", []byte("f"), []QuotaLimit{fresh}, QuotaBound{Money: moneyPtr("0"), Currency: "USD", Revision: "b"}, quotaTestTime.Add(7*24*time.Hour)); !errors.Is(err, ErrQuotaMoney) {
		t.Fatal(err)
	}
	if err := q.ReserveWithLimits("legacy_bypass", []byte("f"), nil, quotaTestTime); !errors.Is(err, ErrQuotaRequired) {
		t.Fatal(err)
	}
}
func TestQuotaOverrunPersistsDebtAndInvalidatesBound(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	policy := quotaPolicy("key_root")
	policy.Tokens5H = limitPtr(100)
	requireReserve(t, q, "overrun", []QuotaLimit{policy}, tokenBound(10), quotaTestTime)
	result := requireComplete(t, q, "overrun", QuotaSettlement{Tokens: limitPtr(20)}, quotaTestTime)
	if !result.Overrun {
		t.Fatal("overrun hidden")
	}
	if value := requireUsage(t, q, policy.Account, quotaTestTime).FiveHours.TokensUsed; value != 20 {
		t.Fatalf("clamped debt %d", value)
	}
	if err := q.ReserveWithQuota("stale_bound", []byte("f"), []QuotaLimit{policy}, tokenBound(1), quotaTestTime); !errors.Is(err, ErrQuotaBound) {
		t.Fatal(err)
	}
	corrected := tokenBound(25)
	corrected.Revision = "bound_v2"
	requireReserve(t, q, "corrected", []QuotaLimit{policy}, corrected, quotaTestTime)
}
func TestQuotaCalendarDSTAndMonthAttribution(t *testing.T) {
	zone := "America/New_York"
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	march := time.Date(2026, 3, 15, 12, 0, 0, 0, location)
	start, end, err := quotaMonth(march.UnixNano(), zone)
	if err != nil || time.Duration(end-start) != (31*24-1)*time.Hour {
		t.Fatalf("DST month %s %v", time.Duration(end-start), err)
	}
	q, _ := newQuotaTest(t, zone)
	policy := quotaPolicy("project")
	policy.TokensMonth = limitPtr(10)
	admission := time.Date(2026, 3, 31, 23, 59, 0, 0, location)
	requireReserve(t, q, "march", []QuotaLimit{policy}, tokenBound(10), admission)
	april := admission.Add(2 * time.Minute)
	if err := q.ReserveWithQuota("carry", []byte("f"), []QuotaLimit{policy}, tokenBound(1), april); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal(err)
	}
	requireComplete(t, q, "march", QuotaSettlement{Tokens: limitPtr(9)}, april)
	usage := requireUsage(t, q, policy.Account, april)
	if usage.Month.TokensUsed != 0 || usage.FiveHours.TokensUsed != 9 {
		t.Fatalf("late usage moved month: %+v", usage)
	}
	requireReserve(t, q, "april", []QuotaLimit{policy}, tokenBound(10), april)
	if err := q.EnableQuota("UTC", april); !errors.Is(err, ErrQuotaConflict) {
		t.Fatal("timezone changed existing periods", err)
	}
}
func TestQuotaCapacityIndependentOfDelivery(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	q.quotaCapacity = 1
	policy := quotaPolicy("account")
	requireReserve(t, q, "one", []QuotaLimit{policy}, tokenBound(1), quotaTestTime)
	requireComplete(t, q, "one", QuotaSettlement{Tokens: limitPtr(1)}, quotaTestTime)
	if err := q.Ack("one"); err != nil {
		t.Fatal(err)
	}
	if err := q.ReserveWithQuota("two", []byte("f"), []QuotaLimit{policy}, tokenBound(1), quotaTestTime); !errors.Is(err, ErrFull) {
		t.Fatal("ack erased history capacity", err)
	}
	requireReserve(t, q, "after_retention", []QuotaLimit{policy}, tokenBound(1), quotaTestTime.AddDate(0, 1, 0))
	if _, err := q.QuotaReceipt("one"); !errors.Is(err, ErrMissing) {
		t.Fatal("expired receipt retained", err)
	}
}
