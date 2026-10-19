// Command vault-cleanup-normal-client executes one disposable published cleanup.
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

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/miclle/routex/internal/routex/entity"
	cleanupevidence "github.com/miclle/routex/scripts/provider-credential-cleanup-evidence"
	vaultwire "github.com/miclle/routex/scripts/vault-cleanup-wire"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var BuildRunID, BuildSource, BuildManifest, BuildHostUID, BuildStarted string
var BuildDriver = "postgres"

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
	// Cookies are scoped by host rather than port. The shared namespace uses
	// loopback for both services, so the Vault observer must have no Session jar.
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
	dsn := fmt.Sprintf("host=postgres port=5432 user=routex_f28_normal password=%s dbname=routex_f28_normal_%s sslmode=disable connect_timeout=2 application_name=routex-f28-normal", password, BuildRunID)
	dialector := postgres.Open(dsn)
	if BuildDriver == "mysql" {
		config := mysqlconfig.NewConfig()
		config.User, config.Passwd, config.Net, config.Addr, config.DBName = "routex_f28_normal", password, "tcp", "mysql:3306", "routex_f28_normal_"+BuildRunID
		config.ParseTime, config.Loc = true, time.UTC
		config.Timeout, config.ReadTimeout, config.WriteTimeout = 2*time.Second, 2*time.Second, 2*time.Second
		config.Params = map[string]string{"charset": "utf8mb4"}
		dialector = gormmysql.New(gormmysql.Config{DSN: config.FormatDSN(), SkipInitializeWithVersion: true})
	} else if BuildDriver != "postgres" {
		return nil, errRejected
	}
	db, e := gorm.Open(dialector, &gorm.Config{TranslateError: true, Logger: logger.Discard, DisableAutomaticPing: true})
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
	var result struct {
		Running bool `json:"running"`
		Receipt struct {
			State     string `json:"state"`
			RequestID string `json:"request_id"`
			Cleanup   struct {
				State       string                              `json:"state"`
				Observation struct{ Attempted, Succeeded bool } `json:"observation"`
			} `json:"cleanup"`
		} `json:"receipt"`
	}
	if o.api("original-cleanup", "POST", target+"/cleanup", preview.Review, intent, 200, &result) != nil || result.Running || result.Receipt.State != "acknowledged" || result.Receipt.RequestID != command || !result.Receipt.Cleanup.Observation.Attempted || !result.Receipt.Cleanup.Observation.Succeeded {
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
	if json.Unmarshal(out.Bytes(), &evidence) != nil || evidence.Result != "verified_published_cleanup_evidence" || evidence.Evidence.Claim.State != "acknowledged" || evidence.Evidence.Claim.RequestID != command {
		return errRejected
	}
	originalBytes, _ := private(filepath.Join(o.dir, "private-response-original-cleanup.json"), 32768)
	delete(intent, "cleanup_token")
	var replay any
	replayAuditBefore, e := o.audit("replay-audit-before.jsonl")
	if e != nil {
		return errRejected
	}
	if o.api("token-free-replay", "POST", target+"/cleanup", preview.Review, intent, 200, &replay) != nil {
		return errRejected
	}
	replayAuditAfter, e := o.audit("replay-audit-after.jsonl")
	if e != nil || !bytes.Equal(replayAuditBefore, replayAuditAfter) {
		return errRejected
	}
	var originalValue any
	if json.Unmarshal(originalBytes, &originalValue) != nil || !reflect.DeepEqual(originalValue, replay) {
		return errRejected
	}
	// Re-project durable rows after replay rather than accepting only HTTP equality.
	replayCmd := exec.CommandContext(childCtx, "/opt/routex/cleanup-evidence")
	replayCmd.Stdin = bytes.NewReader(input)
	var replayOut, replayErr bytes.Buffer
	replayCmd.Stdout = &replayOut
	replayCmd.Stderr = &replayErr
	replayRunErr := replayCmd.Run()
	if o.save("helper-replay-result.json", replayOut.Bytes()) != nil {
		return errRejected
	}
	if replayRunErr != nil || replayErr.Len() != 0 || childCtx.Err() != nil || !bytes.Equal(replayOut.Bytes(), out.Bytes()) {
		return errRejected
	}
	if o.save("helper-replay-terminal.json", map[string]any{"original_pid": replayCmd.Process.Pid, "joined": true, "original_exit": replayCmd.ProcessState.ExitCode(), "context_expired": false, "stdout_sha256": hash(replayOut.Bytes()), "stderr_empty": true}) != nil {
		return errRejected
	}
	if o.api("original-command-read", "GET", target+"/commands/"+command, "", nil, 200, nil) != nil {
		return errRejected
	}
	o.stage = "creation-history-readback"
	opAfter, _, e := original(o.ctx, db, creation, identity.User.ID, config.ID, config.Revision)
	if e != nil || !reflect.DeepEqual(op, opAfter) {
		return errRejected
	}
	closed = true
	if pool.Close() != nil || o.ctx.Err() != nil {
		return errRejected
	}
	return o.save("normal-observations.json", map[string]any{"source": BuildSource, "source_manifest": BuildManifest, "run": BuildRunID, "creation_request_id": creation, "request_id": command, "original_api_status": 200, "durable_state": evidence.Evidence.Claim.State, "status_before": before.Values(), "status_after": after.Values(), "audit_pairs": bracket.Pairs, "token_free_replay_identical": true, "replay_no_vault_events": true, "original_creation_unchanged": true, "pool_closed": true, "product_cleanup_observed": true, "global_completion": false})
}

var artifactNames = []string{"http-census.json", "normal-observations.json", "private-original-expectation.json", "private-helper-input.json", "helper-result.json", "helper-terminal.json", "helper-replay-result.json", "helper-replay-terminal.json", "audit-before.jsonl", "audit-after.jsonl", "replay-audit-before.jsonl", "replay-audit-after.jsonl"}
var responseNames = []string{"setup", "integration-catalog", "integration-create", "policy-read", "policy-write", "storage-context", "provider-create", "retained-preview", "credential-metadata", "credential-delete", "cleanup-preview", "version-hash", "metrics-before", "original-cleanup", "metrics-after", "token-free-replay", "original-command-read"}

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
	if (BuildDriver != "postgres" && BuildDriver != "mysql") || len(args) != 0 || e != nil || uid <= 0 || strconv.Itoa(uid) != BuildHostUID || runtime.GOOS != "linux" || os.Getuid() != uid || os.Geteuid() != uid || !hex32.MatchString(BuildRunID) || !hex40.MatchString(BuildSource) || !hex64.MatchString(BuildManifest) {
		return 1
	}
	dir := filepath.Join("/vault/logs", "normal-"+BuildRunID)
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
		e = o.observe(strings.TrimSpace(string(password)))
	}
	clear(password)
	if o.save("http-census.json", o.http) != nil {
		e = errRejected
	}
	files, manifestErr := normalManifest(dir)
	if manifestErr != nil {
		e = errRejected
	}
	if o.save("normal-terminal.json", map[string]any{"files": files, "passed": e == nil, "last_stage": o.stage, "run": BuildRunID, "source": BuildSource, "source_manifest": BuildManifest, "global_completion": false}) != nil {
		e = errRejected
	}
	if e != nil {
		return 1
	}
	return 0
}
func main() { os.Exit(run(os.Args[1:])) }
