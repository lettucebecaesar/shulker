// Command shulker shrinks, speeds up, and scrambles Minecraft Bedrock
// Edition resource packs. Squashing is the product; obfuscation falls out of
// it. See docs/INVARIANTS.md for the rules this binary refuses to break.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/pack"
	"github.com/shulker-tools/shulker/internal/pipeline"
	"github.com/shulker-tools/shulker/internal/report"
	"github.com/shulker-tools/shulker/internal/transform"
	"github.com/shulker-tools/shulker/internal/validate"
)

// version is set by the Makefile/CI via -ldflags; dev default for source
// builds.
var version = "0.1.0"

const usageText = `shulker — Bedrock pack squasher & obfuscator

Usage:
  shulker squash <in> [out]     squash a pack (.mcpack/.zip file or directory);
                                also accepts .mcaddon/.mctemplate/.mcworld
                                containers — resource pack members are
                                squashed, behavior packs and world files
                                pass through byte-identically
  shulker check <pack>          validate a pack without changing anything
                                (containers: validates each RP member)
  shulker map <mapping.json>    inspect a squash mapping
                                --reverse <pack> <outdir>: re-derive original layout
  shulker regolith              Regolith filter mode (invoked by Regolith)
  shulker version               print the version

Squash flags:
  --no-rename            disable all path renaming (keeps rewrites off too)
  --no-rename-textures   keep texture paths
  --no-rename-sounds     keep sound file paths
  --no-rename-identdirs  keep entity/geometry/animation/controller paths
  --no-minify            don't minify JSON
  --no-png               don't re-encode PNGs
  --png-quantize         LOSSY: quantize binary-alpha PNGs to <=256 colors
                         (opt-in; changes pixels; --png-quantize-colors N)
  --png-optimize-overrides  re-encode override textures (vanilla paths,
                         unreferenced, blocked) with pixel-verified lossless
                         encoding; paths and pixels stay, bytes shrink
  --workers N           parallel PNG workers (default 8)
  --out-map FILE         mapping file to write (default <out>.map.json)
  --report FILE          machine-readable JSON report
  --no-cache             disable the content-addressed transform cache
  -q                     quiet: only errors

Hard invariants (docs/INVARIANTS.md): never corrupt a pack; manifest.json and
pack_icon.png are sacred; vanilla-override paths stay untouched; PBR texture
sets move in lockstep; Molang is never touched; output is byte-deterministic.
`

func main() {
	args := os.Args[1:]
	// Regolith exe filters are invoked as: <shulker> [settingsJSON ...args]
	// with FILTER_DIR/ROOT_DIR in the environment — and with NO arguments at
	// all when the filter has no settings. Detect that shape reliably.
	if looksLikeRegolithInvocation(args) {
		runRegolith(args)
		return
	}
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	switch args[0] {
	case "squash":
		cmdSquash(args[1:])
	case "check":
		cmdCheck(args[1:])
	case "map":
		cmdMap(args[1:])
	case "regolith":
		runRegolith(args[1:])
	case "version", "--version", "-v":
		fmt.Println("shulker " + version)
	case "help", "--help", "-h":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "shulker: unknown command %q\n\n", args[0])
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
}

// looksLikeRegolithInvocation reports whether the argv matches how Regolith
// runs exe filters. Verified against Bedrock-OSS/regolith v1.8
// (filter_exe.go, utils.go):
//
//   - the binary is invoked as <exe> [settingsJSON ...staticArgs], where
//     the settings JSON is present ONLY when the profile filter has
//     non-empty settings;
//   - CWD is the Regolith working dir (.regolith/tmp) containing the staged
//     pack at ./RP;
//   - FILTER_DIR and ROOT_DIR are always exported into the environment.
//
// The environment pair is the reliable signal; argv alone cannot
// distinguish "no settings" from "user typed shulker with no command".
func looksLikeRegolithInvocation(args []string) bool {
	for _, a := range args {
		if a == "regolith" {
			return true
		}
	}
	if os.Getenv("FILTER_DIR") != "" && os.Getenv("ROOT_DIR") != "" {
		return true
	}
	if len(args) >= 1 {
		trimmed := strings.TrimSpace(args[0])
		if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
			return true
		}
	}
	return false
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "shulker: "+format+"\n", args...)
	os.Exit(1)
}

