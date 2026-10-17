package graph

import (
	"path"
	"strings"

	"github.com/shulker-tools/shulker/internal/pack"
)

// resolveTexture maps a texture reference string to a pack file.
//
// MCBE quirks handled:
//   - references are written both pack-root-relative ("textures/entity/pig")
//     and atlas-relative ("entity/pig"); we try both.
//   - references usually omit the extension; the engine tries .png, .tga,
//     .jpg in that order (PBR companions in current vanilla are .tga while
//     color maps are .png, so the extension set must be permissive).
//   - case mismatches are common in community packs (they work on Windows,
//     break on Android). We resolve case-insensitively and note the fix-up:
//     after a rewrite the reference is exact, which is strictly safer.
func (g *Graph) resolveTexture(raw string) (target string, form PathForm, ok bool) {
	return g.resolveAsset(raw, "textures", pack.ImageExts)
}

// resolveSound maps a sound reference to a pack file. Vanilla
// sound_definitions paths are pack-root-relative WITH the "sounds/" prefix
// ("sounds/mob/pig/say1"); older packs and some tooling drop the prefix.
func (g *Graph) resolveSound(raw string) (target string, form PathForm, ok bool) {
	return g.resolveAsset(raw, "sounds", pack.AudioExts)
}

func (g *Graph) resolveAsset(raw, root string, exts []string) (string, PathForm, bool) {
	norm := strings.TrimSpace(raw)
	norm = strings.ReplaceAll(norm, "\\", "/")
	norm = strings.TrimSuffix(norm, "/")
	if norm == "" {
		return "", PathForm{}, false
	}
	// Prefix detection is case-insensitive: community packs ship refs like
	// "Textures/Custom/Foo" that work on Windows and break on Android. When
	// we rewrite such a ref it comes back exact-case, which is strictly
	// safer than what the author shipped.
	prefixed := len(norm) > len(root) && strings.EqualFold(norm[:len(root)], root) && norm[len(root)] == '/'
	form := PathForm{Prefixed: prefixed}

	// The reference may carry its own extension.
	base := norm
	hasExt := false
	if e := strings.ToLower(path.Ext(base)); extIn(e, exts) {
		hasExt = true
	}
	form.WithExt = hasExt

	// Candidate roots: with prefix -> pack root; without -> inside the
	// root dir.
	var cands []string
	if prefixed {
		cands = append(cands, base)
	} else {
		cands = append(cands, root+"/"+base)
	}
	// Some authors write "textures/textures/…" — tolerate a doubled prefix
	// by also trying the trimmed form.
	if prefixed {
		inner := strings.TrimPrefix(base, root+"/")
		if strings.HasPrefix(inner, root+"/") {
			cands = append(cands, inner)
		}
	}
	if hasExt {
		for _, c := range cands {
			if f, _, ok := g.Pack.Lookup(c); ok {
				return f.Path, form, true
			}
		}
		return "", form, false
	}
	for _, c := range cands {
		for _, ext := range exts {
			if f, _, ok := g.Pack.Lookup(c + ext); ok {
				return f.Path, form, true
			}
		}
	}
	return "", form, false
}

func extIn(e string, exts []string) bool {
	for _, x := range exts {
		if e == x {
			return true
		}
	}
	return false
}

// resolveIdentifier finds the file that provides an identifier.
func (g *Graph) resolveIdentifier(id string, index map[string][]string) (string, bool) {
	files := index[id]
	if len(files) == 0 {
		return "", false
	}
	// Deterministic: first provider in sorted order (files slice is built in
	// sorted pack order).
	return files[0], true
}

// resolveShortName checks atlas short-name registries (terrain + item +
// flipbook atlas_tile values).
func (g *Graph) resolveShortName(name string) (string, bool) {
	files := g.shortNameIndex[name]
	if len(files) == 0 {
		return "", false
	}
	return files[0], true
}

// RenderPathForm renders the rewritten reference for a renamed target,
// preserving the authorial form of the original.
func RenderPathForm(form PathForm, root, newTarget string) string {
	rel := strings.TrimPrefix(newTarget, root+"/")
	if !form.WithExt {
		rel = trimExt(rel)
	}
	if form.Prefixed {
		return root + "/" + rel
	}
	return rel
}

func trimExt(p string) string {
	dot := strings.LastIndexByte(p, '.')
	slash := strings.LastIndexByte(p, '/')
	if dot > slash && dot >= 0 {
		return p[:dot]
	}
	return p
}
