package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRoleCreationDescriptionPresenceAndURI(t *testing.T) {
	for _, raw := range []string{`{"name":"Legacy","permissions":[]}`, `{"name":"Recorded","description":"First line\n第二行","permissions":[]}`} {
		request := SaveRoleRequest{RoleID: "rol_exact"}
		if err := json.Unmarshal([]byte(raw), &request); err != nil || request.RoleID != "rol_exact" || request.Permissions == nil {
			t.Fatal("creation/URI compatibility lost", err)
		}
		if strings.Contains(raw, "description") && (request.Description == nil || *request.Description != "First line\n第二行") {
			t.Fatal("description normalized")
		}
	}
	for _, value := range []string{`null`, `""`, `" trailing "`, `"Bad\tvalue"`, `"\ud800"`, `true`} {
		raw := `{"name":"Recorded","description":` + value + `,"permissions":[]}`
		request := SaveRoleRequest{Name: "preserved", RoleID: "rol_exact"}
		if err := json.Unmarshal([]byte(raw), &request); err == nil || request.Name != "preserved" || request.RoleID != "rol_exact" {
			t.Fatal("invalid explicit description fell back to legacy")
		}
	}
}