// reorderArgs lets users write flags before or after positionals
// (`shulker squash pack.mcpack --no-rename`), which the stdlib flag package
// alone does not accept. valueFlags lists the long flags that consume a
// separate value token; those must appear as `--flag value` or `--flag=value`.
func reorderArgs(args []string, valueFlags map[string]bool) (flags, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			name := a
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				name = a[:eq]
			}
			if valueFlags[name] && !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		rest = append(rest, a)
	}
	return flags, rest
}

// squashValueFlags are the squash flags that take a separate value.
var squashValueFlags = map[string]bool{
	"--out-map":             true,
	"--report":              true,
	"--workers":             true,
	"--png-quantize-colors": true,
}

// ---------------------------------------------------------------- squash

func cmdSquash(args []string) {
	fs := flag.NewFlagSet("squash", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	outMap := fs.String("out-map", "", "mapping file to write (default <out>.map.json)")
	reportFile := fs.String("report", "", "write a machine-readable JSON report to this file")
	noRename := fs.Bool("no-rename", false, "disable all renaming")
	noRenameTextures := fs.Bool("no-rename-textures", false, "keep texture paths")
	noRenameSounds := fs.Bool("no-rename-sounds", false, "keep sound file paths")
	noRenameIdentDirs := fs.Bool("no-rename-identdirs", false, "keep entity/geometry/animation paths")
	noMinify := fs.Bool("no-minify", false, "don't minify JSON")
	noPNG := fs.Bool("no-png", false, "don't re-encode PNGs")
	pngQuantize := fs.Bool("png-quantize", false, "LOSSY: quantize binary-alpha PNGs to <=256 colors (opt-in; changes pixels)")
	quantizeColors := fs.Int("png-quantize-colors", 256, "palette size target for --png-quantize (2..256)")
	pngOverrides := fs.Bool("png-optimize-overrides", false, "re-encode override textures (vanilla paths etc.) with pixel-verified lossless encoding")
	audioFFmpeg := fs.Bool("audio-ffmpeg", false, "re-encode pack-internal OGG audio through ffmpeg (opt-in, keep-if-smaller; requires ffmpeg)")
	audioQuality := fs.Int("audio-quality", 4, "Vorbis quality for --audio-ffmpeg (-qscale:a, 0..10)")
	noCache := fs.Bool("no-cache", false, "disable the transform cache")
	workers := fs.Int("workers", 8, "parallel PNG workers")
	quiet := fs.Bool("q", false, "quiet")
	// ExitOnError means Parse exits the process on bad flags; the returned
	// error is always nil but errcheck insists, and honestly so do we.
	flagArgs, rest := reorderArgs(args, squashValueFlags)
	_ = fs.Parse(flagArgs)
	if len(rest) < 1 || len(rest) > 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	in := rest[0]
	out := ""
	if len(rest) == 2 {
		out = rest[1]
	} else {
		out = defaultOutPath(in)
	}

	opts := pipeline.Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/" + version,
	}
	if *noRename {
		opts.Transforms = opts.Transforms.WithRenameDisabled()
	}
	opts.Transforms.RenameTextures = !*noRenameTextures
	opts.Transforms.RenameSounds = !*noRenameSounds
	opts.Transforms.RenameIdentDirs = !*noRenameIdentDirs
	opts.Transforms.Minify = !*noMinify
	opts.Transforms.PNGOptimize = !*noPNG
	opts.Transforms.PNGQuantize = *pngQuantize
	opts.Transforms.QuantizeColors = *quantizeColors
	opts.Transforms.PNGOptimizeOverrides = *pngOverrides
	opts.Transforms.AudioFFmpeg = *audioFFmpeg
	opts.Transforms.AudioQuality = *audioQuality
	opts.Transforms.Workers = *workers
	opts.UseCache = !*noCache

	// Containers (.mcaddon/.mctemplate/.mcworld): squash every resource
	// pack member, pass behavior packs and world files through verbatim.
	if pack.ContainerExt(strings.ToLower(filepath.Ext(in))) {
		cmdSquashContainer(in, out, opts, *outMap, *reportFile, *quiet)
		return
	}

	if !*quiet {
		fmt.Printf("shulker %s — squashing %s\n", version, in)
	}
	res, err := pipeline.Squash(in, opts)
	if err != nil {
		fail("%v", err)
	}

	if err := res.Out.WriteZipFile(out); err != nil {
		fail("writing %s: %v", out, err)
	}
	if *outMap == "" {
		*outMap = strings.TrimSuffix(out, filepath.Ext(out)) + ".map.json"
	}
	mapping := report.NewMapping("shulker/"+version,
		res.Transform.Renames, res.Transform.Rewrites,
		res.Transform.PNGOptimized, res.Transform.SkipReasons)
	if err := os.WriteFile(*outMap, mapping.Encode(), 0o644); err != nil {
		fail("writing mapping %s: %v", *outMap, err)
	}

	sizes := report.BuildSizeReport(res.In, res.Out)
	if !*quiet {
		sizes.Render(os.Stdout)
		fmt.Printf("\n  output:   %s\n  mapping:  %s\n", out, *outMap)
		fmt.Printf("  renamed %d files, rewrote %d references, re-encoded %d PNGs",
			len(res.Transform.Renames), len(res.Transform.Rewrites), len(res.Transform.PNGOptimized))
		if len(res.Transform.OverridesOptimized) > 0 {
			fmt.Printf(" (%d of them vanilla-override textures, pixel-verified)", len(res.Transform.OverridesOptimized))
		}
		fmt.Println()
		if *pngQuantize {
			fmt.Println("  note: --png-quantize is LOSSY — quantized textures render approximated colors")
		}
		printWarnings(res.Warnings(), *quiet)
	}
	if *reportFile != "" {
		var warnings []string
		for _, f := range res.Warnings() {
			warnings = append(warnings, f.String())
		}
		if err := os.WriteFile(*reportFile, sizes.Encode("shulker/"+version,
			len(res.Transform.Renames), len(res.Transform.Rewrites),
			len(res.Transform.PNGOptimized), warnings), 0o644); err != nil {
			fail("writing report %s: %v", *reportFile, err)
		}
		if !*quiet {
			fmt.Printf("  report:   %s\n", *reportFile)
		}
	}
}

