package service

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

const teamMediaTestID = "obj_01arz3ndektsv4rrffq69g5fav"

func teamMediaRow(now time.Time) entity.StorageObject {
	user, member := "usr_creator", "tmb_original"
	expires := now.Add(attachmentReadyTTL)
	return entity.StorageObject{ID: teamMediaTestID, OwnerKind: entity.StorageOwnerTeam, OwnerID: "tem_original", CreatorUserID: &user, CreatorMembershipID: &member, Purpose: "attachment", State: "ready", CreatedAt: now, ExpiresAt: &expires, NextCleanupAt: expires, MIME: "image/png", Size: 10}
}

func TestTeamAttachmentDiscoveryIntersectCurrentEligibleRoutes(t *testing.T) {
	svc, identity := teamGatewayFixture(t, "https://provider-one.example/v1")
	routes := svc.runtime.routes.Load()
	routes.Models["mdl_one"][0].Route.SupportsImageInput = true
	routes.Models["mdl_one"][0].Route.SupportsPDFInput = true
	models, err := svc.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || !slices.Equal(models[0].InputCapabilities[entity.ProtocolOpenAIChat], []string{"image", "pdf"}) || models[0].AttachmentTeamID != identity.TeamID || models[0].AttachmentMembershipID != identity.TeamMembershipID || models[0].PersonalAttachments || models[0].AttachmentProjectID != "" {
		t.Fatal("Team discovery lost exact private scope or capabilities", models, err)
	}
	second := routes.Models["mdl_one"][0]
	second.Route.BindingID = "bnd_second"
	second.Route.Weight = 50
	second.Route.SupportsPDFInput = false
	routes.Models["mdl_one"][0].Route.Weight = 50
	routes.Models["mdl_one"] = append(routes.Models["mdl_one"], second)
	models, err = svc.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || !slices.Equal(models[0].InputCapabilities[entity.ProtocolOpenAIChat], []string{"image"}) {
		t.Fatal("Team capabilities were unioned instead of intersected", models, err)
	}
	svc.invalidateRuntimeTeamMember(identity.TeamID, identity.UserID)
	if models, err = svc.TeamGatewayModels(context.Background(), identity); err == nil || models != nil {
		t.Fatal("revoked membership retained attachment context", models, err)
	}
}

func TestTeamNativeAttachmentAuthorizationRequiresOnlyCapturedRuntimeProof(t *testing.T) {
	svc, identity := teamGatewayFixture(t, "https://provider-one.example/v1")
	svc.db = nil
	authorize := svc.teamNativeAttachmentAuthorization(context.Background(), identity, "mdl_one")
	// No Control Plane connection is available. This boundary must still use
	// exact current Session, Team, member and Model publication, never DB roles.
	if err := authorize(nil); err != nil {
		t.Fatal("native attachment borrowed database authority", err)
	}
	svc.runtime.deniedModels.Store("mdl_one", uint64(1))
	if authorize(nil) == nil {
		t.Fatal("revoked Model retained native object authority")
	}
	svc.runtime.deniedModels.Delete("mdl_one")
	svc.runtime.auth.Load().Teams[identity.TeamID].Members[identity.UserID] = "tmb_rejoined"
	if authorize(nil) == nil {
		t.Fatal("replacement membership restored original native object authority")
	}
}

