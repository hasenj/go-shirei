package slay

import (
	"go.hasen.dev/generic"
)

// Rolling FNV hash to implement a synthetic scope id
// With help from LLMs, plus referencing hash/fnv for logic & magic values

type scopeId uint32

func newScopeId() scopeId {
	return 2166136261 // magic offset
}

func addChildScope[T any](s scopeId, n T) scopeId {
	b := generic.UnsafeRawBytes(&n)
	return addChildScopeFromBytes(s, b)
}

func addChildScopeFromBytes(s scopeId, b []byte) scopeId {
	// FNV-1a: for each byte, hash = (hash XOR byte) * prime
	for _, c := range b {
		s *= 16777619 // magic prime
		s ^= scopeId(c)
	}
	return s
}

func scopeIdFrom(id any) scopeId {
	s := newScopeId()
	return addChildScope(s, id)
}
