package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/secret"
)

func runtimeArchiveWriterWait(t *testing.T, ch <-chan uint64) uint64 {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("bounded archive worker did not progress")
		return 0
	}
}
func TestRuntimeArchiveWriterNonblockingQueueAndMonotonicFailureEvidence(t *testing.T) {
	var w runtimeArchiveWriterState
	entered, completed := make(chan uint64, 2), make(chan uint64, 2)
	release := make(chan struct{})
	w.start(t.Context(), func(ctx context.Context, c *runtimeConfigurationCaptureV1) runtimeArchiveWriterResult {
		entered <- c.sequence
		select {
		case <-release:
		case <-ctx.Done():
		}
		completed <- c.sequence
		return runtimeArchiveWriterResult{Sequence: c.sequence, State: "failed"}
	})
	t.Cleanup(w.stop)
	w.submit(&runtimeConfigurationCaptureV1{sequence: 1})
	if runtimeArchiveWriterWait(t, entered) != 1 {
		t.Fatal("first capture")
	}
	w.submit(&runtimeConfigurationCaptureV1{sequence: 2})
	w.submit(&runtimeConfigurationCaptureV1{sequence: 3})
	result := w.latest.Load()
	if result == nil || result.Sequence != 3 || result.State != "queue_full" || result.ArchiveID != "" {
		t.Fatal("overflow invented durable history", result)
	}
	close(release)
	if runtimeArchiveWriterWait(t, completed) != 1 || runtimeArchiveWriterWait(t, completed) != 2 {
		t.Fatal("queued order")
	}
	w.stop()
	if result = w.latest.Load(); result.Sequence != 3 || result.State != "queue_full" {
		t.Fatal("old completion overwrote newer omission")
	}
	if len(w.queue) != 0 {
		t.Fatal("stopped queue retained input")
	}
}
func TestRuntimeArchiveWriterStopCancelsJoinsAndRejectsFurtherCaptures(t *testing.T) {
	var w runtimeArchiveWriterState
	entered := make(chan uint64, 1)
	w.start(t.Context(), func(ctx context.Context, c *runtimeConfigurationCaptureV1) runtimeArchiveWriterResult {
		entered <- c.sequence
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > runtimeArchiveWriterBudget {
			t.Error("writer lacks own finite operation budget")
		}
		<-ctx.Done()
		return runtimeArchiveWriterResult{Sequence: c.sequence, State: "failed"}
	})
	w.submit(&runtimeConfigurationCaptureV1{sequence: 1})
	runtimeArchiveWriterWait(t, entered)
	w.submit(&runtimeConfigurationCaptureV1{sequence: 2})
	w.stop()
	w.submit(&runtimeConfigurationCaptureV1{sequence: 3})
	if len(entered) != 0 || len(w.queue) != 0 {
		t.Fatal("shutdown dispatched or retained pending capture")
	}
	if result := w.latest.Load(); result == nil || result.Sequence != 1 || result.State != "failed" || result.ArchiveID != "" {
		t.Fatal("cancelled write became storage proof", result)
	}
}
func TestRuntimeArchiveWriterPreparationDoesNotWaitOrMutatePublishedAuthority(t *testing.T) {
	svc, data := configurationCaptureFixtureV1(t)
	entered := make(chan uint64, 1)
	writer := &svc.runtime.configurationCapture.archiveWriter
	writer.start(t.Context(), func(ctx context.Context, c *runtimeConfigurationCaptureV1) runtimeArchiveWriterResult {
		entered <- c.sequence
		<-ctx.Done()
		return runtimeArchiveWriterResult{Sequence: c.sequence, State: "failed"}
	})
	t.Cleanup(writer.stop)
	routes, auth, status := svc.runtime.routes.Load(), svc.runtime.auth.Load(), svc.runtime.status.Load()
	svc.prepareRuntimeConfigurationCaptureV1(t.Context(), svc.runtime, configurationCaptureTicketV1(t, svc, data))
	runtimeArchiveWriterWait(t, entered)
	capture := svc.runtime.configurationCapture.latest.Load()
	if !runtimeArchiveCaptureProvenanceValidV1(capture) {
		t.Fatal("coherent immutable capture absent")
	}
	if svc.runtime.routes.Load() != routes || svc.runtime.auth.Load() != auth || svc.runtime.status.Load() != status {
		t.Fatal("archive side effect altered authority")
	}
	writer.stop()
}
func TestRuntimeArchiveWriterRejectsMalformedOrUnmatchedCaptureBeforeStorage(t *testing.T) {
	svc, data := configurationCaptureFixtureV1(t)
	svc.prepareRuntimeConfigurationCaptureV1(t.Context(), svc.runtime, configurationCaptureTicketV1(t, svc, data))
	original := svc.runtime.configurationCapture.latest.Load()
	for _, change := range []func(*runtimeConfigurationCaptureV1){
		func(c *runtimeConfigurationCaptureV1) { c.snapshotID = "cfg_unknown" },
		func(c *runtimeConfigurationCaptureV1) { c.contentDigest = c.sourceDigest },
		func(c *runtimeConfigurationCaptureV1) { c.sourceDigest = c.networkDigest },
		func(c *runtimeConfigurationCaptureV1) { c.sequence = 0 },
		func(c *runtimeConfigurationCaptureV1) { c.capturedAt = c.routesPublishedAt.Add(-time.Second) },
	} {
		copy := *original
		change(&copy)
		r := svc.writeRuntimeConfigurationCaptureV1(t.Context(), &copy)
		if r.State != "failed" || r.ArchiveID != "" {
			t.Fatal("malformed capture became positive receipt", r)
		}
	}
}

func TestRuntimeArchiveHistoricalSourceNeverRestoresCurrentKeyAuthority(t *testing.T) {
	svc, data := configurationCaptureFixtureV1(t)
	bearer := "rx_" + strings.Repeat("a", 43)
	data.Keys[0].TokenHash = secret.SHA256Hex(bearer)
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	source, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	auth.SourceDigest = source
	auth.installationProjectionDigest, err = runtimeAuthorizationProjection(t.Context(), auth)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(auth)
	routes := *svc.runtime.routes.Load()
	routes.Digest = source
	svc.runtime.routes.Store(&routes)
	svc.storeRuntimeStatus(time.Now().UTC(), "")
	if _, err := svc.authenticateRuntimeKey(bearer); err != nil {
		t.Fatal("live fixture lacked Key authority", err)
	}
	svc.prepareRuntimeConfigurationCaptureV1(t.Context(), svc.runtime, configurationCaptureTicketV1(t, svc, data))
	capture := svc.runtime.configurationCapture.latest.Load()
	svc.InvalidateRuntimeUser(data.Keys[0].UserID)
	for range 2 {
		facts := configurationCaptureFactsV1(t, capture)
		if facts.Document.Source.Keys[0].TokenHash != secret.SHA256Hex(bearer) {
			t.Fatal("historical Key source changed")
		}
		if _, err := svc.authenticateRuntimeKey(bearer); err == nil {
			t.Fatal("archive reconstruction restored denied Key authority")
		}
	}
	if svc.runtime.auth.Load() != auth {
		t.Fatal("historical reconstruction replaced current authorization")
	}
}
