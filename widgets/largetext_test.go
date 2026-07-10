package widgets

import "testing"

func TestScanLineStartsAndLineAt(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		want  []string
		limit int
	}{
		{name: "empty", text: "", want: []string{""}},
		{name: "one line", text: "hello", want: []string{"hello"}},
		{name: "two lines", text: "a\nb", want: []string{"a", "b"}},
		{name: "trailing nl", text: "a\nb\n", want: []string{"a", "b", ""}},
		{name: "blank middle", text: "a\n\nb", want: []string{"a", "", "b"}},
		{name: "tip limit", text: "a\nb\nc\nd", want: []string{"a", "b"}, limit: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			starts, lastEnd := scanLineStarts(tc.text, tc.limit)
			if len(starts) != len(tc.want) {
				t.Fatalf("starts len=%d want %d (%v)", len(starts), len(tc.want), starts)
			}
			for i, w := range tc.want {
				got := lineAt(tc.text, starts, lastEnd, i)
				if got != w {
					t.Fatalf("line %d = %q, want %q", i, got, w)
				}
			}
		})
	}
}
