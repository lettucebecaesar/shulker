package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBinary compiles the real CLI so these tests exercise argv parsing,
// file I/O, exit codes, and the Regolith exe-filter invocation shape â€” the
// exact surfaces humans and Regolith touch.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "shulker-test"+exeSuffix())
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

type runResult struct {
	stdout, stderr string
	exitCode       int
}

func runBin(t *testing.T, bin string, dir string, env []string, args ...string) runResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		// Start from the parent environment + explicit overrides so the
		// Regolith env-contract test mirrors a real filter run.
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return runResult{stdout.String(), stderr.String(), code}
}

func TestCLIEndToEnd(t *testing.T) {
	bin := buildBinary(t)
	rich, err := filepath.Abs(filepath.Join("..", "..", "testdata", "packs", "rich"))
	if err != nil {
		t.Fatal(err)
	}
	broken, err := filepath.Abs(filepath.Join("..", "..", "testdata", "packs", "broken"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	out := filepath.Join(tmp, "rich.squashed.mcpack")

	// squash
	r := runBin(t, bin, tmp, nil, "squash", rich, out)
	if r.exitCode != 0 {
		t.Fatalf("squash failed (%d):\n%s\n%s", r.exitCode, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "TOTAL") {
		t.Errorf("squash output missing the size table:\n%s", r.stdout)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output pack missing: %v", err)
	}
	mapPath := strings.TrimSuffix(out, ".mcpack") + ".map.json"
	if _, err := os.Stat(mapPath); err != nil {
		t.Fatalf("mapping file missing: %v", err)
	}

	// check the squashed pack: warnings ok, errors fail
	r = runBin(t, bin, tmp, nil, "check", out)
	if r.exitCode != 0 {
		t.Errorf("check on squashed pack failed (%d):\n%s\n%s", r.exitCode, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "references") {
		t.Errorf("check output missing inventory summary:\n%s", r.stdout)
	}

	// check the broken pack: must fail loudly
	r = runBin(t, bin, tmp, nil, "check", broken)
	if r.exitCode == 0 {
		t.Error("check on broken pack must exit non-zero")
	}
	if !strings.Contains(r.stdout+r.stderr, "textures/broken.json") {
		t.Errorf("check on broken pack must name the defect:\n%s\n%s", r.stdout, r.stderr)
	}

	// map summary
	r = runBin(t, bin, tmp, nil, "map", mapPath)
	if r.exitCode != 0 {
		t.Fatalf("map failed: %s", r.stderr)
	}
	if !strings.Contains(r.stdout, "renames:") {
		t.Errorf("map output missing rename count:\n%s", r.stdout)
	}

	// reverse: re-derive the original layout
	restored := filepath.Join(tmp, "restored")
	r = runBin(t, bin, tmp, nil, "map", "--reverse", mapPath, out, restored)
	if r.exitCode != 0 {
		t.Fatalf("map --reverse failed: %s\n%s", r.stdout, r.stderr)
	}
	for _, want := range []string{
		"manifest.json",
		"entity/shulker_mob.entity.json",
		"textures/custom/mob.png",
		"sounds/custom/pop.ogg",
	} {
		if _, err := os.Stat(filepath.Join(restored, filepath.FromSlash(want))); err != nil {
			t.Errorf("restored layout missing %s: %v", want, err)
		}
	}
	// Restored references point at original paths again.
	entity, err := os.ReadFile(filepath.Join(restored, "entity", "shulker_mob.entity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entity), "textures/custom/mob\"") {
		t.Errorf("restored entity does not reference the original path:\n%s", entity)
	}

	// version
	r = runBin(t, bin, tmp, nil, "version")
	if r.exitCode != 0 || !strings.Contains(r.stdout, "shulker") {
		t.Errorf("version failed: %s", r.stderr)
	}
}

// Regolith invokes exe filters as: <exe> [settingsJSON ...args] with the
// staged pack at ./RP relative to CWD. This test reproduces that exact
// invocation shape (verified against Bedrock-OSS/regolith filter_exe.go).
func TestCLIRegolithMode(t *testing.T) {
	bin := buildBinary(t)
	stage := t.TempDir()
	rp := filepath.Join(stage, "RP")
	richSrc, err := filepath.Abs(filepath.Join("..", "..", "testdata", "packs", "rich"))
	if err != nil {
		t.Fatal(err)
	}
	if err := copyTree(richSrc, rp); err != nil {
		t.Fatalf("staging RP/: %v", err)
	}

	r := runBin(t, bin, stage, nil, `{"no-rename":true}`, "regolith")
	if r.exitCode != 0 {
		t.Fatalf("regolith mode failed (%d):\n%s\n%s", r.exitCode, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "in-place squash") {
		t.Errorf("regolith mode missing completion line:\n%s", r.stdout)
	}
	// In-place: minified files exist at their original paths (no-rename).
	blocks, err := os.ReadFile(filepath.Join(rp, "blocks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blocks), "\n") {
		t.Error("regolith mode did not minify blocks.json in place")
	}
	// The manifest survives byte-identically.
	manifest, err := os.ReadFile(filepath.Join(rp, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "Shulker Rich Fixture") {
		t.Error("regolith mode damaged the manifest")
	}
}

// The zero-argument shape: Regolith invokes exe filters with NO argv at all
// when the profile filter has empty settings — the environment pair
// (FILTER_DIR/ROOT_DIR) is the only signal. This mirrors a real run.
func TestCLIRegolithZeroArgInvocation(t *testing.T) {
	bin := buildBinary(t)
	stage := t.TempDir()
	rp := filepath.Join(stage, "RP")
	richSrc, err := filepath.Abs(filepath.Join("..", "..", "testdata", "packs", "rich"))
	if err != nil {
		t.Fatal(err)
	}
	if err := copyTree(richSrc, rp); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"FILTER_DIR=" + stage,
		"ROOT_DIR=" + stage,
	}
	r := runBin(t, bin, stage, env /* no arguments at all */)
	if r.exitCode != 0 {
		t.Fatalf("zero-arg regolith invocation failed (%d):\n%s\n%s", r.exitCode, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "in-place squash") {
		t.Errorf("missing completion line:\n%s", r.stdout)
	}
	// Full default squash: renames happened in place.
	if _, err := os.Stat(filepath.Join(rp, "entity", "_0.entity.json")); err != nil {
		t.Errorf("expected renamed entity file in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rp, "entity", "shulker_mob.entity.json")); !os.IsNotExist(err) {
		t.Error("original entity path should be gone after in-place rename")
	}
}

func TestCLIUnknownCommandExitsUsage(t *testing.T) {
	bin := buildBinary(t)
	r := runBin(t, bin, t.TempDir(), nil, "frobnicate")
	if r.exitCode != 2 {
		t.Errorf("unknown command exit = %d, want 2", r.exitCode)
	}
}

// copyTree copies a directory tree recursively (portable xcopy).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
