package graph

import (
	"path"
	"strings"

	"github.com/lettucebecaesar/shulker/internal/pack"
)

// PBR companion suffixes, verified against current vanilla
// (Mojang/bedrock-samples) plus the Microsoft PBR guide:
//
//	_mer        metalness / emissive / roughness   (RTX era, still supported)
//	_normal     surface normal map
//	_heightmap parallax height map
//	_mers       metalness / emissive / roughness / subsurface (Vibrant Visuals)
//
// The engine finds companions by suffix convention next to the color map —
// there is no JSON that ties them together. That is why sets must rename in
// lockstep: rename foo.png without foo_mer.png and the pack renders wrong
// with no error anywhere.
var PBRSuffixes = []string{"_mer", "_normal", "_heightmap", "_mers"}

// TextureSet is a color map plus the companion files that exist beside it.
type TextureSet struct {
	Color      string            // the color map path
	Companions map[string]string // suffix -> companion path (only those present)
}

// Members returns every member path of the set, color map first, companions
// in suffix order.
func (s *TextureSet) Members() []string {
	out := []string{s.Color}
	for _, suffix := range PBRSuffixes {
		if p, ok := s.Companions[suffix]; ok {
			out = append(out, p)
		}
	}
	return out
}

// IsPBRCompanion reports whether a file name is a PBR companion of some
// color map, and returns the suffix stripped.
func IsPBRCompanion(name string) (suffix, base string, ok bool) {
	stem := strings.TrimSuffix(name, path.Ext(name))
	for _, s := range PBRSuffixes {
		if strings.HasSuffix(stem, s) {
			return s, strings.TrimSuffix(stem, s), true
		}
	}
	return "", "", false
}

// buildSets groups color maps with their suffix-convention companions.
// Sets are canonical: built from sorted file order, grouped once, and
// independent of any particular transform.
func (g *Graph) buildSets() {
	// Only files under textures/ can carry PBR companions.
	var images []string
	for _, f := range g.Pack.Files {
		if f.Class != pack.ClassTexture {
			continue
		}
		// A file that is itself a companion is not a color map candidate.
		if _, _, isComp := IsPBRCompanion(path.Base(f.Path)); isComp {
			continue
		}
		images = append(images, f.Path)
	}
	// images is already in sorted order (Pack.Files is sorted).
	for _, color := range images {
		dir := path.Dir(color)
		stem := strings.TrimSuffix(path.Base(color), path.Ext(color))
		set := &TextureSet{Color: color, Companions: map[string]string{}}
		for _, suffix := range PBRSuffixes {
			for _, ext := range pack.ImageExts {
				cand := dir + "/" + stem + suffix + ext
				if f, _, ok := g.Pack.Lookup(cand); ok {
					set.Companions[suffix] = f.Path
					break
				}
			}
		}
		if len(set.Companions) == 0 {
			continue
		}
		g.sets = append(g.sets, set)
		for _, m := range set.Members() {
			g.setByMember[m] = set
		}
	}
}
