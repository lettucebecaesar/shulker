package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/pack"
)

// CategoryTotals aggregates one class bucket across a squash.
type CategoryTotals struct {
	Class       pack.Class
	FilesBefore int
	FilesAfter  int
	Before      int64
	After       int64
}

// SizeReport holds before/after size data for the demo table.
type SizeReport struct {
	Categories  []CategoryTotals
	FilesBefore int
	FilesAfter  int
	TotalBefore int64
	TotalAfter  int64
}

// BuildSizeReport computes per-class buckets. Classes are stable across
// squashing (renames preserve directory + extension, minify preserves
// file-ness), so comparing buckets directly is meaningful.
func BuildSizeReport(in, out *pack.Pack) *SizeReport {
	r := &SizeReport{}
	perClass := map[pack.Class]*CategoryTotals{}
	bucket := func(c pack.Class) *CategoryTotals {
		b, ok := perClass[c]
		if !ok {
			b = &CategoryTotals{Class: c}
			perClass[c] = b
		}
		return b
	}
	for _, f := range in.Files {
		b := bucket(f.Class)
		b.FilesBefore++
		b.Before += int64(len(f.Data))
	}
	for _, f := range out.Files {
		b := bucket(f.Class)
		b.FilesAfter++
		b.After += int64(len(f.Data))
	}
	for _, b := range perClass {
		r.Categories = append(r.Categories, *b)
	}
	// Rows: biggest savings first, then by name — the table tells a story.
	sort.Slice(r.Categories, func(i, j int) bool {
		si := r.Categories[i].Before - r.Categories[i].After
		sj := r.Categories[j].Before - r.Categories[j].After
		if si != sj {
			return si > sj
		}
		if r.Categories[i].Before != r.Categories[j].Before {
			return r.Categories[i].Before > r.Categories[j].Before
		}
		return r.Categories[i].Class < r.Categories[j].Class
	})
	r.FilesBefore = len(in.Files)
	r.FilesAfter = len(out.Files)
	r.TotalBefore = in.TotalBytes()
	r.TotalAfter = out.TotalBytes()
	return r
}

// Saved returns total bytes saved (can be negative if the transform lost —
// reported honestly).
func (r *SizeReport) Saved() int64 { return r.TotalBefore - r.TotalAfter }

// Percent returns the savings percentage, 0 when there was nothing to save.
func (r *SizeReport) Percent() float64 {
	if r.TotalBefore == 0 {
		return 0
	}
	return float64(r.Saved()) * 100 / float64(r.TotalBefore)
}

// Render prints the satisfying table: per category, then the total row.
// Write errors are ignored deliberately — this is display output.
func (r *SizeReport) Render(w io.Writer) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
	p("\n")
	p("  %-14s %10s  %10s  %10s  %7s\n", "category", "before", "after", "saved", "saved %")
	p("  %s\n", strings.Repeat("-", 58))
	for _, c := range r.Categories {
		if c.Before == 0 && c.After == 0 {
			continue
		}
		saved := c.Before - c.After
		pct := 0.0
		if c.Before > 0 {
			pct = float64(saved) * 100 / float64(c.Before)
		}
		p("  %-14s %10s  %10s  %10s  %6.1f%%\n",
			c.Class.String(), HumanBytes(c.Before), HumanBytes(c.After),
			HumanBytes(saved), pct)
	}
	p("  %s\n", strings.Repeat("-", 58))
	p("  %-14s %10s  %10s  %10s  %6.1f%%\n",
		"TOTAL", HumanBytes(r.TotalBefore), HumanBytes(r.TotalAfter),
		HumanBytes(r.Saved()), r.Percent())
	p("  %d files in, %d files out\n", r.FilesBefore, r.FilesAfter)
}

// HumanBytes formats byte counts for humans: B, KiB, MiB, GiB.
func HumanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	case n >= 0:
		return fmt.Sprintf("%d B", n)
	default:
		return fmt.Sprintf("-%d B", -n)
	}
}

// Encode renders the machine-readable JSON report (--report): sizes,
// inventory shape, and the squash's own record of what it did. Built via
// jsonx so the file is deterministic.
func (r *SizeReport) Encode(generator string, renames, rewrites int, pngCount int, warnings []string) []byte {
	root := &jsonx.Object{}
	root.Members = append(root.Members,
		jsonx.Member{Key: "generator", Val: &jsonx.String{Str: generator}},
		jsonx.Member{Key: "files_before", Val: num(r.FilesBefore)},
		jsonx.Member{Key: "files_after", Val: num(r.FilesAfter)},
		jsonx.Member{Key: "bytes_before", Val: num(int(r.TotalBefore))},
		jsonx.Member{Key: "bytes_after", Val: num(int(r.TotalAfter))},
		jsonx.Member{Key: "bytes_saved", Val: num(int(r.Saved()))},
		jsonx.Member{Key: "saved_percent", Val: &jsonx.Number{Raw: trimFloat(r.Percent())}},
		jsonx.Member{Key: "renames", Val: num(renames)},
		jsonx.Member{Key: "ref_rewrites", Val: num(rewrites)},
		jsonx.Member{Key: "png_reencoded", Val: num(pngCount)},
	)
	cats := &jsonx.Array{}
	for _, c := range r.Categories {
		o := &jsonx.Object{}
		o.Members = append(o.Members,
			jsonx.Member{Key: "category", Val: &jsonx.String{Str: c.Class.String()}},
			jsonx.Member{Key: "files_before", Val: num(c.FilesBefore)},
			jsonx.Member{Key: "files_after", Val: num(c.FilesAfter)},
			jsonx.Member{Key: "bytes_before", Val: num(int(c.Before))},
			jsonx.Member{Key: "bytes_after", Val: num(int(c.After))},
		)
		cats.Items = append(cats.Items, o)
	}
	root.Members = append(root.Members, jsonx.Member{Key: "categories", Val: cats})
	if len(warnings) > 0 {
		ws := &jsonx.Array{}
		for _, w := range warnings {
			ws.Items = append(ws.Items, &jsonx.String{Str: w})
		}
		root.Members = append(root.Members, jsonx.Member{Key: "warnings", Val: ws})
	}
	return jsonx.Encode(root)
}

func num(n int) *jsonx.Number { return &jsonx.Number{Raw: fmt.Sprintf("%d", n)} }

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	if strings.HasSuffix(s, ".0") {
		return strings.TrimSuffix(s, ".0")
	}
	return s
}
