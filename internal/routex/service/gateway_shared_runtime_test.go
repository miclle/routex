package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
)

func TestSharedGatewayRuntimeRejectsForeignSpoolAndProcessBeforeSQL(t *testing.T) {
	registration := admissionOwnerFixture()
	journal, err := eventqueue.OpenSharedSpool(t.Context(), filepath.Join(t.TempDir(), "calls.db"), eventqueue.SharedSpoolIdentity{InstallationID: registration.Owner.InstallationID, SpoolID: registration.Owner.SpoolID, ModeRevision: registration.Owner.ModeRevision}, eventqueue.SharedSpoolOptions{Capacity: 1, MaxDataBytes: eventqueue.SharedSpoolMaxDataBytes, DeliveredCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, test := range []struct {
		name   string
		change func(*Service, *admissionOwnerRegistrationReceipt)
	}{
		{"foreign_spool", func(_ *Service, r *admissionOwnerRegistrationReceipt) { r.Owner.SpoolID = "another-spool" }},
		{"stale_mode", func(_ *Service, r *admissionOwnerRegistrationReceipt) { r.Owner.ModeRevision++ }},
		{"another_process", func(s *Service, _ *admissionOwnerRegistrationReceipt) {
			s.instance.id = configurationTestIDV1("ins", 992)
		}},
		{"another_birth", func(s *Service, _ *admissionOwnerRegistrationReceipt) {
			s.instance.startedAt = s.instance.startedAt.Add(time.Microsecond)
		}},
		{"local_selected", func(s *Service, _ *admissionOwnerRegistrationReceipt) { s.admission = &localAdmissionStore{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Service{db: &gorm.DB{}, instance: &systemInstanceLease{id: registration.Owner.IncarnationID, startedAt: registration.InstanceStartedAt}}
			receipt := admissionOwnerRegistrationReceipt{admissionOwnerRegistration: registration, RegisteredAt: registration.InstanceStartedAt.Add(time.Second)}
			test.change(s, &receipt)
			if r, err := newSharedGatewayRuntime(t.Context(), s, journal, receipt, time.Second); err == nil || r != nil {
				t.Fatal("unverified identity published shared runtime", err)
			}
			if s.sharedGateway != nil || s.recorder != nil {
				t.Fatal("constructor partially activated shared or local recording")
			}
		})
	}
}

func TestSharedGatewayFlushRespectsReviewedSpoolCapacity(t *testing.T) {
	for _, capacity := range []int{1, 8, 64, 65} {
		t.Run(strconv.Itoa(capacity), func(t *testing.T) {
			registration := admissionOwnerFixture()
			path := filepath.Join(t.TempDir(), "calls.db")
			journal, err := eventqueue.OpenSharedSpool(t.Context(), path, eventqueue.SharedSpoolIdentity{InstallationID: registration.Owner.InstallationID, SpoolID: registration.Owner.SpoolID, ModeRevision: registration.Owner.ModeRevision}, eventqueue.SharedSpoolOptions{Capacity: capacity, MaxDataBytes: eventqueue.SharedSpoolMaxDataBytes, DeliveredCapacity: capacity})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := journal.Close(); err != nil {
					t.Error(err)
				}
			})
			adapter, err := newDurableSharedCallSpool(journal)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			s := &Service{}
			s.sharedGateway = &sharedGatewayRuntime{service: s, spool: adapter, timeout: time.Second}
			// An empty valid journal needs neither SQL nor reconstructed live
			// authority. The real flush must honor even a one-slot spool.
			if err := s.FlushCallRecorder(t.Context()); err != nil {
				t.Fatal("valid small-capacity delivery read failed", err)
			}
			if _, err := journal.ReadReady(t.Context(), capacity+1); !errors.Is(err, eventqueue.ErrInvalid) {
				t.Fatal("direct spool reads no longer enforce their reviewed bound", err)
			}
			cancelled, cancel := context.WithCancel(t.Context())
			cancel()
			if err := s.FlushCallRecorder(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatal("delivery read ignored cancellation", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("delivery reads mutated the empty owned journal", err)
			}
		})
	}
}
