package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

const credentialDrainCensusLimit = 1000
const credentialDrainObjectLimit = 5000

func sourceProcess(row entity.SystemInstance) entity.CredentialSourceProcess {
	return entity.CredentialSourceProcess{ProcessID: row.ID, Generation: rootHash(row.ID + ":" + row.LeaseToken), Birth: row.StartedAt, RegisteredAt: row.StartedAt}
}
func sourceProcessValid(row entity.CredentialSourceProcess) bool {
	return systemInstanceIDPattern.MatchString(row.ProcessID) && personalModelETag(row.Generation) && connectionMetadataBirth(row.Birth) && connectionMetadataBirth(row.RegisteredAt) && !row.RegisteredAt.Before(row.Birth) && (row.ClosedAt == nil || (connectionMetadataBirth(*row.ClosedAt) && !row.ClosedAt.Before(row.RegisteredAt)))
}
func sourceUseValid(use entity.CredentialSourceUse, process entity.CredentialSourceProcess, object string) bool {
	return sourceProcessValid(process) && personalModelETag(object) && use.PhysicalObject == object && use.ProcessID == process.ProcessID && use.Generation == process.Generation && use.Birth.Equal(process.Birth) && connectionMetadataBirth(use.CreatedAt) && !use.CreatedAt.Before(process.RegisteredAt) && (process.ClosedAt == nil || !use.CreatedAt.After(*process.ClosedAt)) && (use.JoinedAt == nil || (connectionMetadataBirth(*use.JoinedAt) && !use.JoinedAt.Before(use.CreatedAt)))
}
func sourceDenialValid(row entity.CredentialSourceDenial) bool {
	return personalModelETag(row.PhysicalObject) && credentialReplacementRequestID.MatchString(row.CreationRequestID) && credentialReplacementRequestID.MatchString(row.RequestID) && row.RequestID != row.CreationRequestID && connectionMetadataBirth(row.CreatedAt) && (row.RemoteRequestID == nil || (credentialReplacementRequestID.MatchString(*row.RemoteRequestID) && *row.RemoteRequestID != row.CreationRequestID))
}

func (s *Service) sourceProcessCurrent(tx *gorm.DB, process entity.CredentialSourceProcess) error {
	if process.ClosedAt != nil {
		return vaultUnavailable
	}
	var instance entity.SystemInstance
	if err := personalExact(vaultDB(tx), "id", process.ProcessID).Take(&instance).Error; err != nil {
		return err
	}
	if instance.ID != process.ProcessID || !instance.StartedAt.Equal(process.Birth) || rootHash(instance.ID+":"+instance.LeaseToken) != process.Generation || instance.Role != systemInstanceRole || instance.StoppedAt != nil || instance.RetiredAt != nil || !instance.LeaseExpiresAt.After(s.instanceNow().UTC()) {
		return vaultUnavailable
	}
	return nil
}

// Caller holds the governance lock. Registration precedes all remote source
// preparation. Existing denials are checked by the exposure transaction, so
// startup need not scan or hydrate historical objects. Owning processes later
// acknowledge zero exposure only after their local admissions are closed.
func (s *Service) registerCredentialSourceProcess(tx *gorm.DB, instance entity.SystemInstance) error {
	process := sourceProcess(instance)
	if !sourceProcessValid(process) {
		return vaultUnavailable
	}
	return vaultDB(tx).Create(&process).Error
}

func (s *Service) currentSourceProcess() (entity.CredentialSourceProcess, error) {
	s.instanceMu.RLock()
	defer s.instanceMu.RUnlock()
	if s.instance == nil || s.credentialSources.processAdmissionClosed() {
		return entity.CredentialSourceProcess{}, vaultUnavailable
	}
	p := entity.CredentialSourceProcess{ProcessID: s.instance.id, Generation: rootHash(s.instance.id + ":" + s.instance.token), Birth: s.instance.startedAt, RegisteredAt: s.instance.startedAt}
	if !sourceProcessValid(p) {
		return p, vaultUnavailable
	}
	return p, nil
}

