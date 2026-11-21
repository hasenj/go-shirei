// Package media plays local media files with the platform decoder and presents
// video frames through shirei's GPU compositor.
//
// A Player owns playback: decode, audio, clock, seeking, and buffering. The
// application owns the UI: call Player.View each frame to place the video in
// the container tree, and draw controls as ordinary shirei widgets above it.
//
// macOS and iOS decode with AVFoundation and hand each frame's IOSurface to
// shirei as an external Metal image (falling back to CPU RGBA when software
// rendering is active). Android decodes with MediaPlayer into a SurfaceTexture
// sampled via GL_TEXTURE_EXTERNAL_OES. Windows decodes with Media Foundation
// (IMFMediaEngine) into a shared D3D11 texture (or CPU RGBA fallback), or with
// in-process libav* when built with -tags libav. Linux tries in-process libav*
// + VA-API dmabuf when built with -tags libav, then system GStreamer 1.0 via
// purego, then the permissive in-process web codec decoder, then ffmpeg on PATH.
//
// On desktop platforms (macOS, Linux, Windows), a pure-Go WebM/Matroska and MP4
// demuxer paired with dynamically loaded BSD-licensed codecs (libdav1d for AV1,
// libvpx for VP8/VP9, libopus for Opus, libvorbis for Vorbis) fills format gaps
// not handled by the OS decoder without cgo. On iOS, the same BSD codecs are
// linked automatically as static archives when present in the codec cache
// (built by scripts/build_ios_codecs.sh). Set SHIREI_CODEC_DIR to load bundled
// desktop shared libraries from a specific directory, or SHIREI_MEDIA to select
// a backend explicitly. Other platforms return ErrUnsupported.
package media

import "errors"

// ErrUnsupported means this build has no media backend for the running OS.
var ErrUnsupported = errors.New("media: playback is not supported on this platform")
