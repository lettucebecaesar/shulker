package pack

import (
	"path"
	"strings"
)

// ImageExts are the image formats Bedrock resolves for texture references.
// PBR companions in the current vanilla pack are .tga while color maps are
// often .png, so resolution must try several extensions per reference.
var ImageExts = []string{".png", ".tga", ".jpg", ".jpeg", ".webp"}

// AudioExts are the audio formats found in RP sound/ trees (.ogg dominates;
// Vorbis is what the engine actually requires).
var AudioExts = []string{".ogg", ".wav", ".mp3", ".fsb"}

// TextExts are extensions we deflate rather than store: already-compressed
// payloads (png/ogg/jpg) gain nothing but CPU cost from a second deflate.
var TextExts = []string{
	".json", ".lang", ".txt", ".csv", ".material", ".fsh", ".vsh",
	".hlsl", ".mcmeta", ".md", ".html", ".xml",
}

func Ext(name string) string { return strings.ToLower(path.Ext(name)) }

func IsImage(name string) bool {
	e := Ext(name)
	for _, x := range ImageExts {
		if e == x {
			return true
		}
	}
	return false
}

func IsAudio(name string) bool {
	e := Ext(name)
	for _, x := range AudioExts {
		if e == x {
			return true
		}
	}
	return false
}

func IsText(name string) bool {
	e := Ext(name)
	for _, x := range TextExts {
		if e == x {
			return true
		}
	}
	return false
}

// NormalizePath canonicalizes a zip entry or relative file path:
// forward slashes only, no drive letters, no leading '/', './' or '..',
// no empty segments. Bedrock mounts these paths on case-sensitive file
// systems (Android), so case is preserved exactly as authored.
func NormalizePath(raw string) (string, error) {
	p := strings.ReplaceAll(raw, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	if p == "" || p == "." {
		return "", &PathError{Raw: raw, Reason: "empty path"}
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", &PathError{Raw: raw, Reason: "drive-letter path"}
	}
	if strings.Contains(p, ":") {
		return "", &PathError{Raw: raw, Reason: "colon in path"}
	}
	var segs []string
	for _, s := range strings.Split(p, "/") {
		switch s {
		case "", ".":
			// collapse duplicate slashes and '.'
		case "..":
			return "", &PathError{Raw: raw, Reason: "parent-directory segment"}
		default:
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return "", &PathError{Raw: raw, Reason: "empty path"}
	}
	return strings.Join(segs, "/"), nil
}

// PathError explains why an entry path is unusable. A pack with broken
// paths is rejected loudly rather than silently half-loaded.
type PathError struct {
	Raw    string
	Reason string
}

func (e *PathError) Error() string {
	return "bad pack path " + strconv(e.Raw) + ": " + e.Reason
}

func strconv(s string) string { return `"` + s + `"` }

// FixedPaths are registry files the engine looks up by exact path. They can
// be minified but never renamed. Anything not on this list that the engine
// also fixates on (ui/*.json, font glyph sheets) is handled by the rename
// deny-list in the graph package instead.
var FixedPaths = map[string]bool{
	"manifest.json":                     true,
	"blocks.json":                       true,
	"sounds.json":                       true,
	"biomes_client.json":                true,
	"textures/terrain_texture.json":     true,
	"textures/item_texture.json":        true,
	"textures/flipbook_textures.json":   true,
	"sounds/sound_definitions.json":     true,
	"textures_list.json":                true,
	"texture_list.json":                true, // marketplace misspelling variant
	"texts/languages.json":              true,
	"materials/materials.json":          true,
}

// IsFixedPath reports whether the engine resolves this path directly.
func IsFixedPath(p string) bool { return FixedPaths[p] }

// HasPrefixDir reports whether path p sits anywhere inside the directory dir
// (dir given with trailing slash).
func HasPrefixDir(p, dir string) bool { return strings.HasPrefix(p, dir) }

// Parent returns the directory portion of a slash path ("" for root files).
func Parent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}
