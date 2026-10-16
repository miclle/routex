package service

import (
	"path/filepath"
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
