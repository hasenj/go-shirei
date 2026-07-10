// see_exe: see what's inside a built Go executable — which modules got
// linked in (with versions), and how much code each one contributes.
//
// All data comes from the binary itself, using only the standard library:
//   - debug/buildinfo: the module manifest the linker embeds in every Go binary
//   - debug/gosym + debug/macho|elf: per-function sizes from the pclntab
//
// No subprocess, no go toolchain required at runtime, no external packages.
package main

import (
	"debug/buildinfo"
	"debug/elf"
	"debug/gosym"
	"debug/macho"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Module struct {
	Path     string
	Version  string
	CodeSize uint64 // text bytes attributed to this module's functions
	NumFuncs int
}

type ExeInfo struct {
	FilePath  string
	FileSize  uint64
	GoVersion string
	Main      *Module   // the main module (version is usually "(devel)")
	Deps      []*Module // sorted by CodeSize descending
	Stdlib    Module    // runtime + standard library, one bucket
	Unknown   Module    // functions we could not attribute (should be ~empty)
}

// AttributedTotal is the sum of all function text bytes; the remainder of
// FileSize is data, type metadata, strings, and debug info (not attributed yet).
func (e *ExeInfo) AttributedTotal() uint64 {
	total := e.Main.CodeSize + e.Stdlib.CodeSize + e.Unknown.CodeSize
	for _, m := range e.Deps {
		total += m.CodeSize
	}
	return total
}

func LoadExe(path string) (*ExeInfo, error) {
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("not a Go binary? %w", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	info := &ExeInfo{
		FilePath:  path,
		FileSize:  uint64(st.Size()),
		GoVersion: bi.GoVersion,
		Main:      &Module{Path: bi.Main.Path, Version: bi.Main.Version},
		Stdlib:    Module{Path: "(go runtime + stdlib)"},
		Unknown:   Module{Path: "(unattributed)"},
	}

	mods := []*Module{info.Main}
	for _, d := range bi.Deps {
		m := &Module{Path: d.Path, Version: d.Version}
		info.Deps = append(info.Deps, m)
		mods = append(mods, m)
	}
	prefixes := modPrefixes(mods)

	tab, err := readPclntab(path)
	if err != nil {
		return nil, err
	}

	for i := range tab.Funcs {
		fn := &tab.Funcs[i]
		size := fn.End - fn.Entry
		m := attribute(fn.Name, info, prefixes)
		m.CodeSize += size
		m.NumFuncs++
	}

	sort.Slice(info.Deps, func(i, j int) bool {
		return info.Deps[i].CodeSize > info.Deps[j].CodeSize
	})
	return info, nil
}

type modPrefix struct {
	prefix string // one spelling of a module path as it appears in symbols
	mod    *Module
}

// modPrefixes builds the symbol-name prefixes for attribution, longest first
// so subpath modules (cloud.google.com/go/auth) win over their prefix
// (cloud.google.com/go). The linker escapes dots only in the LAST element of
// a package path, so a module like yomitai.app appears under two spellings:
// escaped in its root package's symbols ("yomitai%2eapp.Fn") and raw in its
// subpackages' ("yomitai.app/cfg.Fn") — each module contributes both.
func modPrefixes(mods []*Module) []modPrefix {
	out := make([]modPrefix, 0, len(mods)*2)
	for _, m := range mods {
		esc := escapePath(m.Path)
		out = append(out, modPrefix{esc, m})
		if esc != m.Path {
			out = append(out, modPrefix{m.Path, m})
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i].prefix) > len(out[j].prefix) })
	return out
}

// escapePath mirrors the linker's symbol naming (cmd/internal/objabi.PathToPrefix):
// dots and percents in the final element of a package path are %-escaped, so
// the module yomitai.app appears in symbols as "yomitai%2eapp.Func".
func escapePath(p string) string {
	i := strings.LastIndexByte(p, '/') + 1
	last := strings.ReplaceAll(p[i:], "%", "%25")
	last = strings.ReplaceAll(last, ".", "%2e")
	return p[:i] + last
}

// attribute maps a full symbol name (e.g. "yomitai%2eapp/cfg.(*T).Method") to
// the module that owns it. We match escaped module paths against the raw
// symbol name; gosym's PackageName() is unusable here because it truncates
// at the first dot.
func attribute(sym string, info *ExeInfo, prefixes []modPrefix) *Module {
	// Package main belongs to the main module.
	if strings.HasPrefix(sym, "main.") {
		return info.Main
	}
	// Compiler-generated helpers (type equality funcs, wrappers): count them
	// as runtime overhead rather than charging them to a module.
	if strings.HasPrefix(sym, "type:") || strings.HasPrefix(sym, "go:") {
		return &info.Stdlib
	}
	for _, p := range prefixes {
		if len(sym) > len(p.prefix) && strings.HasPrefix(sym, p.prefix) {
			// Module root package ("path.Fn") or a subpackage ("path/pkg.Fn").
			if c := sym[len(p.prefix)]; c == '.' || c == '/' {
				return p.mod
			}
		}
	}
	// Stdlib import paths have no dot before the first slash ("net/http.Get"),
	// unlike module paths ("github.com/..."). No-slash leftovers are runtime
	// packages and pseudo-symbols ("runtime.main", "type:.eq.*"). Generic
	// instantiations embed type args in brackets ("slices.SortFunc[go.shape...]")
	// whose paths must not confuse the check — the package part ends at '['.
	if i := strings.IndexByte(sym, '['); i >= 0 {
		sym = sym[:i]
	}
	slash := strings.IndexByte(sym, '/')
	dot := strings.IndexByte(sym, '.')
	if slash == -1 || dot == -1 || dot > slash {
		return &info.Stdlib
	}
	return &info.Unknown
}