func defaultOutPath(in string) string {
	ext := strings.ToLower(filepath.Ext(in))
	if pack.ContainerExt(ext) {
		// Preserve the container kind: bundle.mcaddon -> bundle.squashed.mcaddon
		base := filepath.Base(in)
		return strings.TrimSuffix(base, ext) + ".squashed" + ext
	}
	base := filepath.Base(in)
	switch ext {
	case ".mcpack", ".zip":
		return strings.TrimSuffix(base, ext) + ".squashed.mcpack"
	}
	if isDir(in) {
		return strings.TrimSuffix(base, filepath.Ext(base)) + ".squashed.mcpack"
	}
	return base + ".squashed.mcpack"
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// ----------------------------------------------------------------- check

func cmdCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	flagArgs, rest := reorderArgs(args, nil)
	_ = fs.Parse(flagArgs) // ExitOnError handles bad flags
	if len(rest) != 1 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	p, err := pack.Load(rest[0])
	if err != nil {
		// Containers get their own check flow (per-member validation).
		if pack.ContainerExt(strings.ToLower(filepath.Ext(rest[0]))) {
			cmdCheckContainer(rest[0])
			return
		}
		fail("%v", err)
	}
	docs, parseErrs := graph.ParseAll(p)
	g, err := graph.Build(p, docs, parseErrs)
	if err != nil {
		fail("%v", err)
	}
	r := validate.Check(p, g)

	fmt.Printf("shulker %s — checking %s\n", version, rest[0])
	fmt.Printf("  %d files, %s\n", len(p.Files), report.HumanBytes(p.TotalBytes()))
	cats := map[pack.Class]int{}
	for _, f := range p.Files {
		cats[f.Class]++
	}
	fmt.Printf("  references: %d (%d rewriteable, %d blocking)\n",
		len(g.Edges), countRewriteable(g), countBlocking(g))
	if len(g.Sets()) > 0 {
		fmt.Printf("  PBR texture sets: %d\n", len(g.Sets()))
	}
	if len(r.Findings) == 0 {
		fmt.Println("\n  no findings — pack is clean")
		return
	}
	fmt.Printf("\n  findings (%d):\n", len(r.Findings))
	for _, f := range r.Findings {
		fmt.Printf("  - %s\n", f.String())
	}
	if r.HasErrors() {
		fmt.Println("\n  check FAILED")
		os.Exit(1)
	}
	fmt.Println("\n  check passed (with warnings)")
}

