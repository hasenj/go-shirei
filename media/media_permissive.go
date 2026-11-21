//go:build darwin || linux || windows || android

package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

type decodedVideoFrame struct {
	y, u, v   []byte
	yStride   int
	uvStride  int
	w, h      int
	xShift    int
	yShift    int
	bitDepth  int
	bt709     bool
	fullRange bool
	pts       float64
}

type videoDecoder interface {
	decode(pkt demuxPacket, emit func(decodedVideoFrame)) error
	flush(emit func(decodedVideoFrame))
	reset()
	close()
}

type audioDecoder interface {
	decode(pkt demuxPacket) ([]float32, error) // interleaved stereo float32 at sampleRate()
	sampleRate() int
	reset()
	close()
}

type videoDecoderEntry struct {
	available func() bool
	create    func(mediaProbe) (videoDecoder, error)
}

type audioDecoderEntry struct {
	available func() bool
	create    func(mediaProbe) (audioDecoder, error)
}

var (
	codecRegMu  sync.RWMutex
	videoRegMap = map[videoCodec]videoDecoderEntry{}
	audioRegMap = map[audioCodec]audioDecoderEntry{}
)

func registerVideoDecoder(codec videoCodec, available func() bool, create func(mediaProbe) (videoDecoder, error)) {
	codecRegMu.Lock()
	videoRegMap[codec] = videoDecoderEntry{available: available, create: create}
	codecRegMu.Unlock()
}

func registerAudioDecoder(codec audioCodec, available func() bool, create func(mediaProbe) (audioDecoder, error)) {
	codecRegMu.Lock()
	audioRegMap[codec] = audioDecoderEntry{available: available, create: create}
	codecRegMu.Unlock()
}

// canDecodePermissive reports whether the registered permissive codecs can
// decode the video track (and audio track, if present) of probe.
func canDecodePermissive(p mediaProbe) bool {
	if p.Container == containerUnknown || p.VideoCodec == videoCodecNone {
		return false
	}
	codecRegMu.RLock()
	ve, vok := videoRegMap[p.VideoCodec]
	ae, aok := audioRegMap[p.AudioCodec]
	codecRegMu.RUnlock()
	if !vok || !ve.available() {
		return false
	}
	// If the file has an audio track whose permissive decoder is unavailable,
	// we can still play video-only if needed, OR let OS/GStreamer/ffmpeg try first.
	// Return true when audio is either absent or decodable.
	if p.AudioCodec != audioCodecNone && (!aok || !ae.available()) {
		return false
	}
	return true
}

type permissiveEngine struct {
	demux demuxer
	meta  probeInfo
	pInfo mediaProbe
	vdec  videoDecoder
	adec  audioDecoder
}

func openPermissiveEngine(abs string) (streamEngine, error) {
	d, err := openDemuxer(abs)
	if err != nil {
		return nil, err
	}
	pi := d.probe()
	if pi.VideoCodec == videoCodecNone {
		_ = d.close()
		return nil, errors.New("media: no video track")
	}

	codecRegMu.RLock()
	ve, vok := videoRegMap[pi.VideoCodec]
	ae, aok := audioRegMap[pi.AudioCodec]
	codecRegMu.RUnlock()

	if !vok {
		_ = d.close()
		return nil, fmt.Errorf("media: no permissive decoder for video codec %s", pi.VideoCodec)
	}
	vdec, err := ve.create(pi)
	if err != nil {
		_ = d.close()
		return nil, err
	}

	var adec audioDecoder
	if pi.AudioCodec != audioCodecNone && aok {
		if dec, aerr := ae.create(pi); aerr == nil {
			adec = dec
		}
	}

	fps := pi.FPS
	if fps <= 0 || fps > 240 {
		fps = 30
	}
	meta := probeInfo{
		width:    pi.Width,
		height:   pi.Height,
		fps:      fps,
		duration: pi.Duration,
		hasAudio: adec != nil,
	}
	return &permissiveEngine{
		demux: d,
		meta:  meta,
		pInfo: pi,
		vdec:  vdec,
		adec:  adec,
	}, nil
}

