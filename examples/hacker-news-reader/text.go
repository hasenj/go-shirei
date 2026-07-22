package main

import (
	"html"
	"regexp"
	"strings"
)

// Very small HTML → plain converter for HN item text.
// HN comments use a limited subset: <p>, <i>, <b>, <a href>, <pre>, <code>, entities.

var (
	// HN uses bare <p> as a paragraph separator more often than </p>.
	reBlockBreak = regexp.MustCompile(`(?i)</?p>|<br\s*/?>|</pre>`)
	reTag        = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces     = regexp.MustCompile(`[ \t]+\n`)
	reBlankRuns  = regexp.MustCompile(`\n{3,}`)
)

func htmlToPlain(s string) string {
	if s == "" {
		return ""
	}
	// Preserve paragraph breaks before stripping tags.
	s = reBlockBreak.ReplaceAllString(s, "\n\n")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = reSpaces.ReplaceAllString(s, "\n")
	s = reBlankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
