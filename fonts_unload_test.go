package shirei

import (
	"os"
	"testing"
)

func TestUnloadFileBackedParsedFonts(t *testing.T) {
	path := "/System/Library/Fonts/Helvetica.ttc"
	if _, err := os.Stat(path); err != nil {
		path = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
		if _, err := os.Stat(path); err != nil {
			t.Skip("no small system UI font")
		}
	}
	UseFontFile(path)
	var fid FontId
	for _, f := range AllFontFaces() {
		if f.Filepath == path {
			fid = f.FontId
			break
		}
	}
	if fid == 0 {
		t.Fatal("UseFontFile did not register")
	}

	if GetParsedFont(fid) == nil {
		t.Fatal("parse failed")
	}
	if !FontParsed(fid) || !FontWarmed(fid) {
		t.Fatal("expected parsed and warmed after GetParsedFont")
	}

	unloadFileBackedParsedFonts()
	if FontParsed(fid) {
		t.Fatal("file-backed face still resident after unload")
	}
	if !FontWarmed(fid) {
		t.Fatal("warmed must survive unload")
	}

	if GetParsedFont(fid) == nil {
		t.Fatal("re-parse after unload failed")
	}
	if !FontParsed(fid) {
		t.Fatal("expected resident after re-parse")
	}
}

func TestUnloadKeepsUseFontBytes(t *testing.T) {
	// Microns is registered from widgets via UseFontBytes; any in-memory
	// face with no filepath must survive unload.
	var fid FontId
	for _, f := range AllFontFaces() {
		if f.Filepath == "" && f.FontId != 0 {
			fid = f.FontId
			break
		}
	}
	if fid == 0 {
		t.Skip("no UseFontBytes face registered")
	}
	if GetParsedFont(fid) == nil && !FontParsed(fid) {
		// bytes faces are published already parsed
		t.Skip("bytes face not parsed")
	}
	unloadFileBackedParsedFonts()
	if !FontParsed(fid) {
		t.Fatalf("UseFontBytes face %d dropped by unload", fid)
	}
}