func (e *permissiveEngine) info() probeInfo { return e.meta }

func (e *permissiveEngine) close() {
	if e.vdec != nil {
		e.vdec.close()
		e.vdec = nil
	}
	if e.adec != nil {
		e.adec.close()
		e.adec = nil
	}
	if e.demux != nil {
		_ = e.demux.close()
		e.demux = nil
	}
}

func (e *permissiveEngine) run(p *streamPlayer) {
	var (
		done   <-chan struct{}
		cancel context.CancelFunc
	)
	stopSession := func() {
		if cancel == nil {
			return
		}
		cancel()
		<-done
		cancel = nil
		done = nil
	}
	defer stopSession()

	startSession := func(startAt float64) {
		stopSession()
		p.resetAudioClock(startAt)
		ctx, c := context.WithCancel(context.Background())
		ch := make(chan struct{})
		cancel = c
		done = ch
		go func() {
			defer close(ch)
			e.runSession(ctx, p, startAt)
		}()
	}

	startSession(0)

	for {
		select {
		case <-p.stop:
			return
		case <-p.wake:
			if target, ok := p.takeSeek(); ok {
				startSession(target)
			}
		case <-done:
			cancel = nil
			done = nil
			p.mu.Lock()
			closed := p.closed
			p.mu.Unlock()
			if closed {
				return
			}
			if target, ok := p.takeSeek(); ok {
				startSession(target)
				continue
			}
			if p.armLoop() {
				startSession(0)
			}
		}
	}
}

func (e *permissiveEngine) runSession(ctx context.Context, p *streamPlayer, startAt float64) {
	if err := e.demux.seek(startAt); err != nil {
		if ctx.Err() == nil {
			p.setError(err)
		}
		return
	}
	e.vdec.reset()
	if e.adec != nil {
		e.adec.reset()
	}

	vCh := make(chan demuxPacket, 32)
	var aCh chan demuxPacket
	var wg sync.WaitGroup

	if e.adec != nil {
		aCh = make(chan demuxPacket, 64)
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.audioLoop(ctx, p, aCh, startAt)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		e.videoLoop(ctx, p, vCh, startAt)
	}()

	for {
		if ctx.Err() != nil {
			break
		}
		pkt, err := e.demux.nextPacket()
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				p.setError(err)
			}
			break
		}
		if pkt.IsVideo {
			select {
			case vCh <- pkt:
			case <-ctx.Done():
			}
		} else if aCh != nil {
			select {
			case aCh <- pkt:
			case <-ctx.Done():
			}
		}
	}
	close(vCh)
	if aCh != nil {
		close(aCh)
	}
	wg.Wait()
}

func (e *permissiveEngine) videoLoop(ctx context.Context, p *streamPlayer, pkts <-chan demuxPacket, startAt float64) {
	firstFrame := true
	frameIdx := 0

	present := func(f decodedVideoFrame) bool {
		if ctx.Err() != nil {
			return false
		}
		pts := f.pts
		if pts <= 0 && frameIdx > 0 && p.fps > 0 {
			pts = startAt + float64(frameIdx)/p.fps
		}
		frameIdx++
		if startAt > 0 && pts < startAt-0.02 {
			return true
		}
		if firstFrame {
			firstFrame = false
		} else if !p.waitUntil(ctx, pts) {
			return false
		}
		p.publishPlanarYUV(f.y, f.u, f.v, f.yStride, f.uvStride, f.w, f.h, f.xShift, f.yShift, f.bitDepth, f.bt709, f.fullRange)
		return true
	}

	stop := false
	for pkt := range pkts {
		if stop || ctx.Err() != nil {
			return
		}
		_ = e.vdec.decode(pkt, func(f decodedVideoFrame) {
			if !stop && !present(f) {
				stop = true
			}
		})
	}
	if !stop && ctx.Err() == nil {
		e.vdec.flush(func(f decodedVideoFrame) {
			if !stop && !present(f) {
				stop = true
			}
		})
	}
}

