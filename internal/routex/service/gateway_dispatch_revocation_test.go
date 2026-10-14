package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestGatewayRevocationAfterAdmissionDoesNotDispatch(t *testing.T) {
	cases := []struct {
		name    string
		project bool
		reduce  func(*Service, *runtimeData)
	}{
		{"personal_key_tombstone", false, func(s *Service, _ *runtimeData) { s.InvalidateRuntimeKey("key_one") }},
		{"personal_key_revoked", false, func(_ *Service, d *runtimeData) { d.Keys[0].Status = entity.KeyRevoked }},
		{"personal_key_expired", false, func(_ *Service, d *runtimeData) {
			expired := time.Now().Add(-time.Second)
			d.Keys[0].ExpiresAt = &expired
		}},
		{"personal_owner_tombstone", false, func(s *Service, _ *runtimeData) { s.InvalidateRuntimeUser("usr_one") }},
		{"project_tombstone", true, func(s *Service, _ *runtimeData) { s.InvalidateRuntimeProject("prj_one") }},
		{"project_archived", true, func(_ *Service, d *runtimeData) { d.ProjectData.Projects[0].Status = entity.ResourceArchived }},
		{"project_without_manager", true, func(_ *Service, d *runtimeData) { d.ProjectData.Managers = nil }},
		{"project_key_tombstone", true, func(s *Service, _ *runtimeData) { s.InvalidateRuntimeKey("pky_one") }},
		{"project_key_revoked", true, func(_ *Service, d *runtimeData) { d.ProjectData.Keys[0].Status = entity.KeyRevoked }},
		{"project_key_expired", true, func(_ *Service, d *runtimeData) {
			expired := time.Now().Add(-time.Second)
			d.ProjectData.Keys[0].ExpiresAt = &expired
		}},
		{"credential_tombstone", false, func(s *Service, _ *runtimeData) { s.InvalidateRuntimeCredential("crd_one") }},
		{"credential_disabled", false, func(_ *Service, d *runtimeData) { d.Credentials[0].Enabled = false }},
		{"credential_coverage_removed", false, func(_ *Service, d *runtimeData) { d.Access = nil }},
	}
	for _, protocol := range dispatchRevocationProtocols() {
		t.Run(protocol, func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						calls.Add(1)
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{}`)
					}))
					defer server.Close()
					svc, data, bearer := dispatchRevocationFixture(t, server.URL+"/v1", protocol, test.project)
					admissions := 0
					svc.afterGatewayAdmission = func() {
						admissions++
						test.reduce(svc, data)
						// Keep the original routes and prepared request. Only current
						// authorization facts change after the single quota admission.
						svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
					}
					result, err := dispatchRevocationInvoke(t, svc, bearer, protocol, "req_revoked_after_admission")
					if err == nil || result == nil || !result.Admitted || admissions != 1 || calls.Load() != 0 || result.Response != nil || result.AttemptID != "" || len(result.Attempts) != 0 || !result.NoUpstreamWork() {
						t.Fatalf("post-admission reduction: calls=%d admissions=%d result=%+v error=%v", calls.Load(), admissions, result, err)
					}
					if result.ProviderAttribution() != (CallProviderAttribution{}) {
						t.Fatal("unattempted request gained provider attribution")
					}
				})
			}
		})
	}
}

func TestGatewayPostAdmissionRevocationRetainsAndSettlesOneReservation(t *testing.T) {
	for _, protocol := range dispatchRevocationProtocols() {
		t.Run(protocol, func(t *testing.T) {
			for _, project := range []bool{false, true} {
				kind := "personal"
				if project {
					kind = "project"
				}
				t.Run(kind, func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
					defer server.Close()
					svc, data, bearer := dispatchRevocationFixture(t, server.URL+"/v1", protocol, project)
					now := time.Now().UTC()
					ownerKind, ownerID, keyID := "user", "usr_one", "key_one"
					if project {
						ownerKind, ownerID, keyID = "project", "prj_one", "pky_one"
					}
					account := limitAccount(ownerKind, ownerID)
					data.Limits = []entity.ResourceLimit{{ScopeKind: ownerKind, ScopeID: ownerID, ETag: "policy_revocation", Tokens5H: limitNumber(500)}}
					data.Quota.Bounds = map[string]entity.ReservationBound{
						"pmd_one": {ProviderModelID: "pmd_one", Protocol: protocol, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "bound_revocation"},
					}
					data.Quota.Created = map[string]time.Time{account: now.Add(-time.Minute), limitAccount("key", keyID): now.Add(-time.Minute)}
					if err := compileRuntimeLimits(data); err != nil {
						t.Fatal(err)
					}
					svc.runtime.auth.Store(buildRuntimeAuthorization(data, now.Add(time.Minute)))
					journal := filepath.Join(t.TempDir(), "revocation.db")
					queue, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = queue.Close() }()
					if err := queue.EnableQuota("UTC", now.Add(-2*time.Minute)); err != nil {
						t.Fatal(err)
					}
					svc.recorder = &callRecorder{queue: queue}
					const requestID = "req_revocation_reservation"
					var admittedAt int64
					admissions := 0
					svc.afterGatewayAdmission = func() {
						admissions++
						receipt, err := queue.QuotaReceipt(requestID)
						if err != nil || receipt.State != "active" || receipt.Bound.Tokens == nil || *receipt.Bound.Tokens <= 0 {
							t.Fatalf("original reservation: %+v error=%v", receipt, err)
						}
						admittedAt = receipt.AdmittedAt
						svc.InvalidateRuntimeKey(keyID)
					}
					result, err := dispatchRevocationInvoke(t, svc, bearer, protocol, requestID)
					if err == nil || result == nil || !result.Admitted || admissions != 1 || calls.Load() != 0 || !result.NoUpstreamWork() || result.AttemptID != "" || len(result.Attempts) != 0 {
						t.Fatalf("reserved reduction: admissions=%d calls=%d result=%+v error=%v", admissions, calls.Load(), result, err)
					}
					receipt, err := queue.QuotaReceipt(requestID)
					usage, usageErr := queue.AccountQuotaUsage(account, time.Now().UTC())
					if err != nil || usageErr != nil || receipt.State != "active" || receipt.AdmittedAt != admittedAt || receipt.Bound.Tokens == nil || usage.Active.TokensHeld != *receipt.Bound.Tokens || usage.Active.TokensUnknown != 0 || usage.FiveHours.TokensUsed != 0 || usage.FiveHours.TokensHeld != 0 || usage.FiveHours.TokensUnknown != 0 {
						t.Fatalf("reservation lost before settlement: receipt=%+v usage=%+v errors=%v/%v", receipt, usage, err, usageErr)
					}
					raw, err := gatewayFallbackPayload(requestID, result, time.Now().UTC())
					if err != nil {
						t.Fatal(err)
					}
					var fact CallFact
					if err := json.Unmarshal(raw, &fact); err != nil {
						t.Fatal(err)
					}
					fact.ErrorCode = "service_unavailable"
					if err := svc.PersistGatewayCall(context.Background(), fact); err != nil {
						t.Fatal(err)
					}
					receipt, err = queue.QuotaReceipt(requestID)
					entries, readErr := queue.Read(8)
					if err != nil || readErr != nil || len(entries) != 1 || receipt.State != "complete" || receipt.AdmittedAt != admittedAt || receipt.Actual.Tokens == nil || *receipt.Actual.Tokens != 0 || receipt.Overrun {
						t.Fatalf("original zero-work settlement: receipt=%+v entries=%d errors=%v/%v", receipt, len(entries), err, readErr)
					}
					var persisted CallFact
					if err := json.Unmarshal(entries[0].Payload, &persisted); err != nil || !persisted.NoWork || persisted.Pricing == nil || persisted.Pricing.Status != "no_work" || !zeroCounter(persisted.InputTokens) || !zeroCounter(persisted.OutputTokens) || persisted.ProviderID != "" || persisted.ProviderModelID != "" || persisted.ConnectionID != "" || len(persisted.Attempts) != 0 {
						t.Fatalf("persisted no-work attribution: %+v error=%v", persisted, err)
					}
					if err := queue.Close(); err != nil {
						t.Fatal(err)
					}
					reopened, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = reopened.Close() }()
					usage, err = reopened.AccountQuotaUsage(account, time.Now().UTC())
					if err != nil || usage.Active.TokensHeld != 0 || usage.Active.TokensUnknown != 0 || usage.FiveHours.TokensUsed != 0 || usage.FiveHours.TokensHeld != 0 || usage.FiveHours.TokensUnknown != 0 {
						t.Fatalf("reopened settled usage: %+v error=%v", usage, err)
					}
				})
			}
		})
	}
}

func TestGatewayRevocationAfterUpstreamReceiptPreservesAttempt(t *testing.T) {
	for _, protocol := range dispatchRevocationProtocols() {
		t.Run(protocol, func(t *testing.T) {
			for _, project := range []bool{false, true} {
				kind, keyID, projectID := "personal", "key_one", ""
				if project {
					kind, keyID, projectID = "project", "pky_one", "prj_one"
				}
				t.Run(kind, func(t *testing.T) {
					var calls atomic.Int32
					ready := make(chan *Service, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						calls.Add(1)
						select {
						case svc := <-ready:
							// The upstream has already received this exact attempt.
							svc.InvalidateRuntimeKey(keyID)
							svc.InvalidateRuntimeCredential("crd_one")
							if project {
								svc.InvalidateRuntimeProject(projectID)
							}
						default:
							t.Error("upstream request repeated after revocation")
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{}`)
					}))
					defer server.Close()
					svc, _, bearer := dispatchRevocationFixture(t, server.URL+"/v1", protocol, project)
					ready <- svc
					result, err := dispatchRevocationInvoke(t, svc, bearer, protocol, "req_revocation_after_receipt")
					if err != nil || result == nil || result.Response == nil || calls.Load() != 1 || result.AttemptID == "" || result.CredentialID != "crd_one" || result.SnapshotID != "cfg_dispatch_revocation" || result.KeyID != keyID || result.ProjectID != projectID || result.NoUpstreamWork() {
						t.Fatalf("received attempt: calls=%d result=%+v error=%v", calls.Load(), result, err)
					}
					_ = result.Response.Body.Close()
					if _, err := svc.AuthenticateAPIKey(t.Context(), bearer); err == nil {
						t.Fatal("revocation permitted a new authentication")
					}
				})
			}
		})
	}
}

