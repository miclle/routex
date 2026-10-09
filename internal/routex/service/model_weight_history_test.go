package service

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func weightTestState() *modelWeightState {
	birth := time.Date(2026, 10, 9, 0, 0, 0, 123456000, time.UTC)
	row := ModelWeightRow{BindingID: "bnd_retained", BindingCreatedAt: &birth, ProviderModelID: "pmd_retained", ProviderModelCreatedAt: &birth, ConnectionID: "con_retained", ConnectionCreatedAt: &birth, ProviderID: "prv_retained", ProviderCreatedAt: &birth, Protocol: entity.ProtocolOpenAIChat, Weight: 100}
	c := entity.ProviderConnection{ID: row.ConnectionID, ProviderID: row.ProviderID, Protocol: row.Protocol, Enabled: true, CreatedAt: birth, EgressMode: "direct", BaseURL: "http://controlled.invalid/v1"}
	pm := entity.ProviderModel{ID: row.ProviderModelID, ConnectionID: c.ID, CreatedAt: birth, ETag: strings.Repeat("a", 64), UpstreamName: "recorded"}
	cred := modelCreationCredentialProof{ID: "crd_retained", Revision: strings.Repeat("b", 64), CipherHash: strings.Repeat("c", 64), CreatedAt: birth, Enabled: true, VerificationStatus: "verified", Access: []string{pm.ID}}
	return &modelWeightState{Model: entity.Model{ID: "mdl_retained", Status: "active", CreatedAt: birth}, Rows: []ModelWeightRow{row}, Models: []entity.ProviderModel{pm}, Connections: []entity.ProviderConnection{c}, Providers: []entity.Provider{{ID: row.ProviderID, CreatedAt: birth, Enabled: true, ETag: "0"}}, Supplies: map[string]routingSupplyState{pm.ID: {Credentials: []modelCreationCredentialProof{cred}, Enabled: true, Covered: true, SourceAvailable: true}}, EgressReady: map[string]bool{c.ID: true}, EgressRevisions: map[string]string{c.ID: strings.Repeat("d", 64)}}
}
func TestModelWeightRollbackStrictInput(t *testing.T) {
	good := `{"version_id":"mwv_01m36yee4gkbns18pfcqqc75a3","request_id":"e26c3a0b-cfee-4391-bafd-7858fbf997ee","reason":"Reviewed weights"}`
	var input ModelWeightRollbackInput
	if json.Unmarshal([]byte(good), &input) != nil {
		t.Fatal("valid reviewed intent rejected")
	}
	for _, bad := range []string{"null", good + good, strings.Replace(good, `"reason":"Reviewed weights"`, `"reason":null`, 1), strings.Replace(good, `"reason":"Reviewed weights"`, `"reason":"x","reason":"y"`, 1), strings.Replace(good, `"reason":"Reviewed weights"`, `"reason":" x "`, 1), strings.Replace(good, `"reason":"Reviewed weights"`, `"reason":"x\n"`, 1), strings.Replace(good, "Reviewed weights", strings.Repeat("界", 342), 1), strings.Replace(good, "Reviewed weights", `\ud800`, 1), strings.Replace(good, `{"version_id"`, `{"weights":[],"version_id"`, 1), strings.Replace(good, "4391", "3391", 1), strings.Replace(good, "01m36", "81m36", 1), strings.Repeat(" ", 32769)} {
		var rejected ModelWeightRollbackInput
		if json.Unmarshal([]byte(bad), &rejected) == nil {
			t.Fatal("ambiguous/unsafe command accepted", bad)
		}
	}
}
func TestModelWeightHistoryTopologyAndCompleteValidity(t *testing.T) {
	st := weightTestState()
	if !modelWeightRowsValid(st.Rows) || !st.executable(st.Rows) {
		t.Fatal("valid retained complete topology rejected")
	}
	for _, field := range []string{"birth", "protocol", "provider", "binding", "weight", "duplicate", "zero-total"} {
		t.Run(field, func(t *testing.T) {
			rows := slices.Clone(st.Rows)
			switch field {
			case "birth":
				rows[0].ProviderCreatedAt = nil
			case "protocol":
				rows[0].Protocol = "OPENAI_CHAT"
			case "provider":
				rows[0].ProviderID = "prv_foreign"
			case "binding":
				rows[0].BindingID = "bnd_foreign"
			case "weight":
				rows[0].Weight = 101
			case "duplicate":
				rows = append(rows, rows[0])
			case "zero-total":
				rows[0].Weight = 0
			}
			if field == "weight" || field == "zero-total" {
				if !modelWeightTopologyEqual(st.Rows, rows) {
					t.Fatal("weights changed topology")
				}
			} else if modelWeightTopologyEqual(st.Rows, rows) {
				t.Fatal("exact topology drift ignored")
			}
			if field != "provider" && field != "binding" && modelWeightRowsValid(rows) {
				t.Fatal("invalid record restorable")
			}
		})
	}
}
func TestModelWeightReviewPrivateFencesAndNoMutation(t *testing.T) {
	st := weightTestState()
	actor := entity.User{ID: "usr_reader", CreatedAt: st.Model.CreatedAt}
	raw, digest, err := modelWeightSnapshot(st.Rows)
	if err != nil {
		t.Fatal(err)
	}
	target := entity.ModelWeightVersion{ID: "mwv_01m36yee4gkbns18pfcqqc75a3", ModelID: st.Model.ID, ModelBirth: modelWeightBirth(st.Model.CreatedAt), Snapshot: raw, SnapshotDigest: digest, ValidWeightSet: true}
	original := memberModelsDigest(st)
	review := modelWeightReview(actor, true, st, target, st.Rows, nil)
	if !review.Eligible || len(review.BlockerCodes) != 0 || memberModelsDigest(st) != original {
		t.Fatal("review mutated or rejected current exact facts")
	}
	for _, field := range []string{"permission", "actor-birth", "source", "egress", "config-time", "latest-version", "target-birth"} {
		t.Run(field, func(t *testing.T) {
			next := weightTestState()
			who := actor
			v := target
			write := true
			var latest *entity.ModelWeightVersion
			switch field {
			case "permission":
				write = false
			case "actor-birth":
				who.CreatedAt = who.CreatedAt.Add(time.Second)
			case "source":
				x := next.Supplies[next.Rows[0].ProviderModelID]
				x.SourceAvailable = false
				next.Supplies[next.Rows[0].ProviderModelID] = x
			case "egress":
				next.EgressRevisions[next.Rows[0].ConnectionID] = strings.Repeat("e", 64)
			case "config-time":
				now := time.Now()
				next.Model.ConfigUpdatedAt = &now
			case "latest-version":
				latest = &entity.ModelWeightVersion{ID: "mwv_01m36yee4gkbns18pfcqqc75a4", Sequence: 2}
			case "target-birth":
				v.ModelBirth = nil
			}
			got := modelWeightReview(who, write, next, v, next.Rows, latest)
			if got.ReviewETag == review.ReviewETag {
				t.Fatal("review did not bind", field)
			}
			if (field == "permission" || field == "source" || field == "target-birth") && got.Eligible {
				t.Fatal("unsafe rollback remained eligible")
			}
		})
	}
}
func TestModelWeightCursorBoundToExactBirthAndScope(t *testing.T) {
	st := weightTestState()
	a := entity.User{ID: "usr_reader", CreatedAt: st.Model.CreatedAt}
	scope := modelWeightCursorScope(a, st.Model)
	cursor := modelWeightCursor(2, scope)
	if seq, err := modelWeightCursorRead(cursor, scope); err != nil || seq != 2 {
		t.Fatal(seq, err)
	}
	for _, raw := range []string{cursor + "=", strings.Repeat("x", 201), modelWeightCursor(0, scope), modelWeightCursor(2, strings.Repeat("a", 64))} {
		if _, err := modelWeightCursorRead(raw, scope); err == nil {
			t.Fatal("foreign/noncanonical cursor accepted")
		}
	}
	a.CreatedAt = a.CreatedAt.Add(time.Microsecond)
	if _, err := modelWeightCursorRead(cursor, modelWeightCursorScope(a, st.Model)); err == nil {
		t.Fatal("actor birth reuse accepted")
	}
}
func TestModelWeightTypedAuditProjection(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	version := "mwv_01m36yee4gkbns18pfcqqc75a3"
	details := modelWeightAudit{SourceVersionID: &version, SavedVersionID: &version, BeforeDigest: a, AfterDigest: b, BindingCount: 1000, Effect: "changed"}
	raw, _ := json.Marshal(details)
	text := string(raw)
	row := entity.AuditEvent{Action: "model.weights.update", ResourceType: "model", ResourceID: "mdl_retained", DetailsJSON: &text}
	if _, ok := modelWeightAuditProjection(row); !ok {
		t.Fatal("bounded typed references rejected")
	}
	for _, bad := range []string{`{"secret":"private"}`, strings.Replace(text, `"binding_count":1000`, `"binding_count":1001`, 1), strings.Replace(text, `"effect":"changed"`, `"effect":"noop"`, 1), strings.Replace(text, `"effect":"changed"`, `"effect":"changed","secret":"private"`, 1)} {
		row.DetailsJSON = &bad
		if _, ok := modelWeightAuditProjection(row); ok {
			t.Fatal("arbitrary audit data projected")
		}
	}
}

