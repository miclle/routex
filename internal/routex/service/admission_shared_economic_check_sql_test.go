package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

type sharedEconomicCheckFixture struct {
	domain   entity.AdmissionEconomicDomain
	accounts []entity.AdmissionEconomicAccount
	policies []eventqueue.QuotaLimit
	bound    eventqueue.QuotaBound
	instant  int64
	month    int64
}

func newSharedEconomicCheckFixture() sharedEconomicCheckFixture {
	instant := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	birth := instant.AddDate(0, -2, 0)
	installation := "led_01arz3ndektsv4rrffq69g5fav"
	f := sharedEconomicCheckFixture{
		domain: entity.AdmissionEconomicDomain{InstallationID: installation, FormatVersion: 1, Initialized: true,
			CoverageStart: instant.AddDate(0, -1, 0).UnixNano(), ClockHighWater: instant.UnixNano(), ReceiptCapacity: 100},
		bound:   eventqueue.QuotaBound{Tokens: economicCheckPtr[int64](1), Money: economicCheckPtr("0.000000000000000001"), Currency: "USD", Revision: "bound_current"},
		instant: instant.UnixNano(), month: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
	}
	// Locks are sorted; rejection priority follows the original parent/root order.
	for _, account := range []string{"key_personal", "user_member"} {
		f.accounts = append(f.accounts, entity.AdmissionEconomicAccount{InstallationID: installation,
			AccountDigest: sharedEconomicIdentityDigest(account), Account: account, Birth: birth})
	}
	for _, account := range []string{"user_member", "key_personal"} {
		f.policies = append(f.policies, eventqueue.QuotaLimit{Limit: eventqueue.Limit{Account: account}, Revision: "policy_current", CreatedAt: birth})
	}
	return f
}

func economicCheckPtr[T any](value T) *T { return &value }

func emptySharedEconomicCheckCounter() eventqueue.QuotaCounter {
	return eventqueue.QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
}

func (f sharedEconomicCheckFixture) check(readers sharedEconomicCheckReaders) error {
	return checkSharedEconomicAdmission(f.domain, f.accounts, f.policies, f.bound, f.instant, f.month, readers)
}

func sharedEconomicCheckEmptyReaders() sharedEconomicCheckReaders {
	return sharedEconomicCheckReaders{
		Counter: func(entity.AdmissionEconomicAccount, string) (eventqueue.QuotaCounter, error) {
			return emptySharedEconomicCheckCounter(), nil
		},
		InvalidBound: func(string) (bool, error) { return false, nil },
	}
}

func TestSharedEconomicCheckOrderedLazyReadsAndImmutability(t *testing.T) {
	f := newSharedEconomicCheckFixture()
	p := &f.policies[0]
	p.TPM, p.Tokens5H, p.Tokens7D, p.TokensMonth = economicCheckPtr[int64](100), economicCheckPtr[int64](100), economicCheckPtr[int64](100), economicCheckPtr[int64](100)
	p.MoneyMonth, p.Currency = economicCheckPtr("1"), "USD"
	before, err := json.Marshal([]any{f.policies, f.bound})
	if err != nil {
		t.Fatal(err)
	}
	beforeAccounts, beforeDomain := slices.Clone(f.accounts), f.domain
	var reads []string
	readers := sharedEconomicCheckEmptyReaders()
	readers.Counter = func(account entity.AdmissionEconomicAccount, window string) (eventqueue.QuotaCounter, error) {
		reads = append(reads, account.Account+":"+window)
		return emptySharedEconomicCheckCounter(), nil
	}
	readers.InvalidBound = func(revision string) (bool, error) { reads = append(reads, "bound:"+revision); return false, nil }
	if err := f.check(readers); err != nil {
		t.Fatal(err)
	}
	month := "month_" + strconv.FormatInt(f.month, 10)
	want := []string{"user_member:active", "user_member:minute", "user_member:five_hours", "user_member:seven_days",
		"user_member:" + month, "user_member:" + month, "bound:bound_current", "key_personal:active"}
	if !reflect.DeepEqual(reads, want) {
		t.Fatalf("reads = %v; want %v", reads, want)
	}
	after, err := json.Marshal([]any{f.policies, f.bound})
	if err != nil || string(before) != string(after) || !reflect.DeepEqual(beforeAccounts, f.accounts) || !reflect.DeepEqual(beforeDomain, f.domain) {
		t.Fatal("check mutated policy, bound, account or domain", err)
	}
}

