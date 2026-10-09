package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/vault"
	"gorm.io/gorm"
)

// Only immutable, nonsecret source identity enters catalogue/runtime continuity.
// Legacy blank source is inline; V77 backfills it without touching ciphertext.
func credentialSourceProof(c entity.ProviderCredential) string {
	if c.StorageSource == "" || c.StorageSource == "inline" {
		if c.VaultReference != nil || c.Ciphertext == "" {
			return ""
		}
		return personalHash(c.Ciphertext)
	}
	if c.StorageSource != "vault" || c.Ciphertext != "" || c.VaultReference == nil {
		return ""
	}
	r := c.VaultReference
	if r.SourceContext == "" {
		return ""
	}
	if !credentialVaultReferenceShape(c, *r) {
		return ""
	}
	normalized := *r
	normalized.CredentialBirth = normalized.CredentialBirth.UTC()
	normalized.IntegrationBirth = normalized.IntegrationBirth.UTC()
	raw, _ := json.Marshal(struct {
		Credential    string
		Birth         time.Time
		SourceContext string
		Reference     entity.CredentialVaultReference
	}{c.ID, c.CreatedAt.UTC(), r.SourceContext, normalized})
	return rootHash(string(raw))
}
func attachCredentialSources(tx *gorm.DB, rows []entity.ProviderCredential) error {
	ids := []string{}
	for _, c := range rows {
		if c.StorageSource == "vault" {
			ids = append(ids, c.ID)
		} else if c.StorageSource != "" && c.StorageSource != "inline" {
			return vaultUnavailable
		}
	}
	if len(ids) == 0 {
		return nil
	}
	refs := []entity.CredentialVaultReference{}
	if e := vaultDB(tx).Where(memberModelsExactIDs(tx, "credential_id", ids)).Limit(len(ids) + 1).Find(&refs).Error; e != nil {
		return e
	}

	revisions := []string{}
	for _, r := range refs {
		revisions = append(revisions, r.RevisionID)
	}
	type sourceRow struct {
		RevisionID                                                                                                                                      string
		IntegrationID                                                                                                                                   string
		IntegrationBirth                                                                                                                                time.Time
		CurrentBirth                                                                                                                                    time.Time
		Endpoint, Namespace, Mount, Prefix, DataField, ReaderID, ReaderGeneration, ReaderCiphertext, ReaderMethod                                       string
		CurrentReaderID, CurrentReaderCiphertext, CurrentWriterID, CurrentWriterCiphertext, CurrentRevisionID, CurrentReaderMethod, CurrentWriterMethod string
	}
	contexts := []sourceRow{}
	q := vaultDB(tx).Table("vault_revisions AS rev").Select("rev.id AS revision_id, rev.integration_id,rev.integration_birth,integration.created_at AS current_birth,rev.endpoint,rev.namespace,rev.mount,rev.prefix,rev.data_field,reader.id AS reader_id,reader.secret_generation AS reader_generation,reader.auth_ciphertext AS reader_ciphertext,reader.method AS reader_method,integration.revision_id AS current_revision_id,current_reader.id AS current_reader_id,current_reader.auth_ciphertext AS current_reader_ciphertext,current_writer.id AS current_writer_id,current_writer.auth_ciphertext AS current_writer_ciphertext,current_reader.method AS current_reader_method,current_writer.method AS current_writer_method").Joins("JOIN vault_integrations AS integration ON integration.id = rev.integration_id").Joins("JOIN vault_reader_auth AS reader ON reader.id = rev.id").Joins("JOIN vault_reader_auth AS current_reader ON current_reader.id = integration.revision_id").Joins("JOIN vault_writer_auth AS current_writer ON current_writer.id = integration.revision_id").Where("rev.id IN ?", revisions).Limit(len(revisions) + 1)
	if e := q.Scan(&contexts).Error; e != nil {
		return e
	}
	byRevision := map[string]sourceRow{}
	for _, r := range contexts {
		if _, exists := byRevision[r.RevisionID]; exists {
			return vaultUnavailable
		}
		byRevision[r.RevisionID] = r
	}
	for i := range refs {
		r := &refs[i]
		context, ok := byRevision[r.RevisionID]
		if !ok || context.IntegrationID != r.IntegrationID || !context.IntegrationBirth.Equal(r.IntegrationBirth) || !context.CurrentBirth.Equal(r.IntegrationBirth) || context.ReaderID != r.RevisionID || context.ReaderGeneration != r.ReaderGeneration || !vaultStoredMethod(context.ReaderMethod) || !vaultStoredMethod(context.CurrentReaderMethod) || !vaultStoredMethod(context.CurrentWriterMethod) || context.ReaderCiphertext == "" || context.CurrentReaderCiphertext == "" || context.CurrentWriterCiphertext == "" || !vaultRevisionID.MatchString(context.CurrentRevisionID) || context.CurrentReaderID != context.CurrentRevisionID || context.CurrentWriterID != context.CurrentRevisionID {
			r.SourceContext = ""
			continue
		}
		context.IntegrationBirth = context.IntegrationBirth.UTC()
		context.CurrentBirth = context.CurrentBirth.UTC()
		r.ReaderCiphertext = context.ReaderCiphertext
		r.ReaderMethod = context.ReaderMethod
		r.PhysicalObject, _ = credentialPhysicalObject(entity.VaultRevision{Endpoint: context.Endpoint, Namespace: context.Namespace, Mount: context.Mount, Prefix: context.Prefix, DataField: context.DataField}, r.ReferenceID)
		context.ReaderCiphertext = ""
		// Current configured auth is an eligibility gate, not a source revision.
		// Reconfiguration and root-key rewrap must not repoint retained references.
		context.CurrentReaderID, context.CurrentReaderCiphertext = "", ""
		context.CurrentWriterID, context.CurrentWriterCiphertext = "", ""
		context.CurrentRevisionID, context.CurrentReaderMethod, context.CurrentWriterMethod = "", "", ""
		raw, _ := json.Marshal(context)
		r.SourceContext = rootHash(string(raw))
	}

	byID := map[string]entity.CredentialVaultReference{}
	for _, r := range refs {
		if _, exists := byID[r.CredentialID]; exists {
			return vaultUnavailable
		}
		byID[r.CredentialID] = r
	}
	for i := range rows {
		if rows[i].StorageSource == "vault" {
			r, ok := byID[rows[i].ID]
			if !ok {
				rows[i].VaultReference = nil
				continue
			}
			rows[i].VaultReference = &r
		}
	}
	return nil
}
func (s *Service) credentialReferenceRevision(ctx context.Context, r entity.CredentialVaultReference) (entity.VaultRevision, entity.VaultWriterAuth, entity.VaultReaderAuth, error) {
	var integration entity.VaultIntegration
	var rev entity.VaultRevision
	var w entity.VaultWriterAuth
	var reader entity.VaultReaderAuth
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		current, _, cw, cr, err := vaultSnapshot(tx, r.IntegrationID, false)
		if err != nil || !current.CreatedAt.Equal(r.IntegrationBirth) || cw.AuthCiphertext == "" || cr.AuthCiphertext == "" || !vaultStoredMethod(cw.Method) || !vaultStoredMethod(cr.Method) {
			return vaultUnavailable
		}
		for _, q := range []struct {
			value     any
			field, id string
		}{{&integration, "id", r.IntegrationID}, {&rev, "id", r.RevisionID}, {&w, "id", r.RevisionID}, {&reader, "id", r.RevisionID}} {
			if e := personalExact(vaultDB(tx), q.field, q.id).Take(q.value).Error; e != nil {
				return e
			}
		}
		if integration.ID != r.IntegrationID || !integration.CreatedAt.Equal(r.IntegrationBirth) || rev.ID != r.RevisionID || rev.IntegrationID != r.IntegrationID || !rev.IntegrationBirth.Equal(r.IntegrationBirth) || reader.ID != r.RevisionID || reader.SecretGeneration != r.ReaderGeneration || reader.AuthCiphertext == "" || !vaultStoredMethod(reader.Method) || !vaultStoredMethod(w.Method) || w.ID != r.RevisionID {
			return vaultUnavailable
		}
		return nil
	})
	if e != nil {
		return rev, w, reader, e
	}
	return rev, w, reader, nil
}
func credentialPlan(r entity.CredentialVaultReference) vault.CredentialPlan {
	return vault.CredentialPlan{ReferenceID: r.ReferenceID, ExpectedMarkerSHA256: r.ExpectedMarkerSHA256, DescriptorSHA256: r.DescriptorSHA256}
}
func (s *Service) resolveCredential(ctx context.Context, c entity.ProviderCredential) (string, error) {
	if c.StorageSource == "" || c.StorageSource == "inline" {
		if c.VaultReference != nil {
			return "", vaultUnavailable
		}
		return s.openSecret(c.ID, c.Ciphertext)
	}
	if credentialSourceProof(c) == "" {
		return "", vaultUnavailable
	}
	release, e := s.vaultReadLease()
	if e != nil {
		return "", e
	}
	defer release()
	rev, _, reader, e := s.credentialReferenceRevision(ctx, *c.VaultReference)
	if e != nil {
		return "", vaultUnavailable
	}
	operation, e := s.credentialAdmissionOperation(ctx, rev, *c.VaultReference, reader.Method, "resolve")
	if e != nil {
		return "", vaultUnavailable
	}
	defer operation.close()
	token, e := s.openSecret(rootReference("vault_reader_auth", reader.ID, reader.SecretGeneration), reader.AuthCiphertext)
	if e != nil {
		return "", vaultUnavailable
	}
	token, closeLogin, _, e := operation.token(ctx, reader.Method, token)
	if e != nil {
		return "", vaultUnavailable
	}
	defer closeLogin()
	value, _, e := operation.read(ctx, token, credentialPlan(*c.VaultReference))
	if e != nil {
		return "", vaultUnavailable
	}
	defer value.Close()
	if !operation.holder.admitUse() {
		return "", vaultUnavailable
	}
	bytes := value.Bytes()
	defer clear(bytes)
	return string(bytes), nil
}
func (s *Service) cacheCredentialValue(ctx context.Context, c entity.ProviderCredential, value string) error {
	proof := credentialSourceProof(c)
	if proof == "" || value == "" {
		return vaultUnavailable
	}
	if c.StorageSource != "vault" {
		return nil
	}
	holder, holderErr := s.acquireCredentialSource(c)
	if holderErr != nil {
		return holderErr
	}
	defer holder.release()
	authProof, err := preparedCredentialAuthProof(c, s.openSecret)
	if err != nil {
		return err
	}
	// No global publication/DB lock spans remote reads. This local cache lock
	// serializes pruning with other preparers; compiled attempts own their values.
	s.credentialValuesMu.Lock()
	defer s.credentialValuesMu.Unlock()
	keep, err := s.currentCredentialProofs(ctx)
	if err != nil {
		return err
	}
	if s.credentialValues == nil {
		s.credentialValues = map[string]string{}
	}
	if s.credentialAuthProofs == nil {
		s.credentialAuthProofs = map[string]string{}
	}
	prunePreparedCredentialValues(s.credentialValues, keep)
	prunePreparedCredentialValues(s.credentialAuthProofs, keep)
	if !keep[proof] {
		return catalogConflict
	}
	if _, ok := s.credentialValues[proof]; !ok && len(s.credentialValues) >= 5000 {
		return vaultUnavailable
	}
	if !holder.admitUse() {
		return vaultUnavailable
	}
	s.credentialValues[proof] = value
	s.credentialAuthProofs[proof] = authProof
	return nil
}