func TestTeamAttachmentDeadlineCannotBeExtendedByCleanup(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := teamMediaRow(now)
	row.NextCleanupAt = now.Add(30 * 24 * time.Hour)
	if !attachmentReadableAt(row, now.Add(time.Hour-time.Nanosecond)) || attachmentReadableAt(row, now.Add(time.Hour)) || attachmentReadableAt(row, now.Add(2*time.Hour)) {
		t.Fatal("cleanup scheduling changed fixed deadline")
	}
	row.ExpiresAt = nil
	if attachmentReadableAt(row, now) {
		t.Fatal("missing immutable deadline accepted")
	}
	late := now.Add(2 * time.Hour)
	row.ExpiresAt = &late
	if attachmentReadableAt(row, now) {
		t.Fatal("deadline beyond original one hour accepted")
	}
	row = teamMediaRow(now)
	row.State = "delete_pending"
	if attachmentReadableAt(row, now) {
		t.Fatal("deleted draft readable")
	}
	for _, kind := range []string{entity.StorageOwnerUser, entity.StorageOwnerProject} {
		row = teamMediaRow(now)
		row.OwnerKind = kind
		row.ExpiresAt = nil
		row.CreatorUserID = nil
		row.CreatorMembershipID = nil
		if !attachmentReadableAt(row, now) {
			t.Fatal("historical owner readability changed", kind)
		}
		raw, err := json.Marshal(attachmentView(row))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "membership") || strings.Contains(string(raw), "creator_user_id") || strings.Contains(string(raw), "expires_at") || strings.Contains(string(raw), "attachment_team_id") {
			t.Fatal("historical object acquired Team proof", string(raw))
		}
	}
}

func TestTeamAttachmentCreatorProofRejectsPeersAliasesAndRejoins(t *testing.T) {
	row := teamMediaRow(time.Now().UTC())
	owner := attachmentOwner{Kind: entity.StorageOwnerTeam, ID: row.OwnerID, CreatorUserID: *row.CreatorUserID, CreatorMembershipID: *row.CreatorMembershipID}
	if !teamAttachmentMatches(row, owner) {
		t.Fatal("exact creator rejected")
	}
	for _, change := range []func(*attachmentOwner){
		func(o *attachmentOwner) { o.ID = "tem_other" },
		func(o *attachmentOwner) { o.ID = "TEM_ORIGINAL" },
		func(o *attachmentOwner) { o.CreatorUserID = "usr_peer" },
		func(o *attachmentOwner) { o.CreatorUserID = "USR_CREATOR" },
		func(o *attachmentOwner) { o.CreatorMembershipID = "tmb_rejoined" },
		func(o *attachmentOwner) { o.CreatorMembershipID = "TMB_ORIGINAL" },
		func(o *attachmentOwner) { o.CreatorMembershipID = "" },
		func(o *attachmentOwner) { o.Kind = entity.StorageOwnerUser },
	} {
		candidate := owner
		change(&candidate)
		if teamAttachmentMatches(row, candidate) {
			t.Fatal("borrowed object authority", candidate)
		}
	}
	row.CreatorUserID = nil
	if teamAttachmentMatches(row, owner) {
		t.Fatal("missing creator proof accepted")
	}
}

