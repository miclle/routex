package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestGatewayPreparedAliasRetirementBlocksDispatch(t *testing.T) {
	for _, scope := range []string{"key", "team"} {
		for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
			t.Run(scope+"/"+protocol, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{}`)
				}))
				defer server.Close()
				var svc *Service
				var invoke func() (*GatewayResult, error)
				if scope == "team" {
					var identity *TeamSessionIdentity
					svc, identity = teamNativeFixture(t, server.URL+"/v1", protocol)
					invoke = func() (*GatewayResult, error) {
						if protocol == entity.ProtocolOpenAIChat {
							return svc.TeamGatewayChat(context.Background(), identity, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_alias_retired")
						}
						return teamNativeInvoke(svc, identity, protocol, teamNativeInput(protocol), "req_alias_retired")
					}
				} else {
					var data *runtimeData
					var bearer string
					svc, data, bearer = runtimeFixture(t, server.URL+"/v1")
					data.Connections[0].Protocol = protocol
					svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
					routes, err := svc.buildRuntimeRoutes(data)
					if err != nil {
						t.Fatal(err)
					}
					svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_alias", Models: routes, PublishedAt: time.Now()})
					invoke = func() (*GatewayResult, error) {
						switch protocol {
						case entity.ProtocolOpenAIChat:
							return svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_alias_retired")
						case entity.ProtocolOpenAIResponses:
							return svc.GatewayResponses(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_alias_retired")
						case entity.ProtocolAnthropicMessages:
							return svc.GatewayMessages(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_alias_retired", MessagesHeaders{Version: "2023-06-01"})
						default:
							return svc.GatewayGemini(context.Background(), bearer, []byte(teamNativeInput(protocol)), "req_alias_retired", "public-model", false)
						}
					}
				}
				auth := svc.runtime.auth.Load()
				original := auth.Names["public-model"]
				future := time.Now().Add(time.Hour)
				alias := original
				alias.CurrentModelID = nil
				alias.ExpiresAt = &future
				auth.Names[alias.Name] = alias
				original.Name = "current-model"
				auth.Names[original.Name] = original
				svc.afterGatewayAdmission = func() {
					previous := svc.runtime.auth.Load()
					next := *previous
					next.Names = make(map[string]entity.ModelName, len(previous.Names))
					for name, row := range previous.Names {
						next.Names[name] = row
					}
					expired := time.Now().Add(-time.Second)
					row := next.Names[alias.Name]
					row.ExpiresAt = &expired
					next.Names[alias.Name] = row
					svc.runtime.auth.Store(&next)
				}
				result, err := invoke()
				if err == nil || result == nil || !result.Admitted || calls.Load() != 0 || result.AttemptID != "" || !result.NoUpstreamWork() {
					t.Fatalf("retired prepared alias dispatched: calls=%d result=%+v err=%v", calls.Load(), result, err)
				}
			})
		}
	}
}

func TestRuntimeModelAliasAppliedRequiresExactPublishedConfiguration(t *testing.T) {
	subject := aliasSubjectFixture()
	svc, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	data.Models = []entity.Model{subject.Model}
	data.Names = []entity.ModelName{subject.Current, subject.Selected}
	publish := func() { svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute))) }
	publish()
	if !svc.runtimeModelAliasApplied(subject) {
		t.Fatal("exact publication not proven")
	}
	for _, change := range []func(){
		func() { svc.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second) },
		func() { svc.runtime.deniedModels.Store(subject.Model.ID, uint64(1)) },
		func() { delete(svc.runtime.auth.Load().Names, subject.Selected.Name) },
		func() {
			row := svc.runtime.auth.Load().Names[subject.Selected.Name]
			row.ExpiresAt = nil
			svc.runtime.auth.Load().Names[subject.Selected.Name] = row
		},
		func() {
			row := svc.runtime.auth.Load().Names[subject.Current.Name]
			row.ModelID = "mdl_other"
			svc.runtime.auth.Load().Names[subject.Current.Name] = row
		},
		func() {
			svc.runtime.auth.Load().ModelCreated[subject.Model.ID] = subject.Model.CreatedAt.Add(time.Millisecond)
		},
		func() { svc.runtime.auth.Load().Models[subject.Model.ID] = false },
	} {
		publish()
		svc.runtime.deniedModels.Delete(subject.Model.ID)
		change()
		if svc.runtimeModelAliasApplied(subject) {
			t.Fatal("missing/stale publication accepted")
		}
	}
	svc.runtime.deniedModels.Delete(subject.Model.ID)
	subject.Model.Status = "inactive"
	data.Models = []entity.Model{subject.Model}
	publish()
	if !svc.runtimeModelAliasApplied(subject) {
		t.Fatal("saved inactive configuration confused with native readiness")
	}
	svc.runtime = nil
	if svc.runtimeModelAliasApplied(subject) {
		t.Fatal("absent runtime proof accepted")
	}
}
