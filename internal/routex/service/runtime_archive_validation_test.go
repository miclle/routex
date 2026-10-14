package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestRuntimeArchiveValidationRejectsUnboundInput(t *testing.T) {
	var svc *Service
	for _, identity := range []string{"", "rcv_other", configurationTestIDV1("rcv", 1) + "\n"} {
		if result, err := svc.ValidateRuntimeArchive(t.Context(), nil, identity); result != nil || !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("invalid target reached archive read", err)
		}
	}
	if result, err := svc.ValidateRuntimeArchive(t.Context(), nil, configurationTestIDV1("rcv", 1)); result != nil || !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("anonymous read admitted", err)
	}
}

func TestRuntimeArchiveValidationClosedProjection(t *testing.T) {
	view := RuntimeArchiveValidation{Scope: "archive_integrity_only", ArchiveID: configurationTestIDV1("rcv", 1), ArchiveCreatedAt: time.Now().UTC(), CodecVersion: 1, ObservedAt: time.Now().UTC(), Integrity: "authenticated"}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"scope": true, "archive_id": true, "archive_created_at": true, "codec_version": true, "observed_at": true, "integrity": true}
	got := map[string]bool{}
	for name := range fields {
		got[name] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("integrity response exposes non-allowlisted facts")
	}
}
