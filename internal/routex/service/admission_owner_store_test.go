package service

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func admissionOwnerFixture() admissionOwnerRegistration {
	return admissionOwnerRegistration{RegistrationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Owner: sharedAdmissionOwner{InstallationID: "led_01jjjjjjjjjjjjjjjjjjjjjjjj", ModeRevision: 2, SpoolID: "spool_one", IncarnationID: "ins_01jjjjjjjjjjjjjjjjjjjjjjjj", Fence: 1}, InstanceStartedAt: time.Date(2026, 10, 15, 0, 0, 0, 123000, time.UTC)}
}
func TestAdmissionOwnerInputBoundaries(t *testing.T) {
	good := admissionOwnerFixture()
	if !admissionOwnerRegistrationValid(good) {
		t.Fatal("valid captured owner rejected")
	}
	for _, change := range []struct {
		name  string
		apply func(*admissionOwnerRegistration)
	}{
		{"registration_case", func(x *admissionOwnerRegistration) { x.RegistrationID = strings.ToUpper(x.RegistrationID) }},
		{"uuid_version", func(x *admissionOwnerRegistration) { x.RegistrationID = "aaaaaaaa-aaaa-1aaa-8aaa-aaaaaaaaaaaa" }},
		{"foreign_installation", func(x *admissionOwnerRegistration) { x.Owner.InstallationID = "installation_shared" }},
		{"process_alias", func(x *admissionOwnerRegistration) { x.Owner.IncarnationID = strings.ToUpper(x.Owner.IncarnationID) }},
		{"spool_alias", func(x *admissionOwnerRegistration) { x.Owner.SpoolID = "Spool_one" }},
		{"spool_overflow", func(x *admissionOwnerRegistration) { x.Owner.SpoolID = strings.Repeat("s", 65) }},
		{"revision_zero", func(x *admissionOwnerRegistration) { x.Owner.ModeRevision = 0 }},
		{"revision_overflow", func(x *admissionOwnerRegistration) { x.Owner.ModeRevision = uint64(math.MaxInt64) + 1 }},
		{"foreign_fence", func(x *admissionOwnerRegistration) { x.Owner.Fence = 2 }},
		{"missing_birth", func(x *admissionOwnerRegistration) { x.InstanceStartedAt = time.Time{} }},
		{"lossy_birth", func(x *admissionOwnerRegistration) { x.InstanceStartedAt = x.InstanceStartedAt.Add(time.Nanosecond) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			bad := good
			change.apply(&bad)
			if admissionOwnerRegistrationValid(bad) {
				t.Fatal("invalid registration admitted")
			}
		})
	}
	var store *admissionOwnerStore
	if _, err := store.RegisterOwner(context.Background(), good); !errors.Is(err, errAdmissionOwnerInvalid) {
		t.Fatal("nil storage admitted", err)
	}
	if _, err := store.FenceOwner(context.Background(), admissionOwnerFence{FencingID: good.RegistrationID, Owner: good.Owner, InstanceStartedAt: good.InstanceStartedAt}); !errors.Is(err, errAdmissionOwnerInvalid) {
		t.Fatal("nil fencing storage admitted", err)
	}
}
func TestAdmissionOwnerHistoricalReceiptAndFence(t *testing.T) {
	in := admissionOwnerFixture()
	row := entity.AdmissionSpoolOwner{InstallationID: in.Owner.InstallationID, SpoolID: in.Owner.SpoolID, ModeID: 1, ModeRevision: 2, RegistrationID: in.RegistrationID, IncarnationID: in.Owner.IncarnationID, InstanceStartedAt: in.InstanceStartedAt, RegisteredAt: in.InstanceStartedAt.Add(time.Second), RegisteredFence: 1, CurrentFence: 1}
	if !admissionOwnerRowValid(row) {
		t.Fatal("registered receipt invalid")
	}
	original := admissionOwnerRegistrationView(row)
	uuid := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	at := row.RegisteredAt.Add(time.Second)
	row.CurrentFence, row.FencingID, row.FencedAt = 2, &uuid, &at
	if !admissionOwnerRowValid(row) || !reflect.DeepEqual(original, admissionOwnerRegistrationView(row)) {
		t.Fatal("terminal fence reinterpreted original receipt")
	}
	fence := admissionOwnerFenceView(row)
	expectedUUID, expectedAt := uuid, at
	*row.FencingID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	*row.FencedAt = at.Add(time.Second)
	if fence.FencingID != expectedUUID || !fence.FencedAt.Equal(expectedAt) || fence.ResultFence != 2 || fence.Owner.Fence != 1 {
		t.Fatal("receipt aliases mutable row or changes original fence")
	}
	for _, change := range []func(*entity.AdmissionSpoolOwner){
		func(x *entity.AdmissionSpoolOwner) { x.CurrentFence = 3 },
		func(x *entity.AdmissionSpoolOwner) { x.FencedAt = nil },
		func(x *entity.AdmissionSpoolOwner) { x.FencingID = nil },
		func(x *entity.AdmissionSpoolOwner) {
			early := x.RegisteredAt.Add(-time.Microsecond)
			x.FencedAt = &early
		},
		func(x *entity.AdmissionSpoolOwner) { x.RegisteredFence = 2 },
	} {
		bad := row
		change(&bad)
		if admissionOwnerRowValid(bad) {
			t.Fatal("invalid terminal receipt admitted")
		}
	}
	reborn := in
	reborn.InstanceStartedAt = reborn.InstanceStartedAt.Add(time.Microsecond)
	if admissionOwnerMatches(row, reborn.Owner, reborn.InstanceStartedAt) {
		t.Fatal("same ID reborn process matched old owner")
	}
}
func TestAdmissionOwnerStorageFaultClassification(t *testing.T) {
	for _, err := range []error{gorm.ErrDuplicatedKey, gorm.ErrRecordNotFound, errAdmissionOwnerConflict} {
		if !errors.Is(admissionOwnerStorageError(err), errAdmissionOwnerConflict) {
			t.Fatal("conflict lost")
		}
	}
	if !errors.Is(admissionOwnerStorageError(context.Canceled), context.Canceled) {
		t.Fatal("cancellation lost")
	}
	fault := errors.New("commit outcome unavailable")
	result := admissionOwnerStorageError(fault)
	if !errors.Is(result, errAdmissionOwnerUnavailable) || !errors.Is(result, fault) {
		t.Fatal("unknown commit became successful/absent")
	}
	if admissionOwnerStorageError(nil) != nil {
		t.Fatal("success changed")
	}
}
