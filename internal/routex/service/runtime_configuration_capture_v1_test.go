package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func configurationCaptureFixtureV1(t *testing.T) (*Service, *runtimeData) {
	t.Helper()
	data := configurationFixtureDataV1()
	source, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	auth.SourceDigest = source
	auth.installationProjectionDigest, err = runtimeAuthorizationProjection(t.Context(), auth)
	if err != nil {
		t.Fatal(err)
	}
	r := &gatewayRuntime{done: make(chan struct{})}
	r.auth.Store(auth)
	r.routes.Store(&runtimeRoutes{ID: configurationTestIDV1("cfg", 1), Digest: source, PublishedAt: time.Now().UTC().Add(-time.Second)})
	s := &Service{runtime: r}
	s.storeRuntimeStatus(time.Now().UTC(), "")
	return s, data
}

func configurationCaptureTicketV1(t *testing.T, s *Service, data *runtimeData) *runtimeConfigurationSourceTicketV1 {
	t.Helper()
	ticket := s.runtimeConfigurationSourceTicketV1(t.Context(), data, s.runtime.routes.Load(), s.runtime.auth.Load(), s.runtime.epoch.Load(), s.runtime.status.Load())
	if ticket == nil {
		t.Fatal("complete-source prerequisite absent")
	}
	return ticket
}

func configurationCaptureFactsV1(t *testing.T, capture *runtimeConfigurationCaptureV1) *runtimeConfigurationFactsV1 {
	t.Helper()
	if capture == nil {
		t.Fatal("complete historical capture absent")
	}
	facts, err := reconstructRuntimeConfigurationV1(t.Context(), capture.canonical, capture.contentDigest)
	if err != nil {
		t.Fatal("capture cannot reconstruct complete source", err)
	}
	return facts
}

func TestRuntimeConfigurationCaptureV1DetachedCompleteHistoricalSource(t *testing.T) {
	s, data := configurationCaptureFixtureV1(t)
	ticket := configurationCaptureTicketV1(t, s, data)
	r := s.runtime
	auth, routes, status, epoch := r.auth.Load(), r.routes.Load(), r.status.Load(), r.epoch.Load()
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), r, ticket)
	capture := r.configurationCapture.latest.Load()
	facts := configurationCaptureFactsV1(t, capture)
	if capture.sequence != ticket.sequence || capture.snapshotID != routes.ID || !capture.routesPublishedAt.Equal(routes.PublishedAt) || !capture.capturedAt.Equal(ticket.capturedAt) || capture.sourceDigest != auth.SourceDigest || capture.authorizationDigest != auth.installationProjectionDigest || capture.networkDigest != facts.Document.NetworkPolicyDigest || len(facts.UnresolvedSecrets) != 2 {
		t.Fatal("source metadata, dependencies or historical observation changed")
	}
	before := slices.Clone(capture.canonical)
	data.Keys[0].Name = "later source edit"
	data.TeamSessionData.Teams[0].Name = "later Team name"
	delete(data.LimitPolicies, limitAccount("user", data.Users[0].ID))
	if !bytes.Equal(before, capture.canonical) || !reflect.DeepEqual(facts, configurationCaptureFactsV1(t, capture)) {
		t.Fatal("later source mutation changed detached historical facts")
	}
	for _, excluded := range []string{"excluded-user-password-sentinel", "excluded-credential-envelope-sentinel", "excluded-egress-envelope-sentinel", "excluded-diagnostic-sentinel"} {
		if bytes.Contains(capture.canonical, []byte(excluded)) {
			t.Fatal("raw secret/diagnostic source retained")
		}
	}
	//lint:ignore SA9005 Assert that the private capture cannot leak through default JSON serialization.
	//nolint:staticcheck // Intentionally verify that private capture fields are not serialized.
	private, err := json.Marshal(capture)
	if err != nil || string(private) != "{}" {
		t.Fatal("capture exposed source through default JSON", err)
	}
	if r.auth.Load() != auth || r.routes.Load() != routes || r.status.Load() != status || r.epoch.Load() != epoch {
		t.Fatal("capture changed serving publication")
	}
}

