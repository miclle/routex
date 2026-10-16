package eventqueue

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func quotaRulesFrame() QuotaDecisionFrame {
	return QuotaDecisionFrame{Instant: quotaTestTime.Add(8 * 24 * time.Hour).UnixNano(), MonthStart: quotaTestTime.UnixNano(), CoverageStart: quotaTestTime.UnixNano()}
}

func emptyQuotaRulesCounter() QuotaCounter {
	return QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
}

func quotaRulesView() QuotaPolicyView {
	return QuotaPolicyView{Active: emptyQuotaRulesCounter(), Window: func(string) (QuotaCounter, error) { return emptyQuotaRulesCounter(), nil }, InvalidBound: func(string) (bool, error) { return false, nil }}
}

func requirePolicyRules(t *testing.T, policy QuotaLimit, bound QuotaBound, frame QuotaDecisionFrame) QuotaPolicyRules {
	t.Helper()
	rules, err := NewQuotaPolicyRules(policy, bound, frame)
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestQuotaPolicyRulesLazyReadOrder(t *testing.T) {
	policy := quotaPolicy("user_rules")
	policy.TPM, policy.Tokens5H, policy.Tokens7D, policy.TokensMonth = limitPtr(10), limitPtr(10), limitPtr(10), limitPtr(10)
	policy.MoneyMonth, policy.Currency = moneyPtr("10"), "USD"
	bound := tokenBound(1)
	bound.Money, bound.Currency = moneyPtr("1"), "USD"
	rules := requirePolicyRules(t, policy, bound, quotaRulesFrame())
	var reads []string
	view := quotaRulesView()
	view.Window = func(name string) (QuotaCounter, error) {
		reads = append(reads, name)
		return emptyQuotaRulesCounter(), nil
	}
	view.InvalidBound = func(revision string) (bool, error) {
		reads = append(reads, "bound:"+revision)
		return false, nil
	}
	if err := rules.Check(view); err != nil {
		t.Fatal(err)
	}
	month := monthWindow(quotaRulesFrame().MonthStart)
	want := []string{"minute", "five_hours", "seven_days", month, month, "bound:bound_v1"}
	if !reflect.DeepEqual(reads, want) {
		t.Fatalf("lazy reads = %v; want %v", reads, want)
	}
	unconstrained := requirePolicyRules(t, quotaPolicy("user_unconstrained"), QuotaBound{}, quotaRulesFrame())
	if err := unconstrained.Check(QuotaPolicyView{}); err != nil {
		t.Fatal("unconstrained policy unexpectedly read storage", err)
	}
}

func TestQuotaPolicyRulesOrderedRejections(t *testing.T) {
	errWindow := errors.New("window unavailable")
	errInvalidBound := errors.New("bound ledger unavailable")
	for _, test := range []struct {
		name        string
		configure   func(*QuotaLimit, *QuotaBound, *QuotaDecisionFrame, *QuotaPolicyView)
		want        error
		wantWindows int
		wantBound   int
	}{
		{"negative_ceiling", func(p *QuotaLimit, _ *QuotaBound, _ *QuotaDecisionFrame, _ *QuotaPolicyView) {
			p.Tokens5H = limitPtr(-1)
		}, ErrInvalid, 0, 0},
		{"zero_before_coverage", func(p *QuotaLimit, b *QuotaBound, f *QuotaDecisionFrame, _ *QuotaPolicyView) {
			p.Tokens5H = limitPtr(0)
			b.Tokens = nil
			f.CoverageStart = f.Instant
		}, ErrQuotaTokens, 0, 0},
		{"coverage_before_bound", func(_ *QuotaLimit, b *QuotaBound, f *QuotaDecisionFrame, _ *QuotaPolicyView) {
			b.Tokens = nil
			f.CoverageStart = f.Instant
		}, ErrQuotaCoverage, 0, 0},
		{"bound_before_window", func(_ *QuotaLimit, b *QuotaBound, _ *QuotaDecisionFrame, _ *QuotaPolicyView) { b.Tokens = nil }, ErrQuotaBound, 0, 0},
		{"window_before_active_unknown", func(_ *QuotaLimit, _ *QuotaBound, _ *QuotaDecisionFrame, v *QuotaPolicyView) {
			v.Active.TokensUnknown = 1
			v.Window = func(string) (QuotaCounter, error) { return QuotaCounter{}, errWindow }
		}, errWindow, 1, 0},
		{"unknown_before_ceiling", func(_ *QuotaLimit, _ *QuotaBound, _ *QuotaDecisionFrame, v *QuotaPolicyView) {
			v.Active.TokensUnknown = 1
			v.Active.TokensHeld = 10
		}, ErrQuotaUnknown, 1, 0},
		{"ceiling_before_invalid_bound", func(_ *QuotaLimit, _ *QuotaBound, _ *QuotaDecisionFrame, v *QuotaPolicyView) {
			v.Active.TokensHeld = 10
		}, ErrQuotaTokens, 1, 0},
		{"invalid_bound_after_windows", func(_ *QuotaLimit, _ *QuotaBound, _ *QuotaDecisionFrame, v *QuotaPolicyView) {
			v.InvalidBound = func(string) (bool, error) { return false, errInvalidBound }
		}, errInvalidBound, 1, 1},
		{"empty_revision_still_reads_bound", func(_ *QuotaLimit, b *QuotaBound, _ *QuotaDecisionFrame, _ *QuotaPolicyView) { b.Revision = "" }, ErrQuotaBound, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := quotaPolicy("user_rules")
			policy.Tokens5H = limitPtr(10)
			bound, frame, view := tokenBound(1), quotaRulesFrame(), quotaRulesView()
			test.configure(&policy, &bound, &frame, &view)
			windows, bounds := 0, 0
			window, invalid := view.Window, view.InvalidBound
			view.Window = func(name string) (QuotaCounter, error) { windows++; return window(name) }
			view.InvalidBound = func(revision string) (bool, error) { bounds++; return invalid(revision) }
			err := requirePolicyRules(t, policy, bound, frame).Check(view)
			if !errors.Is(err, test.want) || windows != test.wantWindows || bounds != test.wantBound {
				t.Fatalf("check = %v, windows=%d, bounds=%d; want %v/%d/%d", err, windows, bounds, test.want, test.wantWindows, test.wantBound)
			}
		})
	}
}

