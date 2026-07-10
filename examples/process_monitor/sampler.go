package main

import "time"

type Sampler struct {
	prev *RawSnapshot
}

func (s *Sampler) Sample() (*Snapshot, error) {
	raw, err := Collect()
	if err != nil {
		return nil, err
	}
	snap := computeSnapshot(s.prev, &raw)
	s.prev = &raw
	return snap, nil
}

func computeSnapshot(prev, curr *RawSnapshot) *Snapshot {
	out := &Snapshot{
		Time:             curr.Time,
		TotalMemoryBytes: curr.TotalMemoryBytes,
		UsedMemoryBytes:  curr.UsedMemoryBytes,
		Processes:        make([]ProcInfo, 0, len(curr.Processes)),
	}

	prevByPID := make(map[int]RawProcSample)
	if prev != nil {
		for _, p := range prev.Processes {
			prevByPID[p.PID] = p
		}
	}

	var wall time.Duration
	if prev != nil {
		wall = curr.Time.Sub(prev.Time)
	}

	for _, raw := range curr.Processes {
		p := ProcInfo{
			PID:            raw.PID,
			PPID:           raw.PPID,
			Name:           raw.Name,
			Cmdline:        cmdlineOf(raw),
			User:           raw.User,
			State:          raw.State,
			RSSBytes:       raw.RSSBytes,
			StartTime:      raw.StartTime,
			Threads:        raw.Threads,
			MetricsUnknown: raw.MetricsUnknown,
		}
		if curr.TotalMemoryBytes > 0 {
			p.MemPercent = float64(raw.RSSBytes) / float64(curr.TotalMemoryBytes) * 100
		}
		if raw.MetricsUnknown {
			p.CPUPercent = CPUPercentUnknown
		} else if prevRaw, ok := prevByPID[raw.PID]; ok && !prevRaw.MetricsUnknown {
			// If StartTime is known on both samples, use it to avoid carrying CPU
			// history across PID reuse.
			if raw.StartTime.IsZero() || prevRaw.StartTime.IsZero() || raw.StartTime.Equal(prevRaw.StartTime) {
				// Prefer this pid's own read stamps: the counters were read
				// mid-loop, not at the snapshot timestamp, and only the
				// per-pid window makes the delta exact (see RawProcSample).
				procWall := wall
				if !raw.SampleTime.IsZero() && !prevRaw.SampleTime.IsZero() {
					procWall = raw.SampleTime.Sub(prevRaw.SampleTime)
				}
				cpuDelta := raw.CPUTime - prevRaw.CPUTime
				if cpuDelta > 0 && procWall > 0 {
					p.CPUPercent = float64(cpuDelta) / float64(procWall) * 100
				}
			}
		}
		out.Processes = append(out.Processes, p)
	}
	return out
}

func CollectSampleWindow(samples int, period time.Duration) (*Snapshot, time.Duration, error) {
	if samples < 2 {
		samples = 2
	}
	if period <= 0 {
		period = time.Millisecond
	}

	interval := period / time.Duration(samples-1)
	if interval <= 0 {
		interval = time.Nanosecond
	}

	raws := make([]RawSnapshot, 0, samples)
	start := time.Now()
	for i := 0; i < samples; i++ {
		if i > 0 {
			target := start.Add(interval * time.Duration(i))
			if sleep := time.Until(target); sleep > 0 {
				time.Sleep(sleep)
			}
		}
		raw, err := Collect()
		if err != nil {
			return nil, 0, err
		}
		raws = append(raws, raw)
	}

	first := &raws[0]
	last := &raws[len(raws)-1]
	return computeSnapshot(first, last), last.Time.Sub(first.Time), nil
}

// cmdlineOf prefers the real argv string; falls back to the executable path
// so the UI still has something path-like to show on platforms that only
// expose the image name (darwin historically put the path in Cmdline).
func cmdlineOf(raw RawProcSample) string {
	if raw.Cmdline != "" {
		return raw.Cmdline
	}
	return raw.ExePath
}
