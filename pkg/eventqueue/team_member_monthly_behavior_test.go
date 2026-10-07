package eventqueue

import (
	"errors"
	"strings"
	"testing"
)

func TestTeamMemberMonthlyBehaviorIndependentProofAndDimensions(t *testing.T) {
	for _, tc := range []struct {
		name, pmode, kmode, mmode string
		proof, personal           bool
		want                      error
	}{
		{"both_soft", "alert_only", "alert_only", "alert_only", true, false, nil},
		{"hard_Team", "stop", "alert_only", "alert_only", true, false, ErrQuotaTokens},
		{"hard_member", "alert_only", "stop", "alert_only", true, false, ErrQuotaTokens},
		{"hard_money", "alert_only", "alert_only", "stop", true, false, ErrQuotaMoney},
		{"unproved", "alert_only", "alert_only", "alert_only", false, false, ErrInvalid},
		{"ambiguous_proof", "alert_only", "alert_only", "alert_only", true, true, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p, k := quotaPolicy("team_tea_exact"), quotaPolicy("team_member_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
			p.TokensMonth = limitPtr(0)
			p.TokensMonthBehavior = tc.pmode
			k.TeamMember = tc.proof
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
func TestTeamMemberMonthlyBehaviorHardCapacityAndScope(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	p, k := quotaPolicy("team_tea_exact"), quotaPolicy("team_member_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	p.TokensMonth = limitPtr(100)
	p.TokensMonthBehavior = "alert_only"
	k.TeamMember = true
	k.TokensMonth = limitPtr(200)
	requireReserve(t, q, "accepted", []QuotaLimit{p, k}, tokenBound(150), quotaTestTime)
	requireComplete(t, q, "accepted", QuotaSettlement{Tokens: limitPtr(150)}, quotaTestTime)
	if err := q.PreflightWithQuota("over_member", []QuotaLimit{p, k}, tokenBound(51), "UTC", quotaTestTime); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal(err)
	}
	for _, account := range []string{"team_member_wrong", "team_member_" + strings.Repeat("A", 51), "team_member_" + strings.Repeat("a", 52), "team_member_" + strings.Repeat("A", 51) + "B", "key_key_other"} {
		k.Account = account
		k.TokensMonthBehavior = "alert_only"
		if err := q.PreflightWithQuota("bad", []QuotaLimit{k}, tokenBound(1), "UTC", quotaTestTime); !errors.Is(err, ErrInvalid) {
			t.Fatal(account, err)
		}
	}
}

func TestTeamMemberMonthlyBehaviorSoftRetainsUnknownBoundsCurrencyAndHolds(t *testing.T) {
	for _, kind := range []string{"unknown_tokens", "unknown_money", "currency", "missing_bound", "null_inert"} {
		t.Run(kind, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			p := quotaPolicy("team_member_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
			p.TeamMember = true
			p.TokensMonthBehavior, p.MoneyMonthBehavior = "alert_only", "alert_only"
			if kind == "null_inert" {
				requireReserve(t, q, "null", []QuotaLimit{p}, QuotaBound{}, quotaTestTime)
				requireComplete(t, q, "null", QuotaSettlement{}, quotaTestTime)
				return
			}
			if kind != "missing_bound" {
				bound := QuotaBound{}
				settlement := QuotaSettlement{Tokens: limitPtr(3)}
				switch kind {
				case "unknown_tokens":
					settlement = QuotaSettlement{Money: moneyPtr("1"), Currency: "USD"}
				case "currency":
					settlement.Money, settlement.Currency = moneyPtr("1"), "EUR"
					bound.Money, bound.Currency = moneyPtr("1"), "EUR"
				}
				requireReserve(t, q, "history", []QuotaLimit{p}, bound, quotaTestTime)
				requireComplete(t, q, "history", settlement, quotaTestTime)
			}
			p.TokensMonth, p.MoneyMonth, p.Currency = limitPtr(0), moneyPtr("0"), "USD"
			b := tokenBound(5)
			b.Money, b.Currency = moneyPtr("1"), "USD"
			want := ErrQuotaUnknown
			switch kind {
			case "currency":
				want = ErrQuotaCurrency
			case "missing_bound":
				b = QuotaBound{}
				want = ErrQuotaBound
			}
			if err := q.ReserveWithQuota("reject", []byte("f"), []QuotaLimit{p}, b, quotaTestTime); !errors.Is(err, want) {
				t.Fatal(kind, err, want)
			}
			u := requireUsage(t, q, p.Account, quotaTestTime)
			if u.Active.TokensHeld != 0 || len(u.Active.MoneyHeld) != 0 {
				t.Fatal("rejection created holds", u.Active)
			}
		})
	}
}
