package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/vault"
)

func finiteSourceFixture(endpoint string) (entity.VaultRevision, entity.CredentialVaultReference) {
	birth := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	revision := entity.VaultRevision{ID: "vlr_00000000000000000000000000", IntegrationID: "vlt_00000000000000000000000000", IntegrationBirth: birth, Endpoint: endpoint, Namespace: "ns", Mount: "secret", Prefix: "route", DataField: "value"}
	ref := entity.CredentialVaultReference{CredentialID: "crd_00000000000000000000000000", CredentialBirth: birth, ReferenceID: strings.Repeat("a", 32), IntegrationID: revision.IntegrationID, IntegrationBirth: birth, RevisionID: revision.ID, ReaderGeneration: "reader", ReaderMethod: "token", ExpectedMarkerSHA256: strings.Repeat("b", 64), DescriptorSHA256: strings.Repeat("c", 64)}
	return revision, ref
}

func TestCredentialSourcePhysicalAliasesAndDistinctObjects(t *testing.T) {
	rev, ref := finiteSourceFixture("https://vault.example.invalid/base/")
	object, err := credentialPhysicalObject(rev, ref.ReferenceID)
	if err != nil {
		t.Fatal(err)
	}
	alias := rev
	alias.Endpoint = "https://vault.example.invalid/base"
	alias.DataField = "different"
	same, err := credentialPhysicalObject(alias, ref.ReferenceID)
	if err != nil || same != object {
		t.Fatal("same effective object split by field or SDK trailing slash")
	}
	for _, changed := range []entity.VaultRevision{
		{Endpoint: rev.Endpoint, Namespace: "other", Mount: rev.Mount, Prefix: rev.Prefix, DataField: rev.DataField},
		{Endpoint: rev.Endpoint, Namespace: rev.Namespace, Mount: "other", Prefix: rev.Prefix, DataField: rev.DataField},
		{Endpoint: "https://other.example.invalid/base", Namespace: rev.Namespace, Mount: rev.Mount, Prefix: rev.Prefix, DataField: rev.DataField},
	} {
		other, e := credentialPhysicalObject(changed, ref.ReferenceID)
		if e != nil || other == object {
			t.Fatal("distinct physical object merged")
		}
	}
	if _, e := credentialPhysicalObject(rev, "bad"); e == nil {
		t.Fatal("invalid generated reference admitted")
	}
	bad := rev
	bad.Endpoint += "%2F"
	if _, e := credentialPhysicalObject(bad, ref.ReferenceID); e == nil {
		t.Fatal("escaped path admitted")
	}
}

func TestCredentialSourceClosePoisonBeforeAliasRelease(t *testing.T) {
	var registry credentialSourceHolders
	key := credentialSourceKey{CredentialID: "original", Proof: "original", PhysicalObject: rootHash("one-object")}
	first, e := registry.acquire(key)
	if e != nil {
		t.Fatal(e)
	}
	alias := key
	alias.CredentialID = "other"
	alias.Proof = "different"
	alias.ReaderMethod = "approle"
	alias.Descriptor = "other-field"
	held, e := registry.acquire(alias)
	if e != nil {
		t.Fatal(e)
	}
	raw := &sourceBlockingClose{entered: make(chan struct{}), finish: make(chan struct{})}
	body := first.body(raw)
	done := make(chan error, 1)
	go func() { done <- body.Close() }()
	<-raw.entered
	if !held.admitUse() {
		t.Fatal("poison inferred before actual Close result")
	}
	close(raw.finish)
	if <-done == nil {
		t.Fatal("underlying error lost")
	}
	held.state.mu.Lock()
	poisoned, holders := held.state.poisoned, held.state.holders
	held.state.mu.Unlock()
	if !poisoned || holders != 1 {
		t.Fatal("failure not published before releasing original holder")
	}
	recovery, e := registry.acquire(alias)
	if e != nil {
		t.Fatal("OPEN poison invented a lifetime request ban", e)
	}
	recovery.release()
	held.release()
	if !errors.Is(registry.closeAndWait(context.Background(), key), vaultUnavailable) {
		t.Fatal("zero holders falsely certified drain")
	}
	independent := key
	independent.PhysicalObject = rootHash("independent")
	h, e := registry.acquire(independent)
	if e != nil {
		t.Fatal("poison crossed physical object", e)
	}
	h.release()
}

