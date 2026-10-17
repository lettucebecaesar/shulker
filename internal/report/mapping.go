// Package report renders shulker's human-facing output: the before/after
// size table (the demo moment) and the Sourcemap-style mapping file that
// pairs every squashed pack with the knowledge to undo its renames.
//
// The mapping file is deliberately complete: it records every rename, every
// reference rewrite with its JSON pointer, and every skip decision. Creators
// keep their original pack as the source of truth — the squashed artifact is
// a build output, never something you hand-edit.
package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/pack"
)

// Mapping is the versioned, deterministic record of a squash.
type Mapping struct {
	Version      int
	Generator    string
	Renames      []graph.Rename
	Rewrites     []graph.Rewrite
	PNGOptimized []string
	Skips        []MappingSkip
}

// MappingSkip records a conservative decision to leave a file alone.
type MappingSkip struct {
	Path   string
	Reason string
}

// NewMapping snapshots a transform result into a Mapping.
func NewMapping(generator string, renames []graph.Rename, rewrites []graph.Rewrite, pngOptimized map[string]bool, skips map[string]string) *Mapping {
	m := &Mapping{
		Version:   1,
		Generator: generator,
		Renames:   append([]graph.Rename(nil), renames...),
		Rewrites:  append([]graph.Rewrite(nil), rewrites...),
	}
	sort.Slice(m.Renames, func(i, j int) bool { return m.Renames[i].From < m.Renames[j].From })
	sort.Slice(m.Rewrites, func(i, j int) bool {
		if m.Rewrites[i].File != m.Rewrites[j].File {
			return m.Rewrites[i].File < m.Rewrites[j].File
		}
		return m.Rewrites[i].Pointer < m.Rewrites[j].Pointer
	})
	for path := range pngOptimized {
		m.PNGOptimized = append(m.PNGOptimized, path)
	}
	sort.Strings(m.PNGOptimized)
	for path, reason := range skips {
		m.Skips = append(m.Skips, MappingSkip{Path: path, Reason: reason})
	}
	sort.Slice(m.Skips, func(i, j int) bool { return m.Skips[i].Path < m.Skips[j].Path })
	return m
}

// Encode renders the mapping as deterministic compact JSON (built through
// jsonx so field order is fixed and content is stable across runs).
func (m *Mapping) Encode() []byte {
	root := &jsonx.Object{}
	root.Members = append(root.Members, jsonx.Member{Key: "version", Val: &jsonx.Number{Raw: "1"}})
	root.Members = append(root.Members, jsonx.Member{Key: "generator", Val: &jsonx.String{Str: m.Generator}})

	renames := &jsonx.Array{}
	for _, rn := range m.Renames {
		o := &jsonx.Object{}
		o.Members = append(o.Members,
			jsonx.Member{Key: "from", Val: &jsonx.String{Str: rn.From}},
			jsonx.Member{Key: "to", Val: &jsonx.String{Str: rn.To}},
		)
		renames.Items = append(renames.Items, o)
	}
	root.Members = append(root.Members, jsonx.Member{Key: "renames", Val: renames})

	rewrites := &jsonx.Array{}
	for _, rw := range m.Rewrites {
		o := &jsonx.Object{}
		o.Members = append(o.Members,
			jsonx.Member{Key: "file", Val: &jsonx.String{Str: rw.File}},
			jsonx.Member{Key: "pointer", Val: &jsonx.String{Str: rw.Pointer}},
			jsonx.Member{Key: "from", Val: &jsonx.String{Str: rw.From}},
			jsonx.Member{Key: "to", Val: &jsonx.String{Str: rw.To}},
		)
		rewrites.Items = append(rewrites.Items, o)
	}
	root.Members = append(root.Members, jsonx.Member{Key: "rewrites", Val: rewrites})

	pngs := &jsonx.Array{}
	for _, p := range m.PNGOptimized {
		pngs.Items = append(pngs.Items, &jsonx.String{Str: p})
	}
	root.Members = append(root.Members, jsonx.Member{Key: "png_optimized", Val: pngs})

	skips := &jsonx.Array{}
	for _, s := range m.Skips {
		o := &jsonx.Object{}
		o.Members = append(o.Members,
			jsonx.Member{Key: "path", Val: &jsonx.String{Str: s.Path}},
			jsonx.Member{Key: "reason", Val: &jsonx.String{Str: s.Reason}},
		)
		skips.Items = append(skips.Items, o)
	}
	root.Members = append(root.Members, jsonx.Member{Key: "skips", Val: skips})
	return jsonx.Encode(root)
}

