// Package pipeline is the library heart of shulker: load → parse → graph →
// validate → transform → re-load → re-validate. It is usable without the
// CLI (the Regolith filter mode and tests both drive it directly).
//
// The pipeline never writes an output the post-transform validator rejects —
// a squash either ships a pack proven load-equivalent, or it fails loudly
// with the full list of reasons.
package pipeline

import (
	"fmt"
	"sort"

	"github.com/lettucebecaesar/shulker/internal/cache"
	"github.com/lettucebecaesar/shulker/internal/graph"
	"github.com/lettucebecaesar/shulker/internal/pack"
	"github.com/lettucebecaesar/shulker/internal/transform"
	"github.com/lettucebecaesar/shulker/internal/validate"
)

// Options configures a squash.
type Options struct {
	// Transforms selects the passes (see transform.Options).
	Transforms transform.Options
	// UseCache enables the content-addressed PNG transform cache
	// (~/.cache/shulker, or CacheDir when set).
	UseCache bool
	CacheDir string
	// SquashSubpacks recursively squashes each subpacks/<dir> member with
	// the same rigor as the parent (its own full pipeline + validation).
	SquashSubpacks bool
	// Generator labels mapping/report outputs.
	Generator string
}

// Result is a completed squash: the packs, the transform log, validation
// findings, and report data.
type Result struct {
	In  *pack.Pack
	Out *pack.Pack

	Transform *transform.Result
	Check     *validate.Result
	Post      *validate.Result
}

// Squash loads `in` (a .mcpack/.zip file or a directory) and runs the full
// pipeline in memory. Writing the outputs (pack bytes, mapping, report) is
// the caller's job, so the CLI, the Regolith mode, and tests share exactly
// one code path.
func Squash(in string, opts Options) (*Result, error) {
	p, err := pack.Load(in)
	if err != nil {
		return nil, fmt.Errorf("loading pack: %w", err)
	}
	return SquashPack(p, opts)
}

