// Command shirei_web builds a shirei app for GOOS=js/GOARCH=wasm into a static
// site directory (index.html, wasm_exec.js, main.wasm, .headers, embed.js).
//
// Default — build only:
//
//	shirei_web -o dist ./demos/todo
//
// Build and serve (local preview):
//
//	shirei_web -serve -o dist ./demos/todo
//	shirei_web -run ./demos/todo          # temp dir, open browser
//
// Gallery — build a named set of web demos into -o/<slug>/:
//
//	shirei_web -gallery=custom-widgets -o ../static-sites/judi.systems/shirei/custom-widgets/apps
//	shirei_web -gallery=demos -o ../static-sites/judi.systems/shirei/demos/apps
//	shirei_web -gallery=demos -run -o play
//
// package defaults to ".". -o is required for a plain build; with -run/-serve
// it defaults to a temp directory that is removed on exit when omitted.
//
// Demo builds emit .headers (COOP+COEP) for SharedArrayBuffer audio. The gallery
// index page does not use isolation headers so it can embed those demos.
package main

import (
	_ "embed"
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed wasm_exec.js
var wasmExec []byte

//go:embed index.html
var indexHTMLTemplate string

//go:embed embed.js
var embedJS []byte

// pageMeta fills index.html: document title and optional "Source: path" chrome.
// Zero value: bare page titled "shirei" (local -run / plain -o builds).
type pageMeta struct {
	Title       string // <title> only (demos already show their own heading)
	SourceURL   string
	SourceLabel string // link text, e.g. demos/kanban
}

func (m pageMeta) HasChrome() bool {
	return m.SourceURL != "" && m.SourceLabel != ""
}

func renderIndexHTML(meta pageMeta) ([]byte, error) {
	if meta.Title == "" {
		meta.Title = "shirei"
	}
	t, err := template.New("index").Parse(indexHTMLTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, meta); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sproutsDotHeaders isolates the demo document for SharedArrayBuffer (Worklet audio).
// Consumed by sprouts when present; shirei_web -run/-serve applies the same headers.
const sproutsDotHeaders = `# shirei_web — cross-origin isolation for SharedArrayBuffer audio
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Embedder-Policy: require-corp
`

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "listen address (with -run/-serve)")
	noOpen := flag.Bool("n", false, "do not open a browser (with -run/-serve)")
	serve := flag.Bool("serve", false, "after build, serve the output directory over HTTP")
	run := flag.Bool("run", false, "same as -serve, and open a browser unless -n")
	gallery := flag.String("gallery", "", "build gallery set into -o: custom-widgets | demos")
	demosRoot := flag.String("demos", "", "demos directory for -gallery (default: ./demos next to module)")
	outDir := flag.String("o", "", "output directory (required unless -run/-serve)")
	flag.Parse()

	pkg := "."
	if flag.NArg() > 0 {
		pkg = flag.Arg(0)
	}

	wantServe := *serve || *run
	// -run opens a browser; -serve only hosts. -n suppresses the open.
	openBrowserFlag := *run && !*noOpen
	gallerySet := strings.TrimSpace(*gallery)

	if gallerySet == "" && !wantServe && *outDir == "" {
		fatal(fmt.Errorf("build requires -o <dir> (or pass -run/-serve for a temp dir)"))
	}
	if gallerySet != "" && *outDir == "" && !wantServe {
		fatal(fmt.Errorf("-gallery requires -o <dir> (or -run/-serve)"))
	}
	if gallerySet != "" {
		if _, ok := gallerySets[gallerySet]; !ok {
			fatal(fmt.Errorf("unknown -gallery %q (want %s)", gallerySet, knownGalleryNames()))
		}
	}

	dir := *outDir
	var err error
	cleanup := false
	if dir == "" {
		dir, err = os.MkdirTemp("", "shirei-web-*")
		if err != nil {
			fatal(err)
		}
		cleanup = true
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(err)
	}
	if cleanup {
		defer os.RemoveAll(dir)
	}

	if gallerySet != "" {
		root := *demosRoot
		if root == "" {
			root = findDemosRoot()
		}
		if err := buildGallery(gallerySet, dir, root); err != nil {
			fatal(err)
		}
	} else {
		if err := buildStatic(pkg, dir, pageMeta{}); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "shirei_web: wrote static site to %s\n", dir)
		fmt.Fprintf(os.Stderr, "  %s\n  %s\n  %s\n  %s\n  %s\n",
			filepath.Join(dir, "index.html"),
			filepath.Join(dir, "wasm_exec.js"),
			filepath.Join(dir, "main.wasm"),
			filepath.Join(dir, ".headers"),
			filepath.Join(dir, "embed.js"))
	}

	if !wantServe {
		return
	}

	if gallerySet != "" {
		serveGallery(dir, *addr, openBrowserFlag)
		return
	}

	wasmPath := filepath.Join(dir, "main.wasm")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Match emitted .headers so -run/-serve is cross-origin isolated.
		setIsolationHeaders(w)
		switch r.URL.Path {
		case "/", "/index.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			html, err := renderIndexHTML(pageMeta{})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_, _ = w.Write(html)
		case "/wasm_exec.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(wasmExec)
		case "/embed.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(embedJS)
		case "/main.wasm":
			w.Header().Set("Content-Type", "application/wasm")
			http.ServeFile(w, r, wasmPath)
		default:
			http.NotFound(w, r)
		}
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fatal(err)
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Fprintf(os.Stderr, "shirei_web: serving %s (COOP+COEP)\n", url)
	if openBrowserFlag {
		go openBrowser(url)
	}
	fatal(http.Serve(ln, mux))
}

