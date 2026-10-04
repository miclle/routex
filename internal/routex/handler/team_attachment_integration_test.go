package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/objectstore"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

type teamAttachmentNativeContext struct{}

const teamAttachmentFixtureID = "tem_media_fixture"

type teamAttachmentFixture struct {
	t                                *testing.T
	db                               *gorm.DB
	ctx                              context.Context
	svc                              *service.Service
	router                           *fox.Engine
	secrets                          *secretstore.Store
	storage                          *storageFixture
	spool                            string
	actor, other                     *service.MemberRecord
	adminID                          string
	cookie, otherCookie, adminCookie *http.Cookie
	csrf, otherCSRF, adminCSRF       string
	membership                       string
	dispatches                       atomic.Int64
	mode                             atomic.Value
	putMu                            sync.Mutex
	putStarted, putContinue          chan struct{}
	instances                        []*service.Service
	upstreamMu                       sync.Mutex
	upstreamStarted                  chan struct{}
	upstreamContinue                 chan struct{}
	knownTokens, knownMoney          int64
	governanceReads                  atomic.Int64
}

// This fixture is invoked only by the root-owned actual PostgreSQL/MySQL harness.
func testTeamAttachmentLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	stage := func(name string, check func()) {
		t.Helper()
		t.Logf("Team attachment stage: %s", name)
		check()
	}
	t.Log("Team attachment stage: bootstrap")
	f := newTeamAttachmentFixture(t, db)
	stage("capability discovery", f.checkDiscovery)
	var image, pdf service.AttachmentView
	stage("initial image and PDF uploads", func() {
		image = f.upload("image.png", teamAttachmentPNG(), f.cookie, f.csrf, nil)
		pdf = f.upload("document.pdf", []byte("%PDF-1.7\ncontrolled text\n%%EOF"), f.cookie, f.csrf, nil)
	})
	stage("creator-private scope and Session renewal", func() { f.checkPrivateScope(image) })
	stage("four native protocols and durable reservation receipts", func() { f.checkNativeMedia(image, pdf) })
	stage("Personal and Project Key isolation", func() { f.checkKeyScopeIsolation(image) })
	stage("finite-policy and request bounds before storage GET", func() { f.checkFinitePreflight(image) })
	stage("atomic aggregate/member concurrent admission", func() { f.checkConcurrentAdmission(image) })
	stage("corrupt bytes and immutable expiry", func() { f.checkCorruptAndExpired(image) })
	stage("current Team, Model, Session and User authority", func() { f.checkCurrentAuthority(image) })
	stage("membership removal during storage GET and rejoin", func() { f.checkReadRevocation(image) })
	stage("membership removal during storage PUT", f.checkUploadRevocation)
	stage("restart, expiry cleanup and retained usage", f.checkRestartCleanup)
	stage("secondary-Team lifecycle and actor offboarding", f.checkTerminalAuthority)
}

