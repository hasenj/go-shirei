package widgets

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

// Exercise piece splitting, line-index updates, reversed multi-line selections,
// Unicode offsets, and both history directions against an independent string.
func TestTextDocumentEditSequences(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("ending-%q", ending), func(t *testing.T) {
			var d TextDocument
			reference := "日本語" + ending + "ab🙂cd" + ending + ending + "tail" + ending
			d.SetText(reference)
			rng := rand.New(rand.NewSource(7))
			check := func(want string) {
				t.Helper()
				if got := d.Text(); got != want {
					t.Fatalf("text=%q want %q", got, want)
				}
				lines := strings.Split(want, "\n")
				if d.LineCount() != len(lines) {
					t.Fatalf("line count %d != %d", d.LineCount(), len(lines))
				}
				for i, line := range lines {
					if i < len(lines)-1 {
						line = strings.TrimSuffix(line, "\r")
					}
					if d.Line(i) != line {
						t.Fatalf("line %d: %q != %q", i, d.Line(i), line)
					}
				}
				var b strings.Builder
				if n, err := d.WriteTo(&b); err != nil || n != int64(len(want)) || b.String() != want {
					t.Fatal("streamed export differs")
				}
			}
			for step := 0; step < 1500; step++ {
				lines := strings.Split(reference, "\n")
				position := func() (LinePos, int) {
					li := rng.Intn(len(lines))
					line := lines[li]
					if li < len(lines)-1 {
						line = strings.TrimSuffix(line, "\r")
					}
					r := rng.Intn(utf8.RuneCountInString(line) + 1)
					offset := len(string([]rune(line)[:r]))
					for i := 0; i < li; i++ {
						offset += len(lines[i]) + 1
					}
					return LinePos{li, r}, offset
				}
				a, ai := position()
				b, bi := position()
				insert := []string{"x", "日本", "🙂", "\n", "\n\n", "", "a\nb"}[rng.Intn(7)]
				if ai == bi && insert == "" {
					continue
				}
				d.SetSelection(a, b)
				previous := reference
				reference = reference[:min(ai, bi)] + strings.ReplaceAll(insert, "\n", ending) + reference[max(ai, bi):]
				d.ReplaceSelection(insert)
				check(reference)
				if !d.Undo() {
					t.Fatal("missing undo")
				}
				check(previous)
				anchor, cursor := d.Selection()
				if anchor != a || cursor != b {
					t.Fatal("undo selection differs")
				}
				if !d.Redo() {
					t.Fatal("missing redo")
				}
				check(reference)
			}
		})
	}
}

func TestTextDocumentTypingHistoryAndSavedState(t *testing.T) {
	var d TextDocument
	d.SetText("abc\r\n日本語\r\n")
	d.SetSelection(LinePos{1, 3}, LinePos{1, 3})
	for _, s := range []string{"x", "y", "z"} {
		d.apply(_EditCommand{Op: _EditInsert, Text: s}, nil)
	}
	d.MarkSaved()
	d.apply(_EditCommand{Op: _EditInsert, Text: "!"}, nil)
	if !d.Modified() {
		t.Fatal("edit is clean")
	}
	d.Undo()
	if d.Modified() {
		t.Fatal("undo to saved state is dirty")
	}
	d.Undo()
	if !d.Modified() || d.Text() != "abc\r\n日本語\r\n" {
		t.Fatal("typing is not a single undo group")
	}
	d.Redo()
	if d.Modified() {
		t.Fatal("redo to saved state is dirty")
	}
	for range 3 {
		d.apply(_EditCommand{Op: _EditDeleteBackward}, nil)
	}
	if d.Line(1) != "日本語" {
		t.Fatal(d.Text())
	}
	d.Undo()
	if d.Line(1) != "日本語xyz" || d.Modified() {
		t.Fatal("delete undo does not restore save")
	}
	d.SetSelection(LinePos{1, 0}, LinePos{1, 0})
	d.apply(_EditCommand{Op: _EditDeleteBackward}, nil)
	if d.Text() != "abc日本語xyz\r\n" {
		t.Fatal("CRLF join", d.Text())
	}
	d.Undo()
	if d.LineCount() != 3 {
		t.Fatal("join undo")
	}
	d.SetSelection(LinePos{}, LinePos{})
	d.apply(_EditCommand{Op: _EditInsert, Text: "branch"}, nil)
	if d.Redo() {
		t.Fatal("redo survives branched edit")
	}
}

func BenchmarkTextDocumentEditLarge(b *testing.B) {
	for _, lines := range []int{1000, 100000} {
		b.Run(fmt.Sprint(lines), func(b *testing.B) {
			var d TextDocument
			d.SetText(strings.Repeat("a line of Japanese 日本語 and source text\n", lines))
			d.SetSelection(LinePos{lines / 2, 5}, LinePos{lines / 2, 5})
			b.ResetTimer()
			b.ReportAllocs()
			for range b.N {
				d.ReplaceSelection("x")
				d.Undo()
			}
		})
	}
}
