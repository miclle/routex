package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestMonthlyQuotaNotificationCursorAdvancesSkipsFailuresAndWraps(t *testing.T) {
	rows := make([]entity.ResourceLimit, quotaNotificationBatchSize)
	for i := range rows {
		rows[i] = entity.ResourceLimit{ScopeKind: "user", ScopeID: fmt.Sprintf("usr_batch_%03d", i)}
	}
	failure := errors.New("transient observation failure")
	calls := 0
	cursor := quotaNotificationCursor{}
	observe := func(context.Context, string, string) error {
		calls++
		if calls == 1 {
			return failure
		}
		return nil
	}
	done, err := reconcileQuotaNotificationRows(context.Background(), rows, &cursor, observe)
	if done || !errors.Is(err, failure) || calls != 32 || cursor.ID != rows[31].ScopeID {
		t.Fatal("first failure starved later resources")
	}
	next := []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_batch_032"}}
	done, err = reconcileQuotaNotificationRows(context.Background(), next, &cursor, observe)
	if done || err != nil || calls != 33 || cursor.ID != "usr_batch_032" {
		t.Fatal("cursor did not proceed beyond first32")
	}
	done, err = reconcileQuotaNotificationRows(context.Background(), nil, &cursor, observe)
	if !done || err != nil || cursor != (quotaNotificationCursor{}) {
		t.Fatal("EOF did not wrap scanner")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reconcileQuotaNotificationRows(ctx, rows, &cursor, observe)
	if !errors.Is(err, context.Canceled) || calls != 33 || cursor != (quotaNotificationCursor{}) {
		t.Fatal("cancellation performed observation or advanced cursor")
	}
}

func TestStartQuotaNotificationsCancellationJoinsBeforeJournalClose(t *testing.T) {
	q, err := eventqueue.Open(filepath.Join(t.TempDir(), "quota-worker.db"), 10, 1024)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{recorder: &callRecorder{queue: q}, runtime: &gatewayRuntime{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stop, err := service.StartQuotaNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(stop)
	}
	wg.Wait()
	if err := q.Close(); err != nil {
		t.Fatal("joined worker prevented journal close", err)
	}
	unavailable := &Service{}
	if stop, err := unavailable.StartQuotaNotifications(context.Background()); stop != nil || err == nil {
		t.Fatal("worker started without recorder/runtime")
	}
	if stop, err := service.StartQuotaNotifications(context.Background()); stop != nil || err == nil {
		t.Fatal("worker started against closed journal")
	}
}