func newTeamAttachmentFixture(t *testing.T, db *gorm.DB) *teamAttachmentFixture {
	t.Helper()
	ctx := context.Background()
	f := &teamAttachmentFixture{t: t, db: db, ctx: ctx, membership: "tmm_media_actor"}
	f.mode.Store("completed")
	const observer = "test_team_media_governance_reads"
	observe := func(tx *gorm.DB) {
		if tx.Statement.Context.Value(teamAttachmentNativeContext{}) != true {
			return
		}
		switch tx.Statement.Table {
		case "users", "teams", "team_memberships":
			f.governanceReads.Add(1)
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register(observer, observe); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register(observer, observe); err != nil {
		t.Fatal(err)
	}
	// Later cleanup stops every Service before mutating this shared registry.
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(observer); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Row().Remove(observer); err != nil {
			t.Error(err)
		}
	})
	var err error
	f.secrets, err = secretstore.New(bytes.Repeat([]byte{149}, 32))
	if err != nil {
		t.Fatal(err)
	}
	storage, stored := newStorageFixture(t)
	f.storage = stored
	wrapped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.putMu.Lock()
		started, proceed := f.putStarted, f.putContinue
		if r.Method == http.MethodPut && started != nil {
			f.putStarted = nil
		}
		f.putMu.Unlock()
		if r.Method == http.MethodPut && started != nil {
			close(started)
			select {
			case <-proceed:
			case <-r.Context().Done():
				return
			}
		}
		storage.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(wrapped.Close)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.dispatches.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("private Team Session reached upstream")
		}
		protocol := entity.ProtocolOpenAIChat
		switch r.URL.Path {
		case "/v1/responses":
			protocol = entity.ProtocolOpenAIResponses
		case "/v1/messages":
			protocol = entity.ProtocolAnthropicMessages
		case "/v1beta/models/native-gemini:generateContent", "/v1beta/models/native-gemini:streamGenerateContent":
			protocol = entity.ProtocolGeminiGenerateContent
		case "/v1/chat/completions":
		default:
			t.Errorf("unexpected media endpoint %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if bytes.Contains(raw, []byte("routex://attachments/")) {
			t.Error("unresolved private attachment reached upstream")
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(raw, &payload) != nil {
			t.Error("native media payload is invalid")
			w.WriteHeader(400)
			return
		}
		stream := string(payload["stream"]) == "true" || strings.HasSuffix(r.URL.Path, ":streamGenerateContent")
		f.upstreamMu.Lock()
		started, proceed := f.upstreamStarted, f.upstreamContinue
		f.upstreamMu.Unlock()
		if started != nil {
			select {
			case started <- struct{}{}:
			case <-r.Context().Done():
				return
			}
			select {
			case <-proceed:
			case <-r.Context().Done():
				return
			}
		}
		contentType, response := teamNativeProtocolResponse(protocol, f.mode.Load().(string), stream)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(func() {
		for _, svc := range f.instances {
			svc.StopRuntime()
			if err := svc.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
	})
	f.svc = f.newService()
	f.router = fox.New()
	New(f.svc).RegisterRoutes(f.router)
	setup := identityRequest(f.router, "POST", "/api/v1/setup", `{"email":"team-media-admin@example.invalid","password":"test-only-media-password","name":"Team media administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	f.adminID, f.adminCookie, f.adminCSRF = admin.User.ID, cookie, admin.CSRFToken
	f.actor, f.cookie, f.csrf = createSystemStatusMember(t, f.svc, f.router, f.adminID, "team-media-actor", nil)
	f.other, f.otherCookie, f.otherCSRF = createSystemStatusMember(t, f.svc, f.router, f.adminID, "team-media-other", []string{"teams.read_all", "teams.write", "teams.models.write"})
	now := time.Now().UTC()
	f.create(&entity.Provider{ID: "prv_team_media", Name: "Team media provider"},
		&entity.Team{ID: teamAttachmentFixtureID, Name: "Attachment Team", Status: entity.ResourceActive, CreatedAt: now},
		&entity.TeamMembership{ID: "tmm_media_owner", TeamID: teamAttachmentFixtureID, UserID: f.adminID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: f.membership, TeamID: teamAttachmentFixtureID, UserID: f.actor.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_media_other", TeamID: teamAttachmentFixtureID, UserID: f.other.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.Team{ID: "tem_media_second", Name: "Other attachment Team", Status: entity.ResourceActive, CreatedAt: now},
		&entity.TeamMembership{ID: "tmm_media_second", TeamID: "tem_media_second", UserID: f.actor.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		suffix := teamNativeSuffix(protocol)
		connection, credential, pmd, model := "con_tmedia_"+suffix, "crd_tmedia_"+suffix, "pmd_tmedia_"+suffix, "mdl_tmedia_"+suffix
		cipher, err := f.secrets.Seal(credential, "native-team-media-upstream")
		if err != nil {
			t.Fatal(err)
		}
		base := upstream.URL + "/v1"
		if protocol == entity.ProtocolGeminiGenerateContent {
			base = upstream.URL + "/v1beta"
		}
		f.create(&entity.ProviderConnection{ID: connection, ProviderID: "prv_team_media", Name: suffix, Protocol: protocol, BaseURL: base},
			&entity.ProviderCredential{ID: credential, ConnectionID: connection, Name: "Verified media", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
			&entity.ProviderModel{ID: pmd, ConnectionID: connection, UpstreamName: "native-" + suffix, SupportsImageInput: true, SupportsPDFInput: true},
			&entity.CredentialModelAccess{CredentialID: credential, ProviderModelID: pmd},
			&entity.Model{ID: model, Status: entity.ResourceActive, CreatedAt: now},
			&entity.ModelName{Name: "team-native-" + suffix, ModelID: model, CurrentModelID: &model},
			&entity.ModelProviderBinding{ID: "mpb_tmedia_" + suffix, ModelID: model, ProviderModelID: pmd, Weight: 100},
			&entity.TeamModelGrant{TeamID: teamAttachmentFixtureID, ModelID: model},
			&entity.ModelPrice{ID: "prc_tmedia_" + suffix, ProviderModelID: pmd},
			&entity.ReservationBound{ProviderModelID: pmd, Protocol: protocol, MaxInputTokens: 4, MaxOutputTokens: 1, Evidence: "Controlled media maximum", ETag: "bnd_tmedia_" + suffix, Reason: "Team media acceptance", UpdatedAt: now})
		for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite, pricing.ImageInput, pricing.PDFInput} {
			unit, amount := pricing.Unit, "0"
			if metric == pricing.ImageInput {
				unit, amount = pricing.ImageUnit, "2"
			}
			if metric == pricing.PDFInput {
				unit = pricing.PDFUnit
			}
			f.create(&entity.PriceRate{ID: fmt.Sprintf("rate_tmedia_%s_%d", suffix, i), ModelPriceID: "prc_tmedia_" + suffix, Metric: metric, Tier: pricing.Base, Unit: unit, Currency: "USD", Amount: amount, Enabled: true})
		}
	}
	storageAuth, _ := json.Marshal(map[string]string{"AccessKey": "test-only-access", "SecretKey": "test-only-secret"})
	cipher, err := f.secrets.Seal("storage:str_team_media:media-generation", string(storageAuth))
	if err != nil {
		t.Fatal(err)
	}
	f.create(&entity.StorageRevision{ID: "str_team_media", Endpoint: wrapped.URL, Region: "us-east-1", Bucket: "routex-test", SecretGeneration: "media-generation", AuthCiphertext: cipher, VerifiedAt: &now, CreatedBy: f.adminID, CreatedAt: now})
	if err := db.Model(&entity.StorageSetting{}).Where("id = ?", 1).Updates(map[string]any{"enabled": true, "active_revision_id": "str_team_media", "e_tag": "storage_team_media"}).Error; err != nil {
		t.Fatal(err)
	}
	f.spool = filepath.Join(t.TempDir(), "team-media.db")
	journal, err := eventqueue.Open(f.spool, 4096, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.EnableQuota("UTC", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartRuntime(ctx); err != nil {
		t.Fatalf("start Team media runtime: %v (runtime=%s)", err, f.svc.RuntimeStatus().ErrorCode)
	}
	if err := f.svc.StartCallRecorder(ctx, f.spool); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	ownBefore, err := f.svc.GetResourceLimit(ctx, f.adminID, service.LimitTarget{Kind: "user", ID: f.actor.User.ID})
	if err != nil {
		t.Fatalf("read initial Personal isolation policy: %v", err)
	}
	if _, err := f.svc.SetResourceLimit(ctx, f.adminID, service.LimitTarget{Kind: "user", ID: f.actor.User.ID}, ownBefore.ETag, service.LimitInput{Policy: limits.Policy{TokensMonth: &zero, RPM: &zero}, Reason: "Prove Team media isolation"}); err != nil {
		t.Fatalf("set Personal isolation policy: %v (runtime=%s)", err, f.svc.RuntimeStatus().ErrorCode)
	}
	f.policy("", `{"tokens_month":1000,"money_month":"1000","currency":"USD","rpm":1000,"tpm":1000,"concurrency":2,"reason":"Aggregate media acceptance"}`)
	f.policy(f.actor.User.ID, `{"tokens_month":1000,"money_month":"1000","currency":"USD","rpm":1000,"tpm":1000,"concurrency":2,"reason":"Member media acceptance"}`)
	return f
}

func (f *teamAttachmentFixture) newService() *service.Service {
	f.t.Helper()
	svc, err := service.New(f.ctx, f.db, service.WithCredentialStorage(f.secrets), service.WithUpstreamPolicy(true), service.WithStoragePolicy(true))
	if err != nil {
		f.t.Fatal(err)
	}
	f.instances = append(f.instances, svc)
	return svc
}
func (f *teamAttachmentFixture) create(rows ...any) {
	f.t.Helper()
	for _, row := range rows {
		if err := f.db.Create(row).Error; err != nil {
			f.t.Fatal(err)
		}
	}
}
func (f *teamAttachmentFixture) policy(userID, raw string) *service.LimitRecord {
	f.t.Helper()
	before, err := f.svc.GetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, userID)
	if err != nil {
		f.t.Fatalf("read Team media policy user=%q: %v (runtime=%s)", userID, err, f.svc.RuntimeStatus().ErrorCode)
	}
	var input service.TeamLimitInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		f.t.Fatal(err)
	}
	after, err := f.svc.SetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, userID, before.ETag, input)
	if err != nil || !after.Enforced {
		f.t.Fatalf("set Team media policy user=%q: %v record=%+v (runtime=%s)", userID, err, after, f.svc.RuntimeStatus().ErrorCode)
	}
	return after
}
func (f *teamAttachmentFixture) request(cookie *http.Cookie, csrf, method, path string, body io.Reader, contentType string, change func(*http.Request)) *httptest.ResponseRecorder {
	f.t.Helper()
	if err := f.svc.RefreshRuntime(f.ctx); err != nil {
		f.t.Fatalf("refresh before %s %s: %v (runtime=%s)", method, path, err, f.svc.RuntimeStatus().ErrorCode)
	}
	bounded, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	native := strings.HasPrefix(path, "/api/v1/teams/") && (strings.Contains(path, "/chat/completions") || strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/messages") || strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent"))
	if native {
		bounded = context.WithValue(bounded, teamAttachmentNativeContext{}, true)
	}
	beforeGovernance := f.governanceReads.Load()
	req := httptest.NewRequestWithContext(bounded, method, "http://routex.test"+path, body)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	req.Header.Set("Origin", "http://routex.test")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("anthropic-version", "2023-06-01")
	if change != nil {
		change(req)
	}
	res := httptest.NewRecorder()
	f.router.ServeHTTP(res, req)
	if native && f.governanceReads.Load() != beforeGovernance {
		f.t.Fatalf("native Team media queried governance User/Team/membership rows: path=%s count=%d", path, f.governanceReads.Load()-beforeGovernance)
	}
	return res
}
func (f *teamAttachmentFixture) uploadResponse(name string, data []byte, cookie *http.Cookie, csrf string, change func(*http.Request)) *httptest.ResponseRecorder {
	f.t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		f.t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		f.t.Fatal(err)
	}
	return f.request(cookie, csrf, "POST", "/api/v1/teams/"+teamAttachmentFixtureID+"/attachments", &body, writer.FormDataContentType(), change)
}
func (f *teamAttachmentFixture) upload(name string, data []byte, cookie *http.Cookie, csrf string, change func(*http.Request)) service.AttachmentView {
	f.t.Helper()
	response := f.uploadResponse(name, data, cookie, csrf, change)
	if response.Code != http.StatusCreated {
		f.t.Fatalf("Team attachment upload name=%q size=%d: status=%d want=%d response=%s", name, len(data), response.Code, http.StatusCreated, response.Body.String())
	}
	value := decodeCatalogResponse[service.AttachmentView](f.t, response, 201)
	if value.State != "ready" {
		f.t.Fatal("Team upload was not verified", value)
	}
	return value
}
func (f *teamAttachmentFixture) counts() (int, int64) {
	f.storage.mu.Lock()
	n := f.storage.getCalls
	f.storage.mu.Unlock()
	return n, f.dispatches.Load()
}
func (f *teamAttachmentFixture) zeroWork(before int, dispatches int64, res *httptest.ResponseRecorder) {
	f.t.Helper()
	if res.Code < 400 || res.Code >= 600 {
		f.t.Fatal("unsafe Team media was accepted", res.Code, res.Body.String())
	}
	reads, after := f.counts()
	if reads != before || after != dispatches {
		f.t.Fatalf("rejected media performed work: GET %d→%d upstream %d→%d status=%d", before, reads, dispatches, after, res.Code)
	}
}
func teamAttachmentPNG() []byte {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err)
	}
	return data.Bytes()
}

func TestTeamAttachmentFixtureInputsAreValid(t *testing.T) {
	for _, input := range []struct {
		name, mime string
		data       []byte
	}{{"image.png", "image/png", teamAttachmentPNG()}, {"document.pdf", "application/pdf", []byte("%PDF-1.7\ncontrolled text\n%%EOF")}} {
		mime, err := objectstore.ValidateAttachment(input.name, input.data)
		if err != nil || mime != input.mime {
			t.Fatalf("invalid positive media fixture %s: mime=%q err=%v", input.name, mime, err)
		}
	}
}

func TestTeamAttachmentMessagesMediaContract(t *testing.T) {
	var payload struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Source struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	// Native Messages requires explicit media_type before finite media pricing;
	// a valid private reference alone is insufficient for bounded admission.
	raw := teamAttachmentNativeBody(entity.ProtocolAnthropicMessages, false, []string{"image-id", "pdf:pdf-id"})
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || len(payload.Messages) != 1 || len(payload.Messages[0].Content) != 2 {
		t.Fatal("native Messages fixture lost its media occurrences", err, raw)
	}
	for index, expected := range []struct{ kind, mime, reference string }{
		{"image", "image/png", "routex://attachments/image-id"},
		{"document", "application/pdf", "routex://attachments/pdf-id"},
	} {
		block := payload.Messages[0].Content[index]
		if block.Type != expected.kind || block.Source.Type != "base64" || block.Source.MediaType != expected.mime || block.Source.Data != expected.reference {
			t.Fatalf("native Messages media contract occurrence=%d: %+v", index, block)
		}
	}
}

func teamAttachmentDispositionMatches(header, filename string) bool {
	disposition, parameters, err := mime.ParseMediaType(header)
	return err == nil && disposition == "attachment" && parameters["filename"] == filename && len(parameters) == 1
}

func TestTeamAttachmentDownloadDisposition(t *testing.T) {
	for _, input := range []struct {
		header, filename string
		valid            bool
	}{
		{`attachment; filename=image.png`, "image.png", true},
		{`attachment; filename="image.png"`, "image.png", true},
		{mime.FormatMediaType("attachment", map[string]string{"filename": "private image.png"}), "private image.png", true},
		{mime.FormatMediaType("attachment", map[string]string{"filename": "图像.png"}), "图像.png", true},
		{`inline; filename=image.png`, "image.png", false},
		{`attachment; filename=Image.png`, "image.png", false},
		{`attachment; filename=other.png`, "image.png", false},
		{`attachment`, "image.png", false},
		{`attachment; filename=image.png; filename=other.png`, "image.png", false},
	} {
		if got := teamAttachmentDispositionMatches(input.header, input.filename); got != input.valid {
			t.Fatalf("download disposition %q filename=%q: valid=%v want=%v", input.header, input.filename, got, input.valid)
		}
	}
}

func (f *teamAttachmentFixture) checkDiscovery() {
	f.t.Helper()
	res := f.request(f.cookie, "", "GET", "/api/v1/teams/"+teamAttachmentFixtureID+"/inference-models", nil, "application/json", nil)
	expectStatus(f.t, res, 200)
	var listed struct {
		Data []service.GatewayModel `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil {
		f.t.Fatal(err)
	}
	if len(listed.Data) != 4 {
		f.t.Fatal("Team media discovery lost protocol-only models", res.Body.String())
	}
	for _, model := range listed.Data {
		var context struct {
			TeamID       string `json:"attachment_team_id"`
			MembershipID string `json:"attachment_membership_id"`
		}
		raw, _ := json.Marshal(model)
		if err := json.Unmarshal(raw, &context); err != nil {
			f.t.Fatal(err)
		}
		if model.AttachmentScope != "team" || model.PersonalAttachments || context.TeamID != teamAttachmentFixtureID || context.MembershipID != f.membership || len(model.Protocols) != 1 || !slices.Equal(model.InputCapabilities[model.Protocols[0]], []string{"image", "pdf"}) {
			f.t.Fatal("Team media discovery borrowed an owner or invented capabilities", model, context)
		}
	}
	// The second positive-weight ready route narrows image support for Chat.
	f.create(&entity.ProviderModel{ID: "pmd_tmedia_narrow", ConnectionID: "con_tmedia_chat", UpstreamName: "native-chat-narrow", SupportsPDFInput: true},
		&entity.CredentialModelAccess{CredentialID: "crd_tmedia_chat", ProviderModelID: "pmd_tmedia_narrow"})
	// Both ready routes must retain the valid native protocol total of 100.
	// Seed the complete configuration atomically rather than publishing 200.
	if err := f.db.Transaction(func(tx *gorm.DB) error {
		changed := tx.Model(&entity.ModelProviderBinding{}).Where("id = ?", "mpb_tmedia_chat").Update("weight", 50)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return fmt.Errorf("expected exactly one original Team Chat binding, found %d", changed.RowsAffected)
		}
		return tx.Create(&entity.ModelProviderBinding{ID: "mpb_tmedia_narrow", ModelID: "mdl_tmedia_chat", ProviderModelID: "pmd_tmedia_narrow", Weight: 50}).Error
	}); err != nil {
		f.t.Fatalf("seed complete capability intersection routing: %v", err)
	}
	res = f.request(f.cookie, "", "GET", "/api/v1/teams/"+teamAttachmentFixtureID+"/inference-models", nil, "application/json", nil)
	expectStatus(f.t, res, 200)
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil {
		f.t.Fatal(err)
	}
	for _, model := range listed.Data {
		if model.ID == "team-native-chat" && !slices.Equal(model.InputCapabilities[entity.ProtocolOpenAIChat], []string{"pdf"}) {
			f.t.Fatal("discovery did not intersect every ready route", model)
		}
	}
	if err := f.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&entity.ModelProviderBinding{}, "id = ?", "mpb_tmedia_narrow").Error; err != nil {
			return err
		}
		changed := tx.Model(&entity.ModelProviderBinding{}).Where("id = ?", "mpb_tmedia_chat").Update("weight", 100)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return fmt.Errorf("expected exactly one restored Team Chat binding, found %d", changed.RowsAffected)
		}
		return nil
	}); err != nil {
		f.t.Fatalf("restore complete original Team Chat routing: %v", err)
	}
	if err := f.svc.RefreshRuntime(f.ctx); err != nil {
		f.t.Fatalf("refresh restored capability routing: %v (runtime=%s)", err, f.svc.RuntimeStatus().ErrorCode)
	}
}

