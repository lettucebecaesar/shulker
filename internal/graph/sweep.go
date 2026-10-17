package graph

import (
	"path"
	"strings"

	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/molang"
	"github.com/shulker-tools/shulker/internal/pack"
)

// sweep is the conservative backstop of the whole design.
//
// Every JSON string that (a) is not already covered by a schema-extracted
// edge, (b) is not a Molang expression (sacred), and (c) resolves to a real
// pack texture or sound file becomes an edge on that target:
//
//   - from json-ui files: REWRITEABLE — in json-ui, any string that resolves
//     to an actual pack texture (including "$variable" initializer values,
//     which flow into "texture" bindings) IS a texture reference, and the
//     engine resolves it exactly like a path.
//   - from any other file: a BLOCKER. We do not know what that string means
//     to the engine, so the referenced asset must keep its path. Unknown
//     JSON files referencing assets therefore freeze those assets' paths by
//     design (invariant 3: 100% of inbound references rewritten, or no
//     rename).
func (g *Graph) sweep(file string, root jsonx.Value, seen edgeSeen) {
	rewriteable := g.byFile[file] != nil && g.byFile[file].Kind == KindUIFile
	// The walk callback never returns an error (it only records edges).
	_ = jsonx.Walk(root, func(ptr string, v jsonx.Value) error {
		s, ok := jsonx.AsStringNode(v)
		if !ok {
			return nil
		}
		if seen != nil && seen(file, ptr) {
			return nil
		}
		if molang.IsExpression(s.Str) {
			// Hard invariant 5: Molang string literals are sacred.
			return nil
		}
		if target, form, ok := g.resolveTexture(s.Str); ok {
			g.addEdge(file, ptr, KindTexturePath, s.Str, target, form, rewriteable, s)
			return nil
		}
		if target, form, ok := g.resolveSound(s.Str); ok {
			g.addEdge(file, ptr, KindSoundPath, s.Str, target, form, rewriteable, s)
		}
		return nil
	})
}

// resolveSibling resolves a texture_set value: a bare sibling name relative
// to the texture_set file's own directory.
func (g *Graph) resolveSibling(dir, raw string) (string, bool) {
	norm := strings.ReplaceAll(raw, "\\", "/")
	for _, ext := range pack.ImageExts {
		if f, _, ok := g.Pack.Lookup(dir + "/" + norm + ext); ok {
			return f.Path, true
		}
	}
	// The value may already carry an extension.
	if f, _, ok := g.Pack.Lookup(dir + "/" + norm); ok {
		return f.Path, true
	}
	return "", false
}

// esc is jsonx.EscapePointerToken, aliased for the extractor call sites.
func esc(s string) string { return jsonx.EscapePointerToken(s) }

// parentDir returns the directory of a slash path ("" for root files).
func parentDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// itoa renders small indexes for pointers.
func itoa(i int) string {
	if i >= 0 && i < 10 {
		return string(rune('0' + i))
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[n:])
}

// BaseStem is the file name without extension.
func BaseStem(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, path.Ext(base))
}
