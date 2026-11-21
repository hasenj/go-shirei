//go:build ios

package media

/*
#cgo LDFLAGS: -framework AVFoundation -framework CoreMedia -framework CoreVideo -framework Foundation -framework QuartzCore -framework IOSurface
#include "media_ios.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"go.hasen.dev/shirei"
)

const (
	iosItemStatusReady  = 1
	iosItemStatusFailed = 2
	iosTimePaused       = 0
)

// Player owns one AVPlayer (or a permissive stream fallback) on iOS. Create it
// with Open; call View from the frame function to place the video.
type Player struct {
	stream *streamPlayer

	avMu sync.Mutex
	m    *C.ShireiIOSMedia
	key  string

	stop      chan struct{}
	done      chan struct{}
	wake      chan struct{}
	startOnce sync.Once
	started   atomic.Bool
	closeOnce sync.Once

	mu          sync.Mutex
	id          shirei.ImageId
	w, h        int
	playing     bool
	ended       bool
	loop        bool
	muted       bool
	volume      float32
	closed      bool
	needFrame   bool
	seekPending bool
	seekTarget  float64
	seekAt      time.Time
	err         error
	position    time.Duration
	duration    time.Duration

	rgbaBufs [2]*image.RGBA
	rgbaNext int
}

// Open opens path for playback. Call it before Run. Decoding starts when the
// player is first viewed or played from the frame function.
func Open(path string) (*Player, error) {
	isURL := strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "file://")
	target := path
	if !isURL {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, err
		}
		target = abs

		mode := strings.ToLower(strings.TrimSpace(os.Getenv("SHIREI_MEDIA")))
		if mode == "permissive" {
			eng, err := openPermissiveEngine(abs)
			if err != nil {
				return nil, err
			}
			return &Player{stream: newStreamPlayer(eng)}, nil
		}
		if mode != "avfoundation" {
			if probe, ok := sniffFile(abs); ok && shouldUsePermissiveIOS(probe) && canDecodePermissive(probe) {
				if eng, perr := openPermissiveEngine(abs); perr == nil {
					return &Player{stream: newStreamPlayer(eng)}, nil
				}
			}
		}
	}

	cpath := C.CString(target)
	defer C.free(unsafe.Pointer(cpath))
	var errBuf [256]C.char
	m := C.shirei_ios_media_open(cpath, &errBuf[0], C.size_t(len(errBuf)))
	if m == nil {
		return nil, fmt.Errorf("media: %s", C.GoString(&errBuf[0]))
	}
	p := &Player{
		m:         m,
		volume:    1,
		needFrame: true,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		wake:      make(chan struct{}, 1),
	}
	p.key = fmt.Sprintf("media/%d", nextPlayerID.Add(1))
	return p, nil
}

// Play starts or resumes playback.
func (p *Player) Play() {
	if p.stream != nil {
		p.stream.Play()
		return
	}
	p.start()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	ended := p.ended
	p.playing = true
	p.ended = false
	p.needFrame = true
	p.mu.Unlock()

	if ended {
		p.seekTo(0)
	}
	p.avMu.Lock()
	if p.m != nil {
		C.shirei_ios_media_play(p.m)
	}
	p.avMu.Unlock()
	p.kick()
	shirei.RequestNextFrame()
}

// Pause pauses playback without releasing the current frame.
func (p *Player) Pause() {
	if p.stream != nil {
		p.stream.Pause()
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.playing = false
	p.mu.Unlock()

	p.avMu.Lock()
	if p.m != nil {
		C.shirei_ios_media_pause(p.m)
	}
	p.avMu.Unlock()
	shirei.RequestNextFrame()
}

// SetMuted mutes or unmutes playback.
func (p *Player) SetMuted(muted bool) {
	if p.stream != nil {
		p.stream.SetMuted(muted)
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.muted = muted
	p.mu.Unlock()

	v := C.int(0)
	if muted {
		v = 1
	}
	p.avMu.Lock()
	if p.m != nil {
		C.shirei_ios_media_set_muted(p.m, v)
	}
	p.avMu.Unlock()
	shirei.RequestNextFrame()
}

// Muted reports whether playback is muted.
func (p *Player) Muted() bool {
	if p.stream != nil {
		return p.stream.Muted()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.muted
}

// SetVolume sets the player volume in [0, 1].
func (p *Player) SetVolume(volume float32) {
	if p.stream != nil {
		p.stream.SetVolume(volume)
		return
	}
	if volume < 0 {
		volume = 0
	} else if volume > 1 {
		volume = 1
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.volume = volume
	p.mu.Unlock()

	p.avMu.Lock()
	if p.m != nil {
		C.shirei_ios_media_set_volume(p.m, C.float(volume))
	}
	p.avMu.Unlock()
	shirei.RequestNextFrame()
}

// Volume returns the player volume in [0, 1].
func (p *Player) Volume() float32 {
	if p.stream != nil {
		return p.stream.Volume()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// Seek jumps to seconds from the start. It works while playing or paused.
func (p *Player) Seek(sec float64) {
	if p.stream != nil {
		p.stream.Seek(sec)
		return
	}
	p.start()
	p.seekTo(sec)
}

func (p *Player) seekTo(sec float64) {
	if sec < 0 {
		sec = 0
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if p.duration > 0 {
		max := p.duration.Seconds()
		if sec > max {
			sec = max
		}
	}
	p.position = seconds(sec)
	p.ended = false
	p.needFrame = true
	p.seekPending = true
	p.seekTarget = sec
	p.seekAt = time.Now()
	p.mu.Unlock()

	p.avMu.Lock()
	if p.m != nil {
		C.shirei_ios_media_seek(p.m, C.double(sec))
	}
	p.avMu.Unlock()
	p.kick()
	shirei.RequestNextFrame()
}

// SetLoop controls whether reaching the end restarts from the beginning.
func (p *Player) SetLoop(loop bool) {
	if p.stream != nil {
		p.stream.SetLoop(loop)
		return
	}
	p.mu.Lock()
	p.loop = loop
	p.mu.Unlock()
}

// Playing reports whether playback is intended to be running.
func (p *Player) Playing() bool {
	if p.stream != nil {
		return p.stream.Playing()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// Ended reports whether playback reached the end and stopped.
func (p *Player) Ended() bool {
	if p.stream != nil {
		return p.stream.Ended()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ended
}

// Position returns the last sampled playback position.
func (p *Player) Position() time.Duration {
	if p.stream != nil {
		return p.stream.Position()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.position
}

// Duration returns the media duration, or zero until it is known.
func (p *Player) Duration() time.Duration {
	if p.stream != nil {
		return p.stream.Duration()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.duration
}

// Size returns the current video pixel dimensions, or zero before the first
// frame arrives.
func (p *Player) Size() (int, int) {
	if p.stream != nil {
		return p.stream.Size()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.w, p.h
}

// ImageId returns the current video frame's shirei image handle, or 0 before
// the first frame arrives.
func (p *Player) ImageId() shirei.ImageId {
	if p.stream != nil {
		return p.stream.ImageId()
	}
	p.start()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.id
}

// Err returns the decoder error, if any.
func (p *Player) Err() error {
	if p.stream != nil {
		return p.stream.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// View places the current video frame inside maxSize, preserving aspect ratio.
func (p *Player) View(maxSize shirei.Vec2) {
	if p.stream != nil {
		p.stream.View(maxSize)
		return
	}
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

// Close pauses playback and releases the player.
func (p *Player) Close() {
	if p.stream != nil {
		p.stream.Close()
		return
	}
	p.closeOnce.Do(func() {
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

		p.avMu.Lock()
		if p.m != nil {
			C.shirei_ios_media_close(p.m)
			p.m = nil
		}
		p.avMu.Unlock()

		shirei.WithFrameLock(func() {
			shirei.ReleaseImage(p.key)
		})
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
	return p.playing || p.needFrame || p.seekPending || p.duration == 0
}

func (p *Player) pump() {
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

func (p *Player) tick() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	playing := p.playing
	p.mu.Unlock()

	wantIOSurface := C.int(0)
	if shirei.SupportsExternalKind(shirei.ExternalIOSurfaceBGRA) {
		wantIOSurface = 1
	}
	playFlag := C.int(0)
	if playing {
		playFlag = 1
	}

	var res C.ShireiIOSTickResult
	var errBuf [256]C.char
	p.avMu.Lock()
	if p.m == nil {
		p.avMu.Unlock()
		return
	}
	C.shirei_ios_media_tick(p.m, playFlag, wantIOSurface, &res, &errBuf[0], C.size_t(len(errBuf)))
	p.avMu.Unlock()

	if res.item_status == iosItemStatusFailed {
		if res.iosurface != nil {
			C.shirei_ios_release_cf(res.iosurface)
		}
		if res.pixel_buffer != nil {
			C.shirei_ios_release_cf(res.pixel_buffer)
		}
		p.setError(fmt.Errorf("media: %s", C.GoString(&errBuf[0])))
		return
	}

	published := false
	w, h := int(res.width), int(res.height)
	if res.iosurface != nil && w > 0 && h > 0 {
		iosPtr := res.iosurface
		frame := shirei.ExternalFrame{
			Kind:   shirei.ExternalIOSurfaceBGRA,
			Handle: uintptr(iosPtr),
			Width:  w,
			Height: h,
			Release: func() {
				C.shirei_ios_release_cf(iosPtr)
			},
		}
		var id shirei.ImageId
		shirei.WithFrameLock(func() {
			id = shirei.UseExternalImage(p.key, frame)
		})
		if id == 0 {
			frame.Release()
		} else {
			p.mu.Lock()
			p.id = id
			p.w = w
			p.h = h
			p.mu.Unlock()
			shirei.RequestNextFrame()
			published = true
		}
	} else if res.pixel_buffer != nil {
		pb := res.pixel_buffer
		if w > 0 && h > 0 {
			slot := p.rgbaNext & 1
			p.rgbaNext++
			rgba := p.rgbaBufs[slot]
			if rgba == nil || rgba.Rect.Dx() != w || rgba.Rect.Dy() != h {
				rgba = image.NewRGBA(image.Rect(0, 0, w, h))
				p.rgbaBufs[slot] = rgba
			}
			if C.shirei_ios_copy_pixel_buffer_rgba(pb, (*C.uchar)(unsafe.Pointer(&rgba.Pix[0])), C.int(rgba.Stride), C.int(w), C.int(h)) == 0 {
				var id shirei.ImageId
				shirei.WithFrameLock(func() {
					id = shirei.UseImage(p.key, rgba)
				})
				if id != 0 {
					p.mu.Lock()
					p.id = id
					p.w = w
					p.h = h
					p.mu.Unlock()
					shirei.RequestNextFrame()
					published = true
				}
			}
		}
		C.shirei_ios_release_cf(pb)
	}

	if res.system_paused != 0 {
		p.mu.Lock()
		wasPlaying := p.playing
		p.playing = false
		p.mu.Unlock()
		if wasPlaying {
			shirei.RequestNextFrame()
		}
	}

	pos := float64(res.pos)
	dur := float64(res.dur)

	p.mu.Lock()
	if dur > 0 {
		p.duration = seconds(dur)
	}
	if p.seekPending {
		if math.Abs(pos-p.seekTarget) < 0.25 || time.Since(p.seekAt) > 500*time.Millisecond {
			p.seekPending = false
			p.position = seconds(pos)
		}
	} else {
		p.position = seconds(pos)
	}
	if published && !p.seekPending {
		p.needFrame = false
	}

	finished := p.playing && !p.seekPending && res.item_status == iosItemStatusReady &&
		dur > 0 && res.rate == 0 && res.time_control_status == iosTimePaused && pos >= dur-0.25
	loop := p.loop
	if finished {
		p.ended = true
		p.playing = false
		p.mu.Unlock()
		if loop {
			p.Play()
		} else {
			shirei.RequestNextFrame()
		}
		return
	}
	p.mu.Unlock()
}

func (p *Player) setError(err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return
	}
	p.err = err
	p.playing = false
	p.needFrame = false
	p.mu.Unlock()
	shirei.RequestNextFrame()
}

func shouldUsePermissiveIOS(p mediaProbe) bool {
	if p.Container == containerWebM {
		return true
	}
	switch p.VideoCodec {
	case videoCodecVP8, videoCodecVP9, videoCodecAV1:
		return true
	}
	switch p.AudioCodec {
	case audioCodecOpus, audioCodecVorbis:
		return true
	}
	return false
}
