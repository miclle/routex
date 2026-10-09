package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const deploymentCoverageLimit = 2000

type DeploymentCoverageModel struct {
	ID           string `json:"id"`
	UpstreamName string `json:"upstream_name"`
	CanAttest    bool   `json:"can_attest"`
	Attested     bool   `json:"attested"`
}
type DeploymentCoverageRecord struct {
	CredentialID       string                    `json:"credential_id"`
	ConnectionID       string                    `json:"connection_id"`
	Adapter            string                    `json:"adapter"`
	APIVersion         string                    `json:"api_version"`
	VerificationStatus string                    `json:"verification_status"`
	VerifiedAt         *time.Time                `json:"verified_at"`
	CoverageSource     string                    `json:"coverage_source"`
	ProviderModels     []DeploymentCoverageModel `json:"provider_models"`
	ETag               string                    `json:"etag"`
	CanEdit            bool                      `json:"can_edit"`
}
type DeploymentCoverageInput struct {
	ProviderModelIDs []string `json:"provider_model_ids"`
	Reason           string   `json:"reason"`
}
type DeploymentCoverageWriteResult struct {
	Coverage       DeploymentCoverageRecord `json:"coverage"`
	RuntimeApplied bool                     `json:"runtime_applied"`
	Changed        bool                     `json:"changed"`
}

func (input *DeploymentCoverageInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 256<<10 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"provider_model_ids", "reason"})
	if err != nil {
		return err
	}
	var next DeploymentCoverageInput
	if string(fields["provider_model_ids"]) == "null" || json.Unmarshal(fields["provider_model_ids"], &next.ProviderModelIDs) != nil || next.ProviderModelIDs == nil || len(next.ProviderModelIDs) > deploymentCoverageLimit || string(fields["reason"]) == "null" || json.Unmarshal(fields["reason"], &next.Reason) != nil || !validCredentialMetadataReason(next.Reason) || strings.TrimSpace(next.Reason) != next.Reason {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, id := range next.ProviderModelIDs {
		if !modelCreationID(id, "pmd") || seen[id] {
			return apperrors.ErrBadRequest
		}
		seen[id] = true
	}
	slices.Sort(next.ProviderModelIDs)
	*input = next
	return nil
}

// The logical identity excludes ciphertext/root rewrap and mutable metadata.
// A replacement secret always has a separate Credential ID and birth. Retained
// Vault references bind their immutable descriptor and reader generation only.
func deploymentAttestationIdentity(c entity.ProviderCredential, connection entity.ProviderConnection, pm entity.ProviderModel) string {
	if c.ID == "" || c.ConnectionID != connection.ID || pm.ConnectionID != connection.ID || !connectionMetadataBirth(c.CreatedAt) || !connectionMetadataBirth(connection.CreatedAt) || !connectionMetadataBirth(pm.CreatedAt) || entity.ConnectionAdapter(connection) != entity.AdapterAzureOpenAIClassic || connection.APIVersion == nil || !upstream.ValidAzureAPIVersion(*connection.APIVersion) || !upstream.ValidAzureDeployment(pm.UpstreamName) {
		return ""
	}
	source := "inline"
	var ref *entity.CredentialVaultReference
	if c.StorageSource == "vault" {
		source = "vault"
		if c.VaultReference == nil || !credentialVaultReferenceShape(c, *c.VaultReference) {
			return ""
		}
		r := *c.VaultReference
		r.ReaderCiphertext = ""
		r.ReaderMethod = ""
		r.SourceContext = ""
		r.CredentialBirth = r.CredentialBirth.UTC()
		r.IntegrationBirth = r.IntegrationBirth.UTC()
		ref = &r
	} else if c.StorageSource != "" && c.StorageSource != "inline" || c.VaultReference != nil {
		return ""
	}
	legacy := connectionMetadataHash(struct {
		Version, CredentialID, ConnectionID, ProviderID, ModelID, Source, Adapter, Endpoint, APIVersion, Deployment string
		CredentialBirth, ConnectionBirth, ModelBirth                                                                time.Time
		Reference                                                                                                   *entity.CredentialVaultReference
	}{"azure.deployment.identity.v1", c.ID, connection.ID, connection.ProviderID, pm.ID, source, entity.ConnectionAdapter(connection), connection.BaseURL, *connection.APIVersion, pm.UpstreamName, c.CreatedAt.UTC(), connection.CreatedAt.UTC(), pm.CreatedAt.UTC(), ref})
	if !validTransportGeneration(connection.TransportGeneration) {
		return ""
	}
	if connection.TransportGeneration == "0" {
		return legacy
	}
	return connectionMetadataHash(struct{ Version, LegacyIdentity, TransportGeneration string }{"azure.deployment.identity.v2", legacy, connection.TransportGeneration})
}

