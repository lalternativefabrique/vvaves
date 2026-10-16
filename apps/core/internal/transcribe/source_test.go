package transcribe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireBin(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed", name)
	}
	return p
}

// buildHLS encodes a sine wave into a real HLS stream (manifest + .ts segments)
// and returns the manifest path.
func buildHLS(t *testing.T, codec string) string {
	t.Helper()
	ffmpeg := requireBin(t, "ffmpeg")
	dir := t.TempDir()
	manifest := filepath.Join(dir, "master.m3u8")
	cmd := exec.Command(ffmpeg, "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6",
		"-c:a", codec,
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(dir, "seg%03d.ts"),
		manifest,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot build %s HLS fixture: %v (%s)", codec, err, stderr.String())
	}
	return manifest
}

// An HLS input must yield a single decodable audio file. Deriving the output
// extension from the URL made ffmpeg re-emit a playlist: it exited 0 and left a
// few hundred bytes of text that the STT provider then rejected.
func TestURLSource_HLSProducesAudioNotAPlaylist(t *testing.T) {
	for _, tc := range []struct {
		codec   string
		wantExt string
	}{
		{"aac", ".m4a"},
		{"libmp3lame", ".mp3"},
	} {
		t.Run(tc.codec, func(t *testing.T) {
			manifest := buildHLS(t, tc.codec)

			path, cleanup, err := URLSource{URL: manifest}.Localize(context.Background())
			if err != nil {
				t.Fatalf("Localize: %v", err)
			}
			defer cleanup()

			if got := filepath.Ext(path); got != tc.wantExt {
				t.Errorf("output extension = %q, want %q", got, tc.wantExt)
			}
			if filepath.Ext(path) == ".m3u8" {
				t.Fatal("output is an HLS manifest, not an audio file")
			}

			head, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			if strings.HasPrefix(string(head), "#EXTM3U") {
				t.Fatalf("output is a playlist (%d bytes), not audio", len(head))
			}

			// A 6s manifest re-emitted as text is a few hundred bytes; real audio
			// is orders of magnitude larger.
			if len(head) < 10_000 {
				t.Errorf("output is %d bytes, too small to be 6s of audio", len(head))
			}

			ffprobe := requireBin(t, "ffprobe")
			out, err := exec.Command(ffprobe, "-v", "error",
				"-select_streams", "a:0",
				"-show_entries", "stream=codec_name",
				"-of", "default=noprint_wrappers=1:nokey=1",
				"--", path,
			).Output()
			if err != nil {
				t.Fatalf("output is not decodable audio: %v", err)
			}
			if strings.TrimSpace(string(out)) == "" {
				t.Fatal("output carries no audio stream")
			}
		})
	}
}

// The WAV path re-encodes, so its container never depends on the input.
func TestURLSource_WAVPathIgnoresInputContainer(t *testing.T) {
	manifest := buildHLS(t, "aac")

	path, cleanup, err := URLSource{URL: manifest, WAV: true}.Localize(context.Background())
	if err != nil {
		t.Fatalf("Localize: %v", err)
	}
	defer cleanup()

	if got := filepath.Ext(path); got != ".wav" {
		t.Fatalf("output extension = %q, want .wav", got)
	}
}

// copyExtension keys on the probed codec, never on the URL: an .m3u8 URL whose
// payload is AAC must produce .m4a, and a codec ffprobe cannot read falls back
// rather than propagating a non-audio extension.
func TestCopyExtension(t *testing.T) {
	requireBin(t, "ffprobe")

	t.Run("aac hls yields m4a", func(t *testing.T) {
		manifest := buildHLS(t, "aac")
		if got := (URLSource{URL: manifest}).copyExtension(context.Background()); got != ".m4a" {
			t.Errorf("copyExtension = %q, want .m4a", got)
		}
	})

	t.Run("mp3 hls yields mp3", func(t *testing.T) {
		manifest := buildHLS(t, "libmp3lame")
		if got := (URLSource{URL: manifest}).copyExtension(context.Background()); got != ".mp3" {
			t.Errorf("copyExtension = %q, want .mp3", got)
		}
	})

	t.Run("unprobeable input falls back to m4a", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "not-audio.m3u8")
		if err := os.WriteFile(bad, []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := (URLSource{URL: bad}).copyExtension(context.Background()); got != ".m4a" {
			t.Errorf("copyExtension = %q, want .m4a", got)
		}
	})

	t.Run("missing ffprobe falls back to m4a", func(t *testing.T) {
		s := URLSource{URL: "whatever", FFprobePath: filepath.Join(t.TempDir(), "absent")}
		if got := s.copyExtension(context.Background()); got != ".m4a" {
			t.Errorf("copyExtension = %q, want .m4a", got)
		}
	})
}

func TestFileSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, cleanup, err := FileSource{Path: path}.Localize(context.Background())
	if err != nil {
		t.Fatalf("Localize: %v", err)
	}
	cleanup()
	if got != path {
		t.Errorf("path = %q, want %q", got, path)
	}

	if _, _, err := (FileSource{Path: filepath.Join(t.TempDir(), "nope.mp3")}).Localize(context.Background()); err == nil {
		t.Error("Localize on a missing file: want error, got nil")
	}
}
