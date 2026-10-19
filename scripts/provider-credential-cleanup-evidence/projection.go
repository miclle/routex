// Package cleanupevidence projects bounded durable published-cleanup facts for
// controlled acceptance. It is a read-only library, not a cleanup authorizer.
package cleanupevidence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrRejected deliberately excludes input, driver errors and SQL from output.
var ErrRejected = errors.New("cleanup evidence rejected")

const censusLimit = 32

var (
	uuidPattern     = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	hexPattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
	sourcePattern   = regexp.MustCompile(`^[a-f0-9]{40}$`)
	runPattern      = regexp.MustCompile(`^[a-f0-9]{32}$`)
	reviewPattern   = regexp.MustCompile(`^[a-f0-9]{64}\.[a-f0-9]{64}$`)
	identityPattern = regexp.MustCompile(`^(usr|vlt|vlr|crd|ins|aud)_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	durationPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})(\.[0-9]{1,6})?$`)
)

// Expectation is independently captured run/intent identity, never DB discovery.
// SourceCommit and RunID label the caller's binding; SQL cannot authenticate them.
// Phase is exactly pending or acknowledged; this first slice supports published
// cleanup only. Reason/review/proof are compared privately and never projected.
type Expectation struct {
	SourceCommit, RunID, Phase                           string
	CreationRequestID, RequestID                         string
	ActorID                                              string
	ActorBirth                                           time.Time
	IntegrationID                                        string
	IntegrationBirth                                     time.Time
	RevisionID, CredentialID                             string
	CredentialBirth, CreationBirth                       time.Time
	CreatorProcessID, CreatorGeneration                  string
	CreatorBirth                                         time.Time
	PhysicalObject, OperationProof, ReviewedETag, Reason string
	RunStarted, AsOf                                     time.Time
	RootEpoch                                            uint64
}

func (Expectation) String() string   { return "cleanup evidence expectation (redacted)" }
func (Expectation) GoString() string { return "cleanup evidence expectation (redacted)" }

// Claim matches the private coordinator's exact eleven-field pending contract.
// State is durable: expiry changes PresentedState, never this persisted tuple.
type Claim struct {
	RequestID         string     `json:"request_id"`
	CreationRequestID string     `json:"creation_request_id"`
	State             string     `json:"state"`
	FinishedAt        *time.Time `json:"finished_at"`
	StartedAt         time.Time  `json:"started_at"`
	Deadline          time.Time  `json:"deadline"`
	ObservationState  string     `json:"observation_state"`
	ObserveAuditCount int        `json:"observe_audit_count"`
	ClaimAuditCount   int        `json:"claim_audit_count"`
	RemoteRequestID   string     `json:"remote_request_id"`
	KnownNoEffect     bool       `json:"known_no_effect"`
}

// Evidence contains safe derived facts and private retained validation rows.
// It proves neither a literal remote HTTP204 nor live holder/instance closure.
type Evidence struct {
	SourceCommit    string `json:"source_commit"`
	RunID           string `json:"run_id"`
	PresentedState  string `json:"presented_state"`
	Claim           Claim  `json:"claim"`
	SourceProcesses int    `json:"source_processes"`
	SourceUses      int    `json:"source_uses"`
	rows            snapshot
}

func (Evidence) String() string   { return "cleanup evidence (redacted)" }
func (Evidence) GoString() string { return "cleanup evidence (redacted)" }