// All control-plane remote preparations use this interlock before first
// exposure. No inference-path call reaches it. DB commit precedes the local
// holder acquisition: a concurrent denial must join that holder or close its
// admissions before acknowledging zero.
func (s *Service) exposeCredentialSource(ctx context.Context, object string) error {
	p, err := s.currentSourceProcess()
	if err != nil || !personalModelETag(object) {
		return vaultUnavailable
	}
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var process entity.CredentialSourceProcess
		if err := personalExact(vaultDB(tx), "process_id", p.ProcessID).Take(&process).Error; err != nil {
			return err
		}
		if !sourceProcessValid(process) || process.Generation != p.Generation || !process.Birth.Equal(p.Birth) {
			return vaultUnavailable
		}
		if err := s.sourceProcessCurrent(tx, process); err != nil {
			return err
		}
		var denial entity.CredentialSourceDenial
		err := personalExact(vaultDB(tx), "physical_object", object).Take(&denial).Error
		if err == nil {
			return vaultUnavailable
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var use entity.CredentialSourceUse
		err = personalExact(vaultDB(tx), "physical_object", object).Where("process_id = ?", p.ProcessID).Take(&use).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			use = entity.CredentialSourceUse{PhysicalObject: object, ProcessID: p.ProcessID, Generation: p.Generation, Birth: p.Birth, Exposed: true, CreatedAt: s.secretNow().UTC().Truncate(time.Microsecond)}
			return vaultDB(tx).Create(&use).Error
		}
		if err != nil {
			return err
		}
		if !sourceUseValid(use, process, object) || !use.Exposed || use.JoinedAt != nil {
			return vaultUnavailable
		}
		return nil
	})
}

// Only the owning process may acknowledge its joins. Waiting occurs outside
// every DB transaction, publication mutex and instance mutex. Cancelled or
// poisoned holders leave no acknowledgement; no timeout implies drain.
func (s *Service) reconcileCredentialDenial(ctx context.Context, denial entity.CredentialSourceDenial) error {
	if !sourceDenialValid(denial) {
		return vaultUnavailable
	}
	p, err := s.currentSourceProcess()
	if err != nil {
		return err
	}
	if err = s.credentialSources.closeAndWait(ctx, credentialSourceKey{PhysicalObject: denial.PhysicalObject}); err != nil {
		return err
	}
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var current entity.CredentialSourceDenial
		if err := personalExact(vaultDB(tx), "physical_object", denial.PhysicalObject).Take(&current).Error; err != nil {
			return err
		}
		if !sourceDenialSame(current, denial) {
			return catalogConflict
		}
		var process entity.CredentialSourceProcess
		if err := personalExact(vaultDB(tx), "process_id", p.ProcessID).Take(&process).Error; err != nil {
			return err
		}
		if !sourceProcessValid(process) || process.Generation != p.Generation || !process.Birth.Equal(p.Birth) {
			return vaultUnavailable
		}
		if err := s.sourceProcessCurrent(tx, process); err != nil {
			return err
		}
		var use entity.CredentialSourceUse
		err := personalExact(vaultDB(tx), "physical_object", denial.PhysicalObject).Where("process_id = ?", p.ProcessID).Take(&use).Error
		now := s.secretNow().UTC().Truncate(time.Microsecond)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			use = entity.CredentialSourceUse{PhysicalObject: denial.PhysicalObject, ProcessID: p.ProcessID, Generation: p.Generation, Birth: p.Birth, CreatedAt: now, JoinedAt: &now}
			return vaultDB(tx).Create(&use).Error
		}
		if err != nil {
			return err
		}
		if !sourceUseValid(use, process, denial.PhysicalObject) {
			return vaultUnavailable
		}
		if use.JoinedAt != nil {
			return nil
		}
		updated := personalExact(vaultDB(tx).Model(&entity.CredentialSourceUse{}), "physical_object", denial.PhysicalObject).Where("process_id = ? AND generation = ? AND joined_at IS NULL", p.ProcessID, p.Generation).Update("joined_at", now)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return catalogConflict
		}
		return nil
	})
}

