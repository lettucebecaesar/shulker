// Package transform implements shulker's three squash passes: JSON
// minification, conservative path renaming with full reference rewriting,
// and PNG re-encoding. Every pass is deterministic and every pass is gated
// behind a config flag.
//
// The contract with the validator: a transform returns exactly what it did
// (renames, rewrites, re-encodes) and the pipeline refuses to ship output
// the post-transform validator does not accept.
package transform

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/pack"
)

// Plan is the rename plan: which files move where, and (for reporting) why
// every other candidate stayed. Planning is pure — nothing is mutated — so
// plans can be inspected, logged, and diffed before application.
type Plan struct {
	Renames []graph.Rename

	// SkipReasons maps a candidate path to why it was not renamed. The
	// report prints these; the brand is honesty about what was left alone.
	SkipReasons map[string]string

	// PNGEligible lists texture files (original paths) whose references are
	// provably pack-internal: these are the only images v0.1 re-encodes.
	// Vanilla-override and unreferenced textures keep their bytes.
	PNGEligible map[string]bool

	// OverridePNGEligible lists textures the default PNG pass skips
	// (vanilla overrides, deny-listed paths, blocker-locked or
	// unreferenced assets). They are re-encodable only under the explicit
	// --png-optimize-overrides opt-in, which is pixel-verified and
	// lossless-only.
	OverridePNGEligible map[string]bool
}

// Planner runs the conservative v0.1 rename policy.
//
// A file may be renamed only if 100% of its inbound references live inside
// this pack AND every one of them gets rewritten (invariant 3), which in
// code means:
//
//   - path-referenced assets (textures, sounds) need at least one
//     rewriteable edge, zero blocker edges, no deny-listed member, and
//     PBR-set lockstep;
//   - identifier-scanned files (entity/, models/, animations/, …) are
//     looked up by identifier, never by path, so they rename within their
//     directory with their conventional suffix preserved.
type Planner struct {
	opts Options
}

// PlanRenames computes the rename plan over a pack + graph.
func PlanRenames(p *pack.Pack, g *graph.Graph, opts Options) *Plan {
	pl := &Planner{opts: opts}
	plan := &Plan{
		SkipReasons:         map[string]string{},
		PNGEligible:         map[string]bool{},
		OverridePNGEligible: map[string]bool{},
	}

	taken := map[string]bool{} // target paths already assigned
	for _, f := range p.Files {
		if _, ok := taken[f.Path]; ok {
			continue
		}
		taken[f.Path] = true // every existing path is unavailable as a target
	}

	textureN, soundN := 0, 0
	dirN := map[string]int{}

	// Texture candidates, including whole PBR sets, in sorted pack order.
	for _, f := range p.Files {
		if f.Class != pack.ClassTexture {
			continue
		}
		set := g.SetOf(f.Path)
		if set != nil && set.Color != f.Path {
			continue // companions are planned with their color map
		}
		members := []string{f.Path}
		if set != nil {
			members = set.Members()
		}
		pl.planTextureGroup(g, members, f.Path, set, plan, taken, &textureN)
	}

	// Sound candidates.
	if opts.RenameSounds {
		for _, f := range p.Files {
			if f.Class != pack.ClassSound {
				continue
			}
			pl.planSoundFile(g, f.Path, plan, taken, &soundN)
		}
	} else {
		for _, f := range p.Files {
			if f.Class == pack.ClassSound {
				plan.SkipReasons[f.Path] = "sound renaming disabled (--no-rename-sounds)"
			}
		}
	}

	// Identifier-scanned JSON files.
	if opts.RenameIdentDirs {
		for _, f := range p.Files {
			if f.Class != pack.ClassJSON {
				continue
			}
			if !graph.IsIdentifierScanned(f.Path) {
				continue
			}
			pl.planScannedFile(f.Path, plan, taken, dirN)
		}
	} else {
		for _, f := range p.Files {
			if f.Class == pack.ClassJSON && graph.IsIdentifierScanned(f.Path) {
				plan.SkipReasons[f.Path] = "identifier-directory renaming disabled"
			}
		}
	}

	sort.Slice(plan.Renames, func(i, j int) bool { return plan.Renames[i].From < plan.Renames[j].From })
	return plan
}

// renameBlocked returns the human-facing reason a file must keep its path,
// or "" when it may rename. The checks, in order:
//
//  1. engine-fixed files and subtree conventions (registries, ui/, font/…),
//  2. vanilla path matches — exact or extension-insensitive against the
//     frozen vanilla list. Invisible vanilla-side references are exactly
//     the thing invariant 3 forbids us from gambling on.
func renameBlocked(p string) string {
	switch {
	case graph.NeverRename(p):
		return "engine-fixed path: left in place"
	case graph.IsDenyListed(p):
		return "engine-hardcoded subtree (deny-listed): left untouched"
	case graph.IsVanillaPath(p):
		return "matches a vanilla resource pack path (frozen vanilla list): left untouched"
	}
	return ""
}

