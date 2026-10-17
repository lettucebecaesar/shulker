package graph

import (
	"path"
	"strings"
)

// FileKind is the schema role of a JSON file in the pack. It selects which
// extractor runs. Ground truth for every shape here was verified against
// Mojang's bedrock-samples vanilla resource pack (October 2026 checkout).
type FileKind uint8

const (
	KindUnknownFile          FileKind = iota
	KindManifest                      // manifest.json — never part of the graph
	KindTerrainAtlas                  // textures/terrain_texture.json
	KindItemAtlas                     // textures/item_texture.json
	KindFlipbook                      // textures/flipbook_textures.json
	KindTextureSetFile                // textures/**/**.texture_set.json (Vibrant Visuals PBR)
	KindBlocksFile                    // blocks.json
	KindSoundDefinitions              // sounds/sound_definitions.json
	KindSoundsEvents                  // sounds.json
	KindTexturesList                  // textures_list.json (marketplace file inventory)
	KindEntityFile                    // entity/**.json — minecraft:client_entity
	KindAttachableFile                // attachables/**.json — same client entity schema
	KindGeometryFile                  // models/**.json — minecraft:geometry or legacy
	KindAnimationFile                 // animations/**.json
	KindAnimControllerFile            // animation_controllers/**.json
	KindRenderControllerFile          // render_controllers/**.json
	KindParticleFile                  // particles/**.json
	KindUIFile                        // ui/**.json — json-ui
	KindMaterials                     // materials/materials.json
	KindLanguages                     // texts/languages.json
)

// KindOf classifies a normalized pack path.
func KindOf(p string) FileKind {
	switch {
	case p == "manifest.json":
		return KindManifest
	case p == "textures/terrain_texture.json":
		return KindTerrainAtlas
	case p == "textures/item_texture.json":
		return KindItemAtlas
	case p == "textures/flipbook_textures.json":
		return KindFlipbook
	case p == "blocks.json":
		return KindBlocksFile
	case p == "sounds/sound_definitions.json":
		return KindSoundDefinitions
	case p == "sounds.json":
		return KindSoundsEvents
	case p == "textures_list.json" || p == "texture_list.json":
		return KindTexturesList
	case p == "materials/materials.json":
		return KindMaterials
	case p == "texts/languages.json":
		return KindLanguages
	case strings.HasPrefix(p, "subpacks/"):
		return KindUnknownFile // untouchable in v0.1
	}
	if strings.HasSuffix(p, ".texture_set.json") && strings.HasPrefix(p, "textures/") {
		return KindTextureSetFile
	}
	dir := path.Dir(p)
	if path.Ext(p) != ".json" {
		return KindUnknownFile
	}
	switch dir {
	case "entity":
		return KindEntityFile
	case "attachables":
		return KindAttachableFile
	case "models", "models/entity":
		return KindGeometryFile
	case "animations":
		return KindAnimationFile
	case "animation_controllers":
		return KindAnimControllerFile
	case "render_controllers":
		return KindRenderControllerFile
	case "particles":
		return KindParticleFile
	case "ui":
		return KindUIFile
	}
	// animations can nest (animations/sub/x.json still works — the engine
	// scans recursively); same for entity and geometry trees.
	switch {
	case strings.HasPrefix(p, "animations/"):
		return KindAnimationFile
	case strings.HasPrefix(p, "animation_controllers/"):
		return KindAnimControllerFile
	case strings.HasPrefix(p, "render_controllers/"):
		return KindRenderControllerFile
	case strings.HasPrefix(p, "particles/"):
		return KindParticleFile
	case strings.HasPrefix(p, "entity/"):
		return KindEntityFile
	case strings.HasPrefix(p, "attachables/"):
		return KindAttachableFile
	case strings.HasPrefix(p, "models/"):
		return KindGeometryFile
	case strings.HasPrefix(p, "ui/"):
		return KindUIFile
	}
	return KindUnknownFile
}

// IdentifierScannedDirs are directories the engine loads by scanning for
// identifiers rather than by fixed paths. Files in these directories can be
// renamed (basename only, conventional suffix preserved) even with zero
// inbound path references, because no vanilla-side JSON can path-reference
// them — they are only ever looked up by the identifiers they contain, and
// v0.1 never rewrites identifiers.
var IdentifierScannedDirs = []string{
	"entity/", "attachables/", "animations/", "animation_controllers/",
	"render_controllers/", "particles/", "models/",
}

// IsIdentifierScanned reports whether a path lives in a directory the engine
// scans by identifier.
func IsIdentifierScanned(p string) bool {
	for _, d := range IdentifierScannedDirs {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	return false
}
