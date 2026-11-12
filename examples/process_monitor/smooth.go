package main

import (
	"math"
	"time"
)

// cpuSmoothingHalfLife is the wall time over which an old CPU reading loses
// half its weight in the Smooth CPU filter.
const cpuSmoothingHalfLife = 3 * time.Second

// cpuSortBucket is the smallest difference in smoothed CPU percent that can
// move a row past another. Rows inside the same bucket stay in PID order.
const cpuSortBucket = 0.5

// ema is a time-aware exponential moving average. Each update weights the new
// sample by the elapsed wall time, so the smoothing does not change when the
// user picks a different refresh interval. After halfLife, the previous
// average contributes half as much as a sample taken now.
type ema struct {
	Value float64
	Last  time.Time
	Ready bool
}

func (e *ema) Update(x float64, at time.Time, halfLife time.Duration) {
	if !e.Ready {
		e.Value, e.Last, e.Ready = x, at, true
		return
	}
	dt := at.Sub(e.Last)
	if dt <= 0 || halfLife <= 0 {
		return
	}
	e.Last = at
	alpha := 1 - math.Exp2(-dt.Seconds()/halfLife.Seconds())
	e.Value += alpha * (x - e.Value)
}

func (e *ema) Reset() {
	*e = ema{}
}

// cpuDisplayValue is the CPU percent shown in the table and in the history
// header. Smooth CPU returns the filtered value once it has a measured
// reading; otherwise the raw sample is used. Unknown readings stay unknown.
func cpuDisplayValue(p *Process) float64 {
	if p.CPUPercent < 0 {
		return CPUPercentUnknown
	}
	if appData.smoothCPU && p.CPUSmooth.Ready {
		return p.CPUSmooth.Value
	}
	return p.CPUPercent
}

// cpuSortValue quantizes the displayed CPU so near-equal rows do not swap on
// every sample. The bucket is skipped until the filter has a measured value,
// so the first second after launch keeps the raw order.
func cpuSortValue(p *Process) float64 {
	v := cpuDisplayValue(p)
	if !appData.smoothCPU || !p.CPUSmooth.Ready || v < 0 {
		return v
	}
	return math.Floor(v / cpuSortBucket)
}
