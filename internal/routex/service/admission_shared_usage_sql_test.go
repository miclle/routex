package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func sharedUsageReadTestTarget() sharedUsageReadTarget {
	return sharedUsageReadTarget{InstallationID: "led_01arz3ndektsv4rrffq69g5fav", ModeRevision: 2}
}

func TestSharedUsageReadRequiresExactCurrentMode(t *testing.T) {
	target := sharedUsageReadTestTarget()
	binding := entity.LimitInstallation{ID: 1, JournalID: target.InstallationID, Initialized: true}
	mode := entity.AdmissionMode{ID: 1, InstallationID: target.InstallationID, Mode: "shared", Revision: 2}
	if !sharedUsageReadModeMatches(binding, mode, target) {
		t.Fatal("exact initialized shared installation was refused")
	}
	for _, test := range []struct {
		name   string
		change func(*entity.LimitInstallation, *entity.AdmissionMode, *sharedUsageReadTarget)
	}{
		{"uninitialized_binding", func(b *entity.LimitInstallation, _ *entity.AdmissionMode, _ *sharedUsageReadTarget) {
			b.Initialized = false
		}},
		{"foreign_binding", func(b *entity.LimitInstallation, _ *entity.AdmissionMode, _ *sharedUsageReadTarget) {
			b.JournalID = "led_01arz3ndektsv4rrffq69g5faw"
		}},
		{"binding_singleton", func(b *entity.LimitInstallation, _ *entity.AdmissionMode, _ *sharedUsageReadTarget) { b.ID = 2 }},
		{"local_mode", func(_ *entity.LimitInstallation, m *entity.AdmissionMode, _ *sharedUsageReadTarget) { m.Mode = "local" }},
		{"mode_case", func(_ *entity.LimitInstallation, m *entity.AdmissionMode, _ *sharedUsageReadTarget) {
			m.Mode = "SHARED"
		}},
		{"stale_revision", func(_ *entity.LimitInstallation, m *entity.AdmissionMode, _ *sharedUsageReadTarget) { m.Revision++ }},
		{"foreign_mode", func(_ *entity.LimitInstallation, m *entity.AdmissionMode, _ *sharedUsageReadTarget) {
			m.InstallationID = "led_01arz3ndektsv4rrffq69g5faw"
		}},
		{"mode_singleton", func(_ *entity.LimitInstallation, m *entity.AdmissionMode, _ *sharedUsageReadTarget) { m.ID = 2 }},
		{"target_case", func(_ *entity.LimitInstallation, _ *entity.AdmissionMode, p *sharedUsageReadTarget) {
			p.InstallationID = strings.ToUpper(p.InstallationID)
		}},
		{"zero_revision", func(_ *entity.LimitInstallation, _ *entity.AdmissionMode, p *sharedUsageReadTarget) {
			p.ModeRevision = 0
		}},
		{"revision_overflow", func(_ *entity.LimitInstallation, _ *entity.AdmissionMode, p *sharedUsageReadTarget) {
			p.ModeRevision = math.MaxUint64
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			b, m, p := binding, mode, target
			test.change(&b, &m, &p)
			if sharedUsageReadModeMatches(b, m, p) {
				t.Fatal("different or unavailable authority was accepted")
			}
		})
	}
}

func TestSharedUsageReadMonotonicInstantAndUnavailableDomain(t *testing.T) {
	now := sharedAuthorizationTestTime()
	domain := entity.AdmissionEconomicDomain{InstallationID: sharedUsageReadTestTarget().InstallationID, FormatVersion: 1,
		Initialized: true, CoverageStart: now.Add(-time.Hour).UnixNano(), ClockHighWater: now.Add(time.Second).UnixNano(), ReceiptCapacity: 10}
	instant, err := sharedUsageReadInstant(domain, now)
	if err != nil || instant != domain.ClockHighWater {
		t.Fatal("clock regression changed the sampled instant", instant, err)
	}
	later := now.Add(2 * time.Second)
	instant, err = sharedUsageReadInstant(domain, later)
	if err != nil || instant != later.UnixNano() {
		t.Fatal("fresh time did not advance sampling", instant, err)
	}
	for _, change := range []func(*entity.AdmissionEconomicDomain){
		func(d *entity.AdmissionEconomicDomain) { d.Initialized = false },
		func(d *entity.AdmissionEconomicDomain) { d.CoverageStart = 0 },
		func(d *entity.AdmissionEconomicDomain) { d.ClockHighWater = d.CoverageStart - 1 },
		func(d *entity.AdmissionEconomicDomain) { d.ClockHighWater = math.MaxInt64 },
		func(d *entity.AdmissionEconomicDomain) { d.FormatVersion = 2 },
		func(d *entity.AdmissionEconomicDomain) { d.ReceiptCount = d.ReceiptCapacity + 1 },
	} {
		changed := domain
		change(&changed)
		if instant, err := sharedUsageReadInstant(changed, now); instant != 0 || !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatalf("invalid domain provided a sampled instant: %d %v", instant, err)
		}
	}
	for _, invalid := range []time.Time{{}, time.Unix(0, 0), time.Unix(0, -1), time.Unix(0, math.MaxInt64), time.Date(5000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if instant, err := sharedUsageReadTime(invalid); instant != 0 || !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatalf("invalid/wrapped time was accepted: %s, %d, %v", invalid, instant, err)
		}
	}
}

