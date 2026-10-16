package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/routeattempt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Not registered or wired into the maintained matrix. Allocate one exclusive,
// fresh disposable routex_test database, then choose exactly one real driver.
// This tests route verification only, not the preceding owner/auth/policy seam.
func TestGatewayReservationSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_GATEWAY_RESERVATION_SQL") != "1" {
		t.Skip("requires the exclusive disposable route-reservation SQL worker")
	}
	driver := os.Getenv("ROUTEX_TEST_GATEWAY_RESERVATION_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one postgres or mysql worker")
	}
	db := runtimeArchiveIntegrationDB(t, driver)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("current schema", err)
	}
	for _, model := range []any{&entity.Model{}, &entity.Provider{}, &entity.ModelPrice{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("requires fresh empty catalogue", err)
		}
	}
	m, facts := gatewayReservationSQLFixture(t)
	// The real supported database timestamp precision is microseconds.
	m.Model.Birth = m.Model.Birth.UTC().Truncate(time.Microsecond)
	facts.model.CreatedAt = m.Model.Birth
	gatewayReservationSQLSeed(t, db, facts)
	validate := func(t *testing.T, original *GatewayReservationManifest, selected *routeattempt.Attempt) error {
		t.Helper()
		return db.Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(modelCreationDB(tx)); err != nil {
				return err
			}
			if selected != nil {
				return validateGatewayReservationDispatchSQL(tx, original, *selected)
			}
			return validateGatewayReservationAcceptSQL(tx, original)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	t.Run("complete_current_capture", func(t *testing.T) {
		if err := validate(t, m, nil); err != nil {
			t.Fatal("complete current reconstruction", err)
		}
	})
	rollback := errors.New("rollback test-only change")
	for _, tc := range []struct {
		name   string
		change func(*gorm.DB) *gorm.DB
	}{
		{"model_disabled", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.Model{}).Where("id = ?", m.Model.ID).Update("status", entity.ResourceDisabled)
		}},
		{"binding_birth", func(tx *gorm.DB) *gorm.DB {
			c := m.Candidates[0]
			return tx.Model(&entity.ModelProviderBinding{}).Where("id = ?", c.Binding.ID).Update("created_at", c.Binding.Birth.Add(time.Second))
		}},
		{"weight_changed", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.ModelProviderBinding{}).Where("id = ?", m.Candidates[0].Binding.ID).Update("weight", 0)
		}},
		{"credential_disabled", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.ProviderCredential{}).Where("id = ?", m.Candidates[0].Credential.Identity.ID).Update("enabled", false)
		}},
		{"credential_birth", func(tx *gorm.DB) *gorm.DB {
			c := m.Candidates[0]
			return tx.Model(&entity.ProviderCredential{}).Where("id = ?", c.Credential.Identity.ID).Update("created_at", c.Credential.Identity.Birth.Add(time.Second))
		}},
		{"connection_transport", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.ProviderConnection{}).Where("id = ?", m.Candidates[0].Connection.ID).Update("base_url", "https://changed.example/v1")
		}},
		{"provider_revision", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.Provider{}).Where("id = ?", m.Candidates[0].Provider.ID).Update("e_tag", "changed")
		}},
		{"provider_model_revision", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.ProviderModel{}).Where("id = ?", m.Candidates[0].ProviderModel.ID).Update("e_tag", "changed")
		}},
		{"coverage_removed", func(tx *gorm.DB) *gorm.DB {
			c := m.Candidates[0]
			return tx.Where("credential_id = ? AND provider_model_id = ?", c.Credential.Identity.ID, c.ProviderModel.ID).Delete(&entity.CredentialModelAccess{})
		}},
		{"capacity_changed", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.ReservationBound{}).Where("provider_model_id = ?", m.Candidates[0].ProviderModel.ID).Update("max_input_tokens", 999)
		}},
		{"unused_fx_changed", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.PricingExchangeRate{}).Where("currency = ?", "EUR").Update("rate", "9")
		}},
		{"price_rate_changed", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.PriceRate{}).Where("id = ?", facts.pricing.Rates[0].ID).Update("amount", "99")
		}},
		{"egress_default_revision", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.EgressSetting{}).Where("id = ?", 1).Update("e_tag", "changed")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := lockGovernance(modelCreationDB(tx)); err != nil {
					return err
				}
				changed := tc.change(tx)
				if changed.Error != nil || changed.RowsAffected != 1 {
					t.Fatal("change control", changed.Error, changed.RowsAffected)
				}
				if err := validateGatewayReservationAcceptSQL(tx, m); err == nil {
					t.Fatal("changed stored constituent admitted")
				}
				return rollback
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if !errors.Is(err, rollback) {
				t.Fatal("negative transaction did not roll back", err)
			}
		})
	}
	t.Run("selected_original_keeps_full_hold", func(t *testing.T) {
		before, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		c := m.Candidates[0]
		other := m.Candidates[1]
		a := routeattempt.Attempt{Number: 2, TargetID: c.Binding.ID, ConnectionID: c.Connection.ID, CredentialID: c.Credential.Identity.ID, Protocol: m.Protocol}
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(modelCreationDB(tx)); err != nil {
				return err
			}
			if q := tx.Model(&entity.Provider{}).Where("id = ?", other.Provider.ID).Update("enabled", false); q.Error != nil || q.RowsAffected != 1 {
				t.Fatal("unselected change", q.Error)
			}
			if validateGatewayReservationAcceptSQL(tx, m) == nil {
				t.Fatal("full original set admitted disabled route")
			}
			if err := validateGatewayReservationDispatchSQL(tx, m, a); err != nil {
				t.Fatal("unchanged original selected route rejected", err)
			}
			return rollback
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		after, err := json.Marshal(m)
		if err != nil || !slices.Equal(before, after) || *m.MaximumHold.Tokens != 210 {
			t.Fatal("selected dispatch shrank original hold/capture")
		}
		a.CredentialID = "crd_new"
		if validate(t, m, &a) == nil {
			t.Fatal("candidate outside original capture admitted")
		}
	})
	t.Run("exact_case_and_birth", func(t *testing.T) {
		copy := gatewayReservationSQLClone(t, m)
		prefix, suffix, ok := strings.Cut(copy.Candidates[0].Credential.Identity.ID, "_")
		if !ok {
			t.Fatal("invalid identity fixture")
		}
		copy.Candidates[0].Credential.Identity.ID = prefix + "_" + strings.ToUpper(suffix)
		if _, err := gatewayReservationSQLSelection(copy, nil); err != nil {
			t.Fatal("case control must reach SQL exact matching", err)
		}
		if validate(t, copy, nil) == nil {
			t.Fatal("collation alias admitted")
		}
		copy = gatewayReservationSQLClone(t, m)
		copy.Model.Birth = copy.Model.Birth.Add(time.Second)
		if validate(t, copy, nil) == nil {
			t.Fatal("same-ID reborn Model admitted")
		}
	})
	t.Run("azure_current_attestation", func(t *testing.T) {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(modelCreationDB(tx)); err != nil {
				return err
			}
			copy := gatewayReservationSQLClone(t, m)
			captured := &copy.Candidates[0]
			c := facts.connections[captured.Connection.ID]
			version := "2024-10-21"
			c.Adapter = entity.AdapterAzureOpenAIClassic
			c.APIVersion = &version
			c.BaseURL = "https://test-resource.openai.azure.com"
			if err := tx.Model(&entity.ProviderConnection{}).Where("id = ?", c.ID).Updates(map[string]any{"adapter": c.Adapter, "api_version": version, "base_url": c.BaseURL}).Error; err != nil {
				t.Fatal(err)
			}
			cr, pm := facts.credentials[captured.Credential.Identity.ID], facts.models[captured.ProviderModel.ID]
			identity := deploymentAttestationIdentity(cr, c, pm)
			if identity == "" {
				t.Fatal("invalid Azure identity fixture")
			}
			row := entity.CredentialDeploymentAttestation{CredentialID: cr.ID, ProviderModelID: pm.ID, Identity: identity}
			if err := tx.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			captured.Adapter = c.Adapter
			captured.APIVersion = &version
			captured.Credential.DeploymentIdentity = identity
			captured.TransportIdentity = gatewayReservationTransportIdentity(gatewayRoute{BaseURL: c.BaseURL, Protocol: c.Protocol, Adapter: c.Adapter, APIVersion: c.APIVersion, UpstreamName: pm.UpstreamName})
			captured.EgressRevision = egressRevision(c, entity.EgressSetting{}, nil)
			if err := validateGatewayReservationAcceptSQL(tx, copy); err != nil {
				t.Fatal("current exact attestation", err)
			}
			if err := tx.Model(&row).Where("credential_id = ? AND provider_model_id = ?", cr.ID, pm.ID).Update("identity", strings.Repeat("0", 64)).Error; err != nil {
				t.Fatal(err)
			}
			if validateGatewayReservationAcceptSQL(tx, copy) == nil {
				t.Fatal("stale attestation admitted")
			}
			return rollback
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	})
	for _, field := range []string{"connection", "credential"} {
		t.Run(field+"_toggle_lock", func(t *testing.T) {
			held := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if held.Error != nil {
				t.Fatal(held.Error)
			}
			defer func() { _ = held.Rollback().Error }()
			if err := lockGovernance(modelCreationDB(held)); err != nil {
				t.Fatal(err)
			}
			if err := validateGatewayReservationAcceptSQL(held, m); err != nil {
				t.Fatal("hold current route", err)
			}
			writerCtx, writerCancel := context.WithTimeout(ctx, 750*time.Millisecond)
			defer writerCancel()
			done := make(chan *gorm.DB, 1)
			go func() {
				q := db.WithContext(writerCtx)
				if field == "connection" {
					done <- q.Model(&entity.ProviderConnection{}).Where("id = ?", m.Candidates[0].Connection.ID).Update("enabled", false)
				} else {
					done <- q.Model(&entity.ProviderCredential{}).Where("id = ?", m.Candidates[0].Credential.Identity.ID).Update("enabled", false)
				}
			}()
			var result *gorm.DB
			select {
			case result = <-done:
			case <-time.After(5 * time.Second):
				writerCancel()
				_ = held.Rollback().Error
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("competing writer did not join")
				}
				t.Fatal("competing writer exceeded deadline")
			}
			if result.Error == nil || result.RowsAffected != 0 || writerCtx.Err() != context.DeadlineExceeded {
				t.Fatal("toggle crossed retained row lock", result.Error, result.RowsAffected)
			}
			if err := held.Rollback().Error; err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("writer_first_current_read", func(t *testing.T) {
		writer := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if writer.Error != nil {
			t.Fatal(writer.Error)
		}
		defer func() { _ = writer.Rollback().Error }()
		// Match the bypass-governance toggle's Connection -> Credential order.
		var connection entity.ProviderConnection
		if err := writer.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&connection, "id = ?", m.Candidates[0].Connection.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := writer.Model(&entity.ProviderCredential{}).Where("id = ?", m.Candidates[0].Credential.Identity.ID).Update("enabled", false).Error; err != nil {
			t.Fatal(err)
		}
		readCtx, readCancel := context.WithTimeout(ctx, 750*time.Millisecond)
		defer readCancel()
		done := make(chan error, 1)
		go func() {
			done <- db.WithContext(readCtx).Transaction(func(tx *gorm.DB) error {
				if err := lockGovernance(modelCreationDB(tx)); err != nil {
					return err
				}
				return validateGatewayReservationAcceptSQL(tx, m)
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		}()
		select {
		case err := <-done:
			if err == nil || readCtx.Err() != context.DeadlineExceeded {
				t.Fatal("current loader bypassed writer", err)
			}
		case <-time.After(5 * time.Second):
			readCancel()
			_ = writer.Rollback().Error
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("blocked reader did not join")
			}
			t.Fatal("blocked reader exceeded deadline")
		}
		if err := writer.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if validate(t, m, nil) == nil {
			t.Fatal("committed writer-first revocation not observed")
		}
		if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", m.Candidates[0].Credential.Identity.ID).Update("enabled", true).Error; err != nil {
			t.Fatal(err)
		}
	})
	if err := validate(t, m, nil); err != nil {
		t.Fatal("restored original capture", err)
	}
}

func gatewayReservationSQLClone(t *testing.T, m *GatewayReservationManifest) *GatewayReservationManifest {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var copy GatewayReservationManifest
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	return &copy
}

func gatewayReservationSQLSeed(t *testing.T, db *gorm.DB, f *gatewayReservationSQLFacts) {
	t.Helper()
	create := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal("route fixture create", err)
		}
	}
	create(&f.model)
	for _, row := range f.providers {
		row.Name = row.ID
		create(&row)
	}
	for _, row := range f.connections {
		row.Name = row.ID
		create(&row)
	}
	for _, row := range f.credentials {
		row.Name = row.ID
		row.Ciphertext = "test-only-unresolved-ciphertext"
		create(&row)
	}
	for _, row := range f.models {
		create(&row)
	}
	for _, row := range f.bindings {
		create(&row)
	}
	for pair := range f.access {
		row := entity.CredentialModelAccess{CredentialID: pair[0], ProviderModelID: pair[1]}
		create(&row)
	}
	for _, row := range f.quota.Bounds {
		create(&row)
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Updates(map[string]any{"e_tag": f.pricing.Setting.ETag, "platform_currency": f.pricing.Setting.PlatformCurrency}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("1 = 1").Delete(&entity.PricingExchangeRate{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range f.pricing.FX {
		create(&row)
	}
	for _, row := range f.pricing.Prices {
		row.UpdateSource = "manual"
		create(&row)
	}
	for _, row := range f.pricing.Rates {
		create(&row)
	}
	// The pure capture fixture uses legacy default-direct empty revision. This
	// row is test-only; no production setter or runtime publication is invoked.
	if err := db.Model(&entity.EgressSetting{}).Where("id = ?", 1).Updates(map[string]any{"e_tag": "", "default_egress_id": nil}).Error; err != nil {
		t.Fatal(err)
	}
}
