//go:build darwin || linux || windows || android

package media

import (
	"context"
	"fmt"
	"image"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
)

const (
	maxVideoWidth = 1920
	audioRate     = 44100
	audioChannels = 2
)

type probeInfo struct {
	width, height int
	fps           float64
	duration      float64
	hasAudio      bool
	fullRange     bool
}

type streamEngine interface {
	info() probeInfo
	run(p *streamPlayer)
	close()
}

// libavOpen is registered by media_libav.go when built with -tags libav.
var libavOpen func(path string) (streamEngine, error)

type nv12Buf struct {
	y, uv []byte
	w, h  int
}

// streamPlayer implements Player for push-based decode engines (Linux ffmpeg
// subprocesses, and in-process libav* on Linux/Windows).
type streamPlayer struct {
	engine   streamEngine
	outW     int
	outH     int
	fps      float64
	hasAudio bool

	key string

	stop      chan struct{}
	done      chan struct{}
	wake      chan struct{}
	playWake  chan struct{}
	startOnce sync.Once
	started   atomic.Bool
	closeOnce sync.Once
	audioOnce sync.Once

	mu          sync.Mutex
	id          shirei.ImageId
	w, h        int
	playing     bool
	ended       bool
	looping     bool
	muted       bool
	volume      float32
	closed      bool
	err         error
	duration    time.Duration
	base        float64 // media seconds when played frames were zero
	wall        time.Time
	audioOn     bool
	seekTo      float64
	seekPending bool

	playingA   atomic.Bool
	mutedA     atomic.Bool
	volumeBits atomic.Uint32
	played     atomic.Int64 // stereo audio frames consumed by the device since base
	audio      *pcmRing
	mixScratch []float32 // audio-thread-only scratch when mixing multiple players

	// Double-buffered CPU frames reused across publish calls.
	rgbaBufs [2]*image.RGBA
	rgbaNext int
	nv12Bufs [2]nv12Buf
	nv12Next int
}

func newStreamPlayer(eng streamEngine) *streamPlayer {
	info := eng.info()
	w, h := outputSize(info.width, info.height)
	p := &streamPlayer{
		engine:   eng,
		outW:     w,
		outH:     h,
		w:        w,
		h:        h,
		fps:      info.fps,
		hasAudio: info.hasAudio,
		volume:   1,
		duration: seconds(info.duration),
		audio:    newPCMRing(audioRate * audioChannels),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		wake:     make(chan struct{}, 1),
		playWake: make(chan struct{}, 1),
	}
	p.volumeBits.Store(math.Float32bits(1))
	p.key = fmt.Sprintf("media/%d", nextPlayerID.Add(1))
	return p
}

func (p *streamPlayer) Play() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	ended := p.ended
	p.playing = true
	p.ended = false
	if !p.audioOn {
		p.wall = time.Now()
	}
	p.mu.Unlock()
	p.playingA.Store(true)
	p.start()
	if ended {
		p.Seek(0)
	}
	p.wakePlay()
	shirei.RequestNextFrame()
}

func (p *streamPlayer) Pause() {
	pos := p.positionSeconds()
	p.audio.mu.Lock()
	p.played.Store(0)
	p.audio.mu.Unlock()
	p.mu.Lock()
	p.base = pos
	p.playing = false
	p.wall = time.Time{}
	p.mu.Unlock()
	p.playingA.Store(false)
	shirei.RequestNextFrame()
}

func (p *streamPlayer) SetMuted(muted bool) {
	p.mu.Lock()
	p.muted = muted
	p.mu.Unlock()
	p.mutedA.Store(muted)
	shirei.RequestNextFrame()
}

func (p *streamPlayer) Muted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.muted
}

func (p *streamPlayer) SetVolume(volume float32) {
	if volume < 0 {
		volume = 0
	} else if volume > 1 {
		volume = 1
	}
	p.mu.Lock()
	p.volume = volume
	p.mu.Unlock()
	p.volumeBits.Store(math.Float32bits(volume))
	shirei.RequestNextFrame()
}

func (p *streamPlayer) Volume() float32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

func (p *streamPlayer) Seek(sec float64) {
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
	p.seekTo = sec
	p.seekPending = true
	p.ended = false
	p.mu.Unlock()
	p.start()
	p.kick()
	p.wakePlay()
	shirei.RequestNextFrame()
}

func (p *streamPlayer) SetLoop(loop bool) {
	p.mu.Lock()
	p.looping = loop
	p.mu.Unlock()
}