// Fresh local references bound retained plaintext lifetime. No remote reads or
// publication locks are needed; already compiled attempts own their values.
func (s *Service) currentCredentialProofs(ctx context.Context) (map[string]bool, error) {
	keep := map[string]bool{}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []entity.ProviderCredential
		if err := tx.Where("storage_source = ?", "vault").Limit(5001).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 5000 {
			return vaultUnavailable
		}
		if err := attachCredentialSources(tx, rows); err != nil {
			return err
		}
		for _, row := range rows {
			if p := credentialSourceProof(row); p != "" {
				keep[p] = true
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return keep, err
}
func (s *Service) pruneCredentialValues(ctx context.Context) error {
	s.credentialValuesMu.Lock()
	defer s.credentialValuesMu.Unlock()
	if len(s.credentialValues) == 0 {
		return nil
	}
	keep, err := s.currentCredentialProofs(ctx)
	if err != nil {
		return err
	}
	prunePreparedCredentialValues(s.credentialValues, keep)
	prunePreparedCredentialValues(s.credentialAuthProofs, keep)
	return nil
}
func prunePreparedCredentialValues(values map[string]string, keep map[string]bool) {
	for proof := range values {
		if !keep[proof] {
			delete(values, proof)
		}
	}
}
func (s *Service) preparedCredentialValue(c entity.ProviderCredential) (string, error) {
	return s.preparedCredentialValueWithReader(c, s.openSecret)
}

func (s *Service) preparedCredentialValueWithReader(c entity.ProviderCredential, openReader func(string, string) (string, error)) (string, error) {
	if c.StorageSource == "" || c.StorageSource == "inline" {
		return s.resolveCredential(context.Background(), c)
	}
	holder, holderErr := s.acquireCredentialSource(c)
	if holderErr != nil {
		return "", holderErr
	}
	defer holder.release()
	proof := credentialSourceProof(c)
	if proof == "" || c.VaultReference.ReaderCiphertext == "" {
		return "", vaultUnavailable
	}
	authProof, e := preparedCredentialAuthProof(c, openReader)
	if e != nil {
		return "", e
	}
	s.credentialValuesMu.RLock()
	value, ok := s.credentialValues[proof]
	cachedAuth := s.credentialAuthProofs[proof]
	s.credentialValuesMu.RUnlock()
	if !ok || cachedAuth == "" || !vaultTextEqual(cachedAuth, authProof) {
		return "", vaultUnavailable
	}
	if !holder.admitUse() {
		return "", vaultUnavailable
	}
	return value, nil
}

// The private digest is local cache state only, never persisted or published.
// Rewrap changes ciphertext, not the authenticated immutable auth material.
func preparedCredentialAuthProof(c entity.ProviderCredential, openReader func(string, string) (string, error)) (string, error) {
	ref := c.VaultReference
	if ref == nil || ref.ReaderCiphertext == "" {
		return "", vaultUnavailable
	}
	material, e := openReader(rootReference("vault_reader_auth", ref.RevisionID, ref.ReaderGeneration), ref.ReaderCiphertext)
	if e != nil {
		return "", vaultUnavailable
	}
	if _, e = vaultAuthMaterial(ref.ReaderMethod, material); e != nil {
		return "", e
	}
	return personalHash(ref.ReaderMethod + "\x00" + material), nil
}

// Startup performs finite preparation before publication locks. Periodic
// authorization refresh and inference only consume already prepared values.
func (s *Service) prepareStartupCredentialValues(ctx context.Context) error {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var rows []entity.ProviderCredential
	if e := s.authDB(ctx).Where("storage_source = ? AND enabled = ? AND verification_status = ?", "vault", true, "verified").Limit(5001).Find(&rows).Error; e != nil {
		return e
	}
	if len(rows) > 5000 {
		return vaultUnavailable
	}
	if e := attachCredentialSources(s.authDB(ctx), rows); e != nil {
		return e
	}
	prepared := map[string]string{}
	authProofs := map[string]string{}
	for _, c := range rows {
		if c.StorageSource != "vault" || credentialSourceProof(c) == "" {
			continue
		}
		holder, holderErr := s.acquireCredentialSource(c)
		if holderErr != nil {
			continue
		}
		defer holder.release()
		value, e := s.resolveCredential(ctx, c)
		if parent.Err() != nil {
			return parent.Err()
		}
		if e != nil {
			// Unavailable external material never becomes an eligible route.
			// Independent inline routes and the management plane remain usable.
			continue
		}
		authProof, e := preparedCredentialAuthProof(c, s.openSecret)
		if e != nil {
			continue
		}
		prepared[credentialSourceProof(c)] = value
		authProofs[credentialSourceProof(c)] = authProof
	}
	s.credentialValuesMu.Lock()
	var startupHolders []*credentialSourceHolder
	defer func() {
		for _, holder := range startupHolders {
			holder.release()
		}
	}()
	for _, c := range rows {
		key, keyErr := credentialHolderKey(c)
		if keyErr != nil {
			continue
		}
		holder, holderErr := s.credentialSources.acquire(key)
		if holderErr != nil {
			delete(prepared, credentialSourceProof(c))
			delete(authProofs, credentialSourceProof(c))
			continue
		}
		if !holder.admitUse() {
			holder.release()
			delete(prepared, credentialSourceProof(c))
			delete(authProofs, credentialSourceProof(c))
			continue
		}
		startupHolders = append(startupHolders, holder)
	}
	s.credentialValues = prepared
	s.credentialAuthProofs = authProofs
	s.credentialValuesMu.Unlock()
	return nil
}

// Inventory checks retained reference structure independently of current remote
// eligibility. Removing a configured reader cannot erase historical dependencies.
func credentialVaultReferenceShape(c entity.ProviderCredential, r entity.CredentialVaultReference) bool {
	return r.CredentialID == c.ID && r.CredentialBirth.Equal(c.CreatedAt) && connectionMetadataBirth(c.CreatedAt) && vaultProbeID.MatchString(r.ReferenceID) && personalModelETag(r.ExpectedMarkerSHA256) && personalModelETag(r.DescriptorSHA256) && vaultIntegrationID.MatchString(r.IntegrationID) && vaultRevisionID.MatchString(r.RevisionID) && rootSafeIdentity(r.ReaderGeneration, 30) && connectionMetadataBirth(r.IntegrationBirth)
}
