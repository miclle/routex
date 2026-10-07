package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/upstream"
)

var (
	catalogConflict        = &apperrors.Error{Code: http.StatusConflict, Message: "resource conflicts with the current catalog"}
	credentialNotReady     = &apperrors.Error{Code: http.StatusConflict, Message: "credential or model has not been verified for this connection"}
	secretStoreUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "credential storage is not configured"}
)

type ProviderCatalog struct {
	Provider    entity.Provider
	Connections []ConnectionCatalog
}

type ConnectionCatalog struct {
	Connection  entity.ProviderConnection
	Credentials []entity.ProviderCredential
	Models      []entity.ProviderModel
}

type CreateConnectionInput struct {
	Adapter           string  `json:",omitempty"`
	APIVersion        *string `json:",omitempty"`
	RequestID         string
	StoragePolicyETag string
	EgressMode        string
	EgressID          *string
	Name              string
	BaseURL           string
	Protocol          string
	CredentialName    string
	Secret            string
}

func catalogError(err error) error {
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
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return catalogConflict
	}
	return apperrors.ErrInternal
}

func validCatalogLabel(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 100 && !strings.ContainsFunc(value, unicode.IsControl)
}

func validUpstreamName(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 255 && !strings.ContainsFunc(value, unicode.IsControl)
}

func (s *Service) ListProviders(ctx context.Context) ([]ProviderCatalog, error) {
	db := s.authDB(ctx)
	var rows []entity.Provider
	if err := db.Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, catalogError(err)
	}
	result := make([]ProviderCatalog, 0, len(rows))
	for _, provider := range rows {
		item, err := loadProviderCatalog(db, provider.ID)
		if err != nil {
			return nil, catalogError(err)
		}
		result = append(result, *item)
	}
	return result, nil
}

func loadProviderCatalog(db *gorm.DB, providerID string) (*ProviderCatalog, error) {
	result := &ProviderCatalog{Connections: []ConnectionCatalog{}}
	if err := db.First(&result.Provider, "id = ?", providerID).Error; err != nil {
		return nil, err
	}
	var connections []entity.ProviderConnection
	if err := db.Where("provider_id = ?", providerID).Order("created_at, id").Find(&connections).Error; err != nil {
		return nil, err
	}
	for _, connection := range connections {
		item, err := loadConnectionCatalog(db, connection.ID)
		if err != nil {
			return nil, err
		}
		result.Connections = append(result.Connections, *item)
	}
	return result, nil
}

func loadConnectionCatalog(db *gorm.DB, connectionID string) (*ConnectionCatalog, error) {
	result := &ConnectionCatalog{Credentials: []entity.ProviderCredential{}, Models: []entity.ProviderModel{}}
	if err := db.First(&result.Connection, "id = ?", connectionID).Error; err != nil {
		return nil, err
	}
	// Ciphertext is deliberately excluded even from the internal listing result.
	if err := db.Omit("ciphertext").Where("connection_id = ?", connectionID).Order("priority, created_at, id").Find(&result.Credentials).Error; err != nil {
		return nil, err
	}
	if err := db.Where("connection_id = ?", connectionID).Order("upstream_name").Find(&result.Models).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) prepareConnectionMetadata(providerID string, input CreateConnectionInput) (entity.ProviderConnection, error) {
	connection := entity.ProviderConnection{Adapter: input.Adapter, APIVersion: input.APIVersion, Enabled: true, EgressMode: input.EgressMode, EgressID: input.EgressID, ETag: "0", ProviderID: providerID, Name: strings.TrimSpace(input.Name), Protocol: input.Protocol}
	if connection.EgressMode == "" {
		connection.EgressMode = "default"
	}
	if (connection.EgressMode != "default" && connection.EgressMode != "direct" && connection.EgressMode != "proxy") || (connection.EgressMode == "proxy") != (connection.EgressID != nil) || (connection.EgressID != nil && *connection.EgressID == "") {
		return connection, apperrors.ErrBadRequest
	}

	if !validCatalogLabel(connection.Name) || !entity.SupportedNativeProtocol(input.Protocol) {
		return connection, apperrors.ErrBadRequest
	}
	baseURL, err := upstream.ValidateBaseURL(input.BaseURL, s.allowPrivateUpstream)
	if err != nil {
		return connection, apperrors.ErrBadRequest
	}
	connection.BaseURL = input.BaseURL
	if connection.Adapter == "" {
		connection.Adapter = entity.AdapterNative
	}
	if err := validateConnectionAdapter(connection, s.allowPrivateUpstream); err != nil {
		return connection, err
	}
	connection.BaseURL = strings.TrimRight(baseURL.String(), "/")
	connection.ID, err = id.NewPrefixed("con")
	if err != nil {
		return connection, apperrors.ErrInternal
	}
	return connection, nil
}

