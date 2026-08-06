package main

import "testing"

func TestParseMarketingVersion(t *testing.T) {
	cases := []struct {
		in            string
		maj, min, pat int
		suf           string
		ok            bool
	}{
		{"1.0.0", 1, 0, 0, "", true},
		{"0.1.0-beta", 0, 1, 0, "-beta", true},
		{"2.3.4rc1", 2, 3, 4, "rc1", true},
		{"10.20.30", 10, 20, 30, "", true},
		{"1.0", 0, 0, 0, "", false},
		{"v1.0.0", 0, 0, 0, "", false},
		{"", 0, 0, 0, "", false},
	}
	for _, c := range cases {
		v, err := parseMarketingVersion(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("parse %q: %v", c.in, err)
				continue
			}
			if v.Major != c.maj || v.Minor != c.min || v.Patch != c.pat || v.Suffix != c.suf {
				t.Errorf("parse %q = %+v, want %d.%d.%d%q", c.in, v, c.maj, c.min, c.pat, c.suf)
			}
		} else if err == nil {
			t.Errorf("parse %q: want error, got %+v", c.in, v)
		}
	}
}

func TestCompareMarketingVersion(t *testing.T) {
	// pairs: a, b, want compare(a,b)
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "0.9.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0", "1.0.0-beta", 1}, // plain release > prerelease
		{"1.0.0-beta", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"0.1.0", "0.1.0", 0},
	}
	for _, c := range cases {
		got := compareMarketingVersion(c.a, c.b)
		if got != c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionOKForRelease(t *testing.T) {
	if err := versionOKForRelease("1.0.0", ""); err != nil {
		t.Fatal(err)
	}
	// Same or lower marketing version is allowed (build numbers carry uniqueness).
	if err := versionOKForRelease("1.0.0", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := versionOKForRelease("0.9.0", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := versionOKForRelease("nope", ""); err == nil {
		t.Fatal("expected invalid")
	}
}

func TestBumpPatchVersion(t *testing.T) {
	if g := bumpPatchVersion(""); g != "0.1.0" {
		t.Fatalf("empty: %q", g)
	}
	if g := bumpPatchVersion("1.2.3"); g != "1.2.4" {
		t.Fatalf("1.2.3: %q", g)
	}
	if g := bumpPatchVersion("0.1.0-beta"); g != "0.1.1" {
		t.Fatalf("beta: %q", g)
	}
}
