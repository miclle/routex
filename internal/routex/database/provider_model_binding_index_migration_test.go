package database

import (
	"context"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/schema"
)

func TestProviderModelBindingsFrozenReverseIndex(t *testing.T) {
	parsed, err := schema.Parse(&providerModelBindingIndexV69{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil || parsed.Table != "model_provider_bindings" || !reflect.DeepEqual(parsed.DBNames, []string{"provider_model_id", "model_id", "id"}) || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("unfrozen schema", parsed, err)
	}
	indexes := parsed.ParseIndexes()
	if len(indexes) != 1 || indexes[0].Name != "idx_bindings_provider_model" || indexes[0].Class != "" || len(indexes[0].Fields) != 3 {
		t.Fatal("reverse index not nonunique bounded tuple", indexes)
	}
	for i, column := range []string{"provider_model_id", "model_id", "id"} {
		if indexes[0].Fields[i].DBName != column {
			t.Fatal("index ordering lost", indexes)
		}
	}
}

type providerBindingIndexSQLLog struct {
	logger.Interface
	statements []string
}

func (l *providerBindingIndexSQLLog) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	l.statements = append(l.statements, sql)
}
func TestProviderModelBindingsPortableIndexDDL(t *testing.T) {
	for _, dialect := range []gorm.Dialector{postgres.New(postgres.Config{DSN: "host=localhost user=offline dbname=offline sslmode=disable"}), mysql.New(mysql.Config{DSN: "offline:offline@tcp(localhost:3306)/offline", SkipInitializeWithVersion: true})} {
		log := &providerBindingIndexSQLLog{Interface: logger.Default.LogMode(logger.Silent)}
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: log})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		if err := db.Migrator().CreateIndex(&providerModelBindingIndexV69{}, "idx_bindings_provider_model"); err != nil {
			t.Fatal(err)
		}
		sql := strings.Join(log.statements, " ")
		for _, needle := range []string{"CREATE INDEX", "idx_bindings_provider_model", "model_provider_bindings", "provider_model_id", "model_id", "id"} {
			if !strings.Contains(sql, needle) {
				t.Fatal("portable index DDL missing", dialect.Name(), sql)
			}
		}
		if strings.Contains(sql, "CREATE TABLE") || strings.Contains(sql, "ALTER TABLE") || strings.Contains(sql, "UNIQUE") || len(log.statements) != 1 {
			t.Fatal("index-only migration expanded schema", sql)
		}
		// Compare GORM's frozen key order to the generated ordered index tuple.
		quote := `"`
		if dialect.Name() == "mysql" {
			quote = "`"
		}
		key := "(" + quote + "provider_model_id" + quote + "," + quote + "model_id" + quote + "," + quote + "id" + quote + ")"
		if !strings.Contains(strings.ReplaceAll(sql, " ", ""), key) {
			t.Fatal("leading reverse lookup order lost", sql)
		}
		current, err := schema.Parse(&entity.ModelProviderBinding{}, &sync.Map{}, schema.NamingStrategy{})
		if err != nil || len(current.ParseIndexes()) != 0 {
			t.Fatal("business schema unexpectedly acquires release step", err)
		}
	}
}

type providerBindingIndexTestMigrator struct {
	gorm.Migrator
	present               bool
	createErr             error
	hasCalls, createCalls int
	bad                   bool
}

func (m *providerBindingIndexTestMigrator) HasIndex(value any, name string) bool {
	m.hasCalls++
	_, frozen := value.(*providerModelBindingIndexV69)
	m.bad = m.bad || !frozen || name != "idx_bindings_provider_model"
	return m.present
}
func (m *providerBindingIndexTestMigrator) CreateIndex(value any, name string) error {
	m.createCalls++
	_, frozen := value.(*providerModelBindingIndexV69)
	m.bad = m.bad || !frozen || name != "idx_bindings_provider_model"
	return m.createErr
}

type providerBindingIndexTestDialector struct {
	gorm.Dialector
	migrator *providerBindingIndexTestMigrator
}

func (d providerBindingIndexTestDialector) Migrator(*gorm.DB) gorm.Migrator { return d.migrator }
func TestProviderModelBindingsIndexRepeatAndCreateFailure(t *testing.T) {
	fault := errors.New("controlled index creation failure")
	for _, which := range []string{"new", "retained", "partial_failure"} {
		migration := &providerBindingIndexTestMigrator{present: which == "retained"}
		if which == "partial_failure" {
			migration.createErr = fault
		}
		dialector := providerBindingIndexTestDialector{Dialector: postgres.New(postgres.Config{DSN: "host=localhost user=offline dbname=offline sslmode=disable"}), migrator: migration}
		db, err := gorm.Open(dialector, &gorm.Config{DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		got := providerModelBindingIndexMigration(db)
		if got != migration.createErr || migration.hasCalls != 1 || migration.bad || which == "retained" && migration.createCalls != 0 || which != "retained" && migration.createCalls != 1 {
			t.Fatal("frozen index-only repeat/error boundary changed", which, got, migration)
		}
	}
}
