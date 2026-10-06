package eventqueue

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestQuotaUsageProofBoundsInactiveAndExactIDs(t *testing.T) {
	q, err := Open(filepath.Join(t.TempDir(), "inactive.db"), 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	for _, accounts := range [][]string{nil, {}, {""}, {"bad/id"}, {"key_a "}, {"键"}, {strings.Repeat("a", 65)}, {"same", "same"}, make([]string, MaxQuotaUsageBatchAccounts+1)} {
		if result, err := q.AccountQuotaUsageProofBatch(accounts, quotaTestTime); !errors.Is(err, ErrInvalid) || result != nil {
			t.Fatal("invalid proof batch acquired a snapshot", result, err)
		}
	}
	result, err := q.AccountQuotaUsageProofBatch([]string{"Key_a", "key_a"}, time.Time{})
	if err != nil || result.Active || !result.AsOf.IsZero() || !result.CoverageStart.IsZero() || result.TimeZone != "" || len(result.Accounts) != 0 {
		t.Fatal("inactive journal invented identity or usage", result, err)
	}
	if status, err := q.QuotaStatus(); err != nil || status.Active {
		t.Fatal("proof activated accounting", status, err)
	}
	if err := q.EnableQuota("UTC", quotaTestTime); err != nil {
		t.Fatal(err)
	}
	maximum := make([]string, MaxQuotaUsageBatchAccounts)
	maximum[0], maximum[1] = "Key_a", "key_a"
	for i := 2; i < len(maximum); i++ {
		maximum[i] = fmt.Sprintf("key_%d", i)
	}
	result, err = q.AccountQuotaUsageProofBatch(maximum, quotaTestTime)
	if err != nil || !result.Active || len(result.Accounts) != MaxQuotaUsageBatchAccounts {
		t.Fatal("maximum or case-distinct proof batch rejected", result, err)
	}
	for _, account := range maximum {
		proof, ok := result.Accounts[account]
		if !ok || proof.Registered || !proof.CreatedAt.IsZero() || !proof.Usage.AsOf.Equal(result.AsOf) {
			t.Fatal("missing exact registration became identity proof", account, proof)
		}
	}
	if err := q.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(quotaAccountBucket).Stats().KeyN != 0 {
			return errors.New("proof read registered missing accounts")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaUsageProofOriginalBirthUsageAndValueOwnership(t *testing.T) {
	q, _ := newQuotaTest(t, "America/New_York")
	original := quotaPolicy("key_original")
	legacy := quotaPolicy("key_legacy")
	legacy.CreatedAt = time.Time{}
	requireReserve(t, q, "known", []QuotaLimit{original, legacy}, QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("1.000000000000000001"), Currency: "USD"}, quotaTestTime)
	requireComplete(t, q, "known", QuotaSettlement{Tokens: limitPtr(7), Money: moneyPtr("0.000000000000000001"), Currency: "USD"}, quotaTestTime)
	accounts := []string{original.Account, legacy.Account, "key_missing", "Key_original"}
	now := quotaTestTime.Add(time.Second)
	proofs, err := q.AccountQuotaUsageProofBatch(accounts, now)
	if err != nil || !proofs.Active || !proofs.AsOf.Equal(now) || !proofs.CoverageStart.Equal(quotaTestTime) || proofs.TimeZone != "America/New_York" {
		t.Fatal("proof calendar changed", proofs, err)
	}
	old, err := q.AccountQuotaUsageBatch(accounts, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		if !reflect.DeepEqual(proofs.Accounts[account].Usage, old.Accounts[account]) || !reflect.DeepEqual(proofs.Accounts[account].Usage, *requireUsage(t, q, account, now)) {
			t.Fatal("proof changed established usage semantics", account)
		}
	}
	root := proofs.Accounts[original.Account]
	if !root.Registered || !root.CreatedAt.Equal(original.CreatedAt) || root.CreatedAt.Equal(original.CreatedAt.Add(time.Millisecond)) || root.CreatedAt.Equal(proofs.AsOf) {
		t.Fatal("proof substituted the sample or a reincarnated resource birth", root)
	}
	if legacyProof := proofs.Accounts[legacy.Account]; !legacyProof.Registered || !legacyProof.CreatedAt.IsZero() {
		t.Fatal("legacy registration claimed known birth", legacyProof)
	}
	for _, missing := range []string{"key_missing", "Key_original"} {
		if proof := proofs.Accounts[missing]; proof.Registered || !proof.CreatedAt.IsZero() {
			t.Fatal("registration borrowed an alias or invented an identity", missing, proof)
		}
	}
	proofs.Accounts[original.Account].Usage.Month.MoneyUsed["USD"] = "999"
	if old.Accounts[original.Account].Month.MoneyUsed["USD"] == "999" || proofs.Accounts[legacy.Account].Usage.Month.MoneyUsed["USD"] == "999" {
		t.Fatal("proof maps alias another returned snapshot")
	}
	backward, err := q.AccountQuotaUsageProofBatch(accounts, quotaTestTime.Add(-time.Hour))
	if err != nil || !backward.AsOf.Equal(now) || backward.Accounts[original.Account].Usage.Month.MoneyUsed["USD"] != "0.000000000000000001" || !backward.Accounts[original.Account].CreatedAt.Equal(original.CreatedAt) {
		t.Fatal("backward sample changed logical time, values or registration", backward, err)
	}
}

func TestQuotaUsageProofCorruptBirthFailsClosedAndLegacyReadStaysIndependent(t *testing.T) {
	for _, raw := range []string{"", "null", " null ", "-1", "1.5", "true", "{}", "[]", `"1"`, "9223372036854775808", strconv.FormatInt(quotaTestTime.Add(time.Hour).UnixNano(), 10)} {
		t.Run(fmt.Sprintf("raw_%q", raw), func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			if err := q.db.Update(func(tx *bolt.Tx) error {
				return tx.Bucket(quotaAccountBucket).Put([]byte("key_corrupt"), []byte(raw))
			}); err != nil {
				t.Fatal(err)
			}
			var before []byte
			if err := q.db.View(func(tx *bolt.Tx) error {
				before = bytes.Clone(tx.Bucket(limitMetaBucket).Get([]byte("clock")))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if result, err := q.AccountQuotaUsageProofBatch([]string{"key_missing", "key_corrupt"}, quotaTestTime.Add(time.Second)); !errors.Is(err, ErrInvalid) || result != nil {
				t.Fatal("corrupt later birth returned a partial proof", result, err)
			}
			if err := q.db.View(func(tx *bolt.Tx) error {
				if !bytes.Equal(before, tx.Bucket(limitMetaBucket).Get([]byte("clock"))) {
					return errors.New("failed proof committed its logical clock")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if old, err := q.AccountQuotaUsageBatch([]string{"key_corrupt"}, quotaTestTime); err != nil || !old.Active || len(old.Accounts) != 1 {
				t.Fatal("legacy API acquired an unnecessary registry dependency", old, err)
			}
		})
	}
}

func TestQuotaUsageProofMalformedBucketAndOutage(t *testing.T) {
	for _, missingBucket := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing_%v", missingBucket), func(t *testing.T) {
			q, _ := newQuotaTest(t, "UTC")
			if err := q.db.Update(func(tx *bolt.Tx) error {
				if missingBucket {
					return tx.DeleteBucket(quotaAccountBucket)
				}
				_, err := tx.Bucket(quotaAccountBucket).CreateBucket([]byte("key_corrupt"))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if result, err := q.AccountQuotaUsageProofBatch([]string{"key_corrupt"}, quotaTestTime); !errors.Is(err, ErrInvalid) || result != nil {
				t.Fatal("missing or nested registry returned a proof", result, err)
			}
			if old, err := q.AccountQuotaUsageBatch([]string{"key_corrupt"}, quotaTestTime); err != nil || !old.Active {
				t.Fatal("legacy usage unexpectedly inspected registry bucket", old, err)
			}
		})
	}
	q, _ := newQuotaTest(t, "UTC")
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := q.AccountQuotaUsageProofBatch([]string{"key_closed"}, quotaTestTime); err == nil || result != nil {
		t.Fatal("closed journal returned proof or known zero", result, err)
	}
}

func TestQuotaUsageProofSamplesRegistrationAndSettlementAtomically(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	accounts := []string{"key_one", "key_two"}
	policies := []QuotaLimit{quotaPolicy(accounts[0]), quotaPolicy(accounts[1])}
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := q.ReserveWithQuota("atomic", []byte("fallback"), policies, QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("1.000000000000000002"), Currency: "USD"}, quotaTestTime); err != nil {
			t.Error(err)
			return
		}
		if _, err := q.CompleteQuota("atomic", []byte("done"), QuotaSettlement{Tokens: limitPtr(7), Money: moneyPtr("1"), Currency: "USD"}, quotaTestTime); err != nil {
			t.Error(err)
		}
	})
	for range 100 {
		batch, err := q.AccountQuotaUsageProofBatch(accounts, quotaTestTime.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		first, second := batch.Accounts[accounts[0]], batch.Accounts[accounts[1]]
		if !reflect.DeepEqual(first, second) || !first.Usage.AsOf.Equal(batch.AsOf) || !first.Usage.CoverageStart.Equal(batch.CoverageStart) || first.Usage.TimeZone != batch.TimeZone {
			t.Fatal("proof observed half of registration or pair settlement", batch)
		}
		if !first.Registered {
			if !first.CreatedAt.IsZero() || first.Usage.Active.TokensHeld != 0 || first.Usage.Month.TokensUsed != 0 {
				t.Fatal("unregistered account acquired admission or settled facts", first)
			}
		} else if !first.CreatedAt.Equal(quotaTestTime) {
			t.Fatal("registered account lost original birth", first)
		}
	}
	wg.Wait()
	batch, err := q.AccountQuotaUsageProofBatch(accounts, quotaTestTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		proof := batch.Accounts[account]
		if !proof.Registered || !proof.CreatedAt.Equal(quotaTestTime) || proof.Usage.Active.TokensHeld != 0 || len(proof.Usage.Active.MoneyHeld) != 0 || proof.Usage.Month.TokensUsed != 7 || proof.Usage.Month.MoneyUsed["USD"] != "1" {
			t.Fatal("atomic complete facts changed or retained active hold", proof)
		}
	}
}

func TestQuotaUsageProofPreservesHeldAndUnknownDimensions(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	accounts := []string{"key_held", "key_unknown", "key_unbounded"}
	requireReserve(t, q, "held", []QuotaLimit{quotaPolicy(accounts[0])}, QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("1.000000000000000001"), Currency: "USD"}, quotaTestTime)
	requireReserve(t, q, "unknown", []QuotaLimit{quotaPolicy(accounts[1])}, QuotaBound{Tokens: limitPtr(2)}, quotaTestTime)
	requireComplete(t, q, "unknown", QuotaSettlement{}, quotaTestTime)
	requireReserve(t, q, "unbounded", []QuotaLimit{quotaPolicy(accounts[2])}, QuotaBound{}, quotaTestTime)
	requireComplete(t, q, "unbounded", QuotaSettlement{}, quotaTestTime)
	batch, err := q.AccountQuotaUsageProofBatch(accounts, quotaTestTime)
	if err != nil {
		t.Fatal(err)
	}
	held, unknown, unbounded := batch.Accounts[accounts[0]], batch.Accounts[accounts[1]], batch.Accounts[accounts[2]]
	if held.Usage.Active.TokensHeld != 10 || held.Usage.Active.MoneyHeld["USD"] != "1.000000000000000001" || held.Usage.Month.TokensHeld != 0 || held.Usage.Month.TokensUsed != 0 {
		t.Fatal("live holds became monthly settled usage", held)
	}
	if unknown.Usage.Month.TokensHeld != 2 || unknown.Usage.Month.TokensUsed != 0 || unknown.Usage.Month.MoneyUnknown != 1 || len(unknown.Usage.Month.MoneyHeld) != 0 || unbounded.Usage.Month.TokensUnknown != 1 || unbounded.Usage.Month.MoneyUnknown != 1 {
		t.Fatal("unknown terminal usage became settled or retroactively priced", unknown, unbounded)
	}
	old, err := q.AccountQuotaUsageBatch(accounts, quotaTestTime)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		if !reflect.DeepEqual(batch.Accounts[account].Usage, old.Accounts[account]) {
			t.Fatal("new proof altered established hold/unknown semantics", account)
		}
	}
}
