package main

import "testing"

// Android package IDs have non-obvious sanitization (hyphens, single-segment
// prefixing, reverse-DNS rules) that must stay stable for installed apps.
func TestAndroidPackageName(t *testing.T) {
	// Empty id → prefix + sanitized folder (hyphens gone).
	if got := androidPackageName("", "dev.shirei", "hacker-news-reader"); got != "dev.shirei.hackernewsreader" {
		t.Fatalf("empty id: %q", got)
	}
	// Single segment with hyphens is not reverse-DNS — prefix it.
	if got := androidPackageName("hacker-news-reader", "dev.hasen", "x"); got != "dev.hasen.hackernewsreader" {
		t.Fatalf("single segment: %q", got)
	}
	// Multi-segment: strip hyphens inside segments.
	if got := androidPackageName("dev.hasen.hacker-news", "dev.shirei", "x"); got != "dev.hasen.hackernews" {
		t.Fatalf("hyphen in segment: %q", got)
	}
	// Valid id preserved (lowercased via sanitize).
	if got := androidPackageName("dev.hasen.hn", "", "x"); got != "dev.hasen.hn" {
		t.Fatalf("valid: %q", got)
	}
	// Default prefix when empty.
	if got := androidPackageName("", "", "piano"); got != defaultAppIDPrefix+".piano" {
		t.Fatalf("default prefix: %q", got)
	}
}