func TestQuotaPolicyRulesKnownZeroCoverageAndOverflow(t *testing.T) {
	policy := quotaPolicy("user_rules")
	policy.Tokens5H = limitPtr(math.MaxInt64)
	view := quotaRulesView()
	view.Active.TokensHeld = math.MaxInt64 - 1
	if err := requirePolicyRules(t, policy, tokenBound(1), quotaRulesFrame()).Check(view); err != nil {
		t.Fatal("exact maximum rejected", err)
	}
	view.Window = func(string) (QuotaCounter, error) { c := emptyQuotaRulesCounter(); c.TokensUsed = 1; return c, nil }
	if err := requirePolicyRules(t, policy, tokenBound(1), quotaRulesFrame()).Check(view); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal("overflowing total admitted", err)
	}
	policy.Tokens5H = limitPtr(1)
	frame := quotaRulesFrame()
	frame.CoverageStart = frame.Instant
	policy.CreatedAt = time.Unix(0, frame.Instant)
	if err := requirePolicyRules(t, policy, tokenBound(0), frame).Check(quotaRulesView()); err != nil {
		t.Fatal("new exact-birth resource lacked prehistory", err)
	}
	policy.CreatedAt = time.Time{}
	if err := requirePolicyRules(t, policy, tokenBound(0), frame).Check(quotaRulesView()); !errors.Is(err, ErrQuotaCoverage) {
		t.Fatal("unknown birth invented coverage", err)
	}
}