func TestSharedEconomicCheckRatesBeforeQuotaPolicies(t *testing.T) {
	f := newSharedEconomicCheckFixture()
	f.policies[0].RPM = economicCheckPtr[int64](0)
	f.policies[1].TokensMonth = economicCheckPtr[int64](0)
	reads := 0
	readers := sharedEconomicCheckEmptyReaders()
	readers.Counter = func(entity.AdmissionEconomicAccount, string) (eventqueue.QuotaCounter, error) {
		reads++
		return emptySharedEconomicCheckCounter(), nil
	}
	if err := f.check(readers); !errors.Is(err, eventqueue.ErrRateLimit) || reads != 0 {
		t.Fatal("quota work replaced the original rate rejection", err, reads)
	}
	f.policies[0].RPM = nil
	if err := f.check(readers); !errors.Is(err, eventqueue.ErrQuotaTokens) {
		t.Fatal("root hard quota did not remain conjunctive", err)
	}
	f.policies[1].TokensMonth = nil
	f.policies[0].Concurrency = economicCheckPtr[int64](0)
	f.policies[1].RPM = economicCheckPtr[int64](0)
	if err := f.check(sharedEconomicCheckEmptyReaders()); !errors.Is(err, eventqueue.ErrConcurrency) {
		t.Fatal("rate checks lost original parent order", err)
	}
}

func TestSharedEconomicCheckRateAndExecutorCountsStayIndependent(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*sharedEconomicCheckFixture)
		want error
	}{
		{"unset", func(f *sharedEconomicCheckFixture) { f.accounts[1].RPMCount, f.accounts[1].ExecutorCount = 100, 100 }, nil},
		{"rpm_at_ceiling", func(f *sharedEconomicCheckFixture) {
			f.policies[0].RPM = economicCheckPtr[int64](1)
			f.accounts[1].RPMCount = 1
		}, eventqueue.ErrRateLimit},
		{"rpm_below_ceiling", func(f *sharedEconomicCheckFixture) {
			f.policies[0].RPM = economicCheckPtr[int64](1)
			f.accounts[1].ExecutorCount = 100
		}, nil},
		{"executor_at_ceiling", func(f *sharedEconomicCheckFixture) {
			f.policies[0].Concurrency = economicCheckPtr[int64](1)
			f.accounts[1].ExecutorCount = 1
		}, eventqueue.ErrConcurrency},
		{"executor_below_ceiling", func(f *sharedEconomicCheckFixture) {
			f.policies[0].Concurrency = economicCheckPtr[int64](1)
			f.accounts[1].RPMCount = 100
		}, nil},
		{"negative_rpm", func(f *sharedEconomicCheckFixture) { f.policies[0].RPM = economicCheckPtr[int64](-1) }, eventqueue.ErrRateLimit},
		{"negative_concurrency", func(f *sharedEconomicCheckFixture) { f.policies[0].Concurrency = economicCheckPtr[int64](-1) }, eventqueue.ErrConcurrency},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSharedEconomicCheckFixture()
			tc.edit(&f)
			if err := f.check(sharedEconomicCheckEmptyReaders()); !errors.Is(err, tc.want) {
				t.Fatalf("check = %v; want %v", err, tc.want)
			}
		})
	}
}

