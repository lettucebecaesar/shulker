package graph

import (
	"path"
	"strings"

	"github.com/shulker-tools/shulker/internal/vanilla"
)

// DenyPrefixes are engine-hardcoded subtrees where the engine resolves
// content by fixed paths or path conventions that no pack JSON reveals
// (sky/environment lookups, colormaps, glyph sheets, json-ui file names,
// lang). Files inside are never renamed regardless of pack-internal
// references.
//
// v0.2 change: the broad "textures/blocks/" and "textures/particle/"
// prefixes are GONE. Actual vanilla paths under those trees are now
// protected exhaustively by the frozen vanilla path list (internal/vanilla,
// exact + extension-insensitive matching), which simultaneously unblocks
// custom assets that merely live under vanilla directories — a block mod
// can now rename its textures/blocks/my_custom_block.png.
var DenyPrefixes = []string{
	"textures/environment/", // engine-hardcoded (sky, moon, sun, clouds…)
	"textures/colormap/",    // engine colormap lookups (birch/pine…)
	"textures/misc/",        // vanilla item_texture references (potions, missing_texture…)
	"font/",                 // glyph sheets addressed by fixed code paths
	"texts/",                // lang files; keys are sacred anyway
	"ui/",                   // engine references ui/*.json by fixed name
	"subpacks/",             // untouchable nested packs (unless --squash-subpacks)
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

// IsVanillaPath reports whether a pack path matches the frozen vanilla
// resource pack universe — exactly, or extension-insensitively (a pack's
// .tga shadowing vanilla's .png at the same reference target). The planner
// refuses to rename such files: the engine and vanilla-side JSON may
// resolve them by exactly that path, and those references are invisible to
// us. See internal/vanilla for provenance and regeneration.
func IsVanillaPath(p string) bool {
	return vanilla.IsVanillaPath(p) || vanilla.IsVanillaTarget(p)
}

// NeverRename are individual engine-fixed files. Registries can be minified
// and their path references rewritten, but the files themselves stay.
func NeverRename(p string) bool {
	switch p {
	case "manifest.json", "pack_icon.png":
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