func TestQuotaPolicyRulesMonthlyAlertsRemainConjunctive(t *testing.T) {
	soft := quotaPolicy("user_rules")
	soft.TokensMonth, soft.TokensMonthBehavior = limitPtr(0), "alert_only"
	view := quotaRulesView()
	view.Active.TokensHeld = 100
	if err := requirePolicyRules(t, soft, tokenBound(10), quotaRulesFrame()).Check(view); err != nil {
		t.Fatal("alert-only applied hard ceiling", err)
	}
	parent := quotaPolicy("user_parent")
	parent.TokensMonth = limitPtr(0)
	if err := requirePolicyRules(t, parent, tokenBound(10), quotaRulesFrame()).Check(view); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal("child alert-only bypassed parent hard zero", err)
	}
	view.Active.TokensUnknown = 1
	if err := requirePolicyRules(t, soft, tokenBound(10), quotaRulesFrame()).Check(view); !errors.Is(err, ErrQuotaUnknown) {
		t.Fatal("alert-only bypassed unknown usage", err)
	}
	if err := requirePolicyRules(t, soft, QuotaBound{}, quotaRulesFrame()).Check(quotaRulesView()); !errors.Is(err, ErrQuotaBound) {
		t.Fatal("alert-only bypassed missing bound", err)
	}
	frame := quotaRulesFrame()
	frame.CoverageStart = frame.Instant
	if err := requirePolicyRules(t, soft, tokenBound(1), frame).Check(quotaRulesView()); !errors.Is(err, ErrQuotaCoverage) {
		t.Fatal("alert-only bypassed coverage", err)
	}
	soft.TPM = limitPtr(0)
	if err := requirePolicyRules(t, soft, tokenBound(1), quotaRulesFrame()).Check(quotaRulesView()); !errors.Is(err, ErrQuotaTokens) {
		t.Fatal("monthly mode bypassed minute policy", err)
	}
}

func TestQuotaPolicyRulesMoneyAndIndependentUnknowns(t *testing.T) {
	policy := quotaPolicy("user_rules")
	policy.MoneyMonth, policy.Currency = moneyPtr("10.000000000000000001"), "USD"
	bound := QuotaBound{Money: moneyPtr("0.000000000000000001"), Currency: "USD", Revision: "bound_money"}
	view := quotaRulesView()
	view.Active.MoneyHeld["USD"] = "10000000000000000000"
	view.Active.TokensUnknown = 1
	if err := requirePolicyRules(t, policy, bound, quotaRulesFrame()).Check(view); err != nil {
		t.Fatal("exact money or independent unknown tokens rejected", err)
	}
	view.Window = func(string) (QuotaCounter, error) {
		c := emptyQuotaRulesCounter()
		c.MoneyUsed["USD"] = "1"
		return c, nil
	}
	if err := requirePolicyRules(t, policy, bound, quotaRulesFrame()).Check(view); !errors.Is(err, ErrQuotaMoney) {
		t.Fatal("fractional overrun admitted", err)
	}
	policy.MoneyMonthBehavior = "alert_only"
	if err := requirePolicyRules(t, policy, bound, quotaRulesFrame()).Check(view); err != nil {
		t.Fatal("money alert-only enforced ceiling", err)
	}
	view.Active.MoneyUnknown = 1
	if err := requirePolicyRules(t, policy, bound, quotaRulesFrame()).Check(view); !errors.Is(err, ErrQuotaUnknown) {
		t.Fatal("money alert-only bypassed unknown", err)
	}
	view.Active.MoneyUnknown = 0
	view.Active.MoneyHeld["EUR"] = "0"
	if err := requirePolicyRules(t, policy, bound, quotaRulesFrame()).Check(view); !errors.Is(err, ErrQuotaCurrency) {
		t.Fatal("foreign zero currency silently ignored", err)
	}
	delete(view.Active.MoneyHeld, "EUR")
	bound.Currency = "EUR"
	if err := requirePolicyRules(t, policy, bound, quotaRulesFrame()).Check(view); !errors.Is(err, ErrQuotaCurrency) {
		t.Fatal("different reservation currency admitted", err)
	}
	policy.MoneyMonth = nil
	policy.MoneyMonthBehavior = ""
	policy.Tokens5H = limitPtr(10)
	view.Active.TokensUnknown = 0
	view.Active.MoneyUnknown = 1
	if err := requirePolicyRules(t, policy, tokenBound(1), quotaRulesFrame()).Check(view); err != nil {
		t.Fatal("token policy depended on unknown money", err)
	}
}