func TestSharedEconomicCheckRejectsInventedAccountsAndCoverage(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*sharedEconomicCheckFixture)
	}{
		{"missing_account", func(f *sharedEconomicCheckFixture) { f.accounts = f.accounts[:1] }},
		{"unsorted_locks", func(f *sharedEconomicCheckFixture) { f.accounts[0], f.accounts[1] = f.accounts[1], f.accounts[0] }},
		{"duplicate_locks", func(f *sharedEconomicCheckFixture) { f.accounts[0] = f.accounts[1] }},
		{"case_changed_account", func(f *sharedEconomicCheckFixture) { f.accounts[1].Account = "USER_member" }},
		{"case_changed_policy", func(f *sharedEconomicCheckFixture) { f.policies[0].Account = "USER_member" }},
		{"duplicate_policy", func(f *sharedEconomicCheckFixture) { f.policies[1] = f.policies[0] }},
		{"foreign_installation", func(f *sharedEconomicCheckFixture) { f.accounts[0].InstallationID = "led_01arz3ndektsv4rrffq69g5faw" }},
		{"reborn", func(f *sharedEconomicCheckFixture) {
			f.policies[0].CreatedAt = f.policies[0].CreatedAt.Add(time.Microsecond)
		}},
		{"missing_birth", func(f *sharedEconomicCheckFixture) { f.accounts[1].Birth = time.Time{} }},
		{"negative_count", func(f *sharedEconomicCheckFixture) { f.accounts[0].RPMCount = -1 }},
		{"uninitialized", func(f *sharedEconomicCheckFixture) { f.domain.Initialized = false }},
		{"missing_coverage", func(f *sharedEconomicCheckFixture) { f.domain.CoverageStart = 0 }},
		{"backward_clock", func(f *sharedEconomicCheckFixture) { f.instant-- }},
		{"future_month", func(f *sharedEconomicCheckFixture) { f.month = f.instant + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSharedEconomicCheckFixture()
			tc.edit(&f)
			reads := 0
			readers := sharedEconomicCheckEmptyReaders()
			readers.Counter = func(entity.AdmissionEconomicAccount, string) (eventqueue.QuotaCounter, error) {
				reads++
				return emptySharedEconomicCheckCounter(), nil
			}
			if err := f.check(readers); err == nil || reads != 0 {
				t.Fatal("invented coordinates reached counters", err, reads)
			}
		})
	}
}

func TestSharedEconomicCheckCoverageUsesRetainedBirth(t *testing.T) {
	f := newSharedEconomicCheckFixture()
	f.domain.CoverageStart = f.instant
	f.policies[0].Tokens5H = economicCheckPtr[int64](10)
	if err := f.check(sharedEconomicCheckEmptyReaders()); !errors.Is(err, eventqueue.ErrQuotaCoverage) {
		t.Fatal("old account invented prehistory", err)
	}
	birth := time.Unix(0, f.instant).UTC()
	f.policies[0].CreatedAt, f.accounts[1].Birth = birth, birth
	if err := f.check(sharedEconomicCheckEmptyReaders()); err != nil {
		t.Fatal("exact newborn account falsely required old coverage", err)
	}
}

func TestSharedEconomicCheckExactMoneySoftAndParentHard(t *testing.T) {
	f := newSharedEconomicCheckFixture()
	f.policies[0].MoneyMonth, f.policies[0].Currency = economicCheckPtr("0.000000000000000002"), "USD"
	readers := sharedEconomicCheckEmptyReaders()
	readers.Counter = func(account entity.AdmissionEconomicAccount, window string) (eventqueue.QuotaCounter, error) {
		c := emptySharedEconomicCheckCounter()
		if account.Account == "user_member" && window == "active" {
			c.TokensUnknown = 1 // An unknown unrelated dimension is not money usage.
			c.MoneyHeld["USD"] = "1"
		}
		return c, nil
	}
	if err := f.check(readers); err != nil {
		t.Fatal("exact two-unit money boundary or independent tokens changed", err)
	}
	f.policies[0].MoneyMonth = economicCheckPtr("0.000000000000000001")
	if err := f.check(readers); !errors.Is(err, eventqueue.ErrQuotaMoney) {
		t.Fatal("one-unit overflow accepted", err)
	}
	f.policies[0].MoneyMonth, f.policies[0].MoneyMonthBehavior = economicCheckPtr("0"), "alert_only"
	if err := f.check(readers); err != nil {
		t.Fatal("alert-only enforced its ceiling", err)
	}
	f.policies[1].MoneyMonth, f.policies[1].Currency = economicCheckPtr("0"), "USD"
	if err := f.check(readers); !errors.Is(err, eventqueue.ErrQuotaMoney) {
		t.Fatal("parent soft rule bypassed independent root hard zero", err)
	}
	f.policies[1].MoneyMonth = nil
	readers.Counter = func(entity.AdmissionEconomicAccount, string) (eventqueue.QuotaCounter, error) {
		c := emptySharedEconomicCheckCounter()
		c.MoneyUnknown = 1
		return c, nil
	}
	if err := f.check(readers); !errors.Is(err, eventqueue.ErrQuotaUnknown) {
		t.Fatal("soft rule bypassed unknown money", err)
	}
}

