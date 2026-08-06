// AudioWorklet: read mono float32 from a SharedArrayBuffer ring.
// Layout (Int32):
//   [0] writePos  — next write index (main / Go)
//   [1] readPos   — next read index (this processor)
//   [2] underruns — incremented when we must output silence
// then Float32[capacity] samples at byte offset 12.
// Main thread produces; this processor consumes on the audio thread.

class ShireiAudioProcessor extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const opts = options.processorOptions || {};
    this.cap = opts.capacity | 0;
    const sab = opts.sab;
    this.idx = new Int32Array(sab, 0, 3);
    this.samples = new Float32Array(sab, 12, this.cap);
  }

  process(_inputs, outputs) {
    const out = outputs[0] && outputs[0][0];
    if (!out) return true;
    const cap = this.cap;
    let r = Atomics.load(this.idx, 1);
    const w = Atomics.load(this.idx, 0);
    const samples = this.samples;
    let starved = false;
    for (let i = 0; i < out.length; i++) {
      if (r === w) {
        out[i] = 0;
        starved = true;
        continue;
      }
      out[i] = samples[r];
      r++;
      if (r >= cap) r = 0;
    }
    Atomics.store(this.idx, 1, r);
    if (starved) {
      Atomics.add(this.idx, 2, 1);
    }
    return true;
  }
}

registerProcessor("shirei-audio", ShireiAudioProcessor);
