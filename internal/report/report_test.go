package report

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/pack"
	"github.com/shulker-tools/shulker/internal/pipeline"
	"github.com/shulker-tools/shulker/internal/transform"
)

func loadFixture(t *testing.T, name string) (*pack.Pack, *graph.Graph) {
	t.Helper()
	p, err := pack.LoadDir(filepath.Join("..", "..", "testdata", "packs", name))
	if err != nil {
		t.Fatal(err)
	}
	docs, parseErrs := graph.ParseAll(p)
	g, err := graph.Build(p, docs, parseErrs)
	if err != nil {
		t.Fatal(err)
	}
	return p, g
}

func TestMappingRoundTripThroughFile(t *testing.T) {
	m := NewMapping("shulker/test",
		[]graph.Rename{{From: "a.png", To: "b.png"}},
		[]graph.Rewrite{{File: "x.json", Pointer: "/a", From: "a.png", To: "b.png"}},
		map[string]bool{"a.png": true},
		map[string]string{"c.png": "deny-listed"},
	)
	data := m.Encode()
	parsed, err := ParseMapping(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Renames) != 1 || parsed.Renames[0].From != "a.png" || parsed.Renames[0].To != "b.png" {
		t.Errorf("renames round trip failed: %+v", parsed.Renames)
	}
	if len(parsed.Rewrites) != 1 || parsed.Rewrites[0].Pointer != "/a" {
		t.Errorf("rewrites round trip failed: %+v", parsed.Rewrites)
	}
	if len(parsed.PNGOptimized) != 1 || parsed.PNGOptimized[0] != "a.png" {
		t.Errorf("png list round trip failed: %+v", parsed.PNGOptimized)
	}
	if len(parsed.Skips) != 1 || parsed.Skips[0].Path != "c.png" {
		t.Errorf("skips round trip failed: %+v", parsed.Skips)
	}
	// Deterministic: re-encode is byte-stable.
	m2 := NewMapping("shulker/test",
		[]graph.Rename{{From: "a.png", To: "b.png"}},
		[]graph.Rewrite{{File: "x.json", Pointer: "/a", From: "a.png", To: "b.png"}},
		map[string]bool{"a.png": true},
		map[string]string{"c.png": "deny-listed"},
	)
	if !bytes.Equal(m.Encode(), m2.Encode()) {
		t.Error("mapping encoding is not deterministic")
	}
}

func TestParseMappingRejectsWrongVersion(t *testing.T) {
	_, err := ParseMapping([]byte(`{"version": 99}`))
	if err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Errorf("expected version rejection, got %v", err)
	}
}

// The acceptance round-trip: squash the golden fixture, write the mapping,
// then reverse-apply the mapping to the squashed pack. The restored pack
// must be byte-identical to a squash with renaming disabled — same minified
// trees, same optimized PNGs, original paths everywhere, original reference
// strings everywhere.
func TestMappingReverseRoundTrip(t *testing.T) {
	rich := filepath.Join("..", "..", "testdata", "packs", "rich")

	squashed, err := pipeline.Squash(rich, pipeline.Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	mapping := NewMapping("shulker/test", squashed.Transform.Renames,
		squashed.Transform.Rewrites, squashed.Transform.PNGOptimized,
		squashed.Transform.SkipReasons)

	restored, err := mapping.ReverseApply(squashed.Out)
	if err != nil {
		t.Fatal(err)
	}

	// Baseline: same transforms, but renaming disabled (paths + refs stay
	// original). PNG optimization eligibility is unchanged.
	noRename := transform.DefaultOptions().WithRenameDisabled()
	baseline, err := pipeline.Squash(rich, pipeline.Options{
		Transforms: noRename,
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(restored.Files) != len(baseline.Out.Files) {
		t.Fatalf("restored %d files, baseline has %d", len(restored.Files), len(baseline.Out.Files))
	}
	for i := range baseline.Out.Files {
		want := baseline.Out.Files[i]
		got := restored.Files[i]
		if want.Path != got.Path {
			t.Errorf("path mismatch: %q vs %q", got.Path, want.Path)
			continue
		}
		if !bytes.Equal(want.Data, got.Data) {
			t.Errorf("%s: restored bytes differ from no-rename baseline", want.Path)
		}
	}
}

func TestSizeReportRender(t *testing.T) {
	in, _ := loadFixture(t, "rich")
	res, err := pipeline.Squash(filepath.Join("..", "..", "testdata", "packs", "rich"), pipeline.Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sizes := BuildSizeReport(in, res.Out)
	if sizes.TotalAfter > sizes.TotalBefore {
		t.Error("fixture squash should shrink")
	}
	var out bytes.Buffer
	sizes.Render(&out)
	rendered := out.String()
	for _, want := range []string{"category", "TOTAL", "textures", "json"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("table missing %q:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "files in") {
		t.Errorf("table missing file count:\n%s", rendered)
	}
	// Machine report is deterministic.
	a := sizes.Encode("shulker/test", 1, 2, 3, nil)
	b := sizes.Encode("shulker/test", 1, 2, 3, nil)
	if !bytes.Equal(a, b) {
		t.Error("JSON report is not deterministic")
	}
}
