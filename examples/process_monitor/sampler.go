package main

import "time"

type Sampler struct {
	prev *RawSnapshot
}

func (s *Sampler) Sample() (*ProcSnapshot, error) {
	raw, err := Collect()
	if err != nil {
		return nil, err
	}
	snap := computeSnapshot(s.prev, &raw)
	s.prev = &raw
	return snap, nil
}

func computeSnapshot(prev, curr *RawSnapshot) *ProcSnapshot {
	out := &ProcSnapshot{
		Time:             curr.Time,
		TotalMemoryBytes: curr.TotalMemoryBytes,
		UsedMemoryBytes:  curr.UsedMemoryBytes,
		HostCPUPercent:   hostCPUPercent(prev, curr),
		HostCPUMeasured:  prev != nil,
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
			ExePath:        raw.ExePath,
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
		p.PowerWatts = PowerWattsUnknown
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
				if cpuDelta >= 0 && procWall > 0 {
					p.CPUMeasured = true
				}
				if cpuDelta > 0 && procWall > 0 {
					p.CPUPercent = float64(cpuDelta) / float64(procWall) * 100
				}
				if !raw.EnergyUnknown && !prevRaw.EnergyUnknown && raw.EnergyNanoJoules >= prevRaw.EnergyNanoJoules {
					p.PowerWatts = wattsFromNano(raw.EnergyNanoJoules-prevRaw.EnergyNanoJoules, procWall)
				}
			}
		}
		out.Processes = append(out.Processes, p)
	}

	// Linux: no per-pid energy. Attribute package RAPL by this pid's share
	// of CPU time over the same window.
	if prev != nil && !curr.HostEnergyUnknown && !prev.HostEnergyUnknown &&
		curr.HostEnergyNanoJoules >= prev.HostEnergyNanoJoules {
		pkgDelta := curr.HostEnergyNanoJoules - prev.HostEnergyNanoJoules
		var cpuAll time.Duration
		for _, raw := range curr.Processes {
			if prevRaw, ok := prevByPID[raw.PID]; ok && !raw.MetricsUnknown && !prevRaw.MetricsUnknown {
				if d := raw.CPUTime - prevRaw.CPUTime; d > 0 {
					cpuAll += d
				}
			}
		}
		if pkgDelta > 0 && cpuAll > 0 {
			wall := curr.Time.Sub(prev.Time)
			for i := range out.Processes {
				p := &out.Processes[i]
				if p.PowerWatts >= 0 {
					continue // already have a real per-pid counter
				}
				raw := curr.Processes[i]
				prevRaw, ok := prevByPID[raw.PID]
				if !ok || raw.MetricsUnknown || prevRaw.MetricsUnknown {
					continue
				}
				cpuDelta := raw.CPUTime - prevRaw.CPUTime
				if cpuDelta <= 0 {
					p.PowerWatts = 0
					continue
				}
				share := uint64(float64(pkgDelta) * float64(cpuDelta) / float64(cpuAll))
				p.PowerWatts = wattsFromNano(share, wall)
			}
		}
	}
	return out
}

func wattsFromNano(nanoJoules uint64, wall time.Duration) float64 {
	if wall <= 0 {
		return 0
	}
	return float64(nanoJoules) / 1e9 / wall.Seconds()
}

func hostCPUPercent(prev, curr *RawSnapshot) float64 {
	if prev == nil || curr == nil {
		return 0
	}
	dt := curr.HostCPU.Total() - prev.HostCPU.Total()
	if dt == 0 {
		return 0
	}
	idle := (curr.HostCPU.Idle + curr.HostCPU.IOWait) - (prev.HostCPU.Idle + prev.HostCPU.IOWait)
	busy := dt - idle
	if busy > dt {
		busy = dt
	}
	return float64(busy) / float64(dt) * 100
}

func CollectSampleWindow(samples int, period time.Duration) (*ProcSnapshot, time.Duration, error) {
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
