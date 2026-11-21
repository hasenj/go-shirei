//go:build darwin && !ios

package media

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"structs"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"

	"go.hasen.dev/shirei"
)

// cmTime is CoreMedia's rational time value.
type cmTime struct {
	_         structs.HostLayout
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

const (
	avPlayerItemStatusReadyToPlay = 1
	avPlayerItemStatusFailed      = 2

	avPlayerTimeControlStatusPaused = 0

	cvPixelBufferLockReadOnly = 1
)

// Player owns one AVPlayer and one video output. Create it with Open; call View
// from the frame function to place the video. Methods are safe from the frame
// goroutine; View and the control methods are expected there.
type Player struct {
	stream *streamPlayer

	avMu   sync.Mutex
	item   objc.ID
	player objc.ID
	output objc.ID
	key    string

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

	// Double-buffered RGBA images for software-renderer / headless fallback.
	rgbaBufs [2]*image.RGBA
	rgbaNext int
}

// Open opens path (local file path or http:// / https:// / file:// URL) for
// playback. Call it before Run. Decoding starts when the player is first
// viewed or played from the frame function.
func Open(path string) (*Player, error) {
	isURL := strings.HasPrefix(path, "http://") ||
		strings.HasPrefix(path, "https://") ||
		strings.HasPrefix(path, "file://")

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
			if probe, ok := sniffFile(abs); ok && shouldUsePermissiveDarwin(probe) && canDecodePermissive(probe) {
				if eng, perr := openPermissiveEngine(abs); perr == nil {
					return &Player{stream: newStreamPlayer(eng)}, nil
				}
			}
		}
	}

	if err := bind(); err != nil {
		return nil, err
	}

	var item, output, player objc.ID
	var openErr error
	withAutoreleasePool(func() {
		var url objc.ID
		if isURL {
			url = objc.Send[objc.ID](cls("NSURL"), sel("URLWithString:"), nsstring(target))
		} else {
			url = objc.Send[objc.ID](cls("NSURL"), sel("fileURLWithPath:"), nsstring(target))
		}
		if url == 0 {
			openErr = fmt.Errorf("media: invalid URL %q", target)
			return
		}
		item = objc.Send[objc.ID](cls("AVPlayerItem"), sel("playerItemWithURL:"), url)
		if item == 0 {
			openErr = fmt.Errorf("media: could not create AVPlayerItem")
			return
		}
		item = objc.Send[objc.ID](item, sel("retain"))

		attrs := objc.Send[objc.ID](cls("NSMutableDictionary"), sel("dictionary"))
		fmtNum := objc.Send[objc.ID](cls("NSNumber"), sel("numberWithUnsignedInt:"), uint32(0x42475241)) // BGRA
		attrs.Send(sel("setObject:forKey:"), fmtNum, kCVPixelBufferPixelFormatType)
		iosurfaceProps := objc.Send[objc.ID](cls("NSDictionary"), sel("dictionary"))
		attrs.Send(sel("setObject:forKey:"), iosurfaceProps, kCVPixelBufferIOSurfaceProps)

		output = objc.Send[objc.ID](cls("AVPlayerItemVideoOutput"), sel("alloc"))
		output = objc.Send[objc.ID](output, sel("initWithPixelBufferAttributes:"), attrs)
		if output == 0 {
			item.Send(sel("release"))
			openErr = fmt.Errorf("media: could not create AVPlayerItemVideoOutput")
			return
		}
		item.Send(sel("addOutput:"), output)

		player = objc.Send[objc.ID](cls("AVPlayer"), sel("playerWithPlayerItem:"), item)
		if player == 0 {
			output.Send(sel("release"))
			item.Send(sel("release"))
			openErr = fmt.Errorf("media: could not create AVPlayer")
			return
		}
		player = objc.Send[objc.ID](player, sel("retain"))
	})
	if openErr != nil {
		return nil, openErr
	}

	p := &Player{
		item:      item,
		player:    player,
		output:    output,
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
	if p.player != 0 {
		withAutoreleasePool(func() {
			p.player.Send(sel("play"))
		})
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
	if p.player != 0 {
		withAutoreleasePool(func() {
			p.player.Send(sel("pause"))
		})
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

	p.avMu.Lock()
	if p.player != 0 {
		withAutoreleasePool(func() {
			p.player.Send(sel("setMuted:"), muted)
		})
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
	if p.player != 0 {
		withAutoreleasePool(func() {
			p.player.Send(sel("setVolume:"), volume)
		})
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
func (p *Player) Seek(seconds float64) {
	if p.stream != nil {
		p.stream.Seek(seconds)
		return
	}
	p.start()
	p.seekTo(seconds)
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

	zero := cmTimeMakeWithSeconds(0, 600)
	target := cmTimeMakeWithSeconds(sec, 600)
	p.avMu.Lock()
	if p.player != 0 {
		withAutoreleasePool(func() {
			p.player.Send(sel("seekToTime:toleranceBefore:toleranceAfter:"), target, zero, zero)
		})
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
// the first frame arrives. Call it from the frame function when placing the
// video with custom layout or styling.
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
// It draws a placeholder while the first frame is still decoding. Call it from
// the frame function; controls and popups drawn after it stay above the video.
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

// Close pauses playback and releases the player. Call it outside the frame
// function (for example from an exit cleanup handler).
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
		if p.player != 0 {
			withAutoreleasePool(func() {
				p.player.Send(sel("pause"))
				p.item.Send(sel("removeOutput:"), p.output)
				p.output.Send(sel("release"))
				p.player.Send(sel("release"))
				p.item.Send(sel("release"))
				p.output = 0
				p.player = 0
				p.item = 0
			})
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

	var (
		status      int64
		pb          uintptr
		pos, dur    float64
		rate        float32
		timeControl int64
		failErr     error
	)
	p.avMu.Lock()
	if p.player == 0 || p.item == 0 || p.output == 0 {
		p.avMu.Unlock()
		return
	}
	withAutoreleasePool(func() {
		status = objc.Send[int64](p.item, sel("status"))
		if status == avPlayerItemStatusFailed {
			failErr = itemError(p.item)
			return
		}

		// While playing, ask for the frame that should be on screen at the current
		// host time. While paused, ask for the current item time so a seek or the
		// initial poster frame still arrives.
		var itemTime cmTime
		if playing {
			itemTime = objc.Send[cmTime](p.output, sel("itemTimeForHostTime:"), caCurrentMediaTime())
		} else {
			itemTime = objc.Send[cmTime](p.player, sel("currentTime"))
		}

		if objc.Send[bool](p.output, sel("hasNewPixelBufferForItemTime:"), itemTime) {
			pb = objc.Send[uintptr](p.output, sel("copyPixelBufferForItemTime:itemTimeForDisplay:"), itemTime, uintptr(0))
		}

		pos = cmTimeGetSeconds(objc.Send[cmTime](p.player, sel("currentTime")))
		dur = cmTimeGetSeconds(objc.Send[cmTime](p.item, sel("duration")))
		rate = objc.Send[float32](p.player, sel("rate"))
		timeControl = objc.Send[int64](p.player, sel("timeControlStatus"))
	})
	p.avMu.Unlock()

	if failErr != nil {
		p.setError(failErr)
		return
	}

	published := false
	if pb != 0 {
		published = p.publish(pb)
		cfRelease(pb)
	}

	if math.IsNaN(pos) || pos < 0 {
		pos = 0
	}
	if math.IsNaN(dur) || dur < 0 {
		dur = 0
	}

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

	// AVPlayer pauses itself (rate=0, timeControlStatus=Paused) when an item
	// plays to end. Requiring both paused status and near-end position avoids
	// false EOF while starting playback or seeking near the end.
	finished := p.playing && !p.seekPending && status == avPlayerItemStatusReadyToPlay &&
		dur > 0 && rate == 0 && timeControl == avPlayerTimeControlStatusPaused && pos >= dur-0.25
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

func (p *Player) publish(pb uintptr) bool {
	w := int(cvPixelBufferGetWidth(pb))
	h := int(cvPixelBufferGetHeight(pb))
	if w <= 0 || h <= 0 {
		return false
	}

	var id shirei.ImageId
	if shirei.SupportsExternalKind(shirei.ExternalIOSurfaceBGRA) {
		ios := cvPixelBufferGetIOSurface(pb)
		if ios != 0 {
			cfRetain(ios)
			frame := shirei.ExternalFrame{
				Kind:   shirei.ExternalIOSurfaceBGRA,
				Handle: ios,
				Width:  w,
				Height: h,
				Release: func() {
					cfRelease(ios)
				},
			}
			shirei.WithFrameLock(func() {
				id = shirei.UseExternalImage(p.key, frame)
			})
			if id == 0 {
				frame.Release()
			}
		}
	}
	if id == 0 {
		rgba := p.copyPixelBufferRGBA(pb, w, h)
		if rgba == nil {
			return false
		}
		shirei.WithFrameLock(func() {
			id = shirei.UseImage(p.key, rgba)
		})
	}
	if id == 0 {
		return false
	}

	p.mu.Lock()
	p.id = id
	p.w = w
	p.h = h
	p.mu.Unlock()
	shirei.RequestNextFrame()
	return true
}

func (p *Player) copyPixelBufferRGBA(pb uintptr, w, h int) *image.RGBA {
	if cvPixelBufferLockBaseAddress(pb, cvPixelBufferLockReadOnly) != 0 {
		return nil
	}
	defer cvPixelBufferUnlockBaseAddress(pb, cvPixelBufferLockReadOnly)

	base := cvPixelBufferGetBaseAddress(pb)
	bpr := int(cvPixelBufferGetBytesPerRow(pb))
	if base == 0 || bpr < w*4 {
		return nil
	}

	slot := p.rgbaNext & 1
	p.rgbaNext++
	rgba := p.rgbaBufs[slot]
	if rgba == nil || rgba.Rect.Dx() != w || rgba.Rect.Dy() != h {
		rgba = image.NewRGBA(image.Rect(0, 0, w, h))
		p.rgbaBufs[slot] = rgba
	}

	src := unsafe.Slice((*byte)(unsafe.Pointer(base)), h*bpr)
	dst := rgba.Pix
	rowBytes := w * 4
	for y := 0; y < h; y++ {
		sRow := src[y*bpr : y*bpr+rowBytes]
		dRow := dst[y*rowBytes : y*rowBytes+rowBytes]
		for i := 0; i < rowBytes; i += 4 {
			dRow[i+0] = sRow[i+2]
			dRow[i+1] = sRow[i+1]
			dRow[i+2] = sRow[i+0]
			dRow[i+3] = sRow[i+3]
		}
	}
	return rgba
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

func itemError(item objc.ID) error {
	e := objc.Send[objc.ID](item, sel("error"))
	if e == 0 {
		return fmt.Errorf("media: playback failed")
	}
	desc := objc.Send[objc.ID](e, sel("localizedDescription"))
	if desc == 0 {
		return fmt.Errorf("media: playback failed")
	}
	return fmt.Errorf("media: %s", cstrings.NSStringToString(desc))
}

func shouldUsePermissiveDarwin(p mediaProbe) bool {
	if p.Container == containerWebM {
		return true
	}
	switch p.VideoCodec {
	case videoCodecVP8, videoCodecVP9:
		return true
	case videoCodecAV1:
		// AVFoundation handles AV1 only on recent Apple Silicon/macOS and does
		// not reliably pair it with Opus; route AV1 to dav1d when available.
		return true
	}
	switch p.AudioCodec {
	case audioCodecOpus, audioCodecVorbis:
		return true
	}
	return false
}

// -----------------------------------------------------------------------------
// AVFoundation / CoreVideo / CoreMedia bindings

var (
	bindOnce sync.Once
	bindErr  error

	caCurrentMediaTime             func() float64
	cmTimeGetSeconds               func(cmTime) float64
	cmTimeMakeWithSeconds          func(float64, int32) cmTime
	cvPixelBufferGetWidth          func(uintptr) uintptr
	cvPixelBufferGetHeight         func(uintptr) uintptr
	cvPixelBufferGetIOSurface      func(uintptr) uintptr
	cvPixelBufferLockBaseAddress   func(uintptr, uint64) int32
	cvPixelBufferUnlockBaseAddress func(uintptr, uint64) int32
	cvPixelBufferGetBaseAddress    func(uintptr) uintptr
	cvPixelBufferGetBytesPerRow    func(uintptr) uintptr
	cfRetain                       func(uintptr) uintptr
	cfRelease                      func(uintptr)

	kCVPixelBufferPixelFormatType objc.ID
	kCVPixelBufferIOSurfaceProps  objc.ID

	objcMu    sync.RWMutex
	selectors = map[string]objc.SEL{}
	classes   = map[string]objc.ID{}
)

func sel(name string) objc.SEL {
	objcMu.RLock()
	s, ok := selectors[name]
	objcMu.RUnlock()
	if ok {
		return s
	}
	s = objc.RegisterName(name)
	objcMu.Lock()
	selectors[name] = s
	objcMu.Unlock()
	return s
}

func cls(name string) objc.ID {
	objcMu.RLock()
	c, ok := classes[name]
	objcMu.RUnlock()
	if ok {
		return c
	}
	c = objc.ID(objc.GetClass(name))
	objcMu.Lock()
	classes[name] = c
	objcMu.Unlock()
	return c
}

func nsstring(s string) objc.ID {
	return objc.Send[objc.ID](cls("NSString"), sel("stringWithUTF8String:"), s)
}

func withAutoreleasePool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.Send[objc.ID](cls("NSAutoreleasePool"), sel("new"))
	defer objc.Send[objc.ID](pool, sel("drain"))
	fn()
}

func ptr[T any](p uintptr) *T { return *(**T)(unsafe.Pointer(&p)) }

func loadConst(lib uintptr, name string) objc.ID {
	addr, err := purego.Dlsym(lib, name)
	if err != nil || addr == 0 {
		return 0
	}
	return *ptr[objc.ID](addr)
}

func bind() error {
	bindOnce.Do(func() {
		for _, path := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AVFoundation.framework/AVFoundation",
			"/System/Library/Frameworks/CoreMedia.framework/CoreMedia",
			"/System/Library/Frameworks/CoreVideo.framework/CoreVideo",
			"/System/Library/Frameworks/QuartzCore.framework/QuartzCore",
		} {
			if _, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
				bindErr = err
				return
			}
		}
		coreMedia, err := purego.Dlopen("/System/Library/Frameworks/CoreMedia.framework/CoreMedia", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bindErr = err
			return
		}
		coreVideo, err := purego.Dlopen("/System/Library/Frameworks/CoreVideo.framework/CoreVideo", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bindErr = err
			return
		}
		quartz, err := purego.Dlopen("/System/Library/Frameworks/QuartzCore.framework/QuartzCore", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bindErr = err
			return
		}
		purego.RegisterLibFunc(&caCurrentMediaTime, quartz, "CACurrentMediaTime")
		purego.RegisterLibFunc(&cmTimeGetSeconds, coreMedia, "CMTimeGetSeconds")
		purego.RegisterLibFunc(&cmTimeMakeWithSeconds, coreMedia, "CMTimeMakeWithSeconds")
		purego.RegisterLibFunc(&cvPixelBufferGetWidth, coreVideo, "CVPixelBufferGetWidth")
		purego.RegisterLibFunc(&cvPixelBufferGetHeight, coreVideo, "CVPixelBufferGetHeight")
		purego.RegisterLibFunc(&cvPixelBufferGetIOSurface, coreVideo, "CVPixelBufferGetIOSurface")
		purego.RegisterLibFunc(&cvPixelBufferLockBaseAddress, coreVideo, "CVPixelBufferLockBaseAddress")
		purego.RegisterLibFunc(&cvPixelBufferUnlockBaseAddress, coreVideo, "CVPixelBufferUnlockBaseAddress")
		purego.RegisterLibFunc(&cvPixelBufferGetBaseAddress, coreVideo, "CVPixelBufferGetBaseAddress")
		purego.RegisterLibFunc(&cvPixelBufferGetBytesPerRow, coreVideo, "CVPixelBufferGetBytesPerRow")
		purego.RegisterLibFunc(&cfRetain, purego.RTLD_DEFAULT, "CFRetain")
		purego.RegisterLibFunc(&cfRelease, purego.RTLD_DEFAULT, "CFRelease")
		kCVPixelBufferPixelFormatType = loadConst(coreVideo, "kCVPixelBufferPixelFormatTypeKey")
		kCVPixelBufferIOSurfaceProps = loadConst(coreVideo, "kCVPixelBufferIOSurfacePropertiesKey")
		if kCVPixelBufferPixelFormatType == 0 || kCVPixelBufferIOSurfaceProps == 0 {
			bindErr = fmt.Errorf("media: CoreVideo constants unavailable")
		}
	})
	return bindErr
}
