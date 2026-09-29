package service

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestSystemInstanceMetadataValidation(t *testing.T) {
	valid := SystemInstanceMetadata{
		Name: "gateway.example", Hostname: "gateway.example", Version: "v1.2.3",
		Commit: "abc123", BuildTime: "2026-09-29T12:00:00Z", GoVersion: runtime.Version(),
		OS: runtime.GOOS, Arch: runtime.GOARCH, StoragePath: filepath.Join(t.TempDir(), "calls.db"),
	}
	if err := validateSystemInstanceMetadata(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*SystemInstanceMetadata){
		"empty name":       func(value *SystemInstanceMetadata) { value.Name = "" },
		"long hostname":    func(value *SystemInstanceMetadata) { value.Hostname = strings.Repeat("h", 254) },
		"control commit":   func(value *SystemInstanceMetadata) { value.Commit = "safe\nunsafe" },
		"invalid path":     func(value *SystemInstanceMetadata) { value.StoragePath = "bad\x00path" },
		"missing go value": func(value *SystemInstanceMetadata) { value.GoVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			mutate(&invalid)
			if validateSystemInstanceMetadata(invalid) == nil {
				t.Fatal("invalid process metadata was accepted")
			}
		})
	}
}

func TestSystemInstanceRecordUsesLeaseAndServerCleanupThreshold(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	used, total := int64(25), int64(100)
	row := entity.SystemInstance{
		ID: "ins_01m36yee4gkbns18pfcqqc75a3", Name: "gateway", Hostname: "gateway.example",
		Role: systemInstanceRole, Version: "v1", GoVersion: "go1.27", OS: "linux", Arch: "amd64",
		HeartbeatRevision: 4, StartedAt: now.Add(-time.Hour), LastHeartbeatAt: now.Add(-time.Minute),
		LeaseExpiresAt: now.Add(time.Minute), MemoryUsed: &used, MemoryTotal: &total, MemoryScope: "process",
	}
	record := systemInstanceRecord(row, now, 5*time.Minute, "")
	if record.Status != "online" || record.CleanupEligible || record.Role != "combined" {
		t.Fatalf("fresh lease derived incorrectly: %+v", record)
	}
	if record.Resources.Memory == nil || record.Resources.Memory.Percent == nil || *record.Resources.Memory.Percent != 25 {
		t.Fatalf("memory resource = %+v", record.Resources.Memory)
	}

	row.LeaseExpiresAt = now.Add(-time.Second)
	record = systemInstanceRecord(row, now, 5*time.Minute, "")
	if record.Status != "offline" || record.CleanupEligible {
		t.Fatal("recently expired registration must be offline but not cleanup eligible")
	}
	row.LastHeartbeatAt = now.Add(-6 * time.Minute)
	record = systemInstanceRecord(row, now, 5*time.Minute, row.ID)
	if record.CleanupEligible {
		t.Fatal("serving instance must never be cleanup eligible")
	}
	record = systemInstanceRecord(row, now, 5*time.Minute, "ins_01m36yee4gkbns18pfcqqc75a4")
	if !record.CleanupEligible {
		t.Fatal("old expired registration should be cleanup eligible")
	}
}

func TestSystemResourceSamplesRemainTruthfulWhenIncomplete(t *testing.T) {
	used, total, excessive := int64(1), int64(4), int64(5)
	complete := resourceSample("journal_filesystem", "bytes", &used, &total)
	if complete == nil || complete.Percent == nil || *complete.Percent != 25 {
		t.Fatalf("complete sample = %+v", complete)
	}
	partial := resourceSample("go_runtime", "bytes", &used, nil)
	if partial == nil || partial.Percent != nil || partial.Total != nil {
		t.Fatalf("partial sample manufactured capacity: %+v", partial)
	}
	invalid := resourceSample("process", "bytes", &excessive, &total)
	if invalid == nil || invalid.Percent != nil {
		t.Fatalf("invalid bounds manufactured percentage: %+v", invalid)
	}
	if resourceSample("", "bytes", nil, nil) != nil {
		t.Fatal("missing measurement must remain null")
	}
}

func TestSystemInstanceCleanupTargetValidation(t *testing.T) {
	valid := SystemInstanceCleanupTarget{ID: "ins_01m36yee4gkbns18pfcqqc75a3", Revision: 1}
	if err := validateSystemInstanceCleanupTargets([]SystemInstanceCleanupTarget{valid}); err != nil {
		t.Fatal(err)
	}
	for _, targets := range [][]SystemInstanceCleanupTarget{
		nil,
		{{ID: "bad", Revision: 1}},
		{{ID: valid.ID, Revision: 0}},
		{valid, valid},
	} {
		if validateSystemInstanceCleanupTargets(targets) == nil {
			t.Fatalf("invalid targets accepted: %+v", targets)
		}
	}
}

func TestSystemInstanceResourceCollectionKeepsUnknownCPUUsage(t *testing.T) {
	resources := collectSystemInstanceResources(filepath.Join(t.TempDir(), "calls.db"))
	if resources.CPUUsed != nil || resources.CPUTotal == nil || *resources.CPUTotal <= 0 {
		t.Fatalf("CPU resource fabricated usage or omitted capacity: %+v", resources)
	}
	if resources.MemoryUsed == nil || *resources.MemoryUsed <= 0 || resources.MemoryTotal != nil {
		t.Fatalf("memory resource scope is not truthful: %+v", resources)
	}
	if runtime.GOOS != "windows" && (resources.StorageUsed == nil || resources.StorageTotal == nil || *resources.StorageTotal <= 0) {
		t.Fatalf("journal filesystem resource missing: %+v", resources)
	}
}
