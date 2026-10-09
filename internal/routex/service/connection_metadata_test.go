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

func connectionMetadataTestRows() (entity.User, entity.Provider, entity.ProviderConnection) {
	birth := time.Date(2026, 10, 6, 0, 0, 0, 123456000, time.UTC)
	return entity.User{ID: "usr_admin", Role: entity.RoleAdmin, CreatedAt: birth}, entity.Provider{ID: "prv_target", Name: "Provider", Enabled: true, ETag: "0", CreatedAt: birth}, entity.ProviderConnection{TransportGeneration: "0", ID: "con_target", ProviderID: "prv_target", Name: "Original", BaseURL: "https://example.invalid/v1", Protocol: "openai_chat", ETag: "0", EgressMode: "default", CreatedAt: birth}
}
func TestConnectionMetadataStrictIntentBounds(t *testing.T) {
	invalid := []string{`{}`, `null`, `[]`, `{"name":"A","reason":null}`, `{"name":null,"reason":"R"}`, `{"Name":"A","reason":"R"}`, `{"name":"A","reason":"R","egress_mode":"direct"}`, `{"name":"A","reason":"R","name":"B"}`, `{"name":"A","reason":"R","\u006eame":"B"}`, `{"name":"A","reason":"R"} {}`, `{"name":"\ud800","reason":"R"}`, `{"name":"A","reason":"\udfff"}`, `{"name":" A","reason":"R"}`, `{"name":"A","reason":" R"}`, `{"name":"A\n","reason":"R"}`, `{"name":"A","reason":""}`, string([]byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 255, '"', '}'})}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			var input ConnectionMetadataInput
			if json.Unmarshal([]byte(raw), &input) == nil {
				t.Fatal("ambiguous operation accepted")
			}
		})
	}
	for _, input := range []ConnectionMetadataInput{{Name: strings.Repeat("😀", 100), Reason: strings.Repeat("r", 1024)}, {Name: "O'Neil", Reason: "Exact reason"}} {
		raw, _ := json.Marshal(input)
		var got ConnectionMetadataInput
		if err := json.Unmarshal(raw, &got); err != nil || got != input {
			t.Fatal(got, err)
		}
	}
	for _, input := range []ConnectionMetadataInput{{Name: strings.Repeat("😀", 101), Reason: "R"}, {Name: "A", Reason: strings.Repeat("r", 1025)}, {Name: "A", Reason: strings.Repeat("😀", 257)}} {
		raw, _ := json.Marshal(input)
		var got ConnectionMetadataInput
		if json.Unmarshal(raw, &got) == nil {
			t.Fatal("boundary accepted")
		}
	}
}
func TestConnectionMetadataIdentitySharedRevisionAndReconciliation(t *testing.T) {
	actor, provider, row := connectionMetadataTestRows()
	original, err := connectionMetadataRecord(actor, provider, row, true)
	if err != nil || !validConnectionMetadataETag(original.ETag) {
		t.Fatal(original, err)
	}
	input := ConnectionMetadataInput{Name: "Changed", Reason: "Reviewed rename"}
	if connectionMetadataReview(original, original.ETag, input) != nil {
		t.Fatal("fresh write rejected")
	}
	for name, change := range map[string]func(*entity.User, *entity.Provider, *entity.ProviderConnection){
		"actor_id": func(a *entity.User, _ *entity.Provider, _ *entity.ProviderConnection) { a.ID = "usr_other" },
		"actor_birth": func(a *entity.User, _ *entity.Provider, _ *entity.ProviderConnection) {
			a.CreatedAt = a.CreatedAt.Add(time.Millisecond)
		},
		"provider_birth": func(_ *entity.User, p *entity.Provider, _ *entity.ProviderConnection) {
			p.CreatedAt = p.CreatedAt.Add(time.Millisecond)
		},
		"connection_birth": func(_ *entity.User, _ *entity.Provider, c *entity.ProviderConnection) {
			c.CreatedAt = c.CreatedAt.Add(time.Millisecond)
		},
		"provider_identity": func(_ *entity.User, p *entity.Provider, c *entity.ProviderConnection) {
			p.ID = "prv_other"
			c.ProviderID = p.ID
		},
		"connection_identity": func(_ *entity.User, _ *entity.Provider, c *entity.ProviderConnection) { c.ID = "con_other" },
	} {
		t.Run(name, func(t *testing.T) {
			a, p, c := actor, provider, row
			change(&a, &p, &c)
			c.Name = input.Name
			record, err := connectionMetadataRecord(a, p, c, true)
			if err != nil || connectionMetadataReview(record, original.ETag, input) != catalogConflict {
				t.Fatal("same-value identity bypass", record, err)
			}
		})
	}
	for name, change := range map[string]func(*entity.ProviderConnection){
		"egress_shared_revision": func(c *entity.ProviderConnection) { c.ETag = "rev_later" },
		"egress_mode":            func(c *entity.ProviderConnection) { c.EgressMode = "direct" },
		"base_url":               func(c *entity.ProviderConnection) { c.BaseURL = "https://later.invalid" },
		"protocol":               func(c *entity.ProviderConnection) { c.Protocol = "openai_responses" },
	} {
		t.Run(name, func(t *testing.T) {
			c := row
			change(&c)
			record, err := connectionMetadataRecord(actor, provider, c, true)
			if err != nil || record.ETag[:64] != original.ETag[:64] || connectionMetadataReview(record, original.ETag, input) != catalogConflict {
				t.Fatal("concurrent configuration accepted", err)
			}
			c.Name = input.Name
			record, _ = connectionMetadataRecord(actor, provider, c, true)
			if connectionMetadataReview(record, original.ETag, input) != nil {
				t.Fatal("same-identity current-value retry denied")
			}
		})
	}
	a, p, c := actor, provider, row
	a.CreatedAt = a.CreatedAt.In(time.FixedZone("offset", 28800))
	p.CreatedAt = p.CreatedAt.In(time.FixedZone("offset", 28800))
	c.CreatedAt = c.CreatedAt.In(time.FixedZone("offset", 28800))
	same, _ := connectionMetadataRecord(a, p, c, true)
	if same.ETag != original.ETag {
		t.Fatal("driver timezone changed identity")
	}
	for _, birth := range []time.Time{{}, time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		c := row
		c.CreatedAt = birth
		if _, err := connectionMetadataRecord(actor, provider, c, true); err != connectionMetadataUnavailable {
			t.Fatal("invalid birth accepted")
		}
	}
	raw, _ := json.Marshal(original)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 14 || string(fields["transport_generation"]) != `"0"` || string(fields["can_edit_transport"]) != "true" || string(fields["transport_locked"]) != "false" || string(fields["adapter"]) != `"native"` || string(fields["api_version"]) != "null" || fields["created_at"] != nil || fields["ciphertext"] != nil {
		t.Fatal(string(raw))
	}
	for _, bad := range []string{"", strings.Repeat("a", 129), original.ETag[:64] + "/" + original.ETag[65:], strings.ToUpper(original.ETag)} {
		if validConnectionMetadataETag(bad) {
			t.Fatal("bad token accepted")
		}
	}
}
func TestConnectionMetadataRuntimeConfirmationIsCurrentSourceOnly(t *testing.T) {
	_, provider, row := connectionMetadataTestRows()
	data := &runtimeData{Providers: []entity.Provider{provider}, Connections: []entity.ProviderConnection{row}}
	digest, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &gatewayRuntime{done: make(chan struct{})}
	runtime.auth.Store(&runtimeAuthorization{SourceDigest: digest, ValidUntil: time.Now().Add(time.Minute)})
	runtime.routes.Store(&runtimeRoutes{Digest: digest})
	s := &Service{runtime: runtime}
	// Complete source publication can confirm configuration without any ready
	// route, credential or egress eligibility. It never reports inference success.
	if !s.connectionMetadataRuntimeApplied(digest, 0) {
		t.Fatal("published no-route configuration unconfirmed")
	}
	changed := row
	changed.Name = "Later"
	next := &runtimeData{Providers: data.Providers, Connections: []entity.ProviderConnection{changed}}
	nextDigest, _ := runtimeDigest(next)
	if nextDigest == digest || s.connectionMetadataRuntimeApplied(nextDigest, 0) {
		t.Fatal("name omitted from runtime digest")
	}
	for _, change := range []func(){func() { s.egressGeneration.Store(1) }, func() {
		runtime.auth.Store(&runtimeAuthorization{SourceDigest: digest, ValidUntil: time.Now().Add(-time.Second)})
	}, func() { runtime.routes.Store(&runtimeRoutes{Digest: "different"}) }} {
		change()
		if s.connectionMetadataRuntimeApplied(digest, 0) {
			t.Fatal("stale publication accepted")
		}
		s.egressGeneration.Store(0)
		runtime.auth.Store(&runtimeAuthorization{SourceDigest: digest, ValidUntil: time.Now().Add(time.Minute)})
		runtime.routes.Store(&runtimeRoutes{Digest: digest})
	}
	runtime.publication.Lock()
	if s.connectionMetadataRuntimeApplied(digest, 0) {
		t.Fatal("exclusive publication bypass")
	}
	runtime.publication.Unlock()
	runtime.mu.Lock()
	if s.connectionMetadataRuntimeApplied(digest, 0) {
		t.Fatal("runtime mutex bypass")
	}
	runtime.mu.Unlock()
	close(runtime.done)
	if s.connectionMetadataRuntimeApplied(digest, 0) {
		t.Fatal("closed runtime confirmed")
	}
}
func TestConnectionMetadataPortableExactQueriesAndNameOnlyUpdate(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Register("test:connection-metadata-update", func(q *gorm.DB) {
				q.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
				callbacks.Update(&callbacks.Config{})(q)
			}); err != nil {
				t.Fatal(err)
			}
			stmt := connectionMetadataQuery(db, "con_CASE").Updates(map[string]any{"name": "Exact", "etag": "rev_new"}).Statement
			sql := stmt.SQL.String()
			if dialect.Name() == "mysql" && !strings.Contains(sql, "CAST(") || !strings.Contains(sql, "name") || !strings.Contains(sql, "etag") || strings.Contains(sql, "egress_mode") || strings.Contains(sql, "base_url") || !reflect.DeepEqual(stmt.Vars, []any{"rev_new", "Exact", "con_CASE"}) {
				t.Fatal(sql, stmt.Vars)
			}
		})
	}
}
func TestConnectionMetadataTypedAuditFailsClosedAndPreservesLegacy(t *testing.T) {
	detail := connectionMetadataAudit{Before: connectionMetadataValues{"Original"}, After: connectionMetadataValues{"Changed"}, Reason: "Reviewed"}
	raw, _ := json.Marshal(detail)
	text := string(raw)
	row := entity.AuditEvent{ActorID: "usr_admin", ResourceType: "connection", ResourceID: "con_target", Action: "connection.metadata.update", DetailsJSON: &text}
	if _, ok := connectionMetadataAuditProjection(row); !ok || len(auditRecord(row).Changes) == 0 {
		t.Fatal("typed event missing")
	}
	for _, bad := range []string{strings.Replace(text, `"name":"Original"`, `"name":"Original","ciphertext":"secret"`, 1), strings.Replace(text, `"reason":"Reviewed"`, `"reason":null`, 1), strings.Replace(text, `"Changed"`, `"Original"`, 1), strings.Replace(text, `"reason":"Reviewed"`, `"reason":"Reviewed","private":"secret"`, 1)} {
		r := row
		r.DetailsJSON = &bad
		if _, ok := connectionMetadataAuditProjection(r); ok || len(auditRecord(r).Changes) != 0 {
			t.Fatal("unsafe audit projection", bad)
		}
	}
	legacy := `{"before":{"name":"Original","priority":1},"after":{"name":"Changed","priority":2},"reason":"Reviewed"}`
	row.Action = "credential.metadata.update"
	row.ResourceType = "credential"
	row.ResourceID = "cre_target"
	row.DetailsJSON = &legacy
	if len(auditRecord(row).Changes) == 0 {
		t.Fatal("legacy credential projection changed")
	}
	if _, err := (&Service{}).WriteConnectionMetadata(context.Background(), "usr_admin", "con_target", strings.Repeat("a", 64), ConnectionMetadataInput{Name: "A", Reason: "R"}); err != apperrors.ErrBadRequest {
		t.Fatal(err)
	}
}

