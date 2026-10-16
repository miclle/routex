package service

import (
	"context"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
)

// localAdmissionStore delegates each operation to the original journal transaction.
// Context is carried by the authority boundary; the existing bbolt operations
// remain synchronous and cannot be canceled mid-commit. The recorder owns Close.
type localAdmissionStore struct {
	journal *eventqueue.Queue
}

var _ admissionStore = (*localAdmissionStore)(nil)

func newLocalAdmissionStore(journal *eventqueue.Queue) admissionStore {
	if journal == nil {
		return nil
	}
	return &localAdmissionStore{journal: journal}
}

func (s *localAdmissionStore) BindInstallation(_ context.Context, identity string, established bool) error {
	return s.journal.BindInstallation(identity, established)
}

func (s *localAdmissionStore) EnableQuota(_ context.Context, zone string, now time.Time) error {
	return s.journal.EnableQuota(zone, now)
}

func (s *localAdmissionStore) QuotaStatus(_ context.Context) (*eventqueue.QuotaStatus, error) {
	return s.journal.QuotaStatus()
}

func (s *localAdmissionStore) ReserveWithLimits(_ context.Context, requestID string, fallback []byte, policies []eventqueue.Limit, now time.Time) error {
	return s.journal.ReserveWithLimits(requestID, fallback, policies, now)
}

func (s *localAdmissionStore) ReserveWithQuotaRecovery(_ context.Context, requestID string, fallback []byte, policies []eventqueue.QuotaLimit, bound eventqueue.QuotaBound, recovery eventqueue.QuotaSettlement, now time.Time) error {
	return s.journal.ReserveWithQuotaRecovery(requestID, fallback, policies, bound, recovery, now)
}

func (s *localAdmissionStore) PreflightWithQuota(_ context.Context, requestID string, policies []eventqueue.QuotaLimit, bound eventqueue.QuotaBound, zone string, now time.Time) error {
	return s.journal.PreflightWithQuota(requestID, policies, bound, zone, now)
}

func (s *localAdmissionStore) UpdatePending(_ context.Context, requestID string, fallback []byte) error {
	return s.journal.UpdatePending(requestID, fallback)
}

func (s *localAdmissionStore) UpdatePendingQuota(_ context.Context, requestID string, fallback []byte, recovery eventqueue.QuotaSettlement) error {
	return s.journal.UpdatePendingQuota(requestID, fallback, recovery)
}

func (s *localAdmissionStore) Complete(_ context.Context, requestID string, payload []byte) error {
	return s.journal.Complete(requestID, payload)
}

func (s *localAdmissionStore) CompleteQuota(_ context.Context, requestID string, payload []byte, actual eventqueue.QuotaSettlement, now time.Time) (*eventqueue.QuotaReceipt, error) {
	return s.journal.CompleteQuota(requestID, payload, actual, now)
}

func (s *localAdmissionStore) QuotaReceipt(_ context.Context, requestID string) (*eventqueue.QuotaReceipt, error) {
	return s.journal.QuotaReceipt(requestID)
}

func (s *localAdmissionStore) AccountUsage(_ context.Context, account string, now time.Time) (int64, int64, error) {
	return s.journal.AccountUsage(account, now)
}

func (s *localAdmissionStore) AccountQuotaUsage(_ context.Context, account string, now time.Time) (*eventqueue.AccountQuotaUsage, error) {
	return s.journal.AccountQuotaUsage(account, now)
}

func (s *localAdmissionStore) AccountQuotaUsageBatch(_ context.Context, accounts []string, now time.Time) (*eventqueue.QuotaUsageBatch, error) {
	return s.journal.AccountQuotaUsageBatch(accounts, now)
}

func (s *localAdmissionStore) AccountQuotaUsageProofBatch(_ context.Context, accounts []string, now time.Time) (*eventqueue.QuotaUsageProofBatch, error) {
	return s.journal.AccountQuotaUsageProofBatch(accounts, now)
}

func (s *localAdmissionStore) HasLiveMoneyHolds(_ context.Context, now time.Time) (bool, error) {
	return s.journal.HasLiveMoneyHolds(now)
}
