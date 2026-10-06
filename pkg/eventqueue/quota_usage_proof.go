package eventqueue

import (
	"encoding/json"
	"time"

	bolt "go.etcd.io/bbolt"
)

// QuotaUsageAccountProof pairs sampled usage with the journal's original account
// registration. Registered means a record exists; a zero CreatedAt is legacy
// unknown birth, never proof of a current resource incarnation.
type QuotaUsageAccountProof struct {
	Usage      AccountQuotaUsage
	Registered bool
	CreatedAt  time.Time
}

// QuotaUsageProofBatch samples registration and usage in one journal transaction.
// Callers must compare a nonzero registered birth with their authoritative
// resource birth independently; usage alone does not prove current identity.
type QuotaUsageProofBatch struct {
	Active        bool
	AsOf          time.Time
	CoverageStart time.Time
	TimeZone      string
	Accounts      map[string]QuotaUsageAccountProof
}

// AccountQuotaUsageProofBatch applies the existing bounded usage sampling rules,
// adding exact account registration facts without admitting or settling calls.
// Inactive accounting returns an empty, unproven snapshot. Invalid registration
// fails the entire read without exposing a partial result.
func (q *Queue) AccountQuotaUsageProofBatch(accounts []string, now time.Time) (*QuotaUsageProofBatch, error) {
	if err := validateQuotaUsageBatchAccounts(accounts); err != nil {
		return nil, err
	}
	result := &QuotaUsageProofBatch{Accounts: make(map[string]QuotaUsageAccountProof, len(accounts))}
	err := q.db.Update(func(tx *bolt.Tx) error {
		frame, err := sampleQuotaUsageFrame(tx, now)
		if err != nil || !frame.active {
			return err
		}
		bucket := tx.Bucket(quotaAccountBucket)
		if bucket == nil {
			return ErrInvalid
		}
		result.Active = true
		result.AsOf = frame.asOf
		result.CoverageStart = frame.coverageStart
		result.TimeZone = frame.timeZone
		for _, account := range accounts {
			proof := QuotaUsageAccountProof{}
			if bucket.Bucket([]byte(account)) != nil {
				return ErrInvalid
			}
			if raw := bucket.Get([]byte(account)); raw != nil {
				var created *int64
				if json.Unmarshal(raw, &created) != nil || created == nil || *created < 0 || *created > frame.asOf.UnixNano() {
					return ErrInvalid
				}
				proof.Registered = true
				if *created != 0 {
					proof.CreatedAt = time.Unix(0, *created).UTC()
				}
			}
			proof.Usage, err = sampleAccountQuotaUsage(tx, account, frame)
			if err != nil {
				return err
			}
			result.Accounts[account] = proof
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
