package routeattempt

import "context"

type Failure string

const (
	Success            Failure = "success"
	CredentialRejected Failure = "credential_rejected"
	ConnectionFailure  Failure = "connection_failure"
	RateLimited        Failure = "rate_limited"
	PermanentFailure   Failure = "permanent_failure"
)

// WorkEvidence must come from protocol/transport facts, never a guess based on
// HTTP status, timeout, missing usage, or absence of output alone.
type WorkEvidence string

const (
	Unknown             WorkEvidence = "unknown"
	NotSent             WorkEvidence = "not_sent"
	RejectedWithoutWork WorkEvidence = "rejected_without_work"
	Completed           WorkEvidence = "completed"
)

type Outcome struct {
	Failure                        Failure
	Work                           WorkEvidence
	OutputStarted, FinalUsageKnown bool
}
type StopReason string

const (
	Succeeded      StopReason = "succeeded"
	UnsafeToReplay StopReason = "unsafe_to_replay"
	Permanent      StopReason = "permanent_failure"
	Exhausted      StopReason = "attempt_budget_exhausted"
	NoCandidates   StopReason = "no_candidates"
	Canceled       StopReason = "canceled"
	Blocked        StopReason = "blocked"
)

type AttemptResult struct {
	Attempt Attempt
	Outcome Outcome
}
type Result struct {
	Admitted bool
	Attempts []AttemptResult
	Stop     StopReason
}

type Hooks struct {
	// Eligible reads current health/authorization for candidate selection. Errors
	// stop the request; false excludes this candidate credential for this selection.
	Eligible Eligibility
	// Prepare is the authoritative current-authorization and budget boundary for
	// EVERY attempt. It must recheck revocation, route/egress revisions, and price
	// reservation bounds atomically with dispatch permission. Success is not an
	// upstream attempt and must not increment attempt metering.
	Prepare func(context.Context, Attempt) error
	// Admit reserves one durable request record; it runs once, after first Prepare.
	// Retries must update the same reservation inside Prepare, never admit anew.
	Admit func(context.Context, Attempt) error
	// Execute invokes at most one upstream request and returns its safety facts.
	// The caller owns native payloads, response cleanup, attempt IDs, and final
	// settlement. A returned error is conservatively non-retryable.
	Execute func(context.Context, Attempt) (Outcome, error)
}

// Run makes no implicit sleeps or retries. Every fallback is a different
// target/credential pair. Context cancellation and failed authorization stop it.
// The caller settles Result once, including admitted calls with zero attempts.
func (p *Plan) Run(ctx context.Context, h Hooks) (Result, error) {
	result := Result{Attempts: []AttemptResult{}}
	if p == nil || ctx == nil || h.Eligible == nil || h.Prepare == nil || h.Admit == nil || h.Execute == nil {
		return result, ErrConfig
	}
	state := progress{attempted: map[string]bool{}, credentials: map[string]bool{}, connections: map[string]bool{}}
	for {
		if err := ctx.Err(); err != nil {
			result.Stop = Canceled
			return result, err
		}
		attempt, ok, err := p.selectAttempt(ctx, &state, len(result.Attempts)+1, h.Eligible)
		if err != nil {
			result.Stop = Blocked
			if ctx.Err() != nil {
				result.Stop = Canceled
			}
			return result, err
		}
		if !ok {
			result.Stop = NoCandidates
			return result, nil
		}
		if err := h.Prepare(ctx, attempt); err != nil {
			return blockedResult(ctx, result, ErrPrepare)
		}
		if err := ctx.Err(); err != nil {
			result.Stop = Canceled
			return result, err
		}
		if !result.Admitted {
			if err := h.Admit(ctx, attempt); err != nil {
				return blockedResult(ctx, result, ErrAdmission)
			}
			result.Admitted = true
		}
		if err := ctx.Err(); err != nil {
			result.Stop = Canceled
			return result, err
		}
		// Only entering Execute consumes an attempt slot. Preparation/admission
		// failures leave no fabricated attempt in the ordered execution history.
		state.attempted[attemptKey(attempt)] = true
		outcome, executionErr := h.Execute(ctx, attempt)
		if !validOutcome(outcome) {
			outcome = Outcome{Failure: PermanentFailure, Work: Unknown, OutputStarted: outcome.OutputStarted, FinalUsageKnown: outcome.FinalUsageKnown}
			executionErr = ErrExecution
		}
		result.Attempts = append(result.Attempts, AttemptResult{Attempt: attempt, Outcome: outcome})
		if ctx.Err() != nil {
			result.Stop = Canceled
			return result, ctx.Err()
		}
		if executionErr != nil {
			result.Stop = UnsafeToReplay
			return result, ErrExecution
		}
		if outcome.Failure == Success {
			result.Stop = Succeeded
			return result, nil
		}
		if outcome.OutputStarted || outcome.FinalUsageKnown || (outcome.Work != NotSent && outcome.Work != RejectedWithoutWork) {
			result.Stop = UnsafeToReplay
			return result, nil
		}
		state.preferred = ""
		switch outcome.Failure {
		case CredentialRejected:
			if outcome.Work != RejectedWithoutWork {
				result.Stop = UnsafeToReplay
				return result, nil
			}
			state.credentials[attempt.CredentialID] = true
			state.preferred = attempt.TargetID
		case ConnectionFailure, RateLimited:
			state.connections[attempt.ConnectionID] = true
		default:
			result.Stop = Permanent
			return result, nil
		}
		if len(result.Attempts) >= p.maxAttempts {
			result.Stop = Exhausted
			return result, nil
		}
	}
}
func blockedResult(ctx context.Context, result Result, err error) (Result, error) {
	result.Stop = Blocked
	if ctx.Err() != nil {
		result.Stop = Canceled
		return result, ctx.Err()
	}
	return result, err
}

func validOutcome(outcome Outcome) bool {
	switch outcome.Failure {
	case Success, CredentialRejected, ConnectionFailure, RateLimited, PermanentFailure:
	default:
		return false
	}
	switch outcome.Work {
	case Unknown, NotSent, RejectedWithoutWork, Completed:
		return true
	default:
		return false
	}
}
