package graph

import (
	"path"
	"strings"
)

// DenyPrefixes are vanilla-sensitive subtrees where invisible vanilla-side
// references are known to be dense. A pack file inside these prefixes is
// never renamed, even when pack-internal references exist, because the
// engine may also resolve these paths from vanilla JSON we cannot see.
//
// This list is the documented, honest v0.1 approximation of invariant 3
// ("vanilla-override protection"). The zero-inbound-edge rule below already
// protects classic override textures (they have no pack-internal refs); the
// deny-list guards the residual case: files that have BOTH pack-internal
// refs AND possible vanilla refs (e.g. a pack that redefines the "stone"
// atlas short name to point at its own textures/blocks/stone.png).
var DenyPrefixes = []string{
	"textures/blocks/",      // vanilla blocks.json -> vanilla terrain_texture
	"textures/environment/", // engine-hardcoded (sky, moon, sun, clouds…)
	"textures/particle/",    // vanilla particle JSON references these by path
	"textures/colormap/",    // engine colormap lookups (birch/pine…)
	"textures/misc/",        // vanilla item_texture references (potions, missing_texture…)
	"font/",                 // glyph sheets addressed by fixed code paths
	"texts/",                // lang files; keys are sacred anyway
	"ui/",                   // engine references ui/*.json by fixed name
	"subpacks/",             // untouchable nested packs in v0.1
}

// IsDenyListed reports whether a pack path sits in a rename-denied subtree.
func IsDenyListed(p string) bool {
	for _, d := range DenyPrefixes {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	return false
}

// NeverRename are individual engine-fixed files. Registries can be minified
// and their path references rewritten, but the files themselves stay.
func NeverRename(p string) bool {
	switch {
	case p == "manifest.json", p == "pack_icon.png":
		return true
	}
	if strings.HasSuffix(p, ".texture_set.json") {
		// Texture-set files pair with their color map by NAME+directory
		// (iron_block.texture_set.json sits next to iron_block.png).
		// Renaming either side of that pairing is not something v0.1
		// understands, so the set file stays put.
		return true
	}
	if IsDenyListed(p) {
		return true
	}
	switch KindOf(p) {
	case KindTerrainAtlas, KindItemAtlas, KindFlipbook, KindBlocksFile,
		KindSoundDefinitions, KindSoundsEvents, KindTexturesList,
		KindMaterials, KindManifest, KindLanguages:
		return true
	}
	return false
}

// PreservedSuffix returns the file-name suffix a renamed file must keep.
//
// Conventional double extensions are load-bearing for identifier-scanned
// directories: geometry is *.geo.json, animations *.animation.json, etc.
// The engine may (or may not) glob on those, and community tooling
// certainly does. Renaming pig.geo.json -> _0.geo.json keeps every glob
// pattern matching; renaming it to _0.json might not.
//
// The rule: if the extension is .json and the stem contains a dot, keep
// from the innermost dot ("look_at_target.animation.json" ->
// ".animation.json"); otherwise keep the last extension.
func PreservedSuffix(name string) string {
	base := path.Base(name)
	if strings.HasSuffix(base, ".json") {
		stem := strings.TrimSuffix(base, ".json")
		if i := strings.LastIndexByte(stem, '.'); i >= 0 {
			return stem[i:] + ".json" // e.g. ".geo.json"
		}
		return ".json"
	}
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		return base[i:] // ".png", ".ogg", ".tga", …
	}
	return ""
}
