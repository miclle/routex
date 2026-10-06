package eventqueue

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTeamMonthlyBehaviorMixedAccountsAndDurableSettlement(t *testing.T) {
	q, path := newQuotaTest(t, "UTC")
	user, key := quotaPolicy("team_tea_behavior"), quotaPolicy("team_member_behavior")
	user.TokensMonth = limitPtr(100)
	user.TokensMonthBehavior = "alert_only"
	key.TokensMonth = limitPtr(200)
	policies := []QuotaLimit{user, key}
	requireReserve(t, q, "first", policies, tokenBound(150), quotaTestTime)
	requireComplete(t, q, "first", QuotaSettlement{Tokens: limitPtr(150)}, quotaTestTime)
	for _, account := range []string{user.Account, key.Account} {
		usage := requireUsage(t, q, account, quotaTestTime)
		if usage.Month.TokensUsed != 150 || usage.Active.TokensHeld != 0 {
			t.Fatal("shared accounting lost", account, usage)
		}
	}
	if err := q.ReserveWithQuota("over_key", []byte("f"), policies, tokenBound(51), quotaTestTime); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal("soft Team bypassed hard member", err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	usage := requireUsage(t, reopened, user.Account, quotaTestTime)
	if usage.Month.TokensUsed != 150 {
		t.Fatal("restart reset soft account")
	}
	requireReserve(t, reopened, "exact_key", policies, tokenBound(50), quotaTestTime)
	requireComplete(t, reopened, "exact_key", QuotaSettlement{Tokens: limitPtr(50)}, quotaTestTime)
	user.TokensMonthBehavior = "stop"
	key.TokensMonthBehavior = "alert_only"
	if err := reopened.ReserveWithQuota("hard_user", []byte("f"), []QuotaLimit{user, key}, tokenBound(1), quotaTestTime); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal("hard Team bypassed", err)
	}
}

func TestTeamMonthlyBehaviorIndependentMoneyTokensAndZero(t *testing.T) {
	for _, test := range []struct {
		name, tokenMode, moneyMode string
		want                       error
	}{
		{"soft_tokens_hard_money", "alert_only", "", ErrQuotaMoney},
		{"hard_tokens_soft_money", "", "alert_only", ErrQuotaTokens},
		{"both_soft", "alert_only", "alert_only", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p := quotaPolicy("team_tea_modes")
			p.TokensMonth = limitPtr(0)
			p.MoneyMonth = moneyPtr("0")
			p.Currency = "USD"
			p.TokensMonthBehavior = test.tokenMode
			p.MoneyMonthBehavior = test.moneyMode
			bound := tokenBound(1)
			bound.Money = moneyPtr("0.000000000000000001")
			bound.Currency = "USD"
			err := q.ReserveWithQuota("zero", []byte("f"), []QuotaLimit{p}, bound, quotaTestTime)
			if !errors.Is(err, test.want) {
				t.Fatal("independent zero semantics", err, test.want)
			}
			if err == nil {
				requireComplete(t, q, "zero", QuotaSettlement{Tokens: limitPtr(1), Money: moneyPtr("0.000000000000000001"), Currency: "USD"}, quotaTestTime)
				u := requireUsage(t, q, p.Account, quotaTestTime)
				if u.Month.MoneyUsed["USD"] != "0.000000000000000001" {
					t.Fatal("exact money lost")
				}
			}
		})
	}
	q, _ := newQuotaTest(t, "UTC")
	p := quotaPolicy("team_tea_null")
	p.TokensMonthBehavior = "alert_only"
	p.MoneyMonthBehavior = "alert_only"
	requireReserve(t, q, "null", []QuotaLimit{p}, QuotaBound{}, quotaTestTime)
}

func TestTeamMonthlyBehaviorDoesNotBypassProofOrOtherControls(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*QuotaLimit, *QuotaBound)
		want   error
	}{
		{"rolling", func(p *QuotaLimit, b *QuotaBound) { p.Tokens5H = limitPtr(0) }, ErrQuotaTokens},
		{"tpm", func(p *QuotaLimit, b *QuotaBound) { p.TPM = limitPtr(0) }, ErrQuotaTokens},
		{"bound", func(p *QuotaLimit, b *QuotaBound) { b.Tokens = nil }, ErrQuotaBound},
		{"money_bound", func(p *QuotaLimit, b *QuotaBound) { b.Money = nil }, ErrQuotaBound},
		{"currency", func(p *QuotaLimit, b *QuotaBound) { b.Currency = "EUR" }, ErrQuotaCurrency},

		{"mode", func(p *QuotaLimit, b *QuotaBound) { p.TokensMonthBehavior = "ALERT_ONLY" }, ErrInvalid},
		{"other_scope", func(p *QuotaLimit, b *QuotaBound) { p.Account = "team_member_wrong" }, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p := quotaPolicy("team_tea_guard")
			p.TokensMonth = limitPtr(0)
			p.MoneyMonth = moneyPtr("0")
			p.Currency = "USD"
			p.TokensMonthBehavior = "alert_only"
			p.MoneyMonthBehavior = "alert_only"
			b := tokenBound(1)
			b.Money = moneyPtr("0.000000000000000001")
			b.Currency = "USD"
			test.change(&p, &b)
			if err := q.PreflightWithQuota("guard", []QuotaLimit{p}, b, "UTC", quotaTestTime); !errors.Is(err, test.want) {
				t.Fatal("proof bypass", err, test.want)
			}
		})
	}
	t.Run("coverage", func(t *testing.T) {
		q, err := Open(filepath.Join(t.TempDir(), "later.db"), 100, 1024)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = q.Close() }()
		now := quotaTestTime.Add(time.Hour)
		if err = q.EnableQuota("UTC", now); err != nil {
			t.Fatal(err)
		}
		p := quotaPolicy("team_tea_coverage")
		p.TokensMonth = limitPtr(0)
		p.TokensMonthBehavior = "alert_only"
		if err = q.PreflightWithQuota("guard", []QuotaLimit{p}, tokenBound(1), "UTC", now); !errors.Is(err, ErrQuotaCoverage) {
			t.Fatal("incomplete month admitted", err)
		}
	})
	q, _ := newQuotaTest(t, "UTC")
	p := quotaPolicy("team_tea_unknown")
	requireReserve(t, q, "unknown", []QuotaLimit{p}, QuotaBound{}, quotaTestTime)
	requireComplete(t, q, "unknown", QuotaSettlement{}, quotaTestTime)
	p.TokensMonth = limitPtr(0)
	p.TokensMonthBehavior = "alert_only"
	if err := q.ReserveWithQuota("must_fail", []byte("f"), []QuotaLimit{p}, tokenBound(1), quotaTestTime); !errors.Is(err, ErrQuotaUnknown) {
		t.Fatal("soft Team unknown admitted", err)
	}
}

