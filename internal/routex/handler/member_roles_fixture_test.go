package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Review and destination proof are read under the setup administrator; dispatch
// still uses the actor whose authority the assertion exercises.
func reviewedMemberRolesFixtureRequest(t *testing.T, router http.Handler, reviewCookie, requestCookie *http.Cookie, csrf, userID string, ids []string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/v1/admin/members/" + userID + "/roles"
	reviewResponse := identityRequest(router, "GET", path, "", reviewCookie, "")
	expectStatus(t, reviewResponse, 200)
	var review memberRolesFixtureWorkspace
	if err := json.Unmarshal(reviewResponse.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	ids = slices.Clone(ids)
	if ids == nil {
		ids = []string{}
	}
	slices.Sort(ids)
	body := memberRolesFixtureInput{RoleIDs: ids, RoleDefinitions: []memberRolesFixtureProof{}, BuiltinDefinitionETag: review.BuiltinRole.DefinitionETag, Reason: "Reviewed governance fixture assignment"}
	for _, id := range ids {
		proof := strings.Repeat("0", 64)
		if id != "rol_admin" && id != "rol_member" {
			req := httptest.NewRequest("GET", "http://routex.test"+path+"/"+id, nil)
			req.AddCookie(reviewCookie)
			req.Header.Set("If-Match", strconv.Quote(review.ETag))
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			expectStatus(t, out, 200)
			var detail struct {
				Role memberRolesFixtureSummary `json:"role"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &detail); err != nil {
				t.Fatal(err)
			}
			proof = detail.Role.DefinitionETag
		}
		body.RoleDefinitions = append(body.RoleDefinitions, memberRolesFixtureProof{ID: id, ETag: proof})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("PUT", "http://routex.test"+path, strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", strconv.Quote(review.ETag))
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(requestCookie)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	return out
}
