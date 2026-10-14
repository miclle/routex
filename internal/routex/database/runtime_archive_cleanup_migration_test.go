package database

import (
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestRuntimeArchiveCleanupV115FrozenReceipt(t *testing.T) {
	frozen, current := oidcV94Schema(t, &runtimeArchiveCleanupV115{}), oidcV94Schema(t, &entity.RuntimeArchiveCleanup{})
	if frozen.Table != "runtime_archive_cleanups" || frozen.Table != current.Table || len(frozen.DBNames) != len(current.DBNames) {
		t.Fatal("cleanup receipt schema drift")
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.Tag.Get("gorm") != b.Tag.Get("gorm") || a.Tag.Get("json") != "-" || b.Tag.Get("json") != "-" {
			t.Fatal("private frozen field drift", name)
		}
	}
	if len(frozen.Relationships.Relations) != 0 {
		t.Fatal("historical receipt cascades with mutable objects")
	}
	for _, name := range []string{"ck_runtime_archive_cleanup_identity", "ck_runtime_archive_cleanup_bounds", "ck_runtime_archive_cleanup_time", "ck_runtime_archive_cleanup_payload"} {
		if _, ok := frozen.ParseCheckConstraints()[name]; !ok {
			t.Fatal("missing durable cleanup bound", name)
		}
	}
}
