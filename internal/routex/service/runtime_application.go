package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const runtimeApplicationWriteBudget = 250 * time.Millisecond

var runtimeApplicationID = regexp.MustCompile(`^rap_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var runtimeSnapshotID = regexp.MustCompile(`^cfg_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var runtimeRouteDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Called only with publication.RLock and runtime.mu held, after successful
// preparation. Bounded best-effort evidence cannot extend the authorization
// lease or make failed publication succeed. No request hot path invokes this.
func (s *Service) recordRuntimeApplication(ctx context.Context, routes *runtimeRoutes, auth *runtimeAuthorization, epoch uint64) error {
	if !s.runtimeApplicationCaptureCurrent(routes, auth, epoch) {
		return runtimeUnavailable
	}
	if !s.instanceMu.TryRLock() {
		return runtimeUnavailable
	}
	lease := s.instance
	s.instanceMu.RUnlock()
	if lease == nil || lease.startedAt.IsZero() {
		return runtimeUnavailable
	}
	budget := min(runtimeApplicationWriteBudget, time.Until(auth.ValidUntil))
	if budget <= 0 {
		return runtimeUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	applied := time.Now().UTC().Truncate(time.Microsecond)
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var instance entity.SystemInstance
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, lease.id)).Take(&instance).Error
		if err != nil {
			return err
		}
		if !runtimeApplicationInstanceMatches(instance, lease, applied) || !s.runtimeApplicationCaptureCurrent(routes, auth, epoch) {
			return runtimeUnavailable
		}
		var prior []entity.RuntimeRoutingApplication
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "instance_id"}, lease.id)).Where(database.ExactText(tx, clause.Column{Name: "snapshot_id"}, routes.ID)).Limit(2).Find(&prior).Error; err != nil {
			return err
		}
		if len(prior) > 0 {
			if len(prior) != 1 || !validRuntimeApplication(prior[0]) || prior[0].InstanceID != lease.id || !prior[0].InstanceStartedAt.Equal(lease.startedAt) || prior[0].SnapshotID != routes.ID || prior[0].RouteDigest != routes.Digest || !prior[0].PublishedAt.Equal(routes.PublishedAt.UTC().Truncate(time.Microsecond)) {
				return runtimeUnavailable
			}
			if !s.runtimeApplicationCaptureCurrent(routes, auth, epoch) {
				return runtimeUnavailable
			}
			return nil
		}
		rowID, err := id.NewPrefixed("rap")
		if err != nil {
			return err
		}
		row := entity.RuntimeRoutingApplication{ID: rowID, InstanceID: lease.id, InstanceStartedAt: lease.startedAt, SnapshotID: routes.ID, RouteDigest: routes.Digest, PublishedAt: routes.PublishedAt.UTC().Truncate(time.Microsecond), AppliedAt: applied}
		if !validRuntimeApplication(row) {
			return runtimeUnavailable
		}
		// Exact instance row lock serializes the unique process/snapshot observation.
		// A collating alias cannot replace an existing record through an upsert.
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if !s.runtimeApplicationCaptureCurrent(routes, auth, epoch) {
			return runtimeUnavailable
		}
		return nil
	})
}

func (s *Service) runtimeApplicationCaptureCurrent(routes *runtimeRoutes, auth *runtimeAuthorization, epoch uint64) bool {
	r := s.runtime
	return r != nil && routes != nil && auth != nil && runtimeSnapshotID.MatchString(routes.ID) && runtimeRouteDigest.MatchString(routes.Digest) && !routes.PublishedAt.IsZero() && r.routes.Load() == routes && r.auth.Load() == auth && r.epoch.Load() == epoch && auth.publicationEpoch == epoch && time.Now().Before(auth.ValidUntil) && auth.SourceDigest == routes.Digest
}
func runtimeApplicationInstanceMatches(row entity.SystemInstance, lease *systemInstanceLease, now time.Time) bool {
	return lease != nil && row.ID == lease.id && row.LeaseToken == lease.token && row.StartedAt.Equal(lease.startedAt) && row.Role == systemInstanceRole && row.RetiredAt == nil && row.StoppedAt == nil && row.LeaseExpiresAt.After(now)
}
func validRuntimeApplication(row entity.RuntimeRoutingApplication) bool {
	return runtimeApplicationID.MatchString(row.ID) && systemInstanceIDPattern.MatchString(row.InstanceID) && runtimeSnapshotID.MatchString(row.SnapshotID) && runtimeRouteDigest.MatchString(row.RouteDigest) && !row.InstanceStartedAt.IsZero() && !row.PublishedAt.IsZero() && !row.AppliedAt.IsZero() && !row.AppliedAt.Before(row.PublishedAt) && !row.AppliedAt.Before(row.InstanceStartedAt)
}

type RuntimeApplicationFilter struct {
	InstanceID, Cursor string
	Limit              int
}
type RuntimeApplicationRecord struct {
	ID                            string    `json:"id"`
	InstanceID                    string    `json:"instance_id"`
	InstanceStartedAt             time.Time `json:"instance_started_at"`
	SnapshotID                    string    `json:"snapshot_id"`
	PublishedAt                   time.Time `json:"published_at"`
	AppliedAt                     time.Time `json:"applied_at"`
	InstanceStatus                string    `json:"instance_status"`
	CurrentServingSnapshotMatches *bool     `json:"current_serving_snapshot_matches"`
}
type RuntimeApplicationPage struct {
	Scope      string                     `json:"scope"`
	ObservedAt time.Time                  `json:"observed_at"`
	Items      []RuntimeApplicationRecord `json:"items"`
	NextCursor *string                    `json:"next_cursor"`
}

