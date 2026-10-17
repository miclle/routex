package main

import (
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func validDatabaseDriver(driver string) bool {
	return driver == "" || driver == "postgres" || driver == "mysql"
}

// Consume the validated secret before opening the fixed disposable database.
// No host, port, user, database, DSN or connection option comes from input.
func prepareConnection(c *config) (string, bool) {
	if c == nil || !validConfig(*c) {
		return "", false
	}
	password := c.DatabasePassword
	c.DatabasePassword = ""
	if c.DatabaseDriver != "mysql" {
		return "host=postgres port=5432 user=routex password=" + password + " dbname=routex sslmode=disable connect_timeout=2", true
	}
	connection := mysqldriver.NewConfig()
	connection.User, connection.Passwd = "routex", password
	connection.Net, connection.Addr, connection.DBName = "tcp", "mysql:3306", "routex"
	connection.ParseTime, connection.Loc = true, time.UTC
	connection.Params = map[string]string{"charset": "utf8mb4"}
	connection.Timeout = 3 * time.Second
	connection.ReadTimeout = 3 * time.Second
	connection.WriteTimeout = 3 * time.Second
	// RejectReadOnly enables a driver retry on read-only refusal. Keep it off:
	// a deliberate refusal must remain the original typed database error.
	connection.RejectReadOnly = false
	dsn := connection.FormatDSN()
	connection.Passwd, password = "", ""
	return dsn, true
}

func openEvidenceDatabase(driver, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "", "postgres":
		dialector = postgres.Open(dsn)
	case "mysql":
		// The controlled MySQL 8.4 fixture needs no version discovery or schema
		// operations. Disabling discovery also keeps connection I/O contextual.
		dialector = mysql.New(mysql.Config{DSN: dsn, SkipInitializeWithVersion: true})
	default:
		return nil, errInvalid
	}
	return gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true, TranslateError: true})
}
