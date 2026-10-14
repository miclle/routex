package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// This helper runs only inside the maintained exclusive archive worker and its
// two disposable routex_test databases. No public listener or inference call is
// started; the separate lifecycle helper owns its private runtime worker.
func testRuntimeArchiveRetentionUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	if err := database.MigrateThrough(ctx, db, 112); err != nil {
		t.Fatal("empty released V112 creation", err)
	}
	store := runtimeArchiveIntegrationStore(t, "first")
	doc := configurationFixtureV1(t)
	raw, digest, err := encodeRuntimeConfigurationV1(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var ids []string
	expectedBytes := int64(0)
	for range 2 {
		identity, birth, e := newRuntimeArchiveIdentity()
		if e != nil {
			t.Fatal(e)
		}
		header := runtimeArchiveHeaderV1(identity, birth, doc, len(raw), digest)
		var chunks []entity.RuntimeConfigurationArchiveChunk
		for ordinal, start := 0, 0; start < len(raw); ordinal, start = ordinal+1, start+runtimeArchiveChunkBytes {
			end := min(start+runtimeArchiveChunkBytes, len(raw))
			c := entity.RuntimeConfigurationArchiveChunk{ID: fmt.Sprintf("%s-%03d", identity, ordinal), ArchiveID: identity, Ordinal: ordinal, PlaintextBytes: end - start, PlaintextDigest: runtimeArchiveByteDigest(raw[start:end])}
			c.SecretGeneration = runtimeArchiveChunkGeneration(header, c)
			ref, e := runtimeArchiveChunkReference(header, c)
			if e != nil {
				t.Fatal(e)
			}
			c.AuthCiphertext, e = store.Seal(ref, string(raw[start:end]))
			if e != nil {
				t.Fatal(e)
			}
			expectedBytes += int64(len(c.AuthCiphertext))
			chunks = append(chunks, c)
		}
		if e := db.Transaction(func(tx *gorm.DB) error {
			if e := tx.Create(&header).Error; e != nil {
				return e
			}
			return tx.Omit("Archive").Create(&chunks).Error
		}); e != nil {
			t.Fatal("released encrypted fixture", e)
		}
		ids = append(ids, identity)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 3)
	for range 3 {
		wg.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent V113 upgrade", err)
		}
	}
	verify := func() {
		t.Helper()
		var row entity.RuntimeArchiveRetention
		if e := db.Take(&row, 1).Error; e != nil || !row.Initialized || row.RetainedCount != 2 || row.EncryptedBytes != expectedBytes {
			t.Fatal("actual retained upgrade accounting", row, e)
		}
		var target entity.RuntimeArchiveContent
		canonical := ids[0]
		if ids[1] < canonical {
			canonical = ids[1]
		}
		if e := personalExact(db, "content_digest", digest).Take(&target).Error; e != nil || target.ContentDigest != digest || target.ArchiveID != canonical {
			t.Fatal("stable legacy duplicate target", target, e)
		}
		var count int64
		if e := db.Model(&entity.RuntimeArchiveCapture{}).Count(&count).Error; e != nil || count != 0 {
			t.Fatal("fabricated legacy capture provenance", e)
		}
		for _, identity := range ids {
			svc := &Service{db: db, secrets: store}
			facts, e := svc.readRuntimeConfigurationArchiveV1(ctx, identity)
			if e != nil || facts == nil {
				t.Fatal("upgrade lost retained source", e)
			}
		}
	}
	verify()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("repeat V113 startup", err)
	}
	verify()
	// Reproduce interrupted MySQL DDL with a nonempty content map and an empty
	// partially recreated retention table. Counters rebuild without deleting data.
	if err := db.Migrator().DropTable(&entity.RuntimeArchiveRetention{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&runtimeArchiveRetentionPartial{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("version = ?", 113).Delete(&runtimeArchiveTestLedger{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("partial V113 recovery", err)
	}
	verify()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("repeat partial recovery", err)
	}
	verify()
	duplicate := entity.RuntimeArchiveContent{ContentDigest: digest, ArchiveID: ids[0]}
	if err := db.Omit("Archive").Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("dedup index", err)
	}
	orphan := entity.RuntimeArchiveCapture{ArchiveID: configurationTestIDV1("rcv", 500), SnapshotID: configurationTestIDV1("cfg", 500), RoutesPublishedAt: time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond), CapturedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := db.Omit("Archive").Create(&orphan).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("provenance target FK", err)
	}
	if err := db.Model(&entity.RuntimeArchiveRetention{}).Where("id = ?", 1).Update("encrypted_bytes", -1).Error; err == nil {
		t.Fatal("negative accounting accepted")
	}
	verify()
	// Current startup also installs successor schemas. Refuse retained successor
	// facts before any DDL, then peel their exact receipts in reverse order so the
	// following released V111 upgrade really starts from that historical prefix.
	successors := []struct {
		version int
		models  []any
	}{
		{116, []any{&entity.RuntimeArchiveRollback{}}},
		{115, []any{&entity.RuntimeArchiveCleanup{}}},
		{114, []any{&entity.APIKeyDeliveryControllerAcknowledgement{}, &entity.APIKeyDeliveryControllerGrant{}}},
	}
	var before []runtimeArchiveCleanupLedger
	if err := db.Order("version").Find(&before).Error; err != nil || len(before) != 116 {
		t.Fatal("retention reset requires complete current ledger", err)
	}
	for i, row := range before {
		if row.Version != i+1 || row.AppliedAt == "" {
			t.Fatal("retention reset requires exact contiguous ledger")
		}
	}
	for _, successor := range successors {
		for _, model := range successor.models {
			var count int64
			if !db.Migrator().HasTable(model) || db.Model(model).Count(&count).Error != nil || count != 0 {
				t.Fatal("retained or missing successor prevents retention reset")
			}
		}
	}
	for _, successor := range successors {
		if err := db.Migrator().DropTable(successor.models...); err != nil {
			t.Fatal(err)
		}
		removed := db.Where("version = ? AND applied_at = ?", successor.version, before[successor.version-1].AppliedAt).Delete(&runtimeArchiveTestLedger{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal("exact successor receipt removal", removed.Error)
		}
		var after []runtimeArchiveCleanupLedger
		if err := db.Order("version").Find(&after).Error; err != nil || !reflect.DeepEqual(after, before[:successor.version-1]) {
			t.Fatal("retention reset changed predecessor ledger", err)
		}
	}
	// Reset only this worker's archive fixtures and V112/V113 test receipts.
	if err := db.Migrator().DropTable(&entity.RuntimeArchiveCapture{}, &entity.RuntimeArchiveContent{}, &entity.RuntimeArchiveRetention{}, &entity.RuntimeConfigurationArchiveChunk{}, &entity.RuntimeConfigurationArchive{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("version IN ?", []int{112, 113}).Delete(&runtimeArchiveTestLedger{}).Error; err != nil {
		t.Fatal(err)
	}
}

type runtimeArchiveRetentionPartial struct {
	ID int `gorm:"primaryKey;autoIncrement:false"`
}

func (runtimeArchiveRetentionPartial) TableName() string { return "runtime_archive_retention" }

func testRuntimeArchiveCaptureInput(t *testing.T, doc runtimeConfigurationV1, sequence uint64) *runtimeConfigurationCaptureV1 {
	t.Helper()
	raw, digest, err := encodeRuntimeConfigurationV1(t.Context(), doc)
	if err != nil {
		t.Fatal(err)
	}
	return &runtimeConfigurationCaptureV1{sequence: sequence, snapshotID: configurationTestIDV1("cfg", int(sequence)), routesPublishedAt: time.Now().UTC().Add(-time.Second), capturedAt: time.Now().UTC().Truncate(time.Microsecond), sourceDigest: doc.SourceDigest, authorizationDigest: doc.AuthorizationProjectionDigest, networkDigest: doc.NetworkPolicyDigest, contentDigest: digest, canonical: raw}
}
func testRuntimeArchiveStoredAccounting(t *testing.T, db *gorm.DB) entity.RuntimeArchiveRetention {
	t.Helper()
	var row entity.RuntimeArchiveRetention
	if err := db.Take(&row, 1).Error; err != nil {
		t.Fatal(err)
	}
	var count, total int64
	if err := db.Model(&entity.RuntimeConfigurationArchive{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	// Test-only independent byte census, not business persistence or an adapter.
	if err := db.Model(&entity.RuntimeConfigurationArchiveChunk{}).Select("COALESCE(SUM(OCTET_LENGTH(auth_ciphertext)),0)").Scan(&total).Error; err != nil {
		t.Fatal(err)
	}
	if !row.Initialized || row.RetainedCount != count || row.EncryptedBytes != total {
		t.Fatal("retained ledger differs from actual rows/envelopes", row, count, total)
	}
	return row
}
func testRuntimeArchiveDurableCapture(t *testing.T, db *gorm.DB, svc *Service) {
	t.Helper()
	ctx := t.Context()
	doc := configurationFixtureV1(t)
	doc.Source.Providers[0].Name = "first durable capture"
	capture := testRuntimeArchiveCaptureInput(t, doc, 201)
	before := testRuntimeArchiveStoredAccounting(t, db)
	result := svc.writeRuntimeConfigurationCaptureV1(ctx, capture)
	if result.State != "stored" || !runtimeArchiveID.MatchString(result.ArchiveID) {
		t.Fatal("durable capture receipt", result)
	}
	after := testRuntimeArchiveStoredAccounting(t, db)
	if after.RetainedCount != before.RetainedCount+1 || after.EncryptedBytes <= before.EncryptedBytes {
		t.Fatal("admission accounting", before, after)
	}
	var provenance entity.RuntimeArchiveCapture
	if err := personalExact(db, "archive_id", result.ArchiveID).Take(&provenance).Error; err != nil || provenance.SnapshotID != capture.snapshotID || !provenance.CapturedAt.Equal(capture.capturedAt) || !provenance.RoutesPublishedAt.Equal(capture.routesPublishedAt.Truncate(time.Microsecond)) {
		t.Fatal("first capture provenance", provenance, err)
	}
	next := *capture
	next.sequence++
	next.snapshotID = configurationTestIDV1("cfg", int(next.sequence))
	next.capturedAt = next.capturedAt.Add(time.Second)
	repeated := svc.writeRuntimeConfigurationCaptureV1(ctx, &next)
	if repeated.State != "deduplicated" || repeated.ArchiveID != result.ArchiveID {
		t.Fatal("exact canonical dedup", repeated)
	}
	if got := testRuntimeArchiveStoredAccounting(t, db); got != after {
		t.Fatal("dedup consumed capacity", got, after)
	}
	var original entity.RuntimeArchiveCapture
	if err := personalExact(db, "archive_id", result.ArchiveID).Take(&original).Error; err != nil || !reflect.DeepEqual(provenance, original) {
		t.Fatal("dedup rewrote original provenance", err)
	}
	// Cross-transaction duplicate capture must retain only one new target.
	doc.Source.Providers[0].Name = "concurrent durable capture"
	concurrent := testRuntimeArchiveCaptureInput(t, doc, 210)
	var wg sync.WaitGroup
	results := make(chan runtimeArchiveWriterResult, 3)
	for range 3 {
		wg.Go(func() { results <- svc.writeRuntimeConfigurationCaptureV1(ctx, concurrent) })
	}
	wg.Wait()
	close(results)
	saved := ""
	stored := 0
	for r := range results {
		if r.State != "stored" && r.State != "deduplicated" {
			t.Fatal("concurrent durable admission", r)
		}
		if r.State == "stored" {
			stored++
		}
		if saved != "" && saved != r.ArchiveID {
			t.Fatal("duplicate retained target")
		}
		saved = r.ArchiveID
	}
	if stored != 1 {
		t.Fatal("concurrent canonical storage count", stored)
	}
	baseline := testRuntimeArchiveStoredAccounting(t, db)
	// Boundary fixtures isolate cap logic without allocating 256 MiB of data.
	// Actual complete ledger accounting is verified before and after each fixture.
	for _, cap := range []struct {
		name   string
		values map[string]any
		state  string
	}{
		{"version", map[string]any{"retained_count": runtimeArchiveRetainedVersions}, "paused_version_cap"},
		{"encrypted_bytes", map[string]any{"encrypted_bytes": runtimeArchiveRetainedEncryptedBytes}, "paused_byte_cap"},
	} {
		t.Run(cap.name+"_cap", func(t *testing.T) {
			if err := db.Model(&entity.RuntimeArchiveRetention{}).Where("id = ?", 1).Updates(cap.values).Error; err != nil {
				t.Fatal(err)
			}
			doc.Source.Providers[0].Name = cap.name + " rejected capture"
			rejected := svc.writeRuntimeConfigurationCaptureV1(ctx, testRuntimeArchiveCaptureInput(t, doc, 220))
			if rejected.State != cap.state || rejected.ArchiveID != "" {
				t.Fatal("cap became success", rejected)
			}
			dedup := svc.writeRuntimeConfigurationCaptureV1(ctx, capture)
			if dedup.State != "deduplicated" || dedup.ArchiveID != result.ArchiveID {
				t.Fatal("cap blocked retained exact content", dedup)
			}
			if err := db.Model(&entity.RuntimeArchiveRetention{}).Where("id = ?", 1).Updates(map[string]any{"retained_count": baseline.RetainedCount, "encrypted_bytes": baseline.EncryptedBytes}).Error; err != nil {
				t.Fatal(err)
			}
			if got := testRuntimeArchiveStoredAccounting(t, db); got != baseline {
				t.Fatal("cap mutated retained source")
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if r := svc.writeRuntimeConfigurationCaptureV1(cancelled, capture); r.State != "failed" || r.ArchiveID != "" {
		t.Fatal("cancelled capture succeeded", r)
	}
	if got := testRuntimeArchiveStoredAccounting(t, db); got != baseline {
		t.Fatal("cancelled capture changed accounting")
	}
	// Root CAS accepts actual byte growth without blocking rotation at a cap and
	// rolls back a rejected stale subject together with its counter delta.
	var chunk entity.RuntimeConfigurationArchiveChunk
	if err := personalExact(db, "archive_id", result.ArchiveID).Order("ordinal").Take(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	var header entity.RuntimeConfigurationArchive
	if err := personalExact(db, "id", result.ArchiveID).Take(&header).Error; err != nil {
		t.Fatal(err)
	}
	reference, err := runtimeArchiveChunkReference(header, chunk)
	if err != nil {
		t.Fatal(err)
	}
	row := rootInventoryRow{chunk.ID, chunk.SecretGeneration, reference, chunk.AuthCiphertext}
	sameLength := strings.Repeat("x", len(chunk.AuthCiphertext))
	for _, delta := range []int{0, 1, -1} {
		replacement := sameLength
		if delta > 0 {
			replacement += "x"
		}
		if delta < 0 {
			replacement = replacement[:len(replacement)-1]
		}
		rollback := errors.New("rollback controlled envelope byte fixture")
		err := db.Transaction(func(tx *gorm.DB) error {
			outcome, e := rootRuntimeArchiveCAS(tx, row, replacement)
			if e != nil || outcome != "rewrapped" {
				t.Fatal("byte-aware root CAS", outcome, e)
			}
			got := testRuntimeArchiveStoredAccounting(t, tx)
			if got.EncryptedBytes != baseline.EncryptedBytes+int64(delta) || got.RetainedCount != baseline.RetainedCount {
				t.Fatal("actual CAS byte delta", got)
			}
			stale := row
			stale.generation = strings.Repeat("f", 64)
			if outcome, e := rootRuntimeArchiveCAS(tx, stale, replacement); e != nil || outcome != "changed" {
				t.Fatal("stale CAS changed bytes", outcome, e)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("controlled CAS rollback", err)
		}
		if got := testRuntimeArchiveStoredAccounting(t, db); got != baseline {
			t.Fatal("rolled-back CAS changed ledger")
		}
	}
	rollback := errors.New("rollback cap-boundary root rewrap fixture")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&entity.RuntimeArchiveRetention{}).Where("id = ?", 1).Update("encrypted_bytes", runtimeArchiveRetainedEncryptedBytes).Error; err != nil {
			return err
		}
		outcome, err := rootRuntimeArchiveCAS(tx, row, sameLength+"x")
		if err != nil || outcome != "rewrapped" {
			t.Fatal("cap prevented root rewrap", outcome, err)
		}
		var ledger entity.RuntimeArchiveRetention
		if err := tx.Take(&ledger, 1).Error; err != nil || ledger.EncryptedBytes != runtimeArchiveRetainedEncryptedBytes+1 || ledger.RetainedCount != baseline.RetainedCount {
			t.Fatal("over-cap rewrap byte accounting", ledger, err)
		}
		if !errors.Is(runtimeArchiveRetentionAdmission(ledger, 1), errRuntimeArchiveByteCap) {
			t.Fatal("over-cap rewrap allowed new archive")
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal("cap-boundary fixture rollback", err)
	}
	if got := testRuntimeArchiveStoredAccounting(t, db); got != baseline {
		t.Fatal("cap-boundary rewrap changed retained source")
	}
	var chunks []entity.RuntimeConfigurationArchiveChunk
	if err := personalExact(db, "archive_id", result.ArchiveID).Order("ordinal").Find(&chunks).Error; err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if bytes.Contains([]byte(c.AuthCiphertext), capture.canonical) {
			t.Fatal("capture plaintext retained")
		}
	}
}

func testRuntimeArchiveAutomaticLifecycle(t *testing.T, db *gorm.DB, secrets *Service) {
	t.Helper()
	// This separate Service has no public listener or inference traffic. Genuine
	// startup/refresh/shutdown must own and join the automatic capture worker.
	svc := &Service{db: db, secrets: secrets.secrets, runtimeRefreshInterval: time.Hour}
	before := testRuntimeArchiveStoredAccounting(t, db)
	if err := svc.StartRuntime(t.Context()); err != nil {
		t.Fatal("automatic archive startup", err)
	}
	t.Cleanup(svc.StopRuntime)
	wait := func() runtimeArchiveWriterResult {
		t.Helper()
		capture := svc.runtime.configurationCapture.latest.Load()
		if capture == nil {
			t.Fatal("startup did not prepare coherent capture")
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if result := svc.runtime.configurationCapture.archiveWriter.latest.Load(); result != nil && result.Sequence == capture.sequence {
				return *result
			}
			select {
			case <-ticker.C:
			case <-deadline.C:
				t.Fatal("automatic capture lacked bounded terminal observation")
			}
		}
	}
	first := wait()
	if first.State != "stored" || !runtimeArchiveID.MatchString(first.ArchiveID) {
		t.Fatal("startup did not durably capture", first)
	}
	after := testRuntimeArchiveStoredAccounting(t, db)
	if after.RetainedCount != before.RetainedCount+1 {
		t.Fatal("automatic capture retained count", before, after)
	}
	if err := svc.RefreshRuntime(t.Context()); err != nil {
		t.Fatal("unchanged automatic refresh", err)
	}
	second := wait()
	if second.State != "deduplicated" || second.ArchiveID != first.ArchiveID || second.Sequence <= first.Sequence {
		t.Fatal("automatic refresh did not deduplicate original source", first, second)
	}
	if got := testRuntimeArchiveStoredAccounting(t, db); got != after {
		t.Fatal("automatic refresh consumed retention")
	}
	svc.StopRuntime()
	for _, done := range []<-chan struct{}{svc.runtime.done, svc.runtime.configurationCapture.archiveWriter.done} {
		select {
		case <-done:
		default:
			t.Fatal("shutdown returned before owned worker join")
		}
	}
	if svc.runtime.configurationCapture.latest.Load() != nil || len(svc.runtime.configurationCapture.archiveWriter.queue) != 0 {
		t.Fatal("shutdown retained source or queued capture")
	}
}