func TestSharedEconomicCheckLazyFailuresAndInvalidBound(t *testing.T) {
	f := newSharedEconomicCheckFixture()
	f.policies[0].Tokens5H = economicCheckPtr[int64](10)
	windowFailure, boundFailure := errors.New("counter unavailable"), errors.New("invalid-bound history unavailable")
	readers := sharedEconomicCheckEmptyReaders()
	bounds := 0
	readers.InvalidBound = func(revision string) (bool, error) { bounds++; return true, boundFailure }
	readers.Counter = func(_ entity.AdmissionEconomicAccount, window string) (eventqueue.QuotaCounter, error) {
		if window == "five_hours" {
			return eventqueue.QuotaCounter{}, windowFailure
		}
		return emptySharedEconomicCheckCounter(), nil
	}
	if err := f.check(readers); !errors.Is(err, windowFailure) || bounds != 0 {
		t.Fatal("later bound read masked exact window failure", err, bounds)
	}
	readers.Counter = sharedEconomicCheckEmptyReaders().Counter
	if err := f.check(readers); !errors.Is(err, boundFailure) || bounds != 1 {
		t.Fatal("invalid-bound read error lost", err, bounds)
	}
	readers.InvalidBound = func(revision string) (bool, error) {
		if revision != f.bound.Revision {
			t.Fatal("bound revision substituted", revision)
		}
		return true, nil
	}
	if err := f.check(readers); !errors.Is(err, eventqueue.ErrQuotaBound) {
		t.Fatal("permanent invalid original bound allowed", err)
	}
	readers.Counter = func(entity.AdmissionEconomicAccount, string) (eventqueue.QuotaCounter, error) {
		return eventqueue.QuotaCounter{MoneyHeld: map[string]string{}, MoneyUsed: map[string]string{"USD": "01"}}, nil
	}
	if err := f.check(readers); !errors.Is(err, eventqueue.ErrInvalid) {
		t.Fatal("noncanonical accumulated money accepted", err)
	}
}

func TestSharedEconomicInvalidBoundExactFirstCause(t *testing.T) {
	f := newSharedEconomicCheckFixture()
	row := entity.AdmissionEconomicInvalidBound{InstallationID: f.domain.InstallationID, Revision: f.bound.Revision,
		RevisionDigest: sharedEconomicIdentityDigest(f.bound.Revision), RequestID: "req_FirstCause",
		RequestDigest: sharedEconomicIdentityDigest("req_FirstCause"), RecordedAt: f.instant - 1}
	if !sharedEconomicInvalidBoundMatches(row, f.domain.InstallationID, f.bound.Revision, f.instant) {
		t.Fatal("exact first cause refused")
	}
	for _, change := range []func(*entity.AdmissionEconomicInvalidBound){
		func(r *entity.AdmissionEconomicInvalidBound) { r.Revision = "bound_CURRENT" },
		func(r *entity.AdmissionEconomicInvalidBound) { r.RequestID = "req_firstcause" },
		func(r *entity.AdmissionEconomicInvalidBound) { r.InstallationID = "led_01arz3ndektsv4rrffq69g5faw" },
		func(r *entity.AdmissionEconomicInvalidBound) { r.RecordedAt = f.instant + 1 },
		func(r *entity.AdmissionEconomicInvalidBound) { r.RecordedAt = 0 },
	} {
		other := row
		change(&other)
		if sharedEconomicInvalidBoundMatches(other, f.domain.InstallationID, f.bound.Revision, f.instant) {
			t.Fatal("corrupt or future first cause accepted")
		}
	}
}