func TestRuntimeConfigurationCaptureV1RejectedSourceTickets(t *testing.T) {
	for _, name := range []string{"mixed_sources", "failed_status", "stale_status", "disabled_status", "unready_status", "wrong_status_snapshot", "wrong_status_birth", "wrong_status_lease", "stale_auth", "stale_routes", "changed_epoch", "unknown_projection", "expired_auth", "stopped", "cancelled", "nil_data", "oversized_bootstrap_proxies"} {
		t.Run(name, func(t *testing.T) {
			s, data := configurationCaptureFixtureV1(t)
			r := s.runtime
			auth, routes, status, epoch := r.auth.Load(), r.routes.Load(), r.status.Load(), r.epoch.Load()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch name {
			case "mixed_sources":
				auth.SourceDigest = strings.Repeat("f", 64)
			case "failed_status":
				status.ErrorCode = "invalid_configuration"
			case "stale_status":
				r.status.Store(&RuntimeStatus{Enabled: true, Ready: true})
			case "disabled_status":
				status.Enabled = false
			case "unready_status":
				status.Ready = false
			case "wrong_status_snapshot":
				status.SnapshotID = configurationTestIDV1("cfg", 2)
			case "wrong_status_birth":
				status.PublishedAt = nil
			case "wrong_status_lease":
				status.AuthorizationValidUntil = nil
			case "stale_auth":
				replacement := *auth
				r.auth.Store(&replacement)
			case "stale_routes":
				replacement := *routes
				r.routes.Store(&replacement)
			case "changed_epoch":
				r.epoch.Add(1)
			case "unknown_projection":
				auth.installationProjectionDigest = ""
			case "expired_auth":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "stopped":
				close(r.done)
			case "cancelled":
				cancel()
			case "nil_data":
				data = nil
			case "oversized_bootstrap_proxies":
				s.trustedProxies = make([]netip.Prefix, 129)
			}
			if got := s.runtimeConfigurationSourceTicketV1(ctx, data, routes, auth, epoch, status); got != nil || r.configurationCapture.sequence.Load() != 0 || r.configurationCapture.latest.Load() != nil {
				t.Fatal("incomplete/stale source created a ticket or capture")
			}
		})
	}
}

func TestRuntimeConfigurationCaptureV1FreshAuthorizationKeepsSourceDigestScope(t *testing.T) {
	s, firstData := configurationCaptureFixtureV1(t)
	first := configurationCaptureTicketV1(t, s, firstData)
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), s.runtime, first)
	before := s.runtime.configurationCapture.latest.Load()
	secondData := configurationFixtureDataV1()
	secondData.Users[0].Disabled = true
	source, err := runtimeDigest(secondData)
	if err != nil || source != first.sourceDigest {
		t.Fatal("authorization-only change altered legacy route source", err)
	}
	auth := buildRuntimeAuthorization(secondData, time.Now().Add(time.Minute))
	auth.SourceDigest = source
	auth.installationProjectionDigest, err = runtimeAuthorizationProjection(t.Context(), auth)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.auth.Store(auth)
	s.storeRuntimeStatus(time.Now().UTC(), "")
	second := configurationCaptureTicketV1(t, s, secondData)
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), s.runtime, second)
	after := s.runtime.configurationCapture.latest.Load()
	if after == before || after.snapshotID != before.snapshotID || after.sourceDigest != before.sourceDigest || after.contentDigest == before.contentDigest || after.authorizationDigest == before.authorizationDigest || !configurationCaptureFactsV1(t, after).Document.Source.Users[0].Disabled {
		t.Fatal("route digest substituted for complete source identity")
	}
}

func TestRuntimeConfigurationCaptureV1LeaseRenewalAndOutOfOrderHistory(t *testing.T) {
	s, data := configurationCaptureFixtureV1(t)
	first := configurationCaptureTicketV1(t, s, data)
	auth := *s.runtime.auth.Load()
	auth.ValidUntil = auth.ValidUntil.Add(time.Second)
	s.runtime.auth.Store(&auth)
	s.storeRuntimeStatus(time.Now().UTC(), "")
	second := configurationCaptureTicketV1(t, s, data)
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), s.runtime, second)
	newer := s.runtime.configurationCapture.latest.Load()
	configurationCaptureFactsV1(t, newer)
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), s.runtime, first)
	if s.runtime.configurationCapture.latest.Load() != newer || newer.sequence != second.sequence {
		t.Fatal("late earlier ticket replaced newer captured history")
	}
	other, unchangedData := configurationCaptureFixtureV1(t)
	unchanged := configurationCaptureTicketV1(t, other, unchangedData)
	other.prepareRuntimeConfigurationCaptureV1(t.Context(), other.runtime, unchanged)
	if other.runtime.configurationCapture.latest.Load().contentDigest != newer.contentDigest {
		t.Fatal("lease/process observation changed logical content identity")
	}
	// Expiry after coherent observation removes no historical authority: there
	// was none. Original immutable ticket still reconstructs the earlier source.
	auth.ValidUntil = time.Now().Add(-time.Second)
	s.runtime.auth.Store(&auth)
	s.runtime.epoch.Add(1)
	third := *first
	third.sequence = second.sequence + 1
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), s.runtime, &third)
	if got := s.runtime.configurationCapture.latest.Load(); got == newer || got.contentDigest != newer.contentDigest {
		t.Fatal("later live revocation retroactively changed complete historical source")
	}
}

type configurationCaptureCancelContextV1 struct {
	context.Context
	calls atomic.Int64
	at    int64
}

