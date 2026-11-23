// C ABI bridge between Go and optional statically linked BSD codecs on iOS
// (libdav1d, libvpx, libopus, libvorbis). Uses weak default stubs so iOS builds
// succeed without external headers or codec archives; linking the static
// archives overrides the stubs automatically.
#ifndef SHIREI_CODEC_IOS_H
#define SHIREI_CODEC_IOS_H

#include <stddef.h>
#include <stdint.h>

// Availability probes (non-zero when the real static archive is linked).
int shirei_ios_has_dav1d(void);
int shirei_ios_has_vpx_vp9(void);
int shirei_ios_has_vpx_vp8(void);
int shirei_ios_has_opus(void);
int shirei_ios_has_vorbis(void);

// dav1d
void shirei_ios_dav1d_default_settings(void *settings);
int shirei_ios_dav1d_open(void **ctx_out, const void *settings);
void shirei_ios_dav1d_close(void **ctx_out);
void shirei_ios_dav1d_flush(void *ctx);
uint8_t *shirei_ios_dav1d_data_create(void *data, size_t sz);
void shirei_ios_dav1d_data_unref(void *data);
int shirei_ios_dav1d_send_data(void *ctx, void *data);
int shirei_ios_dav1d_get_picture(void *ctx, void *pic);
void shirei_ios_dav1d_picture_unref(void *pic);

// libvpx
int shirei_ios_vpx_dec_init(void *ctx, int is_vp9, const void *cfg, int ver);
int shirei_ios_vpx_decode(void *ctx, const uint8_t *data, unsigned int sz);
void *shirei_ios_vpx_get_frame(void *ctx, void **iter);
int shirei_ios_vpx_destroy(void *ctx);

// libopus
void *shirei_ios_opus_create(int fs, int channels, int *err_out);
int shirei_ios_opus_decode_float(void *st, const unsigned char *data, int len, float *pcm, int frame_size);
void shirei_ios_opus_destroy(void *st);

// libvorbis
void shirei_ios_vorbis_info_init(void *vi);
void shirei_ios_vorbis_info_clear(void *vi);
void shirei_ios_vorbis_comment_init(void *vc);
void shirei_ios_vorbis_comment_clear(void *vc);
int shirei_ios_vorbis_headerin_pkt(void *vi, void *vc, const uint8_t *data, size_t len, int bos, int64_t pktno);
int shirei_ios_vorbis_synthesis_init(void *vd, void *vi);
int shirei_ios_vorbis_block_init(void *vd, void *vb);
int shirei_ios_vorbis_synthesis_pkt(void *vb, const uint8_t *data, size_t len, int64_t pktno);
int shirei_ios_vorbis_blockin(void *vd, void *vb);
int shirei_ios_vorbis_pcmout(void *vd, float ***pcm);
int shirei_ios_vorbis_read(void *vd, int samples);
int shirei_ios_vorbis_restart(void *vd);
int shirei_ios_vorbis_block_clear(void *vb);
void shirei_ios_vorbis_dsp_clear(void *vd);

#endif
