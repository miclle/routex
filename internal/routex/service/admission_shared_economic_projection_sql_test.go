package service

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func sharedEconomicProjectionFixture(t *testing.T) (entity.AdmissionEconomicReceipt, entity.AdmissionEconomicAccount) {
	t.Helper()
	row := sharedEconomicHistoryTestReceipt(t)
	intent, err := sharedEconomicReceiptIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	intent.Policies[0].CreatedAt = row.OwnerStartedAt.Add(-24 * time.Hour)
	intent.Digest = durableSharedSpoolDigest(t, intent)
	row.AdmissionDigest = intent.Digest
	row.IntentBody, err = sharedEconomicCanonicalBody(intent)
	if err != nil {
		t.Fatal(err)
	}
	policy := intent.Policies[0]
	return row, entity.AdmissionEconomicAccount{InstallationID: row.InstallationID,
		AccountDigest: sharedEconomicIdentityDigest(policy.Account), Account: policy.Account, Birth: policy.CreatedAt}
}

func TestSharedEconomicProjectionOriginalWindowsAndBounds(t *testing.T) {
	row, account := sharedEconomicProjectionFixture(t)
	intent, err := sharedEconomicReceiptIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	active := eventqueue.QuotaCounterContribution{Bound: intent.Bound}
	for _, kind := range []string{"rpm", "executor", "active_quota"} {
		window := "active"
		var quota *eventqueue.QuotaCounterContribution
		if kind == "rpm" {
			window = "minute"
		}
		if kind == "active_quota" {
			quota = &active
		}
		projection, err := newSharedEconomicProjection(row, account, kind, window, quota)
		if err != nil {
			t.Fatal(kind, err)
		}
		if (projection.ExpiresAt != nil) != (kind == "rpm") {
			t.Fatal("executor/active quota acquired timer expiry", kind)
		}
	}
	// Settlement in a later month still belongs to the admission month. The
	// planner deliberately has no completion-time argument that could shift it.
	settled := active
	settled.Settled = true
	knownMoney := "0.000000000000000001"
	settled.Actual = eventqueue.QuotaSettlement{Money: &knownMoney, Currency: "USD"}
	windows, err := sharedEconomicWindows(row.AdmittedAt, row.MonthStart, row.MonthEnd)
	if err != nil || len(windows) != 4 {
		t.Fatal("original accounting windows", err)
	}
	for _, window := range windows {
		projection, err := newSharedEconomicProjection(row, account, "terminal_quota", window.Name, &settled)
		if err != nil || projection.ExpiresAt == nil || *projection.ExpiresAt != window.End {
			t.Fatal("original exact expiry changed", window.Name, err)
		}
		body, err := sharedEconomicProjectionDecode(projection, account)
		if err != nil || body.Quota.Actual.Tokens != nil || body.Quota.Actual.Money == nil || body.Quota.Bound.Tokens == nil {
			t.Fatal("independent unknown token/known money dimension lost", err)
		}
		counter, err := eventqueue.QuotaCounterDelta(eventqueue.QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, *body.Quota, 1)
		if err != nil || counter.TokensHeld != *intent.Bound.Tokens || counter.MoneyUsed["USD"] != "1" || len(counter.MoneyHeld) != 0 {
			t.Fatal("unknown token did not retain original bound independently", counter, err)
		}
	}
	if windows[3].End != row.MonthEnd || !strings.HasPrefix(windows[3].Name, "month_") {
		t.Fatal("month was inferred from a settlement clock")
	}
}

func TestSharedEconomicProjectionRejectsCorruptSubtractionData(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*entity.AdmissionEconomicContribution, *entity.AdmissionEconomicAccount, *sharedEconomicProjectionBody)
	}{
		{"request_case", func(r *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, _ *sharedEconomicProjectionBody) {
			r.RequestID = strings.ToUpper(r.RequestID)
		}},
		{"account_case", func(_ *entity.AdmissionEconomicContribution, a *entity.AdmissionEconomicAccount, _ *sharedEconomicProjectionBody) {
			a.Account = strings.ToUpper(a.Account)
		}},
		{"reborn_account", func(_ *entity.AdmissionEconomicContribution, a *entity.AdmissionEconomicAccount, _ *sharedEconomicProjectionBody) {
			a.Birth = a.Birth.Add(time.Microsecond)
		}},
		{"foreign_installation", func(r *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, _ *sharedEconomicProjectionBody) {
			r.InstallationID = "led_01arz3ndektsv4rrffq69g5faw"
		}},
		{"unknown_kind", func(_ *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			b.Kind = "terminal_quotA"
		}},
		{"unknown_version", func(_ *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			b.Version++
		}},
		{"moved_month", func(_ *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			b.MonthStart++
		}},
		{"changed_expiry", func(r *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			n := *b.ExpiresAt + 1
			r.ExpiresAt, b.ExpiresAt = &n, &n
		}},
		{"missing_expiry", func(r *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			r.ExpiresAt, b.ExpiresAt = nil, nil
		}},
		{"missing_quota", func(_ *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			b.Quota = nil
		}},
		{"terminal_became_active", func(_ *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			b.Quota.Settled = false
		}},
		{"negative_usage", func(_ *entity.AdmissionEconomicContribution, _ *entity.AdmissionEconomicAccount, b *sharedEconomicProjectionBody) {
			n := int64(-1)
			b.Quota.Actual.Tokens = &n
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			receipt, account := sharedEconomicProjectionFixture(t)
			intent, err := sharedEconomicReceiptIntent(receipt)
			if err != nil {
				t.Fatal(err)
			}
			windows, err := sharedEconomicWindows(receipt.AdmittedAt, receipt.MonthStart, receipt.MonthEnd)
			if err != nil {
				t.Fatal(err)
			}
			row, err := newSharedEconomicProjection(receipt, account, "terminal_quota", windows[3].Name, &eventqueue.QuotaCounterContribution{Bound: intent.Bound, Settled: true})
			if err != nil {
				t.Fatal(err)
			}
			body, err := sharedEconomicProjectionDecode(row, account)
			if err != nil {
				t.Fatal(err)
			}
			test.change(&row, &account, &body)
			row.ContributionBody, err = sharedEconomicCanonicalBody(body)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sharedEconomicProjectionDecode(row, account); !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("corrupt persisted subtraction data accepted", err)
			}
		})
	}
}

