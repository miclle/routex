package eventqueue

import (
	"errors"
	"testing"
)

func TestProjectKeyMonthlyBehaviorIndependentProofAndDimensions(t *testing.T) {
	for _, tc := range []struct {
		name, pmode, kmode, mmode string
		proof, personal           bool
		want                      error
	}{
		{"both_soft", "alert_only", "alert_only", "alert_only", true, false, nil},
		{"hard_Project", "stop", "alert_only", "alert_only", true, false, ErrQuotaTokens},
		{"hard_Key", "alert_only", "stop", "alert_only", true, false, ErrQuotaTokens},
		{"hard_money", "alert_only", "alert_only", "stop", true, false, ErrQuotaMoney},
		{"unproved", "alert_only", "alert_only", "alert_only", false, false, ErrInvalid},
		{"ambiguous_proof", "alert_only", "alert_only", "alert_only", true, true, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p, k := quotaPolicy("project_prj_exact"), quotaPolicy("key_pky_original")
			p.TokensMonth = limitPtr(0)
			p.TokensMonthBehavior = tc.pmode
			k.ProjectKey = tc.proof
			k.PersonalKey = tc.personal
			k.TokensMonth = limitPtr(0)
			k.TokensMonthBehavior = tc.kmode
			k.MoneyMonth = moneyPtr("0")
			k.MoneyMonthBehavior = tc.mmode
			k.Currency = "USD"
			b := tokenBound(150)
			b.Money = moneyPtr("0.000000000000000001")
			b.Currency = "USD"
			err := q.ReserveWithQuota("mixed", []byte("f"), []QuotaLimit{p, k}, b, quotaTestTime)
			if !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
			if err == nil {
				requireComplete(t, q, "mixed", QuotaSettlement{Tokens: limitPtr(150), Money: moneyPtr("0.000000000000000001"), Currency: "USD"}, quotaTestTime)
				for _, account := range []string{p.Account, k.Account} {
					u := requireUsage(t, q, account, quotaTestTime)
					if u.Month.TokensUsed != 150 || u.Month.MoneyUsed["USD"] != "0.000000000000000001" {
						t.Fatal("settlement changed", u)
					}
				}
			}
		})
	}
}
func TestProjectKeyMonthlyBehaviorHardCapacityAndScope(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	p, k := quotaPolicy("project_prj_exact"), quotaPolicy("key_pky_original")
	p.TokensMonth = limitPtr(100)
	p.TokensMonthBehavior = "alert_only"
	k.ProjectKey = true
	k.TokensMonth = limitPtr(200)
	requireReserve(t, q, "accepted", []QuotaLimit{p, k}, tokenBound(150), quotaTestTime)
	requireComplete(t, q, "accepted", QuotaSettlement{Tokens: limitPtr(150)}, quotaTestTime)
	if err := q.PreflightWithQuota("over_Key", []QuotaLimit{p, k}, tokenBound(51), "UTC", quotaTestTime); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal(err)
	}
	for _, account := range []string{"key_key_personal", "key_PKY_alias", "key_pky_", "team_member_other", "key_pky_wrong/identity"} {
		k.Account = account
		k.TokensMonthBehavior = "alert_only"
		if err := q.PreflightWithQuota("bad", []QuotaLimit{k}, tokenBound(1), "UTC", quotaTestTime); !errors.Is(err, ErrInvalid) {
			t.Fatal(account, err)
		}
	}
}