func weightTestRuntime(st *modelWeightState) (*Service, *runtimeAuthorization, *runtimeRoutes) {
	pm, c, row := st.Models[0], st.Connections[0], st.Rows[0]
	proof := st.Supplies[pm.ID].Credentials[0]
	credential := entity.ProviderCredential{ID: proof.ID, ConnectionID: c.ID, Enabled: true, VerificationStatus: "verified", Ciphertext: "private-inline", CreatedAt: proof.CreatedAt}
	proof.Revision = credentialRuntimeRevision(credential)
	proof.CipherHash = credentialSourceProof(credential)
	supply := st.Supplies[pm.ID]
	supply.Credentials = []modelCreationCredentialProof{proof}
	st.Supplies[pm.ID] = supply
	st.Credentials = []entity.ProviderCredential{credential}
	st.Names = []entity.ModelName{{Name: "Retained/model", ModelID: st.Model.ID, CurrentModelID: &st.Model.ID, CreatedAt: st.Model.CreatedAt}}
	revision := egressRevision(c, entity.EgressSetting{}, nil)
	st.EgressRevisions[c.ID] = revision
	provider := st.Providers[0]
	auth := &runtimeAuthorization{Providers: map[string]runtimeProviderProof{provider.ID: {Birth: provider.CreatedAt, Enabled: provider.Enabled, Revision: provider.ETag}}, SourceDigest: strings.Repeat("f", 64), ValidUntil: time.Now().Add(time.Second), Models: map[string]bool{st.Model.ID: true}, ModelCreated: map[string]time.Time{st.Model.ID: st.Model.CreatedAt}, ModelEligibilityHashes: map[string]string{st.Model.ID: st.currentEligibilityHash()}, Connections: map[string]runtimeConnectionProof{c.ID: {ProviderID: c.ProviderID, Birth: c.CreatedAt, Enabled: true}}, ConnectionRevisions: map[string]string{c.ID: revision}, ProviderModels: map[string]bool{pm.ID: true}, ProviderModelRevisions: map[string]string{pm.ID: pm.ETag}, Credentials: map[string]bool{proof.ID: true}, CredentialRevisions: map[string]string{proof.ID: proof.Revision}, CredentialAccess: map[string]map[string]bool{proof.ID: {pm.ID: true}}}
	route := runtimeRoute{Route: gatewayRoute{BindingID: row.BindingID, Weight: row.Weight, Protocol: c.Protocol, ProviderModelID: pm.ID, ProviderID: c.ProviderID, ProviderBirth: provider.CreatedAt, ProviderEnabled: provider.Enabled, ProviderRevision: provider.ETag, ConnectionID: c.ID, ConnectionBirth: c.CreatedAt, ConnectionEnabled: c.Enabled, UpstreamName: pm.UpstreamName, BaseURL: c.BaseURL, EgressRevision: revision}, Credentials: []runtimeCredential{{ID: proof.ID, CreatedAt: proof.CreatedAt, CipherHash: proof.CipherHash}}}
	routes := &runtimeRoutes{ID: "cfg_retained", Digest: auth.SourceDigest, Models: map[string][]runtimeRoute{st.Model.ID: {route}}}
	svc := &Service{runtime: &gatewayRuntime{}}
	svc.runtime.auth.Store(auth)
	svc.runtime.routes.Store(routes)
	return svc, auth, routes
}
func TestModelWeightRuntimeOnlyExactCurrentApplication(t *testing.T) {
	for _, kind := range []string{"current", "expired", "source-digest", "weight", "model-birth", "connection-birth", "credential-source", "credential-revision", "current-hash", "egress-generation", "tombstone", "runtime-busy", "publication-busy", "credential-pool-extra", "epoch"} {
		t.Run(kind, func(t *testing.T) {
			st := weightTestState()
			svc, auth, routes := weightTestRuntime(st)
			switch kind {
			case "epoch":
				svc.runtime.epoch.Add(1)
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Nanosecond)
			case "source-digest":
				routes.Digest = "other"
			case "weight":
				route := routes.Models[st.Model.ID][0]
				route.Route.Weight = 0
				routes.Models[st.Model.ID][0] = route
			case "model-birth":
				auth.ModelCreated[st.Model.ID] = st.Model.CreatedAt.Add(time.Microsecond)
			case "connection-birth":
				auth.Connections[st.Connections[0].ID] = runtimeConnectionProof{Enabled: true, ProviderID: st.Connections[0].ProviderID, Birth: st.Connections[0].CreatedAt.Add(time.Microsecond)}
			case "credential-source":
				route := routes.Models[st.Model.ID][0]
				route.Credentials = slices.Clone(route.Credentials)
				route.Credentials[0].CipherHash = "changed"
				routes.Models[st.Model.ID][0] = route
			case "credential-revision":
				auth.CredentialRevisions[st.Credentials[0].ID] = "changed"
			case "current-hash":
				auth.ModelEligibilityHashes[st.Model.ID] = "changed"
			case "egress-generation":
				svc.egressGeneration.Add(1)
			case "tombstone":
				svc.runtime.deniedModels.Store(st.Model.ID, true)
			case "runtime-busy":
				svc.runtime.mu.Lock()
				defer svc.runtime.mu.Unlock()
			case "publication-busy":
				svc.runtime.publication.Lock()
				defer svc.runtime.publication.Unlock()
			case "credential-pool-extra":
				route := routes.Models[st.Model.ID][0]
				route.Credentials = append(slices.Clone(route.Credentials), runtimeCredential{ID: "crd_foreign"})
				routes.Models[st.Model.ID][0] = route
			}
			if svc.modelWeightRuntimeApplied(st) != (kind == "current") {
				t.Fatal("application predicate ignored", kind)
			}
		})
	}
}

