package handler

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Appended to the existing Vault lifecycle: the same admitted admin, DB and
// credential store are retained. Only its deliberately removed write grant is
// restored; no new registry case, authority shortcut or Gateway work is added.
func testVaultHistoricalCleanupLifecycle(t *testing.T, db *gorm.DB, router http.Handler, cookie *http.Cookie, csrf string) {
	t.Helper()
	permission := entity.RolePermission{RoleID: "rol_admin", Permission: "secrets.write"}
	var absent int64
	if err := db.Model(&entity.RolePermission{}).Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Count(&absent).Error; err != nil || absent != 0 {
		t.Fatal("original fixture write removal missing", err)
	}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	var restored entity.RolePermission
	if err := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Take(&restored).Error; err != nil || restored != permission {
		t.Fatal("exact fixture permission restore", err)
	}
	var mu sync.Mutex
	marker, dataPath := "", ""
	effects := []string{}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		effects = append(effects, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			if r.Header.Get("X-Vault-Token") != "historical-writer-A" || !strings.HasPrefix(r.URL.Path, "/v1/kv/data/routex-history/") {
				t.Error("write did not use exact original descriptor/auth")
			}
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Options.CAS != 0 || body.Data["value"] == "" {
				t.Error("historical Write plan")
			}
			marker, dataPath = body.Data["value"], r.URL.Path
			_, _ = w.Write([]byte(`{"data":{"version":1,"destroyed":false,"deletion_time":""}}`))
		case "GET":
			if r.Header.Get("X-Vault-Token") != "historical-reader-A" || r.URL.Path != dataPath || r.URL.RawQuery != "version=1" {
				t.Error("cleanup did not use retained reader/exact owned path")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]string{"value": marker}, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		case "PUT":
			if r.Header.Get("X-Vault-Token") != "historical-writer-A" || r.URL.Path != strings.Replace(dataPath, "/data/", "/destroy/", 1) {
				t.Error("cleanup did not use retained writer/exact owned path")
			}
			var body struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) {
				t.Error("cleanup destroyed another version")
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Error("unexpected historical effect")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer stub.Close()
	request := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			var err error
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		if method != http.MethodGet {
			req.Header.Set("Origin", "http://routex.test")
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	base := "/api/v1/admin/secrets/integrations"
	pageResponse := request("GET", base, nil, "")
	expectStatus(t, pageResponse, 200)
	var page service.VaultIntegrationPage
	if json.Unmarshal(pageResponse.Body.Bytes(), &page) != nil || !page.CanWrite || !page.CanTest {
		t.Fatal("fresh restored authority")
	}
	input := service.VaultConfigInput{RequestID: "73000000-1111-4111-8111-111111111111", Name: "Historical cleanup", Descriptor: service.VaultDescriptor{Endpoint: stub.URL, Mount: "kv", Prefix: "routex-history", DataField: "value"}, WriterAuth: service.VaultAuthInput{Action: "replace", Token: "historical-writer-A"}, ReaderAuth: service.VaultAuthInput{Action: "replace", Token: "historical-reader-A"}, Reason: "Review original retained auth"}
	created := request("POST", base, input, page.ReviewETag)
	expectStatus(t, created, 200)
	var saved service.VaultConfigResult
	if json.Unmarshal(created.Body.Bytes(), &saved) != nil || !saved.Committed || !saved.Changed {
		t.Fatal("original config receipt")
	}
	target := base + "/" + saved.IntegrationID
	currentResponse := request("GET", target, nil, "")
	expectStatus(t, currentResponse, 200)
	var current service.VaultIntegrationView
	if json.Unmarshal(currentResponse.Body.Bytes(), &current) != nil || current.RevisionID != saved.RevisionID {
		t.Fatal("original fresh revision")
	}
	write := service.VaultStageInput{RequestID: "73000000-2222-4222-8222-222222222222", Reason: "Write original plan"}
	written := request("POST", target+"/probes/write", write, current.ReviewETag)
	expectStatus(t, written, 200)
	var probe service.VaultProbeView
	if json.Unmarshal(written.Body.Bytes(), &probe) != nil || probe.State != "awaiting_read" || probe.RevisionID != saved.RevisionID || !probe.Write.Succeeded || probe.Read.Attempted {
		t.Fatal("original plan Write")
	}
	var originalProbe entity.VaultProbe
	if err := db.Where("id = ?", probe.ID).Take(&originalProbe).Error; err != nil {
		t.Fatal(err)
	}
	var originalRevision entity.VaultRevision
	var originalWriter entity.VaultWriterAuth
	var originalReader entity.VaultReaderAuth
	if err := db.Where("id = ?", saved.RevisionID).Take(&originalRevision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", saved.RevisionID).Take(&originalWriter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", saved.RevisionID).Take(&originalReader).Error; err != nil {
		t.Fatal(err)
	}
	if originalRevision.IntegrationID != saved.IntegrationID || originalRevision.IntegrationBirth.IsZero() || originalWriter.ID != saved.RevisionID || originalReader.ID != saved.RevisionID {
		t.Fatal("original retained identity/birth")
	}
	input.RequestID = "73000000-3333-4333-8333-333333333333"
	input.WriterAuth.Token, input.ReaderAuth.Token = "current-writer-B", "current-reader-B"
	input.Reason = "Replace auth while retaining owned plan"
	changed := request("PUT", target, input, current.ReviewETag)
	expectStatus(t, changed, 200)
	var replacement service.VaultConfigResult
	if json.Unmarshal(changed.Body.Bytes(), &replacement) != nil || !replacement.Committed || !replacement.Changed || replacement.RevisionID == saved.RevisionID {
		t.Fatal("new config revision")
	}
	probePath := target + "/probes/" + probe.ID
	refreshed := request("GET", probePath, nil, "")
	expectStatus(t, refreshed, 200)
	var historical service.VaultProbeView
	if json.Unmarshal(refreshed.Body.Bytes(), &historical) != nil || historical.RevisionID != saved.RevisionID || historical.RequestID != write.RequestID {
		t.Fatal("historical plan identity")
	}
	read := service.VaultStageInput{RequestID: "73000000-4444-4444-8444-444444444444", Reason: "Do not Read old config"}
	expectStatus(t, request("POST", probePath+"/read", read, historical.ReviewETag), 409)
	mu.Lock()
	n := len(effects)
	mu.Unlock()
	if n != 1 {
		t.Fatal("historical Read dispatched")
	}
	cleanup := service.VaultStageInput{RequestID: "73000000-5555-4555-8555-555555555555", Reason: "Cleanup exact retained plan"}
	finished := request("POST", probePath+"/cleanup", cleanup, historical.ReviewETag)
	expectStatus(t, finished, 200)
	var complete service.VaultProbeView
	if json.Unmarshal(finished.Body.Bytes(), &complete) != nil || complete.ID != probe.ID || complete.RevisionID != saved.RevisionID || complete.RequestID != write.RequestID || complete.State != "completed" || complete.Read.Attempted || complete.Read.Succeeded || complete.Cleanup.State != "acknowledged" || !complete.Cleanup.Observation.Succeeded || complete.Version == nil || *complete.Version != 1 {
		t.Fatal("historical Cleanup ownership is not explicit Read")
	}
	replayed := request("POST", probePath+"/cleanup", cleanup, historical.ReviewETag)
	expectStatus(t, replayed, 200)
	var repeated service.VaultProbeView
	if err := json.Unmarshal(replayed.Body.Bytes(), &repeated); err != nil {
		t.Fatal("recorded completed Cleanup replay invalid DTO")
	}
	if changed := vaultCleanupReplayChangedFields(complete, repeated); len(changed) != 0 {
		t.Fatal("recorded completed Cleanup replay changed fields", changed)
	}
	mu.Lock()
	gotEffects := append([]string(nil), effects...)
	exactDataPath := dataPath
	mu.Unlock()
	if !reflect.DeepEqual(gotEffects, []string{"POST " + exactDataPath, "GET " + exactDataPath, "PUT " + strings.Replace(exactDataPath, "/data/", "/destroy/", 1)}) {
		t.Fatal("historical Cleanup or replay remote effect scope")
	}
	var afterRevision entity.VaultRevision
	var afterWriter entity.VaultWriterAuth
	var afterReader entity.VaultReaderAuth
	if err := db.Where("id = ?", saved.RevisionID).Take(&afterRevision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", saved.RevisionID).Take(&afterWriter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", saved.RevisionID).Take(&afterReader).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterRevision, originalRevision) || afterWriter != originalWriter || afterReader != originalReader {
		t.Fatal("cleanup changed retained revision/auth")
	}
	var integration entity.VaultIntegration
	if err := db.Where("id = ?", saved.IntegrationID).Take(&integration).Error; err != nil || integration.RevisionID != replacement.RevisionID || !integration.CreatedAt.Equal(originalRevision.IntegrationBirth) || integration.ActiveProbeID != nil {
		t.Fatal("current config/birth changed during historical Cleanup", err)
	}
	var retainedProbe entity.VaultProbe
	if err := db.Where("id = ?", probe.ID).Take(&retainedProbe).Error; err != nil {
		t.Fatal(err)
	}
	if retainedProbe.ID != originalProbe.ID || retainedProbe.RequestID != originalProbe.RequestID || retainedProbe.IntegrationID != originalProbe.IntegrationID || !retainedProbe.IntegrationBirth.Equal(originalProbe.IntegrationBirth) || retainedProbe.RevisionID != originalProbe.RevisionID || retainedProbe.ActorID != originalProbe.ActorID || !retainedProbe.ActorBirth.Equal(originalProbe.ActorBirth) || !retainedProbe.CreatedAt.Equal(originalProbe.CreatedAt) || retainedProbe.ExpectedSHA256 != originalProbe.ExpectedSHA256 || retainedProbe.DescriptorSHA256 != originalProbe.DescriptorSHA256 {
		t.Fatal("historical Cleanup changed immutable plan identity/birth")
	}
	var commands []entity.VaultProbeCommand
	if err := db.Where("probe_id = ?", probe.ID).Order("created_at ASC").Find(&commands).Error; err != nil || len(commands) != 2 || commands[0].Kind != "write" || commands[1].Kind != "cleanup" || commands[1].RequestID != cleanup.RequestID || commands[1].FinishedAt == nil {
		t.Fatal("durable command scope/replay", err)
	}
	var ownership service.VaultObservationView
	if json.Unmarshal([]byte(commands[1].OwnershipJSON), &ownership) != nil || !ownership.Attempted || !ownership.Succeeded {
		t.Fatal("historical Cleanup ownership observation missing")
	}
	for _, model := range []any{&entity.APIKey{}, &entity.ProjectKey{}, &entity.CallRecord{}, &entity.CallAttempt{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("historical cleanup added Gateway work", err)
		}
	}
}

// Database timestamp codecs may return a different location for the same instant.
// Compare exact instants, then every public DTO field; never discard replay facts.
func vaultCleanupReplayChangedFields(first, replay service.VaultProbeView) []string {
	var changed []string
	if !first.CreatedAt.Equal(replay.CreatedAt) {
		changed = append(changed, "created_at")
	}
	if (first.FinishedAt == nil) != (replay.FinishedAt == nil) || first.FinishedAt != nil && replay.FinishedAt != nil && !first.FinishedAt.Equal(*replay.FinishedAt) {
		changed = append(changed, "finished_at")
	}
	first.CreatedAt, replay.CreatedAt = first.CreatedAt.UTC(), replay.CreatedAt.UTC()
	if first.FinishedAt != nil {
		x := first.FinishedAt.UTC()
		first.FinishedAt = &x
	}
	if replay.FinishedAt != nil {
		x := replay.FinishedAt.UTC()
		replay.FinishedAt = &x
	}
	a, b := reflect.ValueOf(first), reflect.ValueOf(replay)
	for i := 0; i < a.NumField(); i++ {
		name := a.Type().Field(i).Tag.Get("json")
		if name != "created_at" && name != "finished_at" && !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			changed = append(changed, name)
		}
	}
	return changed
}

