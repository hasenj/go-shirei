//go:build ((linux && !android) || windows) && libav && cgo

package media

/*
#cgo pkg-config: libavformat libavcodec libavutil libswresample libswscale

#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/avutil.h>
#include <libavutil/channel_layout.h>
#include <libavutil/hwcontext.h>
#include <libavutil/imgutils.h>
#include <libavutil/pixdesc.h>
#include <libavutil/pixfmt.h>
#include <libswresample/swresample.h>
#include <libswscale/swscale.h>
#ifdef __linux__
#include <libavutil/hwcontext_drm.h>
#endif

#define SHIREI_FOURCC(a,b,c,d) \
	((uint32_t)(a) | ((uint32_t)(b) << 8) | ((uint32_t)(c) << 16) | ((uint32_t)(d) << 24))
#define SHIREI_DRM_R8       SHIREI_FOURCC('R','8',' ',' ')
#define SHIREI_DRM_GR88     SHIREI_FOURCC('G','R','8','8')
#define SHIREI_DRM_NV12     SHIREI_FOURCC('N','V','1','2')
#define SHIREI_DRM_ARGB8888 SHIREI_FOURCC('A','R','2','4')
#define SHIREI_DRM_XRGB8888 SHIREI_FOURCC('X','R','2','4')
#define SHIREI_DRM_ABGR8888 SHIREI_FOURCC('A','B','2','4')
#define SHIREI_DRM_XBGR8888 SHIREI_FOURCC('X','B','2','4')

typedef struct {
	AVFormatContext *fmt;
	AVCodecContext  *vctx;
	AVCodecContext  *actx;
	AVBufferRef     *hwdev;
	SwrContext      *swr;
	struct SwsContext *sws;
	int sws_w, sws_h, sws_dst_w, sws_dst_h, sws_src_fmt, sws_dst_fmt;

	int vstream;
	int astream;
	int width;
	int height;
	double fps;
	double duration;
	double start_time;
	int has_audio;
	int full_range;
	int use_vaapi;
	double v_tb;
	double a_tb;
} ShireiLibav;

typedef struct {
	uint32_t drm_format;
	int nb_planes;
	int fd[4];
	int offset[4];
	int stride[4];
	uint64_t modifier[4];
	int full_range;
} ShireiDRMInfo;

#ifdef __linux__
static enum AVPixelFormat shirei_get_vaapi_fmt(AVCodecContext *ctx, const enum AVPixelFormat *pix_fmts) {
	(void)ctx;
	for (const enum AVPixelFormat *p = pix_fmts; *p != AV_PIX_FMT_NONE; p++) {
		if (*p == AV_PIX_FMT_VAAPI) {
			return AV_PIX_FMT_VAAPI;
		}
	}
	// Skip other hardware formats (vdpau, vulkan, cuda, ...) so FFmpeg falls
	// back directly to software decode without printing spurious hwaccel errors.
	for (const enum AVPixelFormat *p = pix_fmts; *p != AV_PIX_FMT_NONE; p++) {
		const AVPixFmtDescriptor *desc = av_pix_fmt_desc_get(*p);
		if (desc && !(desc->flags & AV_PIX_FMT_FLAG_HWACCEL)) {
			return *p;
		}
	}
	return pix_fmts[0];
}
#endif

static void shirei_libav_close(ShireiLibav *s) {
	if (!s) return;
	if (s->sws) {
		sws_freeContext(s->sws);
		s->sws = NULL;
	}
	if (s->swr) {
		swr_free(&s->swr);
	}
	if (s->vctx) {
		avcodec_free_context(&s->vctx);
	}
	if (s->actx) {
		avcodec_free_context(&s->actx);
	}
	if (s->hwdev) {
		av_buffer_unref(&s->hwdev);
	}
	if (s->fmt) {
		avformat_close_input(&s->fmt);
	}
	free(s);
}

static int shirei_libav_open(const char *path, const char *drm_node, int try_vaapi, int audio_rate, ShireiLibav **out, char *errbuf, size_t errlen) {
	(void)drm_node;
	(void)try_vaapi;
	ShireiLibav *s = (ShireiLibav *)calloc(1, sizeof(ShireiLibav));
	if (!s) {
		snprintf(errbuf, errlen, "out of memory");
		return -1;
	}
	s->vstream = -1;
	s->astream = -1;

	int rc = avformat_open_input(&s->fmt, path, NULL, NULL);
	if (rc < 0) {
		av_strerror(rc, errbuf, errlen);
		shirei_libav_close(s);
		return rc;
	}
	rc = avformat_find_stream_info(s->fmt, NULL);
	if (rc < 0) {
		av_strerror(rc, errbuf, errlen);
		shirei_libav_close(s);
		return rc;
	}

	s->vstream = av_find_best_stream(s->fmt, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
	if (s->vstream < 0) {
		snprintf(errbuf, errlen, "no video stream");
		shirei_libav_close(s);
		return -1;
	}
	AVStream *vs = s->fmt->streams[s->vstream];
	const AVCodec *vcodec = avcodec_find_decoder(vs->codecpar->codec_id);
	if (!vcodec) {
		snprintf(errbuf, errlen, "unsupported video codec");
		shirei_libav_close(s);
		return -1;
	}
	s->vctx = avcodec_alloc_context3(vcodec);
	if (!s->vctx) {
		snprintf(errbuf, errlen, "alloc video codec context failed");
		shirei_libav_close(s);
		return -1;
	}
	rc = avcodec_parameters_to_context(s->vctx, vs->codecpar);
	if (rc < 0) {
		av_strerror(rc, errbuf, errlen);
		shirei_libav_close(s);
		return rc;
	}
	s->vctx->thread_count = 0; // auto

#ifdef __linux__
	if (try_vaapi) {
		const char *dev = (drm_node && drm_node[0] != '\0') ? drm_node : NULL;
		if (av_hwdevice_ctx_create(&s->hwdev, AV_HWDEVICE_TYPE_VAAPI, dev, NULL, 0) == 0) {
			s->vctx->hw_device_ctx = av_buffer_ref(s->hwdev);
			s->vctx->get_format = shirei_get_vaapi_fmt;
			s->use_vaapi = 1;
		}
	}
#endif

	rc = avcodec_open2(s->vctx, vcodec, NULL);
	if (rc < 0 && s->use_vaapi) {
		avcodec_free_context(&s->vctx);
		av_buffer_unref(&s->hwdev);
		s->use_vaapi = 0;
		s->vctx = avcodec_alloc_context3(vcodec);
		if (s->vctx && avcodec_parameters_to_context(s->vctx, vs->codecpar) == 0) {
			s->vctx->thread_count = 0;
			rc = avcodec_open2(s->vctx, vcodec, NULL);
		}
	}
	if (rc < 0) {
		av_strerror(rc, errbuf, errlen);
		shirei_libav_close(s);
		return rc;
	}

	s->width = vs->codecpar->width;
	s->height = vs->codecpar->height;
	s->full_range = (vs->codecpar->color_range == AVCOL_RANGE_JPEG) ? 1 : 0;
	s->v_tb = av_q2d(vs->time_base);

	AVRational fr = av_guess_frame_rate(s->fmt, vs, NULL);
	if (fr.num > 0 && fr.den > 0) {
		s->fps = av_q2d(fr);
	}
	if (s->fps <= 0 || s->fps > 480) {
		s->fps = 30.0;
	}
	if (s->fmt->start_time != AV_NOPTS_VALUE) {
		s->start_time = (double)s->fmt->start_time / (double)AV_TIME_BASE;
	}
	if (s->fmt->duration != AV_NOPTS_VALUE && s->fmt->duration > 0) {
		s->duration = (double)s->fmt->duration / (double)AV_TIME_BASE;
	} else if (vs->duration != AV_NOPTS_VALUE && vs->duration > 0) {
		s->duration = (double)vs->duration * s->v_tb;
	}

	s->astream = av_find_best_stream(s->fmt, AVMEDIA_TYPE_AUDIO, -1, s->vstream, NULL, 0);
	if (s->astream >= 0) {
		AVStream *as = s->fmt->streams[s->astream];
		const AVCodec *acodec = avcodec_find_decoder(as->codecpar->codec_id);
		if (acodec) {
			s->actx = avcodec_alloc_context3(acodec);
			if (s->actx && avcodec_parameters_to_context(s->actx, as->codecpar) == 0 &&
			    avcodec_open2(s->actx, acodec, NULL) == 0) {
				AVChannelLayout out_layout = {0};
				av_channel_layout_default(&out_layout, 2);
				AVChannelLayout in_layout = {0};
				if (s->actx->ch_layout.nb_channels > 0) {
					av_channel_layout_copy(&in_layout, &s->actx->ch_layout);
				} else {
					av_channel_layout_default(&in_layout, 2);
				}
				if (swr_alloc_set_opts2(&s->swr,
						&out_layout, AV_SAMPLE_FMT_FLT, audio_rate,
						&in_layout, s->actx->sample_fmt, s->actx->sample_rate,
						0, NULL) == 0 && swr_init(s->swr) == 0) {
					s->has_audio = 1;
					s->a_tb = av_q2d(as->time_base);
				} else {
					swr_free(&s->swr);
					avcodec_free_context(&s->actx);
					s->astream = -1;
				}
				av_channel_layout_uninit(&in_layout);
				av_channel_layout_uninit(&out_layout);
			} else {
				avcodec_free_context(&s->actx);
				s->astream = -1;
			}
		} else {
			s->astream = -1;
		}
	}

	*out = s;
	return 0;
}

static int shirei_libav_seek(ShireiLibav *s, double sec) {
	if (!s || !s->fmt) return -1;
	int64_t ts = (int64_t)((sec + s->start_time) * (double)AV_TIME_BASE);
	if (ts < 0) ts = 0;
	int rc = av_seek_frame(s->fmt, -1, ts, AVSEEK_FLAG_BACKWARD);
	if (rc < 0) return rc;
	if (s->vctx) avcodec_flush_buffers(s->vctx);
	if (s->actx) avcodec_flush_buffers(s->actx);
	if (s->swr) swr_init(s->swr);
	return 0;
}

static AVFrame *shirei_export_drm_prime(AVFrame *src, ShireiDRMInfo *info) {
#ifdef __linux__
	if (!src || !info || src->format != AV_PIX_FMT_VAAPI) return NULL;
	AVFrame *drm = av_frame_alloc();
	if (!drm) return NULL;
	drm->format = AV_PIX_FMT_DRM_PRIME;
	if (av_hwframe_map(drm, src, AV_HWFRAME_MAP_READ | AV_HWFRAME_MAP_DIRECT) < 0) {
		av_frame_free(&drm);
		return NULL;
	}
	drm->width = src->width;
	drm->height = src->height;

	const AVDRMFrameDescriptor *desc = (const AVDRMFrameDescriptor *)drm->data[0];
	if (!desc || desc->nb_objects <= 0 || desc->nb_layers <= 0) {
		av_frame_free(&drm);
		return NULL;
	}
	memset(info, 0, sizeof(*info));
	info->full_range = (src->color_range == AVCOL_RANGE_JPEG) ? 1 : 0;

	// Intel iHD exports NV12 as two layers (R8 + GR88); AMD/i965 often export
	// one NV12 layer with two planes. Normalize both into two-plane NV12.
	if (desc->nb_layers == 2 &&
	    desc->layers[0].format == SHIREI_DRM_R8 &&
	    desc->layers[1].format == SHIREI_DRM_GR88 &&
	    desc->layers[0].nb_planes >= 1 &&
	    desc->layers[1].nb_planes >= 1) {
		int o0 = desc->layers[0].planes[0].object_index;
		int o1 = desc->layers[1].planes[0].object_index;
		if (o0 < 0 || o0 >= desc->nb_objects || o1 < 0 || o1 >= desc->nb_objects) {
			av_frame_free(&drm);
			return NULL;
		}
		info->drm_format = SHIREI_DRM_NV12;
		info->nb_planes = 2;
		info->fd[0] = desc->objects[o0].fd;
		info->offset[0] = (int)desc->layers[0].planes[0].offset;
		info->stride[0] = (int)desc->layers[0].planes[0].pitch;
		info->modifier[0] = desc->objects[o0].format_modifier;
		info->fd[1] = desc->objects[o1].fd;
		info->offset[1] = (int)desc->layers[1].planes[0].offset;
		info->stride[1] = (int)desc->layers[1].planes[0].pitch;
		info->modifier[1] = desc->objects[o1].format_modifier;
		return drm;
	}

	const AVDRMLayerDescriptor *lay = &desc->layers[0];
	if (lay->nb_planes <= 0 || lay->nb_planes > 4) {
		av_frame_free(&drm);
		return NULL;
	}
	int supported = (lay->format == SHIREI_DRM_NV12 && lay->nb_planes >= 2) ||
		lay->format == SHIREI_DRM_ARGB8888 || lay->format == SHIREI_DRM_XRGB8888 ||
		lay->format == SHIREI_DRM_ABGR8888 || lay->format == SHIREI_DRM_XBGR8888;
	if (!supported) {
		av_frame_free(&drm);
		return NULL;
	}
	info->drm_format = lay->format;
	info->nb_planes = lay->nb_planes;
	for (int i = 0; i < lay->nb_planes; i++) {
		int oi = lay->planes[i].object_index;
		if (oi < 0 || oi >= desc->nb_objects) {
			av_frame_free(&drm);
			return NULL;
		}
		info->fd[i] = desc->objects[oi].fd;
		info->offset[i] = (int)lay->planes[i].offset;
		info->stride[i] = (int)lay->planes[i].pitch;
		info->modifier[i] = desc->objects[oi].format_modifier;
	}
	return drm;
#else
	(void)src;
	(void)info;
	return NULL;
#endif
}

static void shirei_free_frame(AVFrame *f) {
	if (f) {
		av_frame_free(&f);
	}
}

static int shirei_sws_convert(ShireiLibav *s, AVFrame *src, int dst_fmt, int dst_w, int dst_h, uint8_t *dst0, int stride0, uint8_t *dst1, int stride1) {
	if (!s || !src) return -1;
	if (!s->sws || s->sws_w != src->width || s->sws_h != src->height ||
	    s->sws_dst_w != dst_w || s->sws_dst_h != dst_h ||
	    s->sws_src_fmt != src->format || s->sws_dst_fmt != dst_fmt) {
		if (s->sws) sws_freeContext(s->sws);
		s->sws = sws_getContext(
			src->width, src->height, (enum AVPixelFormat)src->format,
			dst_w, dst_h, (enum AVPixelFormat)dst_fmt,
			SWS_BILINEAR, NULL, NULL, NULL);
		if (!s->sws) return -1;
		s->sws_w = src->width;
		s->sws_h = src->height;
		s->sws_dst_w = dst_w;
		s->sws_dst_h = dst_h;
		s->sws_src_fmt = src->format;
		s->sws_dst_fmt = dst_fmt;
	}
	uint8_t *dst_data[4] = {dst0, dst1, NULL, NULL};
	int dst_linesize[4] = {stride0, stride1, 0, 0};
	int h = sws_scale(s->sws, (const uint8_t *const *)src->data, src->linesize, 0, src->height, dst_data, dst_linesize);
	return (h > 0) ? 0 : -1;
}

static int shirei_resample_audio(ShireiLibav *s, AVFrame *src, float *out, int max_frames) {
	if (!s || !s->swr || max_frames <= 0) return 0;
	uint8_t *out_ptr[1] = {(uint8_t *)out};
	int n;
	if (src) {
		n = swr_convert(s->swr, out_ptr, max_frames, (const uint8_t **)src->data, src->nb_samples);
	} else {
		n = swr_convert(s->swr, out_ptr, max_frames, NULL, 0);
	}
	return (n > 0) ? n : 0;
}

static double shirei_frame_pts(ShireiLibav *s, AVFrame *f, double tb, double fallback) {
	if (!f) return fallback;
	int64_t ts = f->best_effort_timestamp;
	if (ts == AV_NOPTS_VALUE) ts = f->pts;
	if (ts == AV_NOPTS_VALUE) return fallback;
	double pts = (double)ts * tb - (s ? s->start_time : 0.0);
	return (pts < 0.0) ? 0.0 : pts;
}

static int shirei_is_eagain(int rc) { return rc == AVERROR(EAGAIN); }
static int shirei_is_eof(int rc)    { return rc == AVERROR_EOF; }
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"go.hasen.dev/shirei"
)

func init() {
	libavOpen = openLibavEngine
}

type libavEngine struct {
	mu   sync.Mutex
	s    *C.ShireiLibav
	meta probeInfo
}

func openLibavEngine(abs string) (streamEngine, error) {
	cpath := C.CString(abs)
	defer C.free(unsafe.Pointer(cpath))

	drmNode := strings.TrimSpace(os.Getenv("SHIREI_DRM_RENDER"))
	cdrm := C.CString(drmNode)
	defer C.free(unsafe.Pointer(cdrm))

	tryVAAPI := C.int(1)
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SHIREI_VAAPI")), "0") {
		tryVAAPI = 0
	}

	var errBuf [256]C.char
	var s *C.ShireiLibav
	if rc := C.shirei_libav_open(cpath, cdrm, tryVAAPI, C.int(audioRate), &s, &errBuf[0], C.size_t(len(errBuf))); rc < 0 {
		return nil, fmt.Errorf("media: libav: %s", C.GoString(&errBuf[0]))
	}
	meta := probeInfo{
		width:     int(s.width),
		height:    int(s.height),
		fps:       float64(s.fps),
		duration:  float64(s.duration),
		hasAudio:  s.has_audio != 0,
		fullRange: s.full_range != 0,
	}
	return &libavEngine{s: s, meta: meta}, nil
}

func (e *libavEngine) info() probeInfo { return e.meta }

func (e *libavEngine) close() {
	e.mu.Lock()
	if e.s != nil {
		C.shirei_libav_close(e.s)
		e.s = nil
	}
	e.mu.Unlock()
}

func (e *libavEngine) run(p *streamPlayer) {
	var (
		done   <-chan struct{}
		cancel context.CancelFunc
	)
	stopPass := func() {
		if cancel == nil {
			return
		}
		cancel()
		<-done
		cancel = nil
		done = nil
	}
	startPass := func(seek bool, startAt float64) {
		stopPass()
		p.resetAudioClock(startAt)
		if seek {
			if rc := C.shirei_libav_seek(e.s, C.double(startAt)); rc < 0 {
				if startAt <= 0.001 {
					_ = C.shirei_libav_seek(e.s, 0)
				}
			}
		}
		ctx, c := context.WithCancel(context.Background())
		cancel = c
		ch := make(chan struct{})
		done = ch
		go func() {
			defer close(ch)
			err := e.runPass(ctx, p, startAt)
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
	if target, ok := p.takeSeek(); ok {
		startPass(true, target)
	} else {
		startPass(false, 0)
	}

	for {
		select {
		case <-p.stop:
			stopPass()
			return
		case <-p.wake:
			if target, ok := p.takeSeek(); ok {
				startPass(true, target)
			}
		case <-done:
			done = nil
			cancel = nil
			if target, ok := p.takeSeek(); ok {
				startPass(true, target)
				continue
			}
			if p.armLoop() {
				startPass(true, 0)
			}
		}
	}
}

func (e *libavEngine) runPass(ctx context.Context, p *streamPlayer, startAt float64) error {
	vPkts := make(chan *C.AVPacket, 64)
	aPkts := make(chan *C.AVPacket, 128)
	hasAudio := e.s.has_audio != 0 && p.audioEnabled()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.decodeVideoLoop(ctx, p, vPkts, startAt)
		for pkt := range vPkts {
			C.av_packet_free(&pkt)
		}
	}()

	if hasAudio {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.decodeAudioLoop(ctx, p, aPkts, startAt)
			for pkt := range aPkts {
				C.av_packet_free(&pkt)
			}
			if ctx.Err() == nil {
				p.dropAudioClock()
			}
		}()
	}

	for ctx.Err() == nil {
		pkt := C.av_packet_alloc()
		if pkt == nil {
			break
		}
		rc := C.av_read_frame(e.s.fmt, pkt)
		if rc < 0 {
			C.av_packet_free(&pkt)
			break
		}
		switch pkt.stream_index {
		case e.s.vstream:
			select {
			case vPkts <- pkt:
			case <-ctx.Done():
				C.av_packet_free(&pkt)
			}
		case e.s.astream:
			if hasAudio {
				select {
				case aPkts <- pkt:
				case <-ctx.Done():
					C.av_packet_free(&pkt)
				}
			} else {
				C.av_packet_free(&pkt)
			}
		default:
			C.av_packet_free(&pkt)
		}
	}
	close(vPkts)
	close(aPkts)
	wg.Wait()
	return nil
}

func (e *libavEngine) decodeVideoLoop(ctx context.Context, p *streamPlayer, pkts <-chan *C.AVPacket, startAt float64) {
	frame := C.av_frame_alloc()
	if frame == nil {
		return
	}
	defer C.av_frame_free(&frame)

	swFrame := C.av_frame_alloc()
	if swFrame == nil {
		return
	}
	defer C.av_frame_free(&swFrame)

	var (
		convY, convUV, convRGBA []byte
		lastPTS                 = startAt
		lastPublish             time.Time
		postedFirst             bool
	)

	presentFrame := func(f *C.AVFrame) bool {
		pts := float64(C.shirei_frame_pts(e.s, f, e.s.v_tb, C.double(lastPTS)))
		lastPTS = pts + 1.0/p.fps

		if startAt > 0 && pts < startAt-0.02 {
			return true
		}
		if !postedFirst {
			if startAt > 0 && pts > 0 {
				p.alignClock(pts)
			}
		} else {
			if !p.waitUntil(ctx, pts) {
				return false
			}
		}
		late := p.positionSeconds() - pts
		if postedFirst && late >= 1.5/p.fps && time.Since(lastPublish) <= 100*time.Millisecond {
			return true
		}

		e.publishVideoFrame(p, f, swFrame, &convY, &convUV, &convRGBA)
		postedFirst = true
		lastPublish = time.Now()
		return true
	}

	for pkt := range pkts {
		if ctx.Err() != nil {
			C.av_packet_free(&pkt)
			return
		}
		rc := C.avcodec_send_packet(e.s.vctx, pkt)
		C.av_packet_free(&pkt)
		if rc < 0 {
			continue
		}
		for {
			rc = C.avcodec_receive_frame(e.s.vctx, frame)
			if C.shirei_is_eagain(rc) != 0 || C.shirei_is_eof(rc) != 0 || rc < 0 {
				break
			}
			ok := presentFrame(frame)
			C.av_frame_unref(frame)
			if !ok {
				return
			}
		}
	}

	if ctx.Err() != nil {
		return
	}
	C.avcodec_send_packet(e.s.vctx, nil)
	for {
		rc := C.avcodec_receive_frame(e.s.vctx, frame)
		if C.shirei_is_eagain(rc) != 0 || C.shirei_is_eof(rc) != 0 || rc < 0 {
			break
		}
		ok := presentFrame(frame)
		C.av_frame_unref(frame)
		if !ok {
			return
		}
	}
}

func (e *libavEngine) publishVideoFrame(p *streamPlayer, f, swFrame *C.AVFrame, convY, convUV, convRGBA *[]byte) {
	w := int(f.width)
	h := int(f.height)
	if w <= 0 || h <= 0 {
		return
	}
	fullRange := int(f.color_range) == int(C.AVCOL_RANGE_JPEG) || e.meta.fullRange

	// 1. Zero-copy VA-API -> DRM prime dmabuf -> EGLImage on Linux Wayland GPU.
	if int(f.format) == int(C.AV_PIX_FMT_VAAPI) && shirei.SupportsExternalKind(shirei.ExternalDMABuf) {
		var di C.ShireiDRMInfo
		drm := C.shirei_export_drm_prime(f, &di)
		if drm != nil {
			db := &shirei.ExternalDMABufFrame{
				DRMFormat:  uint32(di.drm_format),
				PlaneCount: int(di.nb_planes),
				FullRange:  di.full_range != 0 || fullRange,
			}
			for i := 0; i < db.PlaneCount && i < 4; i++ {
				db.Planes[i] = shirei.ExternalDMABufPlane{
					FD:       int(di.fd[i]),
					Offset:   int(di.offset[i]),
					Stride:   int(di.stride[i]),
					Modifier: uint64(di.modifier[i]),
				}
			}
			drmRef := drm
			if p.publishDMABuf(db, w, h, func() { C.shirei_free_frame(drmRef) }) {
				return
			}
		}
	}

	// 2. If still on a hardware surface, download to CPU memory once.
	src := f
	if int(f.format) == int(C.AV_PIX_FMT_VAAPI) {
		C.av_frame_unref(swFrame)
		if C.av_hwframe_transfer_data(swFrame, f, 0) < 0 {
			return
		}
		swFrame.color_range = f.color_range
		src = swFrame
	}

	// 3. NV12 external texture on GPU (Wayland GLES or Windows D3D11).
	if shirei.SupportsExternalKind(shirei.ExternalNV12) {
		outW, outH := outputSize(w, h)
		if int(src.format) == int(C.AV_PIX_FMT_NV12) && w == outW && h == outH &&
			src.data[0] != nil && src.data[1] != nil &&
			int(src.linesize[0]) >= w && int(src.linesize[1]) >= w {
			yStride := int(src.linesize[0])
			uvStride := int(src.linesize[1])
			ySlice := unsafe.Slice((*byte)(unsafe.Pointer(src.data[0])), h*yStride)
			uvSlice := unsafe.Slice((*byte)(unsafe.Pointer(src.data[1])), (h/2)*uvStride)
			p.publishNV12(ySlice, uvSlice, yStride, uvStride, w, h, fullRange)
			return
		}
		ySize := outW * outH
		uvSize := (outH / 2) * outW
		if len(*convY) != ySize {
			*convY = make([]byte, ySize)
		}
		if len(*convUV) != uvSize {
			*convUV = make([]byte, uvSize)
		}
		if C.shirei_sws_convert(e.s, src, C.int(C.AV_PIX_FMT_NV12), C.int(outW), C.int(outH),
			(*C.uint8_t)(unsafe.Pointer(&(*convY)[0])), C.int(outW),
			(*C.uint8_t)(unsafe.Pointer(&(*convUV)[0])), C.int(outW)) == 0 {
			p.publishNV12(*convY, *convUV, outW, outW, outW, outH, fullRange)
			return
		}
	}

	// 4. Universal CPU RGBA fallback (X11 SoftRenderer, SHIREI_GPU=0, headless).
	outW, outH := outputSize(w, h)
	need := outW * outH * 4
	if len(*convRGBA) != need {
		*convRGBA = make([]byte, need)
	}
	if C.shirei_sws_convert(e.s, src, C.int(C.AV_PIX_FMT_RGBA), C.int(outW), C.int(outH),
		(*C.uint8_t)(unsafe.Pointer(&(*convRGBA)[0])), C.int(outW*4), nil, 0) == 0 {
		p.publishRGBA(*convRGBA, outW*4, outW, outH)
	}
}

func (e *libavEngine) decodeAudioLoop(ctx context.Context, p *streamPlayer, pkts <-chan *C.AVPacket, startAt float64) {
	frame := C.av_frame_alloc()
	if frame == nil {
		return
	}
	defer C.av_frame_free(&frame)

	const maxOutFrames = 8192
	pcm := make([]float32, maxOutFrames*audioChannels)

	for pkt := range pkts {
		if ctx.Err() != nil {
			C.av_packet_free(&pkt)
			return
		}
		rc := C.avcodec_send_packet(e.s.actx, pkt)
		C.av_packet_free(&pkt)
		if rc < 0 {
			continue
		}
		for {
			rc = C.avcodec_receive_frame(e.s.actx, frame)
			if C.shirei_is_eagain(rc) != 0 || C.shirei_is_eof(rc) != 0 || rc < 0 {
				break
			}
			pts := float64(C.shirei_frame_pts(e.s, frame, e.s.a_tb, C.double(startAt)))
			if startAt > 0 && pts < startAt-0.02 {
				C.av_frame_unref(frame)
				continue
			}
			nFrames := int(C.shirei_resample_audio(e.s, frame, (*C.float)(unsafe.Pointer(&pcm[0])), C.int(maxOutFrames)))
			C.av_frame_unref(frame)
			for nFrames > 0 {
				if !p.writeAudio(ctx, pcm[:nFrames*audioChannels]) {
					return
				}
				if nFrames < maxOutFrames {
					break
				}
				nFrames = int(C.shirei_resample_audio(e.s, nil, (*C.float)(unsafe.Pointer(&pcm[0])), C.int(maxOutFrames)))
			}
		}
	}
}