type commandRow struct {
	CreationRequestID, RequestID, ActorID     string
	ActorBirth                                time.Time
	IntegrationID                             string
	IntegrationBirth                          time.Time
	RevisionID, OperationProof                string
	ReviewedETag                              string `gorm:"column:reviewed_etag"`
	Reason, State, OwnershipJSON, CleanupJSON string
	RootEpoch                                 uint64
	StartedAt, Deadline                       time.Time
	FinishedAt                                *time.Time
}
type publishedRow struct {
	Command        commandRow `gorm:"embedded"`
	PhysicalObject string
	KnownNoEffect  bool
}
type operationRow struct {
	RequestID, StorageSource, CredentialID string
	CredentialBirth                        time.Time
	IntegrationID                          string
	IntegrationBirth                       *time.Time
	RevisionID, State, WriteJSON, ReadJSON string
	CreatedAt                              time.Time
}
type identityRow struct {
	ID        string
	CreatedAt time.Time
}
type revisionRow struct {
	ID, IntegrationID string
	IntegrationBirth  time.Time
}
type denialRow struct {
	PhysicalObject, CreationRequestID, RequestID string
	RemoteRequestID                              *string
	CreatedAt                                    time.Time
}
type creationUseRow struct {
	CreationRequestID, ProcessID, ProcessGeneration string
	Exposed                                         bool
}
type processRow struct {
	ProcessID, Generation string
	Birth, RegisteredAt   time.Time
	ClosedAt              *time.Time
}
type useRow struct {
	PhysicalObject, ProcessID, Generation string
	Birth                                 time.Time
	Exposed                               bool
	CreatedAt                             time.Time
	JoinedAt                              *time.Time
}
type auditRow struct {
	ID, ActorID, Action, ResourceType, ResourceID string
	DetailsJSON                                   *string
	CreatedAt                                     time.Time
}
type snapshot struct {
	operations                        []operationRow
	published                         []publishedRow
	orphans                           []commandRow
	actors, integrations, credentials []identityRow
	revisions                         []revisionRow
	denials                           []denialRow
	creationUses                      []creationUseRow
	processes                         []processRow
	uses                              []useRow
	audits                            []auditRow
}
type observation struct {
	Attempted  bool             `json:"attempted"`
	Succeeded  bool             `json:"succeeded"`
	DurationMS string           `json:"duration_ms"`
	Failure    *json.RawMessage `json:"failure"`
}
type cleanupObservation struct {
	State       string      `json:"state"`
	Observation observation `json:"observation"`
}

// Project uses one bounded read-only RepeatableRead transaction and always rolls
// it back. The caller owns DB/pool creation and closure and must separately prove
// source/topology identity. SQL uses the borrowed pool; this function creates no
// pool and performs no migration or remote cleanup. Caller logging is replaced
// by GORM's discard logger for this session.
func Project(ctx context.Context, db *gorm.DB, expected Expectation) (e Evidence, err error) {
	if ctx == nil || db == nil || db.Error != nil || !validExpectation(expected) || ctx.Err() != nil {
		return Evidence{}, ErrRejected
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx := db.Session(&gorm.Session{NewDB: true, Logger: logger.Discard}).WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return Evidence{}, ErrRejected
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			e, err = Evidence{}, ErrRejected
		}
		if tx.Rollback().Error != nil || ctx.Err() != nil || err != nil {
			e, err = Evidence{}, ErrRejected
		}
	}()
	s, readErr := readSnapshot(tx, expected)
	if readErr != nil {
		return Evidence{}, ErrRejected
	}
	e = derive(expected, s)
	if Validate(expected, e) != nil {
		return Evidence{}, ErrRejected
	}
	return e, nil
}

