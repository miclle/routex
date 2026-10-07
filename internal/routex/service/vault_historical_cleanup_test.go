package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestVaultHistoricalSnapshotUsesFreshExactReceivers(t *testing.T) {
	const historicalID = "vlr_01m49mwfqdq8ep8tayx37ng0ht"
	const currentID = "vlr_01m49mwfxsptm4pdv5erq8vgsy"
	birth := time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)
	row := entity.VaultIntegration{ID: "vlt_01m49mwfdz0v15rvpm6y6wfcn8", RevisionID: currentID, CreatedAt: birth}
	for _, driver := range []struct {
		name      string
		dialector gorm.Dialector
	}{
		{"postgres", postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"})},
		{"mysql", mysql.New(mysql.Config{DSN: "test:test@tcp(localhost:3306)/test", SkipInitializeWithVersion: true})},
	} {
		t.Run(driver.name, func(t *testing.T) {
			db, err := gorm.Open(driver.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pool.Close() })
			// Reproduce the failed production receivers using the pinned GORM query
			// builder. NewDB does not remove a nonzero destination primary key.
			for _, receiver := range []any{&entity.VaultRevision{ID: currentID}, &entity.VaultWriterAuth{ID: currentID}, &entity.VaultReaderAuth{ID: currentID}} {
				q := personalExact(vaultDB(db), "id", historicalID).Take(receiver)
				if q.Error != nil || !reflect.DeepEqual(q.Statement.Vars, []any{historicalID, currentID, 1}) || !strings.Contains(q.Statement.SQL.String(), " AND ") {
					t.Fatal("old receiver no longer reproduces incompatible primary-key predicates", q.Error, q.Statement.SQL.String(), q.Statement.Vars)
				}
			}
			for _, scenario := range []string{"valid", "missing_revision", "revision_alias", "cross_integration", "different_birth", "missing_writer", "writer_alias", "missing_reader", "reader_alias"} {
				t.Run(scenario, func(t *testing.T) {
					selected := []string{}
					callback := "test:historical_vault_rows"
					if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
						if !reflect.DeepEqual(tx.Statement.Vars, []any{historicalID, 1}) {
							t.Fatal("query inherited current destination ID", tx.Statement.Vars)
						}
						selected = append(selected, tx.Statement.Table)
						tx.RowsAffected = 1
						switch dest := tx.Statement.Dest.(type) {
						case *entity.VaultRevision:
							*dest = entity.VaultRevision{ID: historicalID, IntegrationID: row.ID, IntegrationBirth: birth}
							if scenario == "missing_revision" {
								_ = tx.AddError(gorm.ErrRecordNotFound)
							}
							if scenario == "revision_alias" {
								dest.ID = strings.ToUpper(historicalID)
							}
							if scenario == "cross_integration" {
								dest.IntegrationID = "vlt_01m49mwfdz0v15rvpm6y6wfcn9"
							}
							if scenario == "different_birth" {
								dest.IntegrationBirth = birth.Add(time.Microsecond)
							}
						case *entity.VaultWriterAuth:
							*dest = entity.VaultWriterAuth{ID: historicalID, SecretGeneration: "retained-writer"}
							if scenario == "missing_writer" {
								_ = tx.AddError(gorm.ErrRecordNotFound)
							}
							if scenario == "writer_alias" {
								dest.ID = strings.ToUpper(historicalID)
							}
						case *entity.VaultReaderAuth:
							*dest = entity.VaultReaderAuth{ID: historicalID, SecretGeneration: "retained-reader"}
							if scenario == "missing_reader" {
								_ = tx.AddError(gorm.ErrRecordNotFound)
							}
							if scenario == "reader_alias" {
								dest.ID = strings.ToUpper(historicalID)
							}
						default:
							t.Fatal("unexpected historical table")
						}
					}); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
					rev, writer, reader, gotErr := vaultHistoricalSnapshot(db, row, historicalID)
					if scenario == "valid" {
						if gotErr != nil || rev.ID != historicalID || writer.ID != historicalID || reader.ID != historicalID || !rev.IntegrationBirth.Equal(birth) || writer.SecretGeneration != "retained-writer" || reader.SecretGeneration != "retained-reader" || !reflect.DeepEqual(selected, []string{"vault_revisions", "vault_writer_auth", "vault_reader_auth"}) {
							t.Fatal("historical snapshot did not retain exact rows", gotErr, selected)
						}
					} else {
						want := vaultUnavailable
						if strings.HasPrefix(scenario, "missing_") {
							if !errors.Is(gotErr, gorm.ErrRecordNotFound) {
								t.Fatal("missing historical row accepted", gotErr)
							}
						} else if !errors.Is(gotErr, want) {
							t.Fatal("historical identity/birth mismatch accepted", gotErr)
						}
						if len(selected) > 3 {
							t.Fatal("historical query bound exceeded")
						}
					}
					if row.RevisionID != currentID || !row.CreatedAt.Equal(birth) {
						t.Fatal("current integration mutated")
					}
				})
			}
		})
	}
}