func TestTeamManagedMediaValidationPreservesOpaqueNativeContent(t *testing.T) {
	ref := "routex://attachments/" + teamMediaTestID
	for _, test := range []struct{ protocol, body string }{
		{entity.ProtocolOpenAIChat, `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"REF","detail":"auto"}},{"type":"file","file":{"file_data":"REF","filename":"input.pdf"}}]}],"tools":[{"type":"function","function":{"parameters":{"image_url":"opaque"}}}]}`},
		{entity.ProtocolOpenAIResponses, `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"REF"},{"type":"input_file","file_data":"REF"}]},{"type":"function_call","arguments":"{\"input_image\":\"opaque\"}"}],"tools":[{"type":"function","parameters":{"input_file":"opaque"}}]}`},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"REF"}},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"REF"}}]}]},{"role":"assistant","content":[{"type":"tool_use","input":{"source":{"data":"opaque"}}}]}]}`},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"content","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"REF"}}]}}]}]}`},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"REF"}},{"functionCall":{"args":{"inlineData":{"data":"opaque"}}}},{"functionResponse":{"parts":[{"inline_data":{"mime_type":"application/pdf","data":"REF"}}],"response":{"fileUri":"opaque"}}}]}],"generationConfig":{"candidateCount":1,"responseModalities":["TEXT"]}}`},
	} {
		t.Run(test.protocol, func(t *testing.T) {
			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(strings.ReplaceAll(test.body, "REF", ref)), &payload); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(payload)
			if err := validateTeamNativeInput(test.protocol, payload); err != nil {
				t.Fatal("managed media rejected", err)
			}
			after, _ := json.Marshal(payload)
			if string(before) != string(after) {
				t.Fatal("validation rewrote opaque/native input")
			}
			for _, forbidden := range []string{"https://media.invalid/file", "data:image/png;base64,YQ==", "routex://attachments/obj_bad"} {
				var invalid map[string]json.RawMessage
				if err := json.Unmarshal([]byte(strings.ReplaceAll(test.body, "REF", forbidden)), &invalid); err != nil {
					t.Fatal(err)
				}
				if validateTeamNativeInput(test.protocol, invalid) == nil {
					t.Fatal("unmanaged media accepted", forbidden)
				}
			}
			payload["project_id"] = json.RawMessage(`"prj_selector"`)
			if validateTeamNativeInput(test.protocol, payload) == nil {
				t.Fatal("managed media permitted a workspace selector")
			}
		})
	}
}

func TestTeamManagedMediaCannotMaskUnmanagedSiblingOrNativeFields(t *testing.T) {
	ref := "routex://attachments/" + teamMediaTestID
	for _, test := range []struct{ protocol, body string }{
		{entity.ProtocolOpenAIChat, `{"messages":[{"content":[{"type":"image_url","image_url":{"url":"REF"},"input_audio":{"data":"private"}}]}]}`},
		{entity.ProtocolOpenAIChat, `{"messages":[{"content":[{"type":"image_url","image_url":{"url":"REF"}},{"type":"image_url","image_url":{"url":"https://media.invalid"}}]}]}`},
		{entity.ProtocolOpenAIResponses, `{"input":[{"content":[{"type":"input_file","file_data":"REF","file_id":"file_remote"}]}]}`},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"base64","data":"REF"}},{"type":"image","source":{"type":"url","url":"https://media.invalid"}}]}]}]}`},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"REF"},"fileData":{"fileUri":"https://media.invalid"}}]}]}`},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"REF"}}]}],"generationConfig":{"responseModalities":["IMAGE"]}}`},
	} {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(strings.ReplaceAll(test.body, "REF", ref)), &payload); err != nil {
			t.Fatal(err)
		}
		if validateTeamNativeInput(test.protocol, payload) == nil {
			t.Fatal("managed reference masked unsupported native media", test.protocol, test.body)
		}
	}
}

func TestTeamAttachmentMetadataPreflightUsesCompleteSetAndOccurrenceKinds(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := teamMediaRow(now)
	plan := &gatewayAttachmentPlan{Occurrences: []gatewayAttachmentReference{{ObjectID: row.ID, Kind: gatewayAttachmentImage}, {ObjectID: row.ID, Kind: gatewayAttachmentImage}}}
	if err := validateTeamAttachmentMetadata(plan, map[string]entity.StorageObject{row.ID: row}, now); err != nil {
		t.Fatal(err)
	}
	images, pdfs := plan.MediaInputs()
	if len(plan.UniqueObjectIDs()) != 1 || images != 2 || pdfs != 0 {
		t.Fatal("occurrences became storage reads or collapsed billing")
	}
	plan.Occurrences = append(plan.Occurrences, gatewayAttachmentReference{ObjectID: "obj_missing", Kind: gatewayAttachmentImage})
	if validateTeamAttachmentMetadata(plan, map[string]entity.StorageObject{row.ID: row}, now) == nil {
		t.Fatal("authorized first object masked later missing object")
	}
	plan.Occurrences = plan.Occurrences[:2]
	plan.Occurrences[1].Kind = gatewayAttachmentPDF
	if validateTeamAttachmentMetadata(plan, map[string]entity.StorageObject{row.ID: row}, now) == nil {
		t.Fatal("deduplication masked an incompatible second occurrence")
	}
	plan.Occurrences = plan.Occurrences[:1]
	for _, change := range []func(*entity.StorageObject){
		func(r *entity.StorageObject) { r.State = "uploading" },
		func(r *entity.StorageObject) { r.ExpiresAt = nil },
		func(r *entity.StorageObject) { r.Size = 0 },
		func(r *entity.StorageObject) { r.Size = (2 << 20) + 1 },
	} {
		candidate := row
		change(&candidate)
		if validateTeamAttachmentMetadata(plan, map[string]entity.StorageObject{row.ID: candidate}, now) == nil {
			t.Fatal("unsafe metadata passed complete-set preflight")
		}
	}
}

