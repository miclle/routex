package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	mysqlconfig "github.com/go-sql-driver/mysql"

	cleanupevidence "github.com/miclle/routex/scripts/provider-credential-cleanup-evidence"
)

var sourcePattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var runPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var uidPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
var passwordPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

type bindings struct{ source, run, phase, state, driver string }

func compiledBindings(real, effective int) (bindings, error) {
	if (BuildDriver != "postgres" && BuildDriver != "mysql") || !sourcePattern.MatchString(BuildSource) || !runPattern.MatchString(BuildRunID) || !uidPattern.MatchString(BuildTrustedHostUID) {
		return bindings{}, errRejected
	}
	uid, err := strconv.ParseUint(BuildTrustedHostUID, 10, 32)
	if err != nil || uid == 0 || strconv.FormatUint(uid, 10) != BuildTrustedHostUID || real <= 0 || real != effective || uint64(real) != uid {
		return bindings{}, errRejected
	}
	b := bindings{source: BuildSource, run: BuildRunID, phase: BuildPhase, driver: BuildDriver}
	switch b.phase {
	case "normal":
		b.state = "acknowledged"
	case "f02":
		b.state = "pending"
	default:
		return bindings{}, errRejected
	}
	return b, nil
}

func (b bindings) database() string { return "routex_f28_" + b.phase + "_" + b.run }
func (b bindings) user() string     { return "routex_f28_" + b.phase }

type input struct {
	Source            string    `json:"source_commit"`
	Run               string    `json:"run_id"`
	Password          string    `json:"database_password"`
	CreationRequestID string    `json:"creation_request_id"`
	RequestID         string    `json:"request_id"`
	ActorID           string    `json:"actor_id"`
	ActorBirth        time.Time `json:"actor_birth"`
	IntegrationID     string    `json:"integration_id"`
	IntegrationBirth  time.Time `json:"integration_birth"`
	RevisionID        string    `json:"revision_id"`
	CredentialID      string    `json:"credential_id"`
	CredentialBirth   time.Time `json:"credential_birth"`
	CreationBirth     time.Time `json:"creation_birth"`
	CreatorProcessID  string    `json:"creator_process_id"`
	CreatorGeneration string    `json:"creator_generation"`
	CreatorBirth      time.Time `json:"creator_birth"`
	PhysicalObject    string    `json:"physical_object"`
	OperationProof    string    `json:"operation_proof"`
	ReviewedETag      string    `json:"reviewed_etag"`
	Reason            string    `json:"reason"`
	RunStarted        time.Time `json:"run_started"`
	AsOf              time.Time `json:"as_of"`
	RootEpoch         uint64    `json:"root_epoch"`
}

func (input) String() string   { return "cleanup evidence input (redacted)" }
func (input) GoString() string { return "cleanup evidence input (redacted)" }

var inputKeys = []string{"source_commit", "run_id", "database_password", "creation_request_id", "request_id", "actor_id", "actor_birth", "integration_id", "integration_birth", "revision_id", "credential_id", "credential_birth", "creation_birth", "creator_process_id", "creator_generation", "creator_birth", "physical_object", "operation_proof", "reviewed_etag", "reason", "run_started", "as_of", "root_epoch"}

func decodeInput(raw []byte, b bindings) (input, error) {
	var value input
	if len(raw) == 0 || len(raw) > 16384 || !utf8.Valid(raw) {
		return value, errRejected
	}
	allowed := map[string]bool{}
	for _, key := range inputKeys {
		allowed[key] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return value, errRejected
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || seen[key] {
			return value, errRejected
		}
		seen[key] = true
		var field json.RawMessage
		if d.Decode(&field) != nil {
			clear(field)
			return value, errRejected
		}
		null := bytes.Equal(bytes.TrimSpace(field), []byte("null"))
		clear(field)
		if null {
			return value, errRejected
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(seen) != len(inputKeys) {
		return value, errRejected
	}
	if _, err = d.Token(); err != io.EOF {
		return value, errRejected
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil || value.Source != b.source || value.Run != b.run || !passwordPattern.MatchString(value.Password) {
		return input{}, errRejected
	}
	return value, nil
}

func (i input) expectation(b bindings) cleanupevidence.Expectation {
	return cleanupevidence.Expectation{
		SourceCommit: b.source, RunID: b.run, Phase: b.state,
		CreationRequestID: i.CreationRequestID, RequestID: i.RequestID,
		ActorID: i.ActorID, ActorBirth: i.ActorBirth,
		IntegrationID: i.IntegrationID, IntegrationBirth: i.IntegrationBirth,
		RevisionID: i.RevisionID, CredentialID: i.CredentialID,
		CredentialBirth: i.CredentialBirth, CreationBirth: i.CreationBirth,
		CreatorProcessID: i.CreatorProcessID, CreatorGeneration: i.CreatorGeneration, CreatorBirth: i.CreatorBirth,
		PhysicalObject: i.PhysicalObject, OperationProof: i.OperationProof,
		ReviewedETag: i.ReviewedETag, Reason: i.Reason,
		RunStarted: i.RunStarted, AsOf: i.AsOf, RootEpoch: i.RootEpoch,
	}
}

func consumeConnection(i *input, b bindings) (string, error) {
	if i == nil || i.Source != b.source || i.Run != b.run || !passwordPattern.MatchString(i.Password) {
		return "", errRejected
	}
	password := i.Password
	i.Password = ""
	if b.driver == "mysql" {
		config := mysqlconfig.NewConfig()
		config.User, config.Passwd, config.Net, config.Addr, config.DBName = b.user(), password, "tcp", "mysql:3306", b.database()
		config.ParseTime, config.Loc = true, time.UTC
		config.Timeout, config.ReadTimeout, config.WriteTimeout = 2*time.Second, 2*time.Second, 2*time.Second
		config.Params = map[string]string{"charset": "utf8mb4"}
		return config.FormatDSN(), nil
	}
	if b.driver != "postgres" {
		return "", errRejected
	}
	dsn := fmt.Sprintf("host=postgres port=5432 user=%s password=%s dbname=%s sslmode=disable connect_timeout=2 application_name=routex-f28-evidence-%s-%s", b.user(), password, b.database(), b.phase, b.run[:16])
	password = ""
	return dsn, nil
}
