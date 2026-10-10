package service

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func oauthAuditProjectionFixture(t *testing.T, action string) entity.AuditEvent {
	t.Helper()
	birth := time.Date(2026, 10, 1, 2, 3, 4, 123456000, time.UTC)
	u := entity.User{ID: "usr_auditor", CreatedAt: birth}
	p := entity.OAuthProvider{ID: "oauth", CreatedAt: birth.Add(time.Minute), ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64), AuthorizationURL: "https://private-issuer.example", ClientID: "private-client", AuthCiphertext: "private-ciphertext"}
	b := entity.OAuthBinding{ID: "oab_retained", ProviderID: "oauth", SubjectKind: "string", UserID: u.ID, UserCreatedAt: u.CreatedAt, CreatedAt: birth.Add(2 * time.Minute), ConfigRevision: p.ConfigRevision, Subject: "private-subject", SubjectDigest: strings.Repeat("d", 64)}
	var binding *entity.OAuthBinding
	resourceType, resourceID := "oauth_provider", "oauth"
	switch action {
	case "identity.oauth.verify":
		binding = &b
	case "account.oauth.bind", "account.oauth.unlink":
		binding = &b
		resourceType, resourceID = "oauth_binding", b.ID
	}
	raw, err := oauthAuditJSON(u, p, binding, "Review access <script>not markup</script> 原因")
	if err != nil {
		t.Fatal("writer fixture", err)
	}
	return entity.AuditEvent{ID: "aud_retained", ActorID: u.ID, Action: action, ResourceType: resourceType, ResourceID: resourceID, CreatedAt: birth.Add(time.Hour), DetailsJSON: &raw}
}

func TestOAuthAuditProjectionPublishesOnlyTypedReason(t *testing.T) {
	for _, action := range []string{"identity.oauth.config.update", "identity.oauth.status.update", "identity.oauth.verify", "account.oauth.bind", "account.oauth.unlink"} {
		t.Run(action, func(t *testing.T) {
			row := oauthAuditProjectionFixture(t, action)
			changes, valid := oauthAuditProjection(row)
			if !valid || changes.Kind != "oauth_identity" || changes.Reason != "Review access <script>not markup</script> 原因" {
				t.Fatal("writer/projection contract", valid, changes)
			}
			record := auditRecord(row)
			if record.ActorID != row.ActorID || record.ResourceID != row.ResourceID || record.ResourceType != row.ResourceType || record.Action != row.Action {
				t.Fatal("exact envelope changed")
			}
			var public map[string]json.RawMessage
			if err := json.Unmarshal(record.Changes, &public); err != nil || len(public) != 2 || string(public["kind"]) != `"oauth_identity"` || public["reason"] == nil {
				t.Fatal("unbounded public projection", err)
			}
			for _, private := range []string{"actor_created_at", "provider_created_at", "binding_created_at", "review_revision", "config_revision", "policy_revision", "private-issuer", "private-client", "private-ciphertext", "private-subject", "subject_digest", "before", "after"} {
				if strings.Contains(string(record.Changes), private) {
					t.Fatal("private or invented audit field exposed", private)
				}
			}
		})
	}
	for _, resource := range []string{"oauth_provider", "oauth_binding"} {
		if !slices.Contains(auditCategories["identity"], resource) {
			t.Fatal("identity filter omits committed external identity events", resource)
		}
	}
}

