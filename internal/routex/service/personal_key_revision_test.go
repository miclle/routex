package service

import (
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestMemberKeyMutationGateBlocksPublicationAndNewWriterUntilReduction(t *testing.T) {
	svc := &Service{runtime: &gatewayRuntime{}}
	release := svc.pinPersonalKeyMutation()
	if svc.runtime.publication.TryRLock() {
		svc.runtime.publication.RUnlock()
		t.Fatal("publication passed an unfinished Key state writer")
	}
	if svc.runtime.publication.TryLock() {
		svc.runtime.publication.Unlock()
		t.Fatal("new writer passed an unfinished older state writer")
	}
	svc.InvalidateRuntimeKey("key-retained")
	if !runtimeDenied(&svc.runtime.deniedKeys, "key-retained") {
		t.Fatal("reduction not installed while state writer is pinned")
	}
	release()
	release() // Deferred exceptional exits and explicit refresh release share one handle.
	if !svc.runtime.publication.TryRLock() {
		t.Fatal("refresh would deadlock after writer release")
	}
	svc.runtime.publication.RUnlock()
	if !svc.runtime.publication.TryLock() {
		t.Fatal("next writer never admitted after reduction")
	}
	svc.runtime.publication.Unlock()
	nilRelease := (&Service{}).pinPersonalKeyMutation()
	nilRelease()
	nilRelease()
}

func TestMemberKeyRevisionDoesNotAuthorizeHistoricalMissingIdentity(t *testing.T) {
	key := entity.APIKey{ID: memberKeyTestID(t, "key"), UserID: memberKeyTestID(t, "usr"), Status: entity.KeyDisabled}
	if _, found := runtimeMemberKeyStates([]entity.APIKey{key})[key.ID]; found {
		t.Fatal("historical blank revision became published proof")
	}
	previous := ""
	for range 100 {
		if err := advancePersonalKeyRevision(&key); err != nil {
			t.Fatal(err)
		}
		if !memberKeyRevision.MatchString(key.LifecycleRevision) || key.LifecycleRevision == previous {
			t.Fatal("revision reused a lifecycle identity")
		}
		previous = key.LifecycleRevision
	}
}