func TestModelWeightSnapshotLogicalNormalizationAndStrictShape(t *testing.T) {
	rows := weightTestState().Rows
	raw, digest, err := modelWeightSnapshot(rows)
	if err != nil {
		t.Fatal(err)
	}
	var object []map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		t.Fatal("snapshot seed")
	}
	alternate, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, canonical, err := decodeModelWeightSnapshot(alternate, 1, digest)
	if err != nil || string(canonical) != string(raw) || memberModelsDigest(got) != digest {
		t.Fatal("logical object order/whitespace changed integrity", err)
	}
	for _, test := range []struct{ name, raw string }{
		{"extra", strings.Replace(string(raw), `"weight":100`, `"weight":100,"secret":"forbidden"`, 1)},
		{"missing", strings.Replace(string(raw), `,"weight":100`, "", 1)},
		{"duplicate", strings.Replace(string(raw), `"weight":100`, `"weight":100,"weight":100`, 1)},
		{"null-weight", strings.Replace(string(raw), `"weight":100`, `"weight":null`, 1)},
		{"zero-birth", strings.Replace(string(raw), rows[0].BindingCreatedAt.Format(time.RFC3339Nano), "0001-01-01T00:00:00Z", 1)},
		{"invalid-calendar", strings.Replace(string(raw), "2026-10-09", "2026-02-30", 1)},
		{"wrong-digest", strings.Replace(string(raw), `"weight":100`, `"weight":99`, 1)},
		{"null-array", "null"}, {"second-value", string(raw) + "[]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := decodeModelWeightSnapshot([]byte(test.raw), 1, digest); err == nil {
				t.Fatal("corrupt snapshot accepted")
			}
		})
	}
	historical := slices.Clone(rows)
	historical[0].BindingCreatedAt = nil
	historical[0].Weight = 0
	old, oldDigest, err := modelWeightSnapshot(historical)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := decodeModelWeightSnapshot(old, 1, oldDigest); err != nil || modelWeightRowsValid(got) {
		t.Fatal("known historical nullable/invalid-total facts became fabricated readiness", err)
	}
}

