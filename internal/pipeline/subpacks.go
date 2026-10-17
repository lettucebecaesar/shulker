package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lettucebecaesar/shulker/internal/graph"
	"github.com/lettucebecaesar/shulker/internal/pack"
	"github.com/lettucebecaesar/shulker/internal/transform"
)

// subpackDirs lists the top-level subpacks/<dir> members that contain a
// manifest.json, in sorted order. Only ONE level is considered: subpacks
// of subpacks do not exist in the format, and the parent loader treats
// everything under subpacks/ as opaque anyway.
func subpackDirs(p *pack.Pack) []string {
	seen := map[string]bool{}
	for _, f := range p.Files {
		if !strings.HasPrefix(f.Path, "subpacks/") {
			continue
		}
		rest := strings.TrimPrefix(f.Path, "subpacks/")
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			continue // not inside a member dir
		}
		if strings.Contains(rest[slash+1:], "/") {
			continue // deeper than one level
		}
		if f.Path == "subpacks/"+rest[:slash]+"/manifest.json" {
			seen["subpacks/"+rest[:slash]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// squashSubpacks recursively squashes each subpack member of the parent
// pack, in place inside the transform result. Each subpack runs the FULL
// pipeline (load → transform → re-load → prove), and its logs are merged
// into the parent's transform result with the subpack path prefix — so
// the mapping file, the report, and the parent-level validator all see
// one coherent story.
func squashSubpacks(parent *pack.Pack, tres *transform.Result, opts Options) error {
	for _, dir := range subpackDirs(parent) {
		prefix := dir + "/"
		var files []*pack.File
		for _, f := range parent.Files {
			if strings.HasPrefix(f.Path, prefix) {
				rel := strings.TrimPrefix(f.Path, prefix)
				files = append(files, &pack.File{Path: rel, Data: f.Data, Class: pack.Classify(rel)})
			}
		}
		if len(files) == 0 {
			continue
		}
		sub, err := pack.FromFiles(dir, files)
		if err != nil {
			return fmt.Errorf("subpack %s: %w", dir, err)
		}
		subOpts := opts
		subOpts.SquashSubpacks = false // recursion guard; the format has one level
		sres, err := SquashPack(sub, subOpts)
		if err != nil {
			return fmt.Errorf("subpack %s: %w", dir, err)
		}

		// Merge the subpack output back into the parent's file set,
		// replacing the subpack's entries.
		var merged []*pack.File
		for _, f := range tres.Files {
			if !strings.HasPrefix(f.Path, prefix) {
				merged = append(merged, f)
			}
		}
		for _, f := range sres.Out.Files {
			merged = append(merged, &pack.File{
				Path:  prefix + f.Path,
				Data:  f.Data,
				Class: pack.ClassSubpack,
			})
		}
		sort.Slice(merged, func(i, j int) bool { return merged[i].Path < merged[j].Path })
		tres.Files = merged

		// Merge the logs, prefixed. Renames and rewrites use input
		// coordinates, which within the parent are the prefixed paths.
		for _, rn := range sres.Transform.Renames {
			tres.Renames = append(tres.Renames, graph.Rename{
				From: prefix + rn.From, To: prefix + rn.To,
			})
		}
		for _, rw := range sres.Transform.Rewrites {
			tres.Rewrites = append(tres.Rewrites, graph.Rewrite{
				File: prefix + rw.File, Pointer: rw.Pointer, From: rw.From, To: rw.To,
			})
		}
		for k, v := range sres.Transform.SkipReasons {
			tres.SkipReasons[prefix+k] = v
		}
		for k := range sres.Transform.PNGOptimized {
			tres.PNGOptimized[prefix+k] = true
		}
		for k := range sres.Transform.OverridesOptimized {
			tres.OverridesOptimized[prefix+k] = true
		}
		for k := range sres.Transform.AudioTranscoded {
			tres.AudioTranscoded[prefix+k] = true
		}
		tres.Warnings = append(tres.Warnings, sres.Check.Findings...)
	}
	return nil
}
