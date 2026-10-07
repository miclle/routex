package eventqueue

import (
	"errors"
	"testing"
)

func TestProjectMonthlyBehaviorIndependentHardKeyAndMoney(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		bound      int64
		money      bool
		want       error
	}{
		{"soft_Project_hard_Key", "alert_only", 150, false, nil},
		{"hard_Project", "stop", 150, false, ErrQuotaTokens},
		{"hard_Key", "alert_only", 201, false, ErrQuotaTokens},
		{"hard_money", "alert_only", 1, true, ErrQuotaMoney},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p, k := quotaPolicy("project_prj_exact"), quotaPolicy("key_key_original")
			p.TokensMonth = limitPtr(100)
			p.TokensMonthBehavior = tc.mode
			k.TokensMonth = limitPtr(200)
			b := tokenBound(tc.bound)
			if tc.money {
				p.MoneyMonth = moneyPtr("0")
				p.Currency = "USD"
				b.Money = moneyPtr("0.000000000000000001")
				b.Currency = "USD"
			}
			err := q.ReserveWithQuota("project_mixed", []byte("f"), []QuotaLimit{p, k}, b, quotaTestTime)
			if !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
			if err == nil {
				requireComplete(t, q, "project_mixed", QuotaSettlement{Tokens: limitPtr(tc.bound)}, quotaTestTime)
				for _, account := range []string{p.Account, k.Account} {
					if u := requireUsage(t, q, account, quotaTestTime); u.Month.TokensUsed != tc.bound {
						t.Fatal("independent accounting changed", u)
					}
				}
			}
		})
	}
}

func TestProjectMonthlyBehaviorKeepsHardAndUnknownGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*QuotaLimit, *QuotaBound)
		want   error
	}{
		{"rolling", func(p *QuotaLimit, b *QuotaBound) { p.Tokens5H = limitPtr(0) }, ErrQuotaTokens},
		{"bound", func(p *QuotaLimit, b *QuotaBound) { b.Tokens = nil }, ErrQuotaBound},
		{"currency", func(p *QuotaLimit, b *QuotaBound) {
			p.MoneyMonth = moneyPtr("0")
			p.MoneyMonthBehavior = "alert_only"
			p.Currency = "USD"
			b.Currency = "EUR"
			b.Money = moneyPtr("1")
		}, ErrQuotaCurrency},
		{"scope", func(p *QuotaLimit, b *QuotaBound) { p.Account = "project_" }, ErrInvalid},
		{"mode", func(p *QuotaLimit, b *QuotaBound) { p.TokensMonthBehavior = "ALERT_ONLY" }, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p := quotaPolicy("project_prj_exact")
			p.TokensMonth = limitPtr(0)
			p.TokensMonthBehavior = "alert_only"
			b := tokenBound(1)
			tc.change(&p, &b)
			if err := q.PreflightWithQuota("project_guard", []QuotaLimit{p}, b, "UTC", quotaTestTime); !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
		})
	}
	q, _ := newQuotaTest(t, "UTC")
	p := quotaPolicy("project_prj_unknown")
	requireReserve(t, q, "unknown", []QuotaLimit{p}, QuotaBound{}, quotaTestTime)
	requireComplete(t, q, "unknown", QuotaSettlement{}, quotaTestTime)
	p.TokensMonth = limitPtr(0)
	p.TokensMonthBehavior = "alert_only"
	if err := q.PreflightWithQuota("after_unknown", []QuotaLimit{p}, tokenBound(1), "UTC", quotaTestTime); !errors.Is(err, ErrQuotaUnknown) {
		t.Fatal("unknown became available", err)
	}
}