func TestVaultHistoricalCleanupReplayTimestampCodec(t *testing.T) {
	instant := time.Date(2026, 10, 7, 1, 2, 3, 456789000, time.UTC)
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(instant.UnixMicro()-time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro()))
	codec := pgtype.TimestamptzCodec{}
	dbValue, err := codec.DecodeDatabaseSQLValue(pgtype.NewMap(), pgtype.TimestamptzOID, pgtype.BinaryFormatCode, encoded[:])
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := dbValue.(time.Time)
	if !ok || !loaded.Equal(instant) || loaded.Location() != time.Local {
		t.Fatal("pinned default PostgreSQL codec instant/location")
	}
	// Exercise the actual codec with an explicit non-UTC scan location on every host.
	codec.ScanLocation = time.FixedZone("database-local", 8*60*60)
	dbValue, err = codec.DecodeDatabaseSQLValue(pgtype.NewMap(), pgtype.TimestamptzOID, pgtype.BinaryFormatCode, encoded[:])
	if err != nil {
		t.Fatal(err)
	}
	loaded = dbValue.(time.Time)
	v := int64(1)
	first := service.VaultProbeView{ID: "probe", RequestID: "write-uuid", IntegrationID: "integration", RevisionID: "original-revision", ReviewETag: "exact-etag", State: "completed", Version: &v,
		Write: service.VaultObservationView{Attempted: true, Succeeded: true, DurationMS: "1"}, Read: service.VaultObservationView{DurationMS: "0"},
		Cleanup: service.VaultCleanupView{State: "acknowledged", Observation: service.VaultObservationView{Attempted: true, Succeeded: true, DurationMS: "2"}}, CreatedAt: instant.Add(-time.Second), FinishedAt: &instant}
	replay := first
	replay.CreatedAt, replay.FinishedAt = first.CreatedAt.In(loaded.Location()), &loaded
	one, _ := json.Marshal(first)
	two, _ := json.Marshal(replay)
	if reflect.DeepEqual(one, two) {
		t.Fatal("original raw-body oracle did not reproduce location-only rejection")
	}
	if changed := vaultCleanupReplayChangedFields(first, replay); len(changed) != 0 {
		t.Fatal("same exact instants and all DTO facts rejected", changed)
	}
	if first.FinishedAt.Location() != time.UTC || replay.FinishedAt.Location() != loaded.Location() {
		t.Fatal("comparison mutated original DTO timestamps")
	}
	cases := []struct {
		name   string
		change func(*service.VaultProbeView)
	}{
		{"created_at", func(x *service.VaultProbeView) { x.CreatedAt = x.CreatedAt.Add(time.Microsecond) }},
		{"finished_at", func(x *service.VaultProbeView) { y := x.FinishedAt.Add(time.Microsecond); x.FinishedAt = &y }},
		{"finished_at", func(x *service.VaultProbeView) { x.FinishedAt = nil }},
		{"id", func(x *service.VaultProbeView) { x.ID = "other" }},
		{"request_id", func(x *service.VaultProbeView) { x.RequestID = "other" }},
		{"integration_id", func(x *service.VaultProbeView) { x.IntegrationID = "other" }},
		{"revision_id", func(x *service.VaultProbeView) { x.RevisionID = "other" }},
		{"review_etag", func(x *service.VaultProbeView) { x.ReviewETag = "other" }},
		{"state", func(x *service.VaultProbeView) { x.State = "cleanup_pending" }},
		{"version", func(x *service.VaultProbeView) { y := int64(0); x.Version = &y }},
		{"write", func(x *service.VaultProbeView) { x.Write.DurationMS = "2" }},
		{"read", func(x *service.VaultProbeView) { x.Read.Attempted = true }},
		{"cleanup", func(x *service.VaultProbeView) { x.Cleanup.Observation.Succeeded = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := replay
			tc.change(&bad)
			if changed := vaultCleanupReplayChangedFields(first, bad); !reflect.DeepEqual(changed, []string{tc.name}) {
				t.Fatal("exact replay fact drift not diagnosed", changed)
			}
		})
	}
}