func (s *Service) prepareConnection(providerID string, input CreateConnectionInput) (entity.ProviderConnection, entity.ProviderCredential, error) {
	c, e := s.prepareConnectionMetadata(providerID, input)
	if e != nil {
		return c, entity.ProviderCredential{}, e
	}
	credential, e := s.prepareCredential(c.ID, input.CredentialName, input.Secret, 0)
	return c, credential, e
}
func prepareCredentialMetadata(connectionID, name string, priority int) (entity.ProviderCredential, error) {
	c := entity.ProviderCredential{ConnectionID: connectionID, Name: strings.TrimSpace(name), Priority: priority, StorageSource: "inline", VerificationStatus: "pending"}
	if !validCatalogLabel(c.Name) || priority < 0 || priority > 10000 {
		return c, apperrors.ErrBadRequest
	}
	var e error
	c.ID, e = id.NewPrefixed("crd")
	return c, e
}

func (s *Service) prepareCredential(connectionID, name, plaintext string, priority int) (entity.ProviderCredential, error) {
	credential := entity.ProviderCredential{ConnectionID: connectionID, Name: strings.TrimSpace(name), Priority: priority, VerificationStatus: "pending"}
	if !validCatalogLabel(credential.Name) || plaintext == "" || len(plaintext) > 2048 || strings.ContainsAny(plaintext, "\r\n") || priority < 0 || priority > 10000 {
		return credential, apperrors.ErrBadRequest
	}
	if s.secrets == nil {
		return credential, secretStoreUnavailable
	}
	var err error
	credential.ID, err = id.NewPrefixed("crd")
	if err != nil {
		return credential, apperrors.ErrInternal
	}
	credential.Ciphertext, err = s.sealSecret(credential.ID, plaintext)
	if err != nil {
		return credential, catalogError(err)
	}
	return credential, nil
}

func (s *Service) CreateProvider(ctx context.Context, actorID, name string, input CreateConnectionInput) (*ProviderCatalog, error) {
	vaultMode, e := s.useVaultCreation(ctx, actorID, input.RequestID, input.StoragePolicyETag)
	if e != nil {
		return nil, e
	}
	if vaultMode {
		op, e := s.createVaultCredential(ctx, actorID, input.RequestID, input.Secret, credentialCreationIntent{Kind: "provider", ProviderName: strings.TrimSpace(name), Connection: input, CredentialName: input.CredentialName, PolicyETag: input.StoragePolicyETag})
		if e != nil {
			return nil, e
		}
		result, e := s.createdProviderCatalog(ctx, op.CredentialStorageOperation)
		return result, catalogError(e)
	}
	if input.RequestID != "" {
		op, e := s.createInlineCredential(ctx, actorID, input.RequestID, input.Secret, credentialCreationIntent{Kind: "provider", ProviderName: strings.TrimSpace(name), Connection: input, CredentialName: input.CredentialName, PolicyETag: input.StoragePolicyETag})
		if e != nil {
			return nil, e
		}
		result, e := s.createdProviderCatalog(ctx, op.CredentialStorageOperation)
		return result, catalogError(e)
	}
	name = strings.TrimSpace(name)
	if !validCatalogLabel(name) {
		return nil, apperrors.ErrBadRequest
	}
	providerID, err := id.NewPrefixed("prv")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	provider := entity.Provider{ID: providerID, Name: name}
	preparedEpoch := s.secretEpoch()
	connection, credential, err := s.prepareConnection(providerID, input)
	if err != nil {
		return nil, err
	}
	db := s.authDB(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		if _, _, _, err := s.resolveConnectionEgress(tx, connection); err != nil {
			return err
		}

		if err := tx.Create(&provider).Error; err != nil {
			return err
		}
		if err := tx.Create(&connection).Error; err != nil {
			return err
		}
		if err := requireInlineCredentialPolicy(tx); err != nil {
			return err
		}
		if err := s.guardSecretWrite(tx, preparedEpoch, credential.ID, credential.Ciphertext); err != nil {
			return err
		}
		if err := tx.Create(&credential).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "provider.create", "provider", providerID)
	})
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadProviderCatalog(db, providerID)
	return result, catalogError(err)
}

