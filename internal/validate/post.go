package validate

import (
	"path"
	"strings"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/pack"
)

// TransformLog describes what a squash claims to have done. PostTransform
// holds the transform to exactly this account — the validator re-derives
// everything from the packs themselves and refuses to trust the log beyond
// using it as the allowlist.
type TransformLog struct {
	Renames  []graph.Rename
	Rewrites []graph.Rewrite
	// PNGOptimized maps ORIGINAL paths of re-encoded textures (and actually
	// got smaller) to true.
	PNGOptimized map[string]bool
	// OverridesOptimized lists vanilla-path/deny-listed textures that were
	// re-encoded under the explicit --png-optimize-overrides opt-in.
	// Without that flag, such textures must stay byte-identical.
	OverridesOptimized map[string]bool
	// AudioTranscoded lists pack-internal OGG sounds re-encoded under the
	// explicit --audio-ffmpeg opt-in (original paths). Without that flag,
	// all audio must stay byte-identical.
	AudioTranscoded map[string]bool
	// SubpacksSquashed records that the transform ran the recursive
	// --squash-subpacks pass: subpack JSON was minified and subpack files
	// may have been renamed inside their subpack. Without it, every byte
	// under subpacks/ must be identical to the input.
	SubpacksSquashed bool
}

// PostTransform proves the output pack is load-equivalent to the input:
//
//   - the manifest is byte-identical (UUIDs, versions, min_engine_version
//     are sacred — they couple to invisible behavior packs),
//   - the file inventory is exactly input-renamed,
//   - every non-JSON byte survives identily except PNGs the transform
//     logged as re-encoded,
//   - every JSON file is structurally identical to its input except at the
//     pointers the rewrite log names — key order, member counts, strings,
//     everything else must match,
//   - every reference that resolved before still resolves after,
//   - PBR texture sets moved in lockstep,
//   - nothing deny-listed or engine-fixed moved.
//
// Any violation is an error finding; the squash pipeline aborts on those.
func PostTransform(in *pack.Pack, inG *graph.Graph, out *pack.Pack, outG *graph.Graph, log *TransformLog) *Result {
	r := &Result{}
	if log == nil {
		log = &TransformLog{}
	}
	renameMap := map[string]string{}
	for _, rn := range log.Renames {
		if _, dup := renameMap[rn.From]; dup {
			r.add(pack.SeverityError, rn.From, "file renamed twice")
		}
		renameMap[rn.From] = rn.To
	}
	// Rename targets must not collide with paths that survive unrenamed.
	for from, to := range renameMap {
		if _, occupied := in.File(to); occupied {
			if _, alsoMoved := renameMap[to]; !alsoMoved {
				r.add(pack.SeverityError, to, "rename target collides with an existing file (from %s)", from)
			}
		}
	}

	r.checkManifest(in, out)
	r.checkInventory(in, out, renameMap)
	r.checkBytes(in, out, renameMap, log)
	r.checkJSONTrees(inG, outG, renameMap, log)
	r.checkEdges(in, inG, out, outG, renameMap, log)
	r.checkSets(inG, outG, renameMap)
	r.checkProtected(in, log)
	sortFindings(r.Findings)
	return r
}

func (r *Result) checkManifest(in, out *pack.Pack) {
	inM, ok := in.File("manifest.json")
	if !ok {
		r.add(pack.SeverityError, "manifest.json", "input pack has no manifest — nothing to compare")
		return
	}
	outM, ok := out.File("manifest.json")
	if !ok {
		r.add(pack.SeverityError, "manifest.json", "output pack lost its manifest")
		return
	}
	if !bytesEqual(inM.Data, outM.Data) {
		r.add(pack.SeverityError, "manifest.json",
			"manifest.json changed — it must be stored byte-identically (UUIDs/versions are sacred)")
	}
}

func (r *Result) checkInventory(in, out *pack.Pack, renameMap map[string]string) {
	expected := map[string]bool{}
	for _, f := range in.Files {
		to := f.Path
		if m, ok := renameMap[f.Path]; ok {
			to = m
		}
		if expected[to] {
			r.add(pack.SeverityError, to, "two input files map to the same output path")
		}
		expected[to] = true
	}
	for _, f := range out.Files {
		if !expected[f.Path] {
			r.add(pack.SeverityError, f.Path, "output contains a file that no input file maps to")
		}
	}
	for path := range expected {
		if _, ok := out.File(path); !ok {
			r.add(pack.SeverityError, path, "output is missing this file")
		}
	}
}

