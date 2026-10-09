package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/upstream"
	"github.com/miclle/routex/pkg/vault"
)

// Match the SDK's validated, effective data URL and exact namespace/version.
// DataField and logical ownership/auth proof remain separate. There is no DNS,
// default-port, or escape alias inference and no public cleanup authorization.
func credentialPhysicalObject(rev entity.VaultRevision, reference string) (string, error) {
	if !vaultProbeID.MatchString(reference) {
		return "", vaultUnavailable
	}
	validated, err := vaultClient(rev, true)
	if err != nil {
		return "", err
	}
	validated.Close()
	endpoint, err := upstream.ValidateBaseURL(rev.Endpoint, true)
	if err != nil {
		return "", vaultUnavailable
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/v1/" + rev.Mount + "/data/" + rev.Prefix + "/routex-credential-" + reference
	raw, _ := json.Marshal(struct {
		URL, Namespace string
		Version        int
	}{endpoint.String(), rev.Namespace, 1})
	return rootHash(string(raw)), nil
}

// One operation owns a newly constructed SDK client, exact local registry, source
// and purpose until all synchronous HTTP methods and Body.Close calls return.
// Only this owner consumes its fresh observer; a public SDK snapshot alone is
// not an operation claim, and no observer is shared across commands or sources.
// This local prerequisite has no durable process/fleet exclusion semantics.
type credentialFiniteOperation struct {
	mu      sync.Mutex
	client  *vault.Client
	holder  *credentialSourceHolder
	process *credentialProcessTicket
	purpose string
	plan    vault.CredentialPlan
	closed  bool
	failed  bool
}

func (s *Service) credentialFiniteOperation(rev entity.VaultRevision, ref entity.CredentialVaultReference, method, purpose string) (*credentialFiniteOperation, error) {
	return s.credentialFiniteOperationOwned(rev, ref, method, purpose, false)
}

// Exclusive cleanup is admitted only by the durable published drain coordinator.
func (s *Service) credentialFiniteOperationOwned(rev entity.VaultRevision, ref entity.CredentialVaultReference, method, purpose string, exclusive bool) (*credentialFiniteOperation, error) {
	ticket, err := s.credentialSources.processAcquire()
	if err != nil {
		return nil, err
	}
	operation, err := s.credentialFiniteOperationTicket(rev, ref, method, purpose, exclusive, ticket)
	if err != nil {
		ticket.release()
	}
	return operation, err
}

// The generation ticket precedes every SDK lifetime, including exclusive cleanup.
func (s *Service) credentialFiniteOperationTicket(rev entity.VaultRevision, ref entity.CredentialVaultReference, method, purpose string, exclusive bool, ticket *credentialProcessTicket) (*credentialFiniteOperation, error) {
	if !ticket.claim(&s.credentialSources) {
		return nil, vaultUnavailable
	}
	if exclusive && purpose != "cleanup" {
		return nil, vaultUnavailable
	}
	switch purpose {
	case "resolve", "create", "recover", "cleanup":
	default:
		return nil, vaultUnavailable
	}
	if !credentialVaultReferenceShape(entity.ProviderCredential{ID: ref.CredentialID, CreatedAt: ref.CredentialBirth}, ref) || !vaultStoredMethod(method) || ref.RevisionID != rev.ID || ref.IntegrationID != rev.IntegrationID || !ref.IntegrationBirth.Equal(rev.IntegrationBirth) {
		return nil, vaultUnavailable
	}
	object, err := credentialPhysicalObject(rev, ref.ReferenceID)
	if err != nil {
		return nil, err
	}
	client, err := vaultClient(rev, s.allowPrivateUpstream)
	if err != nil {
		return nil, err
	}
	// Fresh construction is the exclusive ownership boundary, not this check alone.
	if client.ResponseCloseState() != (vault.ResponseCloseState{}) {
		client.Close()
		return nil, vaultUnavailable
	}
	raw, _ := json.Marshal(ref)
	key := credentialSourceKey{CredentialID: ref.CredentialID, CredentialBirth: ref.CredentialBirth.UTC().Format(time.RFC3339Nano), ReferenceID: ref.ReferenceID, IntegrationID: ref.IntegrationID, IntegrationBirth: ref.IntegrationBirth.UTC().Format(time.RFC3339Nano), RevisionID: ref.RevisionID, ReaderGeneration: ref.ReaderGeneration, ReaderMethod: method, Descriptor: ref.DescriptorSHA256, Proof: rootHash(string(raw)), PhysicalObject: object, Marker: ref.ExpectedMarkerSHA256}
	var holder *credentialSourceHolder
	if !exclusive {
		holder, err = s.credentialSources.acquire(key)
		if err != nil {
			client.Close()
			return nil, err
		}
	}
	return &credentialFiniteOperation{client: client, holder: holder, process: ticket, purpose: purpose, plan: credentialPlan(ref)}, nil
}

func (op *credentialFiniteOperation) admissible() bool {
	return !op.closed && !op.failed && op.holder.admitUse()
}
func (op *credentialFiniteOperation) observe(attempted bool, err error) error {
	state := op.client.ResponseCloseState()
	if state.Failed || state.Pending != 0 || (attempted && !state.Observed) {
		op.failed = true
		op.holder.poison()
		op.process.poison()
		if err == nil {
			err = vaultUnavailable
		}
	}
	return err
}
func (op *credentialFiniteOperation) close() {
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.closed {
		return
	}
	op.closed = true
	_ = op.observe(false, nil)
	op.client.Close() // Idle connection cleanup is not response-body drain proof.
	op.holder.release()
	op.process.release()
}
func (op *credentialFiniteOperation) token(ctx context.Context, method, material string) (string, func(), vault.Observation, error) {
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.admissible() {
		return "", func() {}, vault.Observation{}, vaultUnavailable
	}
	token, release, observation, err := vaultCommandToken(ctx, op.client, method, material)
	err = op.observe(observation.Attempted, err)
	if err != nil {
		release()
		return "", func() {}, observation, err
	}
	return token, release, observation, nil
}
func (op *credentialFiniteOperation) write(ctx context.Context, token string, prepared *vault.PreparedCredential, value []byte) (vault.CredentialWriteResult, error) {
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.admissible() || op.purpose != "create" || prepared == nil || prepared.Plan() != op.plan {
		return vault.CredentialWriteResult{}, vaultUnavailable
	}
	result, err := op.client.WriteCredential(ctx, token, prepared, value)
	return result, op.observe(result.Write.Attempted, err)
}
func (op *credentialFiniteOperation) read(ctx context.Context, token string, plan vault.CredentialPlan) (*vault.CredentialValue, vault.CredentialReadResult, error) {
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.admissible() || op.purpose == "cleanup" || plan != op.plan {
		return nil, vault.CredentialReadResult{}, vaultUnavailable
	}
	value, result, err := op.client.ReadCredential(ctx, token, plan)
	err = op.observe(result.Read.Attempted, err)
	if err != nil && value != nil {
		value.Close()
		value = nil
	}
	return value, result, err
}
func (op *credentialFiniteOperation) cleanup(ctx context.Context, token, cleanup string, plan vault.CredentialPlan) (vault.CredentialCleanupResult, error) {
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.admissible() || op.purpose != "cleanup" || plan != op.plan {
		return vault.CredentialCleanupResult{}, vaultUnavailable
	}
	result, err := op.client.CleanupCredentialOwned(ctx, token, cleanup, plan)
	return result, op.observe(result.Ownership.Attempted || result.Cleanup.Observation.Attempted, err)
}