func (s *Service) CreateConnection(ctx context.Context, actorID, providerID string, input CreateConnectionInput) (*ConnectionCatalog, error) {
	vaultMode, e := s.useVaultCreation(ctx, actorID, input.RequestID, input.StoragePolicyETag)
	if e != nil {
		return nil, e
	}
	if vaultMode {
		op, e := s.createVaultCredential(ctx, actorID, input.RequestID, input.Secret, credentialCreationIntent{Kind: "connection", Target: providerID, Connection: input, CredentialName: input.CredentialName, PolicyETag: input.StoragePolicyETag})
		if e != nil {
			return nil, e
		}
		result, e := s.createdConnectionCatalog(ctx, op.CredentialStorageOperation)
		return result, catalogError(e)
	}
	if input.RequestID != "" {
		op, e := s.createInlineCredential(ctx, actorID, input.RequestID, input.Secret, credentialCreationIntent{Kind: "connection", Target: providerID, Connection: input, CredentialName: input.CredentialName, PolicyETag: input.StoragePolicyETag})
		if e != nil {
			return nil, e
		}
		result, e := s.createdConnectionCatalog(ctx, op.CredentialStorageOperation)
		return result, catalogError(e)
	}
	preparedEpoch := s.secretEpoch()
	connection, credential, err := s.prepareConnection(providerID, input)
	if err != nil {
		return nil, err
	}
	db := s.authDB(ctx)
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		if _, _, _, err := s.resolveConnectionEgress(tx, connection); err != nil {
			return err
		}

		var provider entity.Provider
		if err := tx.First(&provider, "id = ?", providerID).Error; err != nil {
			return err
		}
		if err := tx.Create(&connection).Error; err != nil {
			return err
		}
		if err := requireInlineCredentialPolicy(tx); err != nil {
			return err
		}
		if err := s.guardSecretWrite(tx, preparedEpoch, credential.ID, credential.Ciphertext); err != nil {
			return err
		}
		if err := tx.Create(&credential).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "connection.create", "connection", connection.ID)
	})
	if err := s.refreshAfterMutation(ctx, catalogError(err)); err != nil {
		return nil, err
	}
	result, err := loadConnectionCatalog(db, connection.ID)
	return result, catalogError(err)
}

func (s *Service) CreateCredential(ctx context.Context, actorID, connectionID, name, plaintext string, priority int, requestIDs ...string) (*entity.ProviderCredential, error) {
	requestID := ""
	if len(requestIDs) > 2 {
		return nil, apperrors.ErrBadRequest
	}
	if len(requestIDs) >= 1 {
		requestID = requestIDs[0]
	}
	policyETag := ""
	if len(requestIDs) == 2 {
		policyETag = requestIDs[1]
	}
	vaultMode, e := s.useVaultCreation(ctx, actorID, requestID, policyETag)
	if e != nil {
		return nil, e
	}
	if vaultMode {
		op, e := s.createVaultCredential(ctx, actorID, requestID, plaintext, credentialCreationIntent{Kind: "credential", Target: connectionID, CredentialName: strings.TrimSpace(name), Priority: priority, PolicyETag: policyETag})
		if e != nil {
			return nil, e
		}
		var result entity.ProviderCredential
		e = s.authDB(ctx).Omit("ciphertext").First(&result, "id = ?", op.CredentialID).Error
		return &result, catalogError(e)
	}
	if requestID != "" {
		op, e := s.createInlineCredential(ctx, actorID, requestID, plaintext, credentialCreationIntent{Kind: "credential", Target: connectionID, CredentialName: strings.TrimSpace(name), Priority: priority, PolicyETag: policyETag})
		if e != nil {
			return nil, e
		}
		var result entity.ProviderCredential
		e = s.authDB(ctx).Omit("ciphertext").First(&result, "id = ?", op.CredentialID).Error
		return &result, catalogError(e)
	}
	preparedEpoch := s.secretEpoch()
	credential, err := s.prepareCredential(connectionID, name, plaintext, priority)
	if err != nil {
		return nil, err
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "providers.write"); err != nil {
			return err
		}
		var connection entity.ProviderConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", connectionID).Error; err != nil {
			return err
		}
		if err := checkCredentialName(tx, connectionID, "", credential.Name); err != nil {
			return err
		}
		if err := requireInlineCredentialPolicy(tx); err != nil {
			return err
		}
		if err := s.guardSecretWrite(tx, preparedEpoch, credential.ID, credential.Ciphertext); err != nil {
			return err
		}
		if err := tx.Create(&credential).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "credential.create", "credential", credential.ID)
	})
	credential.Ciphertext = ""
	return &credential, s.refreshAfterMutation(ctx, catalogError(err))
}

