package transform

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lettucebecaesar/shulker/internal/graph"
	"github.com/lettucebecaesar/shulker/internal/jsonx"
	"github.com/lettucebecaesar/shulker/internal/pack"
	"github.com/lettucebecaesar/shulker/internal/validate"
)

func fixture(t *testing.T, name string) (*pack.Pack, map[string]jsonx.Value, *graph.Graph) {
	t.Helper()
	p, err := pack.LoadDir(filepath.Join("..", "..", "testdata", "packs", name))
	if err != nil {
		t.Fatal(err)
	}
	docs, parseErrs := graph.ParseAll(p)
	for path, e := range parseErrs {
		t.Fatalf("parse error in %s: %v", path, e)
	}
	g, err := graph.Build(p, docs, parseErrs)
	if err != nil {
		t.Fatal(err)
	}
	return p, docs, g
}

func renamedTo(res *Result, from string) string {
	for _, rn := range res.Renames {
		if rn.From == from {
			return rn.To
		}
	}
	return ""
}

func TestSquashRichFixtureRenames(t *testing.T) {
	p, docs, g := fixture(t, "rich")
	res, err := Run(p, docs, g, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}

	// Exact deterministic short names, in sorted-assignment order. The
	// v0.2 frozen vanilla list unblocks the custom block texture that
	// lives under textures/blocks/ — it takes _0 as the first eligible
	// texture and shifts every later assignment.
	want := map[string]string{
		"textures/blocks/shulker_custom_block.png":                     "textures/_0.png",
		"textures/custom/block.png":                                    "textures/_1.png",
		"textures/custom/block_mer.png":                                "textures/_1_mer.png",
		"textures/custom/block_normal.png":                             "textures/_1_normal.png",
		"textures/custom/block_heightmap.png":                          "textures/_1_heightmap.png",
		"textures/custom/flip.png":                                     "textures/_2.png",
		"textures/custom/item.png":                                     "textures/_3.png",
		"textures/custom/mob.png":                                      "textures/_4.png",
		"textures/custom/mob_sad.png":                                  "textures/_5.png",
		"textures/custom/particles.png":                                "textures/_6.png",
		"textures/custom/pbr.png":                                      "textures/_7.png",
		"textures/custom/pbr_mer.png":                                  "textures/_7_mer.png",
		"textures/custom/pbr_normal.png":                               "textures/_7_normal.png",
		"textures/custom/pbr_mers.tga":                                 "textures/_7_mers.tga",
		"textures/custom/ui_free.png":                                  "textures/_8.png",
		"sounds/custom/pop.ogg":                                        "sounds/_0.ogg",
		"entity/shulker_mob.entity.json":                               "entity/_0.entity.json",
		"attachables/shulker_hat.json":                                 "attachables/_0.json",
		"animations/shulker_mob.animation.json":                        "animations/_0.animation.json",
		"animation_controllers/shulker_mob.animation_controllers.json": "animation_controllers/_0.animation_controllers.json",
		"render_controllers/shulker_mob.render_controllers.json":       "render_controllers/_0.render_controllers.json",
		"particles/burst.json":                                         "particles/_0.json",
		"models/entity/legacy_mob.geo.json":                            "models/entity/_0.geo.json",
		"models/entity/shulker_mob.geo.json":                           "models/entity/_1.geo.json",
	}
	for from, to := range want {
		if got := renamedTo(res, from); got != to {
			t.Errorf("rename(%s) = %q, want %q", from, got, to)
		}
	}

	// Locked files stay locked, each with a reason on record.
	locked := map[string]bool{
		"textures/blocks/stone.png":          true, // deny-listed vanilla path
		"textures/environment/moon.png":      true, // deny-listed, zero edges
		"font/glyph_20.png":                  true, // fixed engine paths
		"textures/custom/texset_ref.png":     true, // texture_set blocker
		"textures/custom/texset_ref_mer.png": true,
		"textures/custom/ui_panel.png":       true, // unknown-schema blocker
		"manifest.json":                      true,
		"pack_icon.png":                      true,
		"blocks.json":                        true,
		"sounds.json":                        true,
		"textures/terrain_texture.json":      true,
		"ui/custom_screen.json":              true,
	}
	for path := range locked {
		if got := renamedTo(res, path); got != "" {
			t.Errorf("%s was renamed to %q — must stay locked", path, got)
		}
	}
	// Locked *texture candidates* must each have a reason on record; the
	// planner is the honest type that explains itself. (font/ files are
	// ClassFont and never enter the texture candidate set; registries are
	// excluded by KindOf.)
	for _, path := range []string{
		"textures/blocks/stone.png",
		"textures/environment/moon.png",
		"textures/custom/texset_ref.png",
		"textures/custom/ui_panel.png",
	} {
		if _, explained := res.SkipReasons[path]; !explained {
			t.Errorf("%s stayed put with no skip reason", path)
		}
	}
	if r := res.SkipReasons["textures/custom/ui_panel.png"]; !strings.Contains(r, "palette.json") {
		t.Errorf("ui_panel skip reason = %q, want palette.json blocker mention", r)
	}

	// Reference rewrites.
	findRewrite := func(file, from string) (string, bool) {
		for _, rw := range res.Rewrites {
			if rw.File == file && rw.From == from {
				return rw.To, true
			}
		}
		return "", false
	}
	rw, ok := findRewrite("entity/shulker_mob.entity.json", "textures/custom/mob")
	if !ok || rw != "textures/_4" {
		t.Errorf("entity texture rewrite = %q %v, want textures/_4", rw, ok)
	}
	if rw, ok := findRewrite("textures/terrain_texture.json", "textures/custom/block"); !ok || rw != "textures/_1" {
		t.Errorf("terrain block rewrite = %q %v", rw, ok)
	}
	// The v0.2 headline: a custom texture under a vanilla directory
	// renames AND its atlas reference rewrites.
	if rw, ok := findRewrite("textures/terrain_texture.json", "textures/blocks/shulker_custom_block"); !ok || rw != "textures/_0" {
		t.Errorf("custom-under-blocks rewrite = %q %v, want textures/_0", rw, ok)
	}
	if rw, ok := findRewrite("sounds/sound_definitions.json", "sounds/custom/pop"); !ok || rw != "sounds/_0" {
		t.Errorf("sound def rewrite = %q %v", rw, ok)
	}
	if rw, ok := findRewrite("textures_list.json", "textures/custom/mob.png"); !ok || rw != "textures/_4.png" {
		t.Errorf("textures_list rewrite = %q %v", rw, ok)
	}
	// Vanilla override in the atlas: frozen-list-protected, reference
	// untouched.
	if _, changed := findRewrite("textures/terrain_texture.json", "textures/blocks/stone"); changed {
		t.Error("vanilla-override texture must keep its path and its reference")
	}
	// The skip reason must point at the vanilla list (not the old prefix
	// deny-list) so creators can trust the "why".
	if r := res.SkipReasons["textures/blocks/stone.png"]; !strings.Contains(r, "vanilla resource pack path") {
		t.Errorf("stone.png skip reason = %q, want frozen-list explanation", r)
	}

	// Minify: comments/BOM gone, float noise normalized, key order intact.
	outFile := fileAt(res, "sounds/sound_definitions.json")
	if strings.Contains(string(outFile), "//") {
		t.Error("comments survived minification")
	}
	if !strings.Contains(string(outFile), `"volume":0.07`) {
		t.Errorf("float noise not normalized: %s", outFile)
	}
	itemFile := fileAt(res, "textures/item_texture.json")
	if bytes.HasPrefix(itemFile, []byte("\xEF\xBB\xBF")) {
		t.Error("BOM survived minification")
	}
	// Manifest verbatim.
	if !bytes.Equal(fileAt(res, "manifest.json"), fileData(t, p, "manifest.json")) {
		t.Error("manifest was re-serialized — must be verbatim")
	}
	// Vanilla-override bytes untouched.
	if !bytes.Equal(fileAt(res, "textures/blocks/stone.png"), fileData(t, p, "textures/blocks/stone.png")) {
		t.Error("vanilla-override texture bytes changed")
	}
	// Subpacks untouched.
	if !bytes.Equal(fileAt(res, "subpacks/low_res/manifest.json"), fileData(t, p, "subpacks/low_res/manifest.json")) {
		t.Error("subpack content changed")
	}
}