func (r *Result) checkBytes(in, out *pack.Pack, renameMap map[string]string, log *TransformLog) {
	for _, f := range in.Files {
		to := f.Path
		if m, ok := renameMap[f.Path]; ok {
			to = m
		}
		// JSON gets the structural diff; everything else must be
		// byte-identical unless it is a logged re-encode.
		if pack.Ext(f.Path) == ".json" && f.Class != pack.ClassSubpack {
			continue
		}
		if log.PNGOptimized[f.Path] || log.AudioTranscoded[f.Path] {
			continue
		}
		if f.Class == pack.ClassSubpack && log.SubpacksSquashed {
			// The recursive pass replaced subpack content; each subpack
			// ran its own full pipeline + PostTransform inside the
			// transform. The parent only checks inventory accounting.
			continue
		}
		of, ok := out.File(to)
		if !ok {
			continue // inventory check already reported it
		}
		if !bytesEqual(f.Data, of.Data) {
			r.add(pack.SeverityError, f.Path, "non-JSON file content changed")
		}
	}
}

func (r *Result) checkJSONTrees(inG *graph.Graph, outG *graph.Graph, renameMap map[string]string, log *TransformLog) {
	for path, inTree := range inG.Docs {
		to := path
		if m, ok := renameMap[path]; ok {
			to = m
		}
		outTree, ok := outG.Docs[to]
		if !ok {
			r.add(pack.SeverityError, path, "parsed JSON tree missing from output")
			continue
		}
		// The allowlist is per file, keyed by pointer alone — jsonx.Diff
		// has no notion of files.
		allow := map[string]jsonx.StringChange{}
		for _, rw := range log.Rewrites {
			if rw.File == path {
				allow[rw.Pointer] = jsonx.StringChange{From: rw.From, To: rw.To}
			}
		}
		diffs := jsonx.Diff(inTree, outTree, jsonx.DiffOptions{
			AllowNumberNormalization: true,
			AllowStringChanges:       allow,
		})
		for _, d := range diffs {
			r.add(pack.SeverityError, path+d.Pointer, "%s", d.Reason)
		}
	}
}

func (r *Result) checkEdges(in *pack.Pack, inG *graph.Graph, out *pack.Pack, outG *graph.Graph, renameMap map[string]string, log *TransformLog) {
	// Index output edges by (mapped input file, pointer).
	inverse := map[string]string{} // out path -> in path
	for from, to := range renameMap {
		inverse[to] = from
	}
	outByKey := map[string]*edgeLike{}
	for _, e := range outG.Edges {
		file := e.File
		if in, ok := inverse[file]; ok {
			file = in
		}
		outByKey[file+"\x00"+e.Pointer] = &edgeLike{raw: e.Raw, target: e.Target, kind: e.Kind}
	}
	expectedRaw := map[string]string{}
	for _, rw := range log.Rewrites {
		expectedRaw[rw.File+"\x00"+rw.Pointer] = rw.To
	}
	for _, e := range inG.Edges {
		key := e.File + "\x00" + e.Pointer
		oe, ok := outByKey[key]
		if !ok {
			r.add(pack.SeverityError, e.File+e.Pointer, "reference vanished from output")
			continue
		}
		if want, isRewrite := expectedRaw[key]; isRewrite {
			if oe.raw != want {
				r.add(pack.SeverityError, e.File+e.Pointer,
					"rewritten reference is %q, expected %q", oe.raw, want)
			}
		} else if oe.raw != e.Raw {
			r.add(pack.SeverityError, e.File+e.Pointer,
				"reference changed %q -> %q without a logged rewrite", e.Raw, oe.raw)
		}
		// A reference that resolved inside the pack before must resolve
		// after — this is the "no dangling refs after rename" guarantee.
		// (The reverse — a previously dangling ref now resolving — is fine:
		// rewrites normalize author case-mismatches.)
		if e.Target != "" && oe.target == "" {
			r.add(pack.SeverityError, e.File+e.Pointer,
				"reference %q no longer resolves after transform", oe.raw)
		}
		if e.Kind != oe.kind {
			r.add(pack.SeverityError, e.File+e.Pointer, "edge kind changed")
		}
	}
}

