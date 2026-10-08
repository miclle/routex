package handler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type publicationFixtureRefreshContextKey struct{}

func publicationMetadataFixtureDB(ctx context.Context) *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{
		Context: ctx, Table: "runtime_publications", Schema: &schema.Schema{Table: "runtime_publications"},
		Dest: &entity.RuntimePublication{Status: "failed", ErrorCode: "database_unavailable"},
	}}
}

func TestPersonalKeyPublicationMetadataExactRefreshProof(t *testing.T) {
	var b personalKeyWarningFixturePublicationBarrier
	b.enableMetadataFence()
	b.armed.Store(true)
	ctx := context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, &b)
	query := publicationMetadataFixtureDB(ctx)
	b.beforeQuery(query)
	if !errors.Is(query.Error, errPersonalKeyWarningFixturePublication) {
		t.Fatal("controlled publisher query was not rejected")
	}
	// A genuine failure in another refresh cannot borrow a previous rejection.
	otherCtx := context.WithValue(ctx, publicationFixtureRefreshContextKey{}, "another refresh")
	other := publicationMetadataFixtureDB(otherCtx)
	b.beforePublicationCreate(other)
	if other.Error != nil {
		t.Fatal("a different genuine refresh borrowed the synthetic proof")
	}
	b.armed.Store(false)
	first := publicationMetadataFixtureDB(ctx)
	b.beforePublicationCreate(first)
	if !errors.Is(first.Error, errPersonalKeyWarningFixturePublication) {
		t.Fatal("known synthetic metadata escaped after barrier release")
	}
	second := publicationMetadataFixtureDB(ctx)
	b.beforePublicationCreate(second)
	if second.Error != nil {
		t.Fatal("an already consumed proof suppressed another write")
	}
}

func TestPersonalKeyPublicationMetadataIndependentGuards(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*gorm.DB, *personalKeyWarningFixturePublicationBarrier)
	}{
		{"table", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) { tx.Statement.Table = "system_jobs" }},
		{"schema", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Schema.Table = "system_jobs"
		}},
		{"missing schema", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) { tx.Statement.Schema = nil }},
		{"unrelated type", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Dest = &entity.SystemJob{}
		}},
		{"bulk type", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Dest = []entity.RuntimePublication{{Status: "failed", ErrorCode: "database_unavailable"}}
		}},
		{"nil typed row", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Dest = (*entity.RuntimePublication)(nil)
		}},
		{"ready", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Dest.(*entity.RuntimePublication).Status = "ready"
		}},
		{"genuine other failure", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Dest.(*entity.RuntimePublication).ErrorCode = "invalid_configuration"
		}},
		{"existing create error", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Error = errors.New("genuine create failure")
		}},
		{"manual context", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Context = context.Background()
		}},
		{"foreign marker", func(tx *gorm.DB, _ *personalKeyWarningFixturePublicationBarrier) {
			tx.Statement.Context = context.WithValue(tx.Statement.Context, personalKeyWarningFixtureWorkerContext{}, &personalKeyWarningFixturePublicationBarrier{})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var b personalKeyWarningFixturePublicationBarrier
			b.enableMetadataFence()
			b.armed.Store(true)
			ctx := context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, &b)
			b.beforeQuery(publicationMetadataFixtureDB(ctx))
			tx := publicationMetadataFixtureDB(ctx)
			test.change(tx, &b)
			before := tx.Error
			b.beforePublicationCreate(tx)
			if tx.Error != before {
				t.Fatal("unrelated operation was changed")
			}
			matching := publicationMetadataFixtureDB(ctx)
			b.beforePublicationCreate(matching)
			if !errors.Is(matching.Error, errPersonalKeyWarningFixturePublication) {
				t.Fatal("unrelated operation consumed the exact pending proof")
			}
		})
	}
	for _, test := range []string{"disabled", "unarmed", "preexisting query error", "counter only"} {
		t.Run(test, func(t *testing.T) {
			var b personalKeyWarningFixturePublicationBarrier
			if test != "disabled" {
				b.enableMetadataFence()
			}
			b.armed.Store(test != "unarmed")
			ctx := context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, &b)
			query := publicationMetadataFixtureDB(ctx)
			if test == "preexisting query error" {
				query.Error = errors.New("genuine earlier failure")
			}
			if test == "counter only" {
				b.rejected.Store(99)
			} else {
				b.beforeQuery(query)
			}
			tx := publicationMetadataFixtureDB(ctx)
			b.beforePublicationCreate(tx)
			if tx.Error != nil {
				t.Fatal("no exact newly injected query proof existed")
			}
		})
	}
}

type incomparablePublicationFixtureContext struct {
	context.Context
	values []byte
}

func TestPersonalKeyPublicationMetadataProofBounds(t *testing.T) {
	var b personalKeyWarningFixturePublicationBarrier
	b.enableMetadataFence()
	b.armed.Store(true)
	ctx := context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, &b)
	bad := incomparablePublicationFixtureContext{Context: ctx, values: []byte{1}}
	b.beforeQuery(publicationMetadataFixtureDB(bad))
	tx := publicationMetadataFixtureDB(bad)
	b.beforePublicationCreate(tx)
	if tx.Error != nil || b.purgeMetadataProofsAfterJoin() == nil {
		t.Fatal("incomparable identity must fail closed and report fixture failure")
	}
	for i := range personalKeyPublicationMetadataProofLimit {
		b.beforeQuery(publicationMetadataFixtureDB(context.WithValue(ctx, publicationFixtureRefreshContextKey{}, i)))
	}
	overflow := context.WithValue(ctx, publicationFixtureRefreshContextKey{}, "overflow")
	b.beforeQuery(publicationMetadataFixtureDB(overflow))
	tx = publicationMetadataFixtureDB(overflow)
	b.beforePublicationCreate(tx)
	if tx.Error != nil || len(b.metadataProofs) != personalKeyPublicationMetadataProofLimit {
		t.Fatal("overflow must never broaden metadata suppression or grow the map")
	}
	if b.purgeMetadataProofsAfterJoin() == nil || len(b.metadataProofs) != 0 {
		t.Fatal("bounded proof failure or post-join purge missing")
	}
	if b.purgeMetadataProofsAfterJoin() != nil {
		t.Fatal("post-join error was not consumed")
	}
}

func TestPersonalKeyPublicationMetadataConcurrentConsumeOnce(t *testing.T) {
	var b personalKeyWarningFixturePublicationBarrier
	b.enableMetadataFence()
	b.armed.Store(true)
	ctx := context.WithValue(context.Background(), personalKeyWarningFixtureWorkerContext{}, &b)
	b.beforeQuery(publicationMetadataFixtureDB(ctx))
	var wg sync.WaitGroup
	var suppressed atomic.Int64
	for range 16 {
		wg.Go(func() {
			tx := publicationMetadataFixtureDB(ctx)
			b.beforePublicationCreate(tx)
			if errors.Is(tx.Error, errPersonalKeyWarningFixturePublication) {
				suppressed.Add(1)
			}
		})
	}
	wg.Wait()
	if suppressed.Load() != 1 {
		t.Fatal("exact per-refresh proof was not consumed once")
	}
}
