package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var integrationPattern = regexp.MustCompile(`^vlt_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var revisionPattern = regexp.MustCompile(`^vlr_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var vaultReviewPattern = regexp.MustCompile(`^[a-f0-9]{64}\.[a-f0-9]{64}$`)

type authMetadata struct {
	Method     string `json:"method"`
	Configured bool   `json:"configured"`
}
type integrationView struct {
	ID         string       `json:"id"`
	RevisionID string       `json:"revision_id"`
	ReviewETag string       `json:"review_etag"`
	WriterAuth authMetadata `json:"writer_auth"`
	ReaderAuth authMetadata `json:"reader_auth"`
}
type observation struct {
	Attempted bool `json:"attempted"`
	Succeeded bool `json:"succeeded"`
	Failure   *struct {
		Stage      string `json:"stage"`
		Code       string `json:"code"`
		HTTPStatus int    `json:"http_status"`
	} `json:"failure"`
}
type probeView struct {
	ID            string      `json:"id"`
	IntegrationID string      `json:"integration_id"`
	RevisionID    string      `json:"revision_id"`
	ReviewETag    string      `json:"review_etag"`
	State         string      `json:"state"`
	Version       *int64      `json:"version"`
	Write         observation `json:"write"`
	Read          observation `json:"read"`
	Cleanup       struct {
		State       string      `json:"state"`
		Observation observation `json:"observation"`
	} `json:"cleanup"`
}
type fixtureProfile struct {
	Run, Source, Manifest              string
	Actor, CSRF, Integration, Revision string
	Probe                              string
	ReadReview                         string
	ReadInput                          map[string]string
	Cookies                            []*http.Cookie
	OriginalProcess                    string
	Rotation                           string
	Rotated                            bool
}

func (o *fixtureOwner) audit() ([]byte, error) {
	data, err := privateInput("/vault/logs/audit.jsonl", 4<<20)
	if err != nil || data[len(data)-1] != '\n' {
		return nil, errFixture
	}
	return data, nil
}

func (o *fixtureOwner) ready() error {
	// Read-only readiness never retries a mutation or renews the original deadline.
	for deadline := time.Now().Add(30 * time.Second); ; {
		request, _ := http.NewRequestWithContext(o.ctx, http.MethodGet, applicationURL+"/api/v1/setup", nil)
		response, err := o.client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) || o.ctx.Err() != nil {
			return errFixture
		}
		select {
		case <-o.ctx.Done():
			return errFixture
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

func (o *fixtureOwner) setup() (string, error) {
	if o.ready() != nil {
		return "", errFixture
	}
	var password [32]byte
	if _, err := rand.Read(password[:]); err != nil {
		return "", errFixture
	}
	var session struct {
		User struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"user"`
		CSRF string `json:"csrf_token"`
	}
	input := map[string]string{"email": "certificate-" + BuildRunID + "@example.invalid", "name": "Controlled saved certificate", "password": hex.EncodeToString(password[:])}
	if o.api("setup", "POST", "/setup", "", input, 201, &session) != nil || session.User.Role != "admin" || session.User.ID == "" || len(session.CSRF) != 64 {
		return "", errFixture
	}
	o.csrf = session.CSRF
	return session.User.ID, nil
}

func (o *fixtureOwner) certificateAuth(role string, files map[string][]byte) map[string]string {
	return map[string]string{
		"action": "replace", "method": "certificate", "auth_mount": "routex-cert-" + BuildRunID[:16],
		"certificate_role": role, "certificate_pem": string(files[role+".pem"]),
		"private_key_pem": string(files[role+".key"]), "server_ca_pem": string(files["server-ca.pem"]),
	}
}

func (o *fixtureOwner) createIntegration(name string, writer, reader map[string]string) (integrationView, error) {
	var catalog struct {
		Review string `json:"review_etag"`
	}
	if o.api(name+"-catalog", "GET", "/admin/secrets/integrations", "", nil, 200, &catalog) != nil || !vaultReviewPattern.MatchString(catalog.Review) {
		return integrationView{}, errFixture
	}
	id, err := freshUUID()
	if err != nil {
		return integrationView{}, err
	}
	var saved struct {
		ID        string `json:"integration_id"`
		Revision  string `json:"revision_id"`
		Committed bool   `json:"committed"`
	}
	input := map[string]any{
		"request_id": id, "name": "Controlled certificate " + name + " " + BuildRunID,
		"descriptor":  map[string]string{"endpoint": "https://127.0.0.1:8201", "namespace": "", "mount": "kv", "prefix": "discovery", "data_field": "value"},
		"writer_auth": writer, "reader_auth": reader, "reason": "Review isolated saved certificate authentication",
	}
	if o.api(name+"-create", "POST", "/admin/secrets/integrations", catalog.Review, input, 200, &saved) != nil || !saved.Committed || !integrationPattern.MatchString(saved.ID) || !revisionPattern.MatchString(saved.Revision) {
		return integrationView{}, errFixture
	}
	view, err := o.integration(name+"-view", saved.ID)
	if err != nil || view.RevisionID != saved.Revision {
		return integrationView{}, errFixture
	}
	return view, nil
}

func (o *fixtureOwner) integration(name, id string) (integrationView, error) {
	var view integrationView
	if !integrationPattern.MatchString(id) || o.api(name, "GET", "/admin/secrets/integrations/"+id, "", nil, 200, &view) != nil || view.ID != id || !revisionPattern.MatchString(view.RevisionID) || !vaultReviewPattern.MatchString(view.ReviewETag) || view.WriterAuth.Method != "certificate" || view.ReaderAuth.Method != "certificate" || !view.WriterAuth.Configured || !view.ReaderAuth.Configured {
		return view, errFixture
	}
	raw, err := privateInput(filepath.Join(o.dir, o.phase+"-private-response-"+name+".json"), 4<<20)
	if err != nil || bytes.Contains(raw, []byte("-----BEGIN ")) {
		return view, errFixture
	}
	for _, material := range o.sensitiveValues {
		if bytes.Contains(raw, material) {
			return view, errFixture
		}
	}
	return view, nil
}

func (o *fixtureOwner) writeProbe(name string, view integrationView) (probeView, error) {
	id, err := freshUUID()
	if err != nil {
		return probeView{}, err
	}
	var probe probeView
	if o.api(name, "POST", "/admin/secrets/integrations/"+view.ID+"/probes/write", view.ReviewETag, map[string]string{"request_id": id, "reason": "Explicit controlled certificate Write"}, 200, &probe) != nil || !runIDPattern.MatchString(probe.ID) || probe.IntegrationID != view.ID || probe.RevisionID != view.RevisionID || !vaultReviewPattern.MatchString(probe.ReviewETag) {
		return probe, errFixture
	}
	return probe, nil
}

func readSucceededWithDeniedCleanup(probe probeView) bool {
	return probe.Write.Succeeded && probe.Read.Succeeded && probe.Version != nil && *probe.Version == 1 && probe.State == "cleanup_pending" && probe.Cleanup.State == "failed" && probe.Cleanup.Observation.Failure != nil && probe.Cleanup.Observation.Failure.Stage == "cleanup" && probe.Cleanup.Observation.Failure.HTTPStatus == 403
}

func (o *fixtureOwner) readProbe(name string, profile *fixtureProfile, probe probeView) error {
	id, err := freshUUID()
	if err != nil {
		return err
	}
	input := map[string]string{"request_id": id, "reason": "Explicit controlled saved certificate Read"}
	var result probeView
	if o.api(name, "POST", "/admin/secrets/integrations/"+profile.Integration+"/probes/"+probe.ID+"/read", probe.ReviewETag, input, 200, &result) != nil || result.ID != probe.ID || result.RevisionID != profile.Revision || !readSucceededWithDeniedCleanup(result) {
		return errFixture
	}
	if o.phase == "before" {
		profile.ReadReview, profile.ReadInput = probe.ReviewETag, input
	}
	return nil
}

func loadMaterials() (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, name := range []string{"server-ca.pem", "client-ca.pem", "writer.pem", "writer.key", "reader.pem", "reader.key"} {
		data, err := privateInput("/input/"+name, 16<<10)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}

func (o *fixtureOwner) before() error {
	files, err := loadMaterials()
	if err != nil {
		return err
	}
	for _, value := range files {
		o.sensitiveValues = append(o.sensitiveValues, value)
	}
	data, err := privateInput("/vault/logs/discovery-"+BuildRunID+"/private-initialization.json", 32768)
	var initialization struct {
		Token string `json:"root_token"`
	}
	if err != nil || json.Unmarshal(data, &initialization) != nil || len(initialization.Token) < 16 {
		return errFixture
	}
	mount := "routex-cert-" + BuildRunID[:16]
	if o.control("enable-cert", "POST", "/sys/auth/"+mount, initialization.Token, map[string]string{"type": "cert"}, 204) != nil {
		return errFixture
	}
	for _, role := range []string{"writer", "reader"} {
		policy := "routex-cert-" + role + "-" + BuildRunID
		capabilities := `["create","update"]`
		if role == "reader" {
			capabilities = `["read"]`
		}
		if o.control("policy-"+role, "PUT", "/sys/policies/acl/"+policy, initialization.Token, map[string]string{"policy": `path "kv/data/discovery/*" { capabilities = ` + capabilities + ` }`}, 204) != nil || o.control("role-"+role, "POST", "/auth/"+mount+"/certs/"+role, initialization.Token, map[string]any{
			"certificate": string(files["client-ca.pem"]), "allowed_common_names": []string{"routex-" + role + "-" + BuildRunID},
			"token_policies": []string{policy}, "token_ttl": 120, "token_max_ttl": 120, "token_explicit_max_ttl": 120, "token_no_default_policy": true,
		}, 204) != nil {
			return errFixture
		}
	}
	// Bind remote audit metadata to this device's HMAC, rather than comparing
	// unrelated local tuples or trusting redacted metadata labels.
	hashes := map[string]string{}
	for _, value := range []string{"writer", "reader", "routex-writer-" + BuildRunID, "routex-reader-" + BuildRunID} {
		var response struct {
			Data struct {
				Hash string `json:"hash"`
			} `json:"data"`
		}
		if o.request("audit-hash-"+value, controlURL, "POST", "/v1/sys/audit-hash/routex", initialization.Token, "", map[string]string{"input": value}, 200, &response) != nil || !regexp.MustCompile(`^hmac-sha256:[a-f0-9]{64}$`).MatchString(response.Data.Hash) {
			return errFixture
		}
		hashes[value] = response.Data.Hash
	}
	if o.save("audit-expectations.json", hashes) != nil {
		return errFixture
	}
	actor, err := o.setup()
	if err != nil {
		return err
	}
	writer, reader := o.certificateAuth("writer", files), o.certificateAuth("reader", files)
	view, err := o.createIntegration("valid", writer, reader)
	if err != nil {
		return err
	}
	probe, err := o.writeProbe("valid-write", view)
	if err != nil || !probe.Write.Succeeded || probe.Read.Attempted || probe.State != "awaiting_read" || probe.Version == nil || *probe.Version != 1 {
		return errFixture
	}
	profile := fixtureProfile{Run: BuildRunID, Source: BuildSource, Manifest: BuildSourceManifest, Actor: actor, CSRF: o.csrf, Integration: view.ID, Revision: view.RevisionID, Probe: probe.ID}
	if o.readProbe("valid-read", &profile, probe) != nil {
		return errFixture
	}
	for _, negative := range []string{"wrong-role", "wrong-ca"} {
		badWriter := o.certificateAuth("writer", files)
		if negative == "wrong-role" {
			badWriter["certificate_role"] = "reader"
		} else {
			badWriter["server_ca_pem"] = string(files["client-ca.pem"])
		}
		bad, err := o.createIntegration(negative, badWriter, reader)
		if err != nil {
			return err
		}
		before, err := o.audit()
		if err != nil || o.save(negative+"-audit-before.jsonl", before) != nil {
			return errFixture
		}
		denied, err := o.writeProbe(negative+"-write", bad)
		if err != nil || denied.Write.Attempted || denied.Write.Succeeded || denied.Write.Failure == nil || denied.State != "interrupted" || denied.Write.Failure.Stage != "prepare" || negative == "wrong-role" && (denied.Write.Failure.Code != "http_status" || denied.Write.Failure.HTTPStatus != 400) || negative == "wrong-ca" && (denied.Write.Failure.Code != "transport" || denied.Write.Failure.HTTPStatus != 0) {
			return errFixture
		}
		after, err := o.audit()
		if err != nil || !bytes.HasPrefix(after, before) || o.save(negative+"-audit-after.jsonl", after) != nil {
			return errFixture
		}
		appendix := after[len(before):]
		if negative == "wrong-ca" && len(appendix) != 0 || negative == "wrong-role" && (len(appendix) == 0 || bytes.Contains(appendix, []byte(`"path":"kv/`))) {
			return errFixture
		}
	}
	address, _ := url.Parse(applicationURL)
	profile.Cookies = o.client.Jar.Cookies(address)
	if len(profile.Cookies) != 1 || profile.Cookies[0].Name != "routex_session" {
		return errFixture
	}
	root, err := o.store("initial-root")
	if err != nil || root.Process.ID == nil || !root.Process.Verified {
		return errFixture
	}
	profile.OriginalProcess = *root.Process.ID
	return o.save("private-profile.json", profile)
}

func (o *fixtureOwner) loadProfile() (fixtureProfile, error) {
	var profile fixtureProfile
	name := "before-private-profile.json"
	if o.phase == "after" {
		name = "rotate-private-profile.json"
	}
	data, err := privateInput(filepath.Join(o.dir, name), 32768)
	if err != nil || json.Unmarshal(data, &profile) != nil || profile.Run != BuildRunID || profile.Source != BuildSource || profile.Manifest != BuildSourceManifest || !integrationPattern.MatchString(profile.Integration) || !revisionPattern.MatchString(profile.Revision) || !runIDPattern.MatchString(profile.Probe) || len(profile.CSRF) != 64 || o.restoreCookies(profile.Cookies) != nil {
		return profile, errFixture
	}
	o.csrf = profile.CSRF
	return profile, nil
}

func (o *fixtureOwner) after() error {
	if o.ready() != nil {
		return errFixture
	}
	profile, err := o.loadProfile()
	if err != nil {
		return err
	}
	var session struct {
		User struct{ ID string } `json:"user"`
		CSRF string              `json:"csrf_token"`
	}
	if o.api("original-session", "GET", "/auth/session", "", nil, 200, &session) != nil || session.User.ID != profile.Actor || session.CSRF != profile.CSRF {
		return errFixture
	}
	var root rootView
	for index := 0; ; index++ {
		root, err = o.store("restarted-root-" + strconv.Itoa(index))
		if err != nil || index >= 100 {
			return errFixture
		}
		if root.Process.ID != nil && root.Process.Verified && *root.Process.ID != profile.OriginalProcess {
			break
		}
		select {
		case <-o.ctx.Done():
			return errFixture
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !profile.Rotated || !retiredWithoutOldMaterial(root) || root.Rotation.ID != profile.Rotation {
		return errFixture
	}
	view, err := o.integration("saved-integration", profile.Integration)
	if err != nil || view.RevisionID != profile.Revision {
		return errFixture
	}
	baseline, err := o.audit()
	if err != nil || o.save("replay-audit-before.jsonl", baseline) != nil {
		return errFixture
	}
	var replay probeView
	path := "/admin/secrets/integrations/" + profile.Integration + "/probes/" + profile.Probe + "/read"
	if o.api("historical-read-replay", "POST", path, profile.ReadReview, profile.ReadInput, 200, &replay) != nil || !readSucceededWithDeniedCleanup(replay) {
		return errFixture
	}
	current, err := o.audit()
	if err != nil || !bytes.Equal(current, baseline) || o.save("replay-audit-after.jsonl", current) != nil {
		return errFixture
	}
	var probe probeView
	if o.api("current-probe", "GET", strings.TrimSuffix(path, "/read"), "", nil, 200, &probe) != nil || probe.ID != profile.Probe || probe.RevisionID != profile.Revision || !vaultReviewPattern.MatchString(probe.ReviewETag) || o.readProbe("fresh-saved-read", &profile, probe) != nil {
		return errFixture
	}
	return o.save("observations.json", map[string]any{"run": BuildRunID, "source": BuildSource, "source_manifest": BuildSourceManifest, "original_session_preserved": true, "integration_id": profile.Integration, "revision_id": profile.Revision, "probe_id": profile.Probe, "fresh_native_saved_read": true, "root_rotation_and_old_material_removal": profile.Rotated, "cleanup_acknowledged": false, "application_adoption": false, "native_AI_completion": false})
}
