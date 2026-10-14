package service

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
)

func archiveFixture(t *testing.T) (entity.RuntimeConfigurationArchive, []entity.RuntimeConfigurationArchiveChunk, *secretstore.Store, []byte) {
	t.Helper()
	doc := configurationFixtureV1(t)
	doc.Source.Providers[0].Name = strings.Repeat("界", 14000)
	raw, digest := configurationEncodeV1(t, doc)
	identity, birth, err := newRuntimeArchiveIdentity()
	if err != nil {
		t.Fatal(err)
	}
	header := runtimeArchiveHeaderV1(identity, birth, doc, len(raw), digest)
	store, err := secretstore.NewKeyring(map[string][]byte{"first": bytes.Repeat([]byte{17}, 32), "next": bytes.Repeat([]byte{23}, 32)}, "first", "first")
	if err != nil {
		t.Fatal(err)
	}
	chunks := []entity.RuntimeConfigurationArchiveChunk{}
	for ordinal, start := 0, 0; start < len(raw); ordinal, start = ordinal+1, start+runtimeArchiveChunkBytes {
		end := min(start+runtimeArchiveChunkBytes, len(raw))
		chunk := entity.RuntimeConfigurationArchiveChunk{ID: header.ID + "-" + archiveOrdinal(ordinal), ArchiveID: header.ID, Ordinal: ordinal, PlaintextBytes: end - start, PlaintextDigest: runtimeArchiveByteDigest(raw[start:end])}
		chunk.SecretGeneration = runtimeArchiveChunkGeneration(header, chunk)
		reference, e := runtimeArchiveChunkReference(header, chunk)
		if e != nil {
			t.Fatal(e)
		}
		chunk.AuthCiphertext, e = store.Seal(reference, string(raw[start:end]))
		if e != nil {
			t.Fatal(e)
		}
		chunks = append(chunks, chunk)
	}
	return header, chunks, store, raw
}

func archiveOrdinal(n int) string {
	return string([]byte{'0' + byte(n/100), '0' + byte(n/10%10), '0' + byte(n%10)})
}

func TestRuntimeArchiveColdFactsAndRootRewrap(t *testing.T) {
	header, chunks, store, raw := archiveFixture(t)
	if len(chunks) < 2 {
		t.Fatal("fixture must cross chunk and UTF8 boundaries")
	}
	facts, err := reconstructRuntimeArchiveChunksV1(t.Context(), header, chunks, store.Open)
	if err != nil || facts == nil {
		t.Fatal("cold source", err)
	}
	encoded, digest := configurationEncodeV1(t, facts.Document)
	if !bytes.Equal(encoded, raw) || digest != header.ContentDigest {
		t.Fatal("exact cold bytes")
	}
	original := facts.Document
	for i := range chunks {
		ref, err := runtimeArchiveChunkReference(header, chunks[i])
		if err != nil {
			t.Fatal(err)
		}
		chunks[i].AuthCiphertext, err = store.Rewrap(ref, chunks[i].AuthCiphertext, "next")
		if err != nil {
			t.Fatal(err)
		}
	}
	next, err := secretstore.NewKeyring(map[string][]byte{"next": bytes.Repeat([]byte{23}, 32)}, "next", "next")
	if err != nil {
		t.Fatal(err)
	}
	facts, err = reconstructRuntimeArchiveChunksV1(t.Context(), header, chunks, next.Open)
	if err != nil || !reflect.DeepEqual(original, facts.Document) {
		t.Fatal("archive unreadable after old root retirement", err)
	}
	for _, value := range []any{header, chunks[0]} {
		public, err := json.Marshal(value)
		if err != nil || string(public) != "{}" {
			t.Fatal("private archive entity serialization", string(public), err)
		}
	}
}