func (f *teamAttachmentFixture) checkPrivateScope(object service.AttachmentView) {
	f.t.Helper()
	base := "/api/v1/teams/" + teamAttachmentFixtureID + "/attachments/" + object.ID
	var proof struct {
		TeamID       string    `json:"attachment_team_id"`
		MembershipID string    `json:"attachment_membership_id"`
		Creator      string    `json:"creator_user_id"`
		Expires      time.Time `json:"expires_at"`
	}
	own := f.request(f.cookie, "", "GET", base, nil, "application/json", nil)
	expectStatus(f.t, own, 200)
	if err := json.Unmarshal(own.Body.Bytes(), &proof); err != nil {
		f.t.Fatal(err)
	}
	if proof.TeamID != teamAttachmentFixtureID || proof.MembershipID != f.membership || proof.Creator != f.actor.User.ID || !proof.Expires.Equal(object.CreatedAt.Add(time.Hour)) {
		f.t.Fatal("upload did not retain immutable Team/creator/deadline proof", proof, object)
	}
	before, dispatches := f.counts()
	for _, cookie := range []*http.Cookie{f.otherCookie, f.adminCookie} {
		for _, path := range []string{base, base + "/content"} {
			f.zeroWork(before, dispatches, f.request(cookie, "", "GET", path, nil, "application/json", nil))
		}
	}
	for _, path := range []string{
		strings.Replace(base, teamAttachmentFixtureID, "tem_media_second", 1),
		strings.Replace(base, teamAttachmentFixtureID, strings.ToUpper(teamAttachmentFixtureID), 1),
		strings.Replace(base, object.ID, strings.ToUpper(object.ID), 1),
		"/api/v1/attachments/" + object.ID + "/content",
		base + "?key=foreign",
	} {
		f.zeroWork(before, dispatches, f.request(f.cookie, "", "GET", path, nil, "application/json", nil))
	}
	f.zeroWork(before, dispatches, f.request(f.otherCookie, f.otherCSRF, "DELETE", base, nil, "application/json", nil))
	f.zeroWork(before, dispatches, f.request(f.adminCookie, f.adminCSRF, "DELETE", base, nil, "application/json", nil))
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer rx_foreign") },
		func(r *http.Request) { r.Header.Set("x-api-key", "foreign") },
		func(r *http.Request) { r.Header.Set("x-goog-api-key", "foreign") },
		func(r *http.Request) { r.Header.Set("Origin", "https://foreign.invalid") },
	} {
		f.zeroWork(before, dispatches, f.request(f.cookie, f.csrf, "GET", base+"/content", nil, "application/json", change))
	}
	f.zeroWork(before, dispatches, f.uploadResponse("no-csrf.pdf", []byte("%PDF-1.7\n%%EOF"), f.cookie, "", nil))
	f.zeroWork(before, dispatches, f.uploadResponse("invalid.png", []byte("not media"), f.cookie, f.csrf, nil))
	content := f.request(f.cookie, "", "GET", base+"/content", nil, "application/json", nil)
	expectStatus(f.t, content, 200)
	if !bytes.Equal(content.Body.Bytes(), teamAttachmentPNG()) || content.Header().Get("Cache-Control") != "private, no-store" || content.Header().Get("X-Content-Type-Options") != "nosniff" || content.Header().Get("Content-Security-Policy") != "sandbox" || content.Header().Get("Content-Type") != "image/png" || !teamAttachmentDispositionMatches(content.Header().Get("Content-Disposition"), object.Name) {
		f.t.Fatal("Team download lost content/privacy headers", content.Header())
	}
	login := identityRequest(f.router, "POST", "/api/v1/auth/login", `{"email":"system-team-media-actor@example.invalid","password":"test-only-system-password"}`, nil, "")
	expectStatus(f.t, login, 200)
	renewed, cookie := readIdentity(f.t, login)
	f.cookie, f.csrf = cookie, renewed.CSRFToken
	expectStatus(f.t, f.request(f.cookie, "", "GET", base, nil, "application/json", nil), 200)
	// Mixed references must preauthorize all metadata before the first GET.
	for _, scope := range []struct{ kind, owner, creator, membership string }{
		{"user", f.actor.User.ID, "", ""},
		{"project", "prj_media_foreign", "", ""},
		{"team", teamAttachmentFixtureID, f.other.User.ID, "tmm_media_other"},
		{"team", "tem_media_second", f.actor.User.ID, "tmm_media_second"},
		{"team", teamAttachmentFixtureID, strings.ToUpper(f.actor.User.ID), f.membership},
		{"team", teamAttachmentFixtureID, f.actor.User.ID, strings.ToUpper(f.membership)},
	} {
		foreign := f.seedObject(scope.kind, scope.owner, scope.creator, scope.membership, teamAttachmentPNG())
		before, dispatches = f.counts()
		f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{object.ID, foreign}, nil))
	}
	before, dispatches = f.counts()
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{strings.ToUpper(object.ID)}, nil))
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{object.ID}, func(r *http.Request) { r.Header.Set("Authorization", "Bearer rx_foreign") }))
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		func(r *http.Request) { r.URL.RawQuery = "key=foreign" },
		func(r *http.Request) { r.Header.Set("X-Workspace-ID", "foreign") },
	} {
		f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{object.ID}, change))
	}
	aliasBody := strings.Replace(teamAttachmentNativeBody(entity.ProtocolOpenAIChat, false, []string{object.ID}), "team-native-chat", "TEAM-NATIVE-CHAT", 1)
	f.zeroWork(before, dispatches, f.request(f.cookie, f.csrf, "POST", "/api/v1/teams/"+teamAttachmentFixtureID+"/chat/completions", strings.NewReader(aliasBody), "application/json", nil))

}