func fileAt(res *Result, path string) []byte {
	for _, f := range res.Files {
		if f.Path == path {
			return f.Data
		}
	}
	return nil
}

func fileData(t *testing.T, p *pack.Pack, path string) []byte {
	t.Helper()
	f, ok := p.File(path)
	if !ok {
		t.Fatalf("missing %s", path)
	}
	return f.Data
}

// The flagship end-to-end proof: squash the golden fixture, re-load the
// result as a pack, rebuild its graph, and require the post-transform
// validator to find zero defects.
func TestSquashRichFixtureValidates(t *testing.T) {
	p, docs, g := fixture(t, "rich")
	res, err := Run(p, docs, g, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	outPack, err := pack.FromFiles("squashed", res.Files)
	if err != nil {
		t.Fatal(err)
	}
	outDocs, parseErrs := graph.ParseAll(outPack)
	for path, e := range parseErrs {
		t.Fatalf("output %s does not parse: %v", path, e)
	}
	outG, err := graph.Build(outPack, outDocs, parseErrs)
	if err != nil {
		t.Fatal(err)
	}
	r := validate.PostTransform(p, g, outPack, outG, &validate.TransformLog{
		Renames:      res.Renames,
		Rewrites:     res.Rewrites,
		PNGOptimized: res.PNGOptimized,
	})
	for _, f := range r.Errors() {
		t.Errorf("post-transform defect: %s", f.String())
	}
	// No dangling refs among previously-resolving edges is implied by the
	// validator; additionally require the output graph to have no NEW
	// dangling texture/sound path edges beyond the fixture's vanilla refs.
	for _, e := range outG.Edges {
		if e.Target == "" && e.Kind == graph.KindTexturePath &&
			strings.Contains(e.Raw, "custom") {
			t.Errorf("output introduced dangling ref: %s", e.String())
		}
	}
}

func TestSquashDeterministic(t *testing.T) {
	p, docs, g := fixture(t, "rich")
	res1, err := Run(p, docs, g, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	out1, err := pack.FromFiles("squashed", res1.Files)
	if err != nil {
		t.Fatal(err)
	}
	var zip1, zip2 bytes.Buffer
	if err := out1.WriteZip(&zip1); err != nil {
		t.Fatal(err)
	}
	res2, err := Run(p, docs, g, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	out2, err := pack.FromFiles("squashed", res2.Files)
	if err != nil {
		t.Fatal(err)
	}
	if err := out2.WriteZip(&zip2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zip1.Bytes(), zip2.Bytes()) {
		t.Error("squash is not byte-deterministic")
	}
}

func TestSquashToggles(t *testing.T) {
	p, docs, g := fixture(t, "rich")

	// --no-rename: no renames, no rewrites, JSON still minified.
	opts := DefaultOptions().WithRenameDisabled()
	res, err := Run(p, docs, g, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renames) != 0 || len(res.Rewrites) != 0 {
		t.Error("no-rename squash must not rename or rewrite")
	}
	if !bytes.Equal(fileAt(res, "manifest.json"), fileData(t, p, "manifest.json")) {
		t.Error("manifest must always be verbatim")
	}
	if strings.Contains(string(fileAt(res, "blocks.json")), "\n") {
		t.Error("minify should still run with --no-rename")
	}

	// --no-minify --no-rename: byte-identical output (canonical repack only).
	opts = DefaultOptions().WithRenameDisabled()
	opts.Minify = false
	opts.PNGOptimize = false
	res, err = Run(p, docs, g, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Files {
		in, ok := p.File(f.Path)
		if !ok {
			t.Fatalf("unexpected file %s", f.Path)
		}
		if !bytes.Equal(f.Data, in.Data) {
			t.Errorf("%s changed with all transforms off", f.Path)
		}
	}
}

func TestPNGOptimizeKeepsIfSmaller(t *testing.T) {
	// A flat 32x32 image encoded at BestSpeed (how community tools save)
	// should re-encode smaller at BestCompression.
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 8), uint8(y * 8), 0x40, 0xFF})
		}
	}
	var raw bytes.Buffer
	fast := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := fast.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	out, changed, err := OptimizePNG(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Errorf("expected a win on BestSpeed input (%d -> %d)", raw.Len(), len(out))
	}
	// Pixels identical.
	a, err := png.Decode(bytes.NewReader(raw.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	b, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if a.Bounds() != b.Bounds() {
		t.Errorf("bounds changed: %v -> %v", a.Bounds(), b.Bounds())
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if a.At(x, y) != b.At(x, y) {
				t.Fatalf("pixel changed at (%d,%d): %v -> %v", x, y, a.At(x, y), b.At(x, y))
			}
		}
	}
}