func countRewriteable(g *graph.Graph) int {
	n := 0
	for _, e := range g.Edges {
		if e.Rewriteable {
			n++
		}
	}
	return n
}

func countBlocking(g *graph.Graph) int {
	n := 0
	for _, e := range g.Edges {
		if e.Rewriteable {
			continue
		}
		if e.Kind == graph.KindTexturePath || e.Kind == graph.KindSoundPath {
			n++
		}
	}
	return n
}

// ------------------------------------------------------------------- map

func cmdMap(args []string) {
	fs := flag.NewFlagSet("map", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: shulker map <mapping.json>\n       shulker map --reverse <mapping.json> <pack> <outdir>\n")
	}
	reverse := fs.Bool("reverse", false, "undo a squash: rebuild the original layout")
	flagArgs, rest := reorderArgs(args, nil)
	_ = fs.Parse(flagArgs) // ExitOnError handles bad flags
	if !*reverse {
		if len(rest) != 1 {
			fs.Usage()
			os.Exit(2)
		}
		data, err := os.ReadFile(rest[0])
		if err != nil {
			fail("%v", err)
		}
		m, err := report.ParseMapping(data)
		if err != nil {
			fail("%v", err)
		}
		m.Summarize(os.Stdout)
		return
	}
	if len(rest) != 3 {
		fs.Usage()
		os.Exit(2)
	}
	mappingPath, packPath, outDir := rest[0], rest[1], rest[2]
	data, err := os.ReadFile(mappingPath)
	if err != nil {
		fail("%v", err)
	}
	m, err := report.ParseMapping(data)
	if err != nil {
		fail("%v", err)
	}
	p, err := pack.Load(packPath)
	if err != nil {
		fail("%v", err)
	}
	restored, err := m.ReverseApply(p)
	if err != nil {
		fail("reverse failed: %v", err)
	}
	if err := restored.WriteDir(outDir); err != nil {
		fail("writing restored pack: %v", err)
	}
	fmt.Printf("restored %d files to %s (original layout; minified JSON and optimized PNGs remain as built)\n",
		len(restored.Files), outDir)
}

// --------------------------------------------------------------- regolith

