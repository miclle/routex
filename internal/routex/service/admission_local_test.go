package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
)

func admissionTestJournal(t *testing.T) (*eventqueue.Queue, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admission.db")
	journal, err := eventqueue.Open(path, 32, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return journal, path
}

func admissionTestStore(t *testing.T) admissionStore {
	t.Helper()
	journal, _ := admissionTestJournal(t)
	return newLocalAdmissionStore(journal)
}

func TestLocalAdmissionAuthorityConjunctiveAccountsExactSettlementAndReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)
	journal, _ := admissionTestJournal(t)
	store := newLocalAdmissionStore(journal)
	if err := store.BindInstallation(ctx, "led_original", false); err != nil {
		t.Fatal(err)
	}
	if err := store.BindInstallation(ctx, "led_foreign", true); !errors.Is(err, eventqueue.ErrInvalid) {
		t.Fatalf("foreign installation binding = %v", err)
	}
	if err := store.EnableQuota(ctx, "UTC", now); err != nil {
		t.Fatal(err)
	}
	if err := store.EnableQuota(ctx, "Asia/Shanghai", now); !errors.Is(err, eventqueue.ErrQuotaConflict) {
		t.Fatalf("changed active calendar = %v", err)
	}
	capMoney := "0.000000000000000003"
	policies := []eventqueue.QuotaLimit{
		{Limit: eventqueue.Limit{Account: "user_usr_original", RPM: limitNumber(2), Concurrency: limitNumber(1)}, Revision: "lim_user", CreatedAt: now, TokensMonth: limitNumber(100), TokensMonthBehavior: "alert_only", MoneyMonth: &capMoney, Currency: "USD"},
		{Limit: eventqueue.Limit{Account: "key_key_original", RPM: limitNumber(2), Concurrency: limitNumber(1)}, Revision: "lim_key", CreatedAt: now, PersonalKey: true, TokensMonth: limitNumber(200), MoneyMonth: &capMoney, Currency: "USD"},
	}
	boundMoney := "0.000000000000000002"
	bound := eventqueue.QuotaBound{Tokens: limitNumber(150), Money: &boundMoney, Currency: "USD", Revision: "bound_original"}
	if err := store.PreflightWithQuota(ctx, "req_first", policies, bound, "UTC", now); err != nil {
		t.Fatal(err)
	}
	if depth, err := journal.Depth(); err != nil || depth != 0 {
		t.Fatalf("preflight recorded a call: depth=%d error=%v", depth, err)
	}
	if err := store.ReserveWithQuotaRecovery(ctx, "req_first", []byte("fallback"), policies, bound, zeroQuotaSettlement(bound), now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveWithQuotaRecovery(ctx, "req_contended", []byte("fallback"), policies, bound, zeroQuotaSettlement(bound), now); !errors.Is(err, eventqueue.ErrConcurrency) {
		t.Fatalf("concurrent account admission = %v", err)
	}
	for _, policy := range policies {
		rpm, active, err := store.AccountUsage(ctx, policy.Account, now)
		if err != nil || rpm != 1 || active != 1 {
			t.Fatalf("partial admission changed %s: rpm=%d active=%d error=%v", policy.Account, rpm, active, err)
		}
	}
	actualMoney := "0.000000000000000001"
	actual := eventqueue.QuotaSettlement{Tokens: limitNumber(150), Money: &actualMoney, Currency: "USD"}
	for range 2 {
		if _, err := store.CompleteQuota(ctx, "req_first", []byte("final"), actual, now); err != nil {
			t.Fatal(err)
		}
	}
	conflicting := actual
	conflicting.Tokens = limitNumber(151)
	if _, err := store.CompleteQuota(ctx, "req_first", []byte("final"), conflicting, now); !errors.Is(err, eventqueue.ErrQuotaConflict) {
		t.Fatalf("conflicting accepted settlement = %v", err)
	}
	if err := journal.Ack("req_first"); err != nil {
		t.Fatal(err)
	}
	proof, err := store.AccountQuotaUsageProofBatch(ctx, []string{policies[0].Account, policies[1].Account}, now)
	if err != nil || !proof.Active || !proof.CoverageStart.Equal(now) || proof.TimeZone != "UTC" {
		t.Fatalf("coverage proof = %+v error=%v", proof, err)
	}
	for _, policy := range policies {
		account := proof.Accounts[policy.Account]
		if !account.Registered || !account.CreatedAt.Equal(now) || account.Usage.Month.TokensUsed != 150 || account.Usage.Month.MoneyUsed["USD"] != actualMoney || account.Usage.Active.TokensHeld != 0 {
			t.Fatalf("acknowledgement or replay changed %s: %+v", policy.Account, account)
		}
	}
	tooMany := bound
	tooMany.Tokens = limitNumber(51)
	if err := store.ReserveWithQuotaRecovery(ctx, "req_over_key", []byte("fallback"), policies, tooMany, zeroQuotaSettlement(tooMany), now); !errors.Is(err, eventqueue.ErrQuotaTokens) {
		t.Fatalf("soft parent bypassed hard Key = %v", err)
	}
	tooExpensive := bound
	tooExpensive.Tokens = limitNumber(1)
	tooExpensive.Money = &capMoney
	if err := store.ReserveWithQuotaRecovery(ctx, "req_over_money", []byte("fallback"), policies, tooExpensive, zeroQuotaSettlement(tooExpensive), now); !errors.Is(err, eventqueue.ErrQuotaMoney) {
		t.Fatalf("soft Token item bypassed hard money = %v", err)
	}
	if held, err := store.HasLiveMoneyHolds(ctx, now); err != nil || held {
		t.Fatalf("settled exact money retained a hold: held=%t error=%v", held, err)
	}
	if rpm, active, err := store.AccountUsage(ctx, policies[0].Account, now); err != nil || rpm != 1 || active != 0 {
		t.Fatalf("rejected writes or replay changed rates: rpm=%d active=%d error=%v", rpm, active, err)
	}
}

