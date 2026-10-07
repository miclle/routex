package eventqueue

import (
	"errors"
	"testing"
)

func TestPersonalKeyMonthlyBehaviorRequiresTrustedScopeProof(t *testing.T) {
	for _, tc := range []struct {
		name, account string
		proof         bool
		want          error
	}{
		{"personal", "key_key_original", true, nil}, {"unproven", "key_key_original", false, ErrInvalid},
		{"foreign_namespace", "project_key_original", true, ErrInvalid}, {"member", "team_member_original", true, ErrInvalid},
		{"empty", "key_", true, ErrInvalid}, {"malformed", "key_wrong/identity", true, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p := quotaPolicy(tc.account)
			p.TokensMonth = limitPtr(0)
			p.TokensMonthBehavior = "alert_only"
			p.PersonalKey = tc.proof
			if err := q.PreflightWithQuota("proof", []QuotaLimit{p}, tokenBound(1), "UTC", quotaTestTime); !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
		})
	}
}

func TestPersonalKeyMonthlyBehaviorNeverSuppressesHardUserOrOtherDimension(t *testing.T) {
	for _, tc := range []struct {
		name, umode, kmode, mmode string
		want                      error
	}{
		{"hard_User", "stop", "alert_only", "alert_only", ErrQuotaTokens},
		{"hard_Key", "alert_only", "stop", "alert_only", ErrQuotaTokens},
		{"hard_Key_money", "alert_only", "alert_only", "stop", ErrQuotaMoney},
		{"both_soft", "alert_only", "alert_only", "alert_only", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			u, k := quotaPolicy("user_usr_owner"), quotaPolicy("key_key_original")
			u.TokensMonth = limitPtr(0)
			u.TokensMonthBehavior = tc.umode
			k.PersonalKey = true
			k.TokensMonth = limitPtr(0)
			k.TokensMonthBehavior = tc.kmode
			k.MoneyMonth = moneyPtr("0")
			k.MoneyMonthBehavior = tc.mmode
			k.Currency = "USD"
			b := tokenBound(1)
			b.Money = moneyPtr("0.000000000000000001")
			b.Currency = "USD"
			err := q.ReserveWithQuota("mixed", []byte("f"), []QuotaLimit{u, k}, b, quotaTestTime)
			if !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
			if err == nil {
				requireComplete(t, q, "mixed", QuotaSettlement{Tokens: limitPtr(1), Money: moneyPtr("0.000000000000000001"), Currency: "USD"}, quotaTestTime)
				for _, p := range []QuotaLimit{u, k} {
					facts := requireUsage(t, q, p.Account, quotaTestTime)
					if facts.Month.TokensUsed != 1 || facts.Month.MoneyUsed["USD"] != "0.000000000000000001" {
						t.Fatal("settlement lost", facts)
					}
				}
			}
		})
	}
}

func TestPersonalKeyMonthlyBehaviorKeepsBoundsCoverageAndUnknownFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*QuotaLimit, *QuotaBound)
		want   error
	}{
		{"rolling", func(p *QuotaLimit, b *QuotaBound) { p.Tokens5H = limitPtr(0) }, ErrQuotaTokens},
		{"bound", func(p *QuotaLimit, b *QuotaBound) { b.Tokens = nil }, ErrQuotaBound},
		{"currency", func(p *QuotaLimit, b *QuotaBound) { b.Currency = "EUR" }, ErrQuotaCurrency},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p := quotaPolicy("key_key_original")
			p.PersonalKey = true
			p.TokensMonth = limitPtr(0)
			p.TokensMonthBehavior = "alert_only"
			p.MoneyMonth = moneyPtr("0")
			p.MoneyMonthBehavior = "alert_only"
			p.Currency = "USD"
			b := tokenBound(1)
			b.Money = moneyPtr("0.000000000000000001")
			b.Currency = "USD"
			tc.change(&p, &b)
			if err := q.PreflightWithQuota("guard", []QuotaLimit{p}, b, "UTC", quotaTestTime); !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
		})
	}
	q, _ := newQuotaTest(t, "UTC")
	p := quotaPolicy("key_key_unknown")
	p.PersonalKey = true
	requireReserve(t, q, "unknown", []QuotaLimit{p}, QuotaBound{}, quotaTestTime)
	requireComplete(t, q, "unknown", QuotaSettlement{}, quotaTestTime)
	p.TokensMonth = limitPtr(0)
	p.TokensMonthBehavior = "alert_only"
	if err := q.PreflightWithQuota("after_unknown", []QuotaLimit{p}, tokenBound(1), "UTC", quotaTestTime); !errors.Is(err, ErrQuotaUnknown) {
		t.Fatal("unknown became zero", err)
	}
}

func TestPersonalKeyMonthlyBehaviorDurableOriginalRootSettlement(t *testing.T) {
	q, path := newQuotaTest(t, "UTC")
	p := quotaPolicy("key_key_original")
	p.PersonalKey = true
	p.TokensMonth = limitPtr(0)
	p.TokensMonthBehavior = "alert_only"
	p.MoneyMonth = moneyPtr("0")
	p.MoneyMonthBehavior = "alert_only"
	p.Currency = "USD"
	b := tokenBound(1)
	b.Money = moneyPtr("0.000000000000000001")
	b.Currency = "USD"
	requireReserve(t, q, "original", []QuotaLimit{p}, b, quotaTestTime)
	requireComplete(t, q, "original", QuotaSettlement{Tokens: limitPtr(1), Money: b.Money, Currency: "USD"}, quotaTestTime)
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	facts := requireUsage(t, reopened, p.Account, quotaTestTime)
	if facts.Month.TokensUsed != 1 || facts.Month.MoneyUsed["USD"] != "0.000000000000000001" || facts.Active.TokensHeld != 0 {
		t.Fatal(facts)
	}
	// Rotation continues to use the original account; changing only the current
	// Key never creates an empty counter or alters the durable settlement.
	requireReserve(t, reopened, "rotated", []QuotaLimit{p}, b, quotaTestTime)
	requireComplete(t, reopened, "rotated", QuotaSettlement{Tokens: limitPtr(1), Money: b.Money, Currency: "USD"}, quotaTestTime)
	facts = requireUsage(t, reopened, p.Account, quotaTestTime)
	if facts.Month.TokensUsed != 2 || facts.Month.MoneyUsed["USD"] != "0.000000000000000002" {
		t.Fatal(facts)
	}
	p.TokensMonthBehavior = "stop"
	if err := reopened.PreflightWithQuota("restored_stop", []QuotaLimit{p}, b, "UTC", quotaTestTime); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal(err)
	}
}
