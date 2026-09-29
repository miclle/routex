package service

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/objectstore"
)

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
