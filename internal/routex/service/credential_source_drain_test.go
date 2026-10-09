package service

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func drainSnapshotFixture() (credentialDrainSnapshot, entity.CredentialStorageOperation, string) {
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	i := entity.SystemInstance{ID: "ins_00000000000000000000000000", LeaseToken: "lck_00000000000000000000000000", StartedAt: at}
	p := sourceProcess(i)
	object := rootHash("exact-physical-object")
	op := entity.CredentialStorageOperation{RequestID: "11111111-1111-4111-8111-111111111111", CreatedAt: at.Add(time.Second)}
	use := entity.CredentialSourceUse{PhysicalObject: object, ProcessID: p.ProcessID, Generation: p.Generation, Birth: p.Birth, Exposed: true, CreatedAt: op.CreatedAt}
	snap := credentialDrainSnapshot{Instances: []entity.SystemInstance{i}, Processes: []entity.CredentialSourceProcess{p}, Uses: []entity.CredentialSourceUse{use}, CreationUse: entity.ProviderCredentialCreationUse{CreationRequestID: op.RequestID, ProcessID: p.ProcessID, ProcessGeneration: p.Generation}}
	return snap, op, object
}

func TestCredentialSourceDrainHistoryRequiresCompleteOriginalGeneration(t *testing.T) {
	for _, fault := range []string{"valid", "old_process", "missing_registration", "missing_original_use", "birth", "generation", "late_registration", "duplicate", "foreign_use", "overflow", "retired_unproven"} {
		t.Run(fault, func(t *testing.T) {
			snap, op, object := drainSnapshotFixture()
			switch fault {
			case "old_process":
				snap.CreationUse.ProcessID = "ins_10000000000000000000000000"
			case "missing_registration":
				snap.Processes = nil
			case "missing_original_use":
				snap.Uses = nil
			case "birth":
				snap.Uses[0].Birth = snap.Uses[0].Birth.Add(time.Microsecond)
			case "generation":
				snap.Uses[0].Generation = rootHash("other")
			case "late_registration":
				snap.Processes[0].RegisteredAt = op.CreatedAt.Add(time.Microsecond)
			case "duplicate":
				snap.Processes = append(snap.Processes, snap.Processes[0])
			case "foreign_use":
				snap.Uses[0].PhysicalObject = rootHash("other")
			case "overflow":
				for range credentialDrainCensusLimit {
					snap.Processes = append(snap.Processes, snap.Processes[0])
				}
			case "retired_unproven":
				old := snap.Instances[0]
				old.ID = "ins_10000000000000000000000000"
				old.RetiredAt = &old.StartedAt
				old.StoppedAt = &old.StartedAt
				old.LeaseExpiresAt = old.StartedAt
				snap.Instances = append(snap.Instances, old)
			}
			if credentialDrainSnapshotValid(snap, op, object, false) != (fault == "valid") {
				t.Fatal("historical uncertainty treated as current proof")
			}
		})
	}
}

func TestCredentialSourceDrainEveryGenerationMustJoinAndReviewBindsExposure(t *testing.T) {
	snap, op, object := drainSnapshotFixture()
	initial := credentialDrainSnapshotProof(snap)
	if credentialDrainSnapshotValid(snap, op, object, true) {
		t.Fatal("missing join accepted")
	}
	at := op.CreatedAt.Add(time.Second)
	snap.Uses[0].JoinedAt = &at
	if !credentialDrainSnapshotValid(snap, op, object, true) || initial == credentialDrainSnapshotProof(snap) {
		t.Fatal("joined history or review proof missing")
	}
	other := snap.Processes[0]
	other.ProcessID = "ins_10000000000000000000000000"
	other.Generation = rootHash("other-generation")
	snap.Processes = append(snap.Processes, other)
	if credentialDrainSnapshotValid(snap, op, object, true) {
		t.Fatal("unexposed/offline process inferred joined")
	}
	zero := entity.CredentialSourceUse{PhysicalObject: object, ProcessID: other.ProcessID, Generation: other.Generation, Birth: other.Birth, CreatedAt: at, JoinedAt: &at}
	snap.Uses = append(snap.Uses, zero)
	if !credentialDrainSnapshotValid(snap, op, object, true) {
		t.Fatal("exact zero-exposure denial acknowledgment rejected")
	}
	before := credentialDrainSnapshotProof(snap)
	snap.Uses[1].Exposed = true
	if before == credentialDrainSnapshotProof(snap) {
		t.Fatal("review did not bind exposure")
	}
	// Retaining capability and exposure after an instance row is retired/removed
	// does not erase its required join.
	snap.Instances = nil
	snap.Uses[1].JoinedAt = nil
	if credentialDrainSnapshotValid(snap, op, object, true) {
		t.Fatal("removed instance erased exposure")
	}
}

func TestCredentialSourceDrainLocalCloseJoinsBodyAndNeverReopens(t *testing.T) {
	var r credentialSourceHolders
	key := credentialSourceKey{PhysicalObject: rootHash("shared-object"), CredentialID: "original"}
	h, err := r.acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	body := h.body(io.NopCloser(strings.NewReader("original response")))
	if r.closeOnly(key.PhysicalObject) || r.joined(key.PhysicalObject) {
		t.Fatal("active native body inferred drained")
	}
	alias := key
	alias.CredentialID = "another-logical-revision"
	if _, err := r.acquire(alias); err == nil {
		t.Fatal("physical alias reopened")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r.closeAndWait(ctx, key) == nil {
		t.Fatal("cancelled active join succeeded")
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if !r.joined(key.PhysicalObject) || r.closeAndWait(context.Background(), key) != nil {
		t.Fatal("joined body not acknowledged")
	}
	if _, err := r.acquire(key); err == nil {
		t.Fatal("zero-holder tombstone reopened")
	}
}

func TestCredentialSourceDrainPoisonBeforeFiniteRelease(t *testing.T) {
	var r credentialSourceHolders
	key := credentialSourceKey{PhysicalObject: rootHash("finite-object")}
	h, err := r.acquire(key)
	if err != nil {
		t.Fatal(err)
	}
	r.closeOnly(key.PhysicalObject)
	h.poison()
	h.release()
	if r.joined(key.PhysicalObject) || r.closeOnly(key.PhysicalObject) || r.closeAndWait(context.Background(), key) == nil {
		t.Fatal("failed Close became a successful drain")
	}
}
