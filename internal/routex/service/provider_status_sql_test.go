package service

import (
	"context"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"reflect"
	"testing"
	"time"
)

func TestProviderStatusSQLIndependentAuthorityAndExactIdentity(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		prepare func(*rolesSQLFixture, *roleDefinitionSQLControl, *providerMetadataSQLState)
		want    error
	}{
		{"read denied", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState) {
			f.deny["providers.read"] = true
		}, apperrors.ErrForbidden},
		{"actor alias", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState) {
			f.actorAlias = true
		}, apperrors.ErrUnauthorized},
		{"provider alias", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, s *providerMetadataSQLState) {
			s.base.aliasProvider = true
		}, apperrors.ErrNotFound},
		{"pending actor", func(f *rolesSQLFixture, c *roleDefinitionSQLControl, _ *providerMetadataSQLState) {
			roleDefinitionSQLPending(f, c)
		}, apperrors.ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, c, state := providerMetadataSQLService(t)
			tc.prepare(f, c, state)
			got, err := s.GetProviderStatus(ctx, "usr_admin", state.base.provider.ID)
			if got != nil || err != tc.want || len(f.writes) != 0 {
				t.Fatal(got, err)
			}
		})
	}
	t.Run("reader cannot write", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		f.deny["providers.write"] = true
		row, err := s.GetProviderStatus(ctx, "usr_admin", state.base.provider.ID)
		if err != nil || row.CanEdit {
			t.Fatal(row, err)
		}
		got, err := s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, ProviderStatusInput{Reason: "Reviewed"})
		if got != nil || err != apperrors.ErrForbidden || len(f.writes) != 0 {
			t.Fatal(got, err)
		}
	})
	t.Run("precommit conflict retains known rejection", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		state.base.provider.Enabled = true
		row, err := s.GetProviderStatus(ctx, "usr_admin", state.base.provider.ID)
		if err != nil {
			t.Fatal(err)
		}
		state.base.provider.Name = "Concurrent metadata"
		before := state.base.provider
		got, err := s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, ProviderStatusInput{Reason: "Reviewed"})
		if got != nil || err != catalogConflict || len(f.writes) != 0 || len(f.data.audits) != 0 || !reflect.DeepEqual(before, state.base.provider) {
			t.Fatal("precommit conflict became unknown or mutated", got, err)
		}
	})
	t.Run("writer needs no read", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		state.base.provider.Enabled = true
		row, err := s.GetProviderStatus(ctx, "usr_admin", state.base.provider.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.deny["providers.read"] = true
		got, err := s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, ProviderStatusInput{Reason: "Reviewed"})
		if err != nil || got == nil || !got.RuntimeApplied || !got.Changed || got.Provider.Enabled {
			t.Fatal(got, err)
		}
	})
}
func TestProviderStatusSQLAuditRollbackAndCurrentOnlyPublicationRetry(t *testing.T) {
	ctx := context.Background()
	t.Run("audit rollback", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		state.base.provider.Enabled = true
		original := state.base.provider
		row, _ := s.GetProviderStatus(ctx, "usr_admin", original.ID)
		f.failAudit = true
		got, err := s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, ProviderStatusInput{Reason: "Reviewed"})
		if got != nil || err == nil || !reflect.DeepEqual(original, state.base.provider) || len(f.data.audits) != 0 || runtimeDenied(&s.runtime.deniedProviders, row.ID) {
			t.Fatal(got, err, state.base.provider)
		}
	})
	t.Run("closed publication and retry", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		state.base.provider.Enabled = true
		original := state.base.provider
		connection := state.base.row
		row, _ := s.GetProviderStatus(ctx, "usr_admin", original.ID)
		close(s.runtime.done)
		input := ProviderStatusInput{Reason: "Disable after review"}
		got, err := s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, input)
		if got != nil || err != providerMetadataUnavailable || state.base.provider.Enabled || state.base.provider.ETag == original.ETag || len(f.data.audits) != 1 || !runtimeDenied(&s.runtime.deniedProviders, row.ID) {
			t.Fatal(got, err)
		}
		got, err = s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, input)
		if got != nil || err != providerMetadataUnavailable || len(f.data.audits) != 1 {
			t.Fatal(got, err)
		}
		s.runtime = &gatewayRuntime{done: make(chan struct{})}
		got, err = s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, input)
		if err != nil || got == nil || got.Changed || !got.RuntimeApplied || len(f.data.audits) != 1 || !reflect.DeepEqual(connection, state.base.row) {
			t.Fatal(got, err)
		}
		if _, ok := providerStatusAuditProjection(f.data.audits[0]); !ok {
			t.Fatal("invalid typed audit")
		}
	})
	for _, fault := range []string{"actor disabled", "actor birth", "provider birth", "write revoked", "later status", "stopped runtime"} {
		t.Run(fault, func(t *testing.T) {
			s, f, _, state := providerMetadataSQLService(t)
			state.base.provider.Enabled = true
			row, _ := s.GetProviderStatus(ctx, "usr_admin", state.base.provider.ID)
			var committedEnabled bool
			var committedRevision string
			var committedRecorded bool
			state.afterCommit = func(state *providerMetadataSQLState, f *rolesSQLFixture) {
				committedEnabled, committedRevision, committedRecorded = state.base.provider.Enabled, state.base.provider.ETag, true
				switch fault {
				case "actor disabled":
					u := f.data.users["usr_admin"]
					u.Disabled = true
					f.data.users[u.ID] = u
				case "actor birth":
					u := f.data.users["usr_admin"]
					u.CreatedAt = u.CreatedAt.Add(time.Millisecond)
					f.data.users[u.ID] = u
				case "provider birth":
					state.base.provider.CreatedAt = state.base.provider.CreatedAt.Add(time.Millisecond)
				case "write revoked":
					f.deny["providers.write"] = true
				case "stopped runtime":
					close(s.runtime.done)
				case "later status":
					state.base.provider.Enabled = true
					state.base.provider.ETag = "rev_other"
				}
			}
			got, err := s.WriteProviderStatus(ctx, "usr_admin", row.ID, row.ETag, ProviderStatusInput{Reason: "Reviewed"})
			if got != nil || err != providerMetadataUnavailable || len(f.data.audits) != 1 || !committedRecorded || committedEnabled || committedRevision == "" || committedRevision == "0" {
				t.Fatal("postcommit failure lost uncertainty or durable revision", got, err, committedRecorded, committedEnabled, committedRevision)
			}
			detail, ok := providerStatusAuditProjection(f.data.audits[0])
			if !ok {
				t.Fatal("postcommit failure lost typed audit")
			}
			audit := detail.(providerStatusAudit)
			if !audit.Before || audit.After || audit.Reason != "Reviewed" {
				t.Fatal("postcommit failure rewrote original audited intent", audit)
			}
		})
	}
	t.Run("status invalidates name review", func(t *testing.T) {
		s, _, _, state := providerMetadataSQLService(t)
		state.base.provider.Enabled = true
		name, _ := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
		status, _ := s.GetProviderStatus(ctx, "usr_admin", state.base.provider.ID)
		if _, err := s.WriteProviderStatus(ctx, "usr_admin", status.ID, status.ETag, ProviderStatusInput{Reason: "Reviewed"}); err != nil {
			t.Fatal(err)
		}
		got, err := s.WriteProviderMetadata(ctx, "usr_admin", name.ID, name.ETag, ProviderMetadataInput{Name: "Changed", Reason: "Reviewed"})
		if got != nil || err != catalogConflict {
			t.Fatal(got, err)
		}
	})
}
