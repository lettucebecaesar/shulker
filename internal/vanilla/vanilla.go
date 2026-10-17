// Package vanilla embeds the frozen path universe of the vanilla resource
// pack — every vanilla file path plus every texture/sound reference target
// extracted by shulker's own graph parsers.
//
// Source: ZtechNetwork/MCBVanillaResourcePack, a maintained mirror of the
// complete shipped vanilla resource pack. It is a strict superset of
// Mojang's bedrock-samples (which, notably, ships no font/ or ui/ trees at
// all) and is regenerated into this artifact by tools/vanilla-gen.
//
// It is the v0.2 upgrade of vanilla-override protection: matching is
// EXACT (and extension-insensitive for cross-extension overrides) instead
// of the v0.1 broad-directory deny-list. Consequences:
//
//   - a pack file at a genuine vanilla path (textures/blocks/stone.png, or
//     a .tga shadowing vanilla's .png) is never renamed: the engine and
//     vanilla-side JSON may resolve it by exactly that path, and we cannot
//     see those references;
//   - custom assets that merely live under vanilla directories
//     (textures/blocks/my_custom_block.png) become free to rename —
//     vanilla cannot reference a path it does not have.
//
// Staleness is in the SAFE direction: a list from an older vanilla version
// only over-protects. Regenerate after major Bedrock releases with
// tools/vanilla-gen (see that file's header for the exact procedure) — the
// provenance commit is embedded in the artifact itself.
package vanilla

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"path"
	"strings"
	"sync"
)

//go:embed vanilla_paths.gz
var compressed []byte

var (
	once    sync.Once
	loaded  map[string]struct{} // exact paths (files + reference candidates)
	extless map[string]struct{} // same paths with extensions stripped
	prov    string
)

func load() {
	once.Do(func() {
		loaded = make(map[string]struct{}, 1<<16)
		extless = make(map[string]struct{}, 1<<16)
		zr, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			// A corrupt embedded asset is a build-time bug; failing loud is
			// the only honest option.
			panic("vanilla: embedded path list is not valid gzip: " + err.Error())
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			panic("vanilla: reading embedded path list: " + err.Error())
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "#") {
				prov = line
				continue
			}
			loaded[line] = struct{}{}
			if e := path.Ext(line); e != "" {
				extless[strings.TrimSuffix(line, e)] = struct{}{}
			}
		}
	})
}

// IsVanillaPath reports whether p exactly matches a vanilla resource pack
// path (file or reference target).
func IsVanillaPath(p string) bool {
	load()
	_, ok := loaded[p]
	return ok
}

// IsVanillaTarget reports whether p matches a vanilla path ignoring the
// extension — a pack shipping foo.tga where vanilla ships foo.png is an
// override of the same reference target.
func IsVanillaTarget(p string) bool {
	load()
	e := path.Ext(p)
	if e == "" {
		return false
	}
	_, ok := extless[strings.TrimSuffix(p, e)]
	return ok
}

// Count returns the number of frozen paths (diagnostics and tests).
func Count() int {
	load()
	return len(loaded)
}

// Provenance returns the bedrock-samples commit the list was generated
// from, e.g. "# bedrock-samples@46ba6ea985fb...".
func Provenance() string {
	load()
	return prov
}
