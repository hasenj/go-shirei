package main

import (
	"strings"
	"testing"
)

func load(turn Stone, rows ...string) Game {
	n := len(rows)
	g := NewGame(n)
	g.Turn = turn
	for y, row := range rows {
		if len(row) != n {
			panic("row width " + row)
		}
		for x := 0; x < n; x++ {
			switch row[x] {
			case 'B':
				g.Stones[y*n+x] = Black
			case 'W':
				g.Stones[y*n+x] = White
			case '.':
			default:
				panic("cell")
			}
		}
	}
	return g
}

func dump(g *Game) string {
	var b strings.Builder
	for y := 0; y < g.N; y++ {
		for x := 0; x < g.N; x++ {
			switch g.At(x, y) {
			case Black:
				b.WriteByte('B')
			case White:
				b.WriteByte('W')
			default:
				b.WriteByte('.')
			}
		}
		if y+1 < g.N {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestPlay(t *testing.T) {
	t.Run("capture", func(t *testing.T) {
		g := load(Black,
			".B.",
			"BW.",
			".B.",
		)
		if ok, reason := g.Play(2, 1); !ok || reason != "" {
			t.Fatalf("play: ok=%v reason=%q", ok, reason)
		}
		if g.At(1, 1) != Empty || g.At(2, 1) != Black || g.Captured[Black] != 1 || g.Turn != White {
			t.Fatalf("captured=%d turn=%d\n%s", g.Captured[Black], g.Turn, dump(&g))
		}
	})

	t.Run("group", func(t *testing.T) {
		g := load(Black,
			"BBBB",
			"BWWB",
			"BB.B",
			"....",
		)
		if ok, reason := g.Play(2, 2); !ok || reason != "" {
			t.Fatalf("play: ok=%v reason=%q\n%s", ok, reason, dump(&g))
		}
		if g.At(1, 1) != Empty || g.At(2, 1) != Empty || g.Captured[Black] != 2 {
			t.Fatalf("captured=%d\n%s", g.Captured[Black], dump(&g))
		}
	})

	t.Run("capture before suicide", func(t *testing.T) {
		g := load(Black,
			"WB.",
			"W.B",
			"WB.",
		)
		if ok, reason := g.Play(1, 1); !ok || reason != "" {
			t.Fatalf("play: ok=%v reason=%q\n%s", ok, reason, dump(&g))
		}
		if g.Captured[Black] != 3 || g.At(0, 0) != Empty || g.At(1, 1) != Black {
			t.Fatalf("captured=%d\n%s", g.Captured[Black], dump(&g))
		}
	})

	t.Run("suicide", func(t *testing.T) {
		g := load(Black,
			"BBB",
			"B.B",
			"BBB",
		)
		if ok, reason := g.Play(1, 1); ok || reason != "suicide" {
			t.Fatalf("ok=%v reason=%q", ok, reason)
		}
		if g.Turn != Black || g.At(1, 1) != Empty || g.CanUndo() {
			t.Fatalf("turn=%d undo=%v\n%s", g.Turn, g.CanUndo(), dump(&g))
		}
	})

	t.Run("filling a live group", func(t *testing.T) {
		g := load(Black,
			"BB.",
			"B.B",
			"BBB",
		)
		if ok, reason := g.Play(1, 1); !ok || reason != "" {
			t.Fatalf("play: ok=%v reason=%q\n%s", ok, reason, dump(&g))
		}
	})

	t.Run("occupied", func(t *testing.T) {
		g := NewGame(5)
		if ok, _ := g.Play(1, 1); !ok {
			t.Fatal("first")
		}
		if ok, reason := g.Play(1, 1); ok || reason != "" || g.Turn != White {
			t.Fatalf("ok=%v reason=%q turn=%d", ok, reason, g.Turn)
		}
	})

	t.Run("ko", func(t *testing.T) {
		g := load(Black,
			".BW.",
			"BW.W",
			".BW.",
			"....",
		)
		if ok, reason := g.Play(2, 1); !ok || reason != "" {
			t.Fatalf("capture: ok=%v reason=%q\n%s", ok, reason, dump(&g))
		}
		if ok, reason := g.Play(1, 1); ok || reason != "ko" || g.Turn != White {
			t.Fatalf("ko: ok=%v reason=%q turn=%d\n%s", ok, reason, g.Turn, dump(&g))
		}
		g.Pass()
		g.Pass()
		if ok, reason := g.Play(1, 1); !ok || reason != "" {
			t.Fatalf("after pass: ok=%v reason=%q\n%s", ok, reason, dump(&g))
		}
		if g.Captured[White] != 1 || g.At(2, 1) != Empty || g.At(1, 1) != White {
			t.Fatalf("captured=%d\n%s", g.Captured[White], dump(&g))
		}
	})

	t.Run("undo", func(t *testing.T) {
		g := load(Black,
			"BBBB",
			"BWWB",
			"BB.B",
			"....",
		)
		before := dump(&g)
		if ok, _ := g.Play(2, 2); !ok {
			t.Fatal("play")
		}
		g.Pass()
		if !g.Undo() || !g.Undo() {
			t.Fatal("undo")
		}
		if dump(&g) != before || g.Turn != Black || g.Captured[Black] != 0 || g.CanUndo() {
			t.Fatalf("turn=%d captured=%d\n%s", g.Turn, g.Captured[Black], dump(&g))
		}
		if g.Undo() {
			t.Fatal("extra undo")
		}
	})
}
