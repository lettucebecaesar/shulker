package transform

import (
	"sort"
	"sync"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/pack"
)

// Options selects which passes run. Every pass is independently flag-gated.
type Options struct {
	// Minify re-encodes JSON compactly (comments, trailing commas, BOMs and
	// whitespace gone; key order preserved; float noise normalized).
	Minify bool
	// Rename moves eligible files to short generated paths and rewrites
	// every pack-internal reference.
	Rename bool
	// PNGOptimize re-encodes pack-internal textures at best compression,
	// keep-if-smaller.
	PNGOptimize bool

	// Granular rename toggles. Defaults in DefaultOptions.
	RenameTextures  bool
	RenameSounds    bool
	RenameIdentDirs bool

	// Workers bounds concurrent PNG re-encoding (CPU-bound).
	Workers int

	// PNG is the re-encode function; overridable for caching and tests.
	// nil means OptimizePNG.
	PNG func(data []byte) (out []byte, reencoded bool, err error)
}

// DefaultOptions is the shulker squash default: everything on, conservatism
// always.
func DefaultOptions() Options {
	return Options{
		Minify:          true,
		Rename:          true,
		PNGOptimize:     true,
		RenameTextures:  true,
		RenameSounds:    true,
		RenameIdentDirs: true,
		Workers:         0, // 0 = runtime.NumCPU
	}
}

// WithRenameDisabled clears every rename toggle at once (--no-rename).
func (o Options) WithRenameDisabled() Options {
	o.Rename = false
	o.RenameTextures = false
	o.RenameSounds = false
	o.RenameIdentDirs = false
	return o
}

// Result is everything a squash did — the transform log the validator and
// the mapping file are built from.
type Result struct {
	Files []*pack.File // transformed, sorted, reclassified

	Renames  []graph.Rename
	Rewrites []graph.Rewrite

	// PNGOptimized maps ORIGINAL paths that were re-encoded (and actually
	// got smaller) to true. PNGPassthrough lists undecodable PNGs that
	// were left byte-identical.
	PNGOptimized   map[string]bool
	PNGPassthrough []string

	// SkipReasons explains every rename candidate that stayed put.
	SkipReasons map[string]string

	// Warnings collects everything the transform wants to say out loud.
	Warnings []pack.Finding
}

// Run applies the selected transforms.
//
// inDocs/inG are the input's parsed documents and graph; they are treated
// as read-only evidence for the validator. The transform works on a deep
// clone of the documents with its own graph over the clones, so rewriting
// reference strings cannot corrupt the diff baseline.
func Run(p *pack.Pack, inDocs map[string]jsonx.Value, inG *graph.Graph, opts Options) (*Result, error) {
	res := &Result{
		PNGOptimized: map[string]bool{},
		SkipReasons:  map[string]string{},
	}

	// 1. Plan renames against the pristine input graph.
	plan := PlanRenames(p, inG, opts)
	if opts.Rename {
		res.Renames = plan.Renames
	}
	for path, reason := range plan.SkipReasons {
		res.SkipReasons[path] = reason
	}

	// 2. Clone documents + build the working graph over the clones.
	workDocs := jsonx.CloneAll(inDocs)
	workG, err := graph.Build(p, workDocs, nil)
	if err != nil {
		return nil, err
	}

	// 3. Apply renames: mutate the cloned trees, log every rewrite.
	if opts.Rename {
		res.Rewrites = ApplyRenames(workG, plan.Renames)
	}

	// 4. PNG optimization (bounded fan-out; results keyed by path so the
	// final assembly is deterministic).
	pngFn := opts.PNG
	if pngFn == nil {
		pngFn = OptimizePNG
	}
	pngOut := map[string][]byte{}
	pngReencoded := map[string]bool{}
	if opts.PNGOptimize && len(plan.PNGEligible) > 0 {
		var paths []string
		for path := range plan.PNGEligible {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		workers := opts.Workers
		if workers <= 0 {
			workers = 8
		}
		sem := make(chan struct{}, workers)
		var mu sync.Mutex
		var wg sync.WaitGroup
		var undecodable []string
		for _, path := range paths {
			f, ok := p.File(path)
			if !ok || pack.Ext(path) != ".png" {
				continue
			}
			wg.Add(1)
			go func(path string, data []byte) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				out, changed, err := pngFn(data)
				if err != nil {
					mu.Lock()
					undecodable = append(undecodable, path)
					mu.Unlock()
					return
				}
				if changed {
					mu.Lock()
					pngOut[path] = out
					pngReencoded[path] = true
					mu.Unlock()
				}
			}(path, f.Data)
		}
		wg.Wait()
		sort.Strings(undecodable)
		res.PNGPassthrough = undecodable
		for _, path := range undecodable {
			res.Warnings = append(res.Warnings, pack.Finding{
				Severity: pack.SeverityWarning,
				Path:     path,
				Msg:      "PNG could not be decoded; passed through unchanged",
			})
		}
	}

	// 5. Materialize the output file set. Input paths map through the
	// rename plan; JSON bytes come from the (rewritten, minified) working
	// trees; PNG bytes from the optimization results; everything else is
	// verbatim — the manifest, subpacks, lang, and all binary payloads
	// pass through byte-identically.
	//
	// A JSON file is re-encoded from the working tree when minify is on OR
	// when the rename pass rewrote strings inside it — shipping a rename
	// without its rewrites would corrupt the pack, so files touched by the
	// rewriter are always re-encoded (compact), even with --no-minify.
	rewritesByFile := map[string]int{}
	for _, rw := range res.Rewrites {
		rewritesByFile[rw.File]++
	}
	renameTo := map[string]string{}
	for _, rn := range res.Renames {
		renameTo[rn.From] = rn.To
	}
	for _, f := range p.Files {
		out := &pack.File{Data: f.Data}
		out.Path = f.Path
		if to, ok := renameTo[f.Path]; ok {
			out.Path = to
		}
		if tree, parsed := workDocs[f.Path]; parsed && (opts.Minify || rewritesByFile[f.Path] > 0) {
			if opts.Minify {
				// Normalize float noise (0.300000011920929 -> 0.3) —
				// provably value-identical at float32, which is what the
				// engine reads.
				normalizeNumbers(tree)
			}
			out.Data = jsonx.Encode(tree)
		} else if data, ok := pngOut[f.Path]; ok {
			out.Data = data
		}
		out.Class = pack.Classify(out.Path)
		res.Files = append(res.Files, out)
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })

	for path := range pngReencoded {
		res.PNGOptimized[path] = true
	}
	return res, nil
}

// normalizeNumbers rewrites every number literal in place through the
// provably-safe float32 shortener.
func normalizeNumbers(v jsonx.Value) {
	jsonx.Walk(v, func(_ string, n jsonx.Value) error {
		if num, ok := n.(*jsonx.Number); ok {
			num.Raw = jsonx.NormalizeNumber(num.Raw)
		}
		return nil
	})
}
