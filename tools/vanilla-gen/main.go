// Command vanilla-gen freezes the vanilla resource pack's path universe
// into a compressed list that shulker embeds (internal/vanilla).
//
// The frozen list makes vanilla-override protection exhaustive instead of
// heuristic: any pack file whose path matches a vanilla file — exactly, or
// extension-insensitively (a pack's .tga overriding vanilla's .png) — is
// never renamed and (by default) never re-encoded, while custom assets that
// merely live under vanilla directories (textures/blocks/my_block.png)
// become free to rename.
//
// SOURCE: ZtechNetwork/MCBVanillaResourcePack — a maintained mirror of the
// COMPLETE shipped vanilla resource pack. It is a strict superset of
// Mojang's bedrock-samples (verified: every bedrock-samples resource_pack
// path is present) and additionally ships what bedrock-samples omits
// entirely: font/ (glyph sheets, .ttf, smooth fontdata), ui/, atmospherics/,
// color_grading/, lighting/, fogs/, biomes/… Over-protection is the safe
// direction, so the list may lag upstream without risk.
//
// The list is the union of:
//  1. every file path in the pack (from git ls-tree — no blobs needed), and
//  2. every texture/sound REFERENCE target extracted by shulker's own
//     graph parsers from the vanilla JSON — including extension candidates
//     (.png/.tga/.jpg/… , .ogg/.wav/…) and *.texture_set.json sibling
//     references — so paths that vanilla references without shipping them
//     are covered too.
//
// Usage (from the repository root):
//
//	git clone --filter=blob:none --no-checkout --depth 1 \
//	    https://github.com/ZtechNetwork/MCBVanillaResourcePack.git /tmp/mcb
//	git -C /tmp/mcb sparse-checkout init --no-cone
//	git -C /tmp/mcb sparse-checkout set "/**/*.json" "/*.json"
//	git -C /tmp/mcb checkout master
//	go run ./tools/vanilla-gen -repo /tmp/mcb \
//	    -out internal/vanilla/vanilla_paths.gz
//
// Vanilla adds paths every release. The frozen list being stale is the SAFE
// direction (it only over-protects); regenerate after major Bedrock releases
// to keep unblocking new custom paths. The provenance commit is embedded as
// the first line of the artifact.
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lettucebecaesar/shulker/internal/graph"
	"github.com/lettucebecaesar/shulker/internal/pack"
)

// repoMetadata is excluded from the frozen list: it ships in the mirror
// repository but is not pack content.
func repoMetadata(p string) bool {
	return p == "README.md" || strings.HasPrefix(p, ".")
}

func main() {
	repo := flag.String("repo", "", "path to an MCBVanillaResourcePack clone (blob-filtered, JSON sparse-checkout)")
	out := flag.String("out", "internal/vanilla/vanilla_paths.gz", "output .gz file")
	flag.Parse()
	if *repo == "" {
		fmt.Fprintln(os.Stderr, "usage: vanilla-gen -repo <MCBVanillaResourcePack> [-out file.gz]")
		os.Exit(2)
	}

	commit := strings.TrimSpace(mustOutput("git", "-C", *repo, "rev-parse", "HEAD"))

	// 1. Every vanilla pack file path (tree objects only — no blob download).
	files := strings.Split(strings.TrimSpace(
		mustOutput("git", "-C", *repo, "ls-tree", "-r", "--name-only", "HEAD")), "\n")
	paths := make(map[string]struct{}, len(files))
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" || repoMetadata(f) {
			continue
		}
		paths[f] = struct{}{}
	}
	fmt.Printf("vanilla files: %d\n", len(paths))

	// 2. Reference targets via shulker's own graph parsers, run over the
	// sparse vanilla pack (JSON only). Parse failures are fatal: if our
	// parser cannot read vanilla, we want to fix shulker, not ship a
	// partial list.
	p, err := pack.LoadDir(*repo)
	if err != nil {
		bail("loading sparse vanilla pack: %v", err)
	}
	docs, parseErrs := graph.ParseAll(p)
	if len(parseErrs) > 0 {
		for path, e := range parseErrs {
			fmt.Fprintf(os.Stderr, "parse error in %s: %v\n", path, e)
		}
		bail("vanilla JSON must be fully parseable by shulker (shulker bug)", nil)
	}
	g, err := graph.Build(p, docs, parseErrs)
	if err != nil {
		bail("graph over vanilla pack: %v", err)
	}

	candidates := 0
	for _, e := range g.Edges {
		if e.Kind != graph.KindTexturePath && e.Kind != graph.KindSoundPath {
			continue
		}
		exts := pack.ImageExts
		root := "textures"
		if e.Kind == graph.KindSoundPath {
			exts = pack.AudioExts
			root = "sounds"
		}
		raw := strings.ReplaceAll(e.Raw, "\\", "/")
		raw = strings.TrimSuffix(raw, "/")

		var stems []string
		switch {
		case e.Form.Prefixed:
			// Vanilla style: "textures/blocks/stone" / "sounds/mob/pig/say1".
			stems = append(stems, raw)
		case strings.HasSuffix(e.File, ".texture_set.json"):
			// Texture-set values are sibling names relative to the set file.
			stems = append(stems, path.Dir(e.File)+"/"+raw)
		default:
			// Atlas/sound-relative without the root prefix.
			stems = append(stems, root+"/"+raw)
		}
		for _, stem := range stems {
			if e.Form.WithExt {
				paths[stem] = struct{}{}
				candidates++
				continue
			}
			for _, ext := range exts {
				paths[stem+ext] = struct{}{}
				candidates++
			}
		}
	}
	fmt.Printf("reference candidates added: %d (edges: %d)\n", candidates, len(g.Edges))

	// Deterministic output: sorted, provenance header first.
	list := make([]string, 0, len(paths))
	for p := range paths {
		if p == "" || strings.Contains(p, "\n") || repoMetadata(p) {
			continue
		}
		list = append(list, p)
	}
	sort.Strings(list)

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		bail("%v", err)
	}
	f, err := os.Create(*out)
	if err != nil {
		bail("%v", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			bail("closing output: %v", cerr)
		}
	}()
	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		bail("%v", err)
	}
	// Gzip writes are checked: a truncated frozen list would protect
	// nothing.
	if _, err := fmt.Fprintf(zw, "# MCBVanillaResourcePack@%s\n", commit); err != nil {
		bail("%v", err)
	}
	for _, p := range list {
		if _, err := fmt.Fprintln(zw, p); err != nil {
			bail("%v", err)
		}
	}
	if err := zw.Close(); err != nil {
		bail("%v", err)
	}
	fmt.Printf("wrote %s: %d paths (from MCBVanillaResourcePack@%s)\n", *out, len(list), commit[:12])
}

func mustOutput(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		bail("running %s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func bail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "vanilla-gen: "+format+"\n", args...)
	os.Exit(1)
}
