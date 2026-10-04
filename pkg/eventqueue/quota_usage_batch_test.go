package eventqueue

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestQuotaUsageBatchBoundsAndInactive(t *testing.T) {
	q, err := Open(filepath.Join(t.TempDir(), "inactive.db"), 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	for _, accounts := range [][]string{nil, {"bad/id"}, {"same", "same"}, make([]string, MaxQuotaUsageBatchAccounts+1)} {
		if result, err := q.AccountQuotaUsageBatch(accounts, quotaTestTime); !errors.Is(err, ErrInvalid) || result != nil {
			t.Fatal("invalid batch acquired a snapshot", result, err)
		}
	}
	result, err := q.AccountQuotaUsageBatch([]string{"user_one", "team_one"}, quotaTestTime)
	if err != nil || result.Active || !result.AsOf.IsZero() || len(result.Accounts) != 0 || !result.CoverageStart.IsZero() {
		t.Fatal("inactive journal invented usage", result, err)
	}
	if status, err := q.QuotaStatus(); err != nil || status.Active {
		t.Fatal("read activated accounting", status, err)
	}
}

func TestQuotaUsageBatchMatchesSingleReadsAndOwnsValues(t *testing.T) {
	q, _ := newQuotaTest(t, "America/New_York")
	accounts := []string{"user_one", "team_one", "team_member_one"}
	policies := []QuotaLimit{quotaPolicy(accounts[1]), quotaPolicy(accounts[2])}
	requireReserve(t, q, "pair", policies, QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("1.000000000000000001"), Currency: "USD"}, quotaTestTime)
	requireComplete(t, q, "pair", QuotaSettlement{Tokens: limitPtr(7), Money: moneyPtr("0.000000000000000001"), Currency: "USD"}, quotaTestTime)
	result, err := q.AccountQuotaUsageBatch(accounts, quotaTestTime)
	if err != nil || !result.Active || len(result.Accounts) != len(accounts) {
		t.Fatal(result, err)
	}
	for _, account := range accounts {
		actual := result.Accounts[account]
		if !actual.AsOf.Equal(result.AsOf) || !actual.CoverageStart.Equal(result.CoverageStart) || actual.TimeZone != result.TimeZone || !reflect.DeepEqual(actual, *requireUsage(t, q, account, quotaTestTime)) {
			t.Fatal("batch drifted from authoritative single-account semantics", account, actual)
		}
	}
	result.Accounts["team_one"].Month.MoneyUsed["USD"] = "999"
	if requireUsage(t, q, "team_one", quotaTestTime).Month.MoneyUsed["USD"] == "999" || result.Accounts["team_member_one"].Month.MoneyUsed["USD"] == "999" {
		t.Fatal("returned map aliases journal state or another account")
	}
	maximum := make([]string, MaxQuotaUsageBatchAccounts)
	for i := range maximum {
		maximum[i] = fmt.Sprintf("account_%d", i)
	}
	if result, err := q.AccountQuotaUsageBatch(maximum, quotaTestTime); err != nil || len(result.Accounts) != MaxQuotaUsageBatchAccounts {
		t.Fatal("maximum bounded batch rejected", err)
	}
}

func TestQuotaUsageBatchSamplesPairSettlementAtomically(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	accounts := []string{"team_one", "team_member_one"}
	policies := []QuotaLimit{quotaPolicy(accounts[0]), quotaPolicy(accounts[1])}
	requireReserve(t, q, "pair", policies, QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("1.000000000000000002"), Currency: "USD"}, quotaTestTime)
	held, err := q.AccountQuotaUsageBatch(accounts, quotaTestTime)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		usage := held.Accounts[account]
		if !usage.AsOf.Equal(held.AsOf) || usage.Active.TokensHeld != 10 || usage.Active.MoneyHeld["USD"] != "1.000000000000000002" || usage.Month.TokensHeld != 0 || len(usage.Month.MoneyHeld) != 0 || usage.Month.TokensUsed != 0 {
			t.Fatal("live pair holds were not sampled separately from monthly facts", usage)
		}
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := q.CompleteQuota("pair", []byte("done"), QuotaSettlement{Tokens: limitPtr(7), Money: moneyPtr("1"), Currency: "USD"}, quotaTestTime); err != nil {
			t.Error(err)
		}
	})
	for range 100 {
		batch, err := q.AccountQuotaUsageBatch(accounts, quotaTestTime.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(batch.Accounts[accounts[0]].Month, batch.Accounts[accounts[1]].Month) || !reflect.DeepEqual(batch.Accounts[accounts[0]].Active, batch.Accounts[accounts[1]].Active) {
			t.Fatal("batch observed half of an atomic aggregate/pair settlement")
		}
	}
	wg.Wait()
	batch, err := q.AccountQuotaUsageBatch(accounts, quotaTestTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		usage := batch.Accounts[account]
		if usage.Active.TokensHeld != 0 || len(usage.Active.MoneyHeld) != 0 || usage.Month.TokensUsed != 7 || usage.Month.MoneyUsed["USD"] != "1" {
			t.Fatal("completed pair retained live reservations or changed settled facts", usage)
		}
	}
}

func TestQuotaUsageBatchOutageHasNoPartialSnapshot(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := q.AccountQuotaUsageBatch([]string{"team_one"}, quotaTestTime); err == nil || result != nil {
		t.Fatal("closed journal fabricated zero usage", result, err)
	}
}