func TestSharedUsageReadProofPreservesExactBirthAndIndependentCounts(t *testing.T) {
	target := sharedUsageReadTestTarget()
	now := sharedAuthorizationTestTime()
	row := entity.AdmissionEconomicAccount{InstallationID: target.InstallationID, Account: "usr_Case", AccountDigest: sharedEconomicIdentityDigest("usr_Case"),
		Birth: now.Add(-time.Hour), RPMCount: 3, ExecutorCount: 7}
	proof, err := sharedUsageReadProof(target, row.Account, now.UnixNano(), row)
	if err != nil || proof == nil || proof.Recorded == nil || !proof.Recorded.Birth.Equal(row.Birth) || proof.Recorded.RPM != 3 || proof.Recorded.Concurrency != 7 || !proof.AsOf.Equal(now) {
		t.Fatalf("recorded independent proof: %+v, %v", proof, err)
	}
	proof.Recorded.RPM, proof.Recorded.Concurrency = 999, 888
	proof.Recorded.Birth = now
	again, err := sharedUsageReadProof(target, row.Account, now.UnixNano(), row)
	if err != nil || again.Recorded.RPM != 3 || again.Recorded.Concurrency != 7 || !again.Recorded.Birth.Equal(row.Birth) {
		t.Fatal("proof retained a mutable reference to the source account")
	}
	for _, test := range []struct {
		name   string
		change func(*entity.AdmissionEconomicAccount)
	}{
		{"account_case", func(r *entity.AdmissionEconomicAccount) { r.Account = strings.ToLower(r.Account) }},
		{"account_digest", func(r *entity.AdmissionEconomicAccount) { r.AccountDigest = sharedEconomicIdentityDigest("usr_other") }},
		{"installation_case", func(r *entity.AdmissionEconomicAccount) { r.InstallationID = strings.ToUpper(r.InstallationID) }},
		{"birth_unknown", func(r *entity.AdmissionEconomicAccount) { r.Birth = time.Time{} }},
		{"birth_future", func(r *entity.AdmissionEconomicAccount) { r.Birth = now.Add(time.Second) }},
		{"birth_precision", func(r *entity.AdmissionEconomicAccount) { r.Birth = r.Birth.Add(time.Nanosecond) }},
		{"birth_wrap", func(r *entity.AdmissionEconomicAccount) { r.Birth = time.Date(5000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"negative_rpm", func(r *entity.AdmissionEconomicAccount) { r.RPMCount = -1 }},
		{"negative_executors", func(r *entity.AdmissionEconomicAccount) { r.ExecutorCount = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := row
			test.change(&changed)
			if got, err := sharedUsageReadProof(target, row.Account, now.UnixNano(), changed); got != nil || !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatalf("changed identity returned partial proof: %+v, %v", got, err)
			}
		})
	}
	row.RPMCount, row.ExecutorCount = 0, 0
	zero, err := sharedUsageReadProof(target, row.Account, now.UnixNano(), row)
	if err != nil || zero.Recorded == nil || zero.Recorded.RPM != 0 || zero.Recorded.Concurrency != 0 {
		t.Fatal("registered zero was confused with a missing account", err)
	}
}

type sharedUsageGuardTx struct{ calls int }

func (tx *sharedUsageGuardTx) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	tx.calls++
	return nil, errors.New("unexpected prepare")
}
func (tx *sharedUsageGuardTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	tx.calls++
	return nil, errors.New("unexpected execution")
}
func (tx *sharedUsageGuardTx) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	tx.calls++
	return nil, errors.New("unexpected query")
}
func (tx *sharedUsageGuardTx) QueryRowContext(context.Context, string, ...any) *sql.Row {
	tx.calls++
	return nil
}
func (*sharedUsageGuardTx) Commit() error   { return nil }
func (*sharedUsageGuardTx) Rollback() error { return nil }

func TestSharedUsageReadTransactionAndCancellationGuards(t *testing.T) {
	target := sharedUsageReadTestTarget()
	now := sharedAuthorizationTestTime()
	for _, tx := range []*gorm.DB{nil, {}, {Statement: &gorm.Statement{Context: context.Background()}}} {
		if proof, err := sampleSharedAccountUsageSQL(tx, target, "usr_valid", now); proof != nil || !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatalf("nontransaction accepted: %+v, %v", proof, err)
		}
	}
	stub := &sharedUsageGuardTx{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &gorm.DB{Statement: &gorm.Statement{Context: ctx, ConnPool: stub}}
	if proof, err := sampleSharedAccountUsageSQL(tx, target, "usr_valid", now); proof != nil || !errors.Is(err, context.Canceled) || stub.calls != 0 {
		t.Fatalf("canceled transaction touched storage: %+v, %v, %d", proof, err, stub.calls)
	}
	tx.Statement.Context = context.Background()
	for _, invalid := range []struct {
		target  sharedUsageReadTarget
		account string
		now     time.Time
	}{{sharedUsageReadTarget{}, "usr_valid", now}, {target, "invalid identity", now}, {target, "usr_valid", time.Time{}}} {
		if proof, err := sampleSharedAccountUsageSQL(tx, invalid.target, invalid.account, invalid.now); proof != nil || !errors.Is(err, errSharedAdmissionInvalid) || stub.calls != 0 {
			t.Fatalf("invalid request touched storage: %+v, %v, %d", proof, err, stub.calls)
		}
	}
}
