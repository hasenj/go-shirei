package main

import (
	"reflect"
	"testing"
)

// The attribution rules were each discovered empirically from real binaries,
// and each silently mis-bucketed real code before being fixed. These cases
// pin them down: linker %2e escaping (root package vs literal-dot
// subpackage), compiler pseudo-symbols, generic instantiations embedding
// other packages' paths in their type args, and nested-module precedence.
func TestAttribute(t *testing.T) {
	info := &ExeInfo{
		Main:    &Module{Path: "yomitai.app"},
		Stdlib:  Module{Path: "(go runtime + stdlib)"},
		Unknown: Module{Path: "(unattributed)"},
	}
	for _, p := range []string{
		"github.com/google/s2a-go",
		"cloud.google.com/go",
		"cloud.google.com/go/auth",
		"gopkg.in/gomail.v2",
		"go.hasen.dev/generic",
		"shinjikai.app",
	} {
		info.Deps = append(info.Deps, &Module{Path: p})
	}
	prefixes := modPrefixes(append([]*Module{info.Main}, info.Deps...))

	cases := []struct{ sym, want string }{
		// package main belongs to the main module
		{"main.main", "yomitai.app"},
		{"main.(*Server).run", "yomitai.app"},
		// module root package: dot in the last path element is %2e-escaped
		{"yomitai%2eapp.AdminViewDB", "yomitai.app"},
		{"shinjikai%2eapp.Foo", "shinjikai.app"},
		{"gopkg.in/gomail%2ev2.NewMessage", "gopkg.in/gomail.v2"},
		// subpackages of the same modules keep the dot literal
		{"yomitai.app/cfg.init", "yomitai.app"},
		{"shinjikai.app/kagome.Analyze", "shinjikai.app"},
		// plain module paths, root package and subpackage
		{"github.com/google/s2a-go.NewClient", "github.com/google/s2a-go"},
		{"cloud.google.com/go.Version", "cloud.google.com/go"},
		// the longest matching module wins, not its path prefix
		{"cloud.google.com/go/auth/internal.Fetch", "cloud.google.com/go/auth"},
		// a generic func belongs to its defining module even when instantiated
		// with another module's types
		{"go.hasen.dev/generic.(*SyncMap[string,*yomitai%2eapp.blockAnalysisJob]).Get", "go.hasen.dev/generic"},
		// stdlib: no dot before the first slash, or no slash at all
		{"net/http.(*Client).Do", "(go runtime + stdlib)"},
		{"runtime.main", "(go runtime + stdlib)"},
		// stdlib generics whose type args embed slash-y package paths
		{"slices.BinarySearchFunc[go.shape.[]archive/zip.fileListEntry,go.shape.struct{}]", "(go runtime + stdlib)"},
		{"unique.(*Handle[net/netip.addrDetail]).Value", "(go runtime + stdlib)"},
		// compiler-generated helpers count as runtime overhead
		{"type:.eq.debug/macho.Section", "(go runtime + stdlib)"},
		{"go:buildid", "(go runtime + stdlib)"},
		// a domain-shaped path matching no module stays unattributed
		{"some.random.domain/pkg.Fn", "(unattributed)"},
	}
	for _, c := range cases {
		if got := attribute(c.sym, info, prefixes); got.Path != c.want {
			t.Errorf("attribute(%q) = %q, want %q", c.sym, got.Path, c.want)
		}
	}
}

func TestEscapePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"yomitai.app", "yomitai%2eapp"},
		{"gopkg.in/yaml.v2", "gopkg.in/yaml%2ev2"},
		{"github.com/google/s2a-go", "github.com/google/s2a-go"},
		{"golang.org/x/net", "golang.org/x/net"},
		// percent escapes first, so a literal % doesn't double-escape the %2e
		{"a.b/c%d.e", "a.b/c%25d%2ee"},
	}
	for _, c := range cases {
		if got := escapePath(c.in); got != c.want {
			t.Errorf("escapePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseRequires(t *testing.T) {
	gomod := `module example.com/m

go 1.21

require (
	github.com/direct/dep v1.0.0
	github.com/indirect/dep v1.1.0 // indirect
	// a comment line
)

require github.com/single/dep v2.0.0
require github.com/single/indirect v2.1.0 // indirect
`
	want := []string{"github.com/direct/dep", "github.com/single/dep"}
	if got := parseRequires(gomod); !reflect.DeepEqual(got, want) {
		t.Errorf("parseRequires = %v, want %v", got, want)
	}
}

func TestShortVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.36.11", "v1.36.11"},
		{"(devel)", "(devel)"},
		{"v0.0.0-20220702185126-0057e87d1070", "v0.0.0-20220…e87d1070"},
		{"v0.0.0-20250822104345-0c6d2a3dc559+dirty", "v0.0.0-20250…59+dirty"},
	}
	for _, c := range cases {
		if got := shortVersion(c.in); got != c.want {
			t.Errorf("shortVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortestChain(t *testing.T) {
	// d is required by b and c; b by a; c by b. a is a root.
	// shortest root→target: a → b → d.
	requiredBy := map[string][]string{
		"d": {"c", "b"},
		"c": {"b"},
		"b": {"a"},
	}
	if got, want := shortestChain("d", requiredBy), []string{"a", "b", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("shortestChain(d) = %v, want %v", got, want)
	}
	// a root is its own chain
	if got, want := shortestChain("a", requiredBy), []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("shortestChain(a) = %v, want %v", got, want)
	}
	// a cycle with no root degrades to just the target
	cyc := map[string][]string{"x": {"y"}, "y": {"x"}}
	if got, want := shortestChain("x", cyc), []string{"x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("shortestChain(x, cycle) = %v, want %v", got, want)
	}
}
