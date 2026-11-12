package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	webCompilerAuto   = "auto"
	webCompilerGo     = "go"
	webCompilerTinyGo = "tinygo"
)

func validWebCompiler(v string) bool {
	switch v {
	case webCompilerAuto, webCompilerGo, webCompilerTinyGo:
		return true
	}
	return false
}

// effectiveWebCompiler resolves auto mode. -run/-serve is the edit/preview
// loop, where TinyGo's longer builds are usually not worth it; auto uses Go
// there unless TinyGo is requested explicitly.
func effectiveWebCompiler(mode string, serving bool) string {
	if mode == webCompilerAuto && serving {
		return webCompilerGo
	}
	return mode
}

// compileWasm builds pkg to out and returns the compiler used and the
// wasm_exec.js that matches the generated module. In auto mode TinyGo is
// tried first; any failure (missing tools, unsupported code, changed runtime
// file) falls back to Go.
func compileWasm(pkg, out, mode string) (compiler string, wasmExec []byte, err error) {
	switch mode {
	case webCompilerGo:
		if err := buildGoWasm(pkg, out); err != nil {
			return "", nil, err
		}
		return webCompilerGo, goWasmExec, nil
	case webCompilerTinyGo:
		execJS, err := buildTinyGoWasm(pkg, out)
		if err != nil {
			return "", nil, err
		}
		return webCompilerTinyGo, execJS, nil
	case webCompilerAuto:
		execJS, tinyErr := buildTinyGoWasm(pkg, out)
		if tinyErr == nil {
			return webCompilerTinyGo, execJS, nil
		}
		fmt.Fprintf(os.Stderr, "shirei_web: tinygo unavailable or failed (%v); falling back to go\n", tinyErr)
		if err := buildGoWasm(pkg, out); err != nil {
			return "", nil, err
		}
		return webCompilerGo, goWasmExec, nil
	default:
		return "", nil, fmt.Errorf("unknown web compiler %q", mode)
	}
}

func buildGoWasm(pkg, out string) error {
	fmt.Fprintf(os.Stderr, "shirei_web: compiler=go building %s → %s\n", pkg, out)
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}

func buildTinyGoWasm(pkg, out string) ([]byte, error) {
	tinygoPath, err := exec.LookPath("tinygo")
	if err != nil {
		return nil, fmt.Errorf("tinygo not found in PATH")
	}
	if err := checkWasmOpt(); err != nil {
		return nil, err
	}
	root, err := tinyGoRoot(tinygoPath)
	if err != nil {
		return nil, err
	}
	execPath := filepath.Join(root, "targets", "wasm_exec.js")
	execJS, err := os.ReadFile(execPath)
	if err != nil {
		return nil, fmt.Errorf("read tinygo wasm_exec.js: %w", err)
	}
	execJS, err = patchTinyGoWasmExec(execJS)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "shirei_web: compiler=tinygo building %s → %s\n", pkg, out)
	tmp := out + ".tmp"
	_ = os.Remove(tmp)
	defer os.Remove(tmp)
	cmd := exec.Command(tinygoPath, "build", "-target=wasm", "-no-debug", "-o", tmp, pkg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tinygo build: %w", err)
	}
	if err := os.Rename(tmp, out); err != nil {
		return nil, fmt.Errorf("rename tinygo output: %w", err)
	}
	return execJS, nil
}

func checkWasmOpt() error {
	if p := strings.TrimSpace(os.Getenv("WASMOPT")); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return nil
		}
		return fmt.Errorf("WASMOPT=%q not found", p)
	}
	if _, err := exec.LookPath("wasm-opt"); err == nil {
		return nil
	}
	return fmt.Errorf("wasm-opt not found (install binaryen or set WASMOPT)")
}

func tinyGoRoot(tinygoPath string) (string, error) {
	out, err := exec.Command(tinygoPath, "env", "TINYGOROOT").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tinygo env TINYGOROOT: %w: %s", err, strings.TrimSpace(string(out)))
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("tinygo env TINYGOROOT returned an empty path")
	}
	return root, nil
}

// patchTinyGoWasmExec seeds TinyGo's JS value table the way Go's wasm_exec.js
// does. Without the seed, globalThis (and window) are not deduplicated, so
// js.Value.Equal(parent, window) is false even when parent === window. Shirei's
// isEmbeddedFrame relies on that identity check for the window shell layout.
//
// The second replacement is part of the same fix: the seeded ids need ref
// counts, otherwise TinyGo's boxValue mixes an uninitialized count with a
// BigInt id.
func patchTinyGoWasmExec(src []byte) ([]byte, error) {
	const idsOld = "this._ids = new Map();"
	const refCountsOld = "this._goRefCounts = [];"
	if bytes.Count(src, []byte(idsOld)) != 1 {
		return nil, fmt.Errorf("unrecognized TinyGo wasm_exec.js: expected one %q", idsOld)
	}
	if bytes.Count(src, []byte(refCountsOld)) != 1 {
		return nil, fmt.Errorf("unrecognized TinyGo wasm_exec.js: expected one %q", refCountsOld)
	}
	const idsNew = `this._ids = new Map([ // mapping from JS values to reference ids
				[0, 1n],
				[null, 2n],
				[true, 3n],
				[false, 4n],
				[globalThis, 5n],
				[this, 6n],
			]);`
	const refCountsNew = "this._goRefCounts = new Array(this._values.length).fill(Infinity);"
	out := bytes.Replace(src, []byte(idsOld), []byte(idsNew), 1)
	out = bytes.Replace(out, []byte(refCountsOld), []byte(refCountsNew), 1)
	return out, nil
}
