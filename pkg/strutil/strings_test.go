package strutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		name   string
		inputs []string
		want   string
	}{
		{name: "all empty", inputs: []string{"", "  ", "\t"}, want: ""},
		{name: "first wins", inputs: []string{"a", "b"}, want: "a"},
		{name: "skip blanks", inputs: []string{"", " ", "b", "c"}, want: "b"},
		{name: "no inputs", inputs: nil, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstNonEmpty(tc.inputs...); got != tc.want {
				t.Fatalf("FirstNonEmpty(%v) = %q, want %q", tc.inputs, got, tc.want)
			}
		})
	}
}