func (f *teamAttachmentFixture) seedObject(kind, owner, creator, membership string, data []byte) string {
	f.t.Helper()
	// Real upload supplies a canonical object ID; cloning only ownership proves
	// resolver isolation without borrowing another user's control-plane API.
	uploaded := f.upload("seed.png", data, f.cookie, f.csrf, nil)
	values := map[string]any{"owner_kind": kind, "owner_id": owner, "creator_user_id": nil, "creator_membership_id": nil, "expires_at": nil}
	if kind == "team" {
		values["creator_user_id"] = creator
		values["creator_membership_id"] = membership
		values["expires_at"] = uploaded.CreatedAt.Add(time.Hour)
	}
	if err := f.db.Table("storage_objects").Where("id = ?", uploaded.ID).Updates(values).Error; err != nil {
		f.t.Fatal(err)
	}
	return uploaded.ID
}

func (f *teamAttachmentFixture) native(protocol string, stream bool, objects []string, change func(*http.Request)) *httptest.ResponseRecorder {
	f.t.Helper()
	body := teamAttachmentNativeBody(protocol, stream, objects)
	return f.request(f.cookie, f.csrf, "POST", "/api/v1/teams/"+teamAttachmentFixtureID+teamNativeProtocolPath(protocol, stream), strings.NewReader(body), "application/json", change)
}

func (f *teamAttachmentFixture) expectNativeStatus(stage, protocol string, stream bool, res *httptest.ResponseRecorder, status int) {
	f.t.Helper()
	if res.Code != status {
		f.t.Fatalf("%s protocol=%s stream=%v: status %d, want %d; response: %s", stage, protocol, stream, res.Code, status, res.Body.String())
	}
}

