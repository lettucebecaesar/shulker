package transform

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shulker-tools/shulker/internal/pack"
)

// fakeFFmpeg creates a fake ffmpeg executable in dir (cmd on Windows, sh
// script elsewhere) and returns a PATH value that finds it first.
//
//	-version   -> prints a version line
//	transcode  -> writes "shrunk" to its last argument
func fakeFFmpeg(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var name, content string
	if runtime.GOOS == "windows" {
		name = filepath.Join(dir, "ffmpeg.cmd")
		content = "@if \"%1\"==\"-version\" (@echo ffmpeg version 9.9.9-fake& exit /b 0)\r\n" +
			"@for %%A in (%*) do @set OUT=%%A\r\n@echo shrunk>\"%OUT%\"\r\n"
	} else {
		name = filepath.Join(dir, "ffmpeg")
		content = "#!/bin/sh\n" +
			"if [ \"$1\" = \"-version\" ]; then echo \"ffmpeg version 9.9.9-fake\"; exit 0; fi\n" +
			"for a in \"$@\"; do :; done\necho -n shrunk > \"$a\"\n"
	}
	if err := os.WriteFile(name, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func TestTranscodeOggFFmpegWins(t *testing.T) {
	t.Setenv("PATH", fakeFFmpeg(t, t.TempDir()))
	data := bytes.Repeat([]byte("OggS-fake-payload-"), 10) // 180 bytes
	out, smaller, err := TranscodeOggFFmpeg(data, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !smaller {
		t.Fatalf("fake transcode result must count as smaller (in %d, out %d)", len(data), len(out))
	}
	if !bytes.Contains(out, []byte("shrunk")) {
		t.Errorf("unexpected fake output: %q", out)
	}
	if FFmpegVersion() != "ffmpeg version 9.9.9-fake" {
		t.Errorf("FFmpegVersion = %q", FFmpegVersion())
	}
}

func TestTranscodeOggFFmpegKeepsIfLarger(t *testing.T) {
	// A fake that always writes a bigger file than the input.
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var name, content string
	if runtime.GOOS == "windows" {
		name = filepath.Join(dir, "ffmpeg.cmd")
		content = "@for %%A in (%*) do @set OUT=%%A\r\n@echo %random%%random%%random%%random%%random%%random%%random%%random%>\"%OUT%\"\r\n"
	} else {
		name = filepath.Join(dir, "ffmpeg")
		content = "#!/bin/sh\nfor a in \"$@\"; do :; done\nhead -c 4096 /dev/zero > \"$a\"\n"
	}
	if err := os.WriteFile(name, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	data := []byte("tiny")
	out, smaller, err := TranscodeOggFFmpeg(data, 4)
	if err != nil {
		t.Fatal(err)
	}
	if smaller || !bytes.Equal(out, data) {
		t.Errorf("keep-if-smaller violated: smaller=%v out=%q", smaller, out)
	}
}

func TestDefaultAudioFnReportsMissingFFmpeg(t *testing.T) {
	// An empty PATH means LookPath cannot find ffmpeg; the hook must
	// report it as an error (the pipeline warns and passes audio through)
	// rather than crash or corrupt.
	t.Setenv("PATH", t.TempDir())
	out, smaller, err := DefaultAudioFn([]byte("OggS"), 4)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg not found") {
		t.Fatalf("expected ffmpeg-absence error, got %v", err)
	}
	if smaller || !bytes.Equal(out, []byte("OggS")) {
		t.Error("absence must pass bytes through")
	}
}

// The pipeline accounting: --audio-ffmpeg transcodes the pack-internal
// sound, keeps its result through the rename, and the validator accepts
// the accounted change.
func TestSquashAudioTranscodeAccounting(t *testing.T) {
	p, docs, g := fixture(t, "rich")
	opts := DefaultOptions()
	opts.AudioFFmpeg = true
	opts.AudioFn = func(data []byte, quality int) ([]byte, bool, error) {
		return []byte("TRANSCODED-OGG"), true, nil
	}
	res, err := Run(p, docs, g, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AudioTranscoded["sounds/custom/pop.ogg"] {
		t.Fatalf("expected the pack-internal ogg to be transcoded: %+v", res.AudioTranscoded)
	}
	// The renamed file carries the new bytes.
	if got := fileAt(res, "sounds/_0.ogg"); !bytes.Equal(got, []byte("TRANSCODED-OGG")) {
		t.Errorf("renamed ogg carries %q", got)
	}
	// Subpack audio and any other ogg must be untouched.
	for _, f := range res.Files {
		if f.Class == pack.ClassSubpack && strings.HasSuffix(f.Path, ".ogg") &&
			!bytes.Equal(f.Data, fileData(t, p, f.Path)) {
			t.Errorf("subpack audio changed: %s", f.Path)
		}
	}
}
