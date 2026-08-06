package main

import (
	"fmt"
	"strings"
	"unicode"
)

// Marketing versions are {num}.{num}.{num}{optional arbitrary suffix}, e.g.
// "1.0.0", "0.1.0-beta", "2.3.4rc1". Ordering is numeric on the three
// components, then byte-wise on the suffix (empty suffix sorts before any
// non-empty suffix).

type marketingVersion struct {
	Major, Minor, Patch int
	Suffix              string
}

func parseMarketingVersion(s string) (marketingVersion, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return marketingVersion{}, fmt.Errorf("empty version")
	}
	major, rest, ok := readVersionInt(s)
	if !ok {
		return marketingVersion{}, fmt.Errorf("version must start with a number: %q", s)
	}
	if !strings.HasPrefix(rest, ".") {
		return marketingVersion{}, fmt.Errorf("expected major.minor.patch: %q", s)
	}
	minor, rest, ok := readVersionInt(rest[1:])
	if !ok {
		return marketingVersion{}, fmt.Errorf("expected major.minor.patch: %q", s)
	}
	if !strings.HasPrefix(rest, ".") {
		return marketingVersion{}, fmt.Errorf("expected major.minor.patch: %q", s)
	}
	patch, rest, ok := readVersionInt(rest[1:])
	if !ok {
		return marketingVersion{}, fmt.Errorf("expected major.minor.patch: %q", s)
	}
	return marketingVersion{Major: major, Minor: minor, Patch: patch, Suffix: rest}, nil
}

func readVersionInt(s string) (n int, rest string, ok bool) {
	if s == "" || !unicode.IsDigit(rune(s[0])) {
		return 0, s, false
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		// avoid overflow for absurd inputs
		if n > 1_000_000_000 {
			return 0, s, false
		}
		i++
	}
	return n, s[i:], true
}

// compareMarketingVersion returns -1 if a < b, 0 if a == b, 1 if a > b.
// Invalid versions are treated as less than valid ones; two invalids compare equal.
func compareMarketingVersion(a, b string) int {
	va, ea := parseMarketingVersion(a)
	vb, eb := parseMarketingVersion(b)
	if ea != nil && eb != nil {
		return 0
	}
	if ea != nil {
		return -1
	}
	if eb != nil {
		return 1
	}
	if va.Major != vb.Major {
		return cmpInt(va.Major, vb.Major)
	}
	if va.Minor != vb.Minor {
		return cmpInt(va.Minor, vb.Minor)
	}
	if va.Patch != vb.Patch {
		return cmpInt(va.Patch, vb.Patch)
	}
	// Same triple: plain release (empty suffix) sorts above any prerelease/
	// suffix so 1.0.0-beta < 1.0.0. Two non-empty suffixes: string order.
	if va.Suffix == vb.Suffix {
		return 0
	}
	if va.Suffix == "" {
		return 1
	}
	if vb.Suffix == "" {
		return -1
	}
	return strings.Compare(va.Suffix, vb.Suffix)
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// versionFormatOK is true when candidate is a valid marketing version string.
func versionFormatOK(candidate string) error {
	candidate = strings.TrimSpace(candidate)
	if _, err := parseMarketingVersion(candidate); err != nil {
		return fmt.Errorf("invalid version %q (want major.minor.patch…)", candidate)
	}
	return nil
}

// versionOKForRelease validates format only. Marketing versions may be reused
// across builds; monotonicity is enforced on build numbers, not versions.
// The last argument is ignored (kept for call-site compatibility during migration).
func versionOKForRelease(candidate, last string) error {
	_ = last
	return versionFormatOK(candidate)
}

// bumpPatchVersion returns major.minor.(patch+1) with suffix dropped.
// Empty or invalid last → "0.1.0".
func bumpPatchVersion(last string) string {
	last = strings.TrimSpace(last)
	if last == "" {
		return "0.1.0"
	}
	v, err := parseMarketingVersion(last)
	if err != nil {
		return "0.1.0"
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch+1)
}