func (e *permissiveEngine) audioLoop(ctx context.Context, p *streamPlayer, pkts <-chan demuxPacket, startAt float64) {
	res := newStereoResampler(e.adec.sampleRate(), audioRate)
	preSkip := 0
	if startAt <= 0.001 {
		preSkip = e.pInfo.AudioPreSkip
	}
	for pkt := range pkts {
		if ctx.Err() != nil {
			return
		}
		samples, err := e.adec.decode(pkt)
		if err != nil || len(samples) == 0 {
			continue
		}
		if preSkip > 0 {
			frames := len(samples) / audioChannels
			if preSkip >= frames {
				preSkip -= frames
				continue
			}
			samples = samples[preSkip*audioChannels:]
			preSkip = 0
		}
		if startAt > 0 && pkt.PTS < startAt-0.02 {
			continue
		}
		out := res.process(samples)
		if len(out) > 0 {
			if !p.writeAudio(ctx, out) {
				return
			}
		}
	}
}

// stereoResampler resamples interleaved stereo float32 PCM using linear
// interpolation across packet boundaries.
type stereoResampler struct {
	step  float64 // srcRate / dstRate
	pos   float64 // fractional source frame index relative to prev sample
	prevL float32
	prevR float32
	has   bool
	out   []float32
}

func newStereoResampler(srcRate, dstRate int) *stereoResampler {
	if srcRate <= 0 {
		srcRate = dstRate
	}
	return &stereoResampler{
		step: float64(srcRate) / float64(dstRate),
	}
}

func (r *stereoResampler) process(in []float32) []float32 {
	inFrames := len(in) / 2
	if inFrames == 0 {
		return nil
	}
	if r.step == 1.0 {
		return in
	}
	if !r.has {
		r.prevL = in[0]
		r.prevR = in[1]
		r.pos = 0
		r.has = true
	}

	r.out = r.out[:0]
	// Index 0 maps to (prevL, prevR); indices 1..inFrames map to in[0..inFrames-1].
	maxPos := float64(inFrames)
	for r.pos < maxPos {
		i0 := int(r.pos)
		frac := float32(r.pos - float64(i0))
		var l0, r0, l1, r1 float32
		if i0 == 0 {
			l0, r0 = r.prevL, r.prevR
		} else {
			l0, r0 = in[(i0-1)*2], in[(i0-1)*2+1]
		}
		if i0 < inFrames {
			l1, r1 = in[i0*2], in[i0*2+1]
		} else {
			l1, r1 = l0, r0
		}
		r.out = append(r.out, l0+(l1-l0)*frac, r0+(r1-r0)*frac)
		r.pos += r.step
	}
	r.pos -= float64(inFrames)
	r.prevL = in[(inFrames-1)*2]
	r.prevR = in[(inFrames-1)*2+1]
	return r.out
}

// splitVorbisCodecPrivate extracts the 3 Xiph-framed Vorbis initialization
// packets (identification, comment, setup) from Matroska/WebM CodecPrivate.
func splitVorbisCodecPrivate(priv []byte) ([3][]byte, error) {
	var out [3][]byte
	if len(priv) < 3 || priv[0] != 2 {
		return out, errors.New("media: invalid Vorbis CodecPrivate")
	}
	p := priv[1:]
	var lens [2]int
	for i := 0; i < 2; i++ {
		sz := 0
		for {
			if len(p) == 0 {
				return out, errors.New("media: truncated Vorbis CodecPrivate")
			}
			b := int(p[0])
			p = p[1:]
			sz += b
			if b < 255 {
				break
			}
		}
		lens[i] = sz
	}
	if lens[0]+lens[1] >= len(p) {
		return out, errors.New("media: Vorbis CodecPrivate header lengths out of range")
	}
	out[0] = p[:lens[0]]
	out[1] = p[lens[0] : lens[0]+lens[1]]
	out[2] = p[lens[0]+lens[1]:]
	return out, nil
}
