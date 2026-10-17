package main

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestConnectionDefaultsToFixedPostgresAndConsumesSecret(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		for _, driver := range []string{"", "postgres"} {
			t.Run(owner+"/"+driver, func(t *testing.T) {
				c, rows := fixture()
				if owner == "project" {
					c, rows = projectFixture()
				}
				c.DatabaseDriver = driver
				password := c.DatabasePassword
				dsn, ok := prepareConnection(&c)
				want := "host=postgres port=5432 user=routex password=" + password + " dbname=routex sslmode=disable connect_timeout=2"
				if !ok || dsn != want || c.DatabasePassword != "" || !validExpectations(c) || !validProjection(c, rows) {
					t.Fatal("fixed PostgreSQL connection or post-consumption proof changed")
				}
				if _, ok := prepareConnection(&c); ok {
					t.Fatal("consumed secret reused")
				}
			})
		}
	}
}

func TestMySQLConnectionIsFixedBoundedUTCWithoutReadOnlyRetry(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		t.Run(owner, func(t *testing.T) {
			c, rows := fixture()
			if owner == "project" {
				c, rows = projectFixture()
			}
			c.DatabaseDriver = "mysql"
			password := c.DatabasePassword
			dsn, ok := prepareConnection(&c)
			if !ok || c.DatabasePassword != "" || !validExpectations(c) || !validProjection(c, rows) {
				t.Fatal("secret consumption changed exact proof")
			}
			_, query, found := strings.Cut(dsn, "?")
			values, queryErr := url.ParseQuery(query)
			if !found || queryErr != nil || len(values["charset"]) != 1 || values["charset"][0] != "utf8mb4" {
				t.Fatal("fixed charset missing")
			}
			connection, err := mysqldriver.ParseDSN(dsn)
			dsn = ""
			if err != nil || connection.Passwd != password || connection.User != "routex" || connection.Net != "tcp" || connection.Addr != "mysql:3306" || connection.DBName != "routex" || !connection.ParseTime || connection.Loc != time.UTC || connection.Timeout != 3*time.Second || connection.ReadTimeout != 3*time.Second || connection.WriteTimeout != 3*time.Second || connection.RejectReadOnly || connection.MultiStatements || connection.InterpolateParams || connection.AllowAllFiles || connection.AllowCleartextPasswords {
				t.Fatal("fixed safe MySQL connection changed")
			}
			_, roundQuery, found := strings.Cut(connection.FormatDSN(), "?")
			roundValues, err := url.ParseQuery(roundQuery)
			connection.Passwd = ""
			if !found || err != nil || !reflect.DeepEqual(values, roundValues) {
				t.Fatal("public DSN round trip changed options")
			}
			if _, ok := prepareConnection(&c); ok {
				t.Fatal("consumed MySQL secret reused")
			}
		})
	}
}

func TestDatabaseDriverJSONRequiresCanonicalExplicitValue(t *testing.T) {
	for _, owner := range []string{"personal", "project"} {
		c, _ := fixture()
		if owner == "project" {
			c, _ = projectFixture()
		}
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal("encode fixture")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			t.Fatal("decode fixture")
		}
		for _, driver := range []string{"postgres", "mysql"} {
			fields["database_driver"] = json.RawMessage(`"` + driver + `"`)
			input, _ := json.Marshal(fields)
			var decoded config
			if !decodeConfig(input, &decoded) || decoded.DatabaseDriver != driver {
				t.Fatal("canonical driver rejected")
			}
		}
		for _, value := range []string{`null`, `""`, `"MYSQL"`, `"Postgres"`, `"postgresql"`, `" mysql"`, `"sqlite"`, `true`, `123`, `[]`, `{}`} {
			fields["database_driver"] = json.RawMessage(value)
			input, _ := json.Marshal(fields)
			var decoded config
			if decodeConfig(input, &decoded) {
				t.Fatal("ambiguous driver accepted")
			}
		}
		delete(fields, "database_driver")
		for _, alias := range []string{"Database_Driver", "DATABASE_DRIVER", "Database_driver"} {
			for _, mixed := range []bool{false, true} {
				fields[alias] = json.RawMessage(`null`)
				if mixed {
					fields["database_driver"] = json.RawMessage(`"mysql"`)
				}
				input, _ := json.Marshal(fields)
				var decoded config
				if decodeConfig(input, &decoded) {
					t.Fatal("case alias bypassed driver boundary")
				}
				delete(fields, alias)
				delete(fields, "database_driver")
			}
		}
		for _, field := range []string{"dsn", "host", "port", "database", "user", "database_host", "database_dsn"} {
			fields[field] = json.RawMessage(`"foreign"`)
			input, _ := json.Marshal(fields)
			var decoded config
			if decodeConfig(input, &decoded) {
				t.Fatal("network override accepted")
			}
			delete(fields, field)
		}
		fields["database_driver"] = json.RawMessage(`"mysql"`)
		input, _ := json.Marshal(fields)
		input = append([]byte(`{"database_driver":"postgres",`), input[1:]...)
		var decoded config
		if decodeConfig(input, &decoded) {
			t.Fatal("duplicate driver accepted")
		}
	}
}

func TestInvalidConnectionRefusesBeforeOpeningOrConsuming(t *testing.T) {
	if _, ok := prepareConnection(nil); ok {
		t.Fatal("nil input accepted")
	}
	for name, mutate := range map[string]func(*config){
		"unknown driver":    func(c *config) { c.DatabaseDriver = "MYSQL" },
		"injected password": func(c *config) { c.DatabasePassword = "password host=foreign" },
		"foreign owner":     func(c *config) { c.UserID = "usr_wrong" },
		"duplicate Key":     func(c *config) { c.PredecessorID = c.KeyID },
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := fixture()
			mutate(&c)
			original := c.DatabasePassword
			if dsn, ok := prepareConnection(&c); ok || dsn != "" || c.DatabasePassword != original {
				t.Fatal("invalid connection consumed or exposed input")
			}
		})
	}
	if db, err := openEvidenceDatabase("MYSQL", "do-not-print"); db != nil || err != errInvalid {
		t.Fatal("unknown factory driver accepted")
	}
}