func TestCredentialSourceFiniteLostResponsePoisonAndFreshOwnership(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	s := &Service{allowPrivateUpstream: true}
	rev, ref := finiteSourceFixture(server.URL)
	op, e := s.credentialFiniteOperation(rev, ref, "token", "resolve")
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := op.client.PrepareCredential()
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Close()
	plan := prepared.Plan()
	ref.ReferenceID, ref.ExpectedMarkerSHA256, ref.DescriptorSHA256 = plan.ReferenceID, plan.ExpectedMarkerSHA256, plan.DescriptorSHA256
	// Use exactly the generated object on a newly owned client, not the first observer.
	op.close()
	op, e = s.credentialFiniteOperation(rev, ref, "token", "resolve")
	if e != nil {
		t.Fatal(e)
	}
	value, result, e := op.read(context.Background(), "reader", plan)
	if e == nil || value != nil || !result.Read.Attempted || result.Read.Succeeded {
		t.Fatal("lost response observation was replaced")
	}
	if requests.Load() != 1 {
		t.Fatal("implicit network retry")
	}
	if op.admissible() {
		t.Fatal("unproven response closure admitted next stage")
	}
	op.close()
	recovery, e := s.credentialFiniteOperation(rev, ref, "token", "recover")
	if e != nil {
		t.Fatal("fresh explicit recovery denied solely by prior poison", e)
	}
	if recovery.client.ResponseCloseState() != (vault.ResponseCloseState{}) || !recovery.holder.state.poisoned {
		t.Fatal("fresh observer borrowed or erased previous closure evidence")
	}
	recovery.close()
	if !errors.Is(s.credentialSources.closeAndWait(context.Background(), op.holder.key), vaultUnavailable) {
		t.Fatal("fresh no-effect operation falsely cleared drain poison")
	}
	other := ref
	other.ReferenceID = strings.Repeat("b", 32)
	another, e := s.credentialFiniteOperation(rev, other, "token", "recover")
	if e != nil {
		t.Fatal("independent source blocked", e)
	}
	if another.client == op.client || another.client.ResponseCloseState() != (vault.ResponseCloseState{}) {
		t.Fatal("observer shared across finite commands")
	}
	another.close()
	if _, e := s.credentialFiniteOperation(rev, ref, "token", "arbitrary"); e == nil {
		t.Fatal("unbounded purpose admitted")
	}
}