func TestGatewayRevocationAfterCheckpointClearsUnsentAttempt(t *testing.T) {
	for _, protocol := range dispatchRevocationProtocols() {
		t.Run(protocol, func(t *testing.T) {
			for _, project := range []bool{false, true} {
				kind, ownerKind, ownerID, keyID := "personal", "user", "usr_one", "key_one"
				if project {
					kind, ownerKind, ownerID, keyID = "project", "project", "prj_one", "pky_one"
				}
				t.Run(kind, func(t *testing.T) {
					for _, target := range []string{"key", "credential"} {
						t.Run(target, func(t *testing.T) {
							for _, predecessor := range []bool{false, true} {
								stage := "first_attempt"
								if predecessor {
									stage = "after_rejected_attempt"
								}
								t.Run(stage, func(t *testing.T) {
									var calls atomic.Int32
									server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
										calls.Add(1)
										w.Header().Set("Content-Type", "application/json")
										w.WriteHeader(http.StatusUnauthorized)
										body := `{"error":{"type":"authentication_error"}}`
										switch protocol {
										case entity.ProtocolOpenAIChat:
											body = `{"error":{"code":"invalid_api_key"}}`
										case entity.ProtocolGeminiGenerateContent:
											body = `{"error":{"status":"UNAUTHENTICATED"}}`
										}
										_, _ = io.WriteString(w, body)
									}))
									defer server.Close()
									svc, data, bearer := dispatchRevocationFixture(t, server.URL+"/v1", protocol, project)
									now := time.Now().UTC()
									account := limitAccount(ownerKind, ownerID)
									data.Limits = []entity.ResourceLimit{{ScopeKind: ownerKind, ScopeID: ownerID, ETag: "policy_checkpoint", Tokens5H: limitNumber(500)}}
									data.Quota.Bounds = map[string]entity.ReservationBound{
										"pmd_one": {ProviderModelID: "pmd_one", Protocol: protocol, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "bound_checkpoint"},
									}
									data.Quota.Created = map[string]time.Time{account: now.Add(-time.Minute), limitAccount("key", keyID): now.Add(-time.Minute)}
									selectedCredential := "crd_one"
									if predecessor {
										ciphertext, err := svc.secrets.Seal("crd_two", "checkpoint-replacement-secret")
										if err != nil {
											t.Fatal(err)
										}
										data.Credentials = append(data.Credentials, entity.ProviderCredential{ID: "crd_two", ConnectionID: "con_one", Ciphertext: ciphertext, Priority: 1, Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: "0", CreatedAt: data.Credentials[0].CreatedAt})
										data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_two", ProviderModelID: "pmd_one"})
										selectedCredential = "crd_two"
									}
									if err := compileRuntimeLimits(data); err != nil {
										t.Fatal(err)
									}
									svc.runtime.auth.Store(buildRuntimeAuthorization(data, now.Add(time.Minute)))
									routes, err := svc.buildRuntimeRoutes(data)
									if err != nil {
										t.Fatal(err)
									}
									svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_dispatch_revocation", Models: routes, PublishedAt: now})
									journal := filepath.Join(t.TempDir(), "checkpoint.db")
									queue, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
									if err != nil {
										t.Fatal(err)
									}
									defer func() { _ = queue.Close() }()
									if err := queue.EnableQuota("UTC", now.Add(-2*time.Minute)); err != nil {
										t.Fatal(err)
									}
									svc.recorder = &callRecorder{queue: queue}
									const requestID = "req_revoked_after_checkpoint"
									var admittedAt int64
									admissions, revocations := 0, 0
									priorCleared := !predecessor
									svc.afterGatewayAdmission = func() {
										admissions++
										receipt, err := queue.QuotaReceipt(requestID)
										if err != nil || receipt.State != "active" || receipt.Recovery.Tokens == nil || *receipt.Recovery.Tokens != 0 {
											t.Fatalf("initial no-work recovery: %+v error=%v", receipt, err)
										}
										admittedAt = receipt.AdmittedAt
										clock := time.Now().UTC()
										svc.attemptNow = func() time.Time {
											current, err := queue.QuotaReceipt(requestID)
											if err != nil {
												t.Fatal(err)
											}
											// Observe durable recovery transitions, not clock-call
											// counts. A retry arms only after its rejected predecessor
											// has completed the original known-zero checkpoint.
											if predecessor && calls.Load() == 1 && current.Recovery.Tokens != nil && *current.Recovery.Tokens == 0 {
												priorCleared = true
											}
											if priorCleared && revocations == 0 && current.Recovery.Tokens == nil {
												revocations++
												if target == "key" {
													svc.InvalidateRuntimeKey(keyID)
												} else {
													svc.InvalidateRuntimeCredential(selectedCredential)
												}
											}
											return clock
										}
									}
									result, err := dispatchRevocationInvoke(t, svc, bearer, protocol, requestID)
									wantCalls := int32(0)
									if predecessor {
										wantCalls = 1
									}
									if err == nil || result == nil || !result.Admitted || admissions != 1 || revocations != 1 || calls.Load() != wantCalls || result.Response != nil || result.AttemptID != "" || !result.AttemptStartedAt.IsZero() || !result.NoUpstreamWork() || len(result.Attempts) != int(wantCalls) || result.CredentialID != selectedCredential || result.SnapshotID != "cfg_dispatch_revocation" || result.KeyID != keyID {
										t.Fatalf("checkpoint revocation: admissions=%d revocations=%d calls=%d result=%+v error=%v", admissions, revocations, calls.Load(), result, err)
									}
									if predecessor {
										prior := result.Attempts[0]
										if prior.ID == "" || prior.AttemptNumber != 1 || prior.CredentialID != "crd_one" || prior.SnapshotID != "cfg_dispatch_revocation" || prior.ConnectionID != "con_one" || prior.ProviderModelID != "pmd_one" || prior.FailureClass != "credential_rejected" || prior.WorkEvidence != "rejected_without_work" {
											t.Fatalf("original rejected attempt changed: %+v", prior)
										}
									}
									receipt, err := queue.QuotaReceipt(requestID)
									usage, usageErr := queue.AccountQuotaUsage(account, time.Now().UTC())
									if err != nil || usageErr != nil || receipt.AdmittedAt != admittedAt || receipt.State != "active" || receipt.Recovery.Tokens == nil || *receipt.Recovery.Tokens != 0 || usage.Active.TokensHeld <= 0 {
										t.Fatalf("cleared checkpoint lost reservation: receipt=%+v usage=%+v errors=%v/%v", receipt, usage, err, usageErr)
									}
									// Reopen without a final PersistGatewayCall: the durable
									// clearing checkpoint itself must preserve no-work recovery.
									if err := queue.Close(); err != nil {
										t.Fatal(err)
									}
									reopened, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
									if err != nil {
										t.Fatal(err)
									}
									defer func() { _ = reopened.Close() }()
									receipt, err = reopened.QuotaReceipt(requestID)
									entries, readErr := reopened.Read(8)
									if err != nil || readErr != nil || len(entries) != 1 || receipt.State != "interrupted" || receipt.AdmittedAt != admittedAt || receipt.Actual.Tokens == nil || *receipt.Actual.Tokens != 0 {
										t.Fatalf("durable cleared checkpoint: receipt=%+v entries=%d errors=%v/%v", receipt, len(entries), err, readErr)
									}
									var fact CallFact
									if err := json.Unmarshal(entries[0].Payload, &fact); err != nil || !fact.NoWork || fact.Pricing == nil || fact.Pricing.Status != "no_work" || !zeroCounter(fact.InputTokens) || !zeroCounter(fact.OutputTokens) || !reflect.DeepEqual(fact.Attempts, result.Attempts) || fact.KeyID != keyID || fact.ProjectID != result.ProjectID || fact.UserID != result.UserID || fact.SnapshotID != result.SnapshotID {
										t.Fatalf("recovered original identities/no-work: %+v error=%v", fact, err)
									}
									usage, err = reopened.AccountQuotaUsage(account, time.Now().UTC())
									if err != nil || usage.Active.TokensHeld != 0 || usage.Active.TokensUnknown != 0 || usage.FiveHours.TokensUsed != 0 || usage.FiveHours.TokensHeld != 0 || usage.FiveHours.TokensUnknown != 0 {
										t.Fatalf("recovered no-work counters: %+v error=%v", usage, err)
									}
								})
							}
						})
					}
				})
			}
		})
	}
}

