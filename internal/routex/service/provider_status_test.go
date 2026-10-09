package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProviderStatusStrictBody(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"enabled":null,"reason":"R"}`, `{"enabled":1,"reason":"R"}`, `{"enabled":"false","reason":"R"}`, `{"enabled":false,"reason":null}`, `{"enabled":false,"reason":" R"}`, `{"enabled":false,"reason":"R","name":"N"}`, `{"enabled":false,"enabled":true,"reason":"R"}`, `{"enabled":false,"reason":"\ud800"}`, `{"enabled":false,"reason":"R"}{}`, `{"enabled":false,"reason":""}`} {
		var input ProviderStatusInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("ambiguous status accepted", raw)
		}
	}
	for _, enabled := range []bool{false, true} {
		original := ProviderStatusInput{Enabled: enabled, Reason: strings.Repeat("r", 1024)}
		raw, _ := json.Marshal(original)
		var got ProviderStatusInput
		if err := json.Unmarshal(raw, &got); err != nil || got != original {
			t.Fatal(got, err)
		}
	}
	for _, reason := range []string{strings.Repeat("r", 1025), strings.Repeat("😀", 257), "R\n"} {
		raw, _ := json.Marshal(ProviderStatusInput{Reason: reason})
		var got ProviderStatusInput
		if json.Unmarshal(raw, &got) == nil {
			t.Fatal("invalid reason accepted")
		}
	}
}
func TestProviderStatusSharedRevisionAndCurrentOnlyRetry(t *testing.T) {
	actor, provider, _ := connectionMetadataTestRows()
	provider.Enabled = true
	provider.ETag = "rev_initial"
	metadata, _ := providerMetadataRecord(actor, provider, true)
	initial := providerStatusRecord(metadata, provider)
	if initial.ETag == metadata.ETag || providerStatusReview(initial, initial.ETag, false) != nil {
		t.Fatal("purpose review")
	}
	provider.Enabled = false
	provider.ETag = "rev_status"
	nextMeta, _ := providerMetadataRecord(actor, provider, true)
	next := providerStatusRecord(nextMeta, provider)
	if nextMeta.ETag == metadata.ETag || providerMetadataReview(nextMeta, metadata.ETag, ProviderMetadataInput{Name: "Changed", Reason: "R"}) != catalogConflict {
		t.Fatal("status left metadata token current")
	}
	if providerStatusReview(next, initial.ETag, false) != nil || providerStatusReview(next, initial.ETag, true) != catalogConflict {
		t.Fatal("retry does not describe current state")
	}
	provider.Enabled = true
	provider.ETag = "rev_returned"
	returnedMeta, _ := providerMetadataRecord(actor, provider, true)
	returned := providerStatusRecord(returnedMeta, provider)
	if returned.ETag == initial.ETag || providerStatusReview(returned, initial.ETag, false) != catalogConflict {
		t.Fatal("ABA stale operation accepted")
	}
	provider.Name = "Renamed"
	provider.ETag = "rev_name"
	renamedMeta, _ := providerMetadataRecord(actor, provider, true)
	if providerStatusReview(providerStatusRecord(renamedMeta, provider), returned.ETag, false) != catalogConflict {
		t.Fatal("name write left status review current")
	}
	for _, change := range []func(*entity.User, *entity.Provider){func(a *entity.User, _ *entity.Provider) { a.ID = "usr_other" }, func(a *entity.User, _ *entity.Provider) { a.CreatedAt = a.CreatedAt.Add(time.Second) }, func(_ *entity.User, p *entity.Provider) { p.ID = "prv_other" }, func(_ *entity.User, p *entity.Provider) { p.CreatedAt = p.CreatedAt.Add(time.Second) }} {
		a, p := actor, provider
		change(&a, &p)
		p.Enabled = false
		m, _ := providerMetadataRecord(a, p, true)
		if providerStatusReview(providerStatusRecord(m, p), initial.ETag, false) != catalogConflict {
			t.Fatal("identity alias reconciled")
		}
	}
	raw, _ := json.Marshal(initial)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 5 || fields["enabled"] == nil || fields["id"] == nil || fields["name"] == nil || fields["etag"] == nil || fields["can_edit"] == nil {
		t.Fatal(string(raw))
	}
}
func TestProviderStatusPortableFalseUpdatePreservesChildren(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Register("test:provider-status", func(q *gorm.DB) {
				q.Statement.BuildClauses = []string{"UPDATE", "SET", "WHERE"}
				callbacks.Update(&callbacks.Config{})(q)
			}); err != nil {
				t.Fatal(err)
			}
			stmt := providerMetadataQuery(db, "prv_CASE").Updates(map[string]any{"enabled": false, "ETag": "rev_status"}).Statement
			if !reflect.DeepEqual(stmt.Vars, []any{"rev_status", false, "prv_CASE"}) {
				t.Fatal(stmt.SQL.String(), stmt.Vars)
			}
			for _, field := range []string{"provider_connections", "provider_credentials", "provider_models", "weight", "created_at", "name"} {
				if strings.Contains(stmt.SQL.String(), field) {
					t.Fatal("unrelated field altered", stmt.SQL.String())
				}
			}
		})
	}
}
func TestProviderStatusTypedAudit(t *testing.T) {
	raw := `{"before":true,"after":false,"reason":"Reviewed"}`
	row := entity.AuditEvent{ActorID: "usr_admin", ResourceID: "prv_target", ResourceType: "provider", Action: "provider.status.update", DetailsJSON: &raw}
	if _, ok := providerStatusAuditProjection(row); !ok || len(auditRecord(row).Changes) == 0 {
		t.Fatal("typed audit missing")
	}
	for _, bad := range []string{`{"before":false,"after":false,"reason":"R"}`, `{"before":1,"after":false,"reason":"R"}`, `{"before":true,"after":false,"reason":""}`, `{"before":true,"after":false,"reason":"R","secret":"bad"}`} {
		row.DetailsJSON = &bad
		if _, ok := providerStatusAuditProjection(row); ok || len(auditRecord(row).Changes) != 0 {
			t.Fatal("malformed audit exposed")
		}
	}
}
