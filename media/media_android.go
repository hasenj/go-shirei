//go:build android

package media

/*
#include "media_android.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/androidbackend"
)

// Player owns one Android MediaPlayer + SurfaceTexture. Create it with Open;
// call View from the frame function to place the video.
type Player struct {
	jniMu sync.Mutex
	m     *C.ShireiAndroidMedia
	key   string

	stop      chan struct{}
	done      chan struct{}
	wake      chan struct{}
	startOnce sync.Once
	started   atomic.Bool
	closeOnce sync.Once
	unregLife func()

	mu               sync.Mutex
	id               shirei.ImageId
	w, h             int
	playing          bool
	ended            bool
	loop             bool
	muted            bool
	volume           float32
	closed           bool
	needFrame        bool
	needFrameAt      time.Time
	playedSinceStart bool
	playStartedAt    time.Time
	seekPending      bool
	seekTarget       float64
	seekAt           time.Time
	err              error
	position         time.Duration
	duration         time.Duration
}

// Open opens path (local file path or content:// / http(s):// / file:// URI)
// for playback on Android.
func Open(path string) (*Player, error) {
	isURI := strings.HasPrefix(path, "content://") ||
		strings.HasPrefix(path, "http://") ||
		strings.HasPrefix(path, "https://") ||
		strings.HasPrefix(path, "file://")

	target := path
	if !isURI {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, err
		}
		target = abs
	}

	vm := androidbackend.JavaVM()
	if vm == nil {
		return nil, fmt.Errorf("media: Android JavaVM unavailable")
	}
	act := androidbackend.Context{}.Activity()

	cpath := C.CString(target)
	defer C.free(unsafe.Pointer(cpath))
	uriFlag := C.int(0)
	if isURI {
		uriFlag = 1
	}
	var errBuf [256]C.char
	m := C.shirei_android_media_open(vm, act, cpath, uriFlag, &errBuf[0], C.size_t(len(errBuf)))
	if m == nil {
		return nil, fmt.Errorf("media: %s", C.GoString(&errBuf[0]))
	}

	var st C.ShireiAndroidState
	C.shirei_android_media_poll(m, &st)

	p := &Player{
		m:           m,
		w:           int(st.width),
		h:           int(st.height),
		duration:    time.Duration(st.dur_ms) * time.Millisecond,
		volume:      1,
		needFrame:   true,
		needFrameAt: time.Now(),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		wake:        make(chan struct{}, 1),
	}
	if p.duration < 0 {
		p.duration = 0
	}
	p.key = fmt.Sprintf("media/%d", nextPlayerID.Add(1))
	p.unregLife = androidbackend.OnLifecycle(p.onLifecycle)
	return p, nil
}

func (p *Player) onLifecycle(paused bool) {
	if paused {
		p.Pause()
		p.mu.Lock()
		p.needFrame = true
		p.needFrameAt = time.Now()
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	closed := p.closed
	if !closed {
		p.needFrame = true
		p.needFrameAt = time.Now()
	}
	p.mu.Unlock()
	if !closed {
		p.kick()
		shirei.RequestNextFrame()
	}
}

// Play starts or resumes playback.
func (p *Player) Play() {
	p.start()
	p.jniMu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.jniMu.Unlock()
		return
	}
	now := time.Now()
	ended := p.ended
	p.playing = true
	p.ended = false
	p.needFrame = true
	p.needFrameAt = now
	p.playedSinceStart = false
	p.playStartedAt = now
	if ended {
		p.position = 0
		p.seekPending = true
		p.seekTarget = 0
		p.seekAt = now
	}
	p.mu.Unlock()

	if p.m != nil {
		if ended {
			C.shirei_android_media_seek(p.m, 0)
		}
		C.shirei_android_media_play(p.m)
	}
	p.jniMu.Unlock()
	p.kick()
	shirei.RequestNextFrame()
}

// Pause pauses playback without releasing the current frame.
func (p *Player) Pause() {
	p.jniMu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.jniMu.Unlock()
		return
	}
	p.playing = false
	p.mu.Unlock()

	if p.m != nil {
		C.shirei_android_media_pause(p.m)
	}
	p.jniMu.Unlock()
	shirei.RequestNextFrame()
}

// SetMuted mutes or unmutes playback.
func (p *Player) SetMuted(muted bool) {
	p.jniMu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.jniMu.Unlock()
		return
	}
	p.muted = muted
	vol := p.volume
	p.mu.Unlock()

	if muted {
		vol = 0
	}
	if p.m != nil {
		C.shirei_android_media_set_volume(p.m, C.float(vol))
	}
	p.jniMu.Unlock()
	shirei.RequestNextFrame()
}

// Muted reports whether playback is muted.
func (p *Player) Muted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.muted
}

// SetVolume sets the player volume in [0, 1].
func (p *Player) SetVolume(volume float32) {
	if math.IsNaN(float64(volume)) {
		return
	}
	if volume < 0 {
		volume = 0
	} else if volume > 1 {
		volume = 1
	}
	p.jniMu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.jniMu.Unlock()
		return
	}
	p.volume = volume
	muted := p.muted
	p.mu.Unlock()

	effective := volume
	if muted {
		effective = 0
	}
	if p.m != nil {
		C.shirei_android_media_set_volume(p.m, C.float(effective))
	}
	p.jniMu.Unlock()
	shirei.RequestNextFrame()
}

// Volume returns the player volume in [0, 1].
func (p *Player) Volume() float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// Seek jumps to seconds from the start.
func (p *Player) Seek(sec float64) {
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return
	}
	p.start()
	p.seekTo(sec)
}

func (p *Player) seekTo(sec float64) {
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return
	}
	if sec < 0 {
		sec = 0
	}
	p.jniMu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.jniMu.Unlock()
		return
	}
	if p.duration > 0 {
		max := p.duration.Seconds()
		if sec > max {
			sec = max
		}
	}
	now := time.Now()
	p.position = seconds(sec)
	p.ended = false
	p.needFrame = true
	p.needFrameAt = now
	p.playedSinceStart = false
	p.playStartedAt = now
	p.seekPending = true
	p.seekTarget = sec
	p.seekAt = now
	p.mu.Unlock()

	if p.m != nil {
		C.shirei_android_media_seek(p.m, C.int(math.Round(sec*1000)))
	}
	p.jniMu.Unlock()
	p.kick()
	shirei.RequestNextFrame()
}

// SetLoop controls whether reaching the end restarts from the beginning.
func (p *Player) SetLoop(loop bool) {
	p.jniMu.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.jniMu.Unlock()
		return
	}
	p.loop = loop
	p.mu.Unlock()

	v := C.int(0)
	if loop {
		v = 1
	}
	if p.m != nil {
		C.shirei_android_media_set_loop(p.m, v)
	}
	p.jniMu.Unlock()
}

// Playing reports whether playback is intended to be running.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// Ended reports whether playback reached the end and stopped.
func (p *Player) Ended() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ended
}

// Position returns the last sampled playback position.
func (p *Player) Position() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.position
}

// Duration returns the media duration, or zero until it is known.
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.duration
}

// Size returns the video pixel dimensions.
func (p *Player) Size() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.w, p.h
}

// ImageId returns the current video frame's shirei image handle, or 0 before
// the first frame is registered.
func (p *Player) ImageId() shirei.ImageId {
	p.start()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.id
}

// Err returns the playback error, if any.
func (p *Player) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// View places the current video frame inside maxSize, preserving aspect ratio.
func (p *Player) View(maxSize shirei.Vec2) {
	p.start()

	p.mu.Lock()
	id, w, h := p.id, p.w, p.h
	p.mu.Unlock()

	if w <= 0 || h <= 0 {
		w, h = 16, 9
	}
	if maxSize[0] < 1 || maxSize[1] < 1 {
		maxSize = shirei.Vec2{640, 360}
	}
	scale := min(maxSize[0]/float32(w), maxSize[1]/float32(h))
	if scale <= 0 {
		return
	}
	size := shirei.Vec2{float32(w) * scale, float32(h) * scale}

	if id == 0 || shirei.LookupImage(id) == nil {
		shirei.Container(shirei.Attrs(
			shirei.FixSize(size[0], size[1]),
			shirei.Background(0, 0, 5, 1),
			shirei.Corners(6),
		), func() {})
		return
	}
	shirei.ImageViewAt(id, size)
}

// Close stops playback and releases the MediaPlayer and SurfaceTexture.
func (p *Player) Close() {
	p.closeOnce.Do(func() {
		if p.unregLife != nil {
			p.unregLife()
		}
		p.mu.Lock()
		p.closed = true
		p.playing = false
		p.id = 0
		p.mu.Unlock()

		close(p.stop)
		p.startOnce.Do(func() {})
		if p.started.Load() {
			<-p.done
		}

		p.mu.Lock()
		p.id = 0
		p.mu.Unlock()
		shirei.ReleaseImage(p.key)

		p.jniMu.Lock()
		if p.m != nil {
			C.shirei_android_media_close(p.m)
			p.m = nil
		}
		p.jniMu.Unlock()
	})
}

func (p *Player) start() {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	p.startOnce.Do(func() {
		p.started.Store(true)
		go p.pump()
	})
}

func (p *Player) kick() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Player) shouldPoll() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.err != nil {
		return false
	}
	if p.playing || p.seekPending {
		return true
	}
	return p.needFrame && time.Since(p.needFrameAt) < time.Second
}

func (p *Player) pump() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(p.done)
	var ticker *time.Ticker
	var tickC <-chan time.Time
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()

	updateTicker := func() {
		if p.shouldPoll() {
			if ticker == nil {
				ticker = time.NewTicker(16 * time.Millisecond)
				tickC = ticker.C
			}
		} else if ticker != nil {
			ticker.Stop()
			ticker = nil
			tickC = nil
		}
	}

	p.tick()
	updateTicker()

	for {
		select {
		case <-p.stop:
			return
		case <-p.wake:
			p.tick()
			updateTicker()
		case <-tickC:
			p.tick()
			updateTicker()
		}
	}
}

func (p *Player) updateGLTexture(tex uint32) ([16]float32, bool) {
	var mat [16]float32
	p.jniMu.Lock()
	defer p.jniMu.Unlock()
	if p.m == nil {
		return mat, false
	}
	if C.shirei_android_media_update_tex(p.m, C.uint32_t(tex), (*C.float)(unsafe.Pointer(&mat[0]))) != 0 {
		return mat, false
	}
	return mat, true
}

func (p *Player) tick() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	playing := p.playing
	needFrame := p.needFrame
	p.mu.Unlock()

	var st C.ShireiAndroidState
	p.jniMu.Lock()
	if p.m == nil {
		p.jniMu.Unlock()
		return
	}
	C.shirei_android_media_poll(p.m, &st)
	p.jniMu.Unlock()

	w, h := int(st.width), int(st.height)
	pos := float64(st.pos_ms) / 1000.0
	dur := float64(st.dur_ms) / 1000.0
	if pos < 0 {
		pos = 0
	}
	if dur < 0 {
		dur = 0
	}

	if (playing || needFrame) && w > 0 && h > 0 && shirei.SupportsExternalKind(shirei.ExternalAndroidSurfaceTexture) {
		frame := shirei.ExternalFrame{
			Kind:   shirei.ExternalAndroidSurfaceTexture,
			Width:  w,
			Height: h,
			Data: &shirei.ExternalSurfaceTextureFrame{
				Update: p.updateGLTexture,
			},
		}
		id := shirei.UseExternalImage(p.key, frame)
		if id != 0 {
			p.mu.Lock()
			if !p.closed {
				p.id = id
				p.w = w
				p.h = h
				if !p.seekPending {
					p.needFrame = false
				}
			}
			p.mu.Unlock()
			shirei.RequestNextFrame()
		}
	}

	p.mu.Lock()
	if w > 0 && h > 0 {
		p.w = w
		p.h = h
	}
	if dur > 0 {
		p.duration = seconds(dur)
	}
	if st.is_playing != 0 {
		p.playedSinceStart = true
	}
	if p.seekPending {
		if math.Abs(pos-p.seekTarget) < 0.25 || time.Since(p.seekAt) > 500*time.Millisecond {
			p.seekPending = false
			p.position = seconds(pos)
		}
	} else {
		p.position = seconds(pos)
	}
	started := p.playedSinceStart || time.Since(p.playStartedAt) > 250*time.Millisecond
	if p.playing && !p.seekPending && !p.loop && started && dur > 0 && st.is_playing == 0 && pos >= dur-0.25 {
		p.ended = true
		p.playing = false
		p.mu.Unlock()
		p.jniMu.Lock()
		if p.m != nil {
			C.shirei_android_media_abandon_focus(p.m)
		}
		p.jniMu.Unlock()
		shirei.RequestNextFrame()
		return
	}
	p.mu.Unlock()
}