// Short best-effort heartbeat reconciliation never extends authorization leases.
// A busy object does not prevent another denied object or unrelated startup.
func (s *Service) reconcileCredentialDenials(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	var rows []entity.CredentialSourceDenial
	if err := s.authDB(ctx).Order("physical_object").Limit(credentialDrainObjectLimit + 1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > credentialDrainObjectLimit {
		return vaultUnavailable
	}
	p, err := s.currentSourceProcess()
	if err != nil {
		return err
	}
	var acknowledged []entity.CredentialSourceUse
	if err := personalExact(vaultDB(s.authDB(ctx)), "process_id", p.ProcessID).Where("joined_at IS NOT NULL").Limit(credentialDrainObjectLimit + 1).Find(&acknowledged).Error; err != nil {
		return err
	}
	if len(acknowledged) > credentialDrainObjectLimit {
		return vaultUnavailable
	}
	known := map[string]entity.CredentialSourceUse{}
	for _, use := range acknowledged {
		if !sourceUseValid(use, p, use.PhysicalObject) {
			return vaultUnavailable
		}
		known[use.PhysicalObject] = use
	}
	// Close every observed object before attempting any bounded DB acknowledgment.
	for _, row := range rows {
		if !sourceDenialValid(row) {
			return vaultUnavailable
		}
		s.credentialSources.closeOnly(row.PhysicalObject)
	}
	for _, row := range rows {
		if use, ok := known[row.PhysicalObject]; ok && !use.JoinedAt.Before(row.CreatedAt) {
			continue
		}
		if s.credentialSources.joined(row.PhysicalObject) {
			if err := s.reconcileCredentialDenial(ctx, row); err != nil {
				return err
			}
		}
	}
	return nil
}

type credentialDrainSnapshot struct {
	Instances   []entity.SystemInstance
	Processes   []entity.CredentialSourceProcess
	Uses        []entity.CredentialSourceUse
	CreationUse entity.ProviderCredentialCreationUse
}

func (s *Service) credentialDrainSnapshot(tx *gorm.DB, op entity.CredentialStorageOperation, object string) (credentialDrainSnapshot, error) {
	var snap credentialDrainSnapshot
	if err := vaultDB(tx).Order("id").Limit(credentialDrainCensusLimit + 1).Find(&snap.Instances).Error; err != nil {
		return snap, err
	}
	if err := vaultDB(tx).Order("process_id").Limit(credentialDrainCensusLimit + 1).Find(&snap.Processes).Error; err != nil {
		return snap, err
	}
	if err := personalExact(vaultDB(tx), "physical_object", object).Order("process_id").Limit(credentialDrainCensusLimit + 1).Find(&snap.Uses).Error; err != nil {
		return snap, err
	}
	if err := personalExact(vaultDB(tx), "creation_request_id", op.RequestID).Take(&snap.CreationUse).Error; err != nil {
		return snap, err
	}
	return snap, nil
}

// Complete history is mandatory, including retained retired instances. A missing
// instance row for a capable historical process does not remove its exposure.
func credentialDrainSnapshotValid(snap credentialDrainSnapshot, op entity.CredentialStorageOperation, object string, joined bool) bool {
	if len(snap.Processes) == 0 || len(snap.Processes) > credentialDrainCensusLimit || len(snap.Instances) > credentialDrainCensusLimit || len(snap.Uses) > credentialDrainCensusLimit {
		return false
	}
	processes := map[string]entity.CredentialSourceProcess{}
	for _, p := range snap.Processes {
		if !sourceProcessValid(p) {
			return false
		}
		if _, exists := processes[p.ProcessID]; exists {
			return false
		}
		processes[p.ProcessID] = p
	}
	for _, instance := range snap.Instances {
		p, ok := processes[instance.ID]
		if !ok || !p.Birth.Equal(instance.StartedAt) || p.Generation != rootHash(instance.ID+":"+instance.LeaseToken) || p.ClosedAt != nil && (instance.StoppedAt == nil || !instance.StoppedAt.Equal(*p.ClosedAt)) {
			return false
		}
	}
	creator, ok := processes[snap.CreationUse.ProcessID]
	if !ok || snap.CreationUse.CreationRequestID != op.RequestID || snap.CreationUse.ProcessGeneration != creator.Generation || creator.RegisteredAt.After(op.CreatedAt) {
		return false
	}
	uses := map[string]entity.CredentialSourceUse{}
	for _, use := range snap.Uses {
		p, ok := processes[use.ProcessID]
		if !ok || !sourceUseValid(use, p, object) {
			return false
		}
		if _, exists := uses[use.ProcessID]; exists {
			return false
		}
		uses[use.ProcessID] = use
	}
	if use, ok := uses[creator.ProcessID]; !ok || !use.Exposed {
		return false
	}
	if joined {
		for id := range processes {
			if use, ok := uses[id]; (!ok || use.JoinedAt == nil) && processes[id].ClosedAt == nil {
				return false
			}
		}
	}
	return true
}

// Explicit private fields are hashed: the entity JSON redacts them intentionally.
func credentialDrainSnapshotProof(snap credentialDrainSnapshot) string {
	parts := []string{snap.CreationUse.CreationRequestID, snap.CreationUse.ProcessID, snap.CreationUse.ProcessGeneration}
	if snap.CreationUse.Exposed {
		parts = append(parts, "exposed")
	}
	for _, p := range snap.Processes {
		parts = append(parts, "p:"+p.ProcessID+":"+p.Generation+":"+p.Birth.UTC().Format(time.RFC3339Nano)+":"+p.RegisteredAt.UTC().Format(time.RFC3339Nano))
		if p.ClosedAt != nil {
			parts = append(parts, "closed:"+p.ProcessID+":"+p.ClosedAt.UTC().Format(time.RFC3339Nano))
		}
	}
	for _, i := range snap.Instances {
		parts = append(parts, "i:"+i.ID+":"+i.LeaseToken+":"+i.StartedAt.UTC().Format(time.RFC3339Nano))
	}
	for _, u := range snap.Uses {
		part := "u:" + u.PhysicalObject + ":" + u.ProcessID + ":" + u.Generation + ":" + u.Birth.UTC().Format(time.RFC3339Nano) + ":" + u.CreatedAt.UTC().Format(time.RFC3339Nano)
		if u.Exposed {
			part += ":exposed"
		}
		if u.JoinedAt != nil {
			part += ":" + u.JoinedAt.UTC().Format(time.RFC3339Nano)
		}
		parts = append(parts, part)
	}
	sort.Strings(parts)
	return rootHash(strings.Join(parts, "\n"))
}

// Preparation/recovery is control-plane work, never inference-path I/O.
func (s *Service) credentialAdmissionOperation(ctx context.Context, rev entity.VaultRevision, ref entity.CredentialVaultReference, method, purpose string) (*credentialFiniteOperation, error) {
	ticket, err := s.credentialSources.processAcquire()
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			ticket.release()
		}
	}()
	object, err := credentialPhysicalObject(rev, ref.ReferenceID)
	if err != nil {
		return nil, err
	}
	if err := s.exposeCredentialSource(ctx, object); err != nil {
		return nil, err
	}
	operation, err := s.credentialFiniteOperationTicket(rev, ref, method, purpose, false, ticket)
	transferred = err == nil
	return operation, err
}