func TestQuotaPolicyRulesCapturedInputsAndReadOnlyCounters(t *testing.T) {
	policy := quotaPolicy("user_rules")
	policy.TPM, policy.Tokens5H, policy.Tokens7D, policy.TokensMonth = limitPtr(10), limitPtr(10), limitPtr(10), limitPtr(10)
	policy.MoneyMonth, policy.Currency = moneyPtr("10"), "USD"
	policy.RPM, policy.Concurrency = limitPtr(2), limitPtr(2)
	bound := QuotaBound{Tokens: limitPtr(1), Money: moneyPtr("1"), Currency: "USD", Revision: "bound_captured"}
	rules := requirePolicyRules(t, policy, bound, quotaRulesFrame())
	for _, ceiling := range []*int64{policy.TPM, policy.Tokens5H, policy.Tokens7D, policy.TokensMonth, policy.RPM, policy.Concurrency} {
		*ceiling = 0
	}
	*policy.MoneyMonth, *bound.Money, *bound.Tokens = "0", "100", 100
	if *rules.policy.RPM != 2 || *rules.policy.Concurrency != 2 {
		t.Fatal("captured non-economic pointer fields changed")
	}
	if err := rules.Check(quotaRulesView()); err != nil {
		t.Fatal("caller mutation changed captured decision", err)
	}
	counter := emptyQuotaRulesCounter()
	counter.MoneyUsed["USD"] = "1000000000000000000"
	view := quotaRulesView()
	view.Active = counter
	view.Window = func(string) (QuotaCounter, error) { return counter, nil }
	before, err := json.Marshal(counter)
	if err != nil {
		t.Fatal(err)
	}
	if err := rules.Check(view); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(counter)
	if err != nil || string(before) != string(after) {
		t.Fatal("decision mutated reader maps", err)
	}
	if err := (QuotaPolicyRules{}).Check(view); !errors.Is(err, ErrInvalid) {
		t.Fatal("zero rules admitted", err)
	}
}

