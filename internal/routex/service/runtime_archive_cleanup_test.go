package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestRuntimeArchiveCleanupReviewBindsHiddenFacts(t *testing.T) {
	header := archiveMetadataHeaderFixture(t, 1)
	review := runtimeArchiveCleanupReview{ActorID: configurationTestIDV1("usr", 1), ActorBirth: header.CreatedAt, Ledger: entity.RuntimeArchiveRetention{ID: 1, Initialized: true, RetainedCount: 2, EncryptedBytes: 999}, Headers: []entity.RuntimeConfigurationArchive{header}, Chunks: []database.RuntimeArchiveChunkSummary{{ArchiveID: header.ID, ChunkCount: int64(header.ChunkCount), EncryptedBytes: 700}}, Targets: []entity.RuntimeArchiveContent{{ContentDigest: header.ContentDigest, ArchiveID: header.ID}}}
	original := runtimeArchiveCleanupETag(review)
	for name, mutate := range map[string]func(*runtimeArchiveCleanupReview){
		"actor":           func(r *runtimeArchiveCleanupReview) { r.ActorID = configurationTestIDV1("usr", 2) },
		"actor_birth":     func(r *runtimeArchiveCleanupReview) { r.ActorBirth = r.ActorBirth.Add(time.Microsecond) },
		"retained_count":  func(r *runtimeArchiveCleanupReview) { r.Ledger.RetainedCount++ },
		"encrypted_bytes": func(r *runtimeArchiveCleanupReview) { r.Ledger.EncryptedBytes++ },
		"header_birth": func(r *runtimeArchiveCleanupReview) {
			r.Headers[0].CreatedAt = r.Headers[0].CreatedAt.Add(time.Microsecond)
		},
		"source_digest":    func(r *runtimeArchiveCleanupReview) { r.Headers[0].SourceDigest = strings.Repeat("f", 64) },
		"chunk_bytes":      func(r *runtimeArchiveCleanupReview) { r.Chunks[0].EncryptedBytes++ },
		"canonical_target": func(r *runtimeArchiveCleanupReview) { r.Targets[0].ArchiveID = configurationTestIDV1("rcv", 2) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := review
			changed.Headers = slices.Clone(review.Headers)
			changed.Chunks = slices.Clone(review.Chunks)
			changed.Targets = slices.Clone(review.Targets)
			mutate(&changed)
			if runtimeArchiveCleanupETag(changed) == original {
				t.Fatal("obsolete review admitted")
			}
		})
	}
}

func TestRuntimeArchiveCleanupSelectionAndReceipt(t *testing.T) {
	a, b := configurationTestIDV1("rcv", 1), configurationTestIDV1("rcv", 2)
	input := []string{b, a}
	selected, err := runtimeArchiveCleanupSelection(input)
	if err != nil || !slices.Equal(selected, []string{a, b}) || input[0] != b {
		t.Fatal("selection changed caller or lost canonical order", err)
	}
	for _, invalid := range [][]string{nil, {a, a}, {strings.ToUpper(a)}, {a + " "}, slices.Repeat([]string{a}, 21)} {
		if _, err := runtimeArchiveCleanupSelection(invalid); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("invalid selection admitted", err)
		}
	}
	birth := time.Now().UTC().Truncate(time.Microsecond)
	raw, _ := json.Marshal(selected)
	row := entity.RuntimeArchiveCleanup{ActorCreatedAt: birth, ArchiveIDsJSON: string(raw), DeletedCount: 2, DeletedBytes: 700, CompletedAt: birth}
	if receipt, err := runtimeArchiveCleanupReceipt(row, selected); err != nil || receipt.Scope != "archive_storage_only" || receipt.DeletedEncryptedBytes != 700 {
		t.Fatal(receipt, err)
	}
	row.DeletedCount = 1
	if _, err := runtimeArchiveCleanupReceipt(row, selected); err == nil {
		t.Fatal("partial cleanup receipt admitted")
	}
	row.DeletedCount = 2
	if _, err := runtimeArchiveCleanupReceipt(row, []string{b, a}); err == nil {
		t.Fatal("different intent receipt admitted")
	}
}

func TestRuntimeArchiveCleanupAuthenticatesCanonicalRebind(t *testing.T) {
	doc := configurationFixtureV1(t)
	_, digest := configurationEncodeV1(t, doc)
	facts := &runtimeConfigurationFactsV1{Document: doc}
	if err := runtimeArchiveCleanupSameContent(t.Context(), facts, facts, digest); err != nil {
		t.Fatal(err)
	}
	changed := configurationFixtureV1(t)
	changed.Source.Providers[0].Name = "Different retained facts"
	if err := runtimeArchiveCleanupSameContent(t.Context(), facts, &runtimeConfigurationFactsV1{Document: changed}, digest); err == nil {
		t.Fatal("header digest alone authorized canonical rebind")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runtimeArchiveCleanupSameContent(ctx, facts, facts, digest); err == nil {
		t.Fatal("cancelled rebind admitted")
	}
}