// SquashPack runs the pipeline over an already-loaded pack.
func SquashPack(p *pack.Pack, opts Options) (*Result, error) {
	res := &Result{In: p}

	// 1. Parse + graph + pre-flight validation. Parse failures and manifest
	// problems are hard errors: never transform a pack we cannot fully
	// read.
	docs, parseErrs := graph.ParseAll(p)
	gIn, err := graph.Build(p, docs, parseErrs)
	if err != nil {
		return nil, err
	}
	res.Check = validate.Check(p, gIn)
	if res.Check.HasErrors() {
		return nil, fmt.Errorf("pack has problems shulker refuses to work around:\n%s",
			formatFindings(res.Check.Errors()))
	}

	// 2. Transform (with optional PNG cache). Cache profiles pin the exact
	// transform definition per pass so a cached result can never describe
	// different behavior than the code would run.
	topts := opts.Transforms
	if topts.PNGOptimize && opts.UseCache {
		pngFn := topts.PNG
		if pngFn == nil {
			qopts := transform.PNGOptions{Quantize: topts.PNGQuantize, QuantizeColors: topts.QuantizeColors}
			pngFn = func(data []byte) ([]byte, bool, error) { return transform.OptimizePNGWith(data, qopts) }
		}
		profile := fmt.Sprintf("v2:q=%t:c=%d", topts.PNGQuantize, topts.QuantizeColors)
		topts.PNG = makeCachedPNG(profile, pngFn, opts.CacheDir)
	}
	if topts.PNGOptimizeOverrides && opts.UseCache {
		verifyFn := topts.PNGVerify
		if verifyFn == nil {
			verifyFn = transform.OptimizePNGLosslessVerified
		}
		topts.PNGVerify = makeCachedPNG("v2:lossless-verified", verifyFn, opts.CacheDir)
	}
	if topts.AudioFFmpeg && opts.UseCache {
		audioFn := topts.AudioFn
		if audioFn == nil {
			audioFn = transform.DefaultAudioFn
		}
		// The profile embeds the reported ffmpeg version: different builds
		// can produce different bytes, so they must never share entries.
		profile := fmt.Sprintf("v1:ffmpeg:%s:q=%d", transform.FFmpegVersion(), topts.AudioQuality)
		quality := topts.AudioQuality
		c, cerr := cache.New(opts.CacheDir, true)
		if cerr == nil && c != nil {
			topts.AudioFn = func(data []byte, _ int) ([]byte, bool, error) {
				key := cache.Key("audio-transcode", profile, cache.HashBytes(data))
				if out, smaller, ok := c.Get(key); ok {
					return out, smaller, nil
				}
				out, smaller, err := audioFn(data, quality)
				if err == nil {
					c.Put(key, smaller, out)
				}
				return out, smaller, err
			}
		}
	}
	tres, err := transform.Run(p, docs, gIn, topts)
	if err != nil {
		return nil, fmt.Errorf("transform failed: %w", err)
	}
	res.Transform = tres

	// 2b. Recursive subpack squash: each subpacks/<dir> containing a
	// manifest is a full nested pack. It runs its OWN complete pipeline —
	// including its own post-transform validation — and its logs are
	// merged into the parent's with the subpack prefix, so the mapping,
	// report, and parent-level accounting stay one story.
	if opts.SquashSubpacks {
		if err := squashSubpacks(p, tres, opts); err != nil {
			return nil, err
		}
	}

	// 3. Re-load the result as a real pack — classification, ordering, and
	// manifest validation run again on exactly what will ship.
	out, err := pack.FromFiles("squashed", tres.Files)
	if err != nil {
		return nil, fmt.Errorf("transformed pack is not loadable: %w", err)
	}
	res.Out = out

	// 4. Post-transform validation: the squash is only real if the
	// validator proves it. Failures here are shulker bugs, not pack
	// problems — and they abort the build.
	outDocs, outErrs := graph.ParseAll(out)
	outG, err := graph.Build(out, outDocs, outErrs)
	if err != nil {
		return nil, err
	}
	res.Post = validate.PostTransform(p, gIn, out, outG, &validate.TransformLog{
		Renames:            tres.Renames,
		Rewrites:           tres.Rewrites,
		PNGOptimized:       tres.PNGOptimized,
		AudioTranscoded:    tres.AudioTranscoded,
		OverridesOptimized: tres.OverridesOptimized,
		SubpacksSquashed:   opts.SquashSubpacks,
	})
	if res.Post.HasErrors() {
		return nil, fmt.Errorf("post-transform validation FAILED — this is a shulker bug, please report it:\n%s",
			formatFindings(res.Post.Errors()))
	}
	return res, nil
}

// Warnings returns the aggregated non-blocking findings, sorted. Check
// already folds the loader's warnings in, so they are not merged twice.
func (r *Result) Warnings() []pack.Finding {
	var out []pack.Finding
	out = append(out, r.Check.Findings...)
	out = append(out, r.Transform.Warnings...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func formatFindings(fs []pack.Finding) string {
	var lines []string
	for _, f := range fs {
		lines = append(lines, "  - "+f.String())
	}
	return joinLines(lines)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

// makeCachedPNG wraps a PNG transform with the content-addressed cache.
// profile pins the transform definition (see cache.PNGKey).
func makeCachedPNG(profile string, pngFn func(data []byte) ([]byte, bool, error), dir string) func([]byte) ([]byte, bool, error) {
	if pngFn == nil {
		pngFn = transform.OptimizePNG
	}
	c, err := cache.New(dir, true)
	if err != nil || c == nil {
		return pngFn
	}
	return func(data []byte) ([]byte, bool, error) {
		key := cache.PNGKey(profile, data)
		if out, changed, ok := c.Get(key); ok {
			return out, changed, nil
		}
		out, changed, err := pngFn(data)
		if err != nil {
			return out, changed, err
		}
		c.Put(key, changed, out)
		return out, changed, nil
	}
}