func TestRuntimeArchiveHeaderAndChunkAAD(t *testing.T) {
	header, chunks, store, _ := archiveFixture(t)
	for _, tc := range []struct {
		name   string
		change func(*entity.RuntimeConfigurationArchive)
	}{
		{"id", func(h *entity.RuntimeConfigurationArchive) { h.ID = "rcv_01k00000000000000000000000" }},
		{"birth", func(h *entity.RuntimeConfigurationArchive) { h.CreatedAt = h.CreatedAt.Add(time.Microsecond) }},
		{"content", func(h *entity.RuntimeConfigurationArchive) { h.ContentDigest = strings.Repeat("f", 64) }},
		{"source", func(h *entity.RuntimeConfigurationArchive) { h.SourceDigest = strings.Repeat("f", 64) }},
		{"authorization", func(h *entity.RuntimeConfigurationArchive) { h.AuthorizationDigest = strings.Repeat("f", 64) }},
		{"network", func(h *entity.RuntimeConfigurationArchive) { h.NetworkDigest = strings.Repeat("f", 64) }},
		{"pricing", func(h *entity.RuntimeConfigurationArchive) { h.PricingDigest = strings.Repeat("f", 64) }},
		{"length", func(h *entity.RuntimeConfigurationArchive) { h.PlaintextBytes-- }},
		{"codec", func(h *entity.RuntimeConfigurationArchive) { h.CodecVersion++ }},
		{"count", func(h *entity.RuntimeConfigurationArchive) { h.ChunkCount++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := header
			tc.change(&h)
			if facts, err := reconstructRuntimeArchiveChunksV1(t.Context(), h, chunks, store.Open); err == nil || facts != nil {
				t.Fatal("header substitution admitted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*entity.RuntimeConfigurationArchiveChunk)
	}{
		{"id", func(c *entity.RuntimeConfigurationArchiveChunk) { c.ID += "x" }},
		{"owner", func(c *entity.RuntimeConfigurationArchiveChunk) { c.ArchiveID += "x" }},
		{"ordinal", func(c *entity.RuntimeConfigurationArchiveChunk) { c.Ordinal++ }},
		{"length", func(c *entity.RuntimeConfigurationArchiveChunk) { c.PlaintextBytes-- }},
		{"digest", func(c *entity.RuntimeConfigurationArchiveChunk) { c.PlaintextDigest = strings.Repeat("f", 64) }},
		{"generation", func(c *entity.RuntimeConfigurationArchiveChunk) { c.SecretGeneration = strings.Repeat("f", 64) }},
		{"ciphertext", func(c *entity.RuntimeConfigurationArchiveChunk) { c.AuthCiphertext = chunks[1].AuthCiphertext }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := append([]entity.RuntimeConfigurationArchiveChunk(nil), chunks...)
			tc.change(&copy[0])
			if facts, err := reconstructRuntimeArchiveChunksV1(t.Context(), header, copy, store.Open); err == nil || facts != nil {
				t.Fatal("chunk substitution admitted")
			}
		})
	}
}

func TestRuntimeArchiveBoundsOrderAndSanitizedFailure(t *testing.T) {
	header, chunks, store, _ := archiveFixture(t)
	for _, tc := range []struct {
		name string
		rows []entity.RuntimeConfigurationArchiveChunk
	}{
		{"missing", chunks[:len(chunks)-1]},
		{"extra", append(append([]entity.RuntimeConfigurationArchiveChunk(nil), chunks...), chunks[0])},
		{"duplicate", append([]entity.RuntimeConfigurationArchiveChunk{chunks[0]}, chunks[:len(chunks)-1]...)},
		{"reordered", append([]entity.RuntimeConfigurationArchiveChunk{chunks[1], chunks[0]}, chunks[2:]...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if facts, err := reconstructRuntimeArchiveChunksV1(t.Context(), header, tc.rows, store.Open); facts != nil || err != errRuntimeArchive {
				t.Fatal("incomplete source or leaked diagnostics")
			}
		})
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if facts, err := reconstructRuntimeArchiveChunksV1(cancelled, header, chunks, store.Open); err != errRuntimeArchive || facts != nil {
		t.Fatal("cancelled source returned")
	}
	var missing context.Context
	if facts, err := reconstructRuntimeArchiveChunksV1(missing, header, chunks, store.Open); err != errRuntimeArchive || facts != nil {
		t.Fatal("nil source returned")
	}
	for _, length := range []int{0, -1, runtimeConfigurationMaxBytesV1 + 1} {
		h := header
		h.PlaintextBytes = length
		if runtimeArchiveHeaderValid(h) {
			t.Fatal("byte overflow admitted")
		}
	}
	for _, birth := range []time.Time{{}, header.CreatedAt.Add(time.Nanosecond)} {
		h := header
		h.CreatedAt = birth
		if runtimeArchiveHeaderValid(h) {
			t.Fatal("ambiguous persisted birth")
		}
	}
	h := header
	h.ContentDigest = strings.ToUpper(h.ContentDigest)
	if runtimeArchiveHeaderValid(h) {
		t.Fatal("noncanonical digest")
	}
}

func TestRuntimeArchiveRootV11KeepsHistoricalPrefixes(t *testing.T) {
	if rootInventoryVersion != 11 || len(rootDomains) != 13 || rootDomains[12] != "runtime_config_chunks" {
		t.Fatal("bounded new domain")
	}
	for version, n := range map[int]int{1: 5, 2: 7, 3: 8, 4: 9, 5: 10, 6: 11, 7: 11, 8: 11, 9: 12, 10: 12, 11: 13} {
		if !reflect.DeepEqual(rootInventoryDomains(version), rootDomains[:n]) {
			t.Fatal("historical coverage changed", version)
		}
	}
	spec, err := rootSpec("runtime_config_chunks")
	if err != nil || spec.table != "runtime_configuration_archive_chunks" || spec.id != "id" || spec.generation != "secret_generation" || spec.ciphertext != "auth_ciphertext" {
		t.Fatal("dedicated root inventory")
	}
}
