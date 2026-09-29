package eventqueue

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestQuotaKilledProcessDurableBoundary(t *testing.T) {
	if stage := os.Getenv("ROUTEX_QUOTA_CRASH_STAGE"); stage != "" {
		q, err := Open(os.Getenv("ROUTEX_QUOTA_CRASH_PATH"), 10, 1024)
		if err != nil {
			t.Fatal(err)
		}
		if err := q.EnableQuota("UTC", quotaTestTime); err != nil {
			t.Fatal(err)
		}
		policy := quotaPolicy("key_root")
		policy.Tokens5H = limitPtr(10)
		policy.Concurrency = limitPtr(1)
		if stage != "enabled" {
			requireReserve(t, q, "durable", []QuotaLimit{policy}, tokenBound(10), quotaTestTime)
		}
		if stage == "complete" || stage == "acked" {
			requireComplete(t, q, "durable", QuotaSettlement{Tokens: limitPtr(3)}, quotaTestTime)
		}
		if stage == "acked" {
			if err := q.Ack("durable"); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Println("durable")
		for {
			time.Sleep(time.Hour)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"enabled", "reserved", "complete", "acked"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "crash.db")
			command := exec.Command(executable, "-test.run=^TestQuotaKilledProcessDurableBoundary$")
			command.Env = append(os.Environ(), "ROUTEX_QUOTA_CRASH_STAGE="+stage, "ROUTEX_QUOTA_CRASH_PATH="+path)
			pipe, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			command.Stderr = os.Stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
			marker, err := bufio.NewReader(pipe).ReadString('\n')
			if err != nil || strings.TrimSpace(marker) != "durable" {
				t.Fatalf("child durable marker %q %v", marker, err)
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = command.Wait()
			q, err := Open(path, 10, 1024)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = q.Close() }()
			usage := requireUsage(t, q, "key_root", quotaTestTime)
			rpm, active, err := q.AccountUsage("key_root", quotaTestTime)
			if err != nil || active != 0 {
				t.Fatalf("crash retained lease: %d %v", active, err)
			}
			facts, err := q.Read(10)
			if err != nil {
				t.Fatal(err)
			}
			switch stage {
			case "enabled":
				if rpm != 0 || len(facts) != 0 || usage.FiveHours.TokensHeld != 0 {
					t.Fatal("pre-reservation crash invented usage")
				}
			case "reserved":
				if rpm != 1 || len(facts) != 1 || string(facts[0].Payload) != "fallback" || usage.Active.TokensHeld != 0 || usage.FiveHours.TokensHeld != 10 {
					t.Fatalf("lost interrupted hold: %+v %+v", usage, facts)
				}
				receipt, err := q.QuotaReceipt("durable")
				if err != nil || receipt.State != "interrupted" {
					t.Fatal(receipt, err)
				}
				if _, err := q.CompleteQuota("durable", []byte("final"), QuotaSettlement{Tokens: limitPtr(3)}, quotaTestTime); !errors.Is(err, ErrQuotaConflict) {
					t.Fatal("recovered unknown rewritten", err)
				}
			case "complete", "acked":
				expectedFacts := 1
				if stage == "acked" {
					expectedFacts = 0
				}
				if rpm != 1 || len(facts) != expectedFacts || usage.FiveHours.TokensUsed != 3 || usage.FiveHours.TokensHeld != 0 {
					t.Fatalf("settlement torn from fact: %+v %+v", usage, facts)
				}
				requireComplete(t, q, "durable", QuotaSettlement{Tokens: limitPtr(3)}, quotaTestTime)
			}
		})
	}
}

func TestQuotaRecoveryUsesLatestInterruptedSettlementCheckpoint(t *testing.T) {
	q, path := newQuotaTest(t, "UTC")
	policy := quotaPolicy("key_checkpoint")
	policy.Tokens5H = limitPtr(20)
	requireReserve(t, q, "checkpoint", []QuotaLimit{policy}, tokenBound(10), quotaTestTime)
	zero := int64(0)
	if err := q.UpdatePendingQuota("checkpoint", []byte("safe-fallback"), QuotaSettlement{Tokens: &zero}); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	receipt, err := reopened.QuotaReceipt("checkpoint")
	if err != nil || receipt.State != "interrupted" || receipt.Actual.Tokens == nil || *receipt.Actual.Tokens != 0 {
		t.Fatalf("recovered receipt = %+v, error = %v", receipt, err)
	}
	usage := requireUsage(t, reopened, "key_checkpoint", quotaTestTime)
	if usage.FiveHours.TokensUsed != 0 || usage.FiveHours.TokensHeld != 0 || usage.FiveHours.TokensUnknown != 0 {
		t.Fatalf("safe checkpoint recovered as unknown work: %+v", usage)
	}
	entries, err := reopened.Read(10)
	if err != nil || len(entries) != 1 || string(entries[0].Payload) != "safe-fallback" {
		t.Fatalf("recovered entries = %+v, error = %v", entries, err)
	}
}

