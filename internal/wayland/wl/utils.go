package wl

import (
	"math"
	"sync"
)

// BytePool is a sync pool for byte buffers
type BytePool struct {
	sync.Pool
}

var (
	bytePool = &BytePool{
		sync.Pool{
			New: func() interface{} {
				return make([]byte, 16)
			},
		},
	}
)

// Take (BytePool Take) takes a specific number of bytes from the pool
func (bp *BytePool) Take(n int) []byte {
	buf := bp.Get().([]byte)
	if cap(buf) < n {
		t := make([]byte, len(buf), n)
		copy(t, buf)
		buf = t
	}
	return buf[:n]
}

// Give (BytePool Give) returns a specific number of bytes to the pool
func (bp *BytePool) Give(b []byte) {
	bp.Put(b) //nolint:staticcheck // SA6002: slices are already reference types
}

func float64frombits(b uint64) float64 { return math.Float64frombits(b) }
func float64bits(f float64) uint64     { return math.Float64bits(f) }

// FixedToFloat converts a fixed precision Wayland decimal encoded as int32 to a float64
func FixedToFloat(fixed int32) float64 {
	dat := ((int64(1023 + 44)) << 52) + (1 << 51) + int64(fixed)
	return float64frombits(uint64(dat)) - float64(3<<43)
}

// FloatToFixed converts a float64 to a fixed precision Wayland decimal encoded as int32
func FloatToFixed(v float64) int32 {
	dat := v + float64(int64(3)<<(51-8))
	return int32(float64bits(dat))
}
