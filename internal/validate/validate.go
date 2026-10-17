// Package validate is shulker's safety net. It runs in two modes:
//
//   - Check: lint a pack as loaded (used by `shulker check` and as the
//     pre-flight gate of every squash).
//   - PostTransform: prove that a squashed pack still loads the same. Every
//     JSON file is structurally diffed against its input with an allowlist
//     built from the transform's own rewrite log — a squash may only change
//     what it said it changed, anywhere else is a defect.
//
// Severity policy: errors abort squashing and fail `shulker check`;
// warnings (dangling refs that may resolve against vanilla, subpack notes,
// …) never block anything.
package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lettucebecaesar/shulker/internal/graph"
	"github.com/lettucebecaesar/shulker/internal/jsonx"
	"github.com/lettucebecaesar/shulker/internal/pack"
)

// Result is a validation outcome.
type Result struct {
	Findings []pack.Finding
}

// HasErrors reports whether any finding is error-severity.
func (r *Result) HasErrors() bool { return pack.HasErrors(r.Findings) }

func (r *Result) add(sev pack.Severity, path, format string, args ...any) {
	r.Findings = append(r.Findings, pack.Finding{
		Severity: sev,
		Path:     path,
		Msg:      fmt.Sprintf(format, args...),
	})
}

// Errors returns only the error-severity findings.
func (r *Result) Errors() []pack.Finding {
	var out []pack.Finding
	for _, f := range r.Findings {
		if f.Severity == pack.SeverityError {
			out = append(out, f)
		}
	}
	return out
}

// Check validates a loaded, untransformed pack: parse health, reference
// resolution, and pack-level sanity. Graph and loader warnings are merged
// and sorted deterministically.
func Check(p *pack.Pack, g *graph.Graph) *Result {
	r := &Result{}
	seen := map[string]bool{}
	key := func(f pack.Finding) string {
		return strings.ToLower(f.Severity.String()) + "|" + f.Path + "|" + f.Msg
	}
	push := func(fs []pack.Finding) {
		for _, f := range fs {
			k := key(f)
			if seen[k] {
				continue
			}
			seen[k] = true
			r.Findings = append(r.Findings, f)
		}
	}
	push(p.Warnings)
	if g != nil {
		push(g.Warnings)
	}
	r.checkLanguages(p)
	r.checkAtlasShortNameCollisions(g)
	sortFindings(r.Findings)
	return r
}

// checkLanguages warns when texts/languages.json lists a language with no
// corresponding .lang file. The engine silently falls back; we surface it
// because it is almost always a typo.
func (r *Result) checkLanguages(p *pack.Pack) {
	if _, ok := p.File("texts/languages.json"); !ok {
		return
	}
	g, err := parseJSONFile(p, "texts/languages.json")
	if err != nil {
		return // parse problems are reported by the graph pass
	}
	arr, ok := jsonx.AsArray(g)
	if !ok {
		r.add(pack.SeverityWarning, "texts/languages.json", "should be an array of language codes")
		return
	}
	for i, item := range arr.Items {
		code, isStr := jsonx.AsString(item)
		if !isStr {
			r.add(pack.SeverityWarning, "texts/languages.json", "/%d: language code must be a string", i)
			continue
		}
		if _, ok := p.File("texts/" + code + ".lang"); !ok {
			r.add(pack.SeverityWarning, "texts/languages.json",
				"/%d: language %q has no texts/%s.lang file", i, code, code)
		}
	}
}

// checkAtlasShortNameCollisions flags two atlas registries defining the same
// short name with different textures — the winner depends on load order,
// which is exactly the kind of ambiguity shulker must surface rather than
// silently arbitrate.
func (r *Result) checkAtlasShortNameCollisions(g *graph.Graph) {
	if g == nil {
		return
	}
	for name, files := range g.ShortNameIndex() {
		if len(files) <= 1 {
			continue
		}
		r.add(pack.SeverityWarning, files[0],
			"atlas short name %q defined in %d files (%s) — load order decides the winner",
			name, len(files), strings.Join(files, ", "))
	}
}

func parseJSONFile(p *pack.Pack, path string) (jsonx.Value, error) {
	f, ok := p.File(path)
	if !ok {
		return nil, fmt.Errorf("missing")
	}
	return jsonx.Parse(f.Data)
}

func sortFindings(fs []pack.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Path != fs[j].Path {
			return fs[i].Path < fs[j].Path
		}
		return fs[i].Msg < fs[j].Msg
	})
}
