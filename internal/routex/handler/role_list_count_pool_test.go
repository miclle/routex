package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errRoleListCountProbe = errors.New("controlled statement probe")

type roleListCountProbePool struct {
	gorm.ConnPool
	options   sql.TxOptions
	queries   int
	commits   int
	rollbacks int
}

func (p *roleListCountProbePool) BeginTx(_ context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	p.options = *options
	return p, nil
}
func (p *roleListCountProbePool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	p.queries++
	return nil, errRoleListCountProbe
}
func (p *roleListCountProbePool) Commit() error   { p.commits++; return nil }
func (p *roleListCountProbePool) Rollback() error { p.rollbacks++; return nil }

func TestRoleListCountPoolSurvivesDiscardedLoggerAndReadSnapshot(t *testing.T) {
	for _, statements := range []int{6, 7} {
		for _, rollback := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/rollback=%t", statements, rollback), func(t *testing.T) {
				ctx := context.Background()
				probe := &roleListCountProbePool{}
				db, err := gorm.Open(postgres.New(postgres.Config{Conn: probe}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
				if err != nil {
					t.Fatal(err)
				}
				counter := &atomic.Int64{}
				readDB := db.Session(&gorm.Session{NewDB: true, Context: ctx})
				pool := &roleListCountPool{ConnPool: readDB.Statement.ConnPool, statements: counter}
				readDB.ConnPool, readDB.Statement.ConnPool = pool, pool
				svc, err := service.New(ctx, readDB)
				if err != nil {
					t.Fatal(err)
				}
				if db.ConnPool != probe || db.Statement.ConnPool != probe {
					t.Fatal("instrumentation changed the original database handle")
				}
				// Match authDB's privacy boundary; transaction/NewDB clones must
				// retain the counter even though Trace instrumentation is discarded.
				err = svc.DB().WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
					for range statements {
						_, queryErr := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT 1").Rows()
						if !errors.Is(queryErr, errRoleListCountProbe) {
							t.Fatal("probe did not reach the underlying transaction", queryErr)
						}
					}
					if rollback {
						return errRoleListCountProbe
					}
					return nil
				}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
				if rollback && !errors.Is(err, errRoleListCountProbe) || !rollback && err != nil {
					t.Fatal("transaction result changed", err)
				}
				if counter.Load() != int64(statements) || probe.queries != statements || probe.options.Isolation != sql.LevelRepeatableRead || !probe.options.ReadOnly {
					t.Fatal("count or read snapshot lost", counter.Load(), probe)
				}
				if rollback && (probe.commits != 0 || probe.rollbacks == 0) || !rollback && (probe.commits != 1 || probe.rollbacks != 0) {
					t.Fatal("transaction completion not forwarded", probe)
				}
			})
		}
	}
}
