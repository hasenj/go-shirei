//go:build linux && !android

package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.hasen.dev/shirei"
)

// Player owns one Linux video playback instance. Create it with Open; call
// View from the frame function to place the video. Control methods are safe
// from the frame goroutine.
//
// Backend order (overridable via SHIREI_MEDIA=libav|gstreamer|permissive|ffmpeg):
//  1. in-process libav* + VA-API dmabuf when built with -tags libav
//  2. system GStreamer 1.0 via purego (zero cgo)
//  3. permissive in-process demuxer + dav1d/libvpx/libopus/libvorbis via purego
//  4. ffmpeg/ffprobe subprocesses on PATH
type Player struct {
	*streamPlayer
}

// Open opens path for playback. Call it before Run. Decoding starts when the
// player is first viewed or played.
func Open(path string) (*Player, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SHIREI_MEDIA")))
	switch mode {
	case "libav":
		if libavOpen == nil {
			return nil, errors.New("media: binary was not built with -tags libav")
		}
		eng, err := libavOpen(abs)
		if err != nil {
			return nil, err
		}
		return &Player{streamPlayer: newStreamPlayer(eng)}, nil
	case "gstreamer":
		eng, err := openGStreamerEngine(abs)
		if err != nil {
			return nil, err
		}
		return &Player{streamPlayer: newStreamPlayer(eng)}, nil
	case "permissive":
		eng, err := openPermissiveEngine(abs)
		if err != nil {
			return nil, err
		}
		return &Player{streamPlayer: newStreamPlayer(eng)}, nil
	case "ffmpeg":
		eng, err := openFFmpegEngine(abs)
		if err != nil {
			return nil, err
		}
		return &Player{streamPlayer: newStreamPlayer(eng)}, nil
	}

	// Default auto-selection order: libav (if compiled in) → GStreamer →
	// permissive web codecs → ffmpeg CLI.
	var errs []error
	if libavOpen != nil {
		if eng, lerr := libavOpen(abs); lerr == nil {
			logLinuxBackend("libav", eng)
			return &Player{streamPlayer: newStreamPlayer(eng)}, nil
		} else {
			errs = append(errs, lerr)
		}
	}
	if eng, gerr := openGStreamerEngine(abs); gerr == nil {
		logLinuxBackend("gstreamer", eng)
		return &Player{streamPlayer: newStreamPlayer(eng)}, nil
	} else {
		errs = append(errs, gerr)
	}
	if probe, ok := sniffFile(abs); ok && canDecodePermissive(probe) {
		if eng, perr := openPermissiveEngine(abs); perr == nil {
			logLinuxBackend("permissive", eng)
			return &Player{streamPlayer: newStreamPlayer(eng)}, nil
		} else {
			errs = append(errs, perr)
		}
	}
	if eng, ferr := openFFmpegEngine(abs); ferr == nil {
		logLinuxBackend("ffmpeg", eng)
		return &Player{streamPlayer: newStreamPlayer(eng)}, nil
	} else {
		errs = append(errs, ferr)
	}
	return nil, errors.Join(errs...)
}

func logLinuxBackend(name string, eng streamEngine) {
	if os.Getenv("SHIREI_MEDIA_DEBUG") == "" && os.Getenv("SHIREI_PERF") == "" {
		return
	}
	info := eng.info()
	fmt.Fprintf(os.Stderr, "media: linux backend=%s %dx%d %.2ffps audio=%v nv12=%v\n",
		name, info.width, info.height, info.fps, info.hasAudio, shirei.SupportsExternalKind(shirei.ExternalNV12))
}

// -----------------------------------------------------------------------------
// Default Linux engine: ffmpeg + ffprobe subprocesses (CGO_ENABLED=0)

type ffmpegEngine struct {
	path string
	meta probeInfo
}

func openFFmpegEngine(abs string) (streamEngine, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("media: ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil, errors.New("media: ffprobe is not installed")
	}
	info, err := probe(abs)
	if err != nil {
		return nil, err
	}
	return &ffmpegEngine{path: abs, meta: info}, nil
}

func (e *ffmpegEngine) info() probeInfo { return e.meta }
func (e *ffmpegEngine) close()          {}

