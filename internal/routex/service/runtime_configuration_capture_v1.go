package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// A capture is complete historical local source, never serving authority. Only
// canonical detached bytes survive preparation; no clients or raw secret rows
// are retained. Bytes remain private and immutable after publication.
type runtimeConfigurationCaptureV1 struct {
	sequence            uint64
	snapshotID          string
	routesPublishedAt   time.Time
	capturedAt          time.Time
	sourceDigest        string
	authorizationDigest string
	networkDigest       string
	contentDigest       string
	canonical           []byte
}

type runtimeConfigurationCaptureStateV1 struct {
	publication sync.Mutex
	preparing   atomic.Bool
	stopped     atomic.Bool
	sequence    atomic.Uint64
	latest      atomic.Pointer[runtimeConfigurationCaptureV1]
}

func (c *runtimeConfigurationCaptureStateV1) stop() {
	c.publication.Lock()
	defer c.publication.Unlock()
	c.stopped.Store(true)
	c.latest.Store(nil)
}

// Source ownership is transferred only after the publisher's final source
// mutation. The caller holds its normal publication locks and owns the exact
// original repeatable-read data, not a subsequent database reconstruction.
type runtimeConfigurationSourceTicketV1 struct {
	sequence            uint64
	runtime             *gatewayRuntime
	data                *runtimeData
	snapshotID          string
	routesPublishedAt   time.Time
	capturedAt          time.Time
	sourceDigest        string
	authorizationDigest string
	network             runtimeConfigurationNetworkPolicyV1
}

type runtimeConfigurationNetworkPolicyV1 struct {
	PrivateUpstream bool
	PrivateEgress   bool
	PrivateStorage  bool
	TrustedProxies  []netip.Prefix
}

func (s *Service) runtimeConfigurationSourceTicketV1(ctx context.Context, data *runtimeData, routes *runtimeRoutes, auth *runtimeAuthorization, epoch uint64, status *RuntimeStatus) *runtimeConfigurationSourceTicketV1 {
	if s == nil || ctx == nil || data == nil || s.runtime == nil || !s.runtimeInstallationCaptureCurrent(ctx, routes, auth, epoch, status) || s.runtime.configurationCapture.stopped.Load() {
		return nil
	}
	if len(s.trustedProxies) > 128 {
		return nil
	}
	if !status.Enabled || !status.Ready || status.SnapshotID != routes.ID || status.PublishedAt == nil || !status.PublishedAt.Equal(routes.PublishedAt) || status.AuthorizationValidUntil == nil || !status.AuthorizationValidUntil.Equal(auth.ValidUntil) {
		return nil
	}
	return &runtimeConfigurationSourceTicketV1{
		sequence: s.runtime.configurationCapture.sequence.Add(1), runtime: s.runtime, data: data,
		snapshotID: routes.ID, routesPublishedAt: routes.PublishedAt.UTC(),
		capturedAt:   time.Now().UTC().Truncate(time.Microsecond),
		sourceDigest: auth.SourceDigest, authorizationDigest: auth.installationProjectionDigest,
		network: runtimeConfigurationNetworkPolicyV1{
			PrivateUpstream: s.allowPrivateUpstream, PrivateEgress: s.allowPrivateEgress,
			PrivateStorage: s.allowPrivateStorage, TrustedProxies: slices.Clone(s.trustedProxies),
		},
	}
}

func runtimeConfigurationCaptureLiveV1(ctx context.Context, r *gatewayRuntime) bool {
	if ctx == nil || ctx.Err() != nil || r == nil || r.configurationCapture.stopped.Load() {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

// This synchronous best-effort seam has one nonblocking preparation slot and
// no queue or worker. Failure changes neither authorization nor route status.
// Later source publication cannot turn proven historical capture into current
// authority, and does not invalidate the ticket's earlier coherent observation.
func (s *Service) prepareRuntimeConfigurationCaptureV1(ctx context.Context, r *gatewayRuntime, ticket *runtimeConfigurationSourceTicketV1) {
	if s == nil || ticket == nil || ticket.runtime != r || ticket.sequence == 0 || s.runtime != r || !runtimeConfigurationCaptureLiveV1(ctx, r) || !r.configurationCapture.preparing.CompareAndSwap(false, true) {
		return
	}
	defer r.configurationCapture.preparing.Store(false)
	if !runtimeConfigurationCaptureSourceBudgetV1(ctx, ticket.data) {
		return
	}
	networkDigest, ok := runtimeConfigurationNetworkDigestV1(ticket.network)
	if !ok || !runtimeConfigurationCaptureLiveV1(ctx, r) {
		return
	}
	document, err := freezeRuntimeConfigurationV1(ticket.data, ticket.sourceDigest, ticket.authorizationDigest, networkDigest)
	if err != nil || !runtimeConfigurationCaptureLiveV1(ctx, r) {
		return
	}
	raw, contentDigest, err := encodeRuntimeConfigurationV1(ctx, document)
	if err != nil || !runtimeConfigurationCaptureLiveV1(ctx, r) || s.runtime != r {
		clear(raw)
		return
	}
	capture := &runtimeConfigurationCaptureV1{
		sequence: ticket.sequence, snapshotID: ticket.snapshotID,
		routesPublishedAt: ticket.routesPublishedAt, capturedAt: ticket.capturedAt,
		sourceDigest: ticket.sourceDigest, authorizationDigest: ticket.authorizationDigest,
		networkDigest: networkDigest, contentDigest: contentDigest, canonical: raw,
	}
	if !r.configurationCapture.publication.TryLock() {
		clear(raw)
		return
	}
	defer r.configurationCapture.publication.Unlock()
	prior := r.configurationCapture.latest.Load()
	if !runtimeConfigurationCaptureLiveV1(ctx, r) || s.runtime != r || prior != nil && prior.sequence >= capture.sequence {
		clear(raw)
		return
	}
	r.configurationCapture.latest.Store(capture)
}

func runtimeConfigurationNetworkDigestV1(policy runtimeConfigurationNetworkPolicyV1) (string, bool) {
	if len(policy.TrustedProxies) > 128 {
		return "", false
	}
	prefixes := make([]string, 0, len(policy.TrustedProxies))
	for _, prefix := range policy.TrustedProxies {
		if !prefix.IsValid() {
			return "", false
		}
		prefixes = append(prefixes, prefix.Masked().String())
	}
	slices.Sort(prefixes)
	prefixes = slices.Compact(prefixes)
	// Bootstrap policy only: SMTP, roots, clients, resolved addresses and local
	// process/refresh identity are not Gateway network policy observations.
	document := struct {
		Domain          string
		PrivateUpstream bool
		PrivateEgress   bool
		PrivateStorage  bool
		TrustedProxies  []string
	}{"routex.gateway-network-policy.v1", policy.PrivateUpstream, policy.PrivateEgress, policy.PrivateStorage, prefixes}
	raw, _ := json.Marshal(document)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), true
}