func teamAttachmentNativeBody(protocol string, stream bool, objects []string) string {
	parts := make([]any, 0, len(objects)+1)
	for _, id := range objects {
		uri := "routex://attachments/" + id
		// The fixture's final occurrence is PDF only when explicitly marked.
		pdf := strings.HasPrefix(id, "pdf:")
		if pdf {
			uri = "routex://attachments/" + strings.TrimPrefix(id, "pdf:")
		}
		var part any
		switch protocol {
		case entity.ProtocolOpenAIChat:
			if pdf {
				part = map[string]any{"type": "file", "file": map[string]any{"file_data": uri}}
			} else {
				part = map[string]any{"type": "image_url", "image_url": map[string]any{"url": uri}}
			}
		case entity.ProtocolOpenAIResponses:
			if pdf {
				part = map[string]any{"type": "input_file", "file_data": uri}
			} else {
				part = map[string]any{"type": "input_image", "image_url": uri}
			}
		case entity.ProtocolAnthropicMessages:
			kind, mediaType := "image", "image/png"
			if pdf {
				kind, mediaType = "document", "application/pdf"
			}
			part = map[string]any{"type": kind, "source": map[string]any{"type": "base64", "media_type": mediaType, "data": uri}}
		default:
			mime := "image/png"
			if pdf {
				mime = "application/pdf"
			}
			part = map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": uri}}
		}
		parts = append(parts, part)
	}
	body := map[string]any{"model": "team-native-" + teamNativeSuffix(protocol), "stream": stream}
	switch protocol {
	case entity.ProtocolOpenAIChat:
		body["messages"] = []any{map[string]any{"role": "user", "content": parts}}
		body["max_completion_tokens"] = 1
		if stream {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
	case entity.ProtocolOpenAIResponses:
		body["input"] = []any{map[string]any{"role": "user", "content": parts}}
		body["max_output_tokens"] = 1
	case entity.ProtocolAnthropicMessages:
		body["messages"] = []any{map[string]any{"role": "user", "content": parts}}
		body["max_tokens"] = 1
	default:
		body = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": map[string]any{"maxOutputTokens": 1, "candidateCount": 1}}
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func (f *teamAttachmentFixture) checkNativeMedia(image, pdf service.AttachmentView) {
	f.t.Helper()
	var ids []string
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		for _, stream := range []bool{false, true} {
			f.t.Logf("Team attachment native media: protocol=%s stream=%v", protocol, stream)
			before, dispatches := f.counts()
			res := f.native(protocol, stream, []string{image.ID, image.ID, "pdf:" + pdf.ID}, nil)
			f.expectNativeStatus("completed media", protocol, stream, res, 200)
			reads, after := f.counts()
			if reads != before+2 || after != dispatches+1 {
				f.t.Fatalf("native media was reread/readmitted: protocol=%s stream=%v GET=%d upstream=%d", protocol, stream, reads-before, after-dispatches)
			}
			if err := f.svc.FlushCallRecorder(f.ctx); err != nil {
				f.t.Fatal(err)
			}
			requestID := res.Header().Get("X-Request-ID")
			ids = append(ids, requestID)
			var call entity.CallRecord
			if err := f.db.Take(&call, "request_id = ?", requestID).Error; err != nil {
				f.t.Fatal(err)
			}
			var attempts []entity.CallAttempt
			if err := f.db.Where("request_id = ?", requestID).Find(&attempts).Error; err != nil {
				f.t.Fatal(err)
			}
			rawFact, err := json.Marshal(call)
			if err != nil {
				f.t.Fatal(err)
			}
			for _, private := range []string{image.ID, pdf.ID, image.Name, pdf.Name} {
				if bytes.Contains(rawFact, []byte(private)) {
					f.t.Fatal("immutable call retained attachment content/identifiers", private)
				}
			}
			if call.UserID != f.actor.User.ID || call.TeamID != teamAttachmentFixtureID || call.TeamMembershipID != f.membership || call.KeyID != "" || call.ProjectID != "" || call.Protocol != protocol || !equalOptionalInt64(call.ImageInputs, int64Pointer(2)) || !equalOptionalInt64(call.PDFInputs, int64Pointer(1)) || !equalOptionalString(call.ChargeAmount, stringPointer("4")) || !equalOptionalString(call.ChargeCurrency, stringPointer("USD")) || len(attempts) != 1 || attempts[0].NativeCompletionEvidence != "completed" {
				f.t.Fatalf("native media call lost exact attribution/pricing/finality: %+v %+v", call, attempts)
			}
		}
	}
	for _, user := range []string{"", f.actor.User.ID} {
		usage, err := f.svc.GetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, user)
		if err != nil {
			f.t.Fatal(err)
		}
		if usage.QuotaUsage == nil || usage.QuotaUsage.Month == nil || usage.QuotaUsage.Month.TokensUsed != 40 || usage.QuotaUsage.Month.TokensHeld != 0 || usage.QuotaUsage.Month.MoneyUsed["USD"] != "32" || usage.QuotaUsage.Month.MoneyUnknown != 0 {
			f.t.Fatal("Team/stable-pair media settlement was not conjunctive/exact", user, usage.QuotaUsage)
		}
	}
	f.knownTokens, f.knownMoney = 40, 32
	// A native success lacking authoritative usage retains a conservative hold.
	f.mode.Store("unknown_usage")
	unknown := f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil)
	f.expectNativeStatus("unknown usage media", entity.ProtocolOpenAIChat, false, unknown, 200)
	unknownID := unknown.Header().Get("X-Request-ID")
	f.mode.Store("completed")
	if err := f.svc.FlushCallRecorder(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	expectedAccounts := map[string]bool{}
	for _, user := range []string{"", f.actor.User.ID} {
		limit, err := f.svc.GetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, user)
		if err != nil {
			f.t.Fatal(err)
		}
		expectedAccounts[limit.AccountID] = true
	}
	f.svc.StopRuntime()
	if err := f.svc.StopCallRecorder(); err != nil {
		f.t.Fatal(err)
	}
	journal, err := eventqueue.Open(f.spool, 4096, 64<<10)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, requestID := range ids {
		receipt, err := journal.QuotaReceipt(requestID)
		if err != nil {
			f.t.Fatal(err)
		}
		seenAccounts := map[string]bool{}
		for _, account := range receipt.Accounts {
			if !expectedAccounts[account.Account] || seenAccounts[account.Account] {
				f.t.Fatal("media reservation borrowed or duplicated a quota account", receipt.Accounts)
			}
			seenAccounts[account.Account] = true
		}
		if len(receipt.Accounts) != 2 || receipt.State != "complete" || !equalOptionalInt64(receipt.Bound.Tokens, int64Pointer(5)) || !equalOptionalString(receipt.Bound.Money, stringPointer("4.000000000000000003")) || !equalOptionalInt64(receipt.Actual.Tokens, int64Pointer(5)) || !equalOptionalString(receipt.Actual.Money, stringPointer("4")) {
			raw, _ := json.Marshal(receipt)
			f.t.Fatalf("one logical media reservation did not bind both accounts: %s", raw)
		}
	}
	unknownReceipt, err := journal.QuotaReceipt(unknownID)
	if err != nil {
		f.t.Fatal(err)
	}
	if unknownReceipt.Actual.Tokens != nil || unknownReceipt.Actual.Money != nil || !equalOptionalInt64(unknownReceipt.Bound.Tokens, int64Pointer(5)) || !equalOptionalString(unknownReceipt.Bound.Money, stringPointer("2.000000000000000003")) {
		f.t.Fatal("unknown native work invented usage/released media hold", unknownReceipt)
	}
	if err := journal.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.svc = f.newService()
	f.router = fox.New()
	New(f.svc).RegisterRoutes(f.router)
	if err := f.svc.StartRuntime(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	if err := f.svc.StartCallRecorder(f.ctx, f.spool); err != nil {
		f.t.Fatal(err)
	}
}

func (f *teamAttachmentFixture) checkFinitePreflight(image service.AttachmentView) {
	f.t.Helper()
	for _, kind := range []string{"image-price", "capacity", "member-zero", "aggregate-zero", "member-rate-zero", "member-tpm-zero", "member-money-zero"} {
		var restore func()
		switch kind {
		case "image-price":
			if err := f.db.Model(&entity.PriceRate{}).Where("id = ?", "rate_tmedia_chat_4").Update("enabled", false).Error; err != nil {
				f.t.Fatal(err)
			}
			restore = func() {
				if err := f.db.Model(&entity.PriceRate{}).Where("id = ?", "rate_tmedia_chat_4").Update("enabled", true).Error; err != nil {
					f.t.Fatal(err)
				}
			}
		case "capacity":
			var bound entity.ReservationBound
			if err := f.db.First(&bound, "provider_model_id = ? AND protocol = ?", "pmd_tmedia_chat", entity.ProtocolOpenAIChat).Error; err != nil {
				f.t.Fatal(err)
			}
			if err := f.db.Delete(&bound).Error; err != nil {
				f.t.Fatal(err)
			}
			restore = func() { f.create(&bound) }
		case "member-zero":
			f.policy(f.actor.User.ID, `{"tokens_month":0,"reason":"Zero member media allowance"}`)
			restore = func() { f.policy(f.actor.User.ID, `{"tokens_month":1000,"reason":"Restore owned media fixture"}`) }
		case "aggregate-zero":
			f.policy("", `{"tokens_month":0,"reason":"Zero aggregate media allowance"}`)
			restore = func() { f.policy("", `{"tokens_month":1000,"reason":"Restore owned media fixture"}`) }
		case "member-tpm-zero":
			f.policy(f.actor.User.ID, `{"tpm":0,"reason":"Zero member media token rate"}`)
			restore = func() { f.policy(f.actor.User.ID, `{"tpm":1000,"reason":"Restore owned media fixture"}`) }
		case "member-money-zero":
			f.policy(f.actor.User.ID, `{"money_month":"0","currency":"USD","reason":"Zero member media money allowance"}`)
			restore = func() {
				f.policy(f.actor.User.ID, `{"money_month":"1000","currency":"USD","reason":"Restore owned media fixture"}`)
			}
		case "member-rate-zero":
			f.policy(f.actor.User.ID, `{"rpm":0,"reason":"Zero member media request rate"}`)
			restore = func() { f.policy(f.actor.User.ID, `{"rpm":1000,"reason":"Restore owned media fixture"}`) }
		}
		before, dispatches := f.counts()
		denied := f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil)
		f.zeroWork(before, dispatches, denied)
		restore()
	}
	// Shape/reference bounds reject before any object-store read.
	before, dispatches := f.counts()
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID, image.ID, image.ID, image.ID, image.ID}, nil))
	raw := `{"model":"team-native-chat","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://foreign.invalid/image"}}]}],"max_completion_tokens":1}`
	f.zeroWork(before, dispatches, f.request(f.cookie, f.csrf, "POST", "/api/v1/teams/"+teamAttachmentFixtureID+"/chat/completions", strings.NewReader(raw), "application/json", nil))
}

func (f *teamAttachmentFixture) checkCorruptAndExpired(image service.AttachmentView) {
	f.t.Helper()
	path := "/routex-test/routex/" + image.ID
	f.storage.mu.Lock()
	stored := f.storage.objects[path]
	corrupt := stored
	corrupt.data = []byte("corrupt media")
	f.storage.objects[path] = corrupt
	f.storage.mu.Unlock()
	before, dispatches := f.counts()
	denied := f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil)
	if denied.Code != 503 {
		f.t.Fatal("hash mismatch did not fail closed", denied.Code, denied.Body.String())
	}
	reads, after := f.counts()
	if reads != before+1 || after != dispatches {
		f.t.Fatal("corrupt media was dispatched or repeatedly read")
	}
	f.storage.mu.Lock()
	f.storage.objects[path] = stored
	f.storage.mu.Unlock()
	var saved struct {
		ExpiresAt                          *time.Time
		CreatedAt                          time.Time
		SHA256                             string
		CreatorUserID, CreatorMembershipID *string
	}
	if err := f.db.Table("storage_objects").Where("id = ?", image.ID).Take(&saved).Error; err != nil {
		f.t.Fatal(err)
	}
	digest := sha256.Sum256(teamAttachmentPNG())
	if saved.SHA256 != hex.EncodeToString(digest[:]) || saved.CreatorUserID == nil || *saved.CreatorUserID != f.actor.User.ID || saved.CreatorMembershipID == nil || *saved.CreatorMembershipID != f.membership {
		f.t.Fatal("verified storage proof drifted", saved)
	}
	if err := f.db.Table("storage_objects").Where("id = ?", image.ID).Updates(map[string]any{"created_at": saved.CreatedAt.Add(-2 * time.Hour), "expires_at": saved.CreatedAt.Add(-time.Hour), "next_cleanup_at": time.Now().Add(24 * time.Hour)}).Error; err != nil {
		f.t.Fatal(err)
	}
	before, dispatches = f.counts()
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil))
	expectStatus(f.t, f.request(f.cookie, "", "GET", "/api/v1/teams/"+teamAttachmentFixtureID+"/attachments/"+image.ID, nil, "application/json", nil), 404)
	if err := f.db.Table("storage_objects").Where("id = ?", image.ID).Updates(map[string]any{"created_at": saved.CreatedAt, "expires_at": saved.ExpiresAt, "next_cleanup_at": saved.CreatedAt.Add(time.Hour)}).Error; err != nil {
		f.t.Fatal(err)
	}
}

