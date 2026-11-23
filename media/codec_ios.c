//go:build ios

#include "codec_ios.h"
#include <stddef.h>
#include <stdint.h>

// Weak default stubs: overridden when libdav1d.a / libvpx.a / libopus.a /
// libvorbis.a are linked with -Wl,-force_load.

// --- dav1d ---
__attribute__((weak)) const char *dav1d_version(void) { return NULL; }
__attribute__((weak)) void dav1d_default_settings(void *s) { (void)s; }
__attribute__((weak)) int dav1d_open(void **c, const void *s) { (void)c; (void)s; return -1; }
__attribute__((weak)) void dav1d_close(void **c) { (void)c; }
__attribute__((weak)) void dav1d_flush(void *c) { (void)c; }
__attribute__((weak)) uint8_t *dav1d_data_create(void *d, size_t sz) { (void)d; (void)sz; return NULL; }
__attribute__((weak)) void dav1d_data_unref(void *d) { (void)d; }
__attribute__((weak)) int dav1d_send_data(void *c, void *d) { (void)c; (void)d; return -1; }
__attribute__((weak)) int dav1d_get_picture(void *c, void *p) { (void)c; (void)p; return -1; }
__attribute__((weak)) void dav1d_picture_unref(void *p) { (void)p; }

// --- libvpx ---
__attribute__((weak)) const char *vpx_codec_version_str(void) { return NULL; }
__attribute__((weak)) void *vpx_codec_vp9_dx(void) { return NULL; }
__attribute__((weak)) void *vpx_codec_vp8_dx(void) { return NULL; }
__attribute__((weak)) int vpx_codec_dec_init_ver(void *ctx, void *iface, const void *cfg, long flags, int ver) {
	(void)ctx; (void)iface; (void)cfg; (void)flags; (void)ver; return -1;
}
__attribute__((weak)) int vpx_codec_decode(void *ctx, const uint8_t *data, unsigned int sz, void *priv, long deadline) {
	(void)ctx; (void)data; (void)sz; (void)priv; (void)deadline; return -1;
}
__attribute__((weak)) void *vpx_codec_get_frame(void *ctx, void **iter) { (void)ctx; (void)iter; return NULL; }
__attribute__((weak)) int vpx_codec_destroy(void *ctx) { (void)ctx; return -1; }

// --- libopus ---
__attribute__((weak)) const char *opus_get_version_string(void) { return NULL; }
__attribute__((weak)) void *opus_decoder_create(int32_t fs, int channels, int *err) {
	(void)fs; (void)channels; if (err) *err = -1; return NULL;
}
__attribute__((weak)) int opus_decode_float(void *st, const unsigned char *data, int32_t len, float *pcm, int frame_size, int decode_fec) {
	(void)st; (void)data; (void)len; (void)pcm; (void)frame_size; (void)decode_fec; return -1;
}
__attribute__((weak)) void opus_decoder_destroy(void *st) { (void)st; }

// --- libvorbis ---
__attribute__((weak)) const char *vorbis_version_string(void) { return NULL; }
__attribute__((weak)) void vorbis_info_init(void *vi) { (void)vi; }
__attribute__((weak)) void vorbis_info_clear(void *vi) { (void)vi; }
__attribute__((weak)) void vorbis_comment_init(void *vc) { (void)vc; }
__attribute__((weak)) void vorbis_comment_clear(void *vc) { (void)vc; }
__attribute__((weak)) int vorbis_synthesis_headerin(void *vi, void *vc, void *op) { (void)vi; (void)vc; (void)op; return -1; }
__attribute__((weak)) int vorbis_synthesis_init(void *vd, void *vi) { (void)vd; (void)vi; return -1; }
__attribute__((weak)) int vorbis_block_init(void *vd, void *vb) { (void)vd; (void)vb; return -1; }
__attribute__((weak)) int vorbis_synthesis(void *vb, void *op) { (void)vb; (void)op; return -1; }
__attribute__((weak)) int vorbis_synthesis_blockin(void *vd, void *vb) { (void)vd; (void)vb; return -1; }
__attribute__((weak)) int vorbis_synthesis_pcmout(void *vd, float ***pcm) { (void)vd; (void)pcm; return 0; }
__attribute__((weak)) int vorbis_synthesis_read(void *vd, int samples) { (void)vd; (void)samples; return -1; }
__attribute__((weak)) int vorbis_synthesis_restart(void *vd) { (void)vd; return -1; }
__attribute__((weak)) int vorbis_block_clear(void *vb) { (void)vb; return 0; }
__attribute__((weak)) void vorbis_dsp_clear(void *vd) { (void)vd; }

// --- Bridge entry points ---

