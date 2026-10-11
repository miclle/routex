package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This is a private nonsecret AAD commitment, never Principal control/isolation
// proof. Versioned fixed JSON types bind every immutable purpose and identity
// fact while preserving the historical root item reference's 160-byte width.
func apiKeyDeliveryAuthReference(auth entity.APIKeyDeliveryAuth) (string, error) {
	if (auth.Purpose != entity.APIKeyDeliveryWriter && auth.Purpose != entity.APIKeyDeliveryProvisioner) || auth.Method != "token" || !rootSafeIdentity(auth.ID, 30) || !rootSafeIdentity(auth.SecretGeneration, 30) || !rootSafeIdentity(auth.IntegrationID, 30) || !rootSafeIdentity(auth.VaultRevisionID, 30) || len(auth.IntegrationRevision) != 64 || auth.CreatedAt.IsZero() || auth.IntegrationBirth.IsZero() || auth.VaultRevisionBirth.IsZero() || !auth.CreatedAt.Equal(auth.CreatedAt.Truncate(time.Microsecond)) || !auth.IntegrationBirth.Equal(auth.IntegrationBirth.Truncate(time.Microsecond)) || !auth.VaultRevisionBirth.Equal(auth.VaultRevisionBirth.Truncate(time.Microsecond)) {
		return "", secretStoreUnavailable
	}
	for _, c := range auth.IntegrationRevision {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return "", secretStoreUnavailable
		}
	}
	tuple := struct {
		SchemaVersion       int    `json:"schema_version"`
		Domain              string `json:"domain"`
		Purpose             string `json:"purpose"`
		AuthID              string `json:"auth_id"`
		AuthBirth           string `json:"auth_birth"`
		Generation          string `json:"generation"`
		IntegrationID       string `json:"integration_id"`
		IntegrationBirth    string `json:"integration_birth"`
		IntegrationRevision string `json:"integration_revision"`
		VaultRevisionID     string `json:"vault_revision_id"`
		VaultRevisionBirth  string `json:"vault_revision_birth"`
	}{1, "api_key_delivery_auth", auth.Purpose, auth.ID, auth.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"), auth.SecretGeneration, auth.IntegrationID, auth.IntegrationBirth.UTC().Format("2006-01-02T15:04:05.000000Z"), auth.IntegrationRevision, auth.VaultRevisionID, auth.VaultRevisionBirth.UTC().Format("2006-01-02T15:04:05.000000Z")}
	encoded, err := json.Marshal(tuple)
	if err != nil {
		return "", secretStoreUnavailable
	}
	return "api-key-delivery:v1:" + auth.Purpose + ":" + rootHash(string(encoded)), nil
}
func (s *Service) rootDeliveryAuthInventoryPage(ctx context.Context, cursor string) ([]rootInventoryRow, error) {
	var auths []entity.APIKeyDeliveryAuth
	q := s.authDB(ctx).Where(clause.Neq{Column: clause.Column{Name: "auth_ciphertext"}, Value: ""})
	if cursor != "" {
		q = q.Where(database.ByteAfter(q, clause.Column{Name: "id"}, cursor))
	}
	if err := q.Clauses(clause.OrderBy{Expression: database.ByteOrder(q, clause.Column{Name: "id"})}).Limit(50).Find(&auths).Error; err != nil {
		return nil, err
	}
	rows := make([]rootInventoryRow, 0, len(auths))
	for _, auth := range auths {
		ref, err := apiKeyDeliveryAuthReference(auth)
		if err != nil {
			return nil, err
		}
		rows = append(rows, rootInventoryRow{auth.ID, auth.SecretGeneration, ref, auth.AuthCiphertext})
	}
	return rows, nil
}

// The whole AAD tuple is read under the row lock before CAS. A concurrent
// purpose/integration substitution is changed/invalid, never rewrapped under a
// reference captured from another tuple. Disabled/retained auth remains covered.
func rootDeliveryAuthCAS(tx *gorm.DB, row rootInventoryRow, result string) (string, error) {
	var current entity.APIKeyDeliveryAuth
	err := personalExact(tx.Session(&gorm.Session{}), "id", row.id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "deleted", nil
	}
	if err != nil {
		return "", err
	}
	ref, err := apiKeyDeliveryAuthReference(current)
	if err != nil {
		return "", err
	}
	if current.ID != row.id || current.SecretGeneration != row.generation || current.AuthCiphertext != row.ciphertext || ref != row.reference {
		return "changed", nil
	}
	update := personalExact(personalExact(tx.Session(&gorm.Session{}).Model(&entity.APIKeyDeliveryAuth{}), "id", row.id), "secret_generation", row.generation).Where(database.ExactText(tx, clause.Column{Name: "auth_ciphertext"}, row.ciphertext)).UpdateColumn("auth_ciphertext", result)
	if update.Error != nil {
		return "", update.Error
	}
	if update.RowsAffected != 1 {
		return "", secretStoreUnavailable
	}
	return "rewrapped", nil
}
