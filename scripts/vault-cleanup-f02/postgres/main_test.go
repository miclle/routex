//go:build linux

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDummyRefusalRequiresTypedExactErrorAndCheckedRollback(t *testing.T) {
	good := &pgconn.PgError{Code: "23514", ConstraintName: "routex_fixture_f28_dummy_refusal", TableName: "routex_f28_dummy_postack"}
	if !dummyRefusal(good, nil) {
		t.Fatal("exact dummy refusal rejected")
	}
	wrongCode := *good
	wrongCode.Code = "23505"
	wrongTable := *good
	wrongTable.TableName = "credential_published_cleanups"
	wrongConstraint := *good
	wrongConstraint.ConstraintName = "routex_fixture_f28_postack_refusal"
	for _, err := range []error{nil, &wrongCode, &wrongTable, &wrongConstraint, errors.New("23514 synthetic secret"), context.Canceled} {
		if dummyRefusal(err, nil) {
			t.Fatal("unknown or foreign refusal accepted")
		}
	}
	if dummyRefusal(good, errors.New("rollback uncertainty")) {
		t.Fatal("uncertain rollback accepted")
	}
}

func TestDummyConfigurationRejectsDuplicateCaseUnknownAndOldSource(t *testing.T) {
	BuildRunID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	BuildSource = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	base := `{"run_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","database_container_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","database_password":"dddddddddddddddddddddddddddddddd"}`
	if _, err := configuration([]byte(base)); err != nil {
		t.Fatal("exact config refused")
	}
	for _, raw := range []string{
		`{"run_id":"x","run_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","database_container_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","database_password":"dddddddddddddddddddddddddddddddd"}`,
		`{"Run_ID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","database_container_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","database_password":"dddddddddddddddddddddddddddddddd"}`,
		base + ` {"extra":"synthetic secret"}`,
	} {
		if _, err := configuration([]byte(raw)); err == nil {
			t.Fatal("noncanonical config accepted")
		}
	}
	BuildSource = "777048e6a685d56c78565545d1fe94099486668f"
	if _, err := configuration([]byte(base)); err == nil {
		t.Fatal("old source accepted")
	}
}