func (c *configurationCaptureCancelContextV1) Err() error {
	if c.calls.Add(1) >= c.at {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestRuntimeConfigurationCaptureV1PreparationRejectsWithoutPublicationChanges(t *testing.T) {
	for _, name := range []string{"cancelled", "cancel_during_preflight", "cancel_after_freeze", "cancel_at_final_publication", "overflow_bytes", "overflow_entries", "invalid_codec_source", "busy_slot", "stopped", "replaced_runtime", "foreign_ticket", "missing_sequence", "busy_capture_publication"} {
		t.Run(name, func(t *testing.T) {
			s, data := configurationCaptureFixtureV1(t)
			r := s.runtime
			ticket := configurationCaptureTicketV1(t, s, data)
			auth, routes, status := r.auth.Load(), r.routes.Load(), r.status.Load()
			ctx := t.Context()
			switch name {
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "cancel_during_preflight":
				ctx = &configurationCaptureCancelContextV1{Context: ctx, at: 20}
			case "cancel_after_freeze":
				counter := &configurationCaptureCancelContextV1{Context: ctx, at: 1<<63 - 1}
				if !runtimeConfigurationCaptureSourceBudgetV1(counter, data) {
					t.Fatal("preflight positive prerequisite absent")
				}
				ctx = &configurationCaptureCancelContextV1{Context: ctx, at: counter.calls.Load() + 3}
			case "cancel_at_final_publication":
				other, otherData := configurationCaptureFixtureV1(t)
				counter := &configurationCaptureCancelContextV1{Context: ctx, at: 1<<63 - 1}
				other.prepareRuntimeConfigurationCaptureV1(counter, other.runtime, configurationCaptureTicketV1(t, other, otherData))
				configurationCaptureFactsV1(t, other.runtime.configurationCapture.latest.Load())
				ctx = &configurationCaptureCancelContextV1{Context: ctx, at: counter.calls.Load()}
			case "overflow_bytes":
				data.Providers[0].Name = strings.Repeat("x", runtimeConfigurationMaxBytesV1)
			case "overflow_entries":
				data.Scopes = make([]entity.APIKeyModel, runtimeConfigurationMaxEntriesV1+1)
			case "invalid_codec_source":
				data.Users[0].ID = "unknown"
			case "busy_slot":
				r.configurationCapture.preparing.Store(true)
			case "stopped":
				r.configurationCapture.stop()
			case "replaced_runtime":
				s.runtime = &gatewayRuntime{}
			case "foreign_ticket":
				other, otherData := configurationCaptureFixtureV1(t)
				ticket = configurationCaptureTicketV1(t, other, otherData)
			case "missing_sequence":
				ticket.sequence = 0
			case "busy_capture_publication":
				r.configurationCapture.publication.Lock()
				defer r.configurationCapture.publication.Unlock()
			}
			s.prepareRuntimeConfigurationCaptureV1(ctx, r, ticket)
			if r.configurationCapture.latest.Load() != nil || r.auth.Load() != auth || r.routes.Load() != routes || r.status.Load() != status {
				t.Fatal("rejected preparation published partial source or changed live state")
			}
			if name != "busy_slot" && r.configurationCapture.preparing.Load() {
				t.Fatal("failed preparation retained its exclusive slot")
			}
		})
	}
}

func TestRuntimeConfigurationCaptureV1PreparationDoesNotBorrowPublicationLocksOrDatabase(t *testing.T) {
	s, data := configurationCaptureFixtureV1(t)
	ticket := configurationCaptureTicketV1(t, s, data)
	r := s.runtime
	r.refreshMu.Lock()
	r.publication.Lock()
	r.mu.Lock()
	defer r.refreshMu.Unlock()
	defer r.publication.Unlock()
	defer r.mu.Unlock()
	// No DB or secret store exists, and independently held publisher locks cannot
	// delay this pure-source work or block immediate tombstone installation.
	s.InvalidateRuntimeUser(data.Users[0].ID)
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), r, ticket)
	configurationCaptureFactsV1(t, r.configurationCapture.latest.Load())
	if !runtimeDenied(&r.deniedUsers, data.Users[0].ID) {
		t.Fatal("preparation cleared a current denial")
	}
	r.configurationCapture.stop()
	if r.configurationCapture.latest.Load() != nil {
		t.Fatal("shutdown retained historical source")
	}
	s.prepareRuntimeConfigurationCaptureV1(t.Context(), r, ticket)
	if r.configurationCapture.latest.Load() != nil {
		t.Fatal("shutdown source was restored by a late ticket")
	}
}

