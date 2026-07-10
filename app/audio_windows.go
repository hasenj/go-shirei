//go:build windows

package app

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// winmm waveOut output via the standard syscall package — no cgo, the same
// linking style as win32backend, so GOOS=windows cross-compiles keep working
// (and Wine implements winmm well). waveOut is the compatibility choice:
// it has worked since Windows 3.1 at the cost of latency — 4 × 512 frames
// ≈ 46ms at 44.1kHz. If that ever matters, WASAPI can replace this behind
// the same StartAudio without touching apps.
//
// Model: prepared buffers rotate through the device; an auto-reset event is
// signaled on each completion, and a feeder goroutine refills done buffers
// via audioFill and resubmits them. The feeder runs pinned to its own
// time-critical thread so a loaded machine can't starve it past the queue
// depth.
//
// Death and revival: if the device path dies (host sleep under Wine, an
// audio-service restart, a removed endpoint), waveOutWrite starts failing
// and completions stop — with an INFINITE wait the feeder would block
// forever. So feeders wait with a timeout and carry a generation number:
// the shared watchdog notices the fill-stall, bumps the generation (stale
// feeders exit on their next wakeup), tears the session down, and opens a
// fresh one.

var (
	// winmm.dll is a System32 DLL; like win32backend's kernel32/user32/gdi32,
	// NewLazyDLL's default search resolving it from System32 is acceptable here.
	winmm                      = syscall.NewLazyDLL("winmm.dll")
	procWaveOutOpen            = winmm.NewProc("waveOutOpen")
	procWaveOutPrepareHeader   = winmm.NewProc("waveOutPrepareHeader")
	procWaveOutUnprepareHeader = winmm.NewProc("waveOutUnprepareHeader")
	procWaveOutWrite           = winmm.NewProc("waveOutWrite")
	procWaveOutReset           = winmm.NewProc("waveOutReset")
	procWaveOutClose           = winmm.NewProc("waveOutClose")
	procWaveOutPause           = winmm.NewProc("waveOutPause")

	audioKernel32         = syscall.NewLazyDLL("kernel32.dll")
	procAudioCreateEventW = audioKernel32.NewProc("CreateEventW")
	procAudioSetEvent     = audioKernel32.NewProc("SetEvent")
	procGetCurrentThread  = audioKernel32.NewProc("GetCurrentThread")
	procSetThreadPriority = audioKernel32.NewProc("SetThreadPriority")
)

const (
	waveMapper          = 0xFFFFFFFF // "pick a suitable device"
	waveFormatIEEEFloat = 3
	callbackEvent       = 0x00050000
	whdrDone            = 0x00000001

	waveChunkFrames = 512 // ~11.6ms at 44.1kHz
	waveNumBufs     = 4

	threadPriorityTimeCritical = 15
)

type waveFormatEx struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	cbSize         uint16
}

type waveHdr struct {
	data          uintptr
	bufferLength  uint32
	bytesRecorded uint32
	user          uintptr
	flags         uint32
	loops         uint32
	next          uintptr
	reserved      uintptr
}

// the current session; replaced wholesale on rebuild. Only audioStart and
// the watchdog (never concurrent: the watchdog starts after audioStart and
// is the sole rebuilder) write these. waveGen is atomic because stale
// feeder goroutines poll it to learn they've been retired.
var (
	waveGen        atomic.Uint64
	waveHandle     uintptr
	waveEvent      uintptr
	waveHdrs       []waveHdr
	waveSampleRate int
)

func audioStart(sampleRate int) error {
	if err := procWaveOutOpen.Find(); err != nil {
		return fmt.Errorf("winmm unavailable: %w", err)
	}
	waveSampleRate = sampleRate
	if err := waveOpen(); err != nil {
		return err
	}
	audioLastFill.Store(time.Now().UnixNano())
	go audioWatchdog(waveRebuild)
	return nil
}