func TestSharedEconomicProjectionNoTimerForUnknownActiveOrExecutor(t *testing.T) {
	receipt, account := sharedEconomicProjectionFixture(t)
	intent, err := sharedEconomicReceiptIntent(receipt)
	if err != nil {
		t.Fatal(err)
	}
	intent.Bound = eventqueue.QuotaBound{}
	intent.Recovery = eventqueue.QuotaSettlement{}
	intent.Reservation = sharedTestReservation(t, intent.RequestID, intent.Authorization, false, false)
	intent.Digest = durableSharedSpoolDigest(t, intent)
	receipt.AdmissionDigest = intent.Digest
	receipt.IntentBody, err = sharedEconomicCanonicalBody(intent)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"executor", "active_quota"} {
		var quota *eventqueue.QuotaCounterContribution
		if kind == "active_quota" {
			quota = &eventqueue.QuotaCounterContribution{}
		}
		row, err := newSharedEconomicProjection(receipt, account, kind, "active", quota)
		if err != nil || row.ExpiresAt != nil {
			t.Fatal("active unknown/executor not independently retained", kind, err)
		}
		body, err := sharedEconomicProjectionDecode(row, account)
		if err != nil {
			t.Fatal(err)
		}
		at := receipt.MonthEnd + int64(24*time.Hour)
		row.ExpiresAt, body.ExpiresAt = &at, &at
		row.ContributionBody, err = sharedEconomicCanonicalBody(body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sharedEconomicProjectionDecode(row, account); err == nil {
			t.Fatal("timer expiry invented release of an active contribution", kind)
		}
	}
}

func TestSharedEconomicProjectionWindowOverflowAndExactAccount(t *testing.T) {
	for _, values := range [][3]int64{{0, 0, 1}, {math.MaxInt64 - 1, 0, math.MaxInt64}, {20, 21, 30}, {20, 0, 20}, {20, -1, 30}} {
		if _, err := sharedEconomicWindows(values[0], values[1], values[2]); err == nil {
			t.Fatal("invalid/overflowing original window accepted", values)
		}
	}
	receipt, account := sharedEconomicProjectionFixture(t)
	account.Account = "usr_other"
	account.AccountDigest = sharedEconomicIdentityDigest(account.Account)
	if _, err := newSharedEconomicProjection(receipt, account, "executor", "active", nil); err == nil {
		t.Fatal("new account introduced outside the immutable accepted policy set")
	}
}

// Driver timestamp decoding may change the location without changing the birth.
// Canonical subtraction data must retain identical bytes for the same instant.
func TestSharedEconomicProjectionCanonicalAccountBirthZone(t *testing.T) {
	row, account := sharedEconomicProjectionFixture(t)
	intent, err := sharedEconomicReceiptIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	active := eventqueue.QuotaCounterContribution{Bound: intent.Bound}
	settled := eventqueue.QuotaCounterContribution{Bound: intent.Bound, Settled: true}
	for _, test := range []struct {
		kind, window string
		quota        *eventqueue.QuotaCounterContribution
	}{
		{"rpm", "minute", nil},
		{"executor", "active", nil},
		{"active_quota", "active", &active},
		{"terminal_quota", "minute", &settled},
	} {
		t.Run(test.kind, func(t *testing.T) {
			canonical := account
			canonical.Birth = account.Birth.UTC()
			original, err := newSharedEconomicProjection(row, canonical, test.kind, test.window, test.quota)
			if err != nil {
				t.Fatal("canonical UTC projection", err)
			}
			for _, offset := range []int{8 * 60 * 60, -5 * 60 * 60} {
				loaded := account
				loaded.Birth = account.Birth.In(time.FixedZone("driver timestamp", offset))
				projection, err := newSharedEconomicProjection(row, loaded, test.kind, test.window, test.quota)
				if err != nil || !bytes.Equal(projection.ContributionBody, original.ContributionBody) {
					t.Fatal("same recorded birth changed canonical subtraction bytes", offset, err)
				}
				body, err := sharedEconomicProjectionDecode(projection, loaded)
				if err != nil || body.AccountBirth.Location() != time.UTC || !body.AccountBirth.Equal(loaded.Birth) {
					t.Fatal("projection lost exact recorded birth or canonical UTC location", offset, err)
				}
			}
		})
	}
}