func (s *Service) SetCredentialEnabled(ctx context.Context, actorID, credentialID string, enabled bool) (*entity.ProviderCredential, error) {
	preparedProof := ""
	if enabled {
		var c entity.ProviderCredential
		err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
			if e := exactCatalogPermission(tx, actorID, "providers.write"); e != nil {
				return e
			}
			if e := personalExact(vaultDB(tx), "id", credentialID).Take(&c).Error; e != nil {
				return e
			}
			rows := []entity.ProviderCredential{c}
			if e := attachCredentialSources(tx, rows); e != nil {
				return e
			}
			c = rows[0]
			return nil
		})
		if err != nil {
			return nil, catalogError(err)
		}
		preparedProof = credentialSourceProof(c)
		value, err := s.resolveCredential(ctx, c)
		if err != nil {
			return nil, vaultUnavailable
		}
		if err = s.cacheCredentialValue(ctx, c, value); err != nil {
			return nil, err
		}
	}

	var credential entity.ProviderCredential
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&credential, "id = ?", credentialID).Error; err != nil {
			return err
		}
		var connection entity.ProviderConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", credential.ConnectionID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&credential, "id = ?", credentialID).Error; err != nil {
			return err
		}
		if enabled {
			rows := []entity.ProviderCredential{credential}
			if e := attachCredentialSources(tx, rows); e != nil {
				return e
			}
			if credentialSourceProof(rows[0]) != preparedProof {
				return catalogConflict
			}
			if credential.VerificationStatus != "verified" {
				return credentialNotReady
			}
			var missing int64
			if err := tx.Table("model_provider_bindings AS b").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Where("p.connection_id = ? AND b.weight > 0 AND NOT EXISTS (SELECT 1 FROM credential_model_accesses a WHERE a.provider_model_id = p.id AND a.credential_id = ?)", connection.ID, credentialID).Count(&missing).Error; err != nil {
				return err
			}
			if entity.ConnectionAdapter(connection) == entity.AdapterAzureOpenAIClassic {
				absent, e := credentialMissingActiveDeployment(tx, connection, credential.ID)
				if e != nil {
					return e
				}
				missing = 0
				if absent {
					missing = 1
				}
			}
			if missing != 0 {
				return credentialNotReady
			}
		}
		if err := tx.Model(&credential).Update("enabled", enabled).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "credential.update", "credential", credentialID)
	})
	if err == nil && !enabled {
		s.InvalidateRuntimeCredential(credentialID)
	}
	credential.Ciphertext = ""
	return &credential, s.refreshAfterMutation(ctx, catalogError(err))
}

func (s *Service) CreateProviderModel(ctx context.Context, actorID, connectionID, upstreamName string) (*entity.ProviderModel, error) {
	if !validUpstreamName(upstreamName) {
		return nil, apperrors.ErrBadRequest
	}
	modelID, err := id.NewPrefixed("pmd")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	model := entity.ProviderModel{ID: modelID, ConnectionID: connectionID, UpstreamName: upstreamName}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var connection entity.ProviderConnection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&connection, "id = ?", connectionID).Error; err != nil {
			return err
		}
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "provider_model.create", "provider_model", modelID)
	})
	return &model, s.refreshAfterMutation(ctx, catalogError(err))
}
