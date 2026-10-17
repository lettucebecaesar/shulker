package transform

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The Bedrock engine requires Vorbis OGG audio. There is no good pure-Go
// Vorbis encoder, so v0.1 shipped strict passthrough; v0.2 adds an OPT-IN
// ffmpeg shell-out. It is never required and never default: creators who
// already ship well-compressed audio keep byte-for-byte passthrough.
//
// Scope is deliberately conservative: only sounds whose references are
// provably pack-internal (the rename-eligible set — vanilla-path overrides
// keep their exact bytes), keep-if-smaller, and the cache key embeds the
// reported ffmpeg version so different ffmpeg builds never share results.
//
// Determinism note: ffmpeg output is deterministic for a fixed build +
// arguments; the per-build version in the cache profile keeps the
// byte-identical-cache guarantee intact. Across machines with different
// ffmpeg builds, output can differ — documented in the README.

// FFmpegVersion returns the first line of `ffmpeg -version`, used in cache
// keys. Empty string when ffmpeg is unavailable.
func FFmpegVersion() string {
	out, err := exec.Command("ffmpeg", "-version").Output()
	if err != nil {
		return ""
	}
	first := strings.TrimSpace(string(out))
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	return first
}

// TranscodeOggFFmpeg re-encodes OGG data with ffmpeg's Vorbis encoder at
// the given quality (-qscale:a, 0..10; 4 is a good default) and keeps the
// result only when smaller.
func TranscodeOggFFmpeg(data []byte, quality int) ([]byte, bool, error) {
	dir, err := os.MkdirTemp("", "shulker-audio-*")
	if err != nil {
		return data, false, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in := filepath.Join(dir, "in.ogg")
	out := filepath.Join(dir, "out.ogg")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		return data, false, err
	}
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", in,
		"-c:a", "libvorbis", "-qscale:a", fmt.Sprintf("%d", quality),
		out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return data, false, fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	encoded, err := os.ReadFile(out)
	if err != nil {
		return data, false, err
	}
	if len(encoded) < len(data) {
		return encoded, true, nil
	}
	return data, false, nil
}

// DefaultAudioFn is the transform's audio re-encode function. It reports
// ffmpeg absence as an error so the pipeline can warn once and disable the
// pass instead of failing the squash.
func DefaultAudioFn(data []byte, quality int) ([]byte, bool, error) {
	if FFmpegVersion() == "" {
		return data, false, fmt.Errorf("ffmpeg not found on PATH")
	}
	return TranscodeOggFFmpeg(data, quality)
}