func readSnapshot(tx *gorm.DB, e Expectation) (s snapshot, err error) {
	commandColumns := "creation_request_id,request_id,actor_id,actor_birth,integration_id,integration_birth,revision_id,operation_proof,reviewed_etag,reason,state,ownership_json,cleanup_json,root_epoch,started_at,deadline,finished_at"
	reads := []func() error{
		func() error {
			return tx.Table("credential_storage_operations").Select("request_id,storage_source,credential_id,credential_birth,integration_id,integration_birth,revision_id,state,write_json,read_json,created_at").Where("request_id = ?", e.CreationRequestID).Limit(2).Find(&s.operations).Error
		},
		func() error {
			return tx.Table("credential_published_cleanups").Select(commandColumns+",physical_object,known_no_effect").Where("request_id = ? OR creation_request_id = ? OR physical_object = ?", e.RequestID, e.CreationRequestID, e.PhysicalObject).Order("request_id").Limit(3).Find(&s.published).Error
		},
		func() error {
			return tx.Table("provider_credential_cleanups").Select(commandColumns).Where("request_id = ? OR creation_request_id = ?", e.RequestID, e.CreationRequestID).Limit(2).Find(&s.orphans).Error
		},
		func() error {
			return tx.Table("users").Select("id,created_at").Where("id = ?", e.ActorID).Limit(2).Find(&s.actors).Error
		},
		func() error {
			return tx.Table("vault_integrations").Select("id,created_at").Where("id = ?", e.IntegrationID).Limit(2).Find(&s.integrations).Error
		},
		func() error {
			return tx.Table("vault_revisions").Select("id,integration_id,integration_birth").Where("id = ?", e.RevisionID).Limit(2).Find(&s.revisions).Error
		},
		func() error {
			return tx.Table("provider_credentials").Select("id,created_at").Where("id = ?", e.CredentialID).Limit(2).Find(&s.credentials).Error
		},
		func() error {
			return tx.Table("credential_source_denials").Select("physical_object,creation_request_id,request_id,remote_request_id,created_at").Where("physical_object = ? OR creation_request_id = ? OR request_id = ?", e.PhysicalObject, e.CreationRequestID, e.RequestID).Limit(2).Find(&s.denials).Error
		},
		func() error {
			return tx.Table("provider_credential_creation_uses").Select("creation_request_id,process_id,process_generation,exposed").Where("creation_request_id = ?", e.CreationRequestID).Limit(2).Find(&s.creationUses).Error
		},
		func() error {
			return tx.Table("credential_source_processes").Select("process_id,generation,birth,registered_at,closed_at").Order("process_id").Limit(censusLimit + 1).Find(&s.processes).Error
		},
		func() error {
			return tx.Table("credential_source_uses").Select("physical_object,process_id,generation,birth,exposed,created_at,joined_at").Where("physical_object = ?", e.PhysicalObject).Order("process_id").Limit(censusLimit + 1).Find(&s.uses).Error
		},
		func() error {
			return tx.Table("audit_events").Select("id,actor_id,action,resource_type,resource_id,details_json,created_at").Where("resource_type = ? AND resource_id = ? AND action IN ?", "credential", e.CredentialID, []string{"credential.orphan.cleanup.claim", "credential.orphan.cleanup.observe"}).Order("created_at,id").Limit(3).Find(&s.audits).Error
		},
	}
	for _, read := range reads {
		if read() != nil {
			return snapshot{}, ErrRejected
		}
	}
	return s, nil
}

