package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type repositoryReceiptDriver struct{ fixture *repositoryReceiptFixture }
type repositoryReceiptFixture struct {
	receipt   entity.RepositoryPriceReceipt
	queries   []string
	mutations int
	rate      *entity.PriceRate
	affected  int64
}

func (d repositoryReceiptDriver) Open(string) (driver.Conn, error) {
	return repositoryReceiptConnection(d), nil
}
func (d repositoryReceiptDriver) Connect(context.Context) (driver.Conn, error) {
	return repositoryReceiptConnection(d), nil
}
func (d repositoryReceiptDriver) Driver() driver.Driver { return d }

type repositoryReceiptConnection struct{ fixture *repositoryReceiptFixture }

func (repositoryReceiptConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected preparation")
}
func (repositoryReceiptConnection) Close() error { return nil }
func (repositoryReceiptConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c repositoryReceiptConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.fixture.queries = append(c.fixture.queries, query)
	if c.fixture.rate != nil {
		return &repositoryRateRows{rate: *c.fixture.rate}, nil
	}
	return &repositoryReceiptRows{receipt: c.fixture.receipt}, nil
}
func (c repositoryReceiptConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.fixture.mutations++
	if c.fixture.rate != nil {
		return driver.RowsAffected(c.fixture.affected), nil
	}
	return nil, errors.New("receipt lookup attempted a mutation")
}

type repositoryReceiptRows struct {
	receipt entity.RepositoryPriceReceipt
	done    bool
}

func (*repositoryReceiptRows) Columns() []string {
	return []string{"request_id", "actor_id", "request_hash"}
}
func (*repositoryReceiptRows) Close() error { return nil }
func (r *repositoryReceiptRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = r.receipt.RequestID
	values[1] = r.receipt.ActorID
	values[2] = r.receipt.RequestHash
	return nil
}
func TestRepositoryKnownReceiptRequiresExactOwnerOriginalHashAndNoReplayMutation(t *testing.T) {
	requestID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			fixture := &repositoryReceiptFixture{receipt: entity.RepositoryPriceReceipt{RequestID: requestID, ActorID: "usr_owner", RequestHash: strings.Repeat("a", 64)}}
			pool := sql.OpenDB(repositoryReceiptDriver{fixture})
			t.Cleanup(func() { _ = pool.Close() })
			adapter := postgres.New(postgres.Config{Conn: pool})
			if dialect == "mysql" {
				adapter = mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true})
			}
			db, err := gorm.Open(adapter, &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			var receipt entity.RepositoryPriceReceipt
			replay, err := repositoryKnownReceipt(db, "usr_owner", requestID, strings.Repeat("a", 64), &receipt)
			if err != nil || !replay || fixture.mutations != 0 || len(fixture.queries) != 1 {
				t.Fatal("known receipt reapplied mutation", err)
			}
			if dialect == "postgres" && (!strings.Contains(fixture.queries[0], `"request_id" = $1`) || strings.Contains(fixture.queries[0], requestID)) {
				t.Fatal("lookup borrowed collation alias")
			}
			if dialect == "mysql" && !strings.Contains(fixture.queries[0], "CAST(") {
				t.Fatal("lookup borrowed collation alias")
			}
			for _, mode := range []string{"owner", "request_alias", "changed_body"} {
				t.Run(mode, func(t *testing.T) {
					fixture.receipt = entity.RepositoryPriceReceipt{RequestID: requestID, ActorID: "usr_owner", RequestHash: strings.Repeat("a", 64)}
					hash := strings.Repeat("a", 64)
					switch mode {
					case "owner":
						fixture.receipt.ActorID = "usr_Owner"
					case "request_alias":
						fixture.receipt.RequestID = strings.ToUpper(requestID)
					case "changed_body":
						hash = strings.Repeat("b", 64)
					}
					if known, err := repositoryKnownReceipt(db, "usr_owner", requestID, hash, &receipt); known || err == nil || fixture.mutations != 0 {
						t.Fatal("foreign/changed intent reconciled")
					}
				})
			}
		})
	}
}

type repositoryRateRows struct {
	rate entity.PriceRate
	done bool
}

