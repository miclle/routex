package service

import (
	"context"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SystemJobRuntimePublication = "runtime_publication"
	SystemJobCallRecordDelivery = "call_record_delivery"
	SystemJobStorageCleanup     = "storage_cleanup"

	systemJobRunning   = "running"
	systemJobCompleted = "completed"
	systemJobFailed    = "failed"

	systemJobListLimit = 100
	systemJobRetention = 256
)

type SystemJobRecord struct {
	ID             string     `json:"id"`
	Code           string     `json:"code"`
	Status         string     `json:"status"`
	Progress       *int       `json:"progress"`
	ExecutorID     string     `json:"executor_id"`
	ItemsTotal     int        `json:"items_total"`
	ItemsCompleted int        `json:"items_completed"`
	DetailCode     string     `json:"detail_code"`
	StartedAt      time.Time  `json:"started_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

type SystemJobPage struct {
	Items      []SystemJobRecord `json:"items"`
	ObservedAt time.Time         `json:"observed_at"`
}

func (s *Service) ListSystemJobs(ctx context.Context, actorID string) (*SystemJobPage, error) {
	page := &SystemJobPage{Items: []SystemJobRecord{}, ObservedAt: time.Now().UTC()}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actorID, "system.read"); err != nil {
			return err
		}
		if err := reconcileSystemJobs(tx, page.ObservedAt); err != nil {
			return err
		}
		var rows []entity.SystemJob
		if err := tx.Order("updated_at DESC, id DESC").Limit(systemJobListLimit).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			page.Items = append(page.Items, systemJobRecord(row))
		}
		return nil
	})
	if err != nil {
		return nil, catalogError(err)
	}
	return page, nil
}

// reconcileSystemJobs turns abandoned runs into durable failures before they
// are returned. The instance row is locked while its server-owned lease is
// checked so a concurrent heartbeat cannot be mistaken for an expired worker.
func reconcileSystemJobs(tx *gorm.DB, observedAt time.Time) error {
	// Keyset batches keep lock sets bounded without allowing a fixed first page
	// of healthy work to starve abandoned jobs ordered later.
	afterID := ""
	for {
		var running []entity.SystemJob
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status = ?", systemJobRunning)
		if afterID != "" {
			query = query.Where("id > ?", afterID)
		}
		if err := query.Order("id").Limit(systemJobRetention).Find(&running).Error; err != nil {
			return err
		}
		if len(running) == 0 {
			break
		}
		afterID = running[len(running)-1].ID
		for _, job := range running {
			lost := false
			if job.ExecutorID == "" {
				// Production workers always have an instance generation. This timeout
				// only recovers isolated callers interrupted between begin and finish.
				lost = !job.StartedAt.After(observedAt.Add(-time.Minute))
			} else {
				var instance entity.SystemInstance
				err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&instance, "id = ?", job.ExecutorID).Error
				if err != nil && err != gorm.ErrRecordNotFound {
					return err
				}
				lost = err == gorm.ErrRecordNotFound || instance.RetiredAt != nil || instance.StoppedAt != nil || !instance.LeaseExpiresAt.After(observedAt)
			}
			if !lost {
				continue
			}
			progress := 0
			if job.ItemsTotal > 0 {
				progress = min(100, job.ItemsCompleted*100/job.ItemsTotal)
			}
			result := tx.Model(&entity.SystemJob{}).Where("id = ? AND status = ?", job.ID, systemJobRunning).Updates(map[string]any{
				"status":       systemJobFailed,
				"progress":     progress,
				"detail_code":  "executor_lost",
				"updated_at":   observedAt,
				"completed_at": observedAt,
			})
			if result.Error != nil {
				return result.Error
			}
		}
		if len(running) < systemJobRetention {
			break
		}
	}
	return pruneSystemJobs(tx)
}

func systemJobRecord(row entity.SystemJob) SystemJobRecord {
	return SystemJobRecord{
		ID:             row.ID,
		Code:           row.Code,
		Status:         row.Status,
		Progress:       row.Progress,
		ExecutorID:     row.ExecutorID,
		ItemsTotal:     row.ItemsTotal,
		ItemsCompleted: row.ItemsCompleted,
		DetailCode:     row.DetailCode,
		StartedAt:      row.StartedAt,
		UpdatedAt:      row.UpdatedAt,
		CompletedAt:    row.CompletedAt,
	}
}

// beginSystemJob is best-effort operational reporting. A reporting failure
// never changes the result of the job being observed.
func (s *Service) beginSystemJob(code string, itemsTotal int) string {
	if !validSystemJobCode(code) || itemsTotal < 0 {
		return ""
	}
	jobID, err := id.NewPrefixed("job")
	if err != nil {
		return ""
	}
	now := time.Now().UTC()
	row := entity.SystemJob{
		ID:         jobID,
		Code:       code,
		Status:     systemJobRunning,
		ExecutorID: s.CurrentSystemInstanceID(),
		ItemsTotal: itemsTotal,
		StartedAt:  now,
		UpdatedAt:  now,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.authDB(ctx).Create(&row).Error != nil {
		return ""
	}
	return jobID
}

func (s *Service) finishSystemJob(jobID, code, status, detailCode string, itemsCompleted, itemsTotal int) {
	if jobID == "" || !validSystemJobCode(code) || !validSystemJobOutcome(code, status, detailCode) {
		return
	}
	if itemsTotal < 0 {
		itemsTotal = 0
	}
	if itemsCompleted < 0 {
		itemsCompleted = 0
	}
	if itemsCompleted > itemsTotal {
		itemsCompleted = itemsTotal
	}
	progress := 100
	if status != systemJobCompleted {
		progress = 0
		if itemsTotal > 0 {
			progress = itemsCompleted * 100 / itemsTotal
		}
	}
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	db := s.authDB(ctx)
	result := db.Model(&entity.SystemJob{}).Where("id = ? AND code = ? AND status = ?", jobID, code, systemJobRunning).Updates(map[string]any{
		"status":          status,
		"progress":        progress,
		"items_total":     itemsTotal,
		"items_completed": itemsCompleted,
		"detail_code":     detailCode,
		"updated_at":      now,
		"completed_at":    now,
	})
	if result.Error == nil && result.RowsAffected == 1 {
		_ = pruneSystemJobs(db)
	}
}

func (s *Service) recordSystemJobOutcome(code, status, detailCode string, itemsCompleted, itemsTotal int) {
	jobID := s.beginSystemJob(code, itemsTotal)
	s.finishSystemJob(jobID, code, status, detailCode, itemsCompleted, itemsTotal)
}

func pruneSystemJobs(db *gorm.DB) error {
	for {
		var staleIDs []string
		err := db.Model(&entity.SystemJob{}).
			Where("status IN ?", []string{systemJobCompleted, systemJobFailed}).
			Order("updated_at DESC, id DESC").
			Offset(systemJobRetention).
			Limit(systemJobRetention).
			Pluck("id", &staleIDs).Error
		if err != nil || len(staleIDs) == 0 {
			return err
		}
		if err := db.Where("id IN ? AND status IN ?", staleIDs, []string{systemJobCompleted, systemJobFailed}).Delete(&entity.SystemJob{}).Error; err != nil {
			return err
		}
	}
}

func validSystemJobCode(code string) bool {
	switch code {
	case SystemJobRuntimePublication, SystemJobCallRecordDelivery, SystemJobStorageCleanup:
		return true
	default:
		return false
	}
}

func validSystemJobOutcome(code, status, detail string) bool {
	if status != systemJobCompleted && status != systemJobFailed {
		return false
	}
	switch code {
	case SystemJobRuntimePublication:
		return detail == "published" || detail == "database_unavailable" || detail == "invalid_configuration" || detail == "publication_failed" || detail == "executor_lost"
	case SystemJobCallRecordDelivery:
		return detail == "delivered" || detail == "canceled" || detail == "buffer_read_failed" || detail == "invalid_fact" || detail == "identity_mismatch" || detail == "persistence_failed" || detail == "acknowledge_failed" || detail == "executor_lost"
	case SystemJobStorageCleanup:
		return detail == "cleaned" || detail == "canceled" || detail == "claim_failed" || detail == "delete_failed" || detail == "state_update_failed" || detail == "executor_lost"
	default:
		return false
	}
}
