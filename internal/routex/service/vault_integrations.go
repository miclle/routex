package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var vaultIntegrationID = regexp.MustCompile(`^vlt_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var vaultRevisionID = regexp.MustCompile(`^vlr_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var vaultProbeID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var vaultUnavailable = &apperrors.Error{Code: 503, Message: "Vault configuration unavailable"}

func vaultError(err error) error {
	if err == nil {
		return nil
	}
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return vaultUnavailable
}
func vaultDB(tx *gorm.DB) *gorm.DB { return tx.Session(&gorm.Session{NewDB: true}) }
func vaultIdentity(actor entity.User, row entity.VaultIntegration) string {
	return connectionMetadataHash(struct {
		Kind, Actor, Target     string
		ActorBirth, TargetBirth time.Time
	}{"vault.integration.identity.v1", actor.ID, row.ID, actor.CreatedAt.UTC(), row.CreatedAt.UTC()})
}
func vaultReview(actor entity.User, row entity.VaultIntegration, revision entity.VaultRevision) string {
	return vaultIdentity(actor, row) + "." + connectionMetadataHash(struct {
		Kind, ID, Name string
		Descriptor     VaultDescriptor
	}{"vault.integration.review.v1", revision.ID, row.Name, vaultDescriptor(revision)})
}
func vaultCreationReview(actor entity.User, c entity.VaultCatalogue) string {
	return connectionMetadataHash(struct {
		Kind, Actor string
		Birth       time.Time
	}{"vault.creation.identity.v1", actor.ID, actor.CreatedAt.UTC()}) + "." + rootHash("vault.catalogue:"+c.Generation)
}
func vaultDescriptor(r entity.VaultRevision) VaultDescriptor {
	return VaultDescriptor{r.Endpoint, r.Namespace, r.Mount, r.Prefix, r.DataField}
}
func vaultClient(r entity.VaultRevision, allowPrivate bool) (*vault.Client, error) {
	c, err := vault.New(vaultDescriptor(r).client(), allowPrivate)
	if err != nil {
		return nil, vaultUnavailable
	}
	return c, nil
}
func vaultCatalogue(tx *gorm.DB, lock bool) (entity.VaultCatalogue, error) {
	var c entity.VaultCatalogue
	q := vaultDB(tx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&c, 1).Error
	if err == nil && (c.ID != 1 || !personalModelETag(c.Generation)) {
		err = vaultUnavailable
	}
	return c, err
}
func vaultSnapshot(tx *gorm.DB, target string, lock bool) (entity.VaultIntegration, entity.VaultRevision, entity.VaultWriterAuth, entity.VaultReaderAuth, error) {
	var row entity.VaultIntegration
	var rev entity.VaultRevision
	var w entity.VaultWriterAuth
	var r entity.VaultReaderAuth
	if !vaultIntegrationID.MatchString(target) {
		return row, rev, w, r, apperrors.ErrBadRequest
	}
	q := personalExact(vaultDB(tx), "id", target)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&row).Error
	if err == nil && (row.ID != target || !connectionMetadataBirth(row.CreatedAt) || !validCatalogLabel(row.Name) || !vaultRevisionID.MatchString(row.RevisionID)) {
		err = vaultUnavailable
	}
	if err == nil {
		err = personalExact(vaultDB(tx), "id", row.RevisionID).Take(&rev).Error
	}
	if err == nil && (rev.ID != row.RevisionID || rev.IntegrationID != row.ID || !rev.IntegrationBirth.Equal(row.CreatedAt) || rev.Name != row.Name) {
		err = vaultUnavailable
	}
	if err == nil {
		err = personalExact(vaultDB(tx), "id", rev.ID).Take(&w).Error
	}
	if err == nil {
		err = personalExact(vaultDB(tx), "id", rev.ID).Take(&r).Error
	}
	if err == nil && (w.ID != rev.ID || r.ID != rev.ID || !rootSafeIdentity(w.SecretGeneration, 30) || !rootSafeIdentity(r.SecretGeneration, 30) || !vaultStoredMethod(w.Method) || !vaultStoredMethod(r.Method) || w.AuthCiphertext == "" && w.Method != "token" || r.AuthCiphertext == "" && r.Method != "token") {
		err = vaultUnavailable
	}
	if err == nil {
		// Descriptor validation performs no I/O. Actual stages reapply deployment network policy.
		c, e := vaultClient(rev, true)
		if e != nil {
			err = e
		} else {
			c.Close()
		}
	}
	return row, rev, w, r, err
}
func (s *Service) vaultView(tx *gorm.DB, actor entity.User, row entity.VaultIntegration, rev entity.VaultRevision, w entity.VaultWriterAuth, r entity.VaultReaderAuth) (VaultIntegrationView, error) {
	write, e := exactGovernancePermission(tx, actor, "secrets.write")
	if e != nil {
		return VaultIntegrationView{}, e
	}
	test, e := exactGovernancePermission(tx, actor, "secrets.test")
	if e != nil {
		return VaultIntegrationView{}, e
	}
	if !vaultStoredMethod(w.Method) || !vaultStoredMethod(r.Method) || w.AuthCiphertext == "" && w.Method != "token" || r.AuthCiphertext == "" && r.Method != "token" {
		return VaultIntegrationView{}, vaultUnavailable
	}
	v := VaultIntegrationView{row.ID, row.Name, rev.ID, vaultDescriptor(rev), VaultAuthView{w.Method, w.AuthCiphertext != ""}, VaultAuthView{r.Method, r.AuthCiphertext != ""}, vaultReview(actor, row, rev), write, test, nil}
	var probes []entity.VaultProbe
	if e = vaultDB(tx).Where("integration_id = ?", row.ID).Order("created_at DESC, id DESC").Limit(1).Find(&probes).Error; e != nil {
		return v, e
	}
	if len(probes) > 0 {
		p := probes[0]
		if p.IntegrationID != row.ID || !p.IntegrationBirth.Equal(row.CreatedAt) {
			return v, vaultUnavailable
		}
		pv, e := vaultProbeView(actor, row, p)
		if e != nil {
			return v, e
		}
		v.LastProbe = pv
	}
	return v, nil
}
func (s *Service) GetVaultIntegration(ctx context.Context, actorID, target string) (*VaultIntegrationView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var view VaultIntegrationView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := vaultAuthorize(tx, actorID, "secrets.read")
		if e != nil {
			return e
		}
		row, rev, w, r, e := vaultSnapshot(tx, target, false)
		if e != nil {
			return e
		}
		view, e = s.vaultView(tx, actor, row, rev, w, r)
		return e
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, vaultError(err)
	}
	return &view, nil
}
func (s *Service) ListVaultIntegrations(ctx context.Context, actorID, cursor string, limit int) (*VaultIntegrationPage, error) {
	if cursor != "" && !vaultIntegrationID.MatchString(cursor) || limit < 1 || limit > 50 {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	view := &VaultIntegrationPage{Items: []VaultIntegrationView{}}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := vaultAuthorize(tx, actorID, "secrets.read")
		if e != nil {
			return e
		}
		cat, e := vaultCatalogue(tx, false)
		if e != nil {
			return e
		}
		view.ReviewETag = vaultCreationReview(actor, cat)
		view.CanWrite, e = exactGovernancePermission(tx, actor, "secrets.write")
		if e != nil {
			return e
		}
		view.CanTest, e = exactGovernancePermission(tx, actor, "secrets.test")
		if e != nil {
			return e
		}
		var rows []entity.VaultIntegration
		q := vaultDB(tx)
		col := clause.Column{Name: "id"}
		if cursor != "" {
			q = q.Where(database.ByteAfter(tx, col, cursor))
		}
		if e = q.Clauses(clause.OrderBy{Expression: database.ByteOrder(tx, col)}).Limit(limit + 1).Find(&rows).Error; e != nil {
			return e
		}
		if len(rows) > limit {
			n := rows[limit-1].ID
			view.NextCursor = &n
			rows = rows[:limit]
		}
		for _, r := range rows {
			row, rev, w, read, e := vaultSnapshot(tx, r.ID, false)
			if e != nil {
				return e
			}
			v, e := s.vaultView(tx, actor, row, rev, w, read)
			if e != nil {
				return e
			}
			view.Items = append(view.Items, v)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, vaultError(err)
	}
	return view, nil
}
func vaultProbeReview(actor entity.User, row entity.VaultIntegration, p entity.VaultProbe) string {
	return vaultIdentity(actor, row) + "." + rootHash("vault.probe:"+p.ID+":"+p.RevisionID+":"+p.Generation+":"+p.State)
}
func vaultProbeView(actor entity.User, row entity.VaultIntegration, p entity.VaultProbe) (*VaultProbeView, error) {
	if !vaultProbeID.MatchString(p.ID) || p.IntegrationID != row.ID || !p.IntegrationBirth.Equal(row.CreatedAt) || !vaultRevisionID.MatchString(p.RevisionID) || !credentialReplacementRequestID.MatchString(p.RequestID) || !personalModelETag(p.Generation) || p.Version < 0 || p.Version > 1 {
		return nil, vaultUnavailable
	}
	switch p.State {
	case "planned", "writing", "awaiting_read", "reading", "cleanup_pending", "completed", "interrupted":
	default:
		return nil, vaultUnavailable
	}
	if !connectionMetadataBirth(p.CreatedAt) || p.FinishedAt != nil && (!connectionMetadataBirth(*p.FinishedAt) || p.FinishedAt.Before(p.CreatedAt)) {
		return nil, vaultUnavailable
	}
	v := &VaultProbeView{ID: p.ID, RequestID: p.RequestID, IntegrationID: p.IntegrationID, RevisionID: p.RevisionID, ReviewETag: vaultProbeReview(actor, row, p), State: p.State, CreatedAt: p.CreatedAt, FinishedAt: p.FinishedAt}
	if p.Version == 1 {
		x := int64(1)
		v.Version = &x
	}
	if !vaultDecodeObservation(p.WriteJSON, &v.Write) || !vaultDecodeObservation(p.ReadJSON, &v.Read) || !vaultDecodeCleanup(p.CleanupJSON, &v.Cleanup) {
		return nil, vaultUnavailable
	}
	return v, nil
}
func vaultDecodeObservation(raw string, v *VaultObservationView) bool {
	fields, err := vaultStrict([]byte(raw), []string{"attempted", "succeeded", "duration_ms", "failure"})
	if err != nil {
		return false
	}
	for _, key := range []string{"attempted", "succeeded", "duration_ms"} {
		if strings.TrimSpace(string(fields[key])) == "null" {
			return false
		}
	}
	if strings.TrimSpace(string(fields["failure"])) != "null" {
		failure, err := vaultStrict(fields["failure"], []string{"stage", "code", "http_status"})
		if err != nil || strings.TrimSpace(string(failure["http_status"])) == "null" {
			return false
		}
	}
	if len(raw) > 4096 || jsonUnmarshalVault(raw, v) != nil {
		return false
	}
	ms, e := strconv.ParseUint(v.DurationMS, 10, 64)
	return e == nil && strconv.FormatUint(ms, 10) == v.DurationMS && vaultValidObservation(*v)
}

func vaultAuthorize(tx *gorm.DB, actorID, permission string) (entity.User, error) {
	actor, err := rootAuthorize(tx, actorID, permission)
	if err == nil && !connectionMetadataBirth(actor.CreatedAt) {
		err = vaultUnavailable
	}
	return actor, err
}