func (*repositoryRateRows) Columns() []string {
	return []string{"model_price_id", "metric", "tier", "repository_model_key", "repository_rate_key"}
}
func (*repositoryRateRows) Close() error { return nil }
func (r *repositoryRateRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0], values[1], values[2] = r.rate.ModelPriceID, r.rate.Metric, r.rate.Tier
	if r.rate.RepositoryModelKey != nil {
		values[3] = *r.rate.RepositoryModelKey
	}
	if r.rate.RepositoryRateKey != nil {
		values[4] = *r.rate.RepositoryRateKey
	}
	return nil
}
func TestRepositoryRateOwnershipNoChangeRequiresExactStoredProof(t *testing.T) {
	modelKey, rateKey := "source/model", "source/input"
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			for _, mode := range []string{"unchanged", "changed", "model_alias", "metric_alias", "source_alias", "missing_source", "multiple_rows"} {
				t.Run(mode, func(t *testing.T) {
					rate := entity.PriceRate{ModelPriceID: "prc_exact", Metric: pricing.Input, Tier: pricing.Base, RepositoryModelKey: &modelKey, RepositoryRateKey: &rateKey}
					fixture := &repositoryReceiptFixture{rate: &rate}
					switch mode {
					case "changed":
						fixture.affected = 1
					case "model_alias":
						rate.ModelPriceID = "prc_Exact"
					case "metric_alias":
						rate.Metric = strings.ToLower(pricing.Input)
					case "source_alias":
						alias := "source/Input"
						rate.RepositoryRateKey = &alias
					case "missing_source":
						rate.RepositoryModelKey = nil
					case "multiple_rows":
						fixture.affected = 2
					}
					pool := sql.OpenDB(repositoryReceiptDriver{fixture})
					t.Cleanup(func() { _ = pool.Close() })
					adapter := postgres.New(postgres.Config{Conn: pool})
					if dialect == "mysql" {
						adapter = mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true})
					}
					db, err := gorm.Open(adapter, &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
					if err != nil {
						t.Fatal(err)
					}
					err = repositorySetRateSource(db, "prc_exact", RepositoryPriceChange{SourceModelKey: modelKey, SourceRateKey: rateKey, After: &pricing.Rate{Metric: pricing.Input, Tier: pricing.Base}})
					valid := mode == "unchanged" || mode == "changed"
					if (err == nil) != valid || fixture.mutations != 1 {
						t.Fatal("ownership update lost exact row/source proof", err)
					}
					if mode == "unchanged" && len(fixture.queries) != 1 {
						t.Fatal("unchanged ownership lacked stored proof")
					}
					if mode == "changed" && len(fixture.queries) != 0 {
						t.Fatal("changed exact row unnecessarily reread")
					}
				})
			}
		})
	}
}

// A persisted receipt remains available when renewed current-state lookup fails.
// Exercise its real projection, not a duplicate DTO constructor. The SQL driver
// deliberately refuses Begin, so no database service or current authority is
// invented to test the historical HTTP identity boundary.
func TestRepositoryReceiptProjectionHasCanonicalHTTPIdentity(t *testing.T) {
	instant := time.Date(2026, 10, 4, 7, 26, 23, 156539000, time.UTC)
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("Local", 0), time.FixedZone("Recorded offset", 8*60*60)} {
		for _, mode := range []string{"configure", "sync", "restore"} {
			t.Run(zone.String()+"/"+mode, func(t *testing.T) {
				original := entity.RepositoryPriceReceipt{RequestID: "49000000-0000-4000-8000-000000000006", ActorID: "usr_owner", SourceDigest: strings.Repeat("a", 64), Mode: mode, CreatedAt: instant.In(zone)}
				preserved := original
				fixture := &repositoryReceiptFixture{}
				pool := sql.OpenDB(repositoryReceiptDriver{fixture})
				t.Cleanup(func() { _ = pool.Close() })
				db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				svc := &Service{db: db}
				projected := svc.repositoryResult(context.Background(), original.ActorID, original, false)
				if !projected.Committed || projected.RuntimeApplied || projected.ConfigurationApplied || projected.Configuration != nil || projected.ApplicationStatus != "unavailable" {
					t.Fatal("historical receipt inferred current authority", projected)
				}
				if !reflect.DeepEqual(original, preserved) || fixture.mutations != 0 || len(fixture.queries) != 0 {
					t.Fatal("receipt projection mutated persisted input or current state")
				}
				receipt := projected.Receipt
				if receipt.RequestID != original.RequestID || receipt.SourceDigest != original.SourceDigest || receipt.Mode != original.Mode || !receipt.CreatedAt.Equal(original.CreatedAt) || receipt.CreatedAt.UnixMicro() != instant.UnixMicro() || receipt.CreatedAt.Nanosecond() != instant.Nanosecond() {
					t.Fatal("projection changed immutable receipt identity or precision", receipt)
				}
				raw, err := json.Marshal(projected)
				if err != nil {
					t.Fatal(err)
				}
				var decoded RepositoryPriceResult
				if err := json.Unmarshal(raw, &decoded); err != nil {
					t.Fatal(err)
				}
				if receipt.CreatedAt.Location() != time.UTC || !reflect.DeepEqual(receipt, decoded.Receipt) || !reflect.DeepEqual(projected, &decoded) {
					t.Fatalf("direct and HTTP receipt differ despite same instant: directLocation=%q decodedLocation=%q", receipt.CreatedAt.Location(), decoded.Receipt.CreatedAt.Location())
				}
				if !strings.Contains(string(raw), `"created_at":"2026-10-04T07:26:23.156539Z"`) {
					t.Fatal("receipt did not serialize the exact canonical UTC instant")
				}
			})
		}
	}
}