func (e *ffmpegEngine) run(p *streamPlayer) {
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
	startSession := func() {
		stopSession()
		p.mu.Lock()
		startAt := p.base
		p.mu.Unlock()
		p.resetAudioClock(startAt)

		ctx, c := context.WithCancel(context.Background())
		cancel = c
		ch := make(chan struct{})
		done = ch
		go func() {
			defer close(ch)
			err := e.runSession(ctx, p, startAt)
			if err != nil && ctx.Err() == nil {
				p.setError(err)
			}
		}()
	}

	select {
	case <-p.stop:
		return
	default:
	}
	p.takeSeek()
	startSession()

	for {
		select {
		case <-p.stop:
			stopSession()
			return
		case <-p.wake:
			if _, ok := p.takeSeek(); !ok {
				continue
			}
			startSession()
		case <-done:
			done = nil
			cancel = nil
			if _, ok := p.takeSeek(); ok {
				startSession()
				continue
			}
			if p.armLoop() {
				startSession()
			}
		}
	}
}

func (e *ffmpegEngine) runSession(ctx context.Context, p *streamPlayer, start float64) error {
	useNV12 := shirei.SupportsExternalKind(shirei.ExternalNV12)
	pixFmt := "rgba"
	frameBytes := p.outW * p.outH * 4
	if useNV12 {
		pixFmt = "nv12"
		frameBytes = p.outW * p.outH * 3 / 2
	}
	if frameBytes <= 0 {
		return errors.New("media: video has no size")
	}

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()

	stderr := &tailBuf{}
	vcmd := exec.CommandContext(sctx, "ffmpeg", videoArgs(e.path, start, p.outW, p.outH, p.fps, pixFmt)...)
	vcmd.WaitDelay = time.Second
	vcmd.Stderr = stderr
	vout, err := vcmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := vcmd.Start(); err != nil {
		return withStderr(stderr, err)
	}

	var acmd *exec.Cmd
	audioDone := make(chan struct{})
	audioStarted := false
	if p.audioEnabled() {
		acmd = exec.CommandContext(sctx, "ffmpeg", audioArgs(e.path, start)...)
		acmd.WaitDelay = time.Second
		acmd.Stderr = io.Discard
		var aout io.ReadCloser
		aout, err = acmd.StdoutPipe()
		if err != nil {
			scancel()
			_ = vcmd.Process.Kill()
			_ = vcmd.Wait()
			return err
		}
		if err := acmd.Start(); err != nil {
			p.dropAudioClock()
			acmd = nil
		} else {
			audioStarted = true
			go func() {
				defer close(audioDone)
				_ = readAudioPipe(sctx, p, aout)
				if sctx.Err() == nil {
					p.dropAudioClock()
				}
			}()
		}
	}
	if !audioStarted {
		close(audioDone)
	}

	readErr := e.readVideo(sctx, p, vout, start, frameBytes, useNV12)
	scancel()
	if vcmd.Process != nil {
		_ = vcmd.Process.Kill()
	}
	if acmd != nil && acmd.Process != nil {
		_ = acmd.Process.Kill()
	}
	<-audioDone
	_ = vcmd.Wait()
	if acmd != nil {
		_ = acmd.Wait()
	}
	if ctx.Err() != nil {
		return nil
	}
	if readErr != nil {
		return withStderr(stderr, readErr)
	}
	return nil
}

func (e *ffmpegEngine) readVideo(ctx context.Context, p *streamPlayer, r io.Reader, start float64, frameBytes int, useNV12 bool) error {
	buf := make([]byte, frameBytes)
	yBytes := p.outW * p.outH
	var index int
	var lastPublish time.Time
	for {
		_, err := io.ReadFull(r, buf)
		if err != nil {
			if (index > 0 || start > 0) && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				return nil
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return errors.New("no frames")
			}
			return err
		}
		pts := start + float64(index)/p.fps
		if index > 0 {
			if !p.waitUntil(ctx, pts) {
				return nil
			}
		}
		late := p.positionSeconds() - pts
		if index == 0 || late < 1.5/p.fps || time.Since(lastPublish) > 100*time.Millisecond {
			if useNV12 {
				p.publishNV12(buf[:yBytes], buf[yBytes:], p.outW, p.outW, p.outW, p.outH, e.meta.fullRange)
			} else {
				p.publishRGBA(buf, p.outW*4, p.outW, p.outH)
			}
			lastPublish = time.Now()
		}
		index++
	}
}

func readAudioPipe(ctx context.Context, p *streamPlayer, r io.Reader) error {
	const frameBytes = 4 * audioChannels
	raw := make([]byte, 4096*frameBytes)
	samples := make([]float32, 4096*audioChannels)
	carry := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := r.Read(raw[carry:])
		total := carry + n
		aligned := total - (total % frameBytes)
		if aligned > 0 {
			count := aligned / 4
			for i := 0; i < count; i++ {
				samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4 : i*4+4]))
			}
			if !p.writeAudio(ctx, samples[:count]) {
				return ctx.Err()
			}
		}
		carry = total - aligned
		if carry > 0 {
			copy(raw[:carry], raw[aligned:total])
		}
		if err != nil {
			return err
		}
	}
}

