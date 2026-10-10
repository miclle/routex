package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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

const runtimeInstallationProjectionVersion = 1
const runtimeInstallationResponseLimit = 128 * 1024

var runtimeInstallationID = regexp.MustCompile(`^rin_[0-7][0-9a-hjkmnp-tv-z]{25}$`)

// Called once before projection on the unpublished authorization object. All
// subsequent preparation and legacy/new evidence writes consume this deadline.
func runtimeInstallationEvidenceContext(ctx context.Context, auth *runtimeAuthorization) (context.Context, context.CancelFunc) {
	end := time.Now().Add(runtimeApplicationWriteBudget)
	if auth == nil {
		end = time.Now()
	} else if auth.ValidUntil.Before(end) {
		end = auth.ValidUntil
	}
	return context.WithDeadline(ctx, end)
}

func runtimeInstallationContextCurrent(ctx context.Context) bool {
	end, bounded := ctx.Deadline()
	return ctx.Err() == nil && (!bounded || time.Now().Before(end))
}

func runtimeInstallationEvidenceDeadline(ctx context.Context, auth *runtimeAuthorization) bool {
	end, bounded := ctx.Deadline()
	return runtimeInstallationContextCurrent(ctx) && bounded && auth != nil && !end.After(auth.ValidUntil) && !end.After(time.Now().Add(runtimeApplicationWriteBudget))
}

func (s *Service) recordRuntimeObservations(ctx context.Context, routes *runtimeRoutes, auth *runtimeAuthorization, epoch uint64) {
	// Best effort preserves V87 independently when projection is unavailable.
	// Neither call may allocate a fresh evidence timeout.
	_ = s.recordRuntimeApplicationWithinBudget(ctx, routes, auth, epoch)
	_ = s.recordRuntimeInstallation(ctx, routes, auth, epoch)
}