func (f *teamAttachmentFixture) members(include bool) {
	f.t.Helper()
	members := []service.TeamMemberInput{{UserID: f.adminID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: f.other.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}
	if include {
		members = append(members, service.TeamMemberInput{UserID: f.actor.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	}
	if _, err := f.svc.SetTeamMembers(f.ctx, f.adminID, teamAttachmentFixtureID, members); err != nil {
		f.t.Fatal(err)
	}
	if include {
		var current entity.TeamMembership
		if err := f.db.Take(&current, "team_id = ? AND user_id = ?", teamAttachmentFixtureID, f.actor.User.ID).Error; err != nil {
			f.t.Fatal(err)
		}
		f.membership = current.ID
	}
}

func (f *teamAttachmentFixture) checkReadRevocation(image service.AttachmentView) {
	f.t.Helper()
	captured, err := f.svc.RuntimeAuthenticateTeamSession(f.ctx, f.cookie.Value, teamAttachmentFixtureID)
	if err != nil {
		f.t.Fatal(err)
	}
	previous := f.membership
	started, proceed := make(chan struct{}), make(chan struct{})
	f.storage.mu.Lock()
	f.storage.blockGetStarted = started
	f.storage.blockGetContinue = proceed
	f.storage.mu.Unlock()
	var release sync.Once
	unblock := func() { release.Do(func() { close(proceed) }) }
	defer unblock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		f.t.Fatal("controlled Team storage GET did not begin")
	}
	dispatched := f.dispatches.Load()
	f.members(false)
	unblock()
	select {
	case res := <-done:
		if res.Code < 400 || f.dispatches.Load() != dispatched {
			f.t.Fatal("removed member's already-read bytes reached upstream", res.Code)
		}
	case <-time.After(15 * time.Second):
		f.t.Fatal("revoked storage request did not finish")
	}
	f.members(true)
	if f.membership == previous {
		f.t.Fatal("removed/rejoined member reused captured identity")
	}
	if err := f.svc.ReauthorizeTeamSession(f.ctx, captured, "mdl_tmedia_chat"); err == nil {
		f.t.Fatal("old captured Session/member identity survived rejoin")
	}
	before, dispatches := f.counts()
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil))
	// Stable pair use/holds survive membership replacement, while old objects do not.
	value, err := f.svc.GetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, f.actor.User.ID)
	if err != nil || value.QuotaUsage == nil || value.QuotaUsage.Month == nil || value.QuotaUsage.Month.TokensUsed != f.knownTokens || value.QuotaUsage.Month.MoneyUsed["USD"] != fmt.Sprint(f.knownMoney) || value.QuotaUsage.Month.TokensHeld < 5 {
		f.t.Fatal("rejoin reset stable Team/User usage or unknown holds", err, value)
	}
	fresh := f.upload("after-rejoin.png", teamAttachmentPNG(), f.cookie, f.csrf, nil)
	f.expectNativeStatus("fresh membership media", entity.ProtocolOpenAIChat, false, f.native(entity.ProtocolOpenAIChat, false, []string{fresh.ID}, nil), 200)
	if err := f.svc.FlushCallRecorder(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	f.knownTokens += 5
	f.knownMoney += 2
}

func (f *teamAttachmentFixture) checkUploadRevocation() {
	f.t.Helper()
	started, proceed := make(chan struct{}), make(chan struct{})
	f.putMu.Lock()
	f.putStarted, f.putContinue = started, proceed
	f.putMu.Unlock()
	var release sync.Once
	unblock := func() { release.Do(func() { close(proceed) }) }
	defer unblock()
	before, dispatches := f.counts()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.uploadResponse("put-revoked.pdf", []byte("%PDF-1.7\ncontrolled\n%%EOF"), f.cookie, f.csrf, nil)
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		f.t.Fatal("controlled Team storage PUT did not begin")
	}
	f.members(false)
	unblock()
	select {
	case res := <-done:
		f.zeroWork(before, dispatches, res)
	case <-time.After(15 * time.Second):
		f.t.Fatal("revoked Team upload did not finish")
	}
	var row entity.StorageObject
	if err := f.db.Take(&row, "owner_kind = ? AND owner_id = ? AND name = ?", "team", teamAttachmentFixtureID, "put-revoked.pdf").Error; err != nil || row.State != "delete_pending" {
		f.t.Fatal("revoked PUT was not retained for durable cleanup", err, row.State)
	}
	f.members(true)
}

func (f *teamAttachmentFixture) checkRestartCleanup() {
	f.t.Helper()
	fresh := f.upload("expiry-cleanup.pdf", []byte("%PDF-1.7\ncontrolled cleanup\n%%EOF"), f.cookie, f.csrf, nil)
	expiredCreated := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Hour)
	if err := f.db.Table("storage_objects").Where("id = ?", fresh.ID).Updates(map[string]any{"created_at": expiredCreated, "expires_at": expiredCreated.Add(time.Hour), "next_cleanup_at": time.Now().Add(-time.Minute)}).Error; err != nil {
		f.t.Fatal(err)
	}
	f.svc.StopRuntime()
	if err := f.svc.StopCallRecorder(); err != nil {
		f.t.Fatal(err)
	}
	f.svc = f.newService()
	f.router = fox.New()
	New(f.svc).RegisterRoutes(f.router)
	if err := f.svc.StartRuntime(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	if err := f.svc.StartCallRecorder(f.ctx, f.spool); err != nil {
		f.t.Fatal(err)
	}
	before, dispatches := f.counts()
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{fresh.ID}, nil))
	if err := f.svc.FlushStorageCleanup(f.ctx, 32); err != nil {
		f.t.Fatal(err)
	}
	var row entity.StorageObject
	if err := f.db.Take(&row, "id = ?", fresh.ID).Error; err != nil || row.State != "deleted" {
		f.t.Fatal("persisted expired Team object was not cleaned after restart", err, row.State)
	}
	f.storage.mu.Lock()
	_, remaining := f.storage.objects["/routex-test/routex/"+fresh.ID]
	f.storage.mu.Unlock()
	if remaining {
		f.t.Fatal("cleanup left the exact version in storage")
	}
	value, err := f.svc.GetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, f.actor.User.ID)
	if err != nil || value.QuotaUsage == nil || value.QuotaUsage.Month == nil || value.QuotaUsage.Month.TokensUsed != f.knownTokens || value.QuotaUsage.Month.MoneyUsed["USD"] != fmt.Sprint(f.knownMoney) || value.QuotaUsage.Month.TokensHeld != 5 || value.QuotaUsage.Month.MoneyHeld["USD"] != "2.000000000000000003" || value.QuotaUsage.Month.TokensUnknown != 0 || value.QuotaUsage.Month.MoneyUnknown != 0 {
		f.t.Fatal("restart lost settled use/unknown holds", err, value)
	}
}

