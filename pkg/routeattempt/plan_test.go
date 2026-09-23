package routeattempt

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func targets() []Target {
	return []Target{
		{ID: "target_a", ConnectionID: "connection_a", Protocol: "openai_chat", Weight: 70, Credentials: []Credential{{ID: "a_backup", Priority: 2}, {ID: "a_primary", Priority: 1}}},
		{ID: "target_b", ConnectionID: "connection_b", Protocol: "openai_chat", Weight: 30, Credentials: []Credential{{ID: "b_primary", Priority: 1}}},
	}
}
func plan(t *testing.T, values []Target, limit int, draw func(int) (int, error)) *Plan {
	t.Helper()
	p, err := New("openai_chat", values, Options{MaxAttempts: limit, Draw: draw})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func zero(int) (int, error) { return 0, nil }
func hooks(execute func(context.Context, Attempt) (Outcome, error)) Hooks {
	return Hooks{Eligible: func(context.Context, Attempt) (bool, error) { return true, nil }, Prepare: func(context.Context, Attempt) error { return nil }, Admit: func(context.Context, Attempt) error { return nil }, Execute: execute}
}
func succeeded(context.Context, Attempt) (Outcome, error) {
	return Outcome{Failure: Success, Work: Completed}, nil
}

func TestWeightedSelectionAndPriority(t *testing.T) {
	counts := map[string]int{}
	for draw := 0; draw < 100; draw++ {
		p := plan(t, targets(), 3, func(total int) (int, error) {
			if total != 100 {
				t.Fatal("credential count changed route weights")
			}
			return draw, nil
		})
		result, err := p.Run(context.Background(), hooks(succeeded))
		if err != nil || result.Stop != Succeeded {
			t.Fatal("selection failed")
		}
		attempt := result.Attempts[0].Attempt
		counts[attempt.TargetID]++
		if attempt.TargetID == "target_a" && attempt.CredentialID != "a_primary" {
			t.Fatal("credential priority ignored")
		}
	}
	if counts["target_a"] != 70 || counts["target_b"] != 30 {
		t.Fatal("incorrect relative weights")
	}
	values := targets()
	values[0].Weight = 0
	result, err := plan(t, values, 2, func(total int) (int, error) {
		if total != 30 {
			t.Fatal("zero weight was included")
		}
		return 0, nil
	}).Run(context.Background(), hooks(succeeded))
	if err != nil || result.Attempts[0].Attempt.TargetID != "target_b" {
		t.Fatal("candidate received live traffic")
	}
}

func TestCredentialRetryAndConnectionFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first Outcome
		want  []string
	}{
		{"credential", Outcome{Failure: CredentialRejected, Work: RejectedWithoutWork}, []string{"a_primary", "a_backup"}},
		{"connection", Outcome{Failure: ConnectionFailure, Work: NotSent}, []string{"a_primary", "b_primary"}},
		{"throttle", Outcome{Failure: RateLimited, Work: RejectedWithoutWork}, []string{"a_primary", "b_primary"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admissions := 0
			prepares := 0
			seen := []string{}
			h := hooks(func(_ context.Context, a Attempt) (Outcome, error) {
				seen = append(seen, a.CredentialID)
				if a.Number == 1 {
					return tc.first, nil
				}
				return succeeded(context.Background(), a)
			})
			h.Admit = func(context.Context, Attempt) error { admissions++; return nil }
			h.Prepare = func(context.Context, Attempt) error { prepares++; return nil }
			result, err := plan(t, targets(), 3, zero).Run(context.Background(), h)
			if err != nil || result.Stop != Succeeded || !reflect.DeepEqual(seen, tc.want) || admissions != 1 || prepares != 2 {
				t.Fatal("retry changed reservation or credential scope")
			}
		})
	}
}