// ParseMapping reads a mapping file (tolerantly — creators may reformat it).
func ParseMapping(data []byte) (*Mapping, error) {
	root, err := jsonx.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("mapping file is not valid JSON: %w", err)
	}
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return nil, fmt.Errorf("mapping file must be a JSON object")
	}
	m := &Mapping{}
	if v, ok := obj.Get("version"); ok {
		if raw, isNum := jsonx.AsNumber(v); isNum {
			if raw != "1" {
				return nil, fmt.Errorf("unsupported mapping version %s (this shulker understands version 1)", raw)
			}
			m.Version = 1
		}
	} else {
		return nil, fmt.Errorf("mapping file has no version")
	}
	if v, ok := obj.Get("generator"); ok {
		m.Generator, _ = jsonx.AsString(v)
	}
	if v, ok := obj.Get("renames"); ok {
		if arr, isArr := jsonx.AsArray(v); isArr {
			for _, item := range arr.Items {
				o, isObj := jsonx.AsObject(item)
				if !isObj {
					continue
				}
				from, _ := getString(o, "from")
				to, _ := getString(o, "to")
				m.Renames = append(m.Renames, graph.Rename{From: from, To: to})
			}
		}
	}
	if v, ok := obj.Get("rewrites"); ok {
		if arr, isArr := jsonx.AsArray(v); isArr {
			for _, item := range arr.Items {
				o, isObj := jsonx.AsObject(item)
				if !isObj {
					continue
				}
				file, _ := getString(o, "file")
				pointer, _ := getString(o, "pointer")
				from, _ := getString(o, "from")
				to, _ := getString(o, "to")
				m.Rewrites = append(m.Rewrites, graph.Rewrite{File: file, Pointer: pointer, From: from, To: to})
			}
		}
	}
	if v, ok := obj.Get("png_optimized"); ok {
		if arr, isArr := jsonx.AsArray(v); isArr {
			for _, item := range arr.Items {
				if s, isStr := jsonx.AsString(item); isStr {
					m.PNGOptimized = append(m.PNGOptimized, s)
				}
			}
		}
	}
	if v, ok := obj.Get("skips"); ok {
		if arr, isArr := jsonx.AsArray(v); isArr {
			for _, item := range arr.Items {
				o, isObj := jsonx.AsObject(item)
				if !isObj {
					continue
				}
				path, _ := getString(o, "path")
				reason, _ := getString(o, "reason")
				m.Skips = append(m.Skips, MappingSkip{Path: path, Reason: reason})
			}
		}
	}
	return m, nil
}

func getString(o *jsonx.Object, key string) (string, bool) {
	v, has := o.Get(key)
	if !has {
		return "", false
	}
	s, isStr := jsonx.AsString(v)
	return s, isStr
}

// ReverseApply undoes a squash on a loaded pack: files move back to their
// original paths and every logged rewrite is inverted at its JSON pointer.
//
// What reverse cannot restore (by design, and stated honestly): JSON that was
// minified stays minified, and PNGs that were re-encoded keep their optimized
// bytes — those transforms are lossy with respect to author formatting, and
// the original pack remains the source of truth for that. The mapping file
// re-derives the ORIGINAL LAYOUT, which is exactly what invariant-driven
// tooling needs.
func (m *Mapping) ReverseApply(p *pack.Pack) (*pack.Pack, error) {
	back := map[string]string{} // squashed path -> original path
	for _, rn := range m.Renames {
		back[rn.To] = rn.From
	}

	// Move files back first, computing the final path list.
	var files []*pack.File
	for _, f := range p.Files {
		orig, renamed := back[f.Path]
		nf := &pack.File{Data: f.Data}
		if renamed {
			nf.Path = orig
		} else {
			nf.Path = f.Path
		}
		files = append(files, nf)
	}

	// Invert rewrites. Rewrite.File is the ORIGINAL path of the JSON file,
	// and the move above has already restored every file to its original
	// path, so rewrites address the file list directly.
	for _, rw := range m.Rewrites {
		target := findFile(files, rw.File)
		if target == nil {
			return nil, fmt.Errorf("mapping references %s which is not in this pack", rw.File)
		}
		doc, err := jsonx.Parse(target.Data)
		if err != nil {
			return nil, fmt.Errorf("reverse: %s does not parse: %w", rw.File, err)
		}
		node, ok := jsonx.GetByPointer(doc, rw.Pointer)
		if !ok {
			return nil, fmt.Errorf("mapping pointer %s not found in %s", rw.Pointer, rw.File)
		}
		s, isStr := jsonx.AsStringNode(node)
		if !isStr {
			return nil, fmt.Errorf("mapping pointer %s in %s is not a string", rw.Pointer, rw.File)
		}
		if s.Str != rw.To {
			return nil, fmt.Errorf("mapping mismatch at %s in %s: found %q, expected %q — is this the pack this mapping belongs to?",
				rw.Pointer, rw.File, s.Str, rw.To)
		}
		s.Set(rw.From)
		target.Data = jsonx.Encode(doc)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return pack.FromFiles("reversed", files)
}

func findFile(files []*pack.File, path string) *pack.File {
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	return nil
}

// Summarize renders the mapping for `shulker map` output. Write errors to
// the summary writer are ignored deliberately: this is display output.
func (m *Mapping) Summarize(w io.Writer) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
	p("mapping version %d, generated by %s\n", m.Version, m.Generator)
	p("  renames:       %d\n", len(m.Renames))
	p("  ref rewrites:  %d\n", len(m.Rewrites))
	p("  png re-encoded: %d\n", len(m.PNGOptimized))
	p("  skipped files: %d\n", len(m.Skips))
	if len(m.Renames) > 0 {
		p("\nrenames (from -> to):\n")
		for _, rn := range m.Renames {
			p("  %s -> %s\n", rn.From, rn.To)
		}
	}
	if len(m.Skips) > 0 {
		p("\nleft untouched (conservative by design):\n")
		for _, s := range m.Skips {
			p("  %s — %s\n", s.Path, s.Reason)
		}
	}
	if len(m.Rewrites) > 0 {
		p("\nreference rewrites: %d (undo with `shulker map --reverse`)\n", len(m.Rewrites))
	}
}
