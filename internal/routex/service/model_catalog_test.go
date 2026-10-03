package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func catalogGrantFixture(modelID string) memberCatalogGrant {
	return memberCatalogGrant{ModelID: modelID, NameModelID: modelID, NameOwnerID: modelID, GrantModelID: modelID, Name: modelID, Status: "active", UserID: "usr_actor", CreatedAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.FixedZone("test", 3600))}
}

func TestMemberModelCatalogSourcesAndExactScope(t *testing.T) {
	personal := catalogGrantFixture("mdl_one")
	team := personal
	team.TeamID, team.MembershipTeamID, team.GrantTeamID, team.TeamName, team.TeamStatus, team.MembershipStatus, team.Role = "tem_one", "tem_one", "tem_one", "One", "active", "active", "member"
	items, err := memberCatalogRecords([]memberCatalogGrant{personal, personal}, []memberCatalogGrant{team, team}, "usr_actor", "")
	if err != nil || len(items) != 1 || len(items[0].Sources) != 2 || !items[0].Sources[0].InvocationSupported || items[0].Sources[0].TeamID != nil || items[0].Sources[1].InvocationSupported || items[0].CreatedAt.Location() != time.UTC || items[0].PersonalAvailable {
		t.Fatalf("invalid deduplicated sources: %#v %v", items, err)
	}
	for _, field := range []string{"actor", "model", "name", "name_owner", "grant", "team", "membership", "team_status", "membership_status", "role", "target"} {
		t.Run(field, func(t *testing.T) {
			row := team
			target := ""
			switch field {
			case "actor":
				row.UserID = "USR_actor"
			case "model":
				row.Status = "ACTIVE"
			case "name":
				row.NameModelID = "MDL_one"
			case "name_owner":
				row.NameOwnerID = "MDL_one"
			case "grant":
				row.GrantModelID = "MDL_one"
			case "team":
				row.GrantTeamID = "TEM_one"
			case "membership":
				row.MembershipTeamID = "TEM_one"
			case "team_status":
				row.TeamStatus = "ACTIVE"
			case "membership_status":
				row.MembershipStatus = "ACTIVE"
			case "role":
				row.Role = "MEMBER"
			case "target":
				target = "MDL_one"
			}
			items, err := memberCatalogRecords(nil, []memberCatalogGrant{row}, "usr_actor", target)
			if err != nil || len(items) != 0 {
				t.Fatal("folded or mismatched identity promoted scope")
			}
		})
	}
}

func TestMemberModelCatalogCompleteBounds(t *testing.T) {
	for _, kind := range []string{"models", "teams", "grants"} {
		t.Run(kind, func(t *testing.T) {
			var personal, teams []memberCatalogGrant
			switch kind {
			case "models":
				for i := 0; i <= memberCatalogModelLimit; i++ {
					personal = append(personal, catalogGrantFixture(fmt.Sprintf("mdl_%04d", i)))
				}
			case "grants":
				for i := 0; i <= memberCatalogGrantLimit; i++ {
					personal = append(personal, catalogGrantFixture("mdl_one"))
				}
			case "teams":
				for i := 0; i <= memberCatalogTeamLimit; i++ {
					row := catalogGrantFixture("mdl_one")
					row.TeamID = fmt.Sprintf("tem_%03d", i)
					row.MembershipTeamID = row.TeamID
					row.GrantTeamID = row.TeamID
					row.TeamStatus = "active"
					row.MembershipStatus = "active"
					row.Role = "owner"
					teams = append(teams, row)
				}
			}
			items, err := memberCatalogRecords(personal, teams, "usr_actor", "")
			if !errors.Is(err, ErrModelCatalogOverflow) || items != nil {
				t.Fatalf("partial overflow result: %v %v", items, err)
			}
		})
	}
	for _, value := range []string{"", "mdl space", "mdl.unsafe", "模型", "1234567890123456789012345678901"} {
		if validMemberCatalogID(value) {
			t.Fatalf("accepted invalid ID %q", value)
		}
	}
	if !validMemberCatalogID("mdl_historical-1") {
		t.Fatal("bounded historical ID rejected")
	}
}