// waveOpen opens a fresh session: device, event, prepared buffers primed
// with silence, and a feeder goroutine bound to the current generation.
func waveOpen() error {
	event, _, _ := procAudioCreateEventW.Call(0, 0, 0, 0) // auto-reset, unsignaled
	if event == 0 {
		return fmt.Errorf("CreateEvent failed")
	}

	format := waveFormatEx{
		formatTag:      waveFormatIEEEFloat,
		channels:       1,
		samplesPerSec:  uint32(waveSampleRate),
		avgBytesPerSec: uint32(waveSampleRate * 4),
		blockAlign:     4,
		bitsPerSample:  32,
	}
	var h uintptr
	rc, _, _ := procWaveOutOpen.Call(uintptr(unsafe.Pointer(&h)), waveMapper,
		uintptr(unsafe.Pointer(&format)), event, 0, callbackEvent)
	if rc != 0 {
		syscall.CloseHandle(syscall.Handle(event))
		return fmt.Errorf("waveOutOpen: MMSYSERR %d", rc)
	}

	// bufs/hdrs stay referenced by the feeder goroutine below, which keeps
	// the memory the device writes into (and reads from) alive
	bufs := make([][]float32, waveNumBufs)
	hdrs := make([]waveHdr, waveNumBufs)
	for i := range bufs {
		bufs[i] = make([]float32, waveChunkFrames)
		hdrs[i] = waveHdr{
			data:         uintptr(unsafe.Pointer(&bufs[i][0])),
			bufferLength: waveChunkFrames * 4,
		}
		rc, _, _ = procWaveOutPrepareHeader.Call(h, uintptr(unsafe.Pointer(&hdrs[i])), unsafe.Sizeof(hdrs[i]))
		if rc != 0 {
			procWaveOutClose.Call(h)
			syscall.CloseHandle(syscall.Handle(event))
			return fmt.Errorf("waveOutPrepareHeader: MMSYSERR %d", rc)
		}
		// prime the queue with silence; completions start the rotation
		procWaveOutWrite.Call(h, uintptr(unsafe.Pointer(&hdrs[i])), unsafe.Sizeof(hdrs[i]))
	}

	waveHandle, waveEvent, waveHdrs = h, event, hdrs

	fill := audioFill
	gen := waveGen.Load()
	go func() {
		// Pin the feeder to its own OS thread and raise it to time-critical:
		// at default priority a loaded machine can withhold the CPU longer
		// than the ~35ms of queued audio, which drains the queue and gaps
		// the output (see notes/windows-audio-stutter-analysis.md).
		// Deliberately never unlocked — a retired feeder's return makes the
		// runtime terminate the thread, so the elevated priority can't leak
		// back into the scheduler's thread pool.
		runtime.LockOSThread()
		th, _, _ := procGetCurrentThread.Call()
		procSetThreadPriority.Call(th, threadPriorityTimeCritical)
		for {
			// timed wait: a dead device signals nothing, and a stale feeder
			// must notice the generation moved on
			syscall.WaitForSingleObject(syscall.Handle(event), 1000)
			if waveGen.Load() != gen {
				return
			}
			for i := range hdrs {
				if hdrs[i].flags&whdrDone != 0 {
					hdrs[i].flags &^= whdrDone
					fill(bufs[i])
					audioNoteFill()
					procWaveOutWrite.Call(h, uintptr(unsafe.Pointer(&hdrs[i])), unsafe.Sizeof(hdrs[i]))
				}
			}
		}
	}()
	return nil
}

// waveRebuild is the watchdog's restart hook: retire the old session (the
// stale feeder exits on generation mismatch) and open a fresh one.
func waveRebuild() error {
	waveGen.Add(1)
	// wake the old feeder now so it exits promptly rather than within 1s,
	// then give it a beat before the handles go away under it
	procAudioSetEvent.Call(waveEvent)
	time.Sleep(50 * time.Millisecond)

	// best-effort teardown; on a dead device these may fail — that's fine
	procWaveOutReset.Call(waveHandle)
	for i := range waveHdrs {
		procWaveOutUnprepareHeader.Call(waveHandle, uintptr(unsafe.Pointer(&waveHdrs[i])), unsafe.Sizeof(waveHdrs[i]))
	}
	procWaveOutClose.Call(waveHandle)
	syscall.CloseHandle(syscall.Handle(waveEvent))

	return waveOpen()
}

// audioPause freezes the device without tearing anything down — it
// simulates the stream dying, so the watchdog test can exercise the
// revival path. Test hook only.
func audioPause() error {
	if rc, _, _ := procWaveOutPause.Call(waveHandle); rc != 0 {
		return fmt.Errorf("waveOutPause: MMSYSERR %d", rc)
	}
	return nil
}
