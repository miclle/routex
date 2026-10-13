package main

import (
	"strings"
	"testing"
)

func TestFixtureConnectionRequiresExactDisposableIdentity(t *testing.T) {
	valid := map[string]string{
		"postgres": "postgres://routex_smtp_acceptance:disposable@127.0.0.1:6543/routex_smtp_acceptance?sslmode=disable",
		"mysql":    "routex_smtp_acceptance:disposable@tcp(127.0.0.1:6544)/routex_smtp_acceptance?charset=utf8mb4&parseTime=True&loc=UTC",
	}
	for driver, dsn := range valid {
		if err := validateDSN(driver, dsn); err != nil {
			t.Fatal("exact loopback fixture refused", driver)
		}
		for _, altered := range []string{
			strings.ReplaceAll(dsn, "127.0.0.1", "localhost"),
			strings.ReplaceAll(dsn, "127.0.0.1", "192.0.2.1"),
			strings.ReplaceAll(dsn, "routex_smtp_acceptance", "routex"),
			strings.ReplaceAll(dsn, "6543", "70000"),
			strings.ReplaceAll(dsn, "6544", "70000"),
		} {
			if altered != dsn && validateDSN(driver, altered) == nil {
				t.Fatal("unowned connection accepted", driver)
			}
		}
	}
	for _, dsn := range []string{
		valid["mysql"] + "&multiStatements=true",
		valid["mysql"] + "&allowAllFiles=true",
		valid["mysql"] + "&tls=skip-verify",
	} {
		if validateDSN("mysql", dsn) == nil {
			t.Fatal("unbounded fixture capability accepted")
		}
	}
}