func (p *streamPlayer) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

func (p *streamPlayer) Ended() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ended
}

func (p *streamPlayer) Position() time.Duration {
	pos := p.positionSeconds()
	p.mu.Lock()
	dur := p.duration
	p.mu.Unlock()
	if dur > 0 && pos > dur.Seconds() {
		pos = dur.Seconds()
	}
	return seconds(pos)
}

func (p *streamPlayer) Duration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.duration
}

func (p *streamPlayer) Size() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.w, p.h
}

func (p *streamPlayer) ImageId() shirei.ImageId {
	p.start()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.id
}

func (p *streamPlayer) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *streamPlayer) View(maxSize shirei.Vec2) {
	p.start()

	p.mu.Lock()
	id, w, h := p.id, p.w, p.h
	p.mu.Unlock()

	drawVideoBox(id, w, h, maxSize)
}

func (p *streamPlayer) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.playing = false
		p.id = 0
		p.mu.Unlock()
		p.playingA.Store(false)
		unregisterAudioPlayer(p)
		close(p.stop)
		p.startOnce.Do(func() {})
		if p.started.Load() {
			<-p.done
		}
		shirei.WithFrameLock(func() {
			shirei.ReleaseImage(p.key)
		})
		if p.engine != nil {
			p.engine.close()
		}
	})
}

func drawVideoBox(id shirei.ImageId, w, h int, maxSize shirei.Vec2) {
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

func (p *streamPlayer) start() {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	p.prepareAudio()
	p.startOnce.Do(func() {
		p.started.Store(true)
		go func() {
			defer close(p.done)
			p.engine.run(p)
		}()
	})
}

func (p *streamPlayer) prepareAudio() {
	p.audioOnce.Do(func() {
		if !p.hasAudio {
			return
		}
		if err := ensureStreamAudio(); err != nil {
			return
		}
		registerAudioPlayer(p)
		p.mu.Lock()
		p.audioOn = true
		p.mu.Unlock()
	})
}

func (p *streamPlayer) kick() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *streamPlayer) wakePlay() {
	select {
	case p.playWake <- struct{}{}:
	default:
	}
}

func (p *streamPlayer) resetAudioClock(startAt float64) {
	p.audio.mu.Lock()
	p.audio.resetLocked()
	p.played.Store(0)
	p.audio.mu.Unlock()
	p.mu.Lock()
	if p.hasAudio && isAudioPlayerRegistered(p) {
		p.audioOn = true
	}
	p.base = startAt
	if p.playing && !p.audioOn {
		p.wall = time.Now()
	}
	p.mu.Unlock()
}

func (p *streamPlayer) takeSeek() (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.seekPending {
		return 0, false
	}
	p.seekPending = false
	p.base = p.seekTo
	p.ended = false
	if p.playing && !p.audioOn {
		p.wall = time.Now()
	}
	return p.base, true
}

func (p *streamPlayer) armLoop() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.err != nil {
		p.playing = false
		p.playingA.Store(false)
		return false
	}
	if p.looping && p.playing {
		p.ended = false
		p.playing = true
		p.base = 0
		p.playingA.Store(true)
		if !p.audioOn {
			p.wall = time.Now()
		}
		return true
	}
	p.ended = true
	p.playing = false
	p.playingA.Store(false)
	p.wall = time.Time{}
	shirei.RequestNextFrame()
	return false
}

func (p *streamPlayer) audioEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.audioOn
}