func TestCredentialSourceFiniteKnownDeniedResponseClosesWithoutPoison(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"errors"
 "fmt":["denied"]}`)
	}))
	defer server.Close()
	s := &Service{allowPrivateUpstream: true}
	rev, ref := finiteSourceFixture(server.URL)
	op, e := s.credentialFiniteOperation(rev, ref, "token", "resolve")
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := op.client.PrepareCredential()
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Close()
	plan := prepared.Plan()
	// Bind generated plan to exact holder rather than borrowing its different path.
	op.close()
	ref.ReferenceID, ref.ExpectedMarkerSHA256, ref.DescriptorSHA256 = plan.ReferenceID, plan.ExpectedMarkerSHA256, plan.DescriptorSHA256
	op, e = s.credentialFiniteOperation(rev, ref, "token", "resolve")
	if e != nil {
		t.Fatal(e)
	}
	_, result, e := op.read(context.Background(), "reader", plan)
	if e == nil || !result.Read.Attempted || result.Read.Failure == nil || result.Read.Failure.HTTPStatus != 403 {
		t.Fatal("denial facts lost")
	}
	state := op.client.ResponseCloseState()
	if !state.Observed || state.Pending != 0 || state.Failed || !op.holder.admitUse() {
		t.Fatal("known response denial invented closure poison")
	}
	op.close()
	fresh, e := s.credentialFiniteOperation(rev, ref, "token", "recover")
	if e != nil {
		t.Fatal("explicit fresh recovery rejected", e)
	}
	fresh.close()
	if requests.Load() != 1 {
		t.Fatal("recovery performed hidden read")
	}
}

func TestCredentialSourceFiniteExactPlanPurposeAndBirth(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(403) }))
	defer server.Close()
	s := &Service{allowPrivateUpstream: true}
	rev, ref := finiteSourceFixture(server.URL)
	op, e := s.credentialFiniteOperation(rev, ref, "token", "resolve")
	if e != nil {
		t.Fatal(e)
	}
	defer op.close()
	changed := op.plan
	changed.ReferenceID = strings.Repeat("e", 32)
	if _, _, e := op.read(context.Background(), "reader", changed); e == nil {
		t.Fatal("foreign reference used exact source holder")
	}
	changed = op.plan
	changed.ExpectedMarkerSHA256 = strings.Repeat("e", 64)
	if _, e := op.cleanup(context.Background(), "reader", "cleanup", changed); e == nil {
		t.Fatal("foreign proof or purpose crossed ownership")
	}
	if _, e := op.cleanup(context.Background(), "reader", "cleanup", op.plan); e == nil {
		t.Fatal("read purpose acquired destroy authority")
	}
	prepared, e := op.client.PrepareCredential()
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Close()
	if _, e := op.write(context.Background(), "writer", prepared, []byte("private")); e == nil {
		t.Fatal("read purpose admitted Write")
	}
	for _, bad := range []entity.CredentialVaultReference{
		func() entity.CredentialVaultReference {
			x := ref
			x.IntegrationBirth = x.IntegrationBirth.Add(time.Second)
			return x
		}(),
		func() entity.CredentialVaultReference {
			x := ref
			x.RevisionID = "vlr_00000000000000000000000001"
			return x
		}(),
		func() entity.CredentialVaultReference { x := ref; x.ReaderGeneration = ""; return x }(),
	} {
		if other, e := s.credentialFiniteOperation(rev, bad, "token", "recover"); e == nil || other != nil {
			t.Fatal("invalid original source acquired holder")
		}
	}
	if requests.Load() != 0 || op.client.ResponseCloseState() != (vault.ResponseCloseState{}) {
		t.Fatal("invalid plan or purpose performed HTTP")
	}
}

func TestCredentialSourceFiniteLoginClosureStopsWriteWithoutChangingAuthFact(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		connection, _, e := w.(http.Hijacker).Hijack()
		if e == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()
	s := &Service{allowPrivateUpstream: true}
	rev, ref := finiteSourceFixture(server.URL)
	client, e := vaultClient(rev, true)
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := client.PrepareCredential()
	client.Close()
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Close()
	plan := prepared.Plan()
	ref.ReferenceID, ref.ExpectedMarkerSHA256, ref.DescriptorSHA256 = plan.ReferenceID, plan.ExpectedMarkerSHA256, plan.DescriptorSHA256
	op, e := s.credentialFiniteOperation(rev, ref, "approle", "create")
	if e != nil {
		t.Fatal(e)
	}
	defer op.close()
	material := vaultAppRolePrefix + `{"auth_mount":"approle","role_id":"role","secret_id":"secret"}`
	token, release, login, e := op.token(context.Background(), "approle", material)
	defer release()
	if e == nil || token != "" || !login.Attempted || login.Succeeded {
		t.Fatal("failed login observation lost or promoted to KV")
	}
	write, e := op.write(context.Background(), "writer", prepared, []byte("private"))
	if e == nil || write.Write.Attempted || requests.Load() != 1 {
		t.Fatal("failed Login Close permitted next Write stage")
	}
	if op.holder.state.poisoned != true {
		t.Fatal("auth Close uncertainty not attributed to exact physical source")
	}
}

func TestCredentialSourcePoisonedOpenStatesRetainedUntilExplicitDenial(t *testing.T) {
	var registry credentialSourceHolders
	key := credentialSourceKey{PhysicalObject: rootHash("poison-retained")}
	h, e := registry.acquire(key)
	if e != nil {
		t.Fatal(e)
	}
	h.poison()
	h.release()
	registry.mu.Lock()
	state := registry.states[physicalHolderKey(key)]
	registry.mu.Unlock()
	if state == nil || !state.poisoned || state.closed {
		t.Fatal("poison lost or converted to unrequested lifetime denial")
	}
	fresh, e := registry.acquire(key)
	if e != nil {
		t.Fatal("explicit OPEN acquisition denied", e)
	}
	fresh.release()
	if !errors.Is(registry.closeAndWait(context.Background(), key), vaultUnavailable) {
		t.Fatal("poison certified joined drain")
	}
	if h, e := registry.acquire(key); e == nil || h != nil {
		t.Fatal("explicit CLOSED denial reopened")
	}
}

func TestCredentialSourcePoisonedAliasesCannotReclaimOrEvadeCapacity(t *testing.T) {
	var registry credentialSourceHolders
	for i := range 5000 {
		key := credentialSourceKey{CredentialID: "owner", Proof: "proof", PhysicalObject: rootHash(fmt.Sprint(i))}
		h, e := registry.acquire(key)
		if e != nil {
			t.Fatal(e)
		}
		h.poison()
		h.release()
	}
	alias := credentialSourceKey{CredentialID: "different", ReaderMethod: "approle", Proof: "other-field", PhysicalObject: rootHash("0")}
	h, e := registry.acquire(alias)
	if e != nil {
		t.Fatal("existing poisoned OPEN object could not recover", e)
	}
	h.release()
	registry.mu.Lock()
	retained := len(registry.states)
	poisoned := registry.states[physicalHolderKey(alias)].poisoned
	registry.mu.Unlock()
	if retained != 5000 || !poisoned {
		t.Fatal("alias reset or reclaimed original poison")
	}
	if h, e := registry.acquire(credentialSourceKey{PhysicalObject: rootHash("new-object")}); e == nil || h != nil {
		t.Fatal("capacity silently admitted unregistered source")
	}
}
