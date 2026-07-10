// AudioQueue-based mono float32 audio output. Declarations only — this
// header is included from a cgo file that uses //export, which forbids
// definitions in the preamble. Implementation in audio_darwin.c.
int shireiAudioStart(double sampleRate, int bufferFrames);
int shireiAudioRestart(void);
int shireiAudioPause(void);