func TestOAuthAuditProjectionRejectsUnknownAndMalformedDetails(t *testing.T) {
	cases := map[string]func(*entity.AuditEvent, map[string]any){
		"unknown version":                   func(_ *entity.AuditEvent, d map[string]any) { d["version"] = 2 },
		"missing version":                   func(_ *entity.AuditEvent, d map[string]any) { delete(d, "version") },
		"string version":                    func(_ *entity.AuditEvent, d map[string]any) { d["version"] = "1" },
		"null reason":                       func(_ *entity.AuditEvent, d map[string]any) { d["reason"] = nil },
		"empty reason":                      func(_ *entity.AuditEvent, d map[string]any) { d["reason"] = "" },
		"untrimmed reason":                  func(_ *entity.AuditEvent, d map[string]any) { d["reason"] = " review " },
		"control reason":                    func(_ *entity.AuditEvent, d map[string]any) { d["reason"] = "review\nsecret" },
		"oversize reason":                   func(_ *entity.AuditEvent, d map[string]any) { d["reason"] = strings.Repeat("x", 1025) },
		"actor alias":                       func(r *entity.AuditEvent, _ map[string]any) { r.ActorID = "USR_auditor" },
		"actor wrong kind":                  func(r *entity.AuditEvent, _ map[string]any) { r.ActorID = "oab_auditor" },
		"actor empty suffix":                func(r *entity.AuditEvent, _ map[string]any) { r.ActorID = "usr_" },
		"actor oversized":                   func(r *entity.AuditEvent, _ map[string]any) { r.ActorID = "usr_" + strings.Repeat("x", 27) },
		"actor zero birth":                  func(_ *entity.AuditEvent, d map[string]any) { d["actor_created_at"] = "0001-01-01T00:00:00Z" },
		"actor missing birth":               func(_ *entity.AuditEvent, d map[string]any) { delete(d, "actor_created_at") },
		"actor submicro birth":              func(_ *entity.AuditEvent, d map[string]any) { d["actor_created_at"] = "2026-10-01T02:03:04.123456789Z" },
		"provider case alias":               func(_ *entity.AuditEvent, d map[string]any) { d["provider_id"] = "OAuth" },
		"provider zero birth":               func(_ *entity.AuditEvent, d map[string]any) { d["provider_created_at"] = "0001-01-01T00:00:00Z" },
		"provider malformed birth":          func(_ *entity.AuditEvent, d map[string]any) { d["provider_created_at"] = "not-a-time" },
		"review unknown revision":           func(_ *entity.AuditEvent, d map[string]any) { d["review_revision"] = "" },
		"config alias revision":             func(_ *entity.AuditEvent, d map[string]any) { d["config_revision"] = strings.Repeat("B", 64) },
		"policy missing revision":           func(_ *entity.AuditEvent, d map[string]any) { delete(d, "policy_revision") },
		"binding case alias":                func(_ *entity.AuditEvent, d map[string]any) { d["binding_id"] = "OAB_retained" },
		"binding mismatched exact resource": func(_ *entity.AuditEvent, d map[string]any) { d["binding_id"] = "oab_Retained" },
		"binding zero birth":                func(_ *entity.AuditEvent, d map[string]any) { d["binding_created_at"] = "0001-01-01T00:00:00Z" },
		"binding null birth":                func(_ *entity.AuditEvent, d map[string]any) { d["binding_created_at"] = nil },
		"binding missing birth":             func(_ *entity.AuditEvent, d map[string]any) { delete(d, "binding_created_at") },
		"resource wrong type":               func(r *entity.AuditEvent, _ map[string]any) { r.ResourceType = "user" },
		"resource exact mismatch":           func(r *entity.AuditEvent, _ map[string]any) { r.ResourceID = "oab_other" },
		"unknown action":                    func(r *entity.AuditEvent, _ map[string]any) { r.Action = "account.oauth.future" },
		"wrong known action":                func(r *entity.AuditEvent, _ map[string]any) { r.Action = "identity.oauth.verify" },
	}
	for _, name := range []string{"issuer", "subject", "subject_digest", "access_token", "id_token", "refresh_token", "client_secret", "pkce_verifier", "before", "after"} {
		cases["unknown field "+name] = func(_ *entity.AuditEvent, d map[string]any) { d[name] = "PRIVATE_SENTINEL" }
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			row := oauthAuditProjectionFixture(t, "account.oauth.bind")
			if _, valid := oauthAuditProjection(row); !valid {
				t.Fatal("invalid positive projection baseline")
			}
			var detail map[string]any
			if err := json.Unmarshal([]byte(*row.DetailsJSON), &detail); err != nil {
				t.Fatal(err)
			}
			mutate(&row, detail)
			raw, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			value := string(raw)
			row.DetailsJSON = &value
			if _, valid := oauthAuditProjection(row); valid || auditRecord(row).Changes != nil {
				t.Fatal("malformed/private audit became public", name)
			}
		})
	}
}

func TestOAuthAuditProjectionRejectsAmbiguousJSONAndActionShapes(t *testing.T) {
	base := oauthAuditProjectionFixture(t, "identity.oauth.verify")
	reasonJSON, err := json.Marshal("Review access <script>not markup</script> 原因")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"duplicate reason": strings.Replace(*base.DetailsJSON, `"version":1`, `"version":1,"reason":"PRIVATE_SENTINEL"`, 1),
		"trailing object":  *base.DetailsJSON + `{}`,
		"invalid UTF8":     strings.Replace(*base.DetailsJSON, "Review access", string([]byte{255}), 1),
		"invalid Unicode":  strings.Replace(*base.DetailsJSON, string(reasonJSON), `"\ud800"`, 1),
		"oversize object":  *base.DetailsJSON + strings.Repeat(" ", 8193-len(*base.DetailsJSON)),
		"array":            `[]`,
		"null":             `null`,
	} {
		t.Run(name, func(t *testing.T) {
			row := base
			row.DetailsJSON = &raw
			if _, valid := oauthAuditProjection(row); valid || auditRecord(row).Changes != nil {
				t.Fatal("ambiguous details exposed")
			}
		})
	}
	row := base
	row.DetailsJSON = nil
	if _, valid := oauthAuditProjection(row); valid || auditRecord(row).Changes != nil {
		t.Fatal("unknown details invented")
	}
	for _, action := range []string{"identity.oauth.config.update", "identity.oauth.status.update"} {
		row := base
		row.Action = action
		if _, valid := oauthAuditProjection(row); valid {
			t.Fatal("unexpected binding proof admitted for configuration change")
		}
	}
	row = oauthAuditProjectionFixture(t, "identity.oauth.config.update")
	row.ResourceID = "OAuth"
	if _, valid := oauthAuditProjection(row); valid {
		t.Fatal("provider envelope collation alias admitted")
	}
	row = oauthAuditProjectionFixture(t, "identity.oauth.config.update")
	row.Action = "identity.oauth.verify"
	if _, valid := oauthAuditProjection(row); valid {
		t.Fatal("verification without binding proof invented")
	}
}