int shirei_ios_has_dav1d(void) { return dav1d_version() != NULL; }
int shirei_ios_has_vpx_vp9(void) { return vpx_codec_version_str() != NULL && vpx_codec_vp9_dx() != NULL; }
int shirei_ios_has_vpx_vp8(void) { return vpx_codec_version_str() != NULL && vpx_codec_vp8_dx() != NULL; }
int shirei_ios_has_opus(void) { return opus_get_version_string() != NULL; }
int shirei_ios_has_vorbis(void) { return vorbis_version_string() != NULL; }

void shirei_ios_dav1d_default_settings(void *settings) { dav1d_default_settings(settings); }
int shirei_ios_dav1d_open(void **ctx_out, const void *settings) { return dav1d_open(ctx_out, settings); }
void shirei_ios_dav1d_close(void **ctx_out) { dav1d_close(ctx_out); }
void shirei_ios_dav1d_flush(void *ctx) { dav1d_flush(ctx); }
uint8_t *shirei_ios_dav1d_data_create(void *data, size_t sz) { return dav1d_data_create(data, sz); }
void shirei_ios_dav1d_data_unref(void *data) { dav1d_data_unref(data); }
int shirei_ios_dav1d_send_data(void *ctx, void *data) { return dav1d_send_data(ctx, data); }
int shirei_ios_dav1d_get_picture(void *ctx, void *pic) { return dav1d_get_picture(ctx, pic); }
void shirei_ios_dav1d_picture_unref(void *pic) { dav1d_picture_unref(pic); }

int shirei_ios_vpx_dec_init(void *ctx, int is_vp9, const void *cfg, int ver) {
	void *iface = is_vp9 ? vpx_codec_vp9_dx() : vpx_codec_vp8_dx();
	if (!iface) return -1;
	return vpx_codec_dec_init_ver(ctx, iface, cfg, 0, ver);
}
int shirei_ios_vpx_decode(void *ctx, const uint8_t *data, unsigned int sz) {
	return vpx_codec_decode(ctx, data, sz, NULL, 0);
}
void *shirei_ios_vpx_get_frame(void *ctx, void **iter) { return vpx_codec_get_frame(ctx, iter); }
int shirei_ios_vpx_destroy(void *ctx) { return vpx_codec_destroy(ctx); }

void *shirei_ios_opus_create(int fs, int channels, int *err_out) {
	return opus_decoder_create((int32_t)fs, channels, err_out);
}
int shirei_ios_opus_decode_float(void *st, const unsigned char *data, int len, float *pcm, int frame_size) {
	return opus_decode_float(st, data, (int32_t)len, pcm, frame_size, 0);
}
void shirei_ios_opus_destroy(void *st) { opus_decoder_destroy(st); }

void shirei_ios_vorbis_info_init(void *vi) { vorbis_info_init(vi); }
void shirei_ios_vorbis_info_clear(void *vi) { vorbis_info_clear(vi); }
void shirei_ios_vorbis_comment_init(void *vc) { vorbis_comment_init(vc); }
void shirei_ios_vorbis_comment_clear(void *vc) { vorbis_comment_clear(vc); }
typedef struct {
	unsigned char *packet;
	long bytes;
	long b_o_s;
	long e_o_s;
	int64_t granulepos;
	int64_t packetno;
} shirei_ogg_packet;

int shirei_ios_vorbis_headerin_pkt(void *vi, void *vc, const uint8_t *data, size_t len, int bos, int64_t pktno) {
	shirei_ogg_packet op = {
		.packet = (unsigned char *)data,
		.bytes = (long)len,
		.b_o_s = bos ? 1 : 0,
		.e_o_s = 0,
		.granulepos = 0,
		.packetno = pktno,
	};
	return vorbis_synthesis_headerin(vi, vc, &op);
}
int shirei_ios_vorbis_synthesis_init(void *vd, void *vi) { return vorbis_synthesis_init(vd, vi); }
int shirei_ios_vorbis_block_init(void *vd, void *vb) { return vorbis_block_init(vd, vb); }
int shirei_ios_vorbis_synthesis_pkt(void *vb, const uint8_t *data, size_t len, int64_t pktno) {
	shirei_ogg_packet op = {
		.packet = (unsigned char *)data,
		.bytes = (long)len,
		.b_o_s = 0,
		.e_o_s = 0,
		.granulepos = 0,
		.packetno = pktno,
	};
	return vorbis_synthesis(vb, &op);
}
int shirei_ios_vorbis_blockin(void *vd, void *vb) { return vorbis_synthesis_blockin(vd, vb); }
int shirei_ios_vorbis_pcmout(void *vd, float ***pcm) { return vorbis_synthesis_pcmout(vd, pcm); }
int shirei_ios_vorbis_read(void *vd, int samples) { return vorbis_synthesis_read(vd, samples); }
int shirei_ios_vorbis_restart(void *vd) { return vorbis_synthesis_restart(vd); }
int shirei_ios_vorbis_block_clear(void *vb) { return vorbis_block_clear(vb); }
void shirei_ios_vorbis_dsp_clear(void *vd) { vorbis_dsp_clear(vd); }