// A locking handle is already initialized. Authorization must not reuse its
// mutable Statement across the creator, Team and membership reads.
func TestTeamAttachmentAuthorizationIsolatesInitializedLockingQueries(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			svc, identity := teamGatewayFixture(t, "https://provider-one.example/v1")
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			type observedQuery struct {
				table string
				sql   string
				vars  []any
			}
			var reads []observedQuery
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := db.Callback().Query().Register("test:team-attachment-subjects", func(query *gorm.DB) {
				query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "LIMIT", "FOR"}
				callbacks.BuildQuerySQL(query)
				reads = append(reads, observedQuery{query.Statement.Table, query.Statement.SQL.String(), append([]any(nil), query.Statement.Vars...)})
				if query.Statement.Context != ctx {
					t.Fatal("authorization discarded transaction context")
				}
				switch row := query.Statement.Dest.(type) {
				case *entity.User:
					*row = entity.User{ID: identity.UserID}
				case *entity.Team:
					*row = entity.Team{ID: identity.TeamID, Status: entity.ResourceActive}
				case *entity.TeamMembership:
					*row = entity.TeamMembership{ID: identity.TeamMembershipID, TeamID: identity.TeamID, UserID: identity.UserID, Status: entity.ResourceActive, Role: entity.TeamMember}
				default:
					t.Fatalf("unexpected authorization subject %T", query.Statement.Dest)
				}
				// DryRun normally retains SQL for inspection. Reset only rendered
				// SQL, as real query execution does; leave model/conditions intact.
				query.Statement.SQL.Reset()
				query.Statement.Vars = nil
			}); err != nil {
				t.Fatal(err)
			}
			origin := db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"})
			if err := svc.authorizeTeamAttachment(ctx, origin, identity, ""); err != nil {
				t.Fatal(err)
			}
			want := []observedQuery{
				{table: "users", vars: []any{identity.UserID, 1}},
				{table: "teams", vars: []any{identity.TeamID, 1}},
				{table: "team_memberships", vars: []any{identity.TeamMembershipID, identity.TeamID, identity.UserID, 1}},
			}
			if len(reads) != len(want) {
				t.Fatal("authorization omitted a current subject read", reads)
			}
			for i, read := range reads {
				if read.table != want[i].table || !reflect.DeepEqual(read.vars, want[i].vars) || !strings.Contains(read.sql, `FROM "`+want[i].table+`"`) || !strings.HasSuffix(read.sql, " FOR UPDATE") {
					t.Fatalf("authorization subject %d inherited another query or lost its lock: %+v", i, read)
				}
				if dialect.Name() == "mysql" && strings.Count(read.sql, "AS BINARY") != 2*(len(want[i].vars)-1) {
					t.Fatal("authorization lost exact collation guards", read)
				}
			}
			if origin.Statement.Model != nil || origin.Statement.Dest != nil {
				t.Fatal("authorization polluted its locking transaction handle")
			}
			if _, exists := origin.Statement.Clauses["WHERE"]; exists {
				t.Fatal("authorization leaked subject predicates into its caller")
			}
		})
	}
}
