package service

import (
	"errors"
	"math"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestRuntimeArchiveRetentionExactBothCapBoundaries(t *testing.T) {
	base := entity.RuntimeArchiveRetention{ID: 1, Initialized: true, RetainedCount: 99, EncryptedBytes: runtimeArchiveRetainedEncryptedBytes - 17}
	for _, tc := range []struct {
		name  string
		row   entity.RuntimeArchiveRetention
		bytes int64
		want  error
	}{
		{"exact_last_version_and_byte", base, 17, nil},
		{"one_extra_byte", base, 18, errRuntimeArchiveByteCap},
		{"zero_addition", base, 0, errRuntimeArchive},
		{"negative_addition", base, -1, errRuntimeArchive},
		{"unknown_ledger", entity.RuntimeArchiveRetention{ID: 1}, 1, errRuntimeArchive},
		{"version_cap", entity.RuntimeArchiveRetention{ID: 1, Initialized: true, RetainedCount: 100}, 1, errRuntimeArchiveVersionCap},
		{"legacy_over_version_cap", entity.RuntimeArchiveRetention{ID: 1, Initialized: true, RetainedCount: 101}, 1, errRuntimeArchiveVersionCap},
		{"byte_cap", entity.RuntimeArchiveRetention{ID: 1, Initialized: true, EncryptedBytes: runtimeArchiveRetainedEncryptedBytes}, 1, errRuntimeArchiveByteCap},
		{"legacy_over_byte_cap", entity.RuntimeArchiveRetention{ID: 1, Initialized: true, EncryptedBytes: math.MaxInt64}, 1, errRuntimeArchiveByteCap},
		{"negative_stored_count", entity.RuntimeArchiveRetention{ID: 1, Initialized: true, RetainedCount: -1}, 1, errRuntimeArchive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeArchiveRetentionAdmission(tc.row, tc.bytes); !errors.Is(got, tc.want) {
				t.Fatal(got, tc.want)
			}
		})
	}
}
func TestRuntimeArchiveRetentionRewrapAccountsActualBytesWithoutBlockingRotation(t *testing.T) {
	for _, tc := range []struct {
		oldTotal, oldBytes, newBytes, want int64
		valid                              bool
	}{
		{100, 30, 31, 101, true}, {100, 30, 29, 99, true},
		{runtimeArchiveRetainedEncryptedBytes, 30, 31, runtimeArchiveRetainedEncryptedBytes + 1, true},
		{10, 11, 11, 0, false}, {100, 0, 30, 0, false}, {math.MaxInt64, 1, 2, 0, false},
	} {
		got, err := runtimeArchiveRetentionRewrapBytes(tc.oldTotal, tc.oldBytes, tc.newBytes)
		if (err == nil) != tc.valid || got != tc.want {
			t.Fatal(got, err, tc)
		}
	}
}
