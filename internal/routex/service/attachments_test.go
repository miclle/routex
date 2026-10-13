package service

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/objectstore"
)

func TestPersonalAndProjectAttachmentDeadlineIgnoresCleanupSchedule(t *testing.T) {
	created := time.Date(2026, time.October, 13, 1, 2, 3, 4000, time.UTC)
	deadline := created.Add(attachmentReadyTTL)
	for _, kind := range []string{entity.StorageOwnerUser, entity.StorageOwnerProject} {
		t.Run(kind, func(t *testing.T) {
			for _, scheduled := range []time.Time{created.Add(-time.Hour), deadline, deadline.Add(30 * 24 * time.Hour)} {
				row := entity.StorageObject{OwnerKind: kind, Purpose: "attachment", State: "ready", CreatedAt: created, NextCleanupAt: scheduled}
				for _, test := range []struct {
					name     string
					now      time.Time
					readable bool
				}{
					{"before deadline", deadline.Add(-time.Nanosecond), true},
					{"at deadline", deadline, false},
					{"after deadline", deadline.Add(time.Nanosecond), false},
				} {
					if got := attachmentReadableAt(row, test.now); got != test.readable {
						t.Fatalf("%s with cleanup %s: readable=%t, want %t", test.name, scheduled, got, test.readable)
					}
				}
				row.State = "delete_pending"
				if attachmentReadableAt(row, created) {
					t.Fatal("pending deletion retained byte eligibility")
				}
				row.State, row.Purpose = "ready", "probe"
				if !attachmentReadableAt(row, deadline.Add(time.Hour)) {
					t.Fatal("attachment expiry changed unrelated probe behavior")
				}
			}
		})
	}
}

func TestStoredAttachmentMatchesImmutableMetadata(t *testing.T) {
	data := []byte("verified attachment bytes")
	sum := sha256.Sum256(data)
	row := entity.StorageObject{
		ID:        "obj_attachment",
		VersionID: "version-1",
		Size:      int64(len(data)),
		SHA256:    hex.EncodeToString(sum[:]),
	}
	object := objectstore.Object{
		Data:      data,
		VersionID: row.VersionID,
		OwnerID:   row.ID,
		Size:      row.Size,
	}
	if !storedAttachmentMatches(row, object) {
		t.Fatal("matching immutable metadata was rejected")
	}

	tests := []struct {
		name   string
		change func(*objectstore.Object)
	}{
		{"owner", func(object *objectstore.Object) { object.OwnerID = "obj_other" }},
		{"version", func(object *objectstore.Object) { object.VersionID = "version-2" }},
		{"reported size", func(object *objectstore.Object) { object.Size-- }},
		{"body size", func(object *objectstore.Object) { object.Data = object.Data[:len(object.Data)-1] }},
		{"digest", func(object *objectstore.Object) { object.Data[0] ^= 0xff }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := objectstore.Object{
				Data:      append([]byte(nil), object.Data...),
				VersionID: object.VersionID,
				OwnerID:   object.OwnerID,
				Size:      object.Size,
			}
			test.change(&candidate)
			if storedAttachmentMatches(row, candidate) {
				t.Fatal("mismatched immutable metadata was accepted")
			}
		})
	}
}
