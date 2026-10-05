package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestMemberRolesStrictHTTPBoundaryBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/roles", ctrl.GetMemberRoles)
	router.GET("/members/:user_id/roles/candidates", ctrl.MemberRoleCandidates)
	router.GET("/members/:user_id/roles/:role_id", ctrl.GetMemberRoleDefinition)
	router.PUT("/members/:user_id/roles", ctrl.SetReviewedMemberRoles)
	input := service.MemberRolesInput{RoleIDs: []string{}, RoleDefinitions: []service.MemberRoleDefinitionProof{}, BuiltinDefinitionETag: strings.Repeat("a", 64), Reason: "Reviewed clear"}
	raw, _ := json.Marshal(input)
	valid := `"` + strings.Repeat("b", 64) + `"`
	for _, test := range []struct{ method, path, body, etag string }{
		{"GET", "/members/usr_subject/roles?roles=true", "", ""},
		{"GET", "/members/subject%20/roles", "", ""},
		{"GET", "/members/usr_subject/roles/candidates?user_id=foreign", "", valid},
		{"GET", "/members/usr_subject/roles/candidates?limit=0", "", valid},
		{"GET", "/members/usr_subject/roles/candidates?limit=51", "", valid},
		{"GET", "/members/usr_subject/roles/candidates?q=x&q=x", "", valid},
		{"GET", "/members/usr_subject/roles/candidates?limit=", "", valid},
		{"GET", "/members/usr_subject/roles/candidates?foreign=", "", valid},
		{"GET", "/members/usr_subject/roles/candidates", "", ""},
		{"GET", "/members/usr_subject/roles/rol_subject", "", `W/` + valid},
		{"GET", "/members/usr_subject/roles/role%20", "", valid},
		{"PUT", "/members/usr_subject/roles", string(raw), ""},
		{"PUT", "/members/usr_subject/roles", string(raw), strings.Repeat("a", 64)},
		{"PUT", "/members/usr_subject/roles", string(raw), `"` + strings.Repeat("A", 64) + `"`},
		{"PUT", "/members/usr_subject/roles?actor=foreign", string(raw), valid},
		{"PUT", "/members/usr_subject/roles", strings.Replace(string(raw), `"reason":`, `"reason":"first","reason":`, 1), valid},
		{"PUT", "/members/usr_subject/roles", strings.Replace(string(raw), `"role_ids":[]`, `"role_ids":null`, 1), valid},
		{"PUT", "/members/usr_subject/roles", strings.Repeat(" ", 64*1024+1), valid},
	} {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("If-Match", test.etag)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(test.method, test.path, response.Code)
		}
	}
}
func TestMemberRoleCandidateExactSelectorsAndStrongReview(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/candidates", func(c *fox.Context) error {
		_, err := memberRoleCandidateFilter(c)
		if err != nil {
			return err
		}
		_, err = memberMetadataETag(c)
		if err != nil {
			return err
		}
		c.Status(204)
		return nil
	})
	for _, q := range []string{"", "?q=literal%25_&limit=50", "?cursor=opaque", "?limit=25"} {
		req := httptest.NewRequest("GET", "/candidates"+q, nil)
		req.Header.Set("If-Match", `"`+strings.Repeat("a", 64)+`"`)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 204 {
			t.Fatal(q, out.Code)
		}
	}
	req := httptest.NewRequest("GET", "/candidates", nil)
	req.Header.Add("If-Match", `"`+strings.Repeat("a", 64)+`"`)
	req.Header.Add("If-Match", `"`+strings.Repeat("b", 64)+`"`)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if out.Code != 400 {
		t.Fatal("ambiguous validator")
	}
}
