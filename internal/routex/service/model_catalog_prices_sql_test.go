package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type catalogPricesSQLFixture struct {
	base         *effectiveSQLFixture
	grants       []memberCatalogGrant
	transactions int
	onFinal      func()
}
type catalogPricesSQLConnector struct{ fixture *catalogPricesSQLFixture }

func (c catalogPricesSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return catalogPricesSQLConnection{effectiveSQLConnection{fixture: c.fixture.base}, c.fixture}, nil
}
func (c catalogPricesSQLConnector) Driver() driver.Driver { return catalogPricesSQLDriver(c) }

type catalogPricesSQLDriver catalogPricesSQLConnector

func (c catalogPricesSQLDriver) Open(string) (driver.Conn, error) {
	return catalogPricesSQLConnector(c).Connect(context.Background())
}

type catalogPricesSQLConnection struct {
	effectiveSQLConnection
	catalog *catalogPricesSQLFixture
}

func (c catalogPricesSQLConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.catalog.transactions++
	if c.catalog.transactions == 2 && c.catalog.onFinal != nil {
		c.catalog.onFinal()
	}
	return c.effectiveSQLConnection.BeginTx(ctx, opts)
}
func (c catalogPricesSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "JOIN user_model_grants g") || strings.Contains(q, "JOIN team_model_grants g") {
		c.catalog.base.queries = append(c.catalog.base.queries, q)
		if strings.Contains(q, "JOIN team_model_grants g") {
			return effectiveSQLRows([]memberCatalogGrant{})
		}
		return effectiveSQLRows(c.catalog.grants)
	}
	return c.effectiveSQLConnection.QueryContext(ctx, q, args)
}
func TestMemberCatalogPricesFreshSnapshotAndFixedBatchBudget(t *testing.T) {
	for _, name := range []string{"one", "twenty", "permission_revoked", "source_revoked", "actor_recreated", "metadata_failure"} {
		t.Run(name, func(t *testing.T) {
			svc, data, _ := memberModelsProjectionFixture(t)
			data.Prices = []entity.ModelPrice{{ID: "prc_catalog", ProviderModelID: "pmd_one"}}
			data.Rates = []entity.PriceRate{{ModelPriceID: "prc_catalog", Metric: "INPUT_TOKEN", Tier: "base", Unit: "1M_TOKEN", Currency: "USD", Amount: "0", Enabled: true}}
			actor := entity.User{ID: "usr_reader", Role: entity.RoleAdmin, CreatedAt: time.Now().UTC().Add(-time.Hour)}
			base := &effectiveSQLFixture{data: &memberEffectiveModelsData{Metadata: data}, actor: &actor, actorID: actor.ID, permissions: map[string]bool{"prices.read": true}}
			f := &catalogPricesSQLFixture{base: base}
			for _, m := range data.Models {
				row := catalogGrantFixture(m.ID)
				row.Name = data.Names[0].Name
				row.CreatedAt = m.CreatedAt
				row.UserID = actor.ID
				f.grants = append(f.grants, row)
			}
			if name == "twenty" {
				m, n, g := data.Models[0], data.Names[0], f.grants[0]
				data.Models = nil
				data.Names = nil
				f.grants = nil
				for i := range 20 {
					id := fmt.Sprintf("mdl_%03d", i)
					m.ID = id
					n.ModelID = id
					n.CurrentModelID = &id
					n.Name = id
					g.ModelID = id
					g.NameModelID = id
					g.NameOwnerID = id
					g.GrantModelID = id
					g.Name = id
					data.Models = append(data.Models, m)
					data.Names = append(data.Names, n)
					f.grants = append(f.grants, g)
				}
				data.Bindings[0].ModelID = data.Models[0].ID
			}
			switch name {
			case "permission_revoked":
				f.onFinal = func() { base.permissions["prices.read"] = false }
			case "source_revoked":
				f.onFinal = func() { f.grants = nil }
			case "actor_recreated":
				f.onFinal = func() { actor.CreatedAt = actor.CreatedAt.Add(time.Second) }
			case "metadata_failure":
				base.failTable = `FROM "price_rates"`
			}
			pool := sql.OpenDB(catalogPricesSQLConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			svc.db = db
			items, err := svc.ListMemberModelCatalog(context.Background(), actor.ID)
			if f.transactions != 2 || !base.options.ReadOnly || base.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || base.writes != 0 {
				t.Fatal("snapshot/write contract", f.transactions, base.options, base.writes)
			}
			switch name {
			case "source_revoked", "actor_recreated", "metadata_failure":
				if err == nil || items != nil {
					t.Fatal("stale or failed read leaked price", items, err)
				}
			case "permission_revoked":
				if err != nil || len(items) != 1 || items[0].InputPrice.State != "unauthorized" || items[0].InputPrice.Rate != nil {
					t.Fatal(items, err)
				}
				if len(base.queries) != 7 {
					t.Fatal("private metadata queried without current price authority", len(base.queries), base.queries)
				}
			default:
				if err != nil || len(items) != len(f.grants) {
					t.Fatal(items, err)
				}
				if len(base.queries) != 18 {
					t.Fatal("batch query budget changed", len(base.queries), base.queries)
				}
				providerQueries := 0
				for _, q := range base.queries {
					if strings.Contains(q, `FROM "providers"`) {
						providerQueries++
					}
					if strings.Contains(q, `FROM "providers"`) && (strings.Contains(q, `"name"`) || strings.Contains(q, `"etag"`) || !strings.Contains(q, `"e_tag"`) || !strings.Contains(q, `WHERE "id" =`) || !strings.Contains(q, "LIMIT")) {
						t.Fatal("eligibility query exposed names or escaped authorized topology", q)
					}
				}
				if providerQueries != 1 {
					t.Fatal("expected one bounded internal Provider eligibility query", providerQueries)
				}
			}
		})
	}
}
