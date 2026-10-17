package graph

import (
	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/pack"
)

// ParseAll parses every JSON file in the pack (except the manifest, which is
// stored verbatim and never re-serialized, and subpacks, which v0.1 leaves
// completely alone). It is the shared front half of the pipeline: the graph,
// the transforms, and the validator all consume these same trees so that
// parse once, transform everywhere holds.
func ParseAll(p *pack.Pack) (map[string]jsonx.Value, map[string]error) {
	docs := make(map[string]jsonx.Value)
	errs := make(map[string]error)
	for _, f := range p.Files {
		if f.Path == pack.ManifestPath || f.Class == pack.ClassSubpack {
			continue
		}
		if pack.Ext(f.Path) != ".json" {
			continue
		}
		v, err := jsonx.Parse(f.Data)
		if err != nil {
			errs[f.Path] = err
			continue
		}
		docs[f.Path] = v
	}
	return docs, errs
}
