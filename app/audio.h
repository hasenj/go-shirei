// AudioQueue-based float32 audio output (iOS, mono or stereo). Declarations
// only — this header is included from a cgo file that uses //export, which
// forbids definitions in the preamble. Implementation in audio_ios.m. macOS
// audio is purego (audio_darwin.go) and does not use this header.
int shireiAudioStart(double sampleRate, int bufferFrames);
int shireiAudioStartCh(double sampleRate, int channels, int bufferFrames);
int shireiAudioRestart(void);
int shireiAudioPause(void);