func TestUnsafeEvidenceNeverReplays(t *testing.T) {
	for _, outcome := range []Outcome{
		{Failure: CredentialRejected, Work: RejectedWithoutWork, OutputStarted: true},
		{Failure: CredentialRejected, Work: RejectedWithoutWork, FinalUsageKnown: true},
		{Failure: ConnectionFailure, Work: Completed},
		{Failure: ConnectionFailure, Work: Unknown},
		{Failure: RateLimited, Work: Unknown},
		{Failure: CredentialRejected, Work: NotSent},
	} {
		result, err := plan(t, targets(), 4, zero).Run(context.Background(), hooks(func(context.Context, Attempt) (Outcome, error) { return outcome, nil }))
		if err != nil || result.Stop != UnsafeToReplay || len(result.Attempts) != 1 {
			t.Fatal("unsafe outcome replayed")
		}
	}
	result, err := plan(t, targets(), 4, zero).Run(context.Background(), hooks(func(context.Context, Attempt) (Outcome, error) {
		return Outcome{Failure: ConnectionFailure, Work: NotSent}, errors.New("untrusted transport error")
	}))
	if err != ErrExecution || len(result.Attempts) != 1 {
		t.Fatal("raw execution error enabled retry")
	}
}

func TestBoundsAndNoCandidates(t *testing.T) {
	p := plan(t, targets(), 1, zero)
	result, err := p.Run(context.Background(), hooks(func(context.Context, Attempt) (Outcome, error) {
		return Outcome{Failure: CredentialRejected, Work: RejectedWithoutWork}, nil
	}))
	if err != nil || result.Stop != Exhausted || len(result.Attempts) != 1 {
		t.Fatal("attempt cap not explicit")
	}
	result, err = plan(t, nil, 4, zero).Run(context.Background(), hooks(succeeded))
	if err != nil || result.Stop != NoCandidates || result.Admitted || len(result.Attempts) != 0 {
		t.Fatal("empty plan fabricated admission")
	}
	h := hooks(succeeded)
	h.Eligible = func(context.Context, Attempt) (bool, error) { return false, nil }
	result, err = plan(t, targets(), 4, zero).Run(context.Background(), h)
	if err != nil || result.Stop != NoCandidates || result.Admitted {
		t.Fatal("revoked candidates admitted")
	}
}

func TestAuthorizationAdmissionAndCancellationBoundaries(t *testing.T) {
	for _, stage := range []string{"eligible", "prepare", "admit", "after_admit", "after_execute"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			h := hooks(func(context.Context, Attempt) (Outcome, error) {
				calls++
				if stage == "after_execute" {
					cancel()
				}
				return Outcome{Failure: CredentialRejected, Work: RejectedWithoutWork}, nil
			})
			switch stage {
			case "eligible":
				h.Eligible = func(context.Context, Attempt) (bool, error) { return false, errors.New("private") }
			case "prepare":
				h.Prepare = func(context.Context, Attempt) error { return errors.New("revoked") }
			case "admit":
				h.Admit = func(context.Context, Attempt) error { return errors.New("full") }
			case "after_admit":
				h.Admit = func(context.Context, Attempt) error { cancel(); return nil }
			}
			result, err := plan(t, targets(), 4, zero).Run(ctx, h)
			if err == nil {
				t.Fatal("failure hidden")
			}
			expected := 0
			if stage == "after_execute" {
				expected = 1
			}
			if len(result.Attempts) != expected || calls != expected {
				t.Fatal("preparation fabricated attempts or canceled call replayed")
			}
			if stage == "after_admit" && !result.Admitted {
				t.Fatal("durable admission lost on cancellation")
			}
		})
	}
	// Authorization is rechecked before the second dispatch; revocation is not
	// treated as a reason to route around the current policy using a third target.
	h := hooks(func(context.Context, Attempt) (Outcome, error) {
		return Outcome{Failure: CredentialRejected, Work: RejectedWithoutWork}, nil
	})
	h.Prepare = func(_ context.Context, a Attempt) error {
		if a.Number == 2 {
			return errors.New("revoked")
		}
		return nil
	}
	result, err := plan(t, targets(), 4, zero).Run(context.Background(), h)
	if err != ErrPrepare || len(result.Attempts) != 1 || !result.Admitted {
		t.Fatal("revocation after first attempt bypassed")
	}
}