func TestTeamMonthlyBehaviorHoldsBirthAndUnknownMoney(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	user, key := quotaPolicy("team_tea_held"), quotaPolicy("team_member_held")
	user.TokensMonth = limitPtr(0)
	user.TokensMonthBehavior = "alert_only"
	key.TokensMonth = limitPtr(200)
	requireReserve(t, q, "held", []QuotaLimit{user, key}, tokenBound(150), quotaTestTime)
	for _, p := range []QuotaLimit{user, key} {
		if u := requireUsage(t, q, p.Account, quotaTestTime); u.Active.TokensHeld != 150 || u.Month.TokensUsed != 0 {
			t.Fatal("soft hold disappeared", u)
		}
	}
	if err := q.ReserveWithQuota("blocked", []byte("f"), []QuotaLimit{user, key}, tokenBound(51), quotaTestTime); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal("hard member ignored soft Team hold", err)
	}
	changed := user
	changed.CreatedAt = changed.CreatedAt.Add(time.Millisecond)
	if err := q.PreflightWithQuota("changed_birth", []QuotaLimit{changed}, tokenBound(1), "UTC", quotaTestTime.Add(time.Second)); !errors.Is(err, ErrQuotaConflict) {
		t.Fatal("soft birth changed", err)
	}
	requireComplete(t, q, "held", QuotaSettlement{Tokens: limitPtr(50)}, quotaTestTime)
	requireReserve(t, q, "released", []QuotaLimit{user, key}, tokenBound(150), quotaTestTime)
	requireComplete(t, q, "released", QuotaSettlement{Tokens: limitPtr(150)}, quotaTestTime)
	q2, _ := newQuotaTest(t, "UTC")
	money := quotaPolicy("team_tea_unknown_money")
	requireReserve(t, q2, "unpriced", []QuotaLimit{money}, tokenBound(1), quotaTestTime)
	requireComplete(t, q2, "unpriced", QuotaSettlement{Tokens: limitPtr(1)}, quotaTestTime)
	money.MoneyMonth = moneyPtr("0")
	money.Currency = "USD"
	money.MoneyMonthBehavior = "alert_only"
	b := tokenBound(1)
	b.Money = moneyPtr("0.000000000000000001")
	b.Currency = "USD"
	if err := q2.ReserveWithQuota("unknown_soft", []byte("f"), []QuotaLimit{money}, b, quotaTestTime); !errors.Is(err, ErrQuotaUnknown) {
		t.Fatal("soft Team unknown money admitted", err)
	}
	for _, test := range []struct {
		name         string
		rate, active *int64
		want         error
	}{
		{"rate", limitPtr(0), nil, ErrRateLimit}, {"concurrency", nil, limitPtr(0), ErrConcurrency},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p := quotaPolicy("team_tea_rate")
			p.TokensMonth = limitPtr(0)
			p.TokensMonthBehavior = "alert_only"
			p.RPM = test.rate
			p.Concurrency = test.active
			if err := q.ReserveWithQuota("limited", []byte("f"), []QuotaLimit{p}, tokenBound(1), quotaTestTime); !errors.Is(err, test.want) {
				t.Fatal("soft bypassed unchanged rate admission", err)
			}
		})
	}
}

func TestTeamMonthlyBehaviorExactAccountNamespace(t *testing.T) {
	for _, test := range []struct {
		account string
		valid   bool
	}{
		{"team_tea_one", true}, {"team_" + strings.Repeat("a", 30), true},
		{"team_", false}, {"team_" + strings.Repeat("a", 31), false}, {"TEAM_tea_one", false}, {"team_tea_one ", false}, {"team_tea_é", false},
		{"team_member_short", false}, {"team_member_" + strings.Repeat("A", 52), false}, {"project_prj_one", false}, {"key_key_one", false}, {"project_key_key_one", false},
	} {
		q, _ := newQuotaTest(t, "UTC")
		p := quotaPolicy(test.account)
		p.TokensMonth = limitPtr(0)
		p.TokensMonthBehavior = "alert_only"
		err := q.PreflightWithQuota("namespace", []QuotaLimit{p}, tokenBound(1), "UTC", quotaTestTime)
		if test.valid && err != nil || !test.valid && !errors.Is(err, ErrInvalid) {
			t.Fatal("nonaggregate account allowance", test, err)
		}
	}
}
