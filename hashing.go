package slay

import (
	"unsafe"

	"github.com/cespare/xxhash/v2"
	g "go.hasen.dev/generic"
)

func Hash[T any](h *xxhash.Digest, v *T) {
	h.Write(g.UnsafeRawBytes(v))
}

func HashSlice[T any](h *xxhash.Digest, v []T) {
	h.Write(g.UnsafeSliceBytes(v))
}

func HashString(h *xxhash.Digest, s string) {
	h.WriteString(s)
}

func HashStringHeader(h *xxhash.Digest, s string) {
	var ptr = unsafe.StringData(s)
	var length = len(s)
	Hash(h, &ptr)
	Hash(h, &length)
}