// Validate checks exact canonical identities after DB collation has matched them.
// It rejects missing/extra rows and never treats deadline/absence as remote proof.
func Validate(e Expectation, value Evidence) error {
	if !validExpectation(e) {
		return ErrRejected
	}
	s := value.rows
	if len(s.operations) != 1 || len(s.published) != 1 || len(s.orphans) != 0 || len(s.actors) != 1 || len(s.integrations) != 1 || len(s.revisions) != 1 || len(s.credentials) != 0 || len(s.denials) != 1 || len(s.creationUses) != 1 || len(s.processes) == 0 || len(s.processes) > censusLimit || len(s.uses) == 0 || len(s.uses) > censusLimit {
		return ErrRejected
	}
	op, p, d := s.operations[0], s.published[0], s.denials[0]
	if s.actors[0].ID != e.ActorID || !s.actors[0].CreatedAt.Equal(e.ActorBirth) || s.integrations[0].ID != e.IntegrationID || !s.integrations[0].CreatedAt.Equal(e.IntegrationBirth) || s.revisions[0].ID != e.RevisionID || s.revisions[0].IntegrationID != e.IntegrationID || !s.revisions[0].IntegrationBirth.Equal(e.IntegrationBirth) {
		return ErrRejected
	}
	if op.RequestID != e.CreationRequestID || op.StorageSource != "vault" || op.State != "committed" || op.CredentialID != e.CredentialID || !op.CredentialBirth.Equal(e.CredentialBirth) || !op.CreatedAt.Equal(e.CreationBirth) || op.IntegrationID != e.IntegrationID || op.IntegrationBirth == nil || !op.IntegrationBirth.Equal(e.IntegrationBirth) || op.RevisionID != e.RevisionID {
		return ErrRejected
	}
	if p.Command.CreationRequestID != e.CreationRequestID || p.Command.RequestID != e.RequestID || p.Command.ActorID != e.ActorID || !p.Command.ActorBirth.Equal(e.ActorBirth) || p.Command.IntegrationID != e.IntegrationID || !p.Command.IntegrationBirth.Equal(e.IntegrationBirth) || p.Command.RevisionID != e.RevisionID || p.Command.OperationProof != e.OperationProof || p.Command.ReviewedETag != e.ReviewedETag || p.Command.Reason != e.Reason || p.PhysicalObject != e.PhysicalObject || p.KnownNoEffect || p.Command.RootEpoch != e.RootEpoch || p.Command.State != e.Phase || !inRun(p.Command.StartedAt, e) || !p.Command.Deadline.Equal(p.Command.StartedAt.Add(30*time.Second)) {
		return ErrRejected
	}
	if p.Command.StartedAt.Before(e.CreationBirth) {
		return ErrRejected
	}
	if d.PhysicalObject != e.PhysicalObject || d.CreationRequestID != e.CreationRequestID || d.RequestID != e.RequestID || d.RemoteRequestID == nil || *d.RemoteRequestID != e.RequestID || !d.CreatedAt.Equal(p.Command.StartedAt) {
		return ErrRejected
	}
	var originalWrite, originalRead, ownership observation
	var cleanup cleanupObservation
	if !strictJSON(op.WriteJSON, &originalWrite) || !strictJSON(op.ReadJSON, &originalRead) || !succeeded(originalWrite) || !succeeded(originalRead) || !strictJSON(p.Command.OwnershipJSON, &ownership) || !strictJSON(p.Command.CleanupJSON, &cleanup) {
		return ErrRejected
	}
	if e.Phase == "pending" {
		if p.Command.FinishedAt != nil || !emptyObservation(ownership) || cleanup.State != "not_attempted" || !emptyObservation(cleanup.Observation) {
			return ErrRejected
		}
	} else if p.Command.FinishedAt == nil || !inRun(*p.Command.FinishedAt, e) || p.Command.FinishedAt.Before(p.Command.StartedAt) || !succeeded(ownership) || cleanup.State != "acknowledged" || !succeeded(cleanup.Observation) {
		return ErrRejected
	}
	if !validSources(e, s) || !validAudits(e, s, p) {
		return ErrRejected
	}
	expected := derive(e, s)
	if !reflect.DeepEqual(value.Claim, expected.Claim) || value.PresentedState != expected.PresentedState || value.SourceCommit != e.SourceCommit || value.RunID != e.RunID || value.SourceProcesses != len(s.processes) || value.SourceUses != len(s.uses) {
		return ErrRejected
	}
	return nil
}

// PendingClaim returns only the exact existing F02 consumer contract. A normal
// acknowledged ledger cannot substitute for the retained pending post-ack claim.
func PendingClaim(e Expectation, value Evidence) (Claim, error) {
	if e.Phase != "pending" || Validate(e, value) != nil {
		return Claim{}, ErrRejected
	}
	return value.Claim, nil
}

func derive(e Expectation, s snapshot) Evidence {
	v := Evidence{SourceCommit: e.SourceCommit, RunID: e.RunID, rows: s, SourceProcesses: len(s.processes), SourceUses: len(s.uses)}
	if len(s.published) != 1 {
		return v
	}
	p := s.published[0]
	v.PresentedState = p.Command.State
	if p.Command.State == "pending" && !e.AsOf.Before(p.Command.Deadline) {
		v.PresentedState = "unknown"
	}
	remote := ""
	if len(s.denials) == 1 && s.denials[0].RemoteRequestID != nil {
		remote = *s.denials[0].RemoteRequestID
	}
	state := ""
	var cleanup cleanupObservation
	if strictJSON(p.Command.CleanupJSON, &cleanup) {
		state = cleanup.State
	}
	claim, observe := 0, 0
	for _, row := range s.audits {
		if row.Action == "credential.orphan.cleanup.claim" {
			claim++
		}
		if row.Action == "credential.orphan.cleanup.observe" {
			observe++
		}
	}
	var finishedAt *time.Time
	if p.Command.FinishedAt != nil {
		finished := *p.Command.FinishedAt
		finishedAt = &finished
	}
	v.Claim = Claim{RequestID: p.Command.RequestID, CreationRequestID: p.Command.CreationRequestID, State: p.Command.State, FinishedAt: finishedAt, StartedAt: p.Command.StartedAt, Deadline: p.Command.Deadline, ObservationState: state, ObserveAuditCount: observe, ClaimAuditCount: claim, RemoteRequestID: remote, KnownNoEffect: p.KnownNoEffect}
	return v
}