func TestLocalAdmissionAuthorityIndependentUnknownsAndCrashRecovery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)
	journal, path := admissionTestJournal(t)
	store := newLocalAdmissionStore(journal)
	if err := store.EnableQuota(ctx, "UTC", now); err != nil {
		t.Fatal(err)
	}
	money := "0.000000000000000001"
	capMoney := "1"
	policy := eventqueue.QuotaLimit{Limit: eventqueue.Limit{Account: "user_usr_recovery"}, Revision: "lim_original", CreatedAt: now, TokensMonth: limitNumber(1000), MoneyMonth: &capMoney, Currency: "USD"}
	bound := eventqueue.QuotaBound{Tokens: limitNumber(20), Money: &money, Currency: "USD", Revision: "bound_original"}
	for _, requestID := range []string{"req_terminal", "req_active", "req_not_dispatched"} {
		if err := store.ReserveWithQuotaRecovery(ctx, requestID, []byte("fallback"), []eventqueue.QuotaLimit{policy}, bound, zeroQuotaSettlement(bound), now); err != nil {
			t.Fatal(err)
		}
	}
	// A canceled caller must not stop the existing synchronous journal settlement.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.CompleteQuota(canceled, "req_terminal", []byte("known_tokens"), eventqueue.QuotaSettlement{Tokens: limitNumber(7)}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePendingQuota(ctx, "req_active", []byte("active_work"), eventqueue.QuotaSettlement{}); err != nil {
		t.Fatal(err)
	}
	if held, err := store.HasLiveMoneyHolds(ctx, now); err != nil || !held {
		t.Fatalf("unknown monetary result became free: held=%t error=%v", held, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := eventqueue.Open(path, 32, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recovered.Close() })
	store = newLocalAdmissionStore(recovered)
	usage, err := store.AccountQuotaUsage(ctx, policy.Account, now)
	if err != nil || usage.Month.TokensUsed != 7 || usage.Month.TokensHeld != 20 || usage.Month.MoneyHeld["USD"] != "0.000000000000000002" || usage.Month.TokensUnknown != 0 || usage.Month.MoneyUnknown != 0 {
		t.Fatalf("independent recovery usage = %+v error=%v", usage, err)
	}
	if rpm, active, err := store.AccountUsage(ctx, policy.Account, now); err != nil || rpm != 3 || active != 0 {
		t.Fatalf("local recovery reset RPM or retained orphan concurrency: rpm=%d active=%d error=%v", rpm, active, err)
	}
	undispatched, err := store.QuotaReceipt(ctx, "req_not_dispatched")
	if err != nil || undispatched.Actual.Tokens == nil || *undispatched.Actual.Tokens != 0 || undispatched.Actual.Money == nil || *undispatched.Actual.Money != "0" {
		t.Fatalf("original zero-work recovery changed: receipt=%+v error=%v", undispatched, err)
	}
	active, err := store.QuotaReceipt(ctx, "req_active")
	if err != nil || active.Actual.Tokens != nil || active.Actual.Money != nil {
		t.Fatalf("active work acquired invented usage: receipt=%+v error=%v", active, err)
	}
}

