package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
)

func effectiveModelsFixture(t *testing.T) (*Service, *memberEffectiveModelsData) {
	t.Helper()
	s, meta, runtimeData := memberModelsProjectionFixture(t)
	now := meta.Subject.CreatedAt
	team := memberEffectiveTeam{ID: "tea_one", Name: "Recorded Team", Status: entity.ResourceActive, CreatedAt: now, MembershipID: "tmm_one", MembershipUserID: meta.Subject.ID, MembershipTeamID: "tea_one", MembershipRole: entity.TeamMember, MembershipStatus: entity.ResourceActive}
	runtimeData.TeamSessionData = &teamSessionRuntimeData{Teams: []entity.Team{{ID: team.ID, Name: team.Name, Status: team.Status, CreatedAt: team.CreatedAt}}, Memberships: []entity.TeamMembership{{ID: team.MembershipID, TeamID: team.ID, UserID: meta.Subject.ID, Role: team.MembershipRole, Status: team.MembershipStatus}}, Grants: []entity.TeamModelGrant{{TeamID: team.ID, ModelID: meta.Models[0].ID}}}
	for i, protocol := range []string{entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		suffix := string(rune('b' + i))
		conn := meta.Connections[0]
		conn.ID = "con_" + suffix
		conn.Protocol = protocol
		pm := meta.ProviderModels[0]
		pm.ID = "pmd_" + suffix
		pm.ConnectionID = conn.ID
		crd := meta.Credentials[0]
		crd.ID = "crd_" + suffix
		crd.ConnectionID = conn.ID
		var err error
		crd.Ciphertext, err = s.secrets.Seal(crd.ID, "controlled-secret")
		if err != nil {
			t.Fatal(err)
		}
		bind := meta.Bindings[0]
		bind.ID = "bnd_" + suffix
		bind.ProviderModelID = pm.ID
		access := entity.CredentialModelAccess{CredentialID: crd.ID, ProviderModelID: pm.ID}
		meta.Connections = append(meta.Connections, conn)
		runtimeData.Connections = append(runtimeData.Connections, conn)
		meta.ProviderModels = append(meta.ProviderModels, pm)
		runtimeData.ProviderModels = append(runtimeData.ProviderModels, pm)
		meta.Credentials = append(meta.Credentials, crd)
		runtimeData.Credentials = append(runtimeData.Credentials, crd)
		meta.Bindings = append(meta.Bindings, bind)
		runtimeData.Bindings = append(runtimeData.Bindings, bind)
		meta.Access = append(meta.Access, access)
		runtimeData.Access = append(runtimeData.Access, access)
	}
	auth := buildRuntimeAuthorization(runtimeData, time.Now().Add(time.Minute))
	auth.SourceDigest = "same"
	routes, err := s.buildRuntimeRoutes(runtimeData)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime.auth.Store(auth)
	s.runtime.routes.Store(&runtimeRoutes{ID: "cfg_effective", Digest: "same", Models: routes})
	meta.Prices = nil
	meta.Rates = nil
	for _, pm := range meta.ProviderModels {
		price := entity.ModelPrice{ID: "price_" + pm.ID, ProviderModelID: pm.ID}
		meta.Prices = append(meta.Prices, price)
		meta.Rates = append(meta.Rates, entity.PriceRate{ModelPriceID: price.ID, Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true}, entity.PriceRate{ModelPriceID: price.ID, Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "EUR", Amount: "0.000000000000000001", Enabled: true})
	}
	hashes := map[string]string{}
	for _, c := range meta.Credentials {
		hashes[c.ID] = personalHash(c.Ciphertext)
	}
	return s, &memberEffectiveModelsData{CipherHashes: hashes, Metadata: meta, TeamsRead: true, Teams: []memberEffectiveTeam{team}, TeamGrants: slices.Clone(runtimeData.TeamSessionData.Grants)}
}
func TestMemberEffectiveModelsFourProtocolsAndIndependentVisibility(t *testing.T) {
	s, data := effectiveModelsFixture(t)
	result := s.projectMemberEffectiveModels(data)
	if result.UserID != data.Metadata.Subject.ID || result.TeamEnrichment != "included" || result.UnionCompleteness != "complete" || len(result.Items) != 1 {
		t.Fatal(result)
	}
	row := result.Items[0]
	if row.Availability != "ready" || len(row.Protocols) != 4 || len(row.Sources) != 2 || len(row.Sources[0].Protocols) != 4 || len(row.Sources[1].Protocols) != 4 || row.Type != nil || row.UpdatedAt != nil || row.InputPrice.State != "priced" || row.InputPrice.Rate.Amount != "0" || row.OutputPrice.Rate.Amount != "0.000000000000000001" || row.OutputPrice.Rate.Currency != "EUR" {
		t.Fatal(row)
	}
	for _, flags := range [][2]bool{{true, false}, {false, true}, {false, false}} {
		data.Metadata.ProvidersRead = flags[0]
		data.Metadata.PricesRead = flags[1]
		row := s.projectMemberEffectiveModels(data).Items[0]
		if (row.Providers != nil) != flags[0] || (row.InputPrice.State != "unauthorized") != flags[1] {
			t.Fatal("independent metadata gates", flags, row)
		}
	}
	data.TeamsRead = false
	data.Metadata.ProvidersRead = false
	data.Metadata.PricesRead = false
	result = s.projectMemberEffectiveModels(data)
	if result.TeamEnrichment != "not_authorized" || result.UnionCompleteness != "unknown" || len(result.Items[0].Sources) != 1 || result.Items[0].Providers != nil || result.Items[0].InputPrice.State != "unauthorized" {
		t.Fatal(result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"tea_one", "Recorded Team", "tmm_one", "source_request_id", "personal_grant_revision", "selectable", "Ciphertext", "credential_id", "receipt"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("unauthorized/private fact", private)
		}
	}
}
func TestMemberEffectiveModelsCompleteSourceProofAndIndependentFences(t *testing.T) {
	cases := []string{"current", "personal_fence", "personal_revision", "personal_provenance", "team_fence", "member_fence", "member_rejoined", "team_creation", "subject_creation", "published_disabled", "team_added", "team_removed", "team_source_added", "team_source_null", "team_source_replaced", "expired", "retained_old_routes", "new_auth", "route_tombstone", "inactive_subject", "inactive_model", "busy", "publication_busy"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			s, d := effectiveModelsFixture(t)
			auth := s.runtime.auth.Load()
			switch name {
			case "personal_fence":
				s.runtime.deniedPersonalGrants.Store(d.Metadata.Subject.ID, true)
			case "personal_revision":
				d.Metadata.Subject.PersonalGrantRevision = strings.Repeat("b", 64)
			case "personal_provenance":
				source := "mar_new"
				d.Metadata.Grants[0].SourceRequestID = &source
			case "team_fence":
				s.runtime.deniedTeams.Store("tea_one", true)
			case "member_fence":
				s.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey("tea_one", d.Metadata.Subject.ID), true)
			case "member_rejoined":
				d.Teams[0].MembershipID = "tmm_rejoined"
			case "team_creation":
				d.Teams[0].CreatedAt = d.Teams[0].CreatedAt.Add(time.Microsecond)
			case "subject_creation":
				d.Metadata.Subject.CreatedAt = d.Metadata.Subject.CreatedAt.Add(time.Microsecond)
			case "published_disabled":
				auth.UserProofs[d.Metadata.Subject.ID] = runtimeUserProof{CreatedAt: d.Metadata.Subject.CreatedAt, Enabled: false}
				auth.PersonalGrantStates[d.Metadata.Subject.ID] = runtimePersonalGrantState{}
			case "team_added":
				auth.Teams["tea_one"].Models["mdl_extra"] = true
			case "team_removed":
				delete(auth.Teams["tea_one"].Models, "mdl_one")
			case "team_source_added":
				source := "tmr_new"
				d.TeamGrants[0].SourceRequestID = &source
			case "team_source_null":
				auth.TeamGrantSources["tea_one"] = map[string]string{"mdl_one": "tmr_previous"}
			case "team_source_replaced":
				source := "tmr_new"
				d.TeamGrants[0].SourceRequestID = &source
				auth.TeamGrantSources["tea_one"] = map[string]string{"mdl_one": "tmr_old"}
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "retained_old_routes":
				s.runtime.routes.Load().Digest = "old"
			case "new_auth":
				newAuth := *auth
				s.runtime.auth.Store(&newAuth)
				if s.memberEffectiveTeamProof(d, d.Teams[0], auth, s.runtime.routes.Load()) {
					t.Fatal("captured old authorization accepted")
				}
			case "route_tombstone":
				s.runtime.deniedModels.Store("mdl_one", true)
			case "inactive_subject":
				d.Metadata.Subject.Disabled = true
			case "inactive_model":
				d.Metadata.Models[0].Status = entity.ResourceDisabled
			case "publication_busy":
				s.runtime.publication.Lock()
				defer s.runtime.publication.Unlock()
			case "busy":
				s.runtime.mu.Lock()
				defer s.runtime.mu.Unlock()
			}
			row := s.projectMemberEffectiveModels(d).Items[0]
			personal, team := row.Sources[0], row.Sources[1]
			personalOnly := slices.Contains([]string{"personal_fence", "personal_revision", "personal_provenance"}, name)
			teamOnly := slices.Contains([]string{"team_fence", "member_fence", "member_rejoined", "team_creation", "team_added", "team_removed", "team_source_added", "team_source_null", "team_source_replaced"}, name)
			if name == "current" || name == "new_auth" {
				if row.Availability != "ready" || personal.Availability != "ready" || team.Availability != "ready" {
					t.Fatal(row)
				}
			} else if personalOnly {
				if personal.Availability != "unknown" || team.Availability != "ready" || row.Availability != "ready" {
					t.Fatal(row)
				}
			} else if teamOnly {
				if team.Availability != "unknown" || personal.Availability != "ready" || row.Availability != "ready" {
					t.Fatal(row)
				}
			} else if name == "inactive_subject" || name == "inactive_model" {
				if row.Availability != "unavailable" || len(row.Protocols) != 0 {
					t.Fatal(row)
				}
			} else if row.Availability != "unknown" || len(row.Protocols) != 0 || row.InputPrice.State != "unavailable" {
				t.Fatal(row)
			}
		})
	}
}
func TestMemberEffectiveModelsInputRejectsBeforeDatabase(t *testing.T) {
	s := &Service{}
	for _, bad := range []string{"", strings.Repeat("x", 31), "usr_target ", "usr_目标", "usr_target/other"} {
		if _, err := s.MemberEffectiveModels(context.Background(), bad, "usr_target"); err != apperrors.ErrUnauthorized {
			t.Fatal(err)
		}
		if _, err := s.MemberEffectiveModels(context.Background(), "usr_actor", bad); err != apperrors.ErrBadRequest {
			t.Fatal(err)
		}
	}
}

