package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func jsonUnmarshalVault(raw string, target any) error {
	// Validate each object first so duplicate keys cannot overwrite recorded facts.
	if !vaultUniqueJSON([]byte(raw)) {
		return vaultUnavailable
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(target); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return vaultUnavailable
	}
	return nil
}
func vaultDecodeCleanup(raw string, v *VaultCleanupView) bool {
	fields, err := vaultStrict([]byte(raw), []string{"state", "observation"})
	if err != nil || !vaultDecodeObservation(string(fields["observation"]), &v.Observation) {
		return false
	}
	if len(raw) > 4096 || jsonUnmarshalVault(raw, v) != nil {
		return false
	}
	switch v.State {
	case "not_attempted", "acknowledged", "failed", "unknown":
	default:
		return false
	}
	return vaultValidObservation(v.Observation)
}
func vaultValidObservation(v VaultObservationView) bool {
	n, e := strconv.ParseUint(v.DurationMS, 10, 64)
	if e != nil || strconv.FormatUint(n, 10) != v.DurationMS || v.Succeeded && (!v.Attempted || v.Failure != nil) {
		return false
	}
	if v.Failure != nil {
		switch v.Failure.Stage {
		case "prepare", "write", "read", "cleanup":
		default:
			return false
		}
		switch v.Failure.Code {
		case "invalid_descriptor", "invalid_tokens", "invalid_plan", "random_unavailable", "invalid_response", "verification_failed", "canceled", "timed_out", "transport", "http_status", "response_too_large", "invalid_request", "interrupted", "already_attempted", "prepared_closed", "invalid_auth", "auth_expired":
		default:
			return false
		}
		if v.Failure.HTTPStatus != 0 && (v.Failure.HTTPStatus < 100 || v.Failure.HTTPStatus > 599) {
			return false
		}
	}
	return true
}
func vaultJSON(v any) string                      { raw, _ := json.Marshal(v); return string(raw) }
func vaultEmptyObservation() VaultObservationView { return VaultObservationView{DurationMS: "0"} }
func (s *Service) GetVaultProbe(ctx context.Context, actorID, target, probeID string) (*VaultProbeView, error) {
	if !vaultProbeID.MatchString(probeID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var view *VaultProbeView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := vaultAuthorize(tx, actorID, "secrets.read")
		if e != nil {
			return e
		}
		row, _, _, _, e := vaultSnapshot(tx, target, false)
		if e != nil {
			return e
		}
		var p entity.VaultProbe
		if e = personalExact(vaultDB(tx), "id", probeID).Take(&p).Error; e != nil {
			return e
		}
		view, e = vaultProbeView(actor, row, p)
		return e
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, vaultError(err)
	}
	return view, nil
}

// Lease holds the existing root reader drain across the whole finite command.
func (s *Service) vaultReadLease() (func(), error) {
	if s.rootReadersClosed.Load() {
		return nil, vaultUnavailable
	}
	s.rootReaders.Add(1)
	if s.rootReadersClosed.Load() {
		s.rootReaders.Add(-1)
		return nil, vaultUnavailable
	}
	p := s.rootPolicy.Load()
	if p != nil {
		p.refs.Add(1)
		if p.closed.Load() || s.rootPolicy.Load() != p {
			p.refs.Add(-1)
			s.rootReaders.Add(-1)
			return nil, vaultUnavailable
		}
	}
	return func() {
		if p != nil {
			p.refs.Add(-1)
		}
		s.rootReaders.Add(-1)
	}, nil
}
func vaultCommandReplay(tx *gorm.DB, actor entity.User, probeID, kind, etag string, input VaultStageInput) (*entity.VaultProbeCommand, error) {
	var c entity.VaultProbeCommand
	e := personalExact(vaultDB(tx), "request_id", input.RequestID).Take(&c).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if c.ActorID != actor.ID || !c.ActorBirth.Equal(actor.CreatedAt) || c.Kind != kind || c.ReviewETag != etag || c.Reason != input.Reason || probeID != "" && c.ProbeID != probeID {
		return nil, catalogConflict
	}
	return &c, nil
}