// findDemosRoot resolves ./demos relative to the shirei module (cwd or parent).
func findDemosRoot() string {
	candidates := []string{
		"demos",
		filepath.Join("shirei", "demos"),
	}
	if exe, err := os.Executable(); err == nil {
		// not reliable for go run; skip
		_ = exe
	}
	wd, _ := os.Getwd()
	for _, c := range candidates {
		p := filepath.Join(wd, c)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	// Walk up looking for demos/next to go.mod module shirei
	dir := wd
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, "demos")
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "demos"
}

func serveGallery(dir, addr string, openBrowserFlag bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Isolation only for demo sub-apps (their own .headers on sprouts).
		// Local FileServer: apply COOP+COEP for any path under a demo folder
		// that has main.wasm so Worklet audio works inside iframes.
		p := r.URL.Path
		if p != "/" && p != "/index.html" && p != "/embed.js" {
			setIsolationHeaders(w)
		}
		// Correct MIME for wasm
		if strings.HasSuffix(p, ".wasm") {
			w.Header().Set("Content-Type", "application/wasm")
		}
		http.FileServer(http.Dir(dir)).ServeHTTP(w, r)
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatal(err)
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Fprintf(os.Stderr, "shirei_web: serving gallery %s\n", url)
	if openBrowserFlag {
		go openBrowser(url)
	}
	fatal(http.Serve(ln, mux))
}

func setIsolationHeaders(w http.ResponseWriter) {
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
}

// buildStatic compiles pkg to dir/main.wasm and writes index.html, wasm_exec.js,
// and sprouts .headers (COOP+COEP). meta fills optional page chrome (gallery).
func buildStatic(pkg, dir string, meta pageMeta) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	wasmPath := filepath.Join(dir, "main.wasm")
	fmt.Fprintf(os.Stderr, "shirei_web: building %s → %s\n", pkg, wasmPath)
	cmd := exec.Command("go", "build", "-o", wasmPath, pkg)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wasm_exec.js"), wasmExec, 0o644); err != nil {
		return err
	}
	html, err := renderIndexHTML(meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), html, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ".headers"), []byte(sproutsDotHeaders), 0o644); err != nil {
		return err
	}
	// Parent-page helper: sizes iframes from SetupWindow via postMessage.
	if err := os.WriteFile(filepath.Join(dir, "embed.js"), embedJS, 0o644); err != nil {
		return err
	}
	return nil
}

func openBrowser(url string) {
	time.Sleep(200 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "shirei_web:", err)
	os.Exit(1)
}