func TestMemberEffectiveModelsTeamOnlyAndSameNameSourcesRemainDistinct(t *testing.T) {
	s, data := effectiveModelsFixture(t)
	data.Metadata.Grants = nil
	second := data.Teams[0]
	second.ID = "tea_two"
	second.MembershipTeamID = second.ID
	second.MembershipID = "tmm_two"
	data.Teams = append(data.Teams, second)
	data.TeamGrants = append(data.TeamGrants, entity.TeamModelGrant{TeamID: second.ID, ModelID: "mdl_one"})
	auth := s.runtime.auth.Load()
	auth.Teams[second.ID] = runtimeTeam{CreatedAt: second.CreatedAt, Members: map[string]string{data.Metadata.Subject.ID: second.MembershipID}, Models: map[string]bool{"mdl_one": true}}
	page := s.projectMemberEffectiveModels(data)
	if len(page.Items) != 1 || len(page.Items[0].Sources) != 2 || page.Items[0].Sources[0].Kind != "team" || *page.Items[0].Sources[0].TeamID == *page.Items[0].Sources[1].TeamID || page.Items[0].Availability != "ready" {
		t.Fatal(page)
	}
	data.TeamsRead = false
	page = s.projectMemberEffectiveModels(data)
	if len(page.Items) != 0 || page.TeamEnrichment != "not_authorized" || page.UnionCompleteness != "unknown" {
		t.Fatal("Team-only row leaked", page)
	}
}