func TestQuotaPolicyRulesLocalRejectionAndMutationOrder(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	policy := quotaPolicy("user_order")
	policy.Tokens5H = limitPtr(10)
	frame := quotaRulesFrame()
	metadata := quotaMetadata{TimeZone: "UTC", CoverageStart: frame.CoverageStart}
	if err := q.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(quotaAccountBucket).Put([]byte(policy.Account), []byte("malformed")); err != nil {
			return err
		}
		return tx.Bucket(quotaCounterBucket).Put(counterKey(policy.Account, "active"), []byte("malformed"))
	}); err != nil {
		t.Fatal(err)
	}
	invalid := policy
	invalid.Revision = "invalid revision"
	if err := q.db.Update(func(tx *bolt.Tx) error {
		return checkQuota(tx, invalid, tokenBound(1), metadata, frame.Instant, frame.MonthStart, true, true)
	}); !errors.Is(err, ErrInvalid) {
		t.Fatal("policy validation did not precede birth", err)
	}
	if err := q.db.Update(func(tx *bolt.Tx) error {
		return checkQuota(tx, policy, tokenBound(1), metadata, frame.Instant, frame.MonthStart, true, true)
	}); !errors.Is(err, ErrQuotaConflict) {
		t.Fatal("birth conflict did not precede active read", err)
	}
	if err := q.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(quotaAccountBucket).Delete([]byte(policy.Account)) }); err != nil {
		t.Fatal(err)
	}
	sawBirthBeforeFailure := false
	err := q.db.Update(func(tx *bolt.Tx) error {
		err := checkQuota(tx, policy, tokenBound(1), metadata, frame.Instant, frame.MonthStart, true, true)
		var created int64
		raw := tx.Bucket(quotaAccountBucket).Get([]byte(policy.Account))
		sawBirthBeforeFailure = json.Unmarshal(raw, &created) == nil && created == policy.CreatedAt.UnixNano()
		return err
	})
	if !errors.Is(err, ErrInvalid) || !sawBirthBeforeFailure {
		t.Fatal("birth persistence no longer precedes active read", err)
	}
	if err := q.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(quotaAccountBucket).Get([]byte(policy.Account)) != nil {
			return errors.New("failed transaction retained birth")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaPolicyRulesLocalSnapshotParity(t *testing.T) {
	for _, test := range []struct {
		name         string
		used, active QuotaCounter
		invalid      bool
		want         error
	}{
		{"exact", QuotaCounter{TokensUsed: 4, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, QuotaCounter{TokensHeld: 5, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, false, nil},
		{"over", QuotaCounter{TokensUsed: 5, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, QuotaCounter{TokensHeld: 5, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, false, ErrQuotaTokens},
		{"unknown", QuotaCounter{TokensUnknown: 1, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}, emptyQuotaRulesCounter(), false, ErrQuotaUnknown},
		{"invalidated", emptyQuotaRulesCounter(), emptyQuotaRulesCounter(), true, ErrQuotaBound},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			policy := quotaPolicy("user_parity")
			policy.Tokens5H = limitPtr(10)
			bound, frame := tokenBound(1), quotaRulesFrame()
			view := QuotaPolicyView{Active: test.active, Window: func(string) (QuotaCounter, error) { return test.used, nil }, InvalidBound: func(string) (bool, error) { return test.invalid, nil }}
			pure := requirePolicyRules(t, policy, bound, frame).Check(view)
			if err := q.db.Update(func(tx *bolt.Tx) error {
				for name, counter := range map[string]QuotaCounter{"active": test.active, "five_hours": test.used} {
					raw, err := json.Marshal(counter)
					if err != nil {
						return err
					}
					if err := tx.Bucket(quotaCounterBucket).Put(counterKey(policy.Account, name), raw); err != nil {
						return err
					}
				}
				if test.invalid {
					return tx.Bucket(quotaInvalidBoundBucket).Put([]byte(bound.Revision), []byte("1"))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			local := q.db.Update(func(tx *bolt.Tx) error {
				return checkQuota(tx, policy, bound, quotaMetadata{CoverageStart: frame.CoverageStart}, frame.Instant, frame.MonthStart, true, true)
			})
			if !errors.Is(pure, test.want) || !errors.Is(local, test.want) {
				t.Fatalf("pure=%v local=%v; want %v", pure, local, test.want)
			}
		})
	}
}

func TestQuotaPolicyRulesPolicyRepresentationBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*QuotaLimit)
	}{
		{"invalid_revision", func(p *QuotaLimit) { p.Revision = "invalid revision" }},
		{"future_birth", func(p *QuotaLimit) { p.CreatedAt = time.Unix(0, quotaRulesFrame().Instant).Add(time.Nanosecond) }},
		{"nonpositive_birth", func(p *QuotaLimit) { p.CreatedAt = time.Unix(0, 0) }},
		{"invalid_monthly_mode", func(p *QuotaLimit) { p.TokensMonthBehavior = "continue" }},
		{"mixed_key_proof", func(p *QuotaLimit) { p.PersonalKey, p.ProjectKey = true, true }},
		{"mixed_team_proof", func(p *QuotaLimit) { p.TeamMember, p.PersonalKey = true, true }},
		{"key_mode_without_trusted_kind", func(p *QuotaLimit) { p.Account = "key_root"; p.TokensMonthBehavior = "alert_only" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := quotaPolicy("user_rules")
			test.change(&policy)
			if _, err := NewQuotaPolicyRules(policy, tokenBound(1), quotaRulesFrame()); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid captured policy accepted", err)
			}
		})
	}
	policy := quotaPolicy("key_root")
	policy.PersonalKey = true
	policy.TokensMonth, policy.TokensMonthBehavior = limitPtr(0), "alert_only"
	if err := requirePolicyRules(t, policy, tokenBound(1), quotaRulesFrame()).Check(quotaRulesView()); err != nil {
		t.Fatal("trusted key monthly mode rejected", err)
	}
}