func TestConnectionMetadataUTCYearBoundaryFailsClosed(t *testing.T) {
	actor, provider, row := connectionMetadataTestRows()
	// Both local years are positive/supported, but the normalized identity birth
	// crosses the supported UTC range. The upper value cannot be JSON encoded.
	for _, birth := range []time.Time{time.Date(9999, 12, 31, 23, 30, 0, 0, time.FixedZone("negative", -3600)), time.Date(1, 1, 1, 0, 30, 0, 0, time.FixedZone("positive", 3600))} {
		if birth.Year() < 1 || birth.Year() > 9999 || birth.UTC().Year() >= 1 && birth.UTC().Year() <= 9999 {
			t.Fatal("boundary fixture is not pathological", birth)
		}
		for _, target := range []string{"actor", "provider", "connection"} {
			t.Run(target+birth.Format(time.RFC3339), func(t *testing.T) {
				a, p, c := actor, provider, row
				switch target {
				case "actor":
					a.CreatedAt = birth
				case "provider":
					p.CreatedAt = birth
				case "connection":
					c.CreatedAt = birth
				}
				record, err := connectionMetadataRecord(a, p, c, true)
				if err != connectionMetadataUnavailable || record.ETag != "" {
					t.Fatal("out-of-range UTC birth produced identity", record, err)
				}
			})
		}
	}
	// Valid normalized boundary identities remain valid; no wider UTC-only
	// serialization change is made to public records or ordinary timestamps.
	for _, birth := range []time.Time{time.Date(1, 1, 1, 1, 30, 0, 0, time.FixedZone("positive", 3600)), time.Date(9999, 12, 31, 22, 30, 0, 0, time.FixedZone("negative", -3600))} {
		c := row
		c.CreatedAt = birth
		record, err := connectionMetadataRecord(actor, provider, c, true)
		if err != nil || !validConnectionMetadataETag(record.ETag) {
			t.Fatal("valid normalized birth rejected", record, err)
		}
	}
}