func TestMemberModelCatalogInclusiveLimits(t *testing.T) {
	personal := make([]memberCatalogGrant, memberCatalogModelLimit)
	for i := range personal {
		personal[i] = catalogGrantFixture(fmt.Sprintf("mdl_%04d", i))
	}
	items, err := memberCatalogRecords(personal, nil, "usr_actor", "")
	if err != nil || len(items) != memberCatalogModelLimit {
		t.Fatal("inclusive model limit rejected", err)
	}
	teams := make([]memberCatalogGrant, memberCatalogGrantLimit)
	for i := range teams {
		row := catalogGrantFixture("mdl_one")
		row.TeamID = fmt.Sprintf("tem_%03d", i%memberCatalogTeamLimit)
		row.MembershipTeamID = row.TeamID
		row.GrantTeamID = row.TeamID
		row.TeamStatus = "active"
		row.MembershipStatus = "active"
		row.Role = "member"
		teams[i] = row
	}
	items, err = memberCatalogRecords(nil, teams, "usr_actor", "")
	if err != nil || len(items) != 1 || len(items[0].Sources) != memberCatalogTeamLimit {
		t.Fatal("inclusive Team/grant limit rejected", err)
	}
}

func catalogTeamGrantFixture(modelID string) memberCatalogGrant {
	row := catalogGrantFixture(modelID)
	row.TeamID, row.MembershipTeamID, row.GrantTeamID = "tem_one", "tem_one", "tem_one"
	row.TeamName, row.TeamStatus, row.MembershipStatus, row.Role = "One", entity.ResourceActive, entity.ResourceActive, entity.TeamMember
	return row
}

func TestMemberModelCatalogTeamProtocolsFollowReadyNativeRuntime(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			svc, _ := teamGatewayFixture(t, "https://provider.example/v1")
			routes := svc.runtime.routes.Load()
			candidates := routes.Models["mdl_one"]
			candidates[0].Route.Protocol = protocol
			routes.Models["mdl_one"] = candidates
			items, err := memberCatalogRecords(nil, []memberCatalogGrant{catalogTeamGrantFixture("mdl_one")}, "usr_actor", "")
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := svc.gatewayModelMetadata(context.Background(), []string{"mdl_one"})
			if err != nil {
				t.Fatal(err)
			}
			applyMemberCatalogMetadata(items, metadata, true)
			if len(items) != 1 || items[0].PersonalAvailable || items[0].Sources[0].InvocationSupported || !reflect.DeepEqual(items[0].Sources[0].InvocationProtocols, []string{protocol}) {
				t.Fatal("ready native Team source lost its link or promised Personal calling", items)
			}
			svc.InvalidateRuntimeCredential("crd_one")
			metadata, err = svc.gatewayModelMetadata(context.Background(), []string{"mdl_one"})
			if err != nil {
				t.Fatal(err)
			}
			applyMemberCatalogMetadata(items, metadata, true)
			if len(items[0].Sources[0].InvocationProtocols) != 0 || items[0].PersonalAvailable || items[0].Sources[0].InvocationSupported {
				t.Fatal("unready native route kept a Team invocation link", items)
			}
		})
	}
}

func TestMemberModelCatalogNativeProjectionKeepsSourceAndCapabilityBoundaries(t *testing.T) {
	protocols := []string{entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent, "future_native"}
	capabilities := map[string][]string{entity.ProtocolOpenAIResponses: {"image", "pdf"}}
	metadata := map[string]gatewayModelMetadata{"mdl_one": {Protocols: protocols, InputCapabilities: capabilities}}
	items, err := memberCatalogRecords([]memberCatalogGrant{catalogGrantFixture("mdl_one")}, []memberCatalogGrant{catalogTeamGrantFixture("mdl_one")}, "usr_actor", "")
	if err != nil {
		t.Fatal(err)
	}
	applyMemberCatalogMetadata(items, metadata, true)
	if !items[0].PersonalAvailable || !items[0].Sources[0].InvocationSupported || !reflect.DeepEqual(items[0].Sources[0].InvocationProtocols, protocols) || !reflect.DeepEqual(items[0].Sources[1].InvocationProtocols, protocols[:3]) || items[0].Sources[1].InvocationSupported || !reflect.DeepEqual(items[0].InputCapabilities, capabilities) {
		t.Fatal("Team protocol projection changed Personal authority or model metadata", items)
	}
	items[0].Sources[1].InvocationProtocols[0] = "mutated"
	if metadata["mdl_one"].Protocols[0] != entity.ProtocolOpenAIResponses || items[0].Sources[0].InvocationProtocols[0] != entity.ProtocolOpenAIResponses {
		t.Fatal("Team protocol array aliases another source's authority")
	}
	applyMemberCatalogMetadata(items, metadata, false)
	if len(items[0].Sources[1].InvocationProtocols) != 0 || items[0].Sources[1].InvocationSupported || !items[0].PersonalAvailable || !reflect.DeepEqual(items[0].InputCapabilities, capabilities) {
		t.Fatal("database metadata advertised a missing Team runtime or removed independent Personal metadata", items)
	}
}
