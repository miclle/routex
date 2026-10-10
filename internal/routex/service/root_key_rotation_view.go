package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

const secretObservationDuration = 300 * time.Second

type SecretRotationStartInput struct {
	RequestID   string `json:"request_id"`
	TargetKeyID string `json:"target_key_id"`
	Reason      string `json:"reason"`
}
type SecretRotationActionInput struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}
type SecretStorePolicyView struct {
	WriteKeyID *string `json:"write_key_id"`
	Epoch      string  `json:"epoch"`
}
type SecretRootKeyView struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Configured bool   `json:"configured"`
	Verified   bool   `json:"verified"`
}
type SecretProcessView struct {
	ID          *string    `json:"id"`
	Verified    bool       `json:"verified"`
	PolicyEpoch *string    `json:"policy_epoch"`
	SnapshotID  *string    `json:"snapshot_id"`
	VerifiedAt  *time.Time `json:"verified_at"`
}
type SecretRotationDomainView struct {
	Code          string `json:"code"`
	Coverage      string `json:"coverage"`
	Scanned       string `json:"scanned"`
	Rewrapped     string `json:"rewrapped"`
	AlreadyTarget string `json:"already_target"`
	Deleted       string `json:"deleted"`
	Changed       string `json:"changed"`
	Blocked       string `json:"blocked"`
}
type SecretRotationView struct {
	ID                    string                     `json:"id"`
	InventoryVersion      int                        `json:"inventory_version"`
	Status                string                     `json:"status"`
	Phase                 string                     `json:"phase"`
	SourceKeyID           string                     `json:"source_key_id"`
	TargetKeyID           string                     `json:"target_key_id"`
	Domains               []SecretRotationDomainView `json:"domains"`
	BlockerCodes          []string                   `json:"blocker_codes"`
	ObservationStartedAt  *time.Time                 `json:"observation_started_at"`
	ObservationEligibleAt *time.Time                 `json:"observation_eligible_at"`
	AllowedActions        []string                   `json:"allowed_actions"`
}
type SecretStoreView struct {
	Mode             string                `json:"mode"`
	InventoryVersion int                   `json:"inventory_version"`
	ObservedAt       time.Time             `json:"observed_at"`
	ReviewETag       string                `json:"review_etag"`
	CanRead          bool                  `json:"can_read"`
	CanRotate        bool                  `json:"can_rotate"`
	Policy           SecretStorePolicyView `json:"policy"`
	Keys             []SecretRootKeyView   `json:"keys"`
	Process          SecretProcessView     `json:"process"`
	Rotation         *SecretRotationView   `json:"rotation"`
}
type SecretRotationReceiptView struct {
	RequestID  string    `json:"request_id"`
	RotationID string    `json:"rotation_id"`
	Action     string    `json:"action"`
	CreatedAt  time.Time `json:"created_at"`
}
type SecretRotationResult struct {
	Receipt            SecretRotationReceiptView `json:"receipt"`
	Committed          bool                      `json:"committed"`
	WritePolicyApplied bool                      `json:"write_policy_applied"`
	PublicationApplied bool                      `json:"publication_applied"`
	ApplicationStatus  string                    `json:"application_status"`
	Rotation           *SecretRotationView       `json:"rotation"`
	Created            bool                      `json:"-"`
}
type rootDomainCounts struct{ Scanned, Rewrapped, AlreadyTarget, Deleted, Changed, Blocked uint64 }