func runtimeApplicationCursor(actorID string, f RuntimeApplicationFilter, last string) string {
	scope := personalHash([]string{actorID, f.InstanceID})
	return base64.RawURLEncoding.EncodeToString([]byte(last + "|" + scope))
}
func validateRuntimeApplicationFilter(actorID string, f RuntimeApplicationFilter) (RuntimeApplicationFilter, string, error) {
	if f.Limit == 0 {
		f.Limit = 20
	}
	if f.Limit < 1 || f.Limit > 100 || f.InstanceID != "" && !systemInstanceIDPattern.MatchString(f.InstanceID) || len(f.Cursor) > 200 {
		return f, "", apperrors.ErrBadRequest
	}
	last := ""
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		if err != nil {
			return f, "", apperrors.ErrBadRequest
		}
		parts := strings.Split(string(raw), "|")
		if len(parts) != 2 || !runtimeApplicationID.MatchString(parts[0]) || f.Cursor != runtimeApplicationCursor(actorID, f, parts[0]) {
			return f, "", apperrors.ErrBadRequest
		}
		last = parts[0]
	}
	return f, last, nil
}

// ListRuntimeApplications returns retained facts; current process matching is a
// separate conservative observation, never an original receipt or fleet proof.
func (s *Service) ListRuntimeApplications(ctx context.Context, actorID string, f RuntimeApplicationFilter) (*RuntimeApplicationPage, error) {
	f, last, err := validateRuntimeApplicationFilter(actorID, f)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	now := time.Now().UTC()
	page := &RuntimeApplicationPage{Scope: "routing_only", ObservedAt: now, Items: []RuntimeApplicationRecord{}}
	var rows []entity.RuntimeRoutingApplication
	instances := map[string]entity.SystemInstance{}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		allowed, err := exactGovernancePermissionForAdmittedActor(tx, actor, "system.read")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		q := tx.Model(&entity.RuntimeRoutingApplication{})
		if f.InstanceID != "" {
			q = q.Where(database.ExactText(tx, clause.Column{Name: "instance_id"}, f.InstanceID))
		}
		if last != "" {
			q = q.Where("id < ?", last)
		}
		if err := q.Order("id DESC").Limit(f.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > f.Limit {
			next := runtimeApplicationCursor(actorID, f, rows[f.Limit-1].ID)
			page.NextCursor = &next
			rows = rows[:f.Limit]
		}
		ids := []string{}
		for _, row := range rows {
			if !validRuntimeApplication(row) || f.InstanceID != "" && row.InstanceID != f.InstanceID {
				return runtimeUnavailable
			}
			if _, ok := instances[row.InstanceID]; !ok {
				instances[row.InstanceID] = entity.SystemInstance{}
				ids = append(ids, row.InstanceID)
			}
		}
		if len(ids) > 0 {
			var loaded []entity.SystemInstance
			if err := tx.Where("id IN ?", ids).Limit(len(ids) + 1).Find(&loaded).Error; err != nil {
				return err
			}
			seen := map[string]bool{}
			for _, row := range loaded {
				if _, ok := instances[row.ID]; !ok || seen[row.ID] {
					return runtimeUnavailable
				}
				seen[row.ID] = true
				instances[row.ID] = row
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	for _, row := range rows {
		instance := instances[row.InstanceID]
		status := "unknown"
		if instance.ID == row.InstanceID && instance.StartedAt.Equal(row.InstanceStartedAt) {
			status = "offline"
			if instance.StoppedAt == nil && instance.RetiredAt == nil && instance.LeaseExpiresAt.After(now) {
				status = "online"
			}
		}
		record := RuntimeApplicationRecord{ID: row.ID, InstanceID: row.InstanceID, InstanceStartedAt: row.InstanceStartedAt.UTC(), SnapshotID: row.SnapshotID, PublishedAt: row.PublishedAt.UTC(), AppliedAt: row.AppliedAt.UTC(), InstanceStatus: status}
		record.CurrentServingSnapshotMatches = s.runtimeApplicationServingMatch(row, instance)
		page.Items = append(page.Items, record)
	}
	return page, nil
}
func (s *Service) runtimeApplicationServingMatch(row entity.RuntimeRoutingApplication, instance entity.SystemInstance) *bool {
	if !s.instanceMu.TryRLock() {
		return nil
	}
	lease := s.instance
	s.instanceMu.RUnlock()
	if !runtimeApplicationInstanceMatches(instance, lease, time.Now()) || instance.ID != row.InstanceID || !instance.StartedAt.Equal(row.InstanceStartedAt) {
		return nil
	}
	r := s.runtime
	if r == nil {
		return nil
	}
	epoch := r.epoch.Load()
	auth := r.auth.Load()
	routes := r.routes.Load()
	status := r.status.Load()
	if !s.runtimeApplicationCaptureCurrent(routes, auth, epoch) || status == nil || status.ErrorCode != "" {
		return nil
	}
	match := routes.ID == row.SnapshotID && routes.Digest == row.RouteDigest
	if !s.instanceMu.TryRLock() {
		return nil
	}
	same := s.instance == lease
	s.instanceMu.RUnlock()
	if !same || !s.runtimeApplicationCaptureCurrent(routes, auth, epoch) || r.status.Load() != status {
		return nil
	}
	return &match
}

// ParseRuntimeApplicationLimit preserves absent versus explicit empty values.
func ParseRuntimeApplicationLimit(raw string) (int, error) {
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 || strconv.Itoa(limit) != raw {
		return 0, &apperrors.Error{Code: http.StatusBadRequest, Message: "invalid routing application limit"}
	}
	return limit, nil
}
