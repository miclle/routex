// Command f28-f02-client records one expected uncertain published cleanup and replay.
// It accepts no endpoint, identity, SQL, token or output override.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	cleanupevidence "github.com/miclle/routex/scripts/provider-credential-cleanup-evidence"
	vaultwire "github.com/miclle/routex/scripts/vault-cleanup-wire"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var BuildRunID, BuildSource, BuildManifest, BuildHostUID, BuildStarted, BuildNormalReceipt string
var errRejected = errors.New("normal cleanup rejected")
var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hex40 = regexp.MustCompile(`^[a-f0-9]{40}$`)
var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)

const apiBase = "http://127.0.0.1:19001"
const vaultBase = "http://127.0.0.1:8200"

type httpFact struct {
	Name, Method, Path  string
	Status, Bytes       int
	SHA256              string
	CookieHeaderPresent bool
}
type owner struct {
	ctx              context.Context
	client           *http.Client
	dir, stage, csrf string
	http             []httpFact
}

func hash(raw []byte) string { v := sha256.Sum256(raw); return hex.EncodeToString(v[:]) }
func encode(v any) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}
	return json.Marshal(v)
}
func (o *owner) save(name string, v any) error {
	b, e := encode(v)
	if e != nil {
		return errRejected
	}
	f, e := os.OpenFile(filepath.Join(o.dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errRejected
	}
	_, e = f.Write(b)
	c := f.Close()
	if e != nil || c != nil {
		return errRejected
	}
	return nil
}
func fresh() string {
	var v [16]byte
	if _, e := rand.Read(v[:]); e != nil {
		return ""
	}
	v[6] = (v[6] & 15) | 64
	v[8] = (v[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", v[:4], v[4:6], v[6:8], v[8:10], v[10:])
}
func (o *owner) request(name, base, method, path, token, etag string, body any, status int, out any) error {
	o.stage = name
	b, e := encode(body)
	if body == nil {
		b = nil
	}
	if e != nil || len(b) > 32768 {
		return errRejected
	}
	req, e := http.NewRequestWithContext(o.ctx, method, base+path, bytes.NewReader(b))
	if e != nil {
		return errRejected
	}
	req.GetBody = nil
	req.Close = true
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if base == apiBase {
		req.Header.Set("Origin", apiBase)
		if o.csrf != "" {
			req.Header.Set("X-CSRF-Token", o.csrf)
		}
	} else {
		req.Header.Set("X-Vault-Token", token)
	}
	if etag != "" {
		req.Header.Set("If-Match", strconv.Quote(etag))
	}
	client := o.client
	if base != apiBase {
		isolated := *client
		isolated.Jar = nil
		client = &isolated
	}
	res, e := client.Do(req)
	if e != nil {
		return errRejected
	}
	raw, re := io.ReadAll(io.LimitReader(res.Body, 4<<20+1))
	ce := res.Body.Close()
	if re != nil || ce != nil || len(raw) > 4<<20 {
		return errRejected
	}
	if res.Request == nil {
		return errRejected
	}
	cookiePresent := len(res.Request.Cookies()) != 0
	o.http = append(o.http, httpFact{name, method, path, res.StatusCode, len(raw), hash(raw), cookiePresent})
	if o.save("private-response-"+name+".json", raw) != nil {
		return errRejected
	}
	if base != apiBase && cookiePresent {
		return errRejected
	}
	if res.StatusCode != status {
		return errRejected
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return errRejected
	}
	return nil
}
func (o *owner) api(name, method, path, etag string, body any, status int, out any) error {
	return o.request(name, apiBase, method, path, "", etag, body, status, out)
}
func private(path string, max int64) ([]byte, error) {
	s, e := os.Lstat(path)
	if e != nil || !s.Mode().IsRegular() || s.Mode().Perm() != 0600 || s.Size() < 1 || s.Size() > max {
		return nil, errRejected
	}
	b, e := os.ReadFile(path)
	if e != nil || int64(len(b)) != s.Size() {
		return nil, errRejected
	}
	return b, nil
}
func (o *owner) audit(name string) ([]byte, error) {
	b, e := private("/vault/logs/audit.jsonl", 1<<20)
	if e != nil || b[len(b)-1] != '\n' || o.save(name, b) != nil {
		return nil, errRejected
	}
	return b, nil
}
func openDB(password string) (*gorm.DB, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`).MatchString(password) {
		return nil, errRejected
	}
	dsn := fmt.Sprintf("host=postgres port=5432 user=routex_f28_f02 password=%s dbname=routex_f28_f02_%s sslmode=disable connect_timeout=2 application_name=routex-f28-normal", password, BuildRunID)
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true, Logger: logger.Discard, DisableAutomaticPing: true})
	if e != nil {
		return nil, errRejected
	}
	p, e := db.DB()
	if e != nil {
		return nil, errRejected
	}
	p.SetMaxOpenConns(1)
	p.SetMaxIdleConns(0)
	return db, nil
}

// Frozen explicit projection excludes intent/auth/ciphertext, lease and secret
// columns. The original creation operation is read before any cleanup command.
var sqlReadOnly = sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}

const operationColumns = "request_id,actor_id,actor_birth,storage_source,kind,target_id,target_birth,credential_id,credential_birth,provider_id,provider_birth,connection_id,connection_birth,reference_id,expected_marker_sha256,descriptor_sha256,integration_id,integration_birth,revision_id,writer_generation,reader_generation,root_epoch,state,created_at"

func original(ctx context.Context, db *gorm.DB, creation, actor, integration, revision string) (op entity.CredentialStorageOperation, process entity.CredentialSourceProcess, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx := db.WithContext(ctx).Begin(&sqlReadOnly)
	if tx.Error != nil {
		return op, process, errRejected
	}
	defer func() {
		if tx.Rollback().Error != nil || ctx.Err() != nil {
			err = errRejected
		}
	}()
	if tx.Table("credential_storage_operations").Select(operationColumns).Where("request_id = ?", creation).Take(&op).Error != nil || op.RequestID != creation || op.ActorID != actor || op.IntegrationID != integration || op.RevisionID != revision || op.State != "committed" || op.StorageSource != "vault" || !hex32.MatchString(op.ReferenceID) || op.IntegrationBirth == nil {
		return op, process, errRejected
	}
	var use struct {
		CreationRequestID, ProcessID, ProcessGeneration string
		Exposed                                         bool
	}
	if tx.Table("provider_credential_creation_uses").Select("creation_request_id,process_id,process_generation,exposed").Where("creation_request_id = ?", creation).Take(&use).Error != nil || use.CreationRequestID != creation || use.Exposed {
		return op, process, errRejected
	}
	if tx.Table("credential_source_processes").Select("process_id,generation,birth,registered_at,closed_at").Where("process_id = ?", use.ProcessID).Take(&process).Error != nil || process.ProcessID != use.ProcessID || process.Generation != use.ProcessGeneration || process.ClosedAt != nil {
		return op, process, errRejected
	}
	return op, process, nil
}
func proof(op entity.CredentialStorageOperation) string {
	b, _ := json.Marshal(struct {
		Request, Actor, Source, Kind, Target, Credential, Provider, Connection, Reference, Marker, Descriptor, Integration, Revision, Writer, Reader string
		ActorBirth, TargetBirth, CredentialBirth, ProviderBirth, ConnectionBirth                                                                     time.Time
		IntegrationBirth                                                                                                                             *time.Time
	}{op.RequestID, op.ActorID, op.StorageSource, op.Kind, op.TargetID, op.CredentialID, op.ProviderID, op.ConnectionID, op.ReferenceID, op.ExpectedMarkerSHA256, op.DescriptorSHA256, op.IntegrationID, op.RevisionID, op.WriterGeneration, op.ReaderGeneration, op.ActorBirth, op.TargetBirth, op.CredentialBirth, op.ProviderBirth, op.ConnectionBirth, op.IntegrationBirth})
	return hash(b)
}
func physical(reference string) string {
	b, _ := json.Marshal(struct {
		URL, Namespace string
		Version        int
	}{vaultBase + "/v1/kv/data/discovery/routex-credential-" + reference, "", 1})
	return hash(b)
}
func (o *owner) observe(password string) error {
	pre := filepath.Join("/vault/logs", "discovery-"+BuildRunID)
	var initialization struct {
		Root string `json:"root_token"`
	}
	b, e := private(filepath.Join(pre, "private-initialization.json"), 32768)
	if e != nil || json.Unmarshal(b, &initialization) != nil || len(initialization.Root) < 16 {
		return errRejected
	}
	tokens := map[string]string{}
	for _, role := range []string{"writer", "cleanup"} {
		var v struct {
			Auth struct {
				Token string `json:"client_token"`
			} `json:"auth"`
		}
		b, e = private(filepath.Join(pre, "private-token-"+role+".json"), 32768)
		if e != nil || json.Unmarshal(b, &v) != nil || len(v.Auth.Token) < 16 {
			return errRejected
		}
		tokens[role] = v.Auth.Token
	}
	var role struct {
		Data struct {
			Value string `json:"role_id"`
		} `json:"data"`
	}
	b, e = private(filepath.Join(pre, "private-response-reader-role-id.json"), 32768)
	if e != nil || json.Unmarshal(b, &role) != nil || len(role.Data.Value) < 16 {
		return errRejected
	}
	var secret struct {
		Data struct {
			Value string `json:"secret_id"`
		} `json:"data"`
	}
	b, e = private(filepath.Join(pre, "private-response-reader-secret-id.json"), 32768)
	if e != nil || json.Unmarshal(b, &secret) != nil || len(secret.Data.Value) < 16 {
		return errRejected
	}
	// Read-only startup probes precede the one original setup and all mutations.
	ready := false
	for until := time.Now().Add(30 * time.Second); time.Now().Before(until); {
		req, _ := http.NewRequestWithContext(o.ctx, "GET", apiBase+"/api/v1/setup", nil)
		res, err := o.client.Do(req)
		if err == nil {
			raw, re := io.ReadAll(io.LimitReader(res.Body, 1025))
			ce := res.Body.Close()
			if re == nil && ce == nil && len(raw) <= 1024 && res.StatusCode == 200 {
				var state struct {
					Initialized bool `json:"initialized"`
				}
				if json.Unmarshal(raw, &state) == nil && !state.Initialized {
					ready = true
					break
				}
			}
		}
		select {
		case <-o.ctx.Done():
			return errRejected
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		return errRejected
	}
	start, startErr := time.Parse(time.RFC3339Nano, BuildStarted)
	if startErr != nil || start.Nanosecond()%1000 != 0 || time.Since(start) < 0 || time.Since(start) > 10*time.Minute {
		return errRejected
	}
	creation, command, integrationIntent := fresh(), fresh(), fresh()
	if creation == "" || command == "" || integrationIntent == "" {
		return errRejected
	}
	var identity struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		CSRF string `json:"csrf_token"`
	}
	if o.api("setup", "POST", "/api/v1/setup", "", map[string]string{"email": "f28-" + BuildRunID + "@example.invalid", "password": fresh() + fresh(), "name": "Controlled normal cleanup"}, 201, &identity) != nil || identity.User.ID == "" || identity.CSRF == "" {
		return errRejected
	}
	o.csrf = identity.CSRF
	var catalog struct {
		Review string `json:"review_etag"`
	}
	if o.api("integration-catalog", "GET", "/api/v1/admin/secrets/integrations", "", nil, 200, &catalog) != nil {
		return errRejected
	}
	var config struct {
		ID        string `json:"integration_id"`
		Revision  string `json:"revision_id"`
		Committed bool   `json:"committed"`
	}
	body := map[string]any{"request_id": integrationIntent, "name": "Controlled cleanup " + BuildRunID, "descriptor": map[string]string{"endpoint": vaultBase, "namespace": "", "mount": "kv", "prefix": "discovery", "data_field": "value"}, "writer_auth": map[string]string{"action": "replace", "token": tokens["writer"]}, "reader_auth": map[string]string{"action": "replace", "method": "approle", "auth_mount": "routex-f28-" + BuildRunID[:16], "role_id": role.Data.Value, "secret_id": secret.Data.Value}, "reason": "Configure fresh controlled cleanup source"}
	if o.api("integration-create", "POST", "/api/v1/admin/secrets/integrations", catalog.Review, body, 200, &config) != nil || !config.Committed {
		return errRejected
	}
	var policy struct {
		ETag string `json:"etag"`
	}
	if o.api("policy-read", "GET", "/api/v1/admin/secrets/provider-storage", "", nil, 200, &policy) != nil || o.api("policy-write", "PUT", "/api/v1/admin/secrets/provider-storage", policy.ETag, map[string]string{"mode": "vault", "integration_id": config.ID, "revision_id": config.Revision, "reason": "Reviewed future Vault storage"}, 200, nil) != nil {
		return errRejected
	}
	if o.api("storage-context", "GET", "/api/v1/admin/provider-credential-storage-context", "", nil, 200, &policy) != nil {
		return errRejected
	}
	if o.api("provider-create", "POST", "/api/v1/admin/providers", "", map[string]string{"name": "Controlled Provider " + BuildRunID, "connection_name": "Controlled Connection", "base_url": "https://example.invalid/v1", "protocol": "openai_chat", "credential_name": "Controlled Credential", "secret": fresh() + fresh(), "request_id": creation, "storage_policy_etag": policy.ETag}, 201, nil) != nil {
		return errRejected
	}
	db, e := openDB(password)
	if e != nil {
		return errRejected
	}
	pool, e := db.DB()
	if e != nil {
		return errRejected
	}
	closed := false
	defer func() {
		if !closed {
			_ = pool.Close()
		}
	}()
	o.stage = "original-sql"
	op, process, e := original(o.ctx, db, creation, identity.User.ID, config.ID, config.Revision)
	if e != nil {
		return errRejected
	}
	target := "/api/v1/admin/secrets/integrations/" + config.ID + "/provider-orphans/" + creation
	var preview struct {
		Eligible, CanCleanup bool
		Review               string   `json:"review_etag"`
		Blockers             []string `json:"blocker_codes"`
	}
	if o.api("retained-preview", "GET", target, "", nil, 200, &preview) != nil || preview.Eligible {
		return errRejected
	}
	var metadata struct {
		ETag string `json:"etag"`
	}
	path := "/api/v1/admin/credentials/" + op.CredentialID
	if o.api("credential-metadata", "GET", path+"/metadata", "", nil, 200, &metadata) != nil || o.api("credential-delete", "DELETE", path, metadata.ETag, map[string]string{"reason": "Reviewed logical removal before exact cleanup"}, 200, nil) != nil {
		return errRejected
	}
	if o.api("cleanup-preview", "GET", target, "", nil, 200, &preview) != nil || !preview.Eligible || preview.Review == "" {
		return errRejected
	}
	reason := "One original controlled published cleanup"
	expectation := map[string]any{"source_commit": BuildSource, "run_id": BuildRunID, "database_password": password, "creation_request_id": creation, "request_id": command, "actor_id": identity.User.ID, "actor_birth": op.ActorBirth, "integration_id": config.ID, "integration_birth": *op.IntegrationBirth, "revision_id": config.Revision, "credential_id": op.CredentialID, "credential_birth": op.CredentialBirth, "creation_birth": op.CreatedAt, "creator_process_id": process.ProcessID, "creator_generation": process.Generation, "creator_birth": process.Birth, "physical_object": physical(op.ReferenceID), "operation_proof": proof(op), "reviewed_etag": preview.Review, "reason": reason, "run_started": start, "as_of": time.Now().UTC().Truncate(time.Microsecond), "root_epoch": op.RootEpoch}
	if o.save("private-original-expectation.json", expectation) != nil {
		return errRejected
	}
	// Install the extra frozen check only in this fresh, exact F02 database.
	o.stage = "install-published-fault"
	containerID, err := private("/input/database-container-id", 128)
	if err != nil || !hex64.Match(containerID) || !hex64.MatchString(BuildNormalReceipt) {
		return errRejected
	}
	settingsInput, _ := json.Marshal(map[string]string{"run_id": BuildRunID, "source_commit": BuildSource, "database_container_id": string(containerID), "database_password": password})
	settingsCtx, settingsCancel := context.WithTimeout(o.ctx, 10*time.Second)
	defer settingsCancel()
	settingsCmd := exec.CommandContext(settingsCtx, "/opt/routex/pg-qualifier", "settings-f02")
	settingsCmd.Stdin = bytes.NewReader(settingsInput)
	var settingsOut, settingsErr bytes.Buffer
	settingsCmd.Stdout = &settingsOut
	settingsCmd.Stderr = &settingsErr
	settingsRunErr := settingsCmd.Run()
	if o.save("postgres-settings.json", settingsOut.Bytes()) != nil {
		return errRejected
	}
	if settingsRunErr != nil || settingsErr.Len() != 0 || settingsCtx.Err() != nil {
		return errRejected
	}
	var settingsReceipt struct{ Result, Scope string }
	if json.Unmarshal(settingsOut.Bytes(), &settingsReceipt) != nil || settingsReceipt.Result != "f28_pg_settings_observed" || settingsReceipt.Scope != "f02" {
		return errRejected
	}
	faultInput := map[string]any{"run_id": BuildRunID, "source_commit": BuildSource, "namespace": "routex-f28-f02-" + BuildRunID[:16], "database_container_id": string(containerID), "normal_schema_receipt_sha256": BuildNormalReceipt, "database_created_for_run": true, "fault_only": true, "database_password": password, "kind": "published", "creation_id": creation, "cleanup_id": command, "actor_id": identity.User.ID, "actor_birth": op.ActorBirth, "integration_id": config.ID, "integration_birth": *op.IntegrationBirth, "credential_id": op.CredentialID, "credential_birth": op.CredentialBirth, "creation_birth": op.CreatedAt, "run_started": start}
	rawFault, err := json.Marshal(faultInput)
	if err != nil {
		return errRejected
	}
	f, err := os.OpenFile("/fixture-f28-input/config.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errRejected
	}
	_, we := f.Write(rawFault)
	ce := f.Close()
	if we != nil || ce != nil {
		return errRejected
	}
	installCtx, installCancel := context.WithTimeout(o.ctx, 15*time.Second)
	defer installCancel()
	install := exec.CommandContext(installCtx, "/opt/routex/fault-helper", "install-published")
	var installed, installErr bytes.Buffer
	install.Stdout = &installed
	install.Stderr = &installErr
	installRunErr := install.Run()
	if o.save("fault-helper-result.json", installed.Bytes()) != nil {
		return errRejected
	}
	if installRunErr != nil || installErr.Len() != 0 || installCtx.Err() != nil {
		return errRejected
	}
	var installReceipt struct {
		Result, Kind string
		Ledger       bool `json:"released_migration_ledger_preserved"`
	}
	if json.Unmarshal(installed.Bytes(), &installReceipt) != nil || installReceipt.Result != "f28_fixture_constraint_installed" || installReceipt.Kind != "published" || !installReceipt.Ledger {
		return errRejected
	}
	if o.save("fault-helper-terminal.json", map[string]any{"original_pid": install.Process.Pid, "joined": true, "original_exit": install.ProcessState.ExitCode(), "stderr_empty": true, "context_expired": false}) != nil {
		return errRejected
	}
	// Preserve the original Session and exact replay intent before dispatch.
	apiURL, _ := http.NewRequest("GET", apiBase, nil)
	cookies := o.client.Jar.Cookies(apiURL.URL)
	if len(cookies) != 1 || cookies[0].Name != "routex_session" {
		return errRejected
	}
	if o.save("private-original-session.json", map[string]string{"cookie": cookies[0].Value, "csrf": o.csrf, "target": target, "reviewed_etag": preview.Review, "request_id": command, "reason": reason, "actor_id": identity.User.ID}) != nil {
		return errRejected
	}
	var version struct {
		Data struct {
			Hash string `json:"hash"`
		} `json:"data"`
	}
	if o.request("version-hash", vaultBase, "POST", "/v1/sys/audit-hash/routex", initialization.Root, "", map[string]string{"input": "1"}, 200, &version) != nil {
		return errRejected
	}
	bracketStart := time.Now().UTC()
	auditBefore, e := o.audit("audit-before.jsonl")
	if e != nil {
		return e
	}
	if o.request("metrics-before", vaultBase, "GET", "/v1/sys/metrics?format=prometheus", initialization.Root, "", nil, 200, nil) != nil {
		return errRejected
	}
	intent := map[string]string{"request_id": command, "reason": reason, "cleanup_token": tokens["cleanup"]}
	if o.api("original-cleanup", "POST", target+"/cleanup", preview.Review, intent, 503, nil) != nil {
		return errRejected
	}
	if o.request("metrics-after", vaultBase, "GET", "/v1/sys/metrics?format=prometheus", initialization.Root, "", nil, 200, nil) != nil {
		return errRejected
	}
	auditAfter, e := o.audit("audit-after.jsonl")
	if e != nil {
		return e
	}
	bracketEnd := time.Now().UTC()
	mb, e := private(filepath.Join(o.dir, "private-response-metrics-before.json"), 4<<20)
	if e != nil {
		return e
	}
	ma, e := private(filepath.Join(o.dir, "private-response-metrics-after.json"), 4<<20)
	if e != nil {
		return e
	}
	before, e := vaultwire.ParseStatusCounters(mb)
	if e != nil {
		return e
	}
	after, e := vaultwire.ParseStatusCounters(ma)
	if e != nil || vaultwire.RequireStatusDelta(before, after, map[uint16]uint64{200: 3, 204: 1}) != nil {
		return errRejected
	}
	bracket, e := vaultwire.ParseAuditDelta(auditBefore, auditAfter, vaultwire.AuditExpectation{ReaderMode: "app_role", DataPath: "kv/data/discovery/routex-credential-" + op.ReferenceID, LoginPath: "auth/routex-f28-" + BuildRunID[:16] + "/login", VersionOneHMAC: version.Data.Hash, ReaderPolicy: "discovery-reader", CleanupPolicy: "discovery-cleanup", Start: bracketStart, End: bracketEnd})
	if e != nil {
		return errRejected
	}
	expectation["as_of"] = time.Now().UTC().Truncate(time.Microsecond)
	input, e := json.Marshal(expectation)
	if e != nil {
		return errRejected
	}
	if o.save("private-helper-input.json", input) != nil {
		return errRejected
	}
	childCtx, cancel := context.WithTimeout(o.ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, "/opt/routex/cleanup-evidence")
	cmd.Stdin = bytes.NewReader(input)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	e = cmd.Start()
	if e == nil {
		e = cmd.Wait()
	}
	helperExit := -1
	if cmd.ProcessState != nil {
		helperExit = cmd.ProcessState.ExitCode()
	}
	if o.save("helper-terminal.json", map[string]any{"original_pid": func() int {
		if cmd.Process != nil {
			return cmd.Process.Pid
		}
		return 0
	}(), "joined": cmd.ProcessState != nil, "original_exit": helperExit, "context_expired": childCtx.Err() != nil, "stdout_sha256": hash(out.Bytes()), "stderr_empty": stderr.Len() == 0}) != nil {
		return errRejected
	}
	if o.save("helper-result.json", out.Bytes()) != nil {
		return errRejected
	}
	if e != nil || stderr.Len() != 0 || out.Len() > 16384 || childCtx.Err() != nil {
		return errRejected
	}
	var evidence struct {
		Result   string                   `json:"result"`
		Evidence cleanupevidence.Evidence `json:"evidence"`
	}
	if json.Unmarshal(out.Bytes(), &evidence) != nil || evidence.Result != "verified_published_cleanup_evidence" || evidence.Evidence.Claim.State != "pending" || evidence.Evidence.Claim.RequestID != command {
		return errRejected
	}
	if evidence.Evidence.Claim.FinishedAt != nil || evidence.Evidence.Claim.ObserveAuditCount != 0 || evidence.Evidence.Claim.ClaimAuditCount != 1 || evidence.Evidence.Claim.KnownNoEffect {
		return errRejected
	}
	if o.api("pending-command-read", "GET", target+"/commands/"+command, "", nil, 200, nil) != nil {
		return errRejected
	}
	closed = true
	if pool.Close() != nil || o.ctx.Err() != nil {
		return errRejected
	}
	return o.save("f02-observations.json", map[string]any{"phase": "original", "source": BuildSource, "source_manifest": BuildManifest, "run": BuildRunID, "original_api_status": 503, "durable_state": "pending", "audit_pairs": bracket.Pairs, "audit_started_at": bracketStart, "audit_finished_at": bracketEnd, "status_before": before.Values(), "status_after": after.Values(), "normal_receipt_sha256": BuildNormalReceipt, "global_completion": false})
}

// Replay uses the same original Session, intent and claim after natural expiry.
// It never installs a check, re-creates a Credential or sends a cleanup Token.
func (o *owner) replay() error {
	originalDir := filepath.Join("/vault/logs", "f02-original-"+BuildRunID)
	raw, err := private(filepath.Join(originalDir, "private-original-session.json"), 16384)
	if err != nil {
		return errRejected
	}
	var intent struct {
		Cookie, CSRF, Target, Reason string
		Review                       string `json:"reviewed_etag"`
		RequestID                    string `json:"request_id"`
		ActorID                      string `json:"actor_id"`
	}
	if json.Unmarshal(raw, &intent) != nil || intent.Cookie == "" || intent.CSRF == "" || intent.Target == "" || intent.RequestID == "" {
		return errRejected
	}
	req, _ := http.NewRequest("GET", apiBase, nil)
	o.client.Jar.SetCookies(req.URL, []*http.Cookie{{Name: "routex_session", Value: intent.Cookie, Path: "/"}})
	raw, err = private(filepath.Join(originalDir, "helper-result.json"), 16384)
	if err != nil {
		return errRejected
	}
	var originalEvidence struct {
		Evidence cleanupevidence.Evidence `json:"evidence"`
	}
	if json.Unmarshal(raw, &originalEvidence) != nil {
		return errRejected
	}
	claim := originalEvidence.Evidence.Claim
	if claim.RequestID != intent.RequestID || claim.State != "pending" || claim.FinishedAt != nil || claim.ClaimAuditCount != 1 || claim.ObserveAuditCount != 0 || claim.KnownNoEffect {
		return errRejected
	}
	// The deadline is the original retained deadline, never an adjusted test clock.
	if remaining := time.Until(claim.Deadline.Add(20 * time.Millisecond)); remaining > 0 {
		if remaining > 31*time.Second {
			return errRejected
		}
		select {
		case <-o.ctx.Done():
			return errRejected
		case <-time.After(remaining):
		}
	}
	ready := false
	for until := time.Now().Add(30 * time.Second); time.Now().Before(until); {
		request, _ := http.NewRequestWithContext(o.ctx, "GET", apiBase+"/api/v1/setup", nil)
		response, readErr := o.client.Do(request)
		if readErr == nil {
			data, bodyErr := io.ReadAll(io.LimitReader(response.Body, 1025))
			closeErr := response.Body.Close()
			if bodyErr == nil && closeErr == nil && len(data) <= 1024 && response.StatusCode == 200 {
				var initialized struct{ Initialized bool }
				if json.Unmarshal(data, &initialized) == nil && initialized.Initialized {
					ready = true
					break
				}
			}
		}
		select {
		case <-o.ctx.Done():
			return errRejected
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		return errRejected
	}
	var session struct {
		CSRF string `json:"csrf_token"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if o.api("same-session-refresh", "GET", "/api/v1/auth/session", "", nil, 200, &session) != nil || session.User.ID != intent.ActorID || session.CSRF == "" {
		return errRejected
	}
	o.csrf = session.CSRF
	currentCookies := o.client.Jar.Cookies(req.URL)
	if len(currentCookies) != 1 || currentCookies[0].Value != intent.Cookie {
		return errRejected
	}
	auditBefore, err := o.audit("replay-audit-before.jsonl")
	if err != nil {
		return errRejected
	}
	var result struct {
		Running bool `json:"running"`
		Receipt struct {
			RequestID string     `json:"request_id"`
			Creation  string     `json:"creation_request_id"`
			State     string     `json:"state"`
			Started   time.Time  `json:"started_at"`
			Finished  *time.Time `json:"finished_at"`
		} `json:"receipt"`
	}
	if o.api("expired-token-free-replay", "POST", intent.Target+"/cleanup", intent.Review, map[string]string{"request_id": intent.RequestID, "reason": intent.Reason}, 200, &result) != nil || result.Running || result.Receipt.State != "unknown" || result.Receipt.RequestID != claim.RequestID || result.Receipt.Creation != claim.CreationRequestID || !result.Receipt.Started.Equal(claim.StartedAt) || result.Receipt.Finished != nil {
		return errRejected
	}
	auditAfter, err := o.audit("replay-audit-after.jsonl")
	if err != nil || !bytes.Equal(auditBefore, auditAfter) {
		return errRejected
	}
	originalAudit, err := private(filepath.Join(originalDir, "audit-after.jsonl"), 1<<20)
	if err != nil || !bytes.Equal(originalAudit, auditBefore) {
		return errRejected
	}
	raw, err = private(filepath.Join(originalDir, "private-helper-input.json"), 16384)
	if err != nil {
		return errRejected
	}
	var expected map[string]any
	if json.Unmarshal(raw, &expected) != nil {
		return errRejected
	}
	expected["as_of"] = time.Now().UTC().Truncate(time.Microsecond)
	input, err := json.Marshal(expected)
	if err != nil {
		return errRejected
	}
	if o.save("private-helper-input.json", input) != nil {
		return errRejected
	}
	// One original read-only projector, with its own positive process join.
	helperCtx, helperCancel := context.WithTimeout(o.ctx, 15*time.Second)
	defer helperCancel()
	helperCmd := exec.CommandContext(helperCtx, "/opt/routex/cleanup-evidence")
	helperCmd.Stdin = bytes.NewReader(input)
	var helperOut, helperErr bytes.Buffer
	helperCmd.Stdout = &helperOut
	helperCmd.Stderr = &helperErr
	helperRunErr := helperCmd.Run()
	projected := helperOut.Bytes()
	if o.save("helper-terminal.json", map[string]any{"original_pid": func() int {
		if helperCmd.Process != nil {
			return helperCmd.Process.Pid
		}
		return 0
	}(), "joined": helperCmd.ProcessState != nil, "original_exit": func() int {
		if helperCmd.ProcessState != nil {
			return helperCmd.ProcessState.ExitCode()
		}
		return -1
	}(), "context_expired": helperCtx.Err() != nil, "stderr_empty": helperErr.Len() == 0, "stdout_sha256": hash(projected)}) != nil {
		return errRejected
	}
	if helperRunErr != nil || helperErr.Len() != 0 || helperCtx.Err() != nil {
		return errRejected
	}
	if len(projected) == 0 || len(projected) > 16384 || o.save("helper-result.json", projected) != nil {
		return errRejected
	}
	var after struct {
		Result   string                   `json:"result"`
		Evidence cleanupevidence.Evidence `json:"evidence"`
	}
	if json.Unmarshal(projected, &after) != nil || after.Result != "verified_published_cleanup_evidence" || after.Evidence.PresentedState != "unknown" || !reflect.DeepEqual(claim, after.Evidence.Claim) || after.Evidence.SourceProcesses != 2 || after.Evidence.SourceUses != 2 {
		return errRejected
	}
	password, readErr := private("/input/database-password", 128)
	if readErr != nil {
		return errRejected
	}
	restartRaw, readErr := private("/input/restart-started-at", 128)
	if readErr != nil {
		return errRejected
	}
	restartAt, readErr := time.Parse(time.RFC3339Nano, string(restartRaw))
	if readErr != nil {
		return errRejected
	}
	originalRaw, readErr := private(filepath.Join(originalDir, "private-original-expectation.json"), 16384)
	if readErr != nil {
		return errRejected
	}
	var originalInput struct {
		CreatorID    string    `json:"creator_process_id"`
		CreatorBirth time.Time `json:"creator_birth"`
	}
	if json.Unmarshal(originalRaw, &originalInput) != nil {
		return errRejected
	}
	db, dbErr := openDB(string(password))
	clear(password)
	if dbErr != nil {
		return errRejected
	}
	pool, dbErr := db.DB()
	if dbErr != nil {
		return errRejected
	}
	lifecycle, lifecycleErr := readLifecycle(o.ctx, db, originalInput.CreatorID, originalInput.CreatorBirth, restartAt)
	closeErr := pool.Close()
	if lifecycleErr != nil || closeErr != nil || o.ctx.Err() != nil {
		return errRejected
	}
	if o.save("private-restart-lifecycle.json", lifecycle) != nil {
		return errRejected
	}
	if o.api("expired-command-read", "GET", intent.Target+"/commands/"+intent.RequestID, "", nil, 200, nil) != nil {
		return errRejected
	}
	return o.save("f02-observations.json", map[string]any{"phase": "replay", "run": BuildRunID, "source": BuildSource, "source_manifest": BuildManifest, "same_original_session": true, "original_claim_tuple_unchanged": true, "original_deadline_elapsed": time.Now().After(claim.Deadline), "additional_Vault_events": 0, "durable_state": "pending", "presented_state": "unknown", "source_processes": after.Evidence.SourceProcesses, "source_uses": after.Evidence.SourceUses, "global_completion": false})
}

type instanceFact struct {
	ID, Role                  string
	StartedAt, LeaseExpiresAt time.Time
	StoppedAt, RetiredAt      *time.Time
}
type processFact struct {
	ProcessID, Generation string
	Birth, RegisteredAt   time.Time
	ClosedAt              *time.Time
}
type lifecycleFacts struct {
	Instances []instanceFact
	Processes []processFact
}

func validateLifecycle(f lifecycleFacts, creator string, birth, restart, asOf time.Time) bool {
	if len(f.Instances) != 2 || len(f.Processes) != 2 || creator == "" || birth.IsZero() || !restart.After(birth) || asOf.Before(restart) {
		return false
	}
	oldCount, newCount := 0, 0
	seen := map[string]bool{}
	for _, ins := range f.Instances {
		if seen[ins.ID] || ins.Role != "combined" || ins.RetiredAt != nil {
			return false
		}
		seen[ins.ID] = true
		var process *processFact
		for i := range f.Processes {
			if f.Processes[i].ProcessID == ins.ID {
				if process != nil {
					return false
				}
				process = &f.Processes[i]
			}
		}
		if process == nil || !hex64.MatchString(process.Generation) || !process.Birth.Equal(ins.StartedAt) || process.RegisteredAt.Before(process.Birth) {
			return false
		}
		if ins.ID == creator {
			oldCount++
			if !ins.StartedAt.Equal(birth) || ins.StoppedAt == nil || process.ClosedAt == nil || !ins.StoppedAt.Equal(*process.ClosedAt) || ins.StoppedAt.Before(birth) || ins.StoppedAt.After(restart) {
				return false
			}
		} else {
			newCount++
			if ins.StartedAt.Before(restart) || ins.StartedAt.After(asOf) || ins.StoppedAt != nil || process.ClosedAt != nil || !ins.LeaseExpiresAt.After(asOf) {
				return false
			}
		}
	}
	return oldCount == 1 && newCount == 1
}
func readLifecycle(ctx context.Context, db *gorm.DB, creator string, birth, restart time.Time) (facts lifecycleFacts, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx := db.WithContext(ctx).Begin(&sqlReadOnly)
	if tx.Error != nil {
		return facts, errRejected
	}
	defer func() {
		if tx.Rollback().Error != nil || ctx.Err() != nil {
			err = errRejected
		}
	}()
	if tx.Table("system_instances").Select("id,role,started_at,lease_expires_at,stopped_at,retired_at").Order("id").Limit(3).Find(&facts.Instances).Error != nil || tx.Table("credential_source_processes").Select("process_id,generation,birth,registered_at,closed_at").Order("process_id").Limit(3).Find(&facts.Processes).Error != nil || !validateLifecycle(facts, creator, birth, restart, time.Now().UTC()) {
		return lifecycleFacts{}, errRejected
	}
	return facts, nil
}

var artifactNames = []string{"private-restart-lifecycle.json", "postgres-settings.json", "http-census.json", "f02-observations.json", "private-original-session.json", "fault-helper-result.json", "fault-helper-terminal.json", "private-original-expectation.json", "private-helper-input.json", "helper-result.json", "helper-terminal.json", "helper-replay-result.json", "helper-replay-terminal.json", "audit-before.jsonl", "audit-after.jsonl", "replay-audit-before.jsonl", "replay-audit-after.jsonl"}
var responseNames = []string{"setup", "integration-catalog", "integration-create", "policy-read", "policy-write", "storage-context", "provider-create", "retained-preview", "credential-metadata", "credential-delete", "cleanup-preview", "version-hash", "metrics-before", "original-cleanup", "metrics-after", "token-free-replay", "pending-command-read", "same-session-refresh", "expired-token-free-replay", "expired-command-read"}

type artifact struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func normalManifest(dir string) ([]artifact, error) {
	wanted := map[string]bool{}
	for _, n := range artifactNames {
		wanted[n] = true
	}
	for _, n := range responseNames {
		wanted["private-response-"+n+".json"] = true
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) > len(wanted) {
		return nil, errRejected
	}
	files := make([]artifact, 0, len(entries))
	for _, entry := range entries {
		if !wanted[entry.Name()] {
			return nil, errRejected
		}
		b, e := private(filepath.Join(dir, entry.Name()), 4<<20)
		if e != nil {
			return nil, errRejected
		}
		files = append(files, artifact{entry.Name(), len(b), hash(b)})
	}
	return files, nil
}

func run(args []string) int {
	uid, e := strconv.Atoi(BuildHostUID)
	if (len(args) != 1 || (args[0] != "original" && args[0] != "replay")) || e != nil || uid <= 0 || strconv.Itoa(uid) != BuildHostUID || runtime.GOOS != "linux" || os.Getuid() != uid || os.Geteuid() != uid || !hex32.MatchString(BuildRunID) || !hex40.MatchString(BuildSource) || !hex64.MatchString(BuildManifest) || !hex64.MatchString(BuildNormalReceipt) {
		return 1
	}
	dir := filepath.Join("/vault/logs", "f02-"+args[0]+"-"+BuildRunID)
	if os.Mkdir(dir, 0700) != nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second}
	defer tr.CloseIdleConnections()
	o := &owner{ctx: ctx, client: &http.Client{Jar: jar, Transport: tr, Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, dir: dir, stage: "input"}
	password, e := private("/input/database-password", 128)
	if e == nil {
		if args[0] == "original" {
			e = o.observe(strings.TrimSpace(string(password)))
		} else {
			e = o.replay()
		}
	}
	clear(password)
	if o.save("http-census.json", o.http) != nil {
		e = errRejected
	}
	files, manifestErr := normalManifest(dir)
	if manifestErr != nil {
		e = errRejected
	}
	if o.save("f02-terminal.json", map[string]any{"phase": args[0], "normal_acknowledgement": false, "files": files, "passed": e == nil, "last_stage": o.stage, "run": BuildRunID, "source": BuildSource, "source_manifest": BuildManifest, "global_completion": false}) != nil {
		e = errRejected
	}
	if e != nil {
		return 1
	}
	return 0
}
func main() { os.Exit(run(os.Args[1:])) }