func TestLocalAdmissionAuthorityIncompleteCoverageUnknownsAndRollingControls(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)
	journal, _ := admissionTestJournal(t)
	store := newLocalAdmissionStore(journal)
	if err := store.EnableQuota(ctx, "UTC", now); err != nil {
		t.Fatal(err)
	}
	policy := eventqueue.QuotaLimit{Limit: eventqueue.Limit{Account: "user_usr_uncovered"}, Revision: "lim_original", CreatedAt: now.Add(-30 * 24 * time.Hour), Tokens5H: limitNumber(100)}
	bound := eventqueue.QuotaBound{Tokens: limitNumber(1), Revision: "bound_original"}
	if err := store.ReserveWithQuotaRecovery(ctx, "req_uncovered", []byte("fallback"), []eventqueue.QuotaLimit{policy}, bound, zeroQuotaSettlement(bound), now); !errors.Is(err, eventqueue.ErrQuotaCoverage) {
		t.Fatalf("incomplete history admitted finite quota: %v", err)
	}
	policy.Account, policy.CreatedAt, policy.Tokens5H = "user_usr_unknown", now, nil
	if err := store.ReserveWithQuotaRecovery(ctx, "req_unknown", []byte("fallback"), []eventqueue.QuotaLimit{policy}, eventqueue.QuotaBound{}, eventqueue.QuotaSettlement{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteQuota(ctx, "req_unknown", []byte("unknown"), eventqueue.QuotaSettlement{}, now); err != nil {
		t.Fatal(err)
	}
	batch, err := store.AccountQuotaUsageBatch(ctx, []string{policy.Account}, now)
	if err != nil || !batch.Active || batch.Accounts[policy.Account].Month.TokensUnknown != 1 || batch.Accounts[policy.Account].Month.MoneyUnknown != 1 {
		t.Fatalf("batch erased unbounded unknowns: %+v error=%v", batch, err)
	}
	proof, err := store.AccountQuotaUsageProofBatch(ctx, []string{policy.Account, "user_usr_unregistered"}, now)
	if err != nil || proof.Accounts["user_usr_unregistered"].Registered || !proof.Accounts["user_usr_unregistered"].CreatedAt.IsZero() {
		t.Fatalf("batch invented account birth: %+v error=%v", proof, err)
	}
	policy.TokensMonth, policy.TokensMonthBehavior = limitNumber(0), "alert_only"
	if err := store.ReserveWithQuotaRecovery(ctx, "req_soft_unknown", []byte("fallback"), []eventqueue.QuotaLimit{policy}, bound, zeroQuotaSettlement(bound), now); !errors.Is(err, eventqueue.ErrQuotaUnknown) {
		t.Fatalf("alert-only bypassed unknown accounting: %v", err)
	}
	policy.Account = "user_usr_rolling"
	policy.Tokens5H = limitNumber(0)
	if err := store.ReserveWithQuotaRecovery(ctx, "req_rolling", []byte("fallback"), []eventqueue.QuotaLimit{policy}, bound, zeroQuotaSettlement(bound), now); !errors.Is(err, eventqueue.ErrQuotaTokens) {
		t.Fatalf("monthly alert-only bypassed rolling hard cap: %v", err)
	}
	if held, err := store.HasLiveMoneyHolds(ctx, now); err != nil || !held {
		t.Fatalf("unbounded unknown money allowed currency change: held=%t error=%v", held, err)
	}
}