// Historical cleanup must use fresh destinations: GORM adds any nonzero
// destination primary key to the SELECT predicate, even with NewDB enabled.
func vaultHistoricalSnapshot(tx *gorm.DB, row entity.VaultIntegration, revisionID string) (entity.VaultRevision, entity.VaultWriterAuth, entity.VaultReaderAuth, error) {
	var rev entity.VaultRevision
	var w entity.VaultWriterAuth
	var r entity.VaultReaderAuth
	if err := personalExact(vaultDB(tx), "id", revisionID).Take(&rev).Error; err != nil {
		return rev, w, r, err
	}
	if rev.ID != revisionID || rev.IntegrationID != row.ID || !rev.IntegrationBirth.Equal(row.CreatedAt) {
		return rev, w, r, vaultUnavailable
	}
	if err := personalExact(vaultDB(tx), "id", rev.ID).Take(&w).Error; err != nil {
		return rev, w, r, err
	}
	if err := personalExact(vaultDB(tx), "id", rev.ID).Take(&r).Error; err != nil {
		return rev, w, r, err
	}
	if w.ID != rev.ID || r.ID != rev.ID {
		return rev, w, r, vaultUnavailable
	}
	return rev, w, r, nil
}

func (s *Service) RunVaultProbe(ctx context.Context, actorID, target, probeID, kind, etag string, input VaultStageInput) (*VaultProbeView, bool, error) {
	if !credentialReplacementRequestID.MatchString(input.RequestID) || !rootReason(input.Reason) || !vaultStrong(etag) || (kind != "write" && kind != "read" && kind != "cleanup") || kind != "write" && !vaultProbeID.MatchString(probeID) {
		return nil, false, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, e := s.vaultReadLease()
	if e != nil {
		return nil, false, e
	}
	defer release()
	var actor entity.User
	var row entity.VaultIntegration
	var rev entity.VaultRevision
	var w entity.VaultWriterAuth
	var r entity.VaultReaderAuth
	var p entity.VaultProbe
	var replay *entity.VaultProbeCommand
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		actor, err = vaultAuthorize(tx, actorID, "secrets.test")
		if err != nil {
			return err
		}
		row, rev, w, r, err = vaultSnapshot(tx, target, false)
		if err != nil {
			return err
		}
		replay, err = vaultCommandReplay(tx, actor, probeID, kind, etag, input)
		if err != nil {
			return err
		}
		if replay != nil {
			probeID = replay.ProbeID
			return personalExact(vaultDB(tx), "id", probeID).Take(&p).Error
		}
		if kind == "write" {
			if row.ActiveProbeID != nil || vaultReview(actor, row, rev) != etag {
				return catalogConflict
			}
		} else {
			if err = personalExact(vaultDB(tx), "id", probeID).Take(&p).Error; err != nil {
				return err
			}
			if p.State == "completed" || p.IntegrationID != target || !p.IntegrationBirth.Equal(row.CreatedAt) || vaultProbeReview(actor, row, p) != etag {
				return catalogConflict
			}
			if kind == "read" && p.RevisionID != row.RevisionID {
				return catalogConflict
			}
			if p.State == "writing" || p.State == "reading" {
				var commands []entity.VaultProbeCommand
				if err = vaultDB(tx).Where("probe_id = ? AND finished_at IS NULL AND expires_at > ?", p.ID, vaultNow()).Limit(1).Find(&commands).Error; err != nil {
					return err
				}
				if len(commands) > 0 {
					return catalogConflict
				}
			}
			if p.RevisionID != rev.ID {
				rev, w, r, err = vaultHistoricalSnapshot(tx, row, p.RevisionID)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if e != nil {
		return nil, false, vaultError(e)
	}
	if replay != nil {
		if p.IntegrationID != row.ID || !p.IntegrationBirth.Equal(row.CreatedAt) {
			return nil, false, catalogConflict
		}
		view, err := vaultProbeView(actor, row, p)
		if err != nil {
			return nil, false, err
		}
		running := replay.FinishedAt == nil && vaultNow().Before(replay.ExpiresAt)
		if replay.FinishedAt == nil && !running {
			view.State = "interrupted"
			view.FinishedAt = nil
		}
		return view, running, err
	}
	client, e := vaultClient(rev, s.allowPrivateUpstream)
	if e != nil {
		return nil, false, e
	}
	defer client.Close()
	wt, rt, e := s.vaultOpen(w, r)
	if e != nil || wt == "" || rt == "" {
		return nil, false, vaultUnavailable
	}
	var prepared *vault.PreparedProbe
	if kind == "write" {
		prepared, e = client.Prepare()
		if e != nil {
			return nil, false, vaultUnavailable
		}
		defer prepared.Close()
		plan := prepared.Plan()
		probeID = plan.ProbeID
		p = entity.VaultProbe{ID: probeID, RequestID: input.RequestID, IntegrationID: target, IntegrationBirth: row.CreatedAt, RevisionID: rev.ID, ActorID: actor.ID, ActorBirth: actor.CreatedAt, ExpectedSHA256: plan.ExpectedSHA256, DescriptorSHA256: plan.DescriptorSHA256, State: "planned", Generation: rootHash("vault.plan:" + probeID), WriteJSON: vaultJSON(vaultEmptyObservation()), ReadJSON: vaultJSON(vaultEmptyObservation()), CleanupJSON: vaultJSON(VaultCleanupView{"not_attempted", vaultEmptyObservation()}), CreatedAt: vaultNow()}
	}
	claim, e := id.NewPrefixed("vcl")
	if e != nil {
		return nil, false, apperrors.ErrInternal
	}
	now := vaultNow()
	command := entity.VaultProbeCommand{RequestID: input.RequestID, ProbeID: probeID, ActorID: actor.ID, ActorBirth: actor.CreatedAt, Kind: kind, ReviewETag: etag, Reason: input.Reason, Claim: claim, ExpiresAt: now.Add(30 * time.Second), CreatedAt: now}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		currentActor, err := vaultAuthorize(tx, actorID, "secrets.test")
		if err != nil {
			return err
		}
		if !currentActor.CreatedAt.Equal(actor.CreatedAt) {
			return catalogConflict
		}
		current, currentRev, _, _, err := vaultSnapshot(tx, target, true)
		if err != nil {
			return err
		}
		if !current.CreatedAt.Equal(row.CreatedAt) {
			return catalogConflict
		}
		if _, err = vaultCommandReplay(tx, currentActor, probeID, kind, etag, input); err != nil {
			return err
		}
		if kind == "write" {
			if current.ActiveProbeID != nil || currentRev.ID != rev.ID || vaultReview(currentActor, current, currentRev) != etag {
				return catalogConflict
			}
			if err = tx.Create(&p).Error; err != nil {
				return err
			}
			if err = personalExact(vaultDB(tx).Model(&entity.VaultIntegration{}), "id", row.ID).Update("ActiveProbeID", probeID).Error; err != nil {
				return err
			}
		} else {
			var currentProbe entity.VaultProbe
			if err = personalExact(vaultDB(tx), "id", probeID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&currentProbe).Error; err != nil {
				return err
			}
			if currentProbe.Generation != p.Generation || vaultProbeReview(currentActor, current, currentProbe) != etag || kind == "read" && current.RevisionID != p.RevisionID {
				return catalogConflict
			}
			p = currentProbe
		}
		// The unique receipt and probe generation prevent two executors from claiming the same command.
		if err = tx.Create(&command).Error; err != nil {
			return err
		}
		p.State = "reading"
		if kind == "write" {
			p.State = "writing"
		}
		p.Generation = rootHash("vault.command:" + claim)
		p.FinishedAt = nil
		if err = tx.Save(&p).Error; err != nil {
			return err
		}
		return vaultAudit(tx, actor.ID, row.ID, kind, input.RequestID, input.Reason, rev.ID, false)
	})
	if e != nil {
		return nil, false, catalogError(e)
	}
	// No database/governance lock spans either client request. No retry or dispatch on persistence error.
	var ownership VaultObservationView
	var stageErr error
	plan := vault.ProbePlan{ProbeID: p.ID, ExpectedSHA256: p.ExpectedSHA256, DescriptorSHA256: p.DescriptorSHA256}
	// Authentication belongs to the already claimed finite command. Login
	// failures never claim that a KV operation was attempted.
	writerToken, readerToken := wt, rt
	var login vault.Observation
	var closeWriter, closeReader func()
	closeWriter, closeReader = func() {}, func() {}
	if kind != "write" {
		readerToken, closeReader, login, stageErr = vaultCommandToken(ctx, client, r.Method, rt)
	}
	if stageErr == nil {
		writerToken, closeWriter, login, stageErr = vaultCommandToken(ctx, client, w.Method, wt)
	}
	defer closeWriter()
	defer closeReader()
	if stageErr != nil {
		failed := vaultLoginFailure(login)
		p.State = "cleanup_pending"
		p.CleanupJSON = vaultJSON(VaultCleanupView{"unknown", vaultEmptyObservation()})
		switch kind {
		case "write":
			p.State = "interrupted"
			p.WriteJSON = vaultJSON(failed)
		case "read":
			p.ReadJSON = vaultJSON(failed)
		case "cleanup":
			ownership = failed
		}
	} else {
		switch kind {
		case "write":
			result, err := client.Write(ctx, writerToken, prepared)
			stageErr = err
			p.Version = result.Version
			p.WriteJSON = vaultJSON(vaultObservation(result.Write))
			p.State = "awaiting_read"
			if err != nil {
				p.State = "interrupted"
				p.CleanupJSON = vaultJSON(VaultCleanupView{"unknown", vaultEmptyObservation()})
			}
		case "read":
			result, err := client.ReadAndCleanup(ctx, readerToken, writerToken, plan)
			stageErr = err
			p.Version = max(p.Version, result.Version)
			p.ReadJSON = vaultJSON(vaultObservation(result.Read))
			p.CleanupJSON = vaultJSON(vaultCleanup(result.Cleanup))
			p.State = "cleanup_pending"
			if result.Cleanup.State == "acknowledged" {
				p.State = "completed"
			}
		case "cleanup":
			result, err := client.CleanupOwned(ctx, readerToken, writerToken, plan)
			stageErr = err
			p.Version = max(p.Version, result.Version)
			ownership = vaultObservation(result.Ownership)
			p.CleanupJSON = vaultJSON(vaultCleanup(result.Cleanup))
			p.State = "cleanup_pending"
			if result.Cleanup.State == "acknowledged" {
				p.State = "completed"
			}
		}
	}
	_ = stageErr // Sanitized client observations, never the raw error, are the operation result.
	finished := vaultNow()
	p.FinishedAt = &finished
	p.Generation = rootHash("vault.result:" + claim)
	view, e := vaultProbeView(actor, row, p)
	if e != nil {
		return nil, false, e
	}
	command.ResultJSON = vaultJSON(view)
	if kind == "cleanup" {
		command.OwnershipJSON = vaultJSON(ownership)
	}
	command.FinishedAt = &finished
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var stored entity.VaultProbeCommand
		if err := personalExact(vaultDB(tx), "request_id", command.RequestID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&stored).Error; err != nil {
			return err
		}
		if stored.Claim != claim || stored.FinishedAt != nil {
			return catalogConflict
		}
		var current entity.VaultProbe
		if err := personalExact(vaultDB(tx), "id", p.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; err != nil {
			return err
		}
		if current.Generation != rootHash("vault.command:"+claim) {
			return catalogConflict
		}
		if err := tx.Save(&p).Error; err != nil {
			return err
		}
		if err := tx.Save(&command).Error; err != nil {
			return err
		}
		if p.State == "completed" {
			return personalExact(vaultDB(tx).Model(&entity.VaultIntegration{}), "id", row.ID).Where("active_probe_id = ?", p.ID).Update("ActiveProbeID", nil).Error
		}
		return nil
	})
	if e != nil {
		return nil, false, catalogError(e)
	}
	return view, false, nil
}

func vaultUniqueJSON(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func() bool
	walk = func() bool {
		token, err := d.Token()
		if err != nil {
			return false
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				k, ok := key.(string)
				if e != nil || !ok || seen[k] {
					return false
				}
				seen[k] = true
				if !walk() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim('}')
		case json.Delim('['):
			for d.More() {
				if !walk() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim(']')
		default:
			return true
		}
	}
	if !walk() {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func vaultNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
