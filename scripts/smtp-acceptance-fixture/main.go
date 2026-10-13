// Command smtp-acceptance-fixture reads bounded facts from an owned disposable
// database. It never migrates, inserts or changes application records.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type snapshot struct {
	Audits       []entity.AuditEvent                 `json:"audits"`
	Occurrences  []entity.OperationalAlertOccurrence `json:"occurrences"`
	Notices      []entity.Notification               `json:"notices"`
	Deliveries   []entity.NotificationDeliveryIntent `json:"deliveries"`
	Tests        []entity.SMTPTest                   `json:"tests"`
	Settings     []entity.NotificationSetting        `json:"settings"`
	NativeCounts map[string]int64                    `json:"native_counts"`
}

func main() {
	driver := flag.String("driver", "", "postgres or mysql")
	receipt := flag.String("receipt", "", "new private receipt file")
	flag.Parse()
	if flag.NArg() != 0 || run(*driver, os.Getenv("ROUTEX_SMTP_DSN"), *receipt) != nil {
		fmt.Fprintln(os.Stderr, "Local SMTP fact capture failed")
		os.Exit(1)
	}
}

func validateDSN(driver, dsn string) error {
	var host, port, name, user string
	switch driver {
	case "postgres":
		u, err := url.Parse(dsn)
		if err != nil || u.Scheme != "postgres" || u.User == nil || u.RawQuery != "sslmode=disable" || u.Fragment != "" {
			return errors.New("invalid fixture connection")
		}
		host, port, name, user = u.Hostname(), u.Port(), strings.TrimPrefix(u.Path, "/"), u.User.Username()
	case "mysql":
		config, err := mysqldriver.ParseDSN(dsn)
		if err != nil || config.Net != "tcp" || config.TLSConfig != "" || config.AllowAllFiles || config.MultiStatements {
			return errors.New("invalid fixture connection")
		}
		host, port, err = net.SplitHostPort(config.Addr)
		if err != nil {
			return errors.New("invalid fixture address")
		}
		name, user = config.DBName, config.User
	default:
		return errors.New("invalid fixture driver")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || host != "127.0.0.1" || name != "routex_smtp_acceptance" || user != "routex_smtp_acceptance" {
		return errors.New("fixture identity is not owned loopback")
	}
	return nil
}

func privateReceipt(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid receipt path")
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	physical, physicalErr := filepath.EvalSymlinks(parent)
	if err != nil || physicalErr != nil || !info.IsDir() || info.Mode().Perm() != 0700 || physical != parent {
		return errors.New("receipt parent is not private")
	}
	return nil
}

func run(driver, dsn, output string) error {
	if err := validateDSN(driver, dsn); err != nil {
		return err
	}
	if err := privateReceipt(output); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := database.Open(ctx, driver, dsn)
	if err != nil {
		return err
	}
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()
	value := snapshot{NativeCounts: map[string]int64{}}
	// The controller stops the application before capture. Bounded full-row
	// captures prove exact retained delivery state without editing timestamps.
	queries := []struct {
		table, projection string
		target            any
	}{
		{"audit_events", "id, actor_id, action, resource_type, resource_id, created_at", &value.Audits},
		{"operational_alert_occurrences", "*", &value.Occurrences},
		{"notifications", "*", &value.Notices},
		{"notification_delivery_intents", "*", &value.Deliveries},
		{"smtp_tests", "id, request_id, actor_id, config_e_tag, status, started_at, completed_at", &value.Tests},
		{"notification_settings", "*", &value.Settings},
	}
	for _, query := range queries {
		read := db.WithContext(ctx).Table(query.table).Select(query.projection)
		if query.table == "audit_events" {
			read = read.Where("action = ?", "credential.verify.failed")
		}
		order := "id"
		if query.table == "notification_settings" {
			order = "user_id"
		}
		if err = read.Order(order).Limit(5).Find(query.target).Error; err != nil {
			return errors.New("bounded fact read failed")
		}
	}
	for kind, table := range map[string]string{"keys": "api_keys", "project_keys": "project_api_keys", "calls": "call_records", "attempts": "call_attempts"} {
		var count int64
		if err = db.WithContext(ctx).Table(table).Count(&count).Error; err != nil {
			return errors.New("native fact read failed")
		}
		value.NativeCounts[kind] = count
	}
	value.NativeCounts["keys"] += value.NativeCounts["project_keys"]
	delete(value.NativeCounts, "project_keys")
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err = json.NewEncoder(file).Encode(value); err != nil {
		return err
	}
	return file.Sync()
}
