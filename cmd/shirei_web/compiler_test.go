package main

import (
	"bytes"
	"testing"
)

func TestPatchTinyGoWasmExecSeedsValueTable(t *testing.T) {
	src := []byte(`this._values = [NaN, 0, null, true, false, globalThis, this];
this._goRefCounts = []; // number of references that Go has to a JS value
this._ids = new Map();  // mapping from JS values to reference ids
`)
	out, err := patchTinyGoWasmExec(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[globalThis, 5n]",
		"[this, 6n]",
		"this._goRefCounts = new Array(this._values.length).fill(Infinity);",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("patched wasm_exec.js is missing %q:\n%s", want, out)
		}
	}
}

func TestPatchTinyGoWasmExecRejectsUnknownRuntime(t *testing.T) {
	src := []byte("this._values = [];\n")
	if _, err := patchTinyGoWasmExec(src); err == nil {
		t.Fatal("expected an error for an unrecognized wasm_exec.js")
	}
}