func runtimeInstallationSource(routes *runtimeRoutes, auth *runtimeAuthorization) (string, bool) {
	if routes == nil || auth == nil || !runtimeSnapshotID.MatchString(routes.ID) || !runtimeRouteDigest.MatchString(routes.Digest) || routes.Digest != auth.SourceDigest || !runtimeRouteDigest.MatchString(auth.installationProjectionDigest) {
		return "", false
	}
	h := sha256.New()
	for _, part := range []string{"routex.gateway-installation.v1", routes.ID, routes.Digest, auth.installationProjectionDigest} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

func (s *Service) runtimeInstallationCaptureCurrent(ctx context.Context, routes *runtimeRoutes, auth *runtimeAuthorization, epoch uint64, status *RuntimeStatus) bool {
	r := s.runtime
	if r == nil || !runtimeInstallationContextCurrent(ctx) || !s.runtimeApplicationCaptureCurrent(routes, auth, epoch) || s.runtime != r || status == nil || status.ErrorCode != "" || r.status.Load() != status {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
	}
	_, known := runtimeInstallationSource(routes, auth)
	return known && s.runtime == r
}

func (s *Service) runtimeInstallationLeaseCurrent(lease *systemInstanceLease) bool {
	if lease == nil || !s.instanceMu.TryRLock() {
		return false
	}
	same := s.instance == lease
	s.instanceMu.RUnlock()
	return same
}

// Called under the normal publication locks with the already-running evidence
// context. It records actual installed state only, never desired DB state.
func (s *Service) recordRuntimeInstallation(ctx context.Context, routes *runtimeRoutes, auth *runtimeAuthorization, epoch uint64) error {
	if !runtimeInstallationEvidenceDeadline(ctx, auth) || s.runtime == nil {
		return runtimeUnavailable
	}
	r := s.runtime
	status := r.status.Load()
	if !s.runtimeInstallationCaptureCurrent(ctx, routes, auth, epoch, status) || !s.instanceMu.TryRLock() {
		return runtimeUnavailable
	}
	lease := s.instance
	s.instanceMu.RUnlock()
	if lease == nil || lease.startedAt.IsZero() {
		return runtimeUnavailable
	}
	source, known := runtimeInstallationSource(routes, auth)
	if !known {
		return runtimeUnavailable
	}
	var instance entity.SystemInstance
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, lease.id)).Take(&instance).Error; err != nil {
			return err
		}
		current := func() bool {
			return s.runtime == r && s.runtimeInstallationCaptureCurrent(ctx, routes, auth, epoch, status) && s.runtimeInstallationLeaseCurrent(lease) && runtimeApplicationInstanceMatches(instance, lease, time.Now())
		}
		if !current() {
			return runtimeUnavailable
		}
		var prior []entity.RuntimeInstallationObservation
		// A broad equality candidate read also detects collating aliases. Exact
		// comparisons below reject them; there is never an upsert or replacement.
		if err := tx.Where("instance_id = ? AND source_digest = ?", lease.id, source).Limit(2).Find(&prior).Error; err != nil {
			return err
		}
		if len(prior) > 0 {
			if len(prior) != 1 || !validRuntimeInstallation(prior[0]) || prior[0].InstanceID != lease.id || !prior[0].InstanceStartedAt.Equal(lease.startedAt) || prior[0].SnapshotID != routes.ID || prior[0].SourceDigest != source || !prior[0].RoutesPublishedAt.Equal(routes.PublishedAt.UTC().Truncate(time.Microsecond)) || !current() {
				return runtimeUnavailable
			}
			return nil
		}
		rowID, err := id.NewPrefixed("rin")
		if err != nil {
			return err
		}
		row := entity.RuntimeInstallationObservation{
			ID: rowID, InstanceID: lease.id, InstanceStartedAt: lease.startedAt,
			SnapshotID: routes.ID, ProjectionVersion: runtimeInstallationProjectionVersion,
			SourceDigest: source, RoutesPublishedAt: routes.PublishedAt.UTC().Truncate(time.Microsecond),
			FirstObservedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		if !validRuntimeInstallation(row) || !current() {
			return runtimeUnavailable
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if !current() {
			return runtimeUnavailable
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Late/unknown commit acknowledgement is not a positive receipt. A valid
	// committed historical row remains immutable and is read independently.
	if s.runtime != r || !s.runtimeInstallationCaptureCurrent(ctx, routes, auth, epoch, status) || !s.runtimeInstallationLeaseCurrent(lease) || !runtimeApplicationInstanceMatches(instance, lease, time.Now()) {
		return runtimeUnavailable
	}
	return nil
}

func runtimeInstallationMicrosecond(value time.Time) bool {
	return !value.IsZero() && value.Equal(value.Truncate(time.Microsecond))
}

func validRuntimeInstallation(row entity.RuntimeInstallationObservation) bool {
	return runtimeInstallationID.MatchString(row.ID) && systemInstanceIDPattern.MatchString(row.InstanceID) && runtimeSnapshotID.MatchString(row.SnapshotID) && row.ProjectionVersion == runtimeInstallationProjectionVersion && runtimeRouteDigest.MatchString(row.SourceDigest) && runtimeInstallationMicrosecond(row.InstanceStartedAt) && runtimeInstallationMicrosecond(row.RoutesPublishedAt) && runtimeInstallationMicrosecond(row.FirstObservedAt) && !row.RoutesPublishedAt.Before(row.InstanceStartedAt) && !row.FirstObservedAt.Before(row.RoutesPublishedAt)
}

type RuntimeInstallationFilter struct {
	InstanceID, Cursor string
	Limit              int
}
type RuntimeInstallationRecord struct {
	ID                                string    `json:"id"`
	ProjectionVersion                 int       `json:"projection_version"`
	InstanceID                        string    `json:"instance_id"`
	InstanceStartedAt                 time.Time `json:"instance_started_at"`
	SnapshotID                        string    `json:"snapshot_id"`
	RoutesPublishedAt                 time.Time `json:"routes_published_at"`
	FirstObservedAt                   time.Time `json:"first_observed_at"`
	InstanceStatus                    string    `json:"instance_status"`
	CurrentServingInstallationMatches *bool     `json:"current_serving_installation_matches"`
}
type RuntimeInstallationPage struct {
	Scope      string                      `json:"scope"`
	ObservedAt time.Time                   `json:"observed_at"`
	Items      []RuntimeInstallationRecord `json:"items"`
	NextCursor *string                     `json:"next_cursor"`
}

func runtimeInstallationCursor(actorID string, f RuntimeInstallationFilter, last string) string {
	scope := personalHash([]string{"gateway-installation.v1", actorID, f.InstanceID})
	return base64.RawURLEncoding.EncodeToString([]byte(last + "|" + scope))
}

func validateRuntimeInstallationFilter(actorID string, f RuntimeInstallationFilter) (RuntimeInstallationFilter, string, error) {
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
		if len(parts) != 2 || !runtimeInstallationID.MatchString(parts[0]) || f.Cursor != runtimeInstallationCursor(actorID, f, parts[0]) {
			return f, "", apperrors.ErrBadRequest
		}
		last = parts[0]
	}
	return f, last, nil
}

// ListRuntimeInstallations exposes retained observations and a separately checked
// local installed-state match. It is not latest DB, command or fleet authority.
func (s *Service) ListRuntimeInstallations(ctx context.Context, actorID string, f RuntimeInstallationFilter) (*RuntimeInstallationPage, error) {
	f, last, err := validateRuntimeInstallationFilter(actorID, f)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var rows []entity.RuntimeInstallationObservation
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
		q := tx.Model(&entity.RuntimeInstallationObservation{})
		if f.InstanceID != "" {
			q = q.Where(database.ExactText(tx, clause.Column{Name: "instance_id"}, f.InstanceID))
		}
		if last != "" {
			q = q.Where("id < ?", last)
		}
		if err := q.Order("id DESC").Limit(f.Limit + 1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > f.Limit+1 {
			return runtimeUnavailable
		}
		ids := []string{}
		previous := last
		for _, row := range rows {
			if !validRuntimeInstallation(row) || f.InstanceID != "" && row.InstanceID != f.InstanceID || previous != "" && row.ID >= previous {
				return runtimeUnavailable
			}
			previous = row.ID
			if _, exists := instances[row.InstanceID]; !exists {
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
				if _, expected := instances[row.ID]; !expected || seen[row.ID] {
					return runtimeUnavailable
				}
				seen[row.ID] = true
				instances[row.ID] = row
			}
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	page := &RuntimeInstallationPage{Scope: "single_process_gateway_admission", ObservedAt: time.Now().UTC(), Items: []RuntimeInstallationRecord{}}
	if len(rows) > f.Limit {
		next := runtimeInstallationCursor(actorID, f, rows[f.Limit-1].ID)
		page.NextCursor = &next
		rows = rows[:f.Limit]
	}
	for _, row := range rows {
		instance := instances[row.InstanceID]
		status := "unknown"
		if instance.ID == row.InstanceID && instance.StartedAt.Equal(row.InstanceStartedAt) && instance.Role == systemInstanceRole {
			status = "offline"
			if instance.StoppedAt == nil && instance.RetiredAt == nil && instance.LeaseExpiresAt.After(time.Now()) {
				status = "online"
			}
		}
		record := RuntimeInstallationRecord{ID: row.ID, ProjectionVersion: row.ProjectionVersion, InstanceID: row.InstanceID, InstanceStartedAt: row.InstanceStartedAt.UTC(), SnapshotID: row.SnapshotID, RoutesPublishedAt: row.RoutesPublishedAt.UTC(), FirstObservedAt: row.FirstObservedAt.UTC(), InstanceStatus: status}
		record.CurrentServingInstallationMatches = s.runtimeInstallationServingMatch(ctx, row, instance)
		page.Items = append(page.Items, record)
	}
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) > runtimeInstallationResponseLimit || !runtimeInstallationContextCurrent(ctx) {
		return nil, runtimeUnavailable
	}
	return page, nil
}

func (s *Service) runtimeInstallationServingMatch(ctx context.Context, row entity.RuntimeInstallationObservation, instance entity.SystemInstance) *bool {
	if !validRuntimeInstallation(row) || !s.instanceMu.TryRLock() {
		return nil
	}
	lease := s.instance
	s.instanceMu.RUnlock()
	if !runtimeApplicationInstanceMatches(instance, lease, time.Now()) || instance.ID != row.InstanceID || !instance.StartedAt.Equal(row.InstanceStartedAt) || s.runtime == nil {
		return nil
	}
	r := s.runtime
	epoch, auth, routes, status := r.epoch.Load(), r.auth.Load(), r.routes.Load(), r.status.Load()
	if !s.runtimeInstallationCaptureCurrent(ctx, routes, auth, epoch, status) {
		return nil
	}
	source, known := runtimeInstallationSource(routes, auth)
	if !known {
		return nil
	}
	match := row.SourceDigest == source && row.SnapshotID == routes.ID && row.RoutesPublishedAt.Equal(routes.PublishedAt.UTC().Truncate(time.Microsecond))
	if !s.runtimeInstallationLeaseCurrent(lease) || !runtimeApplicationInstanceMatches(instance, lease, time.Now()) || s.runtime != r || !s.runtimeInstallationCaptureCurrent(ctx, routes, auth, epoch, status) {
		return nil
	}
	return &match
}

func ParseRuntimeInstallationLimit(raw string) (int, error) {
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 || strconv.Itoa(limit) != raw {
		return 0, &apperrors.Error{Code: http.StatusBadRequest, Message: "invalid installation observation limit"}
	}
	return limit, nil
}