func TestQuotaAdmissionRecoveryIsAtomicBeforeFirstCheckpoint(t *testing.T) {
	q, path := newQuotaTest(t, "UTC")
	policy := quotaPolicy("key_admission")
	policy.Tokens5H = limitPtr(20)
	zero := int64(0)
	if err := q.ReserveWithQuotaRecovery("admitted", []byte("zero-work-fallback"), []QuotaLimit{policy}, tokenBound(10), QuotaSettlement{Tokens: &zero}, quotaTestTime); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, 100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	receipt, err := reopened.QuotaReceipt("admitted")
	if err != nil || receipt.State != "interrupted" || receipt.Actual.Tokens == nil || *receipt.Actual.Tokens != 0 {
		t.Fatalf("pre-dispatch recovery = %+v, error = %v", receipt, err)
	}
	usage := requireUsage(t, reopened, "key_admission", quotaTestTime)
	if usage.FiveHours.TokensUsed != 0 || usage.FiveHours.TokensHeld != 0 || usage.FiveHours.TokensUnknown != 0 {
		t.Fatalf("pre-dispatch crash retained an unknown hold: %+v", usage)
	}
}

func TestQuotaRecoveryMustFitTheAdmittedBound(t *testing.T) {
	q, _ := newQuotaTest(t, "UTC")
	defer func() { _ = q.Close() }()
	policy := quotaPolicy("key_recovery_bound")
	policy.Tokens5H = limitPtr(20)
	policy.MoneyMonth = moneyPtr("20")
	policy.Currency = "USD"
	bound := QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("10"), Currency: "USD", Revision: "bound", PriceRevision: "price", BasisDigest: strings.Repeat("a", 64)}
	if err := q.ReserveWithQuotaRecovery("currency", []byte("fallback"), []QuotaLimit{policy}, bound, QuotaSettlement{Money: moneyPtr("0"), Currency: "EUR"}, quotaTestTime); !errors.Is(err, ErrQuotaCurrency) {
		t.Fatalf("mismatched recovery currency = %v", err)
	}
	if err := q.ReserveWithQuotaRecovery("tokens", []byte("fallback"), []QuotaLimit{policy}, bound, QuotaSettlement{Tokens: limitPtr(11)}, quotaTestTime); !errors.Is(err, ErrQuotaBound) {
		t.Fatalf("over-bound recovery tokens = %v", err)
	}
	if err := q.ReserveWithQuotaRecovery("money", []byte("fallback"), []QuotaLimit{policy}, bound, QuotaSettlement{Money: moneyPtr("11"), Currency: "USD"}, quotaTestTime); !errors.Is(err, ErrQuotaBound) {
		t.Fatalf("over-bound recovery money = %v", err)
	}
	if err := q.ReserveWithQuotaRecovery("update", []byte("fallback"), []QuotaLimit{policy}, bound, QuotaSettlement{}, quotaTestTime); err != nil {
		t.Fatal(err)
	}
	if err := q.UpdatePendingQuota("update", []byte("changed"), QuotaSettlement{Money: moneyPtr("0"), Currency: "EUR"}); !errors.Is(err, ErrQuotaCurrency) {
		t.Fatalf("updated mismatched recovery currency = %v", err)
	}
}

func TestQuotaMissingOrCorruptHistoryFailsClosed(t *testing.T) {
	for _, damage := range []string{"missing_bucket", "invalid_receipt", "invalid_fact", "unknown_version"} {
		t.Run(damage, func(t *testing.T) {
			q, path := newQuotaTest(t, "UTC")
			requireReserve(t, q, "receipt", []QuotaLimit{quotaPolicy("account")}, tokenBound(2), quotaTestTime)
			requireComplete(t, q, "receipt", QuotaSettlement{Tokens: limitPtr(1)}, quotaTestTime)
			if err := q.db.Update(func(tx *bolt.Tx) error {
				switch damage {
				case "missing_bucket":
					return tx.DeleteBucket(quotaEntryBucket)
				case "invalid_receipt":
					return tx.Bucket(quotaEntryBucket).Put([]byte("receipt"), []byte("{}"))
				case "invalid_fact":
					return tx.Bucket(readyBucket).Put([]byte("receipt"), []byte("changed"))
				default:
					return tx.Bucket(limitMetaBucket).Put([]byte("version"), []byte("999"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := q.Close(); err != nil {
				t.Fatal(err)
			}
			if reopened, err := Open(path, 100, 1024); err == nil {
				_ = reopened.Close()
				t.Fatal("corrupt established history accepted")
			}
		})
	}
}