type edgeLike struct {
	raw    string
	target string
	kind   graph.EdgeKind
}

func (r *Result) checkSets(inG, outG *graph.Graph, renameMap map[string]string) {
	for _, set := range inG.Sets() {
		newColor, renamed := renameMap[set.Color]
		if !renamed {
			for _, p := range set.Members() {
				if _, moved := renameMap[p]; moved {
					r.add(pack.SeverityError, p,
						"PBR set member moved without its color map (lockstep violation)")
				}
			}
			continue
		}
		// The whole set must have moved together: every companion must now
		// sit beside the new color map with its suffix preserved.
		dir := path.Dir(newColor)
		stem := strings.TrimSuffix(path.Base(newColor), path.Ext(newColor))
		for suffix, oldCompanion := range set.Companions {
			wantCand := dir + "/" + stem + suffix
			movedTo, moved := renameMap[oldCompanion]
			if !moved {
				r.add(pack.SeverityError, oldCompanion,
					"PBR companion %q was left behind when its color map moved to %q",
					oldCompanion, newColor)
				continue
			}
			if !strings.HasPrefix(movedTo, wantCand) {
				r.add(pack.SeverityError, movedTo,
					"PBR companion moved to %q but suffix convention beside %q requires a %s* file",
					movedTo, newColor, wantCand)
			}
			// The companion must exist in the output set structure.
			outSet := outG.SetOf(movedTo)
			if outSet == nil || outSet.Color != newColor {
				r.add(pack.SeverityError, movedTo,
					"output does not regroup %q into the PBR set of %q", movedTo, newColor)
			}
		}
		// And nothing may still sit at the old paths.
		for _, member := range set.Members() {
			if _, stillThere := outG.Pack.File(member); stillThere {
				r.add(pack.SeverityError, member, "renamed PBR member still present at its old path")
			}
		}
	}
}

func (r *Result) checkProtected(in *pack.Pack, log *TransformLog) {
	for _, rn := range log.Renames {
		if rn.From == "manifest.json" || rn.From == "pack_icon.png" {
			r.add(pack.SeverityError, rn.From, "sacred file was renamed")
		}
		// Subpack-internal renames under the --squash-subpacks opt-in are
		// sanctioned by their own member-level PostTransform (which
		// enforced NeverRename/deny/vanilla rules on the member-relative
		// paths). The parent only verifies they stay inside their subpack.
		if log.SubpacksSquashed && strings.HasPrefix(rn.From, "subpacks/") {
			subpackRoot := subpackRootOf(rn.From)
			if subpackRoot != subpackRootOf(rn.To) {
				r.add(pack.SeverityError, rn.From,
					"subpack rename escaped its subpack (to %s)", rn.To)
			}
			continue
		}
		if graph.NeverRename(rn.From) {
			r.add(pack.SeverityError, rn.From, "engine-fixed or deny-listed path was renamed")
		}
		if graph.IsVanillaPath(rn.From) {
			r.add(pack.SeverityError, rn.From,
				"vanilla path was renamed — overrides must keep their exact paths")
		}
	}
	for orig := range log.PNGOptimized {
		f, ok := in.File(orig)
		if !ok {
			r.add(pack.SeverityError, orig, "PNG optimization logged for a file that does not exist")
			continue
		}
		if f.Class != pack.ClassTexture {
			r.add(pack.SeverityError, orig, "PNG optimization logged for a non-texture file")
		}
		if log.OverridesOptimized[orig] {
			// Opt-in override re-encode (--png-optimize-overrides):
			// allowed for vanilla-path/deny-listed textures, pixel-verified
			// by the transform.
			continue
		}
		if graph.IsDenyListed(orig) || graph.NeverRename(orig) || graph.IsVanillaPath(orig) {
			r.add(pack.SeverityError, orig,
				"vanilla-override texture was re-encoded (must stay byte-identical; use --png-optimize-overrides to opt in)")
		}
	}
}

// subpackRootOf returns the subpacks/<name> root of a path inside a
// subpack, or "" when the path is not in one.
func subpackRootOf(p string) string {
	if !strings.HasPrefix(p, "subpacks/") {
		return ""
	}
	rest := strings.TrimPrefix(p, "subpacks/")
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return "subpacks/" + rest[:i]
	}
	return ""
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
