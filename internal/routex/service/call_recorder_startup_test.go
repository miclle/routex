package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestLocalRecorderDomainRequiresExplicitCoherentMode(t *testing.T) {
	identity := admissionOwnerFixture().Owner.InstallationID
	local := &entity.AdmissionMode{ID: 1, InstallationID: identity, Mode: "local", Revision: 1}
	for _, test := range []struct {
		name    string
		binding entity.LimitInstallation
		mode    *entity.AdmissionMode
		want    bool
	}{
		{"fresh", entity.LimitInstallation{ID: 1}, nil, true},
		{"fresh_id_commit", entity.LimitInstallation{ID: 1, JournalID: identity}, nil, true},
		{"established_local", entity.LimitInstallation{ID: 1, JournalID: identity, Initialized: true}, local, true},
		{"missing_binding", entity.LimitInstallation{}, nil, false},
		{"foreign_binding_id", entity.LimitInstallation{ID: 2}, nil, false},
		{"uninitialized_mode", entity.LimitInstallation{ID: 1, JournalID: identity}, local, false},
		{"initialized_missing_mode", entity.LimitInstallation{ID: 1, JournalID: identity, Initialized: true}, nil, false},
		{"initialized_missing_identity", entity.LimitInstallation{ID: 1, Initialized: true}, local, false},
		{"bad_identity", entity.LimitInstallation{ID: 1, JournalID: "foreign"}, nil, false},
		{"shared", entity.LimitInstallation{ID: 1, JournalID: identity, Initialized: true}, &entity.AdmissionMode{ID: 1, InstallationID: identity, Mode: "shared", Revision: 2}, false},
		{"alias_mode", entity.LimitInstallation{ID: 1, JournalID: identity, Initialized: true}, &entity.AdmissionMode{ID: 1, InstallationID: identity, Mode: "LOCAL", Revision: 1}, false},
		{"foreign_mode", entity.LimitInstallation{ID: 1, JournalID: identity, Initialized: true}, &entity.AdmissionMode{ID: 1, InstallationID: "led_foreign", Mode: "local", Revision: 1}, false},
		{"foreign_mode_id", entity.LimitInstallation{ID: 1, JournalID: identity, Initialized: true}, &entity.AdmissionMode{ID: 2, InstallationID: identity, Mode: "local", Revision: 1}, false},
		{"invalid_revision", entity.LimitInstallation{ID: 1, JournalID: identity, Initialized: true}, &entity.AdmissionMode{ID: 1, InstallationID: identity, Mode: "local"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := localRecorderDomainValid(test.binding, test.mode); got != test.want {
				t.Fatal("local startup mode predicate", got, test.want)
			}
		})
	}
}

func TestLocalRecorderUnreadableAuthorityDoesNotOpenPath(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-created")
	path := filepath.Join(parent, "queue.db")
	for _, ctx := range []context.Context{nil, context.Background()} {
		q, err := (&Service{}).openLocalRecorderJournal(ctx, path)
		if q != nil || !errors.Is(err, eventqueue.ErrInvalid) {
			t.Fatal("unreadable authority fell back to local file", err)
		}
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed authority check created a local ledger", err)
	}
}
