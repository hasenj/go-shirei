package widgets

// TextRing is a fixed-capacity, append-only store for log-like text.
//
// Two independent rings share one monotonic byte-stream timeline:
//
//   - buf: physical byte storage of Cap bytes. head/tail are monotonic stream
//     offsets; live bytes are [head, tail) at buf[off%Cap]. When a write would
//     exceed Cap, head advances (oldest bytes forgotten). No shifting.
//
//   - starts: fixed-capacity ring of line-start stream offsets. Line ids are
//     monotonic (firstID .. nextID). Physical slot for id L is
//     starts[L%maxLines]. When the line ring is full, firstID advances and the
//     oldest boundary is forgotten — again no shifting. Max lines and byte Cap
//     need not stay in lockstep; a line whose start falls before head is
//     dropped when head moves, and forgotten line boundaries may leave an
//     unaddressable byte prefix until head catches up.
//
// DefaultTextRingCap is 20MiB; DefaultTextRingMaxLines is 256Ki lines.

const DefaultTextRingCap = 20 << 20     // 20 MiB
const DefaultTextRingMaxLines = 256 << 10 // 262144 lines

// TextRing holds recent log bytes and newline-delimited line starts.
type TextRing struct {
	buf []byte

	// head / tail are monotonic stream byte offsets. Bytes currently stored
	// are the half-open range [head, tail). Physically they live at
	// buf[off%cap].
	head, tail int64

	// starts is a fixed ring: line id L begins at stream offset
	// starts[L%len(starts)]. Retained ids are [firstID, nextID).
	starts  []int64
	firstID int64 // oldest retained line id (also LogView's eviction cursor)
	nextID  int64 // next line id to assign

	droppedBytes int64
	droppedLines int64
}

// NewTextRing returns an empty ring with the given byte capacity.
// byteCap <= 0 selects DefaultTextRingCap. Line capacity is DefaultTextRingMaxLines.
func NewTextRing(byteCap int) *TextRing {
	return NewTextRingSize(byteCap, DefaultTextRingMaxLines)
}

// NewTextRingSize returns an empty ring with explicit byte and line capacities.
// Non-positive values select the defaults.
func NewTextRingSize(byteCap, maxLines int) *TextRing {
	if byteCap <= 0 {
		byteCap = DefaultTextRingCap
	}
	if maxLines <= 0 {
		maxLines = DefaultTextRingMaxLines
	}
	return &TextRing{
		buf:    make([]byte, byteCap),
		starts: make([]int64, maxLines),
	}
}

// Cap returns the byte-ring capacity.
func (r *TextRing) Cap() int {
	if r == nil {
		return 0
	}
	return len(r.buf)
}

// MaxLines returns the line-ring capacity.
func (r *TextRing) MaxLines() int {
	if r == nil {
		return 0
	}
	return len(r.starts)
}

// Bytes returns how many bytes are currently retained in the byte ring.
func (r *TextRing) Bytes() int64 {
	if r == nil {
		return 0
	}
	return r.tail - r.head
}

// Len returns the number of lines with retained boundaries.
func (r *TextRing) Len() int {
	if r == nil {
		return 0
	}
	return int(r.nextID - r.firstID)
}

// DroppedBytes / DroppedLines report lifetime forget totals (byte-head advances
// and line-boundary forgets from either max-lines or head catching a start).
func (r *TextRing) DroppedBytes() int64 {
	if r == nil {
		return 0
	}
	return r.droppedBytes
}
func (r *TextRing) DroppedLines() int64 {
	if r == nil {
		return 0
	}
	return r.droppedLines
}

// LineID returns the stable monotonic id for the line at index i (0 .. Len-1).
func (r *TextRing) LineID(i int) int64 {
	return r.firstID + int64(i)
}

// Line returns a copy of line i without its trailing newline.
func (r *TextRing) Line(i int) string {
	if r == nil || i < 0 || i >= r.Len() {
		return ""
	}
	lo := r.lineStart(i)
	hi := r.lineEnd(i)
	if hi < lo {
		return ""
	}
	return string(r.copyRange(lo, hi))
}

func (r *TextRing) lineStart(i int) int64 {
	id := r.firstID + int64(i)
	n := int64(len(r.starts))
	return r.starts[id%n]
}