func TestPNGOptimizeUndecodablePassthrough(t *testing.T) {
	junk := []byte("this is not a png at all")
	out, changed, err := OptimizePNG(junk)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if changed || !bytes.Equal(out, junk) {
		t.Error("undecodable PNG must pass through unchanged")
	}
}

func TestPNGOptimizeAlphaPreserved(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.NRGBA{0x20, 0x40, 0x80, uint8(x * 32)})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	out, _, err := OptimizePNG(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			want := img.NRGBAAt(x, y)
			got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if got != want {
				t.Fatalf("alpha/color changed at (%d,%d): %v -> %v", x, y, want, got)
			}
		}
	}
}

// The squash pipeline must only PNG-optimize eligible textures: the vanilla
// overrides in the fixture (deny-listed / zero-edge) must never even reach
// the optimizer.
func TestPNGOptimizeScope(t *testing.T) {
	p, docs, g := fixture(t, "rich")
	opts := DefaultOptions()
	opts.PNG = func(data []byte) ([]byte, bool, error) {
		// Accept every "win" so we can observe exactly which files were
		// considered eligible.
		return data, true, nil
	}
	res, err := Run(p, docs, g, opts)
	if err != nil {
		t.Fatal(err)
	}
	eligible := []string{
		"textures/custom/mob.png",
		"textures/custom/pbr.png",
		"textures/custom/pbr_mers.tga", // not a PNG: re-encode skipped, eligibility noted
	}
	for _, path := range eligible[:2] {
		if !res.PNGOptimized[path] {
			t.Errorf("%s should be PNG-eligible", path)
		}
	}
	if res.PNGOptimized["textures/custom/pbr_mers.tga"] {
		t.Error(".tga companions are never re-encoded in v0.1")
	}
	for _, locked := range []string{
		"textures/blocks/stone.png",      // deny-listed vanilla path
		"textures/environment/moon.png",  // deny-listed, zero edges
		"textures/custom/ui_panel.png",   // unknown-schema blocker
		"textures/custom/texset_ref.png", // texture_set blocker
		"pack_icon.png",                  // sacred
	} {
		if res.PNGOptimized[locked] {
			t.Errorf("%s must never reach the PNG optimizer", locked)
		}
	}
}
