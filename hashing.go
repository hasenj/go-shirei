package shirei

import (
	"unsafe"

	"github.com/cespare/xxhash/v2"
	g "go.hasen.dev/generic"
)

// Hash writes the raw in-memory bytes of *v into the digest. Intended for small
// plain values; it hashes the memory representation, so results are not portable
// across architectures.
func Hash[T any](h *xxhash.Digest, v *T) {
	h.Write(g.UnsafeRawBytes(v))
}

// HashSlice writes the raw in-memory bytes of the slice's elements into the
// digest.
func HashSlice[T any](h *xxhash.Digest, v []T) {
	h.Write(g.UnsafeSliceBytes(v))
}

// HashString writes the bytes of s into the digest.
func HashString(h *xxhash.Digest, s string) {
	h.WriteString(s)
}

// HashStringHeader writes s's data pointer and length — not its bytes — into the
// digest. This is an identity hash: two strings sharing backing storage hash
// equal, but equal content in separate allocations does not.
func HashStringHeader(h *xxhash.Digest, s string) {
	var ptr = unsafe.StringData(s)
	var length = len(s)
	Hash(h, &ptr)
	Hash(h, &length)
}
