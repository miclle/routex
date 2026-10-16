package service

import (
	"context"
	"errors"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
)

var (
	errSharedAdmissionInvalid   = errors.New("invalid shared admission intent")
	errSharedAdmissionConflict  = errors.New("shared admission intent conflicts")
	errSharedAdmissionUncertain = errors.New("shared admission operation is uncertain")
	errSharedDispatchForbidden  = errors.New("shared admission cannot authorize dispatch")
	errSharedExecutorBusy       = errors.New("shared admission executor is busy")
)

// These interfaces are an unwired coordinator contract, not a shared backend.
// SQL adapters validate current installation/mode and owner fences before new
// admissions or dispatches. Historical checkpoint/settlement retains its exact
// accepted identity independently of current authority. Economic changes are
// atomic and never fall back to the local Queue.
type sharedAdmissionOwner struct {
	InstallationID string
	ModeRevision   uint64
	SpoolID        string
	IncarnationID  string
	Fence          uint64
}

type sharedAdmissionRequest struct {
	RequestID        string
	Reservation      *GatewayReservationManifest
	OwnerStartedAt   time.Time
	Authorization    sharedCapturedAuthorization
	CalendarRevision string
	TimeZone         string
	Policies         []eventqueue.QuotaLimit
	Bound            eventqueue.QuotaBound
	Recovery         eventqueue.QuotaSettlement
	Fallback         []byte
}

type sharedAdmissionIntent struct {
	sharedAdmissionRequest
	Owner       sharedAdmissionOwner
	ExecutionID string
	Digest      string
}

type sharedAdmissionKey struct {
	Owner           sharedAdmissionOwner
	RequestID       string
	ExecutionID     string
	AdmissionDigest string
}

type sharedAttemptIntent struct {
	Key             sharedAdmissionKey
	Number          int
	AttemptID       string
	CredentialID    string
	SnapshotID      string
	ConnectionID    string
	ProviderModelID string
	Fallback        []byte
	Digest          string
}

type sharedAttemptCheckpoint struct {
	Key           sharedAdmissionKey
	Number        int
	AttemptDigest string
	Fallback      []byte
	Recovery      eventqueue.QuotaSettlement
	Digest        string
}

type sharedUpstreamClosure string

const (
	sharedUpstreamUnknown  sharedUpstreamClosure = "unknown"
	sharedUpstreamNoWork   sharedUpstreamClosure = "no_work"
	sharedUpstreamTerminal sharedUpstreamClosure = "native_terminal"
)

// ExecutorClosed proves that this logical RouteX executor/transport has closed
// and may release its exact gateway concurrency independently of unknown
// economic holds. A heartbeat or owner fence alone does not prove closure.
// Upstream evidence remains separate; local closure cannot prove known usage.
type sharedTerminalIntent struct {
	Key            sharedAdmissionKey
	LastAttempt    int
	Fact           []byte
	Actual         eventqueue.QuotaSettlement
	ExecutorClosed bool
	Upstream       sharedUpstreamClosure
	Digest         string
}

type sharedDispatchReceipt struct {
	Key           sharedAdmissionKey
	Number        int
	AttemptDigest string
}

type sharedAuthorityReceipt struct {
	Key         sharedAdmissionKey
	ReceiptID   string
	Dispatches  []sharedDispatchReceipt
	Checkpoints []sharedAttemptCheckpoint
	Terminal    *sharedTerminalIntent
}

type sharedSpoolRecord struct {
	Intent      sharedAdmissionIntent
	ReceiptID   string
	Attempts    []sharedAttemptIntent
	Dispatches  []sharedDispatchReceipt
	Checkpoints []sharedAttemptCheckpoint
	Terminal    *sharedTerminalIntent
	Ready       bool
}

// Read returns authoritative exact identity/digest evidence or an explicit
// missing error. A timeout or incomplete read cannot stand in for absence.
// Accepted and terminal receipts remain available after fact delivery. Settle
// retains independent unknown dimensions and releases gateway concurrency only
// with exact logical executor closure; lease expiry cannot establish that proof.
type sharedEconomicAuthority interface {
	Accept(context.Context, sharedAdmissionIntent) (*sharedAuthorityReceipt, error)
	Read(context.Context, sharedAdmissionKey) (*sharedAuthorityReceipt, error)
	MarkDispatch(context.Context, sharedAttemptIntent) (*sharedDispatchReceipt, error)
	Checkpoint(context.Context, sharedAttemptCheckpoint) error
	Settle(context.Context, sharedTerminalIntent) (*sharedAuthorityReceipt, error)
}

// Every method is an idempotent conditional durable transition for the original
// body/digest. Read returns independent data. Reserve is spool-only; it creates
// no local quota/rate/concurrency state. Terminal preparation cannot be delivered
// until MakeReady has captured the matching authoritative terminal receipt.
// This interface cannot be implemented by the current Queue.Open recovery path.
type sharedCallSpool interface {
	Reserve(context.Context, sharedAdmissionIntent) error
	Read(context.Context, sharedAdmissionKey) (*sharedSpoolRecord, error)
	ConfirmAcceptance(context.Context, sharedAdmissionKey, string) error
	PrepareAttempt(context.Context, sharedAttemptIntent) error
	ConfirmDispatch(context.Context, sharedDispatchReceipt) error
	RecordAttempt(context.Context, sharedAttemptCheckpoint) error
	PrepareTerminal(context.Context, sharedTerminalIntent) error
	MakeReady(context.Context, sharedTerminalIntent, string) error
}

type sharedCoordinatorOptions struct {
	OperationTimeout time.Duration
}