func validSources(e Expectation, s snapshot) bool {
	creation := s.creationUses[0]
	if creation.CreationRequestID != e.CreationRequestID || creation.ProcessID != e.CreatorProcessID || creation.ProcessGeneration != e.CreatorGeneration {
		return false
	}
	processes := map[string]processRow{}
	for _, p := range s.processes {
		if !identity(p.ProcessID, "ins") || !hexPattern.MatchString(p.Generation) || !inRun(p.Birth, e) || !inRun(p.RegisteredAt, e) || p.RegisteredAt.Before(p.Birth) || (p.ClosedAt != nil && (!inRun(*p.ClosedAt, e) || p.ClosedAt.Before(p.RegisteredAt))) {
			return false
		}
		if _, exists := processes[p.ProcessID]; exists {
			return false
		}
		processes[p.ProcessID] = p
	}
	creator, ok := processes[e.CreatorProcessID]
	if !ok || creator.Generation != e.CreatorGeneration || !creator.Birth.Equal(e.CreatorBirth) || creator.RegisteredAt.After(e.CreationBirth) {
		return false
	}
	uses := map[string]useRow{}
	for _, u := range s.uses {
		p, ok := processes[u.ProcessID]
		if !ok || u.PhysicalObject != e.PhysicalObject || u.Generation != p.Generation || !u.Birth.Equal(p.Birth) || !inRun(u.CreatedAt, e) || u.CreatedAt.Before(p.RegisteredAt) || (p.ClosedAt != nil && u.CreatedAt.After(*p.ClosedAt)) || (u.JoinedAt != nil && (!inRun(*u.JoinedAt, e) || u.JoinedAt.Before(u.CreatedAt))) {
			return false
		}
		if _, exists := uses[u.ProcessID]; exists {
			return false
		}
		uses[u.ProcessID] = u
	}
	if u, ok := uses[e.CreatorProcessID]; !ok || !u.Exposed {
		return false
	}
	for id, p := range processes {
		if u, ok := uses[id]; (!ok || u.JoinedAt == nil) && p.ClosedAt == nil {
			return false
		}
		if e.Phase == "acknowledged" {
			// A later durable join/closure cannot establish that the original
			// remote cleanup drained this generation before it finished. A
			// later shutdown is harmless when the join already preceded finish.
			finished := s.published[0].Command.FinishedAt
			u, ok := uses[id]
			joinedBeforeFinish := ok && u.JoinedAt != nil && !u.JoinedAt.After(*finished)
			closedBeforeFinish := p.ClosedAt != nil && !p.ClosedAt.After(*finished)
			if !joinedBeforeFinish && !closedBeforeFinish {
				return false
			}
		}
	}
	return true
}

func validAudits(e Expectation, s snapshot, p publishedRow) bool {
	count := 1
	if e.Phase == "acknowledged" {
		count = 2
	}
	if len(s.audits) != count {
		return false
	}
	seen := map[string]bool{}
	claim, observe := 0, 0
	for _, a := range s.audits {
		if !identity(a.ID, "aud") || seen[a.ID] || a.ActorID != e.ActorID || a.ResourceType != "credential" || a.ResourceID != e.CredentialID || !inRun(a.CreatedAt, e) || a.CreatedAt.Before(p.Command.StartedAt) || a.DetailsJSON == nil {
			return false
		}
		seen[a.ID] = true
		var detail struct {
			CreationRequestID string `json:"creation_request_id"`
			RequestID         string `json:"request_id"`
		}
		if !strictJSON(*a.DetailsJSON, &detail) || detail.CreationRequestID != e.CreationRequestID || detail.RequestID != e.RequestID {
			return false
		}
		switch a.Action {
		case "credential.orphan.cleanup.claim":
			claim++
			if p.Command.FinishedAt != nil && a.CreatedAt.After(*p.Command.FinishedAt) {
				return false
			}
		case "credential.orphan.cleanup.observe":
			observe++
			if p.Command.FinishedAt == nil || a.CreatedAt.Before(*p.Command.FinishedAt) {
				return false
			}
		default:
			return false
		}
	}
	return claim == 1 && observe == count-1
}