// readPclntab locates the Go symbol table in a Mach-O or ELF executable.
// (PE needs symbol-based lookup instead of a named section; not done yet.)
func readPclntab(path string) (*gosym.Table, error) {
	if f, err := macho.Open(path); err == nil {
		defer f.Close()
		pcln := f.Section("__gopclntab")
		text := f.Section("__text")
		if pcln == nil || text == nil {
			return nil, fmt.Errorf("no __gopclntab/__text section in %s", path)
		}
		data, err := pcln.Data()
		if err != nil {
			return nil, err
		}
		return gosym.NewTable(nil, gosym.NewLineTable(data, text.Addr))
	}
	if f, err := elf.Open(path); err == nil {
		defer f.Close()
		pcln := f.Section(".gopclntab")
		text := f.Section(".text")
		if pcln == nil || text == nil {
			return nil, fmt.Errorf("no .gopclntab/.text section in %s", path)
		}
		data, err := pcln.Data()
		if err != nil {
			return nil, err
		}
		return gosym.NewTable(nil, gosym.NewLineTable(data, text.Addr))
	}
	return nil, fmt.Errorf("unsupported executable format (not Mach-O or ELF): %s", path)
}

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 0:
		RunGUI("") // file picker: every Go binary under the current directory
	case args[0] == "--help" || args[0] == "-h":
		fmt.Fprintln(os.Stderr, "usage: see_exe                            pick from Go binaries under the current directory")
		fmt.Fprintln(os.Stderr, "       see_exe <go-executable>            inspect one binary")
		fmt.Fprintln(os.Stderr, "       see_exe <go-executable> --text     print the report to stdout")
		fmt.Fprintln(os.Stderr, "       see_exe <go-executable> <module>   why is that module embedded (path or substring)")
		fmt.Fprintln(os.Stderr, "       see_exe [go-executable] --png <out.png> [module]   render one GUI frame headlessly")
		os.Exit(1)
	case args[0] == "--png" && len(args) == 2:
		// picker screenshot: scan the current directory, render the browse view
		browsing = true
		scanRoot, _ = os.Getwd()
		scanBinaries()
		if err := renderPNG(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
	case len(args) == 1:
		if err := loadModel(args[0]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		RunGUI(args[0])
	case args[1] == "--png" && len(args) >= 3:
		if err := loadModel(args[0]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if len(args) >= 4 { // optional module query: render with it selected
			if m := findModule(model.info, args[3]); m != nil {
				selectedPath = m.Path
			}
		}
		if err := renderPNG(args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
	default:
		info, err := LoadExe(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if args[1] == "--text" {
			printReport(info)
		} else {
			Why(info, args[1])
		}
	}
}

func printReport(info *ExeInfo) {
	fmt.Printf("%s  (%.1f MB, %s)\n", info.FilePath, mb(info.FileSize), info.GoVersion)
	fmt.Printf("main module: %s %s\n", info.Main.Path, info.Main.Version)
	fmt.Printf("dep modules: %d\n", len(info.Deps))
	fmt.Printf("code attributed: %.1f MB of %.1f MB file\n\n", mb(info.AttributedTotal()), mb(info.FileSize))

	requiredBy, _, noModFile := depEdges(info)

	printRow := func(m *Module, note string) {
		fmt.Printf("%8.0f KB %6d fn  %s %s%s\n", float64(m.CodeSize)/1e3, m.NumFuncs, m.Path, m.Version, note)
	}
	printRow(&info.Stdlib, "")
	printRow(info.Main, "")
	for _, m := range info.Deps {
		if m.CodeSize == 0 {
			continue
		}
		chain := shortestChain(m.Path, requiredBy)
		if len(chain) == 1 {
			printRow(m, "  (direct)")
		} else {
			printRow(m, "")
			fmt.Printf("%22s via %s\n", "", strings.Join(chain[:len(chain)-1], " → "))
		}
	}
	zero := 0
	for _, m := range info.Deps {
		if m.CodeSize == 0 {
			zero++
		}
	}
	if zero > 0 {
		fmt.Printf("\n(%d more modules contributed no code — types/constants only, or fully dead-code-eliminated)\n", zero)
	}
	if info.Unknown.CodeSize > 0 {
		printRow(&info.Unknown, "")
	}
	fmt.Printf("\n(direct) = nothing in the module cache requires it; a direct dependency of the main module, inferred\n")
	if len(noModFile) > 0 {
		fmt.Printf("requires of %d local/replaced module(s) unknown: %s\n", len(noModFile), strings.Join(noModFile, ", "))
	}
}

func mb(n uint64) float64 { return float64(n) / 1e6 }