func rootCountsChecked(job entity.SecretRotationJob) (map[string]rootDomainCounts, error) {
	domains := rootInventoryDomains(job.InventoryVersion)
	if len(domains) == 0 || len(job.CountsJSON) > 8192 || !vaultUniqueJSON([]byte(job.CountsJSON)) {
		return nil, secretStoreUnavailable
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(job.CountsJSON), &fields) != nil || fields == nil {
		return nil, secretStoreUnavailable
	}
	result := map[string]rootDomainCounts{}
	for code, raw := range fields {
		if !slices.Contains(domains, code) || string(raw) == "null" {
			return nil, secretStoreUnavailable
		}
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return nil, secretStoreUnavailable
		}
		for k, value := range values {
			if string(value) == "null" || !slices.Contains([]string{"Scanned", "Rewrapped", "AlreadyTarget", "Deleted", "Changed", "Blocked"}, k) {
				return nil, secretStoreUnavailable
			}
		}
		var n rootDomainCounts
		if json.Unmarshal(raw, &n) != nil {
			return nil, secretStoreUnavailable
		}
		result[code] = n
	}
	return result, nil
}
func rootCounts(job entity.SecretRotationJob) map[string]rootDomainCounts {
	result, _ := rootCountsChecked(job)
	return result
}
func rootRotationView(job entity.SecretRotationJob, canRotate bool, eligible bool) *SecretRotationView {
	counts, err := rootCountsChecked(job)
	if err != nil {
		return nil
	}
	view := &SecretRotationView{ID: job.ID, InventoryVersion: job.InventoryVersion, Status: job.Status, Phase: job.Phase, SourceKeyID: job.SourceKeyID, TargetKeyID: job.TargetKeyID, Domains: []SecretRotationDomainView{}, BlockerCodes: []string{}, ObservationStartedAt: job.ObservationStartedAt, AllowedActions: []string{}}
	for _, code := range rootInventoryDomains(job.InventoryVersion) {
		n, seen := counts[code]
		coverage := "not_scanned"
		if seen {
			coverage = "observed"
		}
		view.Domains = append(view.Domains, SecretRotationDomainView{Code: code, Coverage: coverage, Scanned: fmt.Sprint(n.Scanned), Rewrapped: fmt.Sprint(n.Rewrapped), AlreadyTarget: fmt.Sprint(n.AlreadyTarget), Deleted: fmt.Sprint(n.Deleted), Changed: fmt.Sprint(n.Changed), Blocked: fmt.Sprint(n.Blocked)})
	}
	if job.BlockerCode != "" {
		view.BlockerCodes = append(view.BlockerCodes, job.BlockerCode)
	}
	if job.ObservationStartedAt != nil {
		at := job.ObservationStartedAt.Add(secretObservationDuration)
		view.ObservationEligibleAt = &at
	}
	if canRotate {
		if job.Status == "blocked" {
			view.AllowedActions = append(view.AllowedActions, "resume")
		}
		if job.Status != "completed" && job.Status != "rolled_back" {
			view.AllowedActions = append(view.AllowedActions, "rollback")
		}
		if job.Status == "ready" && eligible {
			view.AllowedActions = append(view.AllowedActions, "retire")
		}
	}
	return view
}
func rootAuthorize(tx *gorm.DB, actorID, permission string) (entity.User, error) {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return actor, err
	}
	if actor.Role != entity.RoleAdmin {
		return actor, apperrors.ErrForbidden
	}
	ok, err := exactGovernancePermission(tx, actor, permission)
	if err != nil {
		return actor, err
	}
	if !ok {
		return actor, apperrors.ErrForbidden
	}
	return actor, nil
}
func rootReviewETag(actor string, p entity.SecretWritePolicy, job *entity.SecretRotationJob) string {
	j := ""
	if job != nil {
		j = job.ID + ":" + job.ETag
	}
	return rootHash(actor + ":" + p.ETag + ":" + j)
}
func (s *Service) GetSecretStore(ctx context.Context, actorID string) (*SecretStoreView, error) {
	return s.getSecretStore(ctx, actorID, "")
}
func (s *Service) GetSecretRotation(ctx context.Context, actorID, id string) (*SecretStoreView, error) {
	if !rootRotationID.MatchString(id) {
		return nil, apperrors.ErrBadRequest
	}
	return s.getSecretStore(ctx, actorID, id)
}
func (s *Service) getSecretStore(ctx context.Context, actorID, jobID string) (*SecretStoreView, error) {
	var result *SecretStoreView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := rootAuthorize(tx, actorID, "secrets.read")
		if err != nil {
			return err
		}
		rotate, err := exactGovernancePermission(tx, actor, "secrets.rotate")
		if err != nil {
			return err
		}
		var p entity.SecretWritePolicy
		if err := tx.Take(&p, 1).Error; err != nil {
			return err
		}
		keys := []entity.SecretRootKey{}
		if err := tx.Order("key_id").Find(&keys).Error; err != nil {
			return err
		}
		var job entity.SecretRotationJob
		query := tx.Order("created_at DESC, id DESC")
		if jobID != "" {
			query = personalExact(tx, "id", jobID)
		}
		if err := query.Limit(1).Find(&job).Error; err != nil {
			return err
		}
		if jobID != "" && job.ID != jobID {
			return apperrors.ErrNotFound
		}
		var jp *entity.SecretRotationJob
		if job.ID != "" {
			if _, err := rootCountsChecked(job); err != nil {
				return err
			}
			jp = &job
		}
		view := &SecretStoreView{Mode: "internal", InventoryVersion: rootInventoryVersion, ObservedAt: s.secretNow(), ReviewETag: rootReviewETag(actorID, p, jp), CanRead: true, CanRotate: rotate, Policy: SecretStorePolicyView{p.WriteKeyID, strconv.FormatUint(p.Epoch, 10)}, Keys: []SecretRootKeyView{}}
		var configured []string
		if s.secrets != nil {
			configured = s.secrets.KeyIDs()
		}
		for _, key := range keys {
			known := slices.Contains(configured, key.KeyID)
			verified := false
			if known && key.State != "retired" {
				plain, e := s.secrets.Open(rootProofReference(key.KeyID), key.ProofCiphertext)
				verified = e == nil && plain == rootProofPlaintext
			}
			view.Keys = append(view.Keys, SecretRootKeyView{key.KeyID, key.State, known, verified})
		}
		proof, proofErr := s.rootCurrentProof(tx, p)
		if proofErr == nil {
			epoch := fmt.Sprint(p.Epoch)
			view.Process = SecretProcessView{&proof.ProcessID, true, &epoch, &proof.RuntimeSnapshotID, &proof.VerifiedAt}
		}
		if jp != nil {
			view.Rotation = rootRotationView(*jp, rotate, proofErr == nil && s.rootObservationEligible(*jp, proof))
		}
		result = view
		return nil
	})
	return result, catalogError(err)
}
func (s *Service) rootObservationEligible(job entity.SecretRotationJob, proof entity.SecretProcessVerification) bool {
	if job.InventoryVersion != rootInventoryVersion || proof.InventoryVersion != rootInventoryVersion || job.Domain != len(rootDomains) {
		return false
	}
	counts, err := rootCountsChecked(job)
	if err != nil {
		return false
	}
	for _, code := range rootDomains {
		if _, ok := counts[code]; !ok {
			return false
		}
	}
	now := s.secretNow()
	return job.ObservationStartedAt != nil && job.ObservationLastConfirmedAt != nil && job.VerifiedProcessID == proof.ProcessID && job.VerifiedSnapshotID == proof.RuntimeSnapshotID && now.Sub(*job.ObservationLastConfirmedAt) >= 0 && now.Sub(*job.ObservationLastConfirmedAt) <= 15*time.Second && now.Sub(*job.ObservationStartedAt) >= secretObservationDuration
}

func rootInventoryDomains(v int) []string {
	switch v {
	case 1:
		return rootDomains[:5]
	case 2:
		return rootDomains[:7]
	case 3:
		return rootDomains[:8]
	case rootInventoryVersion:
		return rootDomains
	default:
		return nil
	}
}
func (v SecretRotationDomainView) MarshalJSON() ([]byte, error) {
	type wire struct {
		Code          string  `json:"code"`
		Coverage      string  `json:"coverage"`
		Scanned       *string `json:"scanned"`
		Rewrapped     *string `json:"rewrapped"`
		AlreadyTarget *string `json:"already_target"`
		Deleted       *string `json:"deleted"`
		Changed       *string `json:"changed"`
		Blocked       *string `json:"blocked"`
	}
	w := wire{Code: v.Code, Coverage: v.Coverage}
	if v.Coverage == "observed" {
		w.Scanned = &v.Scanned
		w.Rewrapped = &v.Rewrapped
		w.AlreadyTarget = &v.AlreadyTarget
		w.Deleted = &v.Deleted
		w.Changed = &v.Changed
		w.Blocked = &v.Blocked
	}
	return json.Marshal(w)
}
