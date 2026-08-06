//go:build js

package app

import (
	_ "embed"
	"fmt"
	"sync"
	"syscall/js"
	"time"
)

// Web audio: prefer AudioWorklet + SharedArrayBuffer ring (audio thread
// independent of soft-render on the main thread). Falls back to
// ScriptProcessor when the page is not cross-origin isolated.
//
// Isolation requires COOP+COEP on the document (shirei_web writes .headers;
// -run/-serve sets the same headers). Autoplay: context stays suspended until
// a user gesture — we resume on pointerdown/keydown.
//
// Buffering is adaptive: keep a short lead for snappy pads, grow after
// underruns (long soft-render frames), shrink when stable.

//go:embed audio_worklet.js
var audioWorkletJS string

const (
	// Ring capacity is headroom only; pump targets a much smaller lead.
	webAudioRingCapacity = 16384
	webAudioFillChunk    = 512
	webAudioScriptFrames = 512 // ScriptProcessor fallback quantum

	// Adaptive target (samples). At 48kHz: 32ms / 60ms / 150ms.
	webAudioTargetMin = 1536
	webAudioTargetDef = 2880
	webAudioTargetMax = 7200
)

var (
	webAudioMu sync.Mutex

	webAudioCtx    js.Value
	webAudioNode   js.Value // WorkletNode or ScriptProcessor
	webAudioResume js.Func

	// Worklet path
	webAudioSAB     js.Value
	webAudioIdx     js.Value // Int32Array length 3
	webAudioSamples js.Value // Float32Array length capacity
	webAudioCap     int
	webAudioProd    js.Func
	webAudioTimer   js.Value
	webAudioFillBuf []float32

	// Adaptive lead (samples buffered ahead of the playhead).
	webAudioTarget   int
	webAudioLastUnd  int // last seen underrun counter
	webAudioStable   int // pumps since last underrun growth

	// ScriptProcessor fallback
	webAudioOnProc js.Func
)

func audioStart(sampleRate int) error {
	acCtor := js.Global().Get("AudioContext")
	if !acCtor.Truthy() {
		acCtor = js.Global().Get("webkitAudioContext")
	}
	if !acCtor.Truthy() {
		return fmt.Errorf("Web Audio API not available")
	}

	opts := map[string]any{}
	if sampleRate > 0 {
		opts["sampleRate"] = sampleRate
	}
	ctx := acCtor.New(opts)
	if !ctx.Truthy() {
		return fmt.Errorf("AudioContext constructor failed")
	}

	webAudioMu.Lock()
	webAudioCtx = ctx
	webAudioTarget = webAudioTargetDef
	webAudioLastUnd = 0
	webAudioStable = 0
	webAudioMu.Unlock()

	installAudioResumeHandlers()
	audioLastFill.Store(0)
	resumeWebAudio()

	actual := int(ctx.Get("sampleRate").Float() + 0.5)
	if sampleRate > 0 && actual > 0 && actual != sampleRate {
		fmt.Printf("audio: Web Audio sample rate is %d (requested %d)\n", actual, sampleRate)
	}

	if canUseSharedArrayBuffer() {
		if err := startWorkletAudio(ctx); err != nil {
			fmt.Printf("audio: worklet path failed (%v); falling back to ScriptProcessor\n", err)
			return startScriptProcessorAudio(ctx)
		}
		return nil
	}
	fmt.Println("audio: page not cross-origin isolated; using ScriptProcessor (UI may glitch audio)")
	return startScriptProcessorAudio(ctx)
}

func canUseSharedArrayBuffer() bool {
	if iso := js.Global().Get("crossOriginIsolated"); iso.Truthy() && !iso.Bool() {
		return false
	}
	sab := js.Global().Get("SharedArrayBuffer")
	return sab.Truthy()
}

func installAudioResumeHandlers() {
	if webAudioResume.Truthy() {
		return
	}
	webAudioResume = js.FuncOf(func(this js.Value, args []js.Value) any {
		resumeWebAudio()
		return nil
	})
	doc := js.Global().Get("document")
	optsListen := map[string]any{"capture": true}
	doc.Call("addEventListener", "pointerdown", webAudioResume, optsListen)
	doc.Call("addEventListener", "keydown", webAudioResume, optsListen)
}

func resumeWebAudio() {
	webAudioMu.Lock()
	ctx := webAudioCtx
	webAudioMu.Unlock()
	if !ctx.Truthy() {
		return
	}
	if ctx.Get("state").String() == "suspended" {
		p := ctx.Call("resume")
		if p.Truthy() && p.Get("then").Truthy() {
			p.Call("then", js.FuncOf(func(this js.Value, args []js.Value) any {
				startAudioProducer()
				return nil
			}))
			return
		}
	}
	startAudioProducer()
}

