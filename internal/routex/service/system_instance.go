package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

const systemInstanceRole = "combined"

var (
	systemInstanceIDPattern = regexp.MustCompile(`^ins_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	systemInstanceConflict  = &apperrors.Error{Code: http.StatusConflict, Message: "system instance state changed"}
)

type systemInstanceLease struct {
	id          string
	token       string
	storagePath string
	cancel      context.CancelFunc
	done        chan struct{}
	stopOnce    sync.Once
}

// SystemInstanceMetadata contains bounded non-secret process facts captured at
// registration. Role is deliberately omitted because RouteX is one monolith.
type SystemInstanceMetadata struct {
	Name, Hostname, Version, Commit, BuildTime, GoVersion, OS, Arch string
	StoragePath                                                     string
}

// RuntimeSystemInstanceMetadata reads stable process facts without exposing
// paths in the persisted registration or API.
func RuntimeSystemInstanceMetadata(commit, buildTime, storagePath string) (SystemInstanceMetadata, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return SystemInstanceMetadata{}, fmt.Errorf("read hostname: %w", err)
	}
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return SystemInstanceMetadata{
		Name: hostname, Hostname: hostname, Version: version, Commit: commit,
		BuildTime: buildTime, GoVersion: runtime.Version(), OS: runtime.GOOS,
		Arch: runtime.GOARCH, StoragePath: storagePath,
	}, nil
}

type entitySystemInstanceResources struct {
	CPUUsed, CPUTotal                   *int64
	MemoryUsed, MemoryTotal             *int64
	StorageUsed, StorageTotal           *int64
	CPUScope, MemoryScope, StorageScope string
}

type SystemResourceSample struct {
	Scope   string   `json:"scope"`
	Used    *int64   `json:"used"`
	Total   *int64   `json:"total"`
	Unit    string   `json:"unit"`
	Percent *float64 `json:"percent"`
}

type SystemInstanceResources struct {
	CPU     *SystemResourceSample `json:"cpu"`
	Memory  *SystemResourceSample `json:"memory"`
	Storage *SystemResourceSample `json:"storage"`
}

type SystemInstanceRecord struct {
	ID                string                  `json:"id"`
	Name              string                  `json:"name"`
	Hostname          string                  `json:"hostname"`
	Status            string                  `json:"status"`
	CleanupEligible   bool                    `json:"cleanup_eligible"`
	Role              string                  `json:"role"`
	Version           string                  `json:"version"`
	Commit            string                  `json:"commit"`
	BuildTime         string                  `json:"build_time"`
	GoVersion         string                  `json:"go_version"`
	OS                string                  `json:"os"`
	Arch              string                  `json:"arch"`
	StartedAt         time.Time               `json:"started_at"`
	LastHeartbeatAt   time.Time               `json:"last_heartbeat_at"`
	HeartbeatRevision uint64                  `json:"heartbeat_revision"`
	StoppedAt         *time.Time              `json:"stopped_at"`
	Resources         SystemInstanceResources `json:"resources"`
}

type SystemInstancePage struct {
	Items                    []SystemInstanceRecord `json:"items"`
	ObservedAt               time.Time              `json:"observed_at"`
	HeartbeatIntervalSeconds int64                  `json:"heartbeat_interval_seconds"`
	LeaseDurationSeconds     int64                  `json:"lease_duration_seconds"`
	CleanupAfterSeconds      int64                  `json:"cleanup_after_seconds"`
}

type SystemInstanceCleanupTarget struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}

type SystemInstanceCleanupResult struct {
	CleanedIDs   []string `json:"cleaned_ids"`
	CleanedCount int      `json:"cleaned_count"`
}

func validSystemInstanceText(value string, maximum int, required bool) bool {
	if required && value == "" {
		return false
	}
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum && !strings.ContainsFunc(value, unicode.IsControl)
}

func validateSystemInstanceMetadata(metadata SystemInstanceMetadata) error {
	fields := []struct {
		value    string
		maximum  int
		required bool
	}{
		{metadata.Name, 100, true}, {metadata.Hostname, 253, true},
		{metadata.Version, 100, true}, {metadata.Commit, 64, false},
		{metadata.BuildTime, 64, false}, {metadata.GoVersion, 40, true},
		{metadata.OS, 32, true}, {metadata.Arch, 32, true},
	}
	for _, field := range fields {
		if !validSystemInstanceText(field.value, field.maximum, field.required) {
			return fmt.Errorf("invalid system instance metadata")
		}
	}
	if strings.ContainsRune(metadata.StoragePath, 0) {
		return fmt.Errorf("invalid system instance storage path")
	}
	return nil
}

// StartSystemInstance registers a fresh process generation and starts its
// renewable lease. It must run before any background worker accepts work.
func (s *Service) StartSystemInstance(ctx context.Context, metadata SystemInstanceMetadata) error {
	if err := validateSystemInstanceMetadata(metadata); err != nil {
		return err
	}
	s.instanceMu.Lock()
	defer s.instanceMu.Unlock()
	if s.instance != nil {
		return fmt.Errorf("system instance already started")
	}
	instanceID, err := id.NewPrefixed("ins")
	if err != nil {
		return err
	}
	leaseToken, err := id.NewPrefixed("lck")
	if err != nil {
		return err
	}
	now := s.instanceNow().UTC()
	resources := s.instanceResources(metadata.StoragePath)
	row := entity.SystemInstance{
		ID: instanceID, LeaseToken: leaseToken, HeartbeatRevision: 1,
		Name: metadata.Name, Hostname: metadata.Hostname, Role: systemInstanceRole,
		Version: metadata.Version, Commit: metadata.Commit, BuildTime: metadata.BuildTime,
		GoVersion: metadata.GoVersion, OS: metadata.OS, Arch: metadata.Arch,
		StartedAt: now, LastHeartbeatAt: now, LeaseExpiresAt: now.Add(s.instanceLeaseDuration),
	}
	applySystemInstanceResources(&row, resources)
	if err := s.authDB(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("register system instance: %w", err)
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	lease := &systemInstanceLease{id: instanceID, token: leaseToken, storagePath: metadata.StoragePath, cancel: cancel, done: make(chan struct{})}
	s.instance = lease
	go s.runSystemInstanceHeartbeat(run, lease)
	return nil
}

func (s *Service) runSystemInstanceHeartbeat(ctx context.Context, lease *systemInstanceLease) {
	defer close(lease.done)
	ticker := time.NewTicker(s.instanceHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			persist, cancel := context.WithTimeout(context.Background(), min(5*time.Second, s.instanceHeartbeat))
			err := s.heartbeatSystemInstance(persist, lease)
			cancel()
			if err != nil {
				log.Printf("system instance heartbeat failed: %v", err)
			}
		}
	}
}

func (s *Service) heartbeatSystemInstance(ctx context.Context, lease *systemInstanceLease) error {
	now := s.instanceNow().UTC()
	resources := s.instanceResources(lease.storagePath)
	updates := systemInstanceResourceUpdates(resources)
	updates["last_heartbeat_at"] = now
	updates["lease_expires_at"] = now.Add(s.instanceLeaseDuration)
	updates["heartbeat_revision"] = gorm.Expr("heartbeat_revision + ?", 1)
	updates["retired_at"] = nil
	updates["retired_by"] = ""
	result := s.authDB(ctx).Model(&entity.SystemInstance{}).
		Where("id = ? AND lease_token = ? AND stopped_at IS NULL", lease.id, lease.token).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("system instance lease was replaced")
	}
	return nil
}

// RefreshSystemInstance renews the current process lease immediately. Periodic
// heartbeats use the same token-checked path; callers may use this at a
// readiness boundary where waiting for the next interval would be misleading.
func (s *Service) RefreshSystemInstance(ctx context.Context) error {
	s.instanceMu.RLock()
	lease := s.instance
	s.instanceMu.RUnlock()
	if lease == nil {
		return fmt.Errorf("system instance is not started")
	}
	return s.heartbeatSystemInstance(ctx, lease)
}

// StopSystemInstance joins the heartbeat before recording a graceful stop.
func (s *Service) StopSystemInstance(ctx context.Context) error {
	s.instanceMu.RLock()
	lease := s.instance
	s.instanceMu.RUnlock()
	if lease == nil {
		return nil
	}
	lease.stopOnce.Do(lease.cancel)
	select {
	case <-lease.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	now := s.instanceNow().UTC()
	updates := systemInstanceResourceUpdates(s.instanceResources(lease.storagePath))
	updates["last_heartbeat_at"] = now
	updates["lease_expires_at"] = now
	updates["stopped_at"] = now
	updates["heartbeat_revision"] = gorm.Expr("heartbeat_revision + ?", 1)
	updates["retired_at"] = nil
	updates["retired_by"] = ""
	result := s.authDB(ctx).Model(&entity.SystemInstance{}).
		Where("id = ? AND lease_token = ? AND stopped_at IS NULL", lease.id, lease.token).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("system instance lease was replaced")
	}
	s.instanceMu.Lock()
	if s.instance == lease {
		s.instance = nil
	}
	s.instanceMu.Unlock()
	return nil
}

func applySystemInstanceResources(row *entity.SystemInstance, resources entitySystemInstanceResources) {
	row.CPUUsed, row.CPUTotal, row.CPUScope = resources.CPUUsed, resources.CPUTotal, resources.CPUScope
	row.MemoryUsed, row.MemoryTotal, row.MemoryScope = resources.MemoryUsed, resources.MemoryTotal, resources.MemoryScope
	row.StorageUsed, row.StorageTotal, row.StorageScope = resources.StorageUsed, resources.StorageTotal, resources.StorageScope
}

func systemInstanceResourceUpdates(resources entitySystemInstanceResources) map[string]any {
	return map[string]any{
		"cpu_used": resources.CPUUsed, "cpu_total": resources.CPUTotal, "cpu_scope": resources.CPUScope,
		"memory_used": resources.MemoryUsed, "memory_total": resources.MemoryTotal, "memory_scope": resources.MemoryScope,
		"storage_used": resources.StorageUsed, "storage_total": resources.StorageTotal, "storage_scope": resources.StorageScope,
	}
}

// CurrentSystemInstanceID returns the registered process generation for job
// attribution. Empty means lifecycle registration has not started.
func (s *Service) CurrentSystemInstanceID() string {
	s.instanceMu.RLock()
	defer s.instanceMu.RUnlock()
	if s.instance == nil {
		return ""
	}
	return s.instance.id
}

func resourceSample(scope, unit string, used, total *int64) *SystemResourceSample {
	if used == nil && total == nil {
		return nil
	}
	result := &SystemResourceSample{Scope: scope, Used: used, Total: total, Unit: unit}
	if used != nil && total != nil && *total > 0 && *used >= 0 && *used <= *total {
		percent := math.Round(float64(*used)*1000/float64(*total)) / 10
		result.Percent = &percent
	}
	return result
}

func systemInstanceRecord(row entity.SystemInstance, now time.Time, cleanupAfter time.Duration, currentID string) SystemInstanceRecord {
	online := row.StoppedAt == nil && row.LeaseExpiresAt.After(now)
	status := "offline"
	if online {
		status = "online"
	}
	return SystemInstanceRecord{
		ID: row.ID, Name: row.Name, Hostname: row.Hostname, Status: status,
		CleanupEligible: !online && row.ID != currentID && row.RetiredAt == nil && !row.LastHeartbeatAt.After(now.Add(-cleanupAfter)),
		Role:            row.Role, Version: row.Version, Commit: row.Commit, BuildTime: row.BuildTime,
		GoVersion: row.GoVersion, OS: row.OS, Arch: row.Arch, StartedAt: row.StartedAt,
		LastHeartbeatAt: row.LastHeartbeatAt, HeartbeatRevision: row.HeartbeatRevision, StoppedAt: row.StoppedAt,
		Resources: SystemInstanceResources{
			CPU:     resourceSample(row.CPUScope, "cores", row.CPUUsed, row.CPUTotal),
			Memory:  resourceSample(row.MemoryScope, "bytes", row.MemoryUsed, row.MemoryTotal),
			Storage: resourceSample(row.StorageScope, "bytes", row.StorageUsed, row.StorageTotal),
		},
	}
}

func (s *Service) ListSystemInstances(ctx context.Context, actorID string) (*SystemInstancePage, error) {
	now := s.instanceNow().UTC()
	result := &SystemInstancePage{
		Items: []SystemInstanceRecord{}, ObservedAt: now,
		HeartbeatIntervalSeconds: int64(s.instanceHeartbeat / time.Second),
		LeaseDurationSeconds:     int64(s.instanceLeaseDuration / time.Second),
		CleanupAfterSeconds:      int64(s.instanceCleanupAfter / time.Second),
	}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "system.read"); err != nil {
			return err
		}
		var rows []entity.SystemInstance
		if err := tx.Where("retired_at IS NULL").Order("last_heartbeat_at DESC, id DESC").Limit(200).Find(&rows).Error; err != nil {
			return err
		}
		currentID := s.CurrentSystemInstanceID()
		for _, row := range rows {
			result.Items = append(result.Items, systemInstanceRecord(row, now, s.instanceCleanupAfter, currentID))
		}
		return nil
	})
	return result, catalogError(err)
}

func validateSystemInstanceCleanupTargets(targets []SystemInstanceCleanupTarget) error {
	if len(targets) == 0 || len(targets) > 100 {
		return apperrors.ErrBadRequest
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if !systemInstanceIDPattern.MatchString(target.ID) || target.Revision == 0 || seen[target.ID] {
			return apperrors.ErrBadRequest
		}
		seen[target.ID] = true
	}
	return nil
}

func appendSystemInstanceCleanupAudit(tx *gorm.DB, actorID string, target SystemInstanceCleanupTarget) error {
	details, err := json.Marshal(struct {
		Revision uint64 `json:"revision"`
	}{target.Revision})
	if err != nil {
		return err
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw := string(details)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "system.instance.cleanup", ResourceType: "system_instance", ResourceID: target.ID, DetailsJSON: &raw}).Error
}

func (s *Service) CleanupOfflineSystemInstances(ctx context.Context, actorID string, targets []SystemInstanceCleanupTarget) (*SystemInstanceCleanupResult, error) {
	if err := validateSystemInstanceCleanupTargets(targets); err != nil {
		return nil, err
	}
	result := &SystemInstanceCleanupResult{CleanedIDs: make([]string, 0, len(targets))}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "system.write"); err != nil {
			return err
		}
		ids := make([]string, 0, len(targets))
		for _, target := range targets {
			ids = append(ids, target.ID)
		}
		slices.Sort(ids)
		var rows []entity.SystemInstance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(targets) {
			return systemInstanceConflict
		}
		byID := make(map[string]entity.SystemInstance, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		now := s.instanceNow().UTC()
		currentID := s.CurrentSystemInstanceID()
		for _, target := range targets {
			row := byID[target.ID]
			online := row.StoppedAt == nil && row.LeaseExpiresAt.After(now)
			if row.ID == currentID || row.RetiredAt != nil || row.HeartbeatRevision != target.Revision || online || row.LastHeartbeatAt.After(now.Add(-s.instanceCleanupAfter)) {
				return systemInstanceConflict
			}
		}
		for _, target := range targets {
			update := tx.Model(&entity.SystemInstance{}).
				Where("id = ? AND heartbeat_revision = ? AND retired_at IS NULL", target.ID, target.Revision).
				Updates(map[string]any{"retired_at": now, "retired_by": actorID, "heartbeat_revision": gorm.Expr("heartbeat_revision + ?", 1)})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return systemInstanceConflict
			}
			if err := appendSystemInstanceCleanupAudit(tx, actorID, target); err != nil {
				return err
			}
			result.CleanedIDs = append(result.CleanedIDs, target.ID)
		}
		result.CleanedCount = len(result.CleanedIDs)
		return nil
	})
	if err != nil {
		result.CleanedIDs = result.CleanedIDs[:0]
		result.CleanedCount = 0
		return nil, catalogError(err)
	}
	return result, nil
}

func collectSystemInstanceResources(storagePath string) entitySystemInstanceResources {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	memoryUsed := int64(memory.Sys)
	cpuTotal := int64(runtime.NumCPU())
	result := entitySystemInstanceResources{
		CPUTotal: &cpuTotal, CPUScope: "host_logical_cores",
		MemoryUsed: &memoryUsed, MemoryScope: "go_runtime",
	}
	if used, total, ok := systemInstanceFilesystemUsage(storagePath); ok {
		result.StorageUsed, result.StorageTotal = &used, &total
		result.StorageScope = "journal_filesystem"
	}
	return result
}