func videoArgs(path string, start float64, w, h int, fps float64, pixFmt string) []string {
	return append(ffmpegHead(path, start),
		"-map", "0:v:0",
		"-an", "-sn", "-dn",
		"-fps_mode", "cfr",
		"-r", strconv.FormatFloat(fps, 'f', 6, 64),
		"-vf", fmt.Sprintf("scale=%d:%d:flags=bilinear", w, h),
		"-pix_fmt", pixFmt,
		"-f", "rawvideo",
		"pipe:1",
	)
}

func audioArgs(path string, start float64) []string {
	return append(ffmpegHead(path, start),
		"-map", "0:a:0",
		"-vn", "-sn", "-dn",
		"-ac", strconv.Itoa(audioChannels),
		"-ar", strconv.Itoa(audioRate),
		"-f", "f32le",
		"pipe:1",
	)
}

// ffmpegHead places -ss before -i for fast accurate input seeking (supported
// natively by FFmpeg without decoding a multi-second pre-roll window).
func ffmpegHead(path string, start float64) []string {
	args := []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
	}
	if start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(start, 'f', 3, 64))
	}
	args = append(args, "-i", path)
	return args
}

func probe(path string) (probeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	var stderr tailBuf
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return probeInfo{}, withStderr(&stderr, err)
	}
	var doc struct {
		Streams []struct {
			CodecType    string            `json:"codec_type"`
			Width        int               `json:"width"`
			Height       int               `json:"height"`
			ColorRange   string            `json:"color_range"`
			AvgFrameRate string            `json:"avg_frame_rate"`
			RFrameRate   string            `json:"r_frame_rate"`
			Duration     flexFloat         `json:"duration"`
			Tags         map[string]string `json:"tags"`
			SideDataList []struct {
				Rotation flexFloat `json:"rotation"`
			} `json:"side_data_list"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
		Format struct {
			Duration flexFloat `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return probeInfo{}, fmt.Errorf("media: ffprobe: %w", err)
	}
	var info probeInfo
	info.duration = float64(doc.Format.Duration)
	for _, s := range doc.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic != 0 || s.Width <= 0 || s.Height <= 0 || info.width != 0 {
				continue
			}
			w, h := s.Width, s.Height
			rot := 0
			for _, sd := range s.SideDataList {
				if sd.Rotation != 0 {
					rot = int(math.Round(float64(sd.Rotation)))
					break
				}
			}
			if rot == 0 && s.Tags != nil {
				if v, err := strconv.Atoi(strings.TrimSpace(s.Tags["rotate"])); err == nil {
					rot = v
				}
			}
			if isQuarterTurn(rot) {
				w, h = h, w
			}
			info.width = w
			info.height = h
			info.fullRange = strings.EqualFold(s.ColorRange, "pc") || strings.EqualFold(s.ColorRange, "jpeg")
			info.fps = parseRate(s.AvgFrameRate)
			if info.fps <= 0 {
				info.fps = parseRate(s.RFrameRate)
			}
			if info.duration <= 0 {
				info.duration = float64(s.Duration)
			}
		case "audio":
			info.hasAudio = true
		}
	}
	if info.width <= 0 || info.height <= 0 {
		return probeInfo{}, errors.New("media: no video stream")
	}
	if info.fps <= 0 || info.fps > 480 {
		info.fps = 30
	}
	return info, nil
}

func isQuarterTurn(deg int) bool {
	deg %= 180
	if deg < 0 {
		deg += 180
	}
	return deg == 90
}

func parseRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return v
	}
	n, errN := strconv.ParseFloat(num, 64)
	d, errD := strconv.ParseFloat(den, 64)
	if errN != nil || errD != nil || d == 0 {
		return 0
	}
	return n / d
}

type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || bytes.Equal(b, []byte("N/A")) {
		return nil
	}
	v, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}

func withStderr(buf *tailBuf, err error) error {
	msg := strings.TrimSpace(buf.string())
	if len(msg) > 300 {
		msg = msg[len(msg)-300:]
	}
	if msg == "" {
		if err == nil {
			return errors.New("media: ffmpeg failed")
		}
		return fmt.Errorf("media: ffmpeg: %w", err)
	}
	return fmt.Errorf("media: ffmpeg: %s", msg)
}

type tailBuf struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = append([]byte(nil), t.b[len(t.b)-4096:]...)
	}
	return len(p), nil
}

func (t *tailBuf) string() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
