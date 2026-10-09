package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func TestProviderMetadataStrictIntentBounds(t *testing.T) {
	invalid := []string{`{}`, `null`, `[]`, `{"name":"A","reason":null}`, `{"name":null,"reason":"R"}`, `{"Name":"A","reason":"R"}`, `{"name":"A","reason":"R","egress_mode":"direct"}`, `{"name":"A","reason":"R","name":"B"}`, `{"name":"A","reason":"R","\u006eame":"B"}`, `{"name":"A","reason":"R"} {}`, `{"name":"\ud800","reason":"R"}`, `{"name":"A","reason":"\udfff"}`, `{"name":" A","reason":"R"}`, `{"name":"A","reason":" R"}`, `{"name":"A\n","reason":"R"}`, `{"name":"A","reason":""}`, string([]byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 255, '"', '}'})}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			var input ProviderMetadataInput
			if json.Unmarshal([]byte(raw), &input) == nil {
				t.Fatal("ambiguous operation accepted")
			}
		})
	}
	for _, input := range []ProviderMetadataInput{{strings.Repeat("😀", 100), strings.Repeat("r", 1024)}, {"O'Neil", "Exact reason"}} {
		raw, _ := json.Marshal(input)
		var got ProviderMetadataInput
		if err := json.Unmarshal(raw, &got); err != nil || got != input {
			t.Fatal(got, err)
		}
	}
	for _, input := range []ProviderMetadataInput{{strings.Repeat("😀", 101), "R"}, {"A", strings.Repeat("r", 1025)}, {"A", strings.Repeat("😀", 257)}} {
		raw, _ := json.Marshal(input)
		var got ProviderMetadataInput
		if json.Unmarshal(raw, &got) == nil {
			t.Fatal("boundary accepted")
		}
	}
}