func TestPlanCopiesInputAndSupportsIndependentConcurrentRuns(t *testing.T) {
	values := targets()
	p := plan(t, values, 2, zero)
	values[0].ID = "mutated"
	values[0].Credentials[1].ID = "mutated"
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			result, err := p.Run(context.Background(), hooks(succeeded))
			if err != nil || result.Attempts[0].Attempt.TargetID != "target_a" || result.Attempts[0].Attempt.CredentialID != "a_primary" {
				t.Error("plan mutated or progress shared")
				return
			}
			result.Attempts[0].Attempt.TargetID = "caller mutation"
			successes.Add(1)
		})
	}
	wg.Wait()
	if successes.Load() != 20 {
		t.Fatal("request progress was shared")
	}
}

func TestInvalidPlansAndRandomnessFailClosed(t *testing.T) {
	mutations := []func([]Target){
		func(v []Target) { v[1].ID = v[0].ID },
		func(v []Target) { v[1].Protocol = "anthropic_messages" },
		func(v []Target) { v[0].Credentials = append(v[0].Credentials, v[0].Credentials[0]) },
		func(v []Target) { v[1].Credentials[0].ID = v[0].Credentials[0].ID },
		func(v []Target) { v[0].ID = "id\x00collision" },
		func(v []Target) { v[0].Weight = -1 },
	}
	for _, mutate := range mutations {
		values := targets()
		mutate(values)
		if _, err := New("openai_chat", values, Options{MaxAttempts: 3}); err != ErrConfig {
			t.Fatal("invalid plan accepted")
		}
	}
	for _, limit := range []int{0, MaxAttempts + 1} {
		if _, err := New("openai_chat", targets(), Options{MaxAttempts: limit}); err != ErrConfig {
			t.Fatal("invalid attempt cap accepted")
		}
	}
	for _, draw := range []func(int) (int, error){func(int) (int, error) { return -1, nil }, func(n int) (int, error) { return n, nil }, func(int) (int, error) { return 0, errors.New("private") }} {
		result, err := plan(t, targets(), 3, draw).Run(context.Background(), hooks(succeeded))
		if err != ErrRandom || len(result.Attempts) != 0 {
			t.Fatal("invalid randomness admitted request")
		}
	}
}

func TestExclusionsAndEligibilityRenormalization(t *testing.T) {
	h := hooks(func(_ context.Context, a Attempt) (Outcome, error) {
		if a.TargetID == "target_a" {
			return Outcome{Failure: CredentialRejected, Work: RejectedWithoutWork}, nil
		}
		return Outcome{Failure: ConnectionFailure, Work: NotSent}, nil
	})
	result, err := plan(t, targets(), 4, zero).Run(context.Background(), h)
	if err != nil || result.Stop != NoCandidates || len(result.Attempts) != 3 {
		t.Fatal("exhausted credentials or connections were revisited")
	}
	if result.Attempts[0].Attempt.CredentialID == result.Attempts[1].Attempt.CredentialID {
		t.Fatal("rejected credential was reused")
	}
	h = hooks(succeeded)
	h.Eligible = func(_ context.Context, a Attempt) (bool, error) { return a.ConnectionID != "connection_a", nil }
	result, err = plan(t, targets(), 4, func(total int) (int, error) {
		if total != 30 {
			t.Fatal("unavailable route weight retained")
		}
		return 0, nil
	}).Run(context.Background(), h)
	if err != nil || result.Attempts[0].Attempt.TargetID != "target_b" {
		t.Fatal("unhealthy route was selected")
	}
}

func TestInvalidOutcomeDoesNotExposeRawProviderData(t *testing.T) {
	result, err := plan(t, targets(), 4, zero).Run(context.Background(), hooks(func(context.Context, Attempt) (Outcome, error) {
		return Outcome{Failure: "provider-secret", Work: "untrusted", FinalUsageKnown: true}, nil
	}))
	if err != ErrExecution || len(result.Attempts) != 1 || result.Attempts[0].Outcome.Failure != PermanentFailure || !result.Attempts[0].Outcome.FinalUsageKnown {
		t.Fatal("invalid outcome retained provider data or lost finality")
	}
}
