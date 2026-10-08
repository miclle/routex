package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	"reflect"
	"testing"
)

func TestPublicNameReservationProjectionPreservesExactHistoricalReservations(t *testing.T) {
	candidates := []string{"gpt-5.2", "gpt-5.2-2025-12-11", "claude-sonnet-4-6"}
	rows := []entity.ModelName{{Name: candidates[0]}, {Name: candidates[2]}}
	before := append([]entity.ModelName(nil), rows...)
	got, err := projectModelCreationPublicNames(candidates, rows)
	want := []ModelCreationPublicName{{Name: candidates[0], Available: false}, {Name: candidates[1], Available: true}, {Name: candidates[2], Available: false}}
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(rows, before) {
		t.Fatal(got, err)
	}
	for _, bad := range [][]entity.ModelName{{{Name: "GPT-5.2"}}, {{Name: "gpt-5.2 "}}, {{Name: "private/arbitrary"}}, {{Name: candidates[0]}, {Name: candidates[0]}}} {
		if _, err := projectModelCreationPublicNames(candidates, bad); err == nil {
			t.Fatal("corrupt or nonexact query row treated as available")
		}
	}
	if _, err := projectModelCreationPublicNames([]string{"a", "a"}, nil); err == nil {
		t.Fatal("duplicate candidate accepted")
	}
}