func (p *streamPlayer) waitUntil(ctx context.Context, pts float64) bool {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		if ctx.Err() != nil {
			return false
		}
		if !p.playingA.Load() {
			select {
			case <-ctx.Done():
				return false
			case <-p.playWake:
				continue
			}
		}
		rem := pts - p.positionSeconds()
		if rem <= 0.001 {
			return true
		}
		d := time.Duration(rem * float64(time.Second))
		if d < 2*time.Millisecond {
			d = 2 * time.Millisecond
		} else if d > 50*time.Millisecond {
			d = 50 * time.Millisecond
		}
		if timer == nil {
			timer = time.NewTimer(d)
		} else {
			timer.Reset(d)
		}
		select {
		case <-ctx.Done():
			return false
		case <-p.playWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (p *streamPlayer) publishRGBA(pix []byte, stride, w, h int) {
	if w <= 0 || h <= 0 || stride < w*4 || len(pix) < h*stride {
		return
	}
	slot := p.rgbaNext & 1
	p.rgbaNext++
	rgba := p.rgbaBufs[slot]
	if rgba == nil || rgba.Rect.Dx() != w || rgba.Rect.Dy() != h {
		rgba = image.NewRGBA(image.Rect(0, 0, w, h))
		p.rgbaBufs[slot] = rgba
	}
	rowBytes := w * 4
	if stride == rowBytes {
		copy(rgba.Pix, pix[:h*rowBytes])
	} else {
		for y := 0; y < h; y++ {
			copy(rgba.Pix[y*rowBytes:(y+1)*rowBytes], pix[y*stride:y*stride+rowBytes])
		}
	}
	var id shirei.ImageId
	shirei.WithFrameLock(func() {
		id = shirei.UseImage(p.key, rgba)
	})
	if id == 0 {
		return
	}
	p.mu.Lock()
	p.id = id
	p.w = w
	p.h = h
	p.mu.Unlock()
	shirei.RequestNextFrame()
}

func (p *streamPlayer) publishNV12(yPlane, uvPlane []byte, yStride, uvStride, w, h int, fullRange bool) {
	uvW, uvH := w/2, h/2
	if w <= 0 || h <= 0 || uvW <= 0 || uvH <= 0 || yStride < w || uvStride < uvW*2 {
		return
	}
	if len(yPlane) < h*yStride || len(uvPlane) < uvH*uvStride {
		return
	}
	slot := p.nv12Next & 1
	p.nv12Next++
	buf := &p.nv12Bufs[slot]
	ySize := w * h
	uvRowBytes := uvW * 2
	uvSize := uvH * uvRowBytes
	if buf.w != w || buf.h != h || len(buf.y) != ySize || len(buf.uv) != uvSize {
		buf.y = make([]byte, ySize)
		buf.uv = make([]byte, uvSize)
		buf.w = w
		buf.h = h
	}
	if yStride == w {
		copy(buf.y, yPlane[:ySize])
	} else {
		for row := 0; row < h; row++ {
			copy(buf.y[row*w:(row+1)*w], yPlane[row*yStride:row*yStride+w])
		}
	}
	if uvStride == uvRowBytes {
		copy(buf.uv, uvPlane[:uvSize])
	} else {
		for row := 0; row < uvH; row++ {
			copy(buf.uv[row*uvRowBytes:(row+1)*uvRowBytes], uvPlane[row*uvStride:row*uvStride+uvRowBytes])
		}
	}
	frame := shirei.ExternalFrame{
		Kind:   shirei.ExternalNV12,
		Width:  w,
		Height: h,
		Data: &shirei.ExternalNV12Frame{
			Y: buf.y, UV: buf.uv,
			YStride: w, UVStride: uvRowBytes,
			FullRange: fullRange,
		},
	}
	var id shirei.ImageId
	shirei.WithFrameLock(func() {
		id = shirei.UseExternalImage(p.key, frame)
	})
	if id == 0 {
		return
	}
	p.mu.Lock()
	p.id = id
	p.w = w
	p.h = h
	p.mu.Unlock()
	shirei.RequestNextFrame()
}

func (p *streamPlayer) publishDMABuf(db *shirei.ExternalDMABufFrame, w, h int, release func()) bool {
	if db == nil || w <= 0 || h <= 0 {
		if release != nil {
			release()
		}
		return false
	}
	frame := shirei.ExternalFrame{
		Kind:    shirei.ExternalDMABuf,
		Width:   w,
		Height:  h,
		Data:    db,
		Release: release,
	}
	var id shirei.ImageId
	shirei.WithFrameLock(func() {
		id = shirei.UseExternalImage(p.key, frame)
	})
	if id == 0 {
		if release != nil {
			release()
		}
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

// publishPlanarYUV publishes a planar Y/U/V frame (I420, I422, I444, or mono,
// 8/10/12-bit). When the GPU supports ExternalNV12 and the frame is even-sized
// 4:2:0, it packs NV12 for direct GPU sampling; otherwise it converts into the
// double-buffered RGBA image.
func (p *streamPlayer) publishPlanarYUV(
	yPlane, uPlane, vPlane []byte,
	yStride, uvStride int,
	w, h, xShift, yShift, bitDepth int,
	bt709, fullRange bool,
) {
	if w <= 0 || h <= 0 || len(yPlane) == 0 {
		return
	}
	if bitDepth <= 8 {
		bitDepth = 8
	}
	shift := uint(bitDepth - 8)
	hasChroma := len(uPlane) > 0 && len(vPlane) > 0 && uvStride > 0

	// Fast GPU NV12 path for 4:2:0 frames when the backend supports ExternalNV12.
	if hasChroma && xShift == 1 && yShift == 1 && w >= 2 && h >= 2 && w%2 == 0 && h%2 == 0 &&
		shirei.SupportsExternalKind(shirei.ExternalNV12) {
		uvW, uvH := w/2, h/2
		slot := p.nv12Next & 1
		p.nv12Next++
		buf := &p.nv12Bufs[slot]
		ySize := w * h
		uvRowBytes := uvW * 2
		uvSize := uvH * uvRowBytes
		if buf.w != w || buf.h != h || len(buf.y) != ySize || len(buf.uv) != uvSize {
			buf.y = make([]byte, ySize)
			buf.uv = make([]byte, uvSize)
			buf.w = w
			buf.h = h
		}
		if bitDepth == 8 {
			if yStride == w && len(yPlane) >= ySize {
				copy(buf.y, yPlane[:ySize])
			} else {
				for row := 0; row < h; row++ {
					srcOff := row * yStride
					if srcOff+w > len(yPlane) {
						break
					}
					copy(buf.y[row*w:(row+1)*w], yPlane[srcOff:srcOff+w])
				}
			}
			for row := 0; row < uvH; row++ {
				srcOff := row * uvStride
				if srcOff+uvW > len(uPlane) || srcOff+uvW > len(vPlane) {
					break
				}
				dst := buf.uv[row*uvRowBytes : (row+1)*uvRowBytes]
				uRow := uPlane[srcOff : srcOff+uvW]
				vRow := vPlane[srcOff : srcOff+uvW]
				for x := 0; x < uvW; x++ {
					dst[x*2] = uRow[x]
					dst[x*2+1] = vRow[x]
				}
			}
		} else {
			for row := 0; row < h; row++ {
				srcOff := row * yStride
				if srcOff+w*2 > len(yPlane) {
					break
				}
				dst := buf.y[row*w : (row+1)*w]
				for x := 0; x < w; x++ {
					v := uint16(yPlane[srcOff+x*2]) | uint16(yPlane[srcOff+x*2+1])<<8
					dst[x] = byte(v >> shift)
				}
			}
			for row := 0; row < uvH; row++ {
				srcOff := row * uvStride
				if srcOff+uvW*2 > len(uPlane) || srcOff+uvW*2 > len(vPlane) {
					break
				}
				dst := buf.uv[row*uvRowBytes : (row+1)*uvRowBytes]
				for x := 0; x < uvW; x++ {
					u := uint16(uPlane[srcOff+x*2]) | uint16(uPlane[srcOff+x*2+1])<<8
					v := uint16(vPlane[srcOff+x*2]) | uint16(vPlane[srcOff+x*2+1])<<8
					dst[x*2] = byte(u >> shift)
					dst[x*2+1] = byte(v >> shift)
				}
			}
		}
		frame := shirei.ExternalFrame{
			Kind:   shirei.ExternalNV12,
			Width:  w,
			Height: h,
			Data: &shirei.ExternalNV12Frame{
				Y: buf.y, UV: buf.uv,
				YStride: w, UVStride: uvRowBytes,
				FullRange: fullRange,
			},
		}
		var id shirei.ImageId
		shirei.WithFrameLock(func() {
			id = shirei.UseExternalImage(p.key, frame)
		})
		if id != 0 {
			p.mu.Lock()
			p.id = id
			p.w = w
			p.h = h
			p.mu.Unlock()
			shirei.RequestNextFrame()
			return
		}
	}

	// CPU YUV → RGBA conversion into double-buffered image.RGBA.
	slot := p.rgbaNext & 1
	p.rgbaNext++
	rgba := p.rgbaBufs[slot]
	if rgba == nil || rgba.Rect.Dx() != w || rgba.Rect.Dy() != h {
		rgba = image.NewRGBA(image.Rect(0, 0, w, h))
		p.rgbaBufs[slot] = rgba
	}

	var yMul, yOff, rv, gu, gv, bu int
	switch {
	case bt709 && !fullRange:
		yMul, yOff, rv, gu, gv, bu = 298, 16, 459, -55, -136, 541
	case bt709 && fullRange:
		yMul, yOff, rv, gu, gv, bu = 256, 0, 403, -48, -120, 475
	case !bt709 && !fullRange:
		yMul, yOff, rv, gu, gv, bu = 298, 16, 409, -100, -208, 516
	default:
		yMul, yOff, rv, gu, gv, bu = 256, 0, 359, -88, -183, 454
	}

	pix := rgba.Pix
	rowBytes := w * 4
	xs, ys := uint(xShift), uint(yShift)
	for row := 0; row < h; row++ {
		yRowOff := row * yStride
		uvRowOff := (row >> ys) * uvStride
		dst := pix[row*rowBytes : (row+1)*rowBytes]
		if bitDepth == 8 {
			if yRowOff+w > len(yPlane) {
				break
			}
			for x := 0; x < w; x++ {
				yv := int(yPlane[yRowOff+x]) - yOff
				var d, e int
				if hasChroma {
					ux := uvRowOff + (x >> xs)
					if ux < len(uPlane) && ux < len(vPlane) {
						d = int(uPlane[ux]) - 128
						e = int(vPlane[ux]) - 128
					}
				}
				c := yMul*yv + 128
				dst[x*4+0] = clampByte((c + rv*e) >> 8)
				dst[x*4+1] = clampByte((c + gu*d + gv*e) >> 8)
				dst[x*4+2] = clampByte((c + bu*d) >> 8)
				dst[x*4+3] = 255
			}
		} else {
			if yRowOff+w*2 > len(yPlane) {
				break
			}
			for x := 0; x < w; x++ {
				y16 := uint16(yPlane[yRowOff+x*2]) | uint16(yPlane[yRowOff+x*2+1])<<8
				yv := int(y16>>shift) - yOff
				var d, e int
				if hasChroma {
					ux := uvRowOff + (x>>xs)*2
					if ux+1 < len(uPlane) && ux+1 < len(vPlane) {
						u16 := uint16(uPlane[ux]) | uint16(uPlane[ux+1])<<8
						v16 := uint16(vPlane[ux]) | uint16(vPlane[ux+1])<<8
						d = int(u16>>shift) - 128
						e = int(v16>>shift) - 128
					}
				}
				c := yMul*yv + 128
				dst[x*4+0] = clampByte((c + rv*e) >> 8)
				dst[x*4+1] = clampByte((c + gu*d + gv*e) >> 8)
				dst[x*4+2] = clampByte((c + bu*d) >> 8)
				dst[x*4+3] = 255
			}
		}
	}

	var id shirei.ImageId
	shirei.WithFrameLock(func() {
		id = shirei.UseImage(p.key, rgba)
	})
	if id == 0 {
		return
	}
	p.mu.Lock()
	p.id = id
	p.w = w
	p.h = h
	p.mu.Unlock()
	shirei.RequestNextFrame()
}

func clampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func (p *streamPlayer) writeAudio(ctx context.Context, samples []float32) bool {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for len(samples) > 0 {
		if ctx.Err() != nil {
			return false
		}
		n := p.audio.write(samples)
		if n > 0 {
			samples = samples[n:]
			continue
		}
		if !p.playingA.Load() {
			select {
			case <-ctx.Done():
				return false
			case <-p.playWake:
				continue
			}
		}
		if timer == nil {
			timer = time.NewTimer(8 * time.Millisecond)
		} else {
			timer.Reset(8 * time.Millisecond)
		}
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
		}
	}
	return true
}

func (p *streamPlayer) mixInto(out []float32, add bool) {
	if !p.playingA.Load() {
		if !add {
			clear(out)
		}
		return
	}
	dst := out
	if add {
		if cap(p.mixScratch) < len(out) {
			p.mixScratch = make([]float32, len(out))
		}
		dst = p.mixScratch[:len(out)]
	}
	p.audio.mu.Lock()
	n := p.audio.readLocked(dst)
	p.played.Add(int64(n / audioChannels))
	p.audio.mu.Unlock()
	if p.mutedA.Load() {
		if !add {
			clear(out)
		}
		return
	}
	vol := math.Float32frombits(p.volumeBits.Load())
	if vol <= 0 {
		if !add {
			clear(out)
		}
		return
	}
	if !add {
		if vol < 1 {
			for i := 0; i < n; i++ {
				out[i] *= vol
			}
		}
		for i := n; i < len(out); i++ {
			out[i] = 0
		}
		return
	}
	for i := 0; i < n; i++ {
		s := out[i] + dst[i]*vol
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		out[i] = s
	}
}

func (p *streamPlayer) dropAudioClock() {
	p.audio.mu.Lock()
	played := p.played.Swap(0)
	p.audio.mu.Unlock()
	p.mu.Lock()
	if p.audioOn {
		p.audioOn = false
		p.base += float64(played) / float64(audioRate)
		if p.playingA.Load() {
			p.wall = time.Now()
		} else {
			p.wall = time.Time{}
		}
	}
	p.mu.Unlock()
}

func (p *streamPlayer) positionSeconds() float64 {
	p.mu.Lock()
	if p.seekPending {
		pos := p.seekTo
		p.mu.Unlock()
		if pos < 0 {
			return 0
		}
		return pos
	}
	base := p.base
	audio := p.audioOn
	playing := p.playing
	wall := p.wall
	p.mu.Unlock()
	var pos float64
	if audio {
		pos = base + float64(p.played.Load())/float64(audioRate) - app.AudioLatency().Seconds()
		if pos < base {
			pos = base
		}
	} else if playing && !wall.IsZero() {
		pos = base + time.Since(wall).Seconds()
	} else {
		pos = base
	}
	if pos < 0 {
		pos = 0
	}
	return pos
}

func (p *streamPlayer) setError(err error) {
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
	p.mu.Unlock()
	p.playingA.Store(false)
	shirei.RequestNextFrame()
}

func seconds(v float64) time.Duration {
	if v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

func outputSize(w, h int) (int, int) {
	if w > maxVideoWidth && w > 0 && h > 0 {
		h = h * maxVideoWidth / w
		w = maxVideoWidth
	}
	if w%2 != 0 {
		w--
	}
	if h%2 != 0 {
		h--
	}
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	return w, h
}

type pcmRing struct {
	mu  sync.Mutex
	buf []float32
	r   int
	n   int
}

func newPCMRing(n int) *pcmRing {
	return &pcmRing{buf: make([]float32, n)}
}

func (r *pcmRing) write(src []float32) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	space := len(r.buf) - r.n
	if space > len(src) {
		space = len(src)
	}
	space -= space % audioChannels
	if space <= 0 {
		return 0
	}
	w := (r.r + r.n) % len(r.buf)
	first := copy(r.buf[w:], src[:space])
	if first < space {
		copy(r.buf, src[first:space])
	}
	r.n += space
	return space
}

func (r *pcmRing) readLocked(dst []float32) int {
	n := r.n
	if n > len(dst) {
		n = len(dst)
	}
	n -= n % audioChannels
	if n == 0 {
		return 0
	}
	first := copy(dst[:n], r.buf[r.r:])
	if first < n {
		copy(dst[first:n], r.buf)
	}
	r.r = (r.r + n) % len(r.buf)
	r.n -= n
	return n
}

func (r *pcmRing) resetLocked() {
	r.r = 0
	r.n = 0
}

var (
	nextPlayerID atomic.Uint64

	audioStartOnce sync.Once
	audioStartErr  error
	audioMixMu     sync.Mutex
	audioPlayers   atomic.Pointer[[]*streamPlayer]
)

func registerAudioPlayer(p *streamPlayer) {
	audioMixMu.Lock()
	defer audioMixMu.Unlock()
	var cur []*streamPlayer
	if ptr := audioPlayers.Load(); ptr != nil {
		cur = *ptr
	}
	for _, existing := range cur {
		if existing == p {
			return
		}
	}
	next := make([]*streamPlayer, len(cur)+1)
	copy(next, cur)
	next[len(cur)] = p
	audioPlayers.Store(&next)
}

func unregisterAudioPlayer(p *streamPlayer) {
	audioMixMu.Lock()
	defer audioMixMu.Unlock()
	ptr := audioPlayers.Load()
	if ptr == nil {
		return
	}
	cur := *ptr
	next := make([]*streamPlayer, 0, len(cur))
	for _, existing := range cur {
		if existing != p {
			next = append(next, existing)
		}
	}
	audioPlayers.Store(&next)
}

func isAudioPlayerRegistered(p *streamPlayer) bool {
	ptr := audioPlayers.Load()
	if ptr == nil {
		return false
	}
	for _, existing := range *ptr {
		if existing == p {
			return true
		}
	}
	return false
}

func ensureStreamAudio() error {
	audioStartOnce.Do(func() {
		audioStartErr = app.StartAudioChannels(audioRate, audioChannels, func(out []float32) {
			ptr := audioPlayers.Load()
			if ptr == nil || len(*ptr) == 0 {
				clear(out)
				return
			}
			players := *ptr
			if len(players) == 1 {
				players[0].mixInto(out, false)
				return
			}
			clear(out)
			for _, p := range players {
				p.mixInto(out, true)
			}
		})
	})
	return audioStartErr
}
