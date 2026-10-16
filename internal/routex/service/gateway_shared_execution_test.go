package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type sharedGatewayTestBody struct {
	io.Reader
	closed int
	err    error
}

func (b *sharedGatewayTestBody) Close() error { b.closed++; return b.err }

type sharedGatewayClosureAuthority struct {
	*sharedFaultAuthority
	body *sharedGatewayTestBody
	t    *testing.T
}

func (a *sharedGatewayClosureAuthority) Settle(ctx context.Context, terminal sharedTerminalIntent) (*sharedAuthorityReceipt, error) {
	a.t.Helper()
	if a.body.closed != 1 {
		a.t.Fatal("SQL settlement preceded response body closure")
	}
	return a.sharedFaultAuthority.Settle(ctx, terminal)
}

func sharedGatewayTerminalFixture(t *testing.T) (*Service, *GatewayResult, CallFact, *sharedFaultSpool, *sharedFaultAuthority, *sharedGatewayTestBody) {
	t.Helper()
	c, spool, authority, _ := sharedTestFixture(t)
	if err := c.Admit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(t.Context(), sharedTestDispatch(t, 1), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	body := &sharedGatewayTestBody{Reader: strings.NewReader("native stream")}
	c.authority = &sharedGatewayClosureAuthority{sharedFaultAuthority: authority, body: body, t: t}
	s := &Service{}
	r := &sharedGatewayRuntime{service: s, timeout: time.Second}
	s.sharedGateway = r
	result := &GatewayResult{Admitted: true}
	e := &sharedGatewayExecution{runtime: r, result: result, coordinator: c, admissionAttempted: true, bodies: []*sharedGatewayBody{{ReadCloser: body}}}
	result.sharedExecution = e
	fact, err := sharedCallPayload("req_shared", sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, false))
	if err != nil {
		t.Fatal(err)
	}
	return s, result, fact, spool, authority, body
}

func TestSharedGatewayFinalizationClosesExactExecutorBeforeSQLAndKeepsUnknownHolds(t *testing.T) {
	s, result, fact, spool, authority, body := sharedGatewayTerminalFixture(t)
	heldTokens, heldMoney := authority.tokensHeld, authority.moneyHeld
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.FinalizeGatewayCall(ctx, result, fact); err != nil {
		t.Fatal(err)
	}
	if body.closed != 1 || !spool.record.Ready || authority.concurrency != 0 || authority.tokensHeld != heldTokens || authority.moneyHeld != heldMoney || authority.settleCount != 1 {
		t.Fatal("closure lost unknown economics or did not publish exact fact", authority)
	}
	if authority.settleWasCanceled || !authority.settleHadDeadline {
		t.Fatal("terminal did not use finite detached operation")
	}
	if err := s.FinalizeGatewayCall(ctx, result, fact); err != nil {
		t.Fatal("identical terminal retry", err)
	}
	if body.closed != 1 || authority.settleCount != 1 {
		t.Fatal("retry repeated closure or economics")
	}
}

func TestSharedGatewayClosureFailureCannotReleaseExecutorOrPublishFact(t *testing.T) {
	s, result, fact, spool, authority, body := sharedGatewayTerminalFixture(t)
	body.err = errors.New("transport closure is uncertain")
	if err := s.FinalizeGatewayCall(t.Context(), result, fact); err == nil {
		t.Fatal("uncertain closure accepted")
	}
	if authority.settleCount != 0 || authority.concurrency != 1 || spool.record.Ready || spool.record.Terminal != nil {
		t.Fatal("uncertain closure discarded live executor")
	}
	if err := s.FinalizeGatewayCall(t.Context(), result, fact); err == nil || body.closed != 1 {
		t.Fatal("closure retry invented success or reran physical close")
	}
}

func TestSharedGatewayFinalizationRejectsBorrowedResultAndChangedIntent(t *testing.T) {
	s, result, fact, spool, authority, _ := sharedGatewayTerminalFixture(t)
	borrowed := *result
	if err := s.FinalizeGatewayCall(t.Context(), &borrowed, fact); err == nil {
		t.Fatal("copied result borrowed executor")
	}
	changed := fact
	changed.RequestID = "req_other"
	if err := s.FinalizeGatewayCall(t.Context(), result, changed); err == nil {
		t.Fatal("changed logical identity accepted")
	}
	if authority.settleCount != 0 || spool.record.Terminal != nil {
		t.Fatal("rejected completion changed history")
	}
	if err := s.FinalizeGatewayCall(t.Context(), result, fact); err != nil {
		t.Fatal("rejected completion poisoned original intent", err)
	}
}

func TestSharedGatewayTerminalRetryKeepsOriginalAfterLostSQLReply(t *testing.T) {
	s, result, fact, spool, authority, _ := sharedGatewayTerminalFixture(t)
	authority.fault = &sharedCommitFault{operation: "settle", after: true}
	if err := s.FinalizeGatewayCall(t.Context(), result, fact); !errors.Is(err, errSharedInjected) {
		t.Fatal("lost SQL response", err)
	}
	if spool.record.Ready || authority.settleCount != 1 || authority.concurrency != 0 {
		t.Fatal("lost reply published fact or forgot economic commit")
	}
	changed := fact
	changed.ErrorCode = "upstream_error"
	if err := s.FinalizeGatewayCall(t.Context(), result, changed); err == nil {
		t.Fatal("uncertain terminal body replaced")
	}
	if err := s.FinalizeGatewayCall(t.Context(), result, fact); err != nil {
		t.Fatal("original terminal retry", err)
	}
	if !spool.record.Ready || authority.settleCount != 1 {
		t.Fatal("original retry repeated economics or failed readiness")
	}
}

func TestSharedGatewayMissingRuntimeDependenciesNeverUseLocalAdmission(t *testing.T) {
	s := &Service{}
	s.sharedGateway = &sharedGatewayRuntime{service: s}
	result := &GatewayResult{}
	if err := s.AdmitGatewayCall("req_guard", result); err == nil {
		t.Fatal("generic admission silently bypassed shared dependency")
	}
	if err := s.CheckpointGatewayCall("req_guard", result); err == nil {
		t.Fatal("generic checkpoint bypassed shared dependency")
	}
	if err := s.PersistGatewayCall(t.Context(), CallFact{}); err == nil {
		t.Fatal("request-ID-only finalization bypassed exact executor")
	}
	if err := s.StartCallRecorder(t.Context(), "unused.db"); err == nil {
		t.Fatal("shared and local recorder selected together")
	}
	if s.sharedGateway.current(s) {
		t.Fatal("unconstructed runtime admitted requests")
	}
}

func TestSharedGatewayFinalizationStillSettlesAfterAdmissionStops(t *testing.T) {
	s, result, fact, spool, authority, _ := sharedGatewayTerminalFixture(t)
	s.sharedGateway.stop()
	if err := s.FinalizeGatewayCall(t.Context(), result, fact); err != nil {
		t.Fatal("shutdown discarded original settlement", err)
	}
	if !spool.record.Ready || authority.concurrency != 0 || authority.settleCount != 1 {
		t.Fatal("shutdown lost executor closure")
	}
}
