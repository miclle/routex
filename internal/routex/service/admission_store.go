package service

import (
	"context"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
)

// admissionStore owns synchronous enforcement and its original receipts. Call
// reports are asynchronous projections, never the authority for this interface.
// Implementations must preserve conjunctive accounts, exact decimal money,
// independent settlement dimensions, coverage, and conservative unknown holds.
// Recorder file ownership and delivery Read/Ack/Close remain separate. Reserve,
// UpdatePending and Complete still commit their supplied call payload atomically
// with local economic state; a future backend must explicitly coordinate the
// durable spool/authority barrier, not silently discard that payload contract.
// Context-free service compatibility entry points currently supply Background;
// this is not bounded distributed I/O. A shared implementation must establish
// finite caller/operation contexts at every dispatch and settlement seam.
//
// TODO: add the shared GORM authority, verified local-history transfer, durable
// dispatch fencing and real two-process acceptance before enabling multi-node
// admission. The current implementation is exclusively local; this boundary does
// not provide a shared backend or authorize fallback to another ledger.
type admissionStore interface {
	BindInstallation(context.Context, string, bool) error
	EnableQuota(context.Context, string, time.Time) error
	QuotaStatus(context.Context) (*eventqueue.QuotaStatus, error)
	ReserveWithLimits(context.Context, string, []byte, []eventqueue.Limit, time.Time) error
	ReserveWithQuotaRecovery(context.Context, string, []byte, []eventqueue.QuotaLimit, eventqueue.QuotaBound, eventqueue.QuotaSettlement, time.Time) error
	PreflightWithQuota(context.Context, string, []eventqueue.QuotaLimit, eventqueue.QuotaBound, string, time.Time) error
	UpdatePending(context.Context, string, []byte) error
	UpdatePendingQuota(context.Context, string, []byte, eventqueue.QuotaSettlement) error
	Complete(context.Context, string, []byte) error
	CompleteQuota(context.Context, string, []byte, eventqueue.QuotaSettlement, time.Time) (*eventqueue.QuotaReceipt, error)
	QuotaReceipt(context.Context, string) (*eventqueue.QuotaReceipt, error)
	AccountUsage(context.Context, string, time.Time) (int64, int64, error)
	AccountQuotaUsage(context.Context, string, time.Time) (*eventqueue.AccountQuotaUsage, error)
	AccountQuotaUsageBatch(context.Context, []string, time.Time) (*eventqueue.QuotaUsageBatch, error)
	AccountQuotaUsageProofBatch(context.Context, []string, time.Time) (*eventqueue.QuotaUsageProofBatch, error)
	HasLiveMoneyHolds(context.Context, time.Time) (bool, error)
}