func dispatchRevocationProtocols() []string {
	return []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent}
}

func dispatchRevocationFixture(t *testing.T, baseURL, protocol string, project bool) (*Service, *runtimeData, string) {
	t.Helper()
	var svc *Service
	var data *runtimeData
	var bearer string
	if project {
		svc, data, bearer, _ = projectRuntimeFixture(t)
		data.Connections[0].BaseURL = baseURL
	} else {
		svc, data, bearer = runtimeFixture(t, baseURL)
	}
	data.Connections[0].Protocol = protocol
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_dispatch_revocation", Models: routes, PublishedAt: time.Now()})
	t.Cleanup(svc.upstream.CloseIdleConnections)
	return svc, data, bearer
}

func dispatchRevocationInvoke(t *testing.T, svc *Service, bearer, protocol, requestID string) (*GatewayResult, error) {
	t.Helper()
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return svc.GatewayChat(t.Context(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":10}`), requestID)
	case entity.ProtocolOpenAIResponses:
		return svc.GatewayResponses(t.Context(), bearer, []byte(`{"model":"public-model","input":"hello","max_output_tokens":10}`), requestID)
	case entity.ProtocolAnthropicMessages:
		return svc.GatewayMessages(t.Context(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`), requestID, MessagesHeaders{Version: "2023-06-01"})
	case entity.ProtocolGeminiGenerateContent:
		return svc.GatewayGemini(t.Context(), bearer, []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":10}}`), requestID, "public-model", false)
	default:
		t.Fatalf("unsupported fixture protocol %q", protocol)
		return nil, nil
	}
}
