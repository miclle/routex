//go:build linux

package main

import (
	"context"
	"errors"
	"testing"

	mysqlconfig "github.com/go-sql-driver/mysql"
)

func TestDummyRefusalRequiresTypedExactErrorAndCheckedRollback(t *testing.T) {
	good := &mysqlconfig.MySQLError{Number: 3819, SQLState: [5]byte{'H', 'Y', '0', '0', '0'}, Message: "Check constraint 'routex_fixture_f28_dummy_refusal' is violated."}
	if !dummyRefusal(good, nil) {
		t.Fatal("exact dummy refusal rejected")
	}
	wrongCode := *good
	wrongCode.Number = 1062
	wrongState := *good
	wrongState.SQLState = [5]byte{'2', '3', '0', '0', '0'}
	wrongConstraint := *good
	wrongConstraint.Message = "Check constraint 'routex_fixture_f28_postack_refusal' is violated."
	for _, err := range []error{nil, &wrongCode, &wrongState, &wrongConstraint, errors.New("3819 HY000 synthetic secret"), context.Canceled} {
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