func validExpectation(e Expectation) bool {
	if !sourcePattern.MatchString(e.SourceCommit) || !runPattern.MatchString(e.RunID) || (e.Phase != "pending" && e.Phase != "acknowledged") || !uuidPattern.MatchString(e.CreationRequestID) || !uuidPattern.MatchString(e.RequestID) || e.RequestID == e.CreationRequestID || !identity(e.ActorID, "usr") || !identity(e.IntegrationID, "vlt") || !identity(e.RevisionID, "vlr") || !identity(e.CredentialID, "crd") || !identity(e.CreatorProcessID, "ins") || !hexPattern.MatchString(e.CreatorGeneration) || !hexPattern.MatchString(e.OperationProof) || !hexPattern.MatchString(e.PhysicalObject) || !reviewPattern.MatchString(e.ReviewedETag) || strings.TrimSpace(e.Reason) != e.Reason || len(e.Reason) == 0 || len(e.Reason) > 500 {
		return false
	}
	for _, r := range e.Reason {
		if r < 32 || r == 127 {
			return false
		}
	}
	if !validTime(e.RunStarted) || !validTime(e.AsOf) || e.AsOf.Before(e.RunStarted) || e.AsOf.Sub(e.RunStarted) > 15*time.Minute {
		return false
	}
	for _, birth := range []time.Time{e.ActorBirth, e.IntegrationBirth, e.CredentialBirth, e.CreationBirth, e.CreatorBirth} {
		if !inRun(birth, e) {
			return false
		}
	}
	return !e.CreationBirth.Before(e.CredentialBirth)
}
func identity(s, prefix string) bool {
	return strings.HasPrefix(s, prefix+"_") && identityPattern.MatchString(s)
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 2000 && t.Nanosecond()%1000 == 0 }
func inRun(t time.Time, e Expectation) bool {
	return validTime(t) && !t.Before(e.RunStarted) && !t.After(e.AsOf)
}
func succeeded(o observation) bool {
	return o.Attempted && o.Succeeded && o.Failure == nil && durationPattern.MatchString(o.DurationMS)
}
func emptyObservation(o observation) bool {
	return !o.Attempted && !o.Succeeded && o.Failure == nil && o.DurationMS == "0"
}

// JSON from durable observations/audits is private and bounded. Reject duplicate
// and noncanonical keys recursively before typed decoding (encoding/json alone
// accepts duplicate fields and case-insensitive aliases).
func strictJSON(raw string, target any) bool {
	if len(raw) == 0 || len(raw) > 4096 {
		return false
	}
	var object func(*json.Decoder) bool
	object = func(d *json.Decoder) bool {
		token, err := d.Token()
		if err != nil || token != json.Delim('{') {
			return false
		}
		keys := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || keys[name] {
				return false
			}
			keys[name] = true
			if name == "observation" {
				if !object(d) {
					return false
				}
				continue
			}
			value, err := d.Token()
			if err != nil {
				return false
			}
			switch name {
			case "attempted", "succeeded":
				if _, ok := value.(bool); !ok {
					return false
				}
			case "duration_ms", "state", "creation_request_id", "request_id":
				if _, ok := value.(string); !ok {
					return false
				}
			case "failure":
				if value != nil {
					return false
				}
			default:
				return false
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return false
		}
		return (len(keys) == 4 && keys["attempted"] && keys["succeeded"] && keys["duration_ms"] && keys["failure"]) || (len(keys) == 2 && keys["state"] && keys["observation"]) || (len(keys) == 2 && keys["creation_request_id"] && keys["request_id"])
	}
	d := json.NewDecoder(strings.NewReader(raw))
	if !object(d) {
		return false
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return false
	}
	d = json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target) == nil
}