func (pl *Planner) planTextureGroup(g *graph.Graph, members []string, color string, set *graph.TextureSet, plan *Plan, taken map[string]bool, counter *int) {
	// Eligibility first — it is a property of the input pack and is shared
	// by the rename AND png-optimize passes.
	eligibleForOverrideOptIn := func() {
		for _, m := range members {
			plan.OverridePNGEligible[m] = true
		}
	}
	if reason := renameBlocked(color); reason != "" {
		eligibleForOverrideOptIn()
		for _, m := range members {
			plan.SkipReasons[m] = reason
		}
		return
	}
	colorEdges := 0
	blockers := map[string]string{}
	for _, m := range members {
		if reason := renameBlocked(m); reason != "" {
			eligibleForOverrideOptIn()
			for _, mm := range members {
				plan.SkipReasons[mm] = "PBR set member locked: " + reason
			}
			return
		}
		for _, e := range g.Inbound(m) {
			if !e.Rewriteable {
				blockers[m] = fmt.Sprintf("referenced from unknown schema position in %s", e.File)
			}
		}
	}
	for _, e := range g.Inbound(color) {
		if e.Rewriteable {
			colorEdges++
		}
	}
	if colorEdges == 0 {
		eligibleForOverrideOptIn()
		for _, m := range members {
			plan.SkipReasons[m] = "no pack-internal reference found (may be a vanilla override): left untouched"
		}
		return
	}
	if len(blockers) > 0 {
		eligibleForOverrideOptIn()
		for _, m := range members {
			if why, ok := blockers[m]; ok {
				plan.SkipReasons[m] = why
			} else {
				plan.SkipReasons[m] = "PBR set is blocked by an unrewriteable reference on a set member"
			}
		}
		return
	}

	// The set's references are provably pack-internal: these are the only
	// textures the PNG pass may re-encode (vanilla-override and orphan
	// textures keep their exact bytes).
	for _, m := range members {
		plan.PNGEligible[m] = true
	}

	if !pl.opts.RenameTextures {
		for _, m := range members {
			plan.SkipReasons[m] = "texture renaming disabled (--no-rename-textures)"
		}
		return
	}

	// Assign: color map gets a fresh short stem; companions follow with
	// their suffixes (and keep their own extensions, mirroring vanilla's
	// .png color + .tga companion mixes).
	ext := path.Ext(color)
	stem := pl.freshName("textures", ext, taken, counter)
	plan.Renames = append(plan.Renames, graph.Rename{From: color, To: stem + ext})
	if set != nil {
		for _, suffix := range graph.PBRSuffixes {
			old, ok := set.Companions[suffix]
			if !ok {
				continue
			}
			cext := path.Ext(old)
			plan.Renames = append(plan.Renames, graph.Rename{From: old, To: stem + suffix + cext})
		}
	}
}

func (pl *Planner) planSoundFile(g *graph.Graph, file string, plan *Plan, taken map[string]bool, counter *int) {
	if reason := renameBlocked(file); reason != "" {
		plan.SkipReasons[file] = reason
		return
	}
	rewriteable := 0
	for _, e := range g.Inbound(file) {
		if !e.Rewriteable {
			plan.SkipReasons[file] = fmt.Sprintf("referenced from unknown schema position in %s", e.File)
			return
		}
		if e.Kind == graph.KindSoundPath {
			rewriteable++
		}
	}
	if rewriteable == 0 {
		plan.SkipReasons[file] = "no pack-internal reference found (may be a vanilla override): left untouched"
		return
	}
	ext := path.Ext(file)
	to := pl.freshName("sounds", ext, taken, counter) + ext
	plan.Renames = append(plan.Renames, graph.Rename{From: file, To: to})
}

func (pl *Planner) planScannedFile(file string, plan *Plan, taken map[string]bool, dirN map[string]int) {
	if graph.NeverRename(file) || graph.IsDenyListed(file) {
		plan.SkipReasons[file] = "engine-fixed path: left in place"
		return
	}
	dir := path.Dir(file)
	suffix := graph.PreservedSuffix(path.Base(file))
	n := dirN[dir]
	dirN[dir] = n + 1
	to := fmt.Sprintf("%s/_%d%s", dir, n, suffix)
	for taken[to] {
		n++
		dirN[dir] = n + 1
		to = fmt.Sprintf("%s/_%d%s", dir, n, suffix)
	}
	taken[to] = true
	plan.Renames = append(plan.Renames, graph.Rename{From: file, To: to})
}

// freshName mints the next short asset name in a root dir: textures/_0.png,
// textures/_1.png, … Skipping anything that already exists in the pack
// (belt and braces: the _ prefix avoids vanilla collisions by convention —
// vanilla files are human-named).
func (pl *Planner) freshName(root, ext string, taken map[string]bool, counter *int) string {
	for {
		candidate := fmt.Sprintf("%s/_%d%s", root, *counter, ext)
		*counter++
		if !taken[candidate] {
			taken[candidate] = true
			return strings.TrimSuffix(candidate, ext)
		}
	}
}

// ApplyRenames executes a plan against a graph whose document trees are the
// working copies: every rewriteable inbound edge of every renamed file is
// rewritten, and every rewrite is logged. Edges the graph could not promise
// to rewrite made planning skip the file already, so by the time we are
// here the move is safe by construction.
func ApplyRenames(g *graph.Graph, renames []graph.Rename) []graph.Rewrite {
	var rewrites []graph.Rewrite
	seen := map[string]bool{}
	for _, rn := range renames {
		for _, e := range g.Inbound(rn.From) {
			if !e.Rewriteable {
				continue
			}
			key := e.File + "\x00" + e.Pointer
			if seen[key] {
				continue
			}
			seen[key] = true
			root := "textures"
			if e.Kind == graph.KindSoundPath {
				root = "sounds"
			}
			newRaw := graph.RenderPathForm(e.Form, root, rn.To)
			if newRaw == e.Raw {
				continue
			}
			e.Node().Set(newRaw)
			rewrites = append(rewrites, graph.Rewrite{
				File: e.File, Pointer: e.Pointer, From: e.Raw, To: newRaw,
			})
		}
	}
	sort.Slice(rewrites, func(i, j int) bool {
		a, b := rewrites[i], rewrites[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Pointer < b.Pointer
	})
	return rewrites
}