func (f *teamAttachmentFixture) checkCurrentAuthority(image service.AttachmentView) {
	f.t.Helper()
	before, dispatches := f.counts()
	disabled, active := entity.ResourceDisabled, entity.ResourceActive
	if _, err := f.svc.UpdateResource(f.ctx, f.adminID, service.TeamResource, teamAttachmentFixtureID, service.ResourceUpdate{Status: &disabled}); err != nil {
		f.t.Fatal(err)
	}
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil))
	if _, err := f.svc.UpdateResource(f.ctx, f.adminID, service.TeamResource, teamAttachmentFixtureID, service.ResourceUpdate{Status: &active}); err != nil {
		f.t.Fatal(err)
	}
	// Model grant revocation is distinct from attachment ownership.
	if _, err := f.svc.SetResourceModels(f.ctx, f.adminID, service.TeamResource, teamAttachmentFixtureID, []string{"mdl_tmedia_responses", "mdl_tmedia_messages", "mdl_tmedia_gemini"}); err != nil {
		f.t.Fatal(err)
	}
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil))
	if _, err := f.svc.SetResourceModels(f.ctx, f.adminID, service.TeamResource, teamAttachmentFixtureID, []string{"mdl_tmedia_chat", "mdl_tmedia_responses", "mdl_tmedia_messages", "mdl_tmedia_gemini"}); err != nil {
		f.t.Fatal(err)
	}
	// An expired Session cannot reuse an otherwise-current object's proof.
	var session entity.Session
	if err := f.db.Take(&session, "id = ?", func() string {
		identity, err := f.svc.RuntimeAuthenticateTeamSession(f.ctx, f.cookie.Value, teamAttachmentFixtureID)
		if err != nil {
			f.t.Fatal(err)
		}
		return identity.SessionID
	}()).Error; err != nil {
		f.t.Fatal(err)
	}
	if err := f.db.Model(&entity.Session{}).Where("id = ?", session.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		f.t.Fatal(err)
	}
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil))
	if err := f.db.Model(&entity.Session{}).Where("id = ?", session.ID).Update("expires_at", session.ExpiresAt).Error; err != nil {
		f.t.Fatal(err)
	}
	if err := f.svc.RefreshRuntime(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	// Revoking the exact Session never invalidates the object's membership proof,
	// but the old cookie cannot read or invoke it.
	if err := f.svc.RevokeAccountSession(f.ctx, f.actor.User.ID, session.ID); err != nil {
		f.t.Fatal(err)
	}
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil))
	f.renewSession()
	// Disable/enable retains creator/membership identity but requires a new Session.
	flag := true
	if _, err := f.svc.UpdateMember(f.ctx, f.adminID, f.actor.User.ID, &flag, nil); err != nil {
		f.t.Fatal(err)
	}
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil))
	flag = false
	if _, err := f.svc.UpdateMember(f.ctx, f.adminID, f.actor.User.ID, &flag, nil); err != nil {
		f.t.Fatal(err)
	}
	f.renewSession()
	// Public identity fields are not authority: preserve private proof while
	// changing each identity to a case alias, and demand zero object reads.
	identity, err := f.svc.RuntimeAuthenticateTeamSession(f.ctx, f.cookie.Value, teamAttachmentFixtureID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, edit := range []func(*service.TeamSessionIdentity){
		func(i *service.TeamSessionIdentity) { i.UserID = strings.ToUpper(i.UserID) },
		func(i *service.TeamSessionIdentity) { i.TeamID = strings.ToUpper(i.TeamID) },
		func(i *service.TeamSessionIdentity) { i.TeamMembershipID = strings.ToUpper(i.TeamMembershipID) },
	} {
		alias := *identity
		edit(&alias)
		if _, _, err := f.svc.TeamAttachmentContent(f.ctx, &alias, image.ID); err == nil {
			f.t.Fatal("identity alias borrowed a creator's object")
		}
		reads, calls := f.counts()
		if reads != before || calls != dispatches {
			f.t.Fatal("identity alias read storage or dispatched")
		}
	}
	// Team lifecycle can change during remote I/O independently of membership.
	started, proceed := make(chan struct{}), make(chan struct{})
	f.storage.mu.Lock()
	f.storage.blockGetStarted, f.storage.blockGetContinue = started, proceed
	f.storage.mu.Unlock()
	var release sync.Once
	unblock := func() { release.Do(func() { close(proceed) }) }
	defer unblock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		f.t.Fatal("Team lifecycle read race did not begin")
	}
	if _, err := f.svc.UpdateResource(f.ctx, f.adminID, service.TeamResource, teamAttachmentFixtureID, service.ResourceUpdate{Status: &disabled}); err != nil {
		f.t.Fatal(err)
	}
	unblock()
	select {
	case res := <-done:
		if res.Code < 400 || f.dispatches.Load() != dispatches {
			f.t.Fatal("inactive Team bytes reached upstream", res.Code)
		}
	case <-time.After(15 * time.Second):
		f.t.Fatal("Team lifecycle read race did not finish")
	}
	if _, err := f.svc.UpdateResource(f.ctx, f.adminID, service.TeamResource, teamAttachmentFixtureID, service.ResourceUpdate{Status: &active}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *teamAttachmentFixture) checkTerminalAuthority() {
	f.t.Helper()
	foreign := f.seedObject("team", "tem_media_second", f.actor.User.ID, "tmm_media_second", teamAttachmentPNG())
	path := "/api/v1/teams/tem_media_second/attachments/" + foreign + "/content"
	var row entity.StorageObject
	if err := f.db.Take(&row, "id = ?", foreign).Error; err != nil {
		f.t.Fatal("secondary-Team download precondition: persisted object missing", foreign, err)
	}
	creatorProof, membershipProof := "<missing>", "<missing>"
	if row.CreatorUserID != nil {
		creatorProof = *row.CreatorUserID
	}
	if row.CreatorMembershipID != nil {
		membershipProof = *row.CreatorMembershipID
	}
	if row.ID != foreign || row.OwnerKind != entity.StorageOwnerTeam || row.OwnerID != "tem_media_second" || row.CreatorUserID == nil || *row.CreatorUserID != f.actor.User.ID || row.CreatorMembershipID == nil || *row.CreatorMembershipID != "tmm_media_second" || row.Purpose != "attachment" || row.State != "ready" || row.ExpiresAt == nil || !row.ExpiresAt.Equal(row.CreatedAt.Add(time.Hour)) || !row.ExpiresAt.After(time.Now().UTC()) {
		f.t.Fatalf("secondary-Team download precondition: invalid stored proof object=%s owner=%s/%s creator=%s membership=%s purpose=%s state=%s created=%s expires=%v", row.ID, row.OwnerKind, row.OwnerID, creatorProof, membershipProof, row.Purpose, row.State, row.CreatedAt.Format(time.RFC3339Nano), row.ExpiresAt)
	}
	var team entity.Team
	var member entity.TeamMembership
	var user entity.User
	if err := f.db.Take(&team, "id = ?", row.OwnerID).Error; err != nil {
		f.t.Fatal("secondary-Team download precondition: current Team missing", err)
	}
	if err := f.db.Take(&member, "id = ?", *row.CreatorMembershipID).Error; err != nil {
		f.t.Fatal("secondary-Team download precondition: current membership missing", err)
	}
	if err := f.db.Take(&user, "id = ?", f.actor.User.ID).Error; err != nil {
		f.t.Fatal("secondary-Team download precondition: current creator missing", err)
	}
	if team.ID != row.OwnerID || team.Status != entity.ResourceActive || member.ID != *row.CreatorMembershipID || member.TeamID != team.ID || member.UserID != user.ID || member.Status != entity.ResourceActive || member.Role != entity.TeamMember || user.ID != f.actor.User.ID || user.Disabled || user.OffboardedAt != nil {
		f.t.Fatalf("secondary-Team download precondition: current subjects differ Team=%s/%s member=%s Team=%s actor=%s role=%s status=%s creator=%s disabled=%v offboarded=%v", team.ID, team.Status, member.ID, member.TeamID, member.UserID, member.Role, member.Status, user.ID, user.Disabled, user.OffboardedAt != nil)
	}
	if err := f.svc.RefreshRuntime(f.ctx); err != nil {
		f.t.Fatal("secondary-Team download precondition: refresh current authority", err)
	}
	identity, err := f.svc.RuntimeAuthenticateTeamSession(f.ctx, f.cookie.Value, team.ID)
	if err != nil || identity == nil || identity.TeamID != team.ID || identity.UserID != user.ID || identity.TeamMembershipID != member.ID {
		if identity != nil {
			f.t.Fatalf("secondary-Team download precondition: current Session identity differs Team=%s actor=%s membership=%s: %v", identity.TeamID, identity.UserID, identity.TeamMembershipID, err)
		}
		f.t.Fatalf("secondary-Team download precondition: current Session identity unavailable/mismatched: %v", err)
	}
	beforeGET, beforeDispatch := f.counts()
	response := f.request(f.cookie, "", "GET", path, nil, "application/json", nil)
	if response.Code != http.StatusOK {
		reads, dispatched := f.counts()
		f.t.Fatalf("secondary-Team creator content GET %s: status=%d want=200 storage_GET_delta=%d upstream_delta=%d response=%s", path, response.Code, reads-beforeGET, dispatched-beforeDispatch, response.Body.String())
	}
	disabled, archived := entity.ResourceDisabled, entity.ResourceArchived
	for _, status := range []*string{&disabled, &archived} {
		if _, err := f.svc.UpdateResource(f.ctx, f.adminID, service.TeamResource, "tem_media_second", service.ResourceUpdate{Status: status}); err != nil {
			f.t.Fatal(err)
		}
	}
	before, dispatches := f.counts()
	f.zeroWork(before, dispatches, f.request(f.cookie, "", "GET", path, nil, "application/json", nil))
	fresh := f.upload("offboard.png", teamAttachmentPNG(), f.cookie, f.csrf, nil)
	if _, err := f.svc.EmergencyOffboarding(f.ctx, f.adminID, f.actor.User.ID, service.OffboardingEmergencyInput{RequestID: "afeefeed-cc12-4ffd-8100-123456789abc", CurrentPassword: "test-only-media-password", Reason: "Controlled Team attachment departure"}); err != nil {
		f.t.Fatal(err)
	}
	before, dispatches = f.counts()
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{fresh.ID}, nil))
	f.zeroWork(before, dispatches, f.request(f.cookie, f.csrf, "DELETE", "/api/v1/teams/"+teamAttachmentFixtureID+"/attachments/"+fresh.ID, nil, "application/json", nil))
}