// runRegolith implements the Regolith "exe filter" contract, verified
// against Bedrock-OSS/regolith v1.8 source (filter_exe.go):
//
//   - Regolith stages the compiled pack at ./RP relative to the filter's
//     working directory (.regolith/tmp),
//   - invokes the binary as <exe> [settingsJSON ...staticArgs] with
//     FILTER_DIR/ROOT_DIR in the environment,
//   - relays stdout/stderr into its log.
//
// The squash runs IN PLACE on ./RP so the rest of the filter chain and the
// export step see the optimized pack. Settings arrive as a JSON object in
// argv and map onto squash flags.
func runRegolith(args []string) {
	settings := map[string]any{}
	for _, a := range args {
		trimmed := strings.TrimSpace(a)
		if !strings.HasPrefix(trimmed, "{") {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			settings = parsed
		}
	}

	const stagedPack = "RP"
	if !isDir(stagedPack) {
		fail("regolith mode expects the staged pack at ./%s (CWD: %s)", stagedPack, mustWd())
	}

	opts := pipeline.Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/" + version,
	}
	// Regolith settings use no-* keys: {"no-minify": true} must DISABLE
	// the pass, so the value is negated on assignment.
	applyNoSetting := func(key string, dst *bool) {
		if v, ok := settings[key]; ok {
			if b, ok := v.(bool); ok {
				*dst = !b
			}
		}
	}
	if v, ok := settings["no-rename"].(bool); ok && v {
		opts.Transforms = opts.Transforms.WithRenameDisabled()
	}
	applyNoSetting("no-rename-textures", &opts.Transforms.RenameTextures)
	applyNoSetting("no-rename-sounds", &opts.Transforms.RenameSounds)
	applyNoSetting("no-rename-identdirs", &opts.Transforms.RenameIdentDirs)
	applyNoSetting("no-minify", &opts.Transforms.Minify)
	applyNoSetting("no-png", &opts.Transforms.PNGOptimize)
	applyNoSetting("png-optimize-overrides", &opts.Transforms.PNGOptimizeOverrides)
	if v, ok := settings["png-quantize"].(bool); ok {
		opts.Transforms.PNGQuantize = v
	}
	if v, ok := settings["png-quantize-colors"].(float64); ok && v >= 2 {
		opts.Transforms.QuantizeColors = int(v)
	}
	opts.UseCache = true
	if v, ok := settings["no-cache"].(bool); ok && v {
		opts.UseCache = false
	}
	if v, ok := settings["workers"].(float64); ok && v >= 1 {
		opts.Transforms.Workers = int(v)
	}

	res, err := pipeline.Squash(stagedPack, opts)
	if err != nil {
		fail("%v", err)
	}

	// Write back in place: every output file, then delete the paths that
	// only existed before renaming (Regolith's export copies whatever is
	// in RP/ onward).
	outPaths := map[string]bool{}
	for _, f := range res.Out.Files {
		target := filepath.Join(stagedPack, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			fail("writing %s: %v", target, err)
		}
		if err := os.WriteFile(target, f.Data, 0o644); err != nil {
			fail("writing %s: %v", target, err)
		}
		outPaths[f.Path] = true
	}
	for _, f := range res.In.Files {
		if !outPaths[f.Path] {
			stale := filepath.Join(stagedPack, filepath.FromSlash(f.Path))
			if err := os.Remove(stale); err != nil && !os.IsNotExist(err) {
				fail("removing renamed-away file %s: %v", stale, err)
			}
		}
	}

	sizes := report.BuildSizeReport(res.In, res.Out)
	sizes.Render(os.Stdout)
	fmt.Printf("  renamed %d files, rewrote %d references, re-encoded %d PNGs\n",
		len(res.Transform.Renames), len(res.Transform.Rewrites), len(res.Transform.PNGOptimized))
	fmt.Println("  shulker: in-place squash of ./RP complete")
}

func mustWd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "?"
	}
	return wd
}

func printWarnings(findings []pack.Finding, quiet bool) {
	if len(findings) == 0 || quiet {
		return
	}
	fmt.Printf("\n  notes (%d):\n", len(findings))
	for _, f := range findings {
		fmt.Printf("  - %s\n", f.String())
	}
}
