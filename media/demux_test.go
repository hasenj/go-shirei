package media

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func findTestFFmpeg(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("ffmpeg not installed")
	return ""
}

func makeEncodedClip(t *testing.T, name string, args ...string) string {
	t.Helper()
	ffmpeg := findTestFFmpeg(t)
	path := filepath.Join(t.TempDir(), name)
	base := []string{
		"-y",
		"-f", "lavfi", "-i", "testsrc=size=160x90:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "2",
	}
	base = append(base, args...)
	base = append(base, path)
	out, err := exec.Command(ffmpeg, base...).CombinedOutput()
	if err != nil {
		t.Skipf("ffmpeg could not encode %s (encoder unavailable?): %v\n%s", name, err, out)
	}
	return path
}

func TestDemuxWebMAndMP4(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		ffArgs     []string
		wantCont   containerKind
		wantVideo  videoCodec
		wantAudio  audioCodec
		wantPriv   bool
	}{
		{
			name:      "webm-vp9-opus",
			file:      "clip.webm",
			ffArgs:    []string{"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-g", "25", "-c:a", "libopus", "-b:a", "64k"},
			wantCont:  containerWebM,
			wantVideo: videoCodecVP9,
			wantAudio: audioCodecOpus,
		},
		{
			name:      "webm-vp8-vorbis",
			file:      "clip.webm",
			ffArgs:    []string{"-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8", "-g", "25", "-c:a", "libvorbis", "-q:a", "3"},
			wantCont:  containerWebM,
			wantVideo: videoCodecVP8,
			wantAudio: audioCodecVorbis,
			wantPriv:  true,
		},
		{
			name:      "webm-av1-opus",
			file:      "clip.webm",
			ffArgs:    []string{"-c:v", "libsvtav1", "-preset", "12", "-g", "25", "-c:a", "libopus", "-b:a", "64k"},
			wantCont:  containerWebM,
			wantVideo: videoCodecAV1,
			wantAudio: audioCodecOpus,
		},
		{
			name:      "mp4-av1-opus",
			file:      "clip.mp4",
			ffArgs:    []string{"-c:v", "libsvtav1", "-preset", "12", "-g", "25", "-c:a", "libopus", "-b:a", "64k", "-movflags", "+faststart"},
			wantCont:  containerMP4,
			wantVideo: videoCodecAV1,
			wantAudio: audioCodecOpus,
		},
		{
			name:      "mp4-vp9-opus",
			file:      "clip.mp4",
			ffArgs:    []string{"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-g", "25", "-c:a", "libopus", "-b:a", "64k", "-movflags", "+faststart"},
			wantCont:  containerMP4,
			wantVideo: videoCodecVP9,
			wantAudio: audioCodecOpus,
		},
		{
			name:      "mp4-h264-aac",
			file:      "clip.mp4",
			ffArgs:    []string{"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "25", "-c:a", "aac", "-movflags", "+faststart"},
			wantCont:  containerMP4,
			wantVideo: videoCodecH264,
			wantAudio: audioCodecAAC,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := makeEncodedClip(t, tc.file, tc.ffArgs...)

			sn, ok := sniffFile(path)
			if !ok {
				t.Fatalf("sniffFile failed for %s", path)
			}
			if sn.Container != tc.wantCont || sn.VideoCodec != tc.wantVideo || sn.AudioCodec != tc.wantAudio {
				t.Fatalf("sniff got (%v, %v, %v), want (%v, %v, %v)",
					sn.Container, sn.VideoCodec, sn.AudioCodec, tc.wantCont, tc.wantVideo, tc.wantAudio)
			}
			if sn.Width != 160 || sn.Height != 90 {
				t.Fatalf("size = %dx%d, want 160x90", sn.Width, sn.Height)
			}
			if sn.Duration < 1.5 || sn.Duration > 2.5 {
				t.Fatalf("duration = %.3fs, want ~2s", sn.Duration)
			}
			if tc.wantPriv && len(sn.AudioPrivate) == 0 {
				t.Fatalf("expected non-empty AudioPrivate")
			}

			d, err := openDemuxer(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.close()

			var vCount, aCount, keyCount int
			var maxPTS float64
			for {
				pkt, err := d.nextPacket()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("nextPacket: %v", err)
				}
				if len(pkt.Data) == 0 {
					t.Fatalf("empty packet data")
				}
				if pkt.IsVideo {
					vCount++
					if pkt.Keyframe {
						keyCount++
					}
				} else {
					aCount++
				}
				if pkt.PTS > maxPTS {
					maxPTS = pkt.PTS
				}
			}
			if vCount < 40 || aCount < 40 || keyCount < 1 {
				t.Fatalf("unexpected counts: video=%d audio=%d keys=%d", vCount, aCount, keyCount)
			}
			if maxPTS < 1.7 {
				t.Fatalf("maxPTS=%.3fs too short", maxPTS)
			}

			// Seek to 1.0s and ensure we land on/before 1.0s at a keyframe and reach near end.
			if err := d.seek(1.0); err != nil {
				t.Fatalf("seek(1.0): %v", err)
			}
			var postV, postA int
			firstVideoSeen := false
			for {
				pkt, err := d.nextPacket()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("post-seek nextPacket: %v", err)
				}
				if pkt.IsVideo {
					if !firstVideoSeen {
						firstVideoSeen = true
						if !pkt.Keyframe {
							t.Fatalf("first post-seek video packet was not a keyframe (pts=%.3f)", pkt.PTS)
						}
						if pkt.PTS > 1.15 {
							t.Fatalf("post-seek first video pts=%.3f > 1.15s", pkt.PTS)
						}
					}
					postV++
				} else {
					postA++
				}
			}
			if postV < 15 || postA < 15 {
				t.Fatalf("post-seek counts too small: video=%d audio=%d", postV, postA)
			}
		})
	}
}