func TestModelWeightRuntimeProjectionUsesExistingPublicationIndex(t *testing.T) {
	st := weightTestState()
	svc, _, routes := weightTestRuntime(st)
	row := st.Rows[0]
	data := &runtimeData{Models: []entity.Model{st.Model}, Names: st.Names, Bindings: []entity.ModelProviderBinding{{ID: row.BindingID, ModelID: st.Model.ID, ProviderModelID: row.ProviderModelID, Weight: row.Weight, CreatedAt: *row.BindingCreatedAt}}, ProviderModels: st.Models, Connections: st.Connections, Providers: st.Providers, Credentials: st.Credentials, Access: []entity.CredentialModelAccess{{CredentialID: st.Credentials[0].ID, ProviderModelID: row.ProviderModelID}}}
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Second))
	auth.SourceDigest = routes.Digest
	svc.runtime.auth.Store(auth)
	if !svc.modelWeightRuntimeApplied(st) {
		t.Fatal("same exact DB projection failed existing publication index")
	}
	st.Rows[0].Weight = 0
	if svc.modelWeightRuntimeApplied(st) {
		t.Fatal("captured runtime projected changed database set as applied")
	}
}

func TestModelWeightHistoryUTCWirePreservesExactBirth(t *testing.T) {
	for _, recorded := range []string{"2026-10-09T14:27:38.676+08:00", "2026-10-09T14:27:38.676123456+08:00", "2026-10-09T01:27:38.676123456-05:00"} {
		t.Run(recorded, func(t *testing.T) {
			birth, err := time.Parse(time.RFC3339Nano, recorded)
			if err != nil {
				t.Fatal(err)
			}
			stored := entity.ModelWeightVersion{ModelBirth: &birth, CapturedAt: birth}
			summary := modelWeightSummary(stored)
			if summary.ModelCreatedAt == nil || summary.ModelCreatedAt == stored.ModelBirth || !summary.ModelCreatedAt.Equal(birth) || summary.ModelCreatedAt.Location() != time.UTC || summary.ModelCreatedAt.Nanosecond() != birth.Nanosecond() || stored.ModelBirth.Format(time.RFC3339Nano) != recorded {
				t.Fatal("UTC projection changed or mutated exact recorded birth")
			}
			raw, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			expected, _ := json.Marshal(birth.UTC().Format(time.RFC3339Nano))
			if string(fields["model_created_at"]) != string(expected) || string(fields["captured_at"]) != string(expected) {
				t.Fatal("summary wire did not match strict UTC Model detail identity")
			}
			later := birth.Add(time.Nanosecond)
			if modelWeightSameBirth(summary.ModelCreatedAt, &later) {
				t.Fatal("UTC projection rounded distinct retained births together")
			}
		})
	}
	if modelWeightSummary(entity.ModelWeightVersion{}).ModelCreatedAt != nil {
		t.Fatal("unknown legacy birth became a recorded identity")
	}
	zero := time.Time{}
	if value := modelWeightSummary(entity.ModelWeightVersion{ModelBirth: &zero}).ModelCreatedAt; value == nil || !value.IsZero() {
		t.Fatal("invalid recorded zero birth was silently changed into unknown")
	}
}

