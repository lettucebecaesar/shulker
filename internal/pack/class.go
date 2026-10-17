package pack

// Class is the coarse content category of a pack file. It drives report
// buckets, zip compression choices, and transform eligibility.
type Class uint8

const (
	ClassUnknown   Class = iota
	ClassManifest        // manifest.json — stored verbatim, never transformed
	ClassIcon            // pack_icon.png — never modified
	ClassTexture         // images under textures/
	ClassSound           // audio under sounds/
	ClassLang            // *.lang — content untouched (lang keys are sacred)
	ClassFont            // font/** — engine addresses glyph sheets by fixed paths
	ClassSubpack         // subpacks/** — v0.1 leaves subpacks completely alone
	ClassJSON            // any .json outside the categories above
	ClassJSONFixed       // registry JSON at engine-fixed paths
	ClassImage           // image files outside textures/
	ClassAudio           // audio files outside sounds/
	ClassText            // other text-ish files
	ClassOther           // everything else
)

func (c Class) String() string {
	switch c {
	case ClassManifest:
		return "manifest"
	case ClassIcon:
		return "icon"
	case ClassTexture:
		return "textures"
	case ClassSound:
		return "sounds"
	case ClassLang:
		return "lang"
	case ClassFont:
		return "font"
	case ClassSubpack:
		return "subpacks"
	case ClassJSON:
		return "json"
	case ClassJSONFixed:
		return "json-registry"
	case ClassImage:
		return "image"
	case ClassAudio:
		return "audio"
	case ClassText:
		return "text"
	default:
		return "other"
	}
}

// Classify assigns the category for a normalized pack path.
//
// MCBE quirks encoded here:
//   - everything under subpacks/ is a complete nested pack (with its own
//     manifest and registries); v0.1 never looks inside, per the docs.
//   - font/** glyph sheets are addressed by hard-coded engine paths
//     (font/glyph_XX.png), so they get their own bucket.
func Classify(p string) Class {
	switch {
	case p == "manifest.json":
		return ClassManifest
	case p == "pack_icon.png":
		return ClassIcon
	case HasPrefixDir(p, "subpacks/"):
		return ClassSubpack
	case HasPrefixDir(p, "font/"):
		return ClassFont
	case HasPrefixDir(p, "textures/"):
		// Registry and texture_set JSON under textures/ (terrain_texture,
		// item_texture, flipbook, *.texture_set.json) is engine-fixed-path
		// content; plain images are the payload we optimize.
		switch {
		case IsImage(p):
			return ClassTexture
		case Ext(p) == ".json":
			return ClassJSONFixed
		}
		return ClassOther
	case HasPrefixDir(p, "sounds/"):
		if IsAudio(p) {
			return ClassSound
		}
		if Ext(p) == ".json" {
			return ClassJSONFixed
		}
		return ClassOther
	case HasPrefixDir(p, "texts/"):
		if Ext(p) == ".lang" || Ext(p) == ".json" {
			return ClassLang
		}
		return ClassOther
	}
	switch {
	case Ext(p) == ".json":
		if IsFixedPath(p) {
			return ClassJSONFixed
		}
		return ClassJSON
	case IsImage(p):
		return ClassImage
	case IsAudio(p):
		return ClassAudio
	case IsText(p):
		return ClassText
	}
	return ClassOther
}

// Compressible reports whether deflate helps this class. Already-compressed
// payloads (png/ogg/jpg) are STOREd — deflating them twice is pure CPU waste
// on the client's join path.
func (c Class) Compressible() bool {
	switch c {
	case ClassTexture, ClassSound, ClassImage, ClassAudio, ClassFont, ClassOther:
		return false
	default:
		return true
	}
}