// Deployment evidence is projected only in memory. It never changes discovered
// CredentialModelAccess rows, and stale/replaced identities cannot authorize.
func deploymentCoverageProjection(credentials []entity.ProviderCredential, connections []entity.ProviderConnection, models []entity.ProviderModel, discovered []entity.CredentialModelAccess, attestations []entity.CredentialDeploymentAttestation) []entity.CredentialModelAccess {
	cs := map[string]entity.ProviderConnection{}
	creds := map[string]entity.ProviderCredential{}
	pms := map[string]entity.ProviderModel{}
	for _, c := range connections {
		cs[c.ID] = c
	}
	for _, c := range credentials {
		creds[c.ID] = c
	}
	for _, m := range models {
		pms[m.ID] = m
	}
	result := []entity.CredentialModelAccess{}
	seen := map[[2]string]bool{}
	add := func(c, m string) {
		key := [2]string{c, m}
		if !seen[key] {
			seen[key] = true
			result = append(result, entity.CredentialModelAccess{CredentialID: c, ProviderModelID: m})
		}
	}
	for _, a := range discovered {
		c := creds[a.CredentialID]
		connection := cs[c.ConnectionID]
		pm, ok := pms[a.ProviderModelID]
		if ok && verifiedTransportCurrent(c, connection) && pm.ConnectionID == connection.ID && entity.ConnectionAdapter(connection) != entity.AdapterAzureOpenAIClassic {
			add(a.CredentialID, a.ProviderModelID)
		}
	}
	for _, a := range attestations {
		c, ok := creds[a.CredentialID]
		m, mok := pms[a.ProviderModelID]
		connection, cok := cs[c.ConnectionID]
		if ok && mok && cok && verifiedTransportCurrent(c, connection) && a.Identity != "" && a.Identity == deploymentAttestationIdentity(c, connection, m) {
			add(c.ID, m.ID)
		}
	}
	slices.SortFunc(result, func(a, b entity.CredentialModelAccess) int {
		if a.CredentialID != b.CredentialID {
			return strings.Compare(a.CredentialID, b.CredentialID)
		}
		return strings.Compare(a.ProviderModelID, b.ProviderModelID)
	})
	return result
}

func loadDeploymentCoverage(tx *gorm.DB, credentials []entity.ProviderCredential, connections []entity.ProviderConnection, models []entity.ProviderModel, discovered []entity.CredentialModelAccess, budget int) ([]entity.CredentialModelAccess, error) {
	ids := []string{}
	for _, c := range credentials {
		for _, connection := range connections {
			if connection.ID == c.ConnectionID && entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
				ids = append(ids, c.ID)
				break
			}
		}
	}
	if len(ids) == 0 {
		return discovered, nil
	}
	var rows []entity.CredentialDeploymentAttestation
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "credential_id", ids)).Limit(budget + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > budget {
		return nil, connectionMetadataUnavailable
	}
	return deploymentCoverageProjection(credentials, connections, models, discovered, rows), nil
}

