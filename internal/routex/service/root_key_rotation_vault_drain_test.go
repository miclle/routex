package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Combine the existing data-only SQL fixtures: real source joins authenticate
// the retained reader while the normal root fixture supplies lease/key proofs.
func rootVaultDrainFixture(t *testing.T) (*Service, *rootPublicationFixture, *credentialStorageFixture, entity.ProviderCredential) {
	t.Helper()
	source, f, _ := credentialStorageSQLService(t)
	i := credentialStorageFixtureIntent(t, source, f, "credential")
	op, err := source.createVaultCredential(context.Background(), "usr_admin", "11111111-1111-4111-8111-111111111111", "private-value", i)
	if err != nil {
		t.Fatal(err)
	}
	svc, root := rootPublicationService(t)
	f.vault.data.reader.AuthCiphertext, err = svc.secrets.Seal(rootReference("vault_reader_auth", f.vault.data.reader.ID, f.vault.data.reader.SecretGeneration), "reader-token")
	if err != nil {
		t.Fatal(err)
	}
	// GORM's source Scan uses the existing SQL fixture, not a fabricated join DTO.
	svc.db.ConnPool = source.db.ConnPool
	svc.db.Statement.ConnPool = source.db.Statement.ConnPool
	query := svc.db.Callback().Query().Get("gorm:query")
	if err := svc.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		switch dst := tx.Statement.Dest.(type) {
		case *[]entity.ProviderCredential:
			*dst = []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
			tx.RowsAffected = 1
		case *[]entity.CredentialVaultReference:
			*dst = []entity.CredentialVaultReference{f.data.refs[op.CredentialID]}
			tx.RowsAffected = 1
		default:
			query(tx)
		}
	}); err != nil {
		t.Fatal(err)
	}
	rows := []entity.ProviderCredential{f.data.credentials[op.CredentialID]}
	if err := attachCredentialSources(svc.db, rows); err != nil {
		t.Fatal(err)
	}
	c := rows[0]
	if err := svc.cacheCredentialValue(context.Background(), c, "private-value"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.rootCurrentProof(svc.db, root.policy); err != nil {
		t.Fatal("open-view proof", err)
	}
	return svc, root, f, c
}

func TestRootRetirementProofAuthenticatesPreparedVaultReaderWhileDrained(t *testing.T) {
	svc, root, f, c := rootVaultDrainFixture(t)
	beforeGET, beforePOST := f.gets, f.posts
	view := svc.rootPolicy.Load()
	if err := svc.drainSecretView(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.preparedCredentialValue(c); !errors.Is(err, vaultUnavailable) {
		t.Fatal("ordinary reader bypassed drain", err)
	}
	if _, err := svc.rootCurrentProofAdmission(svc.db, root.policy, true); err != nil {
		t.Fatal("retirement proof lost unchanged authenticated prepared source", err)
	}
	if !view.closed.Load() || !svc.rootReadersClosed.Load() || view.refs.Load() != 0 || svc.rootReaders.Load() != 0 {
		t.Fatal("proof reopened or retained ordinary readers")
	}
	if f.gets != beforeGET || f.posts != beforePOST {
		t.Fatal("retirement proof performed remote I/O")
	}
}

func TestRootRetirementProofKeepsVaultSourceAndAdmissionFences(t *testing.T) {
	for _, fault := range []string{"reader_tampered", "reader_removed", "reader_generation", "integration_birth", "credential_birth", "missing_cache", "epoch", "write_key", "expired_lease", "drain_not_joined", "reader_drain_missing", "view_drain_missing", "nil_store", "noncurrent_view", "late_noncurrent_view"} {
		t.Run(fault, func(t *testing.T) {
			svc, root, f, c := rootVaultDrainFixture(t)
			ctx := context.Background()
			view := svc.rootPolicy.Load()
			if err := svc.drainSecretView(ctx, view); err != nil {
				t.Fatal(err)
			}
			policy := root.policy
			beforeGET, beforePOST := f.gets, f.posts
			published := svc.runtime.routes.Load()
			switch fault {
			case "reader_tampered":
				f.vault.data.reader.AuthCiphertext = "invalid-envelope"
			case "reader_removed":
				f.vault.data.reader.AuthCiphertext = ""
			case "reader_generation":
				f.vault.data.reader.SecretGeneration = "other-generation"
			case "integration_birth":
				f.vault.data.row.CreatedAt = f.vault.data.row.CreatedAt.Add(time.Second)
			case "credential_birth":
				row := f.data.credentials[c.ID]
				row.CreatedAt = row.CreatedAt.Add(time.Second)
				f.data.credentials[c.ID] = row
			case "missing_cache":
				delete(svc.credentialValues, credentialSourceProof(c))
			case "epoch":
				policy.Epoch++
			case "write_key":
				other := "next"
				policy.WriteKeyID = &other
			case "expired_lease":
				svc.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second)
			case "drain_not_joined":
				view.refs.Store(1)
			case "reader_drain_missing":
				svc.rootReadersClosed.Store(false)
			case "view_drain_missing":
				view.closed.Store(false)
			case "nil_store":
				view.store = nil
			case "noncurrent_view", "late_noncurrent_view":
				query := svc.db.Callback().Query().Get("gorm:query")
				if err := svc.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
					_, credentialQuery := tx.Statement.Dest.(*[]entity.ProviderCredential)
					_, keyQuery := tx.Statement.Dest.(*[]entity.SecretRootKey)
					if (fault == "noncurrent_view" && credentialQuery) || (fault == "late_noncurrent_view" && keyQuery) {
						replacement := &secretPolicyView{epoch: view.epoch, writeID: view.writeID, store: view.store}
						replacement.closed.Store(true)
						svc.rootPolicy.Store(replacement)
					}
					query(tx)
				}); err != nil {
					t.Fatal(err)
				}
			}
			closed, readersClosed := view.closed.Load(), svc.rootReadersClosed.Load()
			if _, err := svc.rootCurrentProofAdmission(svc.db, policy, true); !errors.Is(err, secretStoreUnavailable) {
				t.Fatal("retirement proof accepted invalid current source/admission", err)
			}
			if f.gets != beforeGET || f.posts != beforePOST {
				t.Fatal("failure attempted remote source recovery")
			}
			if view.closed.Load() != closed || svc.rootReadersClosed.Load() != readersClosed || svc.runtime.routes.Load() != published {
				t.Fatal("failure reopened readers or replaced published routes")
			}
		})
	}
}

func TestRootRetirementAllowClosedDoesNotChangeOrdinaryOpenView(t *testing.T) {
	svc, root, f, c := rootVaultDrainFixture(t)
	beforeGET, beforePOST := f.gets, f.posts
	if _, err := svc.rootCurrentProofAdmission(svc.db, root.policy, true); err != nil {
		t.Fatal("open-view compatibility", err)
	}
	if value, err := svc.preparedCredentialValue(c); err != nil || value != "private-value" {
		t.Fatal("ordinary prepared reader changed", err)
	}
	if svc.rootReadersClosed.Load() || svc.rootPolicy.Load().closed.Load() || f.gets != beforeGET || f.posts != beforePOST {
		t.Fatal("open-view proof changed reader state or performed remote I/O")
	}
}
