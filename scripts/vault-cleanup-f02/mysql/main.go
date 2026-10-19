//go:build linux

// Separate disposable qualification DB only. No production migration/seed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"golang.org/x/sys/unix"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var BuildSource string
var BuildRunID string
var BuildTrustedHostUID string

type refusal string

func (r refusal) Error() string { return "F28 MySQL qualification refused" }

type input struct {
	RunID     string `json:"run_id"`
	Source    string `json:"source_commit"`
	Container string `json:"database_container_id"`
	Password  string `json:"database_password"`
}
type dummy struct {
	ID     int    `gorm:"primaryKey"`
	State  string `gorm:"check:routex_fixture_f28_dummy_refusal,state <> 'acknowledged'"`
	Marker string `gorm:"size:128;not null"`
}

func (dummy) TableName() string { return "routex_f28_dummy_postack" }

var settings = map[string]string{"general_log": "0", "slow_query_log": "0", "log_output": "NONE", "log_error_verbosity": "2", "log_timestamps": "UTC", "skip_name_resolve": "1", "local_infile": "0", "lc_messages": "en_US"}

func trustedUID() (uint32, error) {
	if !regexp.MustCompile(`^[1-9][0-9]{0,9}$`).MatchString(BuildTrustedHostUID) {
		return 0, refusal("argument")
	}
	uid, err := strconv.ParseUint(BuildTrustedHostUID, 10, 32)
	if err != nil || uid == 0 || strconv.FormatUint(uid, 10) != BuildTrustedHostUID || os.Geteuid() <= 0 || uint64(os.Geteuid()) != uid {
		return 0, refusal("argument")
	}
	return uint32(uid), nil
}
func stdinBounded() ([]byte, error) {
	deadline := time.Now().Add(5 * time.Second)
	raw := make([]byte, 0, 16384)
	flags, err := unix.FcntlInt(uintptr(0), unix.F_GETFL, 0)
	if err != nil || unix.SetNonblock(0, true) != nil {
		return nil, refusal("input")
	}
	defer unix.FcntlInt(uintptr(0), unix.F_SETFL, flags)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			clear(raw)
			return nil, refusal("input")
		}
		fds := []unix.PollFd{{Fd: 0, Events: unix.POLLIN | unix.POLLHUP}}
		ready, e := unix.Poll(fds, int((remaining+time.Millisecond-1)/time.Millisecond))
		if e == unix.EINTR {
			continue
		}
		if e != nil || ready == 0 || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			clear(raw)
			return nil, refusal("input")
		}
		buffer := make([]byte, 4096)
		n, e := unix.Read(0, buffer)
		if e == unix.EAGAIN || e == unix.EINTR {
			continue
		}
		if e != nil {
			clear(raw)
			return nil, refusal("input")
		}
		if n == 0 {
			if len(raw) == 0 {
				return nil, refusal("input")
			}
			return raw, nil
		}
		raw = append(raw, buffer[:n]...)
		clear(buffer)
		if len(raw) > 16384 {
			clear(raw)
			return nil, refusal("input")
		}
	}
}
func configuration(raw []byte) (input, error) {
	var c input
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 4 {
		return c, refusal("input")
	}
	for _, key := range []string{"run_id", "source_commit", "database_container_id", "database_password"} {
		if len(fields[key]) == 0 || string(fields[key]) == "null" {
			return c, refusal("input")
		}
	}
	// Reject duplicate keys rather than relying on map decoding's last value.
	d := json.NewDecoder(bytes.NewReader(raw))
	seen := map[string]bool{}
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return c, refusal("input")
	}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || seen[k] || fields[k] == nil {
			return c, refusal("input")
		}
		seen[k] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return c, refusal("input")
		}
	}
	if json.Unmarshal(raw, &c) != nil || c.RunID != BuildRunID || c.Source != BuildSource || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(c.RunID) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(c.Source) || c.Source == "777048e6a685d56c78565545d1fe94099486668f" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(c.Container) || !regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`).MatchString(c.Password) {
		return c, refusal("input")
	}
	return c, nil
}
func verifySettings(db *gorm.DB) error {
	for key, want := range settings {
		var row struct{ Value string }
		// MySQL system variable identifiers cannot be bound by GORM. Keys are
		// this fixed private allowlist, never input, SQL or endpoint overrides.
		if db.Raw("SELECT @@GLOBAL."+key+" AS value").Scan(&row).Error != nil || row.Value != want {
			return refusal("settings")
		}
	}
	return nil
}
func dummyRefusal(err, rolled error) bool {
	var server *mysqlconfig.MySQLError
	return rolled == nil && errors.As(err, &server) && server.Number == 3819 && server.SQLState == [5]byte{'H', 'Y', '0', '0', '0'} && server.Message == "Check constraint 'routex_fixture_f28_dummy_refusal' is violated."
}
func qualification(db *gorm.DB, mode string, c input) (map[string]any, error) {
	var who struct {
		DatabaseName string
		DatabaseUser string
	}
	prefix := "dummy"
	if mode == "settings-f02" {
		prefix = "f02"
	}
	// Fixed connection identity is checked before any synthetic DDL.
	if db.Raw("SELECT DATABASE() AS database_name,CURRENT_USER() AS database_user").Scan(&who).Error != nil || who.DatabaseName != "routex_f28_"+prefix+"_"+c.RunID || who.DatabaseUser != "routex_f28_"+prefix+"@%" {
		return nil, refusal("identity")
	}
	if e := verifySettings(db); e != nil {
		return nil, e
	}
	out := map[string]any{"result": "f28_mysql_settings_observed", "scope": prefix, "safe_settings": settings, "business_acceptance": false}
	if mode == "dummy" {
		for _, table := range []string{"schema_migrations", "provider_credential_cleanups", "credential_published_cleanups", "routex_f28_dummy_postack"} {
			if db.Migrator().HasTable(table) {
				return nil, refusal("dummy_not_empty")
			}
		}
		// This creates ONLY a synthetic fixed dummy table in the independently
		// owned disposable dummy DB. It does not touch any business/normal DB.
		if db.Migrator().CreateTable(&dummy{}) != nil || db.Create(&dummy{ID: 1, State: "pending", Marker: "routex-f28-private-" + c.RunID}).Error != nil {
			return nil, refusal("dummy_create")
		}
		tx := db.Begin()
		if tx.Error != nil {
			return nil, refusal("begin")
		}
		err := tx.Model(&dummy{}).Where("id = ?", 1).UpdateColumn("state", "acknowledged").Error
		rolled := tx.Rollback().Error
		if !dummyRefusal(err, rolled) {
			return nil, refusal("dummy_refusal")
		}
		var row dummy
		if db.Where("id = ?", 1).Take(&row).Error != nil || row.State != "pending" {
			return nil, refusal("dummy_rollback")
		}
		out["result"] = "dummy_typed_refusal_and_checked_rollback"
		out["sqlstate"] = "HY000"
		out["server_error_number"] = 3819
		out["safe_server_log_qualified"] = false
	}
	return out, nil
}
func run() error {
	if len(os.Args) != 2 || (os.Args[1] != "settings-dummy" && os.Args[1] != "dummy" && os.Args[1] != "settings-f02") {
		return refusal("argument")
	}
	if _, e := trustedUID(); e != nil {
		return e
	}
	raw, e := stdinBounded()
	if e != nil {
		return e
	}
	defer clear(raw)
	c, e := configuration(raw)
	if e != nil {
		return e
	}
	prefix := "dummy"
	if os.Args[1] == "settings-f02" {
		prefix = "f02"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection := mysqlconfig.NewConfig()
	connection.User, connection.Passwd, connection.Net, connection.Addr, connection.DBName = "routex_f28_"+prefix, c.Password, "tcp", "mysql:3306", "routex_f28_"+prefix+"_"+c.RunID
	connection.ParseTime, connection.Loc = true, time.UTC
	connection.Timeout, connection.ReadTimeout, connection.WriteTimeout = 2*time.Second, 2*time.Second, 2*time.Second
	connection.Params = map[string]string{"charset": "utf8mb4"}
	// Diagnostic-only translation bypass retains the actual typed 3819/HY000
	// refusal. Production services keep portable translation enabled.
	db, e := gorm.Open(gormmysql.New(gormmysql.Config{DSN: connection.FormatDSN(), SkipInitializeWithVersion: true}), &gorm.Config{TranslateError: false, Logger: logger.Discard, DisableAutomaticPing: true})
	if e != nil {
		return refusal("connect")
	}
	pool, e := db.DB()
	if e != nil {
		return refusal("connect")
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	out, queryErr := qualification(db.WithContext(ctx), os.Args[1], c)
	closeErr := pool.Close()
	if queryErr != nil || closeErr != nil || ctx.Err() != nil {
		return refusal("qualification")
	}
	if json.NewEncoder(os.Stdout).Encode(out) != nil {
		return refusal("encode")
	}
	return nil
}
func main() {
	if run() != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"result": "f28_mysql_qualification_refused", "business_acceptance": false})
		os.Exit(1)
	}
}