func TestModelWeightHistoryOtherWireDatesRemainUTC(t *testing.T) {
	birth, err := time.Parse(time.RFC3339Nano, "2026-10-09T14:27:38.676123456+08:00")
	if err != nil {
		t.Fatal(err)
	}
	st := weightTestState()
	st.Model.CreatedAt = birth
	row := st.Rows[0]
	row.BindingCreatedAt, row.ProviderModelCreatedAt, row.ConnectionCreatedAt, row.ProviderCreatedAt = &birth, &birth, &birth, &birth
	raw, err := json.Marshal([]ModelWeightRow{row})
	if err != nil {
		t.Fatal(err)
	}
	current := row
	current.BindingCreatedAt, current.ProviderModelCreatedAt, current.ConnectionCreatedAt, current.ProviderCreatedAt = modelWeightBirth(birth), modelWeightBirth(birth), modelWeightBirth(birth), modelWeightBirth(birth)
	_, digest, err := modelWeightSnapshot([]ModelWeightRow{current})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := decodeModelWeightSnapshot(raw, 1, digest)
	if err != nil || len(rows) != 1 {
		t.Fatal("offset snapshot lost canonical logical identity", err)
	}
	for _, value := range []*time.Time{rows[0].BindingCreatedAt, rows[0].ProviderModelCreatedAt, rows[0].ConnectionCreatedAt, rows[0].ProviderCreatedAt} {
		if value == nil || value.Location() != time.UTC || !value.Equal(birth) || value.Nanosecond() != birth.Nanosecond() {
			t.Fatal("retained topology birth lost exact UTC precision")
		}
	}
	st.Rows = rows
	target := entity.ModelWeightVersion{ModelBirth: &birth, ValidWeightSet: true}
	review := modelWeightReview(entity.User{}, false, st, target, rows, nil)
	if review.ObservedAt.Location() != time.UTC || review.ObservedAt.IsZero() {
		t.Fatal("review timestamp left the strict UTC boundary")
	}
	receipt := modelWeightCommandReceipt(entity.ModelWeightRollbackCommand{CreatedAt: birth})
	if receipt.CreatedAt.Location() != time.UTC || !receipt.CreatedAt.Equal(birth) || receipt.CreatedAt.Nanosecond() != birth.Nanosecond() {
		t.Fatal("rollback and recovered receipt timestamp lost exact UTC precision")
	}
}