func TestProviderMetadataIdentityCurrentContentAndBirth(t *testing.T) {
	actor, provider, _ := connectionMetadataTestRows()
	original, err := providerMetadataRecord(actor, provider, true)
	if err != nil || !validConnectionMetadataETag(original.ETag) {
		t.Fatal(original, err)
	}
	input := ProviderMetadataInput{"Changed", "Reviewed"}
	if providerMetadataReview(original, original.ETag, input) != nil {
		t.Fatal("fresh review rejected")
	}
	for name, change := range map[string]func(*entity.User, *entity.Provider){
		"actor_id":       func(a *entity.User, _ *entity.Provider) { a.ID = "usr_other" },
		"actor_birth":    func(a *entity.User, _ *entity.Provider) { a.CreatedAt = a.CreatedAt.Add(time.Millisecond) },
		"provider_id":    func(_ *entity.User, p *entity.Provider) { p.ID = "prv_other" },
		"provider_birth": func(_ *entity.User, p *entity.Provider) { p.CreatedAt = p.CreatedAt.Add(time.Millisecond) },
	} {
		t.Run(name, func(t *testing.T) {
			a, p := actor, provider
			change(&a, &p)
			p.Name = input.Name
			record, err := providerMetadataRecord(a, p, true)
			if err != nil || providerMetadataReview(record, original.ETag, input) != catalogConflict {
				t.Fatal("same-value identity bypass", record, err)
			}
		})
	}
	later := provider
	later.Name = "Concurrent"
	changed, _ := providerMetadataRecord(actor, later, true)
	if changed.ETag[:64] != original.ETag[:64] || changed.ETag == original.ETag || providerMetadataReview(changed, original.ETag, input) != catalogConflict {
		t.Fatal("stale content accepted")
	}
	later.Name = input.Name
	saved, _ := providerMetadataRecord(actor, later, true)
	if providerMetadataReview(saved, original.ETag, input) != nil {
		t.Fatal("exact current reconciliation rejected")
	}
	later.Name = provider.Name
	later.ETag = "rev_returned"
	returned, _ := providerMetadataRecord(actor, later, true)
	if returned.ETag == original.ETag || providerMetadataReview(returned, original.ETag, input) != catalogConflict {
		t.Fatal("content ABA retained stale review")
	}
	a, p := actor, provider
	a.CreatedAt = a.CreatedAt.In(time.FixedZone("offset", 28800))
	p.CreatedAt = p.CreatedAt.In(time.FixedZone("offset", 28800))
	normalized, _ := providerMetadataRecord(a, p, false)
	if normalized.ETag != original.ETag {
		t.Fatal("permissions/timezone affect stored-content token")
	}
	for _, birth := range []time.Time{{}, time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(1, 1, 1, 0, 0, 0, 0, time.FixedZone("offset", 28800)), time.Date(9999, 12, 31, 23, 59, 0, 0, time.FixedZone("offset", -28800))} {
		for _, actorFault := range []bool{false, true} {
			a, p := actor, provider
			if actorFault {
				a.CreatedAt = birth
			} else {
				p.CreatedAt = birth
			}
			if _, err := providerMetadataRecord(a, p, true); err != providerMetadataUnavailable {
				t.Fatal("invalid UTC birth accepted", birth, actorFault)
			}
		}
	}
	for _, name := range []string{"", " Invalid", "Invalid\n", strings.Repeat("a", 101)} {
		p := provider
		p.Name = name
		if _, err := providerMetadataRecord(actor, p, true); err != providerMetadataUnavailable {
			t.Fatal("corrupt name accepted")
		}
	}
	raw, _ := json.Marshal(original)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 4 || fields["id"] == nil || fields["name"] == nil || fields["etag"] == nil || fields["can_edit"] == nil {
		t.Fatal(string(raw))
	}
	raw, _ = json.Marshal(ProviderMetadataWriteResult{original, true, false})
	fields = nil
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 3 {
		t.Fatal(string(raw))
	}
	for _, ids := range [][2]string{{"USR_ADMIN", "prv_target"}, {"usr_admin", "PRV_TARGET"}, {"usr_admin", "prv_target "}, {"usr_admin", "con_target"}, {"usr_admin", "prv_" + strings.Repeat("x", 27)}} {
		if providerMetadataIDs(ids[0], ids[1]) == nil {
			t.Fatal("unsafe identity accepted", ids)
		}
	}
}
func TestProviderMetadataPortableNameOnlyGORMUpdate(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Register("test:provider-name-only", func(q *gorm.DB) {
				q.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
				callbacks.Update(&callbacks.Config{})(q)
			}); err != nil {
				t.Fatal(err)
			}
			stmt := providerMetadataQuery(db, "prv_CASE").Updates(map[string]any{"Name": "Exact", "ETag": "rev_changed"}).Statement
			sql := stmt.SQL.String()
			if !strings.Contains(sql, "name") || strings.Contains(sql, "created_at") || !strings.Contains(sql, "e_tag") || strings.Contains(sql, "enabled") || !reflect.DeepEqual(stmt.Vars, []any{"rev_changed", "Exact", "prv_CASE"}) || dialect.Name() == "mysql" && !strings.Contains(sql, "CAST(") {
				t.Fatal(sql, stmt.Vars)
			}
		})
	}
}
func TestProviderMetadataTypedAuditAndRuntimeNameProof(t *testing.T) {
	text := `{"before":{"name":"Original"},"after":{"name":"Changed"},"reason":"Reviewed"}`
	row := entity.AuditEvent{ActorID: "usr_admin", ResourceType: "provider", ResourceID: "prv_target", Action: "provider.metadata.update", DetailsJSON: &text}
	if _, ok := providerMetadataAuditProjection(row); !ok || len(auditRecord(row).Changes) == 0 {
		t.Fatal("typed event missing")
	}
	for _, bad := range []string{strings.Replace(text, `"name":"Original"`, `"name":"Original","secret":"private"`, 1), strings.Replace(text, `"reason":"Reviewed"`, `"reason":null`, 1), strings.Replace(text, `"Changed"`, `"Original"`, 1), strings.Replace(text, `"reason":"Reviewed"`, `"reason":"Reviewed","private":"secret"`, 1)} {
		r := row
		r.DetailsJSON = &bad
		if _, ok := providerMetadataAuditProjection(r); ok || len(auditRecord(r).Changes) != 0 {
			t.Fatal("malformed audit exposed", bad)
		}
	}
	historical := row
	historical.Action = "provider.create"
	historical.DetailsJSON = nil
	if len(auditRecord(historical).Changes) != 0 {
		t.Fatal("legacy history invented")
	}
	_, provider, _ := connectionMetadataTestRows()
	data := &runtimeData{Providers: []entity.Provider{provider}}
	digest, _ := runtimeDigest(data)
	runtime := &gatewayRuntime{done: make(chan struct{})}
	runtime.auth.Store(&runtimeAuthorization{SourceDigest: digest, ValidUntil: time.Now().Add(time.Minute)})
	runtime.routes.Store(&runtimeRoutes{Digest: digest})
	s := &Service{runtime: runtime}
	if !s.connectionMetadataRuntimeApplied(digest, 0) {
		t.Fatal("published no-route provider unconfirmed")
	}
	data.Providers[0].Name = "Changed"
	changed, _ := runtimeDigest(data)
	if changed == digest || s.connectionMetadataRuntimeApplied(changed, 0) {
		t.Fatal("old Provider name digest accepted")
	}
	close(runtime.done)
	if s.connectionMetadataRuntimeApplied(digest, 0) {
		t.Fatal("closed runtime accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.GetProviderMetadata(ctx, "invalid", "prv_target"); err != apperrors.ErrUnauthorized {
		t.Fatal(err)
	}
}