func startWorkletAudio(ctx js.Value) error {
	// SharedArrayBuffer: 12 bytes indices + capacity float32s
	byteLen := 12 + webAudioRingCapacity*4
	sabCtor := js.Global().Get("SharedArrayBuffer")
	sab := sabCtor.New(byteLen)
	idx := js.Global().Get("Int32Array").New(sab, 0, 3)
	samples := js.Global().Get("Float32Array").New(sab, 12, webAudioRingCapacity)
	atomics := js.Global().Get("Atomics")
	atomics.Call("store", idx, 0, 0)
	atomics.Call("store", idx, 1, 0)
	atomics.Call("store", idx, 2, 0)

	blob := js.Global().Get("Blob").New(
		[]any{audioWorkletJS},
		map[string]any{"type": "application/javascript"},
	)
	url := js.Global().Get("URL").Call("createObjectURL", blob)

	aw := ctx.Get("audioWorklet")
	if !aw.Truthy() {
		return fmt.Errorf("AudioWorklet not available")
	}

	done := make(chan error, 1)
	then := js.FuncOf(func(this js.Value, args []js.Value) any {
		node := js.Global().Get("AudioWorkletNode").New(ctx, "shirei-audio", map[string]any{
			"numberOfInputs":     0,
			"numberOfOutputs":    1,
			"outputChannelCount": []any{1},
			"processorOptions": map[string]any{
				"sab":      sab,
				"capacity": webAudioRingCapacity,
			},
		})
		node.Call("connect", ctx.Get("destination"))

		webAudioMu.Lock()
		webAudioSAB = sab
		webAudioIdx = idx
		webAudioSamples = samples
		webAudioCap = webAudioRingCapacity
		webAudioNode = node
		webAudioFillBuf = make([]float32, webAudioFillChunk)
		webAudioMu.Unlock()

		startAudioProducer()
		done <- nil
		return nil
	})
	catch := js.FuncOf(func(this js.Value, args []js.Value) any {
		msg := "addModule failed"
		if len(args) > 0 {
			msg = args[0].String()
		}
		done <- fmt.Errorf("%s", msg)
		return nil
	})
	promise := aw.Call("addModule", url)
	promise.Call("then", then).Call("catch", catch)

	select {
	case err := <-done:
		js.Global().Get("URL").Call("revokeObjectURL", url)
		then.Release()
		catch.Release()
		return err
	case <-time.After(5 * time.Second):
		then.Release()
		catch.Release()
		return fmt.Errorf("worklet addModule timed out")
	}
}

func startAudioProducer() {
	webAudioMu.Lock()
	defer webAudioMu.Unlock()
	if !webAudioSamples.Truthy() || webAudioTimer.Truthy() {
		return
	}
	if !webAudioProd.Truthy() {
		webAudioProd = js.FuncOf(func(this js.Value, args []js.Value) any {
			pumpAudioRing()
			return nil
		})
	}
	// 4ms: top up often so a single long frame is less likely to empty a short lead.
	webAudioTimer = js.Global().Call("setInterval", webAudioProd, 4)
}

func pumpAudioRing() {
	webAudioMu.Lock()
	idx := webAudioIdx
	samples := webAudioSamples
	capN := webAudioCap
	buf := webAudioFillBuf
	target := webAudioTarget
	webAudioMu.Unlock()
	if !idx.Truthy() || !samples.Truthy() || capN <= 0 {
		return
	}

	atomics := js.Global().Get("Atomics")

	// Adapt target from worklet underrun counter.
	und := atomics.Call("load", idx, 2).Int()
	webAudioMu.Lock()
	if und > webAudioLastUnd {
		// Grew: long UI frame emptied the lead — ask for more headroom.
		webAudioTarget += 1024
		if webAudioTarget > webAudioTargetMax {
			webAudioTarget = webAudioTargetMax
		}
		webAudioLastUnd = und
		webAudioStable = 0
		target = webAudioTarget
	} else {
		webAudioStable++
		// After ~2s of stability, ease target down toward default.
		if webAudioStable > 500 && webAudioTarget > webAudioTargetDef {
			webAudioTarget -= 256
			if webAudioTarget < webAudioTargetDef {
				webAudioTarget = webAudioTargetDef
			}
			webAudioStable = 0
			target = webAudioTarget
		}
	}
	webAudioMu.Unlock()

	if target < webAudioTargetMin {
		target = webAudioTargetMin
	}
	if target >= capN {
		target = capN / 4
	}

	for {
		w := atomics.Call("load", idx, 0).Int()
		r := atomics.Call("load", idx, 1).Int()
		buffered := w - r
		if buffered < 0 {
			buffered += capN
		}
		if buffered >= target {
			return
		}
		free := r - w - 1
		if free < 0 {
			free += capN
		}
		need := target - buffered
		if need > free {
			need = free
		}
		if need <= 0 {
			return
		}
		n := webAudioFillChunk
		if n > need {
			n = need
		}
		if n > len(buf) {
			buf = make([]float32, n)
			webAudioMu.Lock()
			webAudioFillBuf = buf
			webAudioMu.Unlock()
		}
		chunk := buf[:n]
		audioNoteFill()
		if fill := audioFill; fill != nil {
			fill(chunk)
		} else {
			clear(chunk)
		}
		for i := 0; i < n; i++ {
			samples.SetIndex(w, chunk[i])
			w++
			if w >= capN {
				w = 0
			}
		}
		atomics.Call("store", idx, 0, w)
	}
}

func startScriptProcessorAudio(ctx js.Value) error {
	proc := ctx.Call("createScriptProcessor", webAudioScriptFrames, 0, 1)
	if !proc.Truthy() {
		return fmt.Errorf("createScriptProcessor failed")
	}
	buf := make([]float32, webAudioScriptFrames)
	webAudioOnProc = js.FuncOf(func(this js.Value, args []js.Value) any {
		audioNoteFill()
		if len(args) < 1 {
			return nil
		}
		outJS := args[0].Get("outputBuffer").Call("getChannelData", 0)
		n := outJS.Get("length").Int()
		if n <= 0 {
			return nil
		}
		if n > len(buf) {
			buf = make([]float32, n)
		}
		out := buf[:n]
		if fill := audioFill; fill != nil {
			fill(out)
		} else {
			clear(out)
		}
		for i := 0; i < n; i++ {
			outJS.SetIndex(i, out[i])
		}
		return nil
	})
	proc.Set("onaudioprocess", webAudioOnProc)
	proc.Call("connect", ctx.Get("destination"))

	webAudioMu.Lock()
	webAudioNode = proc
	webAudioMu.Unlock()
	return nil
}