func (r *TextRing) lineEnd(i int) int64 {
	// exclusive end of line content (newline not included)
	if i+1 < r.Len() {
		return r.lineStart(i+1) - 1
	}
	if r.tail > r.lineStart(i) && r.at(r.tail-1) == '\n' {
		return r.tail - 1
	}
	return r.tail
}

// AppendLine appends s plus a trailing newline as one log line.
// If s itself contains newlines, each segment becomes its own line.
func (r *TextRing) AppendLine(s string) {
	if r == nil || len(r.buf) == 0 {
		return
	}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			r.appendOneLine(s[start:i])
			start = i + 1
		}
	}
	r.appendOneLine(s[start:])
}

// Append writes raw bytes, splitting into lines on '\n'. A trailing fragment
// without a newline is kept as an incomplete last line and completed by a
// later Append that supplies the newline.
func (r *TextRing) Append(p []byte) {
	if r == nil || len(r.buf) == 0 || len(p) == 0 {
		return
	}
	for len(p) > 0 {
		nl := -1
		for i, b := range p {
			if b == '\n' {
				nl = i
				break
			}
		}
		if nl < 0 {
			r.appendFragment(p)
			return
		}
		r.appendFragment(p[:nl])
		r.finishLine()
		p = p[nl+1:]
	}
}

func (r *TextRing) appendOneLine(s string) {
	r.appendFragment([]byte(s))
	r.finishLine()
}

func (r *TextRing) appendFragment(p []byte) {
	if len(p) == 0 {
		return
	}
	// truncate a single fragment that alone exceeds capacity
	if len(p) >= len(r.buf) {
		p = p[len(p)-len(r.buf)+1:] // leave room for the newline
	}
	r.ensure(len(p))
	if r.Len() == 0 || r.lineClosed() {
		r.recordStart(r.tail)
	}
	r.write(p)
}

func (r *TextRing) finishLine() {
	r.ensure(1)
	if r.Len() == 0 || r.lineClosed() {
		r.recordStart(r.tail) // empty line
	}
	r.write([]byte{'\n'})
}

func (r *TextRing) lineClosed() bool {
	return r.tail > r.head && r.at(r.tail-1) == '\n'
}

// recordStart notes a new line at stream offset off under the next monotonic id.
func (r *TextRing) recordStart(off int64) {
	n := int64(len(r.starts))
	if r.nextID-r.firstID >= n {
		r.firstID++
		r.droppedLines++
	}
	r.starts[r.nextID%n] = off
	r.nextID++
}

// ensure makes room for n more bytes by advancing head. Line starts that fall
// before the new head are forgotten (O(dropped), each line once).
func (r *TextRing) ensure(n int) {
	cap64 := int64(len(r.buf))
	need := r.tail - r.head + int64(n)
	if need <= cap64 {
		return
	}
	newHead := r.tail + int64(n) - cap64
	if newHead > r.head {
		r.droppedBytes += newHead - r.head
		r.head = newHead
	}
	r.forgetLinesBeforeHead()
}

func (r *TextRing) forgetLinesBeforeHead() {
	n := int64(len(r.starts))
	if n == 0 {
		return
	}
	for r.firstID < r.nextID {
		off := r.starts[r.firstID%n]
		if off >= r.head {
			break
		}
		r.firstID++
		r.droppedLines++
	}
}

func (r *TextRing) write(p []byte) {
	c := len(r.buf)
	for len(p) > 0 {
		off := int(r.tail % int64(c))
		n := copy(r.buf[off:], p)
		p = p[n:]
		r.tail += int64(n)
	}
}

func (r *TextRing) at(streamOff int64) byte {
	c := int64(len(r.buf))
	return r.buf[streamOff%c]
}

func (r *TextRing) copyRange(lo, hi int64) []byte {
	if hi <= lo {
		return nil
	}
	n := int(hi - lo)
	out := make([]byte, n)
	c := len(r.buf)
	start := int(lo % int64(c))
	if start+n <= c {
		copy(out, r.buf[start:start+n])
		return out
	}
	n1 := c - start
	copy(out, r.buf[start:])
	copy(out[n1:], r.buf[:n-n1])
	return out
}
