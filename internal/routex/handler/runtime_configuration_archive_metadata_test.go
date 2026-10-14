package handler

import "testing"

func TestRuntimeArchiveMetadataStrictQuery(t *testing.T) {
	for _, q := range []string{"", "limit=1", "limit=100", "cursor=opaque"} {
		if _, err := parseRuntimeArchiveMetadataQuery(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, q := range []string{"unknown=1", "limit=", "limit=0", "limit=101", "limit=01", "limit=1&limit=2", "cursor=", "cursor=%zz", "instance_id=ins_exact"} {
		if _, err := parseRuntimeArchiveMetadataQuery(q); err == nil {
			t.Fatal("invalid query accepted", q)
		}
	}
}
