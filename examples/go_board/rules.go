package main

// Game is one Go position. Stones is row-major, index y*N+x, with the origin
// at the top left. Captured[c] counts stones of the opposite color that c has
// taken. Ban is the stone layout the player to move may not recreate; nil
// means the ko rule is not restricting this move.
type Game struct {
	N        int
	Stones   []Stone
	Turn     Stone
	Captured [3]int
	LastX    int
	LastY    int
	HasLast  bool
	Passed   bool
	Ban      []Stone
	past     []snap
}

type Stone uint8

const (
	Empty Stone = iota
	Black
	White
)

func other(c Stone) Stone {
	if c == Black {
		return White
	}
	return Black
}

type snap struct {
	stones   []Stone
	turn     Stone
	captured [3]int
	lastX    int
	lastY    int
	hasLast  bool
	passed   bool
	ban      []Stone
}

func NewGame(n int) Game {
	return Game{
		N:      n,
		Stones: make([]Stone, n*n),
		Turn:   Black,
	}
}

func (g *Game) CanUndo() bool { return len(g.past) > 0 }

func (g *Game) At(x, y int) Stone { return g.Stones[y*g.N+x] }

// Play places the current player's stone at (x, y). A false result leaves the
// position unchanged. reason is "suicide" or "ko" for those rejections, and
// empty when the point is occupied, out of range, or the move is played.
func (g *Game) Play(x, y int) (ok bool, reason string) {
	next, taken, reason := resolve(g.Stones, g.N, x, y, g.Turn, g.Ban)
	if next == nil {
		return false, reason
	}
	g.push()
	g.Ban = append([]Stone(nil), g.Stones...)
	g.Stones = next
	g.Captured[g.Turn] += taken
	g.LastX, g.LastY = x, y
	g.HasLast = true
	g.Passed = false
	g.Turn = other(g.Turn)
	return true, ""
}

// Pass switches the turn and clears the ko restriction.
func (g *Game) Pass() {
	g.push()
	g.HasLast = false
	g.Passed = true
	g.Ban = nil
	g.Turn = other(g.Turn)
}

func (g *Game) Undo() bool {
	n := len(g.past)
	if n == 0 {
		return false
	}
	s := g.past[n-1]
	g.past = g.past[:n-1]
	g.Stones = s.stones
	g.Turn = s.turn
	g.Captured = s.captured
	g.LastX, g.LastY = s.lastX, s.lastY
	g.HasLast = s.hasLast
	g.Passed = s.passed
	g.Ban = s.ban
	return true
}

// Legal reports whether Play would change the position.
func (g *Game) Legal(x, y int) bool {
	next, _, _ := resolve(g.Stones, g.N, x, y, g.Turn, g.Ban)
	return next != nil
}

func (g *Game) push() {
	g.past = append(g.past, snap{
		stones:   append([]Stone(nil), g.Stones...),
		turn:     g.Turn,
		captured: g.Captured,
		lastX:    g.LastX,
		lastY:    g.LastY,
		hasLast:  g.HasLast,
		passed:   g.Passed,
		ban:      append([]Stone(nil), g.Ban...),
	})
}

var ortho = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// resolve applies the capture and suicide checks. next is nil when the move
// is not played. Opponent groups with no liberties are removed before the
// played stone's own liberties are counted.
func resolve(stones []Stone, n, x, y int, color Stone, ban []Stone) (next []Stone, taken int, reason string) {
	if x < 0 || y < 0 || x >= n || y >= n {
		return nil, 0, ""
	}
	if stones[y*n+x] != Empty {
		return nil, 0, ""
	}
	next = append([]Stone(nil), stones...)
	next[y*n+x] = color
	opp := other(color)
	seen := make([]bool, n*n)
	for _, d := range ortho {
		nx, ny := x+d[0], y+d[1]
		if nx < 0 || ny < 0 || nx >= n || ny >= n {
			continue
		}
		j := ny*n + nx
		if next[j] != opp || seen[j] {
			continue
		}
		members, libs := flood(next, n, nx, ny, seen)
		if libs == 0 {
			for _, k := range members {
				next[k] = Empty
			}
			taken += len(members)
		}
	}
	if _, libs := flood(next, n, x, y, make([]bool, n*n)); libs == 0 {
		return nil, 0, "suicide"
	}
	if ban != nil && sameLayout(next, ban) {
		return nil, 0, "ko"
	}
	return next, taken, ""
}

func flood(stones []Stone, n, x, y int, seen []bool) (members []int, libs int) {
	color := stones[y*n+x]
	start := y*n + x
	seen[start] = true
	stack := []int{start}
	libSeen := make([]bool, n*n)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		members = append(members, i)
		cx, cy := i%n, i/n
		for _, d := range ortho {
			nx, ny := cx+d[0], cy+d[1]
			if nx < 0 || ny < 0 || nx >= n || ny >= n {
				continue
			}
			j := ny*n + nx
			switch stones[j] {
			case Empty:
				if !libSeen[j] {
					libSeen[j] = true
					libs++
				}
			case color:
				if !seen[j] {
					seen[j] = true
					stack = append(stack, j)
				}
			}
		}
	}
	return members, libs
}

func sameLayout(a, b []Stone) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
