// Command vault-cleanup-wire-client observes one disposable Vault dummy object.
// It never invokes RouteX or accepts an endpoint, token, path or SQL argument.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var BuildRunID, BuildSourceManifest, BuildHostUID string

var errRefused = errors.New("wire discovery refused")
var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var auditHMAC = regexp.MustCompile(`^hmac-sha256:[a-f0-9]{64}$`)

type receipt struct {
	Sequence int    `json:"sequence"`
	Name     string `json:"name"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	Bytes    int    `json:"bytes"`
	SHA256   string `json:"sha256"`
}

type witness struct {
	client  *http.Client
	ctx     context.Context
	dir     string
	stage   string
	rows    []receipt
	secrets []string
}

type artifact struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

var artifactNames = []string{"private-initialization.json", "private-token-writer.json", "private-token-reader.json", "private-token-cleanup.json", "private-material.json", "metrics-before.prom", "metrics-after.prom", "observed-wire.json", "http-census.json", "private-response-dummy-create.json", "private-response-dummy-read.json", "private-response-dummy-metadata.json", "private-response-audit-version-hash.json"}

func privateManifest(dir string) ([]artifact, error) {
	wanted := map[string]bool{}
	for _, name := range artifactNames {
		wanted[name] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > len(wanted) {
		return nil, errRefused
	}
	rows := make([]artifact, 0, len(entries))
	for _, entry := range entries {
		if !wanted[entry.Name()] {
			return nil, errRefused
		}
		info, err := os.Lstat(filepath.Join(dir, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4<<20 {
			return nil, errRefused
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil || int64(len(raw)) != info.Size() {
			return nil, errRefused
		}
		rows = append(rows, artifact{entry.Name(), len(raw), digest(raw)})
	}
	return rows, nil
}

func bindings(run, source, trusted, goos string, uid, euid int) bool {
	n, err := strconv.Atoi(trusted)
	return err == nil && strconv.Itoa(n) == trusted && n > 0 && n == uid && uid == euid && goos == "linux" && hex32.MatchString(run) && hex64.MatchString(source)
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func save(dir, name string, value any) error {
	raw, ok := value.([]byte)
	if !ok {
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			return errRefused
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errRefused
	}
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errRefused
	}
	return nil
}

func (w *witness) request(name, method, path, token string, body any, status int) ([]byte, error) {
	w.stage = name
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil || len(raw) > 16384 {
			return nil, errRefused
		}
	}
	req, err := http.NewRequestWithContext(w.ctx, method, "http://127.0.0.1:8200"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, errRefused
	}
	// A fresh connection and no replay body prevent a transport from retrying a
	// possibly dispatched mutation after a stale connection or redirect.
	req.Close, req.GetBody = true, nil
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	response, err := w.client.Do(req)
	if err != nil {
		return nil, errRefused
	}
	value, readErr := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	closeErr := response.Body.Close()
	w.rows = append(w.rows, receipt{len(w.rows) + 1, name, method, path, response.StatusCode, len(value), digest(value)})
	if readErr != nil || closeErr != nil || len(value) > 4<<20 || response.StatusCode != status {
		return nil, errRefused
	}
	return value, nil
}

func (w *witness) json(name, method, path, token string, body any, status int, target any) error {
	raw, err := w.request(name, method, path, token, body, status)
	if err != nil {
		return err
	}
	switch name {
	case "dummy-create", "dummy-read", "dummy-metadata", "audit-version-hash":
		if save(w.dir, "private-response-"+name+".json", raw) != nil {
			return errRefused
		}
	}
	if target != nil && json.Unmarshal(raw, target) != nil {
		return errRefused
	}
	return nil
}

func secretFree(raw []byte, secrets []string) bool {
	if len(secrets) == 0 {
		return false
	}
	for _, secret := range secrets {
		if len(secret) < 16 {
			return false
		}
		materials := [][]byte{[]byte(secret)}
		if hex64.MatchString(secret) {
			value, _ := hex.DecodeString(secret)
			materials = append(materials, value)
		}
		for _, value := range materials {
			for _, form := range [][]byte{value, []byte(hex.EncodeToString(value)), []byte(strings.ToUpper(hex.EncodeToString(value))), []byte(base64.StdEncoding.EncodeToString(value))} {
				if bytes.Contains(raw, form) {
					return false
				}
			}
		}
	}
	return true
}

func randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", errRefused
	}
	return hex.EncodeToString(raw), nil
}

func (w *witness) observe() error {
	until := time.Now().Add(10 * time.Second)
	for {
		_, err := w.request("sealed-health", "GET", "/v1/sys/health", "", nil, 501)
		if err == nil {
			break
		}
		if len(w.rows) != 0 || time.Now().After(until) || w.ctx.Err() != nil {
			return errRefused
		}
		time.Sleep(100 * time.Millisecond)
	}
	var init struct {
		Keys      []string `json:"keys"`
		RootToken string   `json:"root_token"`
	}
	if w.json("initialize", "PUT", "/v1/sys/init", "", map[string]int{"secret_shares": 1, "secret_threshold": 1}, 200, &init) != nil || len(init.Keys) != 1 || len(init.RootToken) < 16 || len(init.Keys[0]) < 16 {
		return errRefused
	}
	w.secrets = append(w.secrets, init.RootToken, init.Keys[0])
	if save(w.dir, "private-initialization.json", init) != nil {
		return errRefused
	}
	if w.json("unseal", "PUT", "/v1/sys/unseal", "", map[string]string{"key": init.Keys[0]}, 200, nil) != nil {
		return errRefused
	}
	root := init.RootToken
	if w.json("enable-kv", "POST", "/v1/sys/mounts/kv", root, map[string]any{"type": "kv", "options": map[string]string{"version": "2"}}, 204, nil) != nil {
		return errRefused
	}
	if w.json("enable-audit", "PUT", "/v1/sys/audit/routex", root, map[string]any{"type": "file", "options": map[string]string{"file_path": "/vault/logs/audit.jsonl"}}, 204, nil) != nil {
		return errRefused
	}
	tokens := map[string]string{}
	for _, spec := range []struct {
		name, path   string
		capabilities []string
	}{{"writer", "data", []string{"create", "update"}}, {"reader", "data", []string{"read"}}, {"cleanup", "destroy", []string{"update"}}} {
		caps, _ := json.Marshal(spec.capabilities)
		policy := fmt.Sprintf("path \"kv/%s/discovery/*\" { capabilities = %s }", spec.path, caps)
		if w.json("policy-"+spec.name, "PUT", "/v1/sys/policies/acl/discovery-"+spec.name, root, map[string]string{"policy": policy}, 204, nil) != nil {
			return errRefused
		}
		var value struct {
			Auth struct {
				ClientToken string `json:"client_token"`
			} `json:"auth"`
		}
		if w.json("token-"+spec.name, "POST", "/v1/auth/token/create", root, map[string]any{"policies": []string{"discovery-" + spec.name}, "ttl": "300s", "renewable": false, "no_default_policy": true}, 200, &value) != nil || len(value.Auth.ClientToken) < 16 {
			return errRefused
		}
		tokens[spec.name] = value.Auth.ClientToken
		w.secrets = append(w.secrets, value.Auth.ClientToken)
		if save(w.dir, "private-token-"+spec.name+".json", value) != nil {
			return errRefused
		}
	}
	object, err := randomHex(16)
	if err != nil {
		return err
	}
	secret, err := randomHex(32)
	if err != nil {
		return err
	}
	w.secrets = append(w.secrets, secret)
	if save(w.dir, "private-material.json", w.secrets) != nil {
		return errRefused
	}
	// Vault HMACs the audit version value. Obtain its real hash from this exact
	// fresh audit device instead of treating an opaque audit string as version 1.
	var versionProof struct {
		Data struct {
			Hash string `json:"hash"`
		} `json:"data"`
	}
	if w.json("audit-version-hash", "POST", "/v1/sys/audit-hash/routex", root, map[string]string{"input": "1"}, 200, &versionProof) != nil || !auditHMAC.MatchString(versionProof.Data.Hash) {
		return errRefused
	}
	var created struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if w.json("dummy-create", "POST", "/v1/kv/data/discovery/"+object, tokens["writer"], map[string]any{"options": map[string]int{"cas": 0}, "data": map[string]string{"value": secret, "owner": BuildRunID}}, 200, &created) != nil || created.Data.Version != 1 {
		return errRefused
	}
	var read struct {
		Data struct {
			Data struct {
				Value string `json:"value"`
				Owner string `json:"owner"`
			} `json:"data"`
		} `json:"data"`
	}
	if w.json("dummy-read", "GET", "/v1/kv/data/discovery/"+object+"?version=1", tokens["reader"], nil, 200, &read) != nil || read.Data.Data.Value != secret || read.Data.Data.Owner != BuildRunID {
		return errRefused
	}
	baseline, err := w.request("metrics-before", "GET", "/v1/sys/metrics?format=prometheus", root, nil, 200)
	if err != nil || !secretFree(baseline, w.secrets) || save(w.dir, "metrics-before.prom", baseline) != nil {
		return errRefused
	}
	// Exactly one original dummy PUT. An uncertain response ends the run.
	if w.json("dummy-destroy", "PUT", "/v1/kv/destroy/discovery/"+object, tokens["cleanup"], map[string][]int{"versions": {1}}, 204, nil) != nil {
		return errRefused
	}
	final, err := w.request("metrics-after", "GET", "/v1/sys/metrics?format=prometheus", root, nil, 200)
	if err != nil || !secretFree(final, w.secrets) || save(w.dir, "metrics-after.prom", final) != nil {
		return errRefused
	}
	var metadata struct {
		Data struct {
			Versions map[string]struct {
				Destroyed bool `json:"destroyed"`
			} `json:"versions"`
		} `json:"data"`
	}
	if w.json("dummy-metadata", "GET", "/v1/kv/metadata/discovery/"+object, root, nil, 200, &metadata) != nil || len(metadata.Data.Versions) != 1 || !metadata.Data.Versions["1"].Destroyed {
		return errRefused
	}
	return save(w.dir, "observed-wire.json", map[string]any{"kind": "actual_dummy_vault_wire_discovery", "run": BuildRunID, "source_manifest": BuildSourceManifest, "dummy_object": object, "literal_destroy_status": 204, "version_one_audit_hmac": versionProof.Data.Hash, "application_runtime_acceptance": false, "numeric_metrics_attribution": false, "complete_audit_schema_qualification": false})
}

func run(args []string, out io.Writer) int {
	if len(args) != 0 || !bindings(BuildRunID, BuildSourceManifest, BuildHostUID, runtime.GOOS, os.Getuid(), os.Geteuid()) {
		return 1
	}
	parent, err := os.Lstat("/vault/logs")
	if err != nil || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		return 1
	}
	dir := "/vault/logs/discovery-" + BuildRunID
	if os.Mkdir(dir, 0700) != nil {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: 2 * time.Second}
	defer transport.CloseIdleConnections()
	w := &witness{client: &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, ctx: ctx, dir: dir, stage: "startup"}
	err = w.observe()
	if save(dir, "http-census.json", w.rows) != nil {
		err = errRefused
	}
	files, manifestErr := privateManifest(dir)
	if manifestErr != nil {
		err = errRefused
	}
	if save(dir, "native-terminal.json", map[string]any{"passed": err == nil, "last_stage": w.stage, "source_manifest": BuildSourceManifest, "run": BuildRunID, "files": files, "application_runtime_acceptance": false}) != nil {
		err = errRefused
	}
	if err != nil {
		return 1
	}
	if json.NewEncoder(out).Encode(map[string]any{"result": "actual_dummy_wire_observed", "source_manifest": BuildSourceManifest, "run": BuildRunID, "application_runtime_acceptance": false}) != nil {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
