package handler

import (
	"strings"
	"testing"
)

func TestRuntimeInstallationStrictQuery(t *testing.T) {
	for _, q := range []string{"", "limit=1", "limit=100", "instance_id=ins_01m36yee4gkbns18pfcqqc75a3", "cursor=opaque"} {
		if _, err := parseRuntimeInstallationQuery(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, q := range []string{"unknown=1", "limit=", "limit=0", "limit=101", "limit=01", "limit=1&limit=2", "cursor=", "instance_id=", "cursor=%zz", "limit=+1", "limit=1.0", "cursor=x&cursor=y", "instance_id=x&instance_id=y", strings.Repeat("x", 513)} {
		if _, err := parseRuntimeInstallationQuery(q); err == nil {
			t.Fatal("invalid query accepted", q)
		}
	}
}