func (s *Service) deploymentCoverageSnapshot(tx *gorm.DB, actorID, credentialID string, write, lock bool) (entity.ProviderCredential, entity.ProviderConnection, DeploymentCoverageRecord, error) {
	var c entity.ProviderCredential
	var connection entity.ProviderConnection
	var view DeploymentCoverageRecord
	actor, err := registrationAdmittedUser(modelCreationDB(tx), actorID, lock)
	if err != nil {
		return c, connection, view, err
	}
	perm := "providers.read"
	if write {
		perm = "providers.write"
	}
	allowed, err := exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, perm)
	if err != nil {
		return c, connection, view, err
	}
	if !allowed {
		return c, connection, view, apperrors.ErrForbidden
	}
	canEdit := write
	if !write {
		canEdit, err = exactGovernancePermissionForAdmittedActor(modelCreationDB(tx), actor, "providers.write")
		if err != nil {
			return c, connection, view, err
		}
	}
	query := personalExact(modelCreationDB(tx), "id", credentialID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err = query.Take(&c).Error; err != nil {
		return c, connection, view, err
	}
	if !validCoverageIntentState(c) {
		return c, connection, view, connectionMetadataUnavailable
	}
	if c.ID != credentialID {
		return c, connection, view, apperrors.ErrNotFound
	}
	query = personalExact(modelCreationDB(tx), "id", c.ConnectionID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err = query.Take(&connection).Error; err != nil {
		return c, connection, view, err
	}
	if connection.ID != c.ConnectionID {
		return c, connection, view, apperrors.ErrNotFound
	}
	if entity.ConnectionAdapter(connection) != entity.AdapterAzureOpenAIClassic {
		return c, connection, view, apperrors.ErrBadRequest
	}
	if validateConnectionAdapter(connection, s.allowPrivateUpstream) != nil {
		return c, connection, view, connectionMetadataUnavailable
	}
	var provider entity.Provider
	if err = personalExact(modelCreationDB(tx), "id", connection.ProviderID).Take(&provider).Error; err != nil {
		return c, connection, view, err
	}
	if provider.ID != connection.ProviderID || !connectionMetadataBirth(provider.CreatedAt) || !connectionMetadataBirth(actor.CreatedAt) || !connectionMetadataBirth(c.CreatedAt) || !connectionMetadataBirth(connection.CreatedAt) {
		return c, connection, view, connectionMetadataUnavailable
	}
	rows := []entity.ProviderCredential{c}
	if err = attachCredentialSources(modelCreationDB(tx), rows); err != nil {
		return c, connection, view, err
	}
	c = rows[0]
	models := []entity.ProviderModel{}
	if err = personalExact(modelCreationDB(tx), "connection_id", connection.ID).Order("id").Limit(deploymentCoverageLimit + 1).Find(&models).Error; err != nil {
		return c, connection, view, err
	}
	if len(models) > deploymentCoverageLimit {
		return c, connection, view, connectionMetadataUnavailable
	}
	attestations := []entity.CredentialDeploymentAttestation{}
	if err = personalExact(modelCreationDB(tx), "credential_id", c.ID).Order("provider_model_id").Limit(deploymentCoverageLimit + 1).Find(&attestations).Error; err != nil {
		return c, connection, view, err
	}
	if len(attestations) > deploymentCoverageLimit {
		return c, connection, view, connectionMetadataUnavailable
	}
	for i := range models {
		models[i].CreatedAt = models[i].CreatedAt.UTC()
	}
	for _, a := range attestations {
		if a.CredentialID != c.ID || !modelCreationID(a.ProviderModelID, "pmd") || !validMemberRoleDigest(a.Identity) {
			return c, connection, view, connectionMetadataUnavailable
		}
	}
	covered := deploymentCoverageProjection(rows, []entity.ProviderConnection{connection}, models, nil, attestations)
	selected := map[string]bool{}
	for _, a := range covered {
		selected[a.ProviderModelID] = true
	}
	view = DeploymentCoverageRecord{CredentialID: c.ID, ConnectionID: connection.ID, Adapter: entity.ConnectionAdapter(connection), APIVersion: *connection.APIVersion, VerificationStatus: c.VerificationStatus, VerifiedAt: c.VerifiedAt, CoverageSource: "administrator_attestation", ProviderModels: []DeploymentCoverageModel{}, CanEdit: canEdit}
	if view.VerifiedAt != nil {
		normalized := view.VerifiedAt.UTC()
		view.VerifiedAt = &normalized
	}
	seen := map[string]bool{}
	for _, m := range models {
		if m.ConnectionID != connection.ID || !modelCreationID(m.ID, "pmd") || seen[m.ID] || !connectionMetadataBirth(m.CreatedAt) {
			return c, connection, view, connectionMetadataUnavailable
		}
		seen[m.ID] = true
		view.ProviderModels = append(view.ProviderModels, DeploymentCoverageModel{m.ID, m.UpstreamName, deploymentAttestationIdentity(c, connection, m) != "", selected[m.ID]})
	}
	identity := connectionMetadataHash(struct {
		Version                                         string
		Actor                                           entity.User
		CredentialID, ConnectionID, ProviderID          string
		CredentialBirth, ConnectionBirth, ProviderBirth time.Time
		Source, Adapter, BaseURL, VersionID             string
	}{"azure.coverage.review.identity.v1", entity.User{ID: actor.ID, CreatedAt: actor.CreatedAt.UTC()}, c.ID, connection.ID, provider.ID, c.CreatedAt.UTC(), connection.CreatedAt.UTC(), provider.CreatedAt.UTC(), deploymentSourceIdentity(c), view.Adapter, connection.BaseURL, view.APIVersion})
	review := connectionMetadataHash(struct {
		View                                                DeploymentCoverageRecord
		CredentialRevision, ConnectionRevision, SourceProof string
		CoverageRevision                                    int64
		Models                                              []entity.ProviderModel
		Attestations                                        []entity.CredentialDeploymentAttestation
	}{view, credentialMetadataRecord(c).ETag, connection.ETag, credentialSourceProof(c), c.CoverageRevision, models, attestations})
	view.ETag = identity + "." + review
	return c, connection, view, nil
}
func deploymentSourceIdentity(c entity.ProviderCredential) string {
	if c.StorageSource == "vault" {
		if c.VaultReference == nil {
			return ""
		}
		r := *c.VaultReference
		r.ReaderCiphertext = ""
		r.ReaderMethod = ""
		r.SourceContext = ""
		r.CredentialBirth = r.CredentialBirth.UTC()
		r.IntegrationBirth = r.IntegrationBirth.UTC()
		return connectionMetadataHash(r)
	}
	return "inline"
}
func coverageCurrentIDs(view DeploymentCoverageRecord) []string {
	ids := []string{}
	for _, m := range view.ProviderModels {
		if m.Attested {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// Only the current review or the immediately preceding changed intent can
// reconcile the current set. Later changes replace this bounded checkpoint.
func validCoverageIntentState(c entity.ProviderCredential) bool {
	if c.CoverageRevision == 0 {
		return c.CoverageReviewETag == "" && c.CoverageIntentSHA256 == ""
	}
	return c.CoverageRevision > 0 && validConnectionMetadataETag(c.CoverageReviewETag) && validMemberRoleDigest(c.CoverageIntentSHA256)
}
func coverageIntentDigest(input DeploymentCoverageInput) string { return connectionMetadataHash(input) }
func coverageReviewAuthorized(c entity.ProviderCredential, view DeploymentCoverageRecord, etag string, input DeploymentCoverageInput) bool {
	if !validCoverageIntentState(c) || !validConnectionMetadataETag(view.ETag) || !validConnectionMetadataETag(etag) || view.ETag[:64] != etag[:64] {
		return false
	}
	if view.ETag == etag {
		return true
	}
	return c.CoverageReviewETag == etag && c.CoverageIntentSHA256 == coverageIntentDigest(input) && slices.Equal(coverageCurrentIDs(view), input.ProviderModelIDs)
}
func deploymentCoverageError(err error) error {
	if err == nil {
		return nil
	}
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return connectionMetadataUnavailable
}
func (s *Service) GetDeploymentCoverage(ctx context.Context, actorID, credentialID string) (*DeploymentCoverageRecord, error) {
	if !modelCreationID(actorID, "usr") || !modelCreationID(credentialID, "crd") {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var view DeploymentCoverageRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, next, e := s.deploymentCoverageSnapshot(tx, actorID, credentialID, false, false)
		view = next
		return e
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, deploymentCoverageError(err)
	}
	return &view, nil
}
func (s *Service) WriteDeploymentCoverage(ctx context.Context, actorID, credentialID, etag string, input DeploymentCoverageInput) (*DeploymentCoverageWriteResult, error) {
	if !modelCreationID(actorID, "usr") || !modelCreationID(credentialID, "crd") || !validConnectionMetadataETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return nil, apperrors.ErrBadRequest
	}
	var validated DeploymentCoverageInput
	if e = validated.UnmarshalJSON(raw); e != nil {
		return nil, e
	}
	input = validated
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	changed := false
	var desired DeploymentCoverageRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		c, connection, view, e := s.deploymentCoverageSnapshot(tx, actorID, credentialID, true, true)
		if e != nil {
			return e
		}
		same := slices.Equal(coverageCurrentIDs(view), input.ProviderModelIDs)
		if !coverageReviewAuthorized(c, view, etag, input) {
			return catalogConflict
		}
		for _, id := range input.ProviderModelIDs {
			found := false
			for _, m := range view.ProviderModels {
				if m.ID == id && m.CanAttest {
					found = true
				}
			}
			if !found {
				return apperrors.ErrBadRequest
			}
		}
		if !same {
			if c.CoverageRevision < 0 || c.CoverageRevision == math.MaxInt64 {
				return connectionMetadataUnavailable
			}
			update := personalExact(modelCreationDB(tx), "id", c.ID).Model(&entity.ProviderCredential{}).Updates(map[string]any{"coverage_revision": c.CoverageRevision + 1, "coverage_review_etag": etag, "coverage_intent_sha256": coverageIntentDigest(input)})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return connectionMetadataUnavailable
			}
			if e := personalExact(modelCreationDB(tx), "credential_id", c.ID).Delete(&entity.CredentialDeploymentAttestation{}).Error; e != nil {
				return e
			}
			for _, id := range input.ProviderModelIDs {
				var m entity.ProviderModel
				if e := personalExact(modelCreationDB(tx), "id", id).Take(&m).Error; e != nil {
					return e
				}
				identity := deploymentAttestationIdentity(c, connection, m)
				if identity == "" {
					return catalogConflict
				}
				if e := modelCreationDB(tx).Create(&entity.CredentialDeploymentAttestation{CredentialID: c.ID, ProviderModelID: id, Identity: identity}).Error; e != nil {
					return e
				}
			}
			if e := appendDeploymentCoverageAudit(tx, actorID, c.ID, coverageCurrentIDs(view), input); e != nil {
				return e
			}
			changed = true
		}
		_, _, desired, e = s.deploymentCoverageSnapshot(tx, actorID, credentialID, true, false)
		return e
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if changed {
		s.InvalidateRuntimeCredential(credentialID)
	}
	release()
	if e = s.RefreshRuntime(ctx); e != nil {
		return nil, connectionMetadataUnavailable
	}
	var confirmed DeploymentCoverageRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		_, _, next, e := s.deploymentCoverageSnapshot(tx, actorID, credentialID, true, false)
		if e != nil {
			return e
		}
		if next.ETag != desired.ETag || !slices.Equal(coverageCurrentIDs(next), input.ProviderModelIDs) {
			return catalogConflict
		}
		data, e := s.loadRuntimeDataTx(tx)
		if e != nil {
			return e
		}
		digest, e := runtimeDigest(data)
		if e != nil {
			return e
		}
		if !s.connectionMetadataRuntimeApplied(digest, data.EgressGeneration) {
			return connectionMetadataUnavailable
		}
		confirmed = next
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, deploymentCoverageError(err)
	}
	return &DeploymentCoverageWriteResult{Coverage: confirmed, RuntimeApplied: true, Changed: changed}, nil
}

// Existing native supply keeps its discovery-only contract. Azure reads are
// bounded and project reviewed evidence through the same in-memory interface.
func connectionCredentialCoverage(tx *gorm.DB, connectionID string, selected []string) ([]entity.ProviderCredential, map[string]map[string]bool, error) {
	var connection entity.ProviderConnection
	if err := personalExact(modelCreationDB(tx), "id", connectionID).Take(&connection).Error; err != nil {
		return nil, nil, err
	}
	if connection.ID != connectionID {
		return nil, nil, apperrors.ErrNotFound
	}
	credentials := []entity.ProviderCredential{}
	if err := personalExact(modelCreationDB(tx), "connection_id", connectionID).Order("priority,created_at,id").Limit(2001).Find(&credentials).Error; err != nil {
		return nil, nil, err
	}
	if len(credentials) > 2000 {
		return nil, nil, connectionMetadataUnavailable
	}
	models := []entity.ProviderModel{}
	q := personalExact(modelCreationDB(tx), "connection_id", connectionID)
	if selected != nil {
		q = q.Where(memberModelsExactIDs(tx, "id", selected))
	}
	if err := q.Limit(2001).Find(&models).Error; err != nil {
		return nil, nil, err
	}
	if len(models) > 2000 {
		return nil, nil, connectionMetadataUnavailable
	}
	ids := []string{}
	for _, c := range credentials {
		if c.ConnectionID != connectionID {
			return nil, nil, connectionMetadataUnavailable
		}
		ids = append(ids, c.ID)
	}
	if err := attachCredentialSources(modelCreationDB(tx), credentials); err != nil {
		return nil, nil, err
	}
	access := []entity.CredentialModelAccess{}
	if len(ids) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "credential_id", ids)).Limit(10001).Find(&access).Error; err != nil {
			return nil, nil, err
		}
		if len(access) > 10000 {
			return nil, nil, connectionMetadataUnavailable
		}
	}
	access, err := loadDeploymentCoverage(tx, credentials, []entity.ProviderConnection{connection}, models, access, 10000)
	if err != nil {
		return nil, nil, err
	}
	covered := map[string]map[string]bool{}
	for _, c := range credentials {
		covered[c.ID] = map[string]bool{}
	}
	for _, a := range access {
		if covered[a.CredentialID] != nil {
			covered[a.CredentialID][a.ProviderModelID] = true
		}
	}
	return credentials, covered, nil
}
func credentialMissingActiveDeployment(tx *gorm.DB, connection entity.ProviderConnection, credentialID string) (bool, error) {
	var bindings []entity.ModelProviderBinding
	if err := modelCreationDB(tx).Table("model_provider_bindings AS b").Select("b.*").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Where("p.connection_id = ? AND b.weight > 0", connection.ID).Limit(2001).Find(&bindings).Error; err != nil {
		return false, err
	}
	if len(bindings) > 2000 {
		return false, connectionMetadataUnavailable
	}
	_, covered, err := connectionCredentialCoverage(tx, connection.ID, nil)
	if err != nil {
		return false, err
	}
	for _, b := range bindings {
		if !covered[credentialID][b.ProviderModelID] {
			return true, nil
		}
	}
	return false, nil
}

func appendDeploymentCoverageAudit(tx *gorm.DB, actor, credential string, before []string, input DeploymentCoverageInput) error {
	raw, err := json.Marshal(struct {
		Before, After []string
		Reason        string
	}{before, input.ProviderModelIDs, input.Reason})
	if err != nil {
		return apperrors.ErrInternal
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	text := string(raw)
	return modelCreationDB(tx).Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: "credential.deployment_coverage", ResourceType: "credential", ResourceID: credential, DetailsJSON: &text}).Error
}

// Coverage generations are internal publication facts, not metadata or a secret identity.
func credentialRuntimeRevision(c entity.ProviderCredential) string {
	metadata := credentialMetadataRecord(c).ETag
	if c.VerifiedTransportGeneration != "0" {
		return connectionMetadataHash(struct {
			Version, Metadata, Generation string
			Coverage                      int64
		}{"credential.runtime.transport.v1", metadata, c.VerifiedTransportGeneration, c.CoverageRevision})
	}
	if c.CoverageRevision == 0 {
		return metadata
	}
	return connectionMetadataHash(struct {
		Metadata string
		Coverage int64
	}{metadata, c.CoverageRevision})
}