// A published cleanup command is the sole permitted use after durable denial.
// Its proof is rechecked immediately before client construction; it cannot be
// reused for resolution, creation, recovery or native routing.
func (s *Service) publishedCleanupOperation(ctx context.Context, rev entity.VaultRevision, ref entity.CredentialVaultReference, method string, op entity.CredentialStorageOperation, ledger entity.ProviderCredentialCleanup) (*credentialFiniteOperation, error) {
	ticket, err := s.credentialSources.processAcquire()
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			ticket.release()
		}
	}()
	object, err := credentialPhysicalObject(rev, ref.ReferenceID)
	if err != nil {
		return nil, err
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var denial entity.CredentialSourceDenial
		if err := personalExact(vaultDB(tx), "physical_object", object).Take(&denial).Error; err != nil {
			return err
		}
		if !sourceDenialValid(denial) || denial.RemoteRequestID != nil || denial.CreationRequestID != op.RequestID {
			return catalogConflict
		}
		snap, err := s.credentialDrainSnapshot(tx, op, object)
		if err != nil {
			return err
		}
		if !credentialDrainSnapshotValid(snap, op, object, true) || !s.credentialSources.joined(object) {
			return vaultUnavailable
		}
		_, _, current, err := cleanupTargetTx(tx, ledger.ActorID, ledger.IntegrationID, op.RequestID, true)
		if err != nil {
			return err
		}
		if cleanupOperationProof(current) != ledger.OperationProof || s.secretEpoch() != ledger.RootEpoch {
			return catalogConflict
		}
		physical, _, code, err := s.publishedCleanupPreview(tx, current, true)
		if err != nil {
			return err
		}
		if code != "" || physical != object {
			return catalogConflict
		}
		return s.claimPublishedRemoteCurrent(tx, op, ledger, object)
	})
	if err != nil {
		return nil, err
	}
	operation, err := s.credentialFiniteOperationTicket(rev, ref, method, "cleanup", true, ticket)
	transferred = err == nil
	return operation, err
}