func (f *teamAttachmentFixture) renewSession() {
	f.t.Helper()
	login := identityRequest(f.router, "POST", "/api/v1/auth/login", `{"email":"system-team-media-actor@example.invalid","password":"test-only-system-password"}`, nil, "")
	expectStatus(f.t, login, 200)
	renewed, cookie := readIdentity(f.t, login)
	f.cookie, f.csrf = cookie, renewed.CSRFToken
	if err := f.svc.RefreshRuntime(f.ctx); err != nil {
		f.t.Fatal(err)
	}
}

func (f *teamAttachmentFixture) checkConcurrentAdmission(image service.AttachmentView) {
	f.t.Helper()
	started, proceed := make(chan struct{}, 2), make(chan struct{})
	f.upstreamMu.Lock()
	f.upstreamStarted, f.upstreamContinue = started, proceed
	f.upstreamMu.Unlock()
	var release sync.Once
	unblock := func() { release.Do(func() { close(proceed) }) }
	defer unblock()
	done := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { done <- f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil) }()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			f.t.Fatal("two admitted Team media lanes did not reach controlled upstream")
		}
	}
	before, dispatches := f.counts()
	third := f.native(entity.ProtocolOpenAIChat, false, []string{image.ID}, nil)
	f.zeroWork(before, dispatches, third)
	for _, user := range []string{"", f.actor.User.ID} {
		value, err := f.svc.GetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, user)
		if err != nil || value.Active == nil || *value.Active != 2 {
			f.t.Fatal("media admission did not atomically acquire both Team/member accounts", err, value)
		}
	}
	f.upstreamMu.Lock()
	f.upstreamStarted = nil
	f.upstreamMu.Unlock()
	unblock()
	for range 2 {
		select {
		case res := <-done:
			f.expectNativeStatus("concurrent media", entity.ProtocolOpenAIChat, false, res, 200)
		case <-time.After(15 * time.Second):
			f.t.Fatal("independent Team media lanes did not finish")
		}
	}
	if err := f.svc.FlushCallRecorder(f.ctx); err != nil {
		f.t.Fatal(err)
	}
	f.knownTokens += 10
	f.knownMoney += 4
	for _, user := range []string{"", f.actor.User.ID} {
		value, err := f.svc.GetTeamResourceLimit(f.ctx, f.adminID, teamAttachmentFixtureID, user)
		if err != nil || value.Active == nil || *value.Active != 0 || value.QuotaUsage == nil || value.QuotaUsage.Month == nil || value.QuotaUsage.Month.TokensUsed != f.knownTokens || value.QuotaUsage.Month.MoneyUsed["USD"] != fmt.Sprint(f.knownMoney) {
			f.t.Fatal("media lanes did not settle/release both admission accounts exactly once", err, value)
		}
	}
	personal, err := f.svc.GetResourceLimit(f.ctx, f.adminID, service.LimitTarget{Kind: "user", ID: f.actor.User.ID})
	if err != nil || personal.QuotaUsage == nil || personal.QuotaUsage.Month == nil || personal.QuotaUsage.Month.TokensUsed != 0 || personal.QuotaUsage.Month.TokensHeld != 0 || personal.QuotaUsage.Month.MoneyUsed["USD"] != "" && personal.QuotaUsage.Month.MoneyUsed["USD"] != "0" {
		f.t.Fatal("Team media charged the Personal account", err, personal)
	}
}

func (f *teamAttachmentFixture) checkKeyScopeIsolation(image service.AttachmentView) {
	f.t.Helper()
	// These real Keys are currently allowed the Model and have no finite owner
	// restriction; denial therefore proves ownership rather than quota failure.
	personal := "rx_" + strings.Repeat("m", 43)
	project := "rxp_" + strings.Repeat("p", 43)
	f.create(&entity.UserModelGrant{UserID: f.other.User.ID, ModelID: "mdl_tmedia_chat"},
		&entity.APIKey{ID: "key_tmedia_personal", UserID: f.other.User.ID, Name: "Isolated Personal", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personal), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_tmedia_personal", ModelID: "mdl_tmedia_chat"},
		&entity.Project{ID: "prj_tmedia_keys", Name: "Isolated Project", Status: entity.ResourceActive, CreatorID: f.adminID},
		&entity.ProjectManager{ID: "pmg_tmedia_keys", ProjectID: "prj_tmedia_keys", UserID: f.adminID},
		&entity.ProjectModelGrant{ProjectID: "prj_tmedia_keys", ModelID: "mdl_tmedia_chat"},
		&entity.ProjectKey{ID: "key_tmedia_project", ProjectID: "prj_tmedia_keys", CreatorID: f.adminID, Name: "Isolated Project", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(project), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "key_tmedia_project", ModelID: "mdl_tmedia_chat"})
	for _, bearer := range []string{personal, project} {
		before, dispatches := f.counts()
		denied := f.request(nil, "", "POST", "/v1/chat/completions", strings.NewReader(teamAttachmentNativeBody(entity.ProtocolOpenAIChat, false, []string{image.ID})), "application/json", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+bearer) })
		f.zeroWork(before, dispatches, denied)
		if !strings.Contains(denied.Body.String(), "attachment_not_found") {
			f.t.Fatal("Key was not rejected by exact attachment ownership", denied.Code, denied.Body.String())
		}
	}
	scratch := f.upload("delete-draft.png", teamAttachmentPNG(), f.cookie, f.csrf, nil)
	before, dispatches := f.counts()
	deleted := f.request(f.cookie, f.csrf, "DELETE", "/api/v1/teams/"+teamAttachmentFixtureID+"/attachments/"+scratch.ID, nil, "application/json", nil)
	expectStatus(f.t, deleted, 200)
	reads, after := f.counts()
	if reads != before || after != dispatches {
		f.t.Fatal("deleting a draft read storage or dispatched inference")
	}
	f.zeroWork(before, dispatches, f.native(entity.ProtocolOpenAIChat, false, []string{scratch.ID}, nil))
}

// Actual media amounts stay exact while reservation additionally covers native
// token/cache component rounding. The fixture must not equate those amounts.
func TestTeamAttachmentFixtureReservationRounding(t *testing.T) {
	schedule := pricing.Schedule{ProviderModelID: "pmd_tmedia_chat", Protocol: entity.ProtocolOpenAIChat}
	for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite, pricing.ImageInput, pricing.PDFInput} {
		rate := pricing.Rate{Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}
		if metric == pricing.ImageInput {
			rate.Unit, rate.Amount = pricing.ImageUnit, "2"
		}
		if metric == pricing.PDFInput {
			rate.Unit = pricing.PDFUnit
		}
		schedule.Rates = append(schedule.Rates, rate)
	}
	fx := pricing.FX{PlatformCurrency: "USD", Rates: map[string]string{"USD": "1"}}
	for _, write := range []bool{false, true} {
		bound, err := pricing.ReserveBound(schedule, fx, pricing.Capacity{Input: 4, Output: 1, CacheRead: true, CacheWrite: write, ImageInputs: 2, PDFInputs: 1})
		if err != nil || bound.Amount != "4.000000000000000003" {
			t.Fatalf("known media reservation bound: %+v error=%v", bound, err)
		}
	}
	bound, err := pricing.ReserveBound(schedule, fx, pricing.Capacity{Input: 4, Output: 1, CacheRead: true, CacheWrite: true, ImageInputs: 1})
	if err != nil || bound.Amount != "2.000000000000000003" {
		t.Fatalf("unknown Chat media retained bound: %+v error=%v", bound, err)
	}
}
