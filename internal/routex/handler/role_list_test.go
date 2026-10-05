package handler

import (
	"encoding/json"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func TestRoleListCountDTOIsListOnlyAndPreservesZero(t *testing.T) {
	seven := int64(7)
	for _, count := range []*int64{nil, new(int64), &seven} {
		response := roleResponse(service.RoleRecord{Role: entity.Role{ID: "rol_test", Name: "Test"}, Permissions: []string{}, MemberCount: count})
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		value, present := fields["member_count"]
		if present != (count != nil) {
			t.Fatal("non-list response invented a count, or list lost zero", string(raw))
		}
		if count != nil {
			var got int64
			if json.Unmarshal(value, &got) != nil || got != *count {
				t.Fatal(string(raw), count)
			}
		}
	}
}