func TestMemberEffectiveModelsUnpublishedCipherCannotClaimReady(t *testing.T) {
	s, data := effectiveModelsFixture(t)
	// Current SQL/source changes while the old authenticated route remains published.
	data.Metadata.Credentials[0].Ciphertext = "changed-ciphertext"
	data.CipherHashes[data.Metadata.Credentials[0].ID] = personalHash(data.Metadata.Credentials[0].Ciphertext)
	row := s.projectMemberEffectiveModels(data).Items[0]
	if row.Availability != "unknown" || len(row.Protocols) != 0 || row.InputPrice.State != "unavailable" {
		t.Fatal("ciphertext-only source mismatch claimed ready", row.Availability)
	}
}

func TestMemberEffectiveCipherComparisonPreservesOpaqueMaterialAndExactIdentity(t *testing.T) {
	s, data := effectiveModelsFixture(t)
	routes := s.runtime.routes.Load()
	if !memberEffectiveModelCipherProof("mdl_one", data.CipherHashes, routes) {
		t.Fatal("same captured source rejected")
	}
	for _, name := range []string{"missing", "alias", "same-plaintext-new-envelope", "old-routes-new-auth"} {
		t.Run(name, func(t *testing.T) {
			current := map[string]string{}
			for id, hash := range data.CipherHashes {
				current[id] = hash
			}
			id := data.Metadata.Credentials[0].ID
			if name == "alias" {
				current[strings.ToUpper(id)] = current[id]
			}
			delete(current, id)
			if name == "same-plaintext-new-envelope" || name == "old-routes-new-auth" {
				current[id] = personalHash("new-authenticated-envelope")
			}
			if memberEffectiveModelCipherProof("mdl_one", current, routes) {
				t.Fatal("new or absent source borrowed old route")
			}
		})
	}
}

func TestMemberEffectiveModelsStableOrderingDoesNotBorrowDatabaseCollation(t *testing.T) {
	s, data := effectiveModelsFixture(t)
	id := "mdl_AA"
	model := data.Metadata.Models[0]
	model.ID = id
	data.Metadata.Models = append(data.Metadata.Models, model)
	data.Metadata.Names = append(data.Metadata.Names, entity.ModelName{ModelID: id, CurrentModelID: &id, Name: "exact-order"})
	data.Metadata.Grants = append(data.Metadata.Grants, entity.UserModelGrant{UserID: data.Metadata.Subject.ID, ModelID: id})
	page := s.projectMemberEffectiveModels(data)
	if len(page.Items) != 2 || page.Items[0].ID != id || page.Items[1].ID != "mdl_one" {
		t.Fatal("database order entered stable API", page)
	}
}