func sourceDenialSame(a, b entity.CredentialSourceDenial) bool {
	if a.PhysicalObject != b.PhysicalObject || a.CreationRequestID != b.CreationRequestID || a.RequestID != b.RequestID || !a.CreatedAt.Equal(b.CreatedAt) || (a.RemoteRequestID == nil) != (b.RemoteRequestID == nil) {
		return false
	}
	return a.RemoteRequestID == nil || *a.RemoteRequestID == *b.RemoteRequestID
}

// Historical joined generations establish drain, not this caller's authority.
// The caller holds the governance lock and has just rechecked actor/source and
// complete joins. The instance mutex is used only for local captures, never
// held across database work, holder waiting or SDK calls.
func (s *Service) claimPublishedRemoteCurrent(tx *gorm.DB, op entity.CredentialStorageOperation, ledger entity.ProviderCredentialCleanup, object string) error {
	captured, err := s.currentSourceProcess()
	if err != nil {
		return err
	}
	var registered entity.CredentialSourceProcess
	if err := personalExact(vaultDB(tx), "process_id", captured.ProcessID).Take(&registered).Error; err != nil {
		return err
	}
	if !sourceProcessValid(registered) || registered.ProcessID != captured.ProcessID || registered.Generation != captured.Generation || !registered.Birth.Equal(captured.Birth) || !registered.RegisteredAt.Equal(captured.RegisteredAt) {
		return vaultUnavailable
	}
	if err := s.sourceProcessCurrent(tx, registered); err != nil {
		return err
	}
	// Stop/replacement may clear the local capability during the DB read.
	current, err := s.currentSourceProcess()
	if err != nil || current.ProcessID != captured.ProcessID || current.Generation != captured.Generation || !current.Birth.Equal(captured.Birth) || !current.RegisteredAt.Equal(captured.RegisteredAt) {
		return vaultUnavailable
	}
	return s.claimPublishedRemote(tx, op, ledger, object)
}