func TestRuntimeConfigurationCaptureV1RealRefreshAndFailedRoutes(t *testing.T) {
	s, fixture, _, state := providerMetadataSQLService(t)
	users := map[string]entity.User{}
	n := 0
	for _, u := range fixture.data.users {
		n++
		u.ID = configurationTestIDV1("usr", n)
		users[u.ID] = u
	}
	fixture.data.users = users
	state.base.provider.ID = configurationTestIDV1("prv", 1)
	state.base.row.ID = configurationTestIDV1("con", 1)
	state.base.row.ProviderID = state.base.provider.ID
	if err := s.RefreshRuntime(t.Context()); err != nil {
		t.Fatal("real successful publication failed", err)
	}
	before := s.runtime.configurationCapture.latest.Load()
	configurationCaptureFactsV1(t, before)
	if before.snapshotID != s.runtime.routes.Load().ID {
		t.Fatal("capture did not belong to successful real publication")
	}
	if err := s.RefreshRuntime(t.Context()); err != nil {
		t.Fatal("real unchanged refresh failed", err)
	}
	renewed := s.runtime.configurationCapture.latest.Load()
	if renewed == before || renewed.contentDigest != before.contentDigest || renewed.snapshotID != before.snapshotID {
		t.Fatal("ordinary authorization renewal changed complete logical content")
	}
	beforeAuth, beforeRoutes := s.runtime.auth.Load(), s.runtime.routes.Load()
	state.base.provider.Enabled = false
	if err := s.db.Callback().Query().After("gorm:query").Register("test:capture_invalid_routes", func(tx *gorm.DB) {
		if tx.Statement.Table == "model_provider_bindings" {
			if rows, ok := tx.Statement.Dest.(*[]entity.ModelProviderBinding); ok {
				*rows = []entity.ModelProviderBinding{{ID: configurationTestIDV1("bnd", 1), ModelID: configurationTestIDV1("mdl", 1), ProviderModelID: configurationTestIDV1("pmd", 1), Weight: 100}}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshRuntime(t.Context()); !errors.Is(err, runtimeUnavailable) {
		t.Fatal("invalid routes accepted", err)
	}
	if s.runtime.auth.Load() == beforeAuth || s.runtime.auth.Load().Providers[state.base.provider.ID].Enabled || s.runtime.routes.Load() != beforeRoutes || s.runtime.status.Load().ErrorCode != "invalid_configuration" || s.runtime.configurationCapture.latest.Load() != renewed {
		t.Fatal("failed routes altered capture or prevented fresh denial publication")
	}
}

func TestRuntimeConfigurationCaptureV1NetworkFingerprint(t *testing.T) {
	base := runtimeConfigurationNetworkPolicyV1{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.1.2.3/8"), netip.MustParsePrefix("2001:db8::1/32")}}
	digest, ok := runtimeConfigurationNetworkDigestV1(base)
	if !ok {
		t.Fatal("valid network policy rejected")
	}
	canonical := base
	canonical.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8")}
	if got, valid := runtimeConfigurationNetworkDigestV1(canonical); !valid || got != digest {
		t.Fatal("prefix order, duplicate or host bits changed policy fingerprint")
	}
	for _, name := range []string{"upstream", "egress", "storage", "proxy_boundary", "address_family"} {
		t.Run(name, func(t *testing.T) {
			changed := base
			switch name {
			case "upstream":
				changed.PrivateUpstream = true
			case "egress":
				changed.PrivateEgress = true
			case "storage":
				changed.PrivateStorage = true
			case "proxy_boundary":
				changed.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/9")}
			case "address_family":
				changed.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("::ffff:10.0.0.0/104")}
			}
			if got, valid := runtimeConfigurationNetworkDigestV1(changed); !valid || got == digest {
				t.Fatal("distinct network policy shared a fingerprint")
			}
		})
	}
	for _, prefixes := range [][]netip.Prefix{{{}}, make([]netip.Prefix, 129)} {
		if got, valid := runtimeConfigurationNetworkDigestV1(runtimeConfigurationNetworkPolicyV1{TrustedProxies: prefixes}); valid || got != "" {
			t.Fatal("invalid/unbounded network policy returned a fingerprint")
		}
	}
	if absent, valid := runtimeConfigurationNetworkDigestV1(runtimeConfigurationNetworkPolicyV1{}); !valid {
		t.Fatal("default policy rejected")
	} else if empty, valid := runtimeConfigurationNetworkDigestV1(runtimeConfigurationNetworkPolicyV1{TrustedProxies: []netip.Prefix{}}); !valid || absent != empty {
		t.Fatal("nil versus empty proxy configuration changed actual policy")
	}
	s, data := configurationCaptureFixtureV1(t)
	before := configurationCaptureTicketV1(t, s, data)
	s.allowPrivateSMTP = true
	s.runtimeRefreshInterval = 2 * time.Second
	if after := configurationCaptureTicketV1(t, s, data); !reflect.DeepEqual(before.network, after.network) {
		t.Fatal("SMTP or publisher cadence entered Gateway network fingerprint")
	}
}
