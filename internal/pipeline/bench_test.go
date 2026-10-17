package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/shulker-tools/shulker/internal/report"
	"github.com/shulker-tools/shulker/internal/transform"
)

// TestBenchmarkFixtureReductions is the honesty enforcement for the README's
// headline numbers. The fixture packs under testdata/benchmarks are
// deterministic, so these thresholds are assertable facts, not vibes:
//
//   - json_heavy models an author-formatted pack (comments, 4-space
//     indentation, trailing commas, BOMs, float noise) and must shrink by
//     at least 25%.
//   - png_heavy models community-tool-saved PNGs (BestSpeed sprites) and
//     must shrink by at least 10% — without quantization, which v0.1
//     deliberately does not do.
func TestBenchmarkFixtureReductions(t *testing.T) {
	cases := []struct {
		name       string
		minPercent float64
	}{
		{"json_heavy", 25.0},
		{"png_heavy", 10.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := filepath.Join("..", "..", "testdata", "benchmarks", tc.name)
			res, err := Squash(in, Options{
				Transforms: transform.DefaultOptions(),
				Generator:  "shulker/bench",
			})
			if err != nil {
				t.Fatal(err)
			}
			sizes := report.BuildSizeReport(res.In, res.Out)
			sizes.Render(&testWriter{t})
			if sizes.Percent() < tc.minPercent {
				t.Errorf("%s shrank %.1f%%, need ≥ %.1f%%",
					tc.name, sizes.Percent(), tc.minPercent)
			}
			t.Logf("%s: %s -> %s (%.1f%% saved)",
				tc.name, report.HumanBytes(sizes.TotalBefore),
				report.HumanBytes(sizes.TotalAfter), sizes.Percent())
		})
	}
}

// testWriter routes the size table into t.Logf so `go test -v` shows the
// same table the CLI prints.
type testWriter struct{ t *testing.T }

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", p)
	return len(p), nil
}
