package transform

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/pack"
	"github.com/shulker-tools/shulker/internal/validate"
)

// encodeNRGBA is a test helper producing PNG bytes from a function.
func encodeNRGBA(t *testing.T, w, h int, fn func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fn(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeNRGBA(t *testing.T, data []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Normalize to NRGBA for comparison regardless of encoded form.
	n := image.NewNRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			n.Set(x, y, img.At(x, y))
		}
	}
	return n
}

// UI-style art with few distinct colors (including semi-transparency) must
// convert losslessly to an indexed PNG and get smaller.
func TestPaletteConversionLossless(t *testing.T) {
	palette := []color.NRGBA{
		{0x20, 0x40, 0x80, 0xFF}, {0xFF, 0x00, 0x00, 0x80}, {0x00, 0x00, 0x00, 0x00},
		{0xEE, 0xDD, 0xCC, 0xFF}, {0x11, 0x22, 0x33, 0xFF},
	}
	raw := encodeNRGBA(t, 128, 128, func(x, y int) color.NRGBA {
		return palette[(x/8+y/8)%len(palette)]
	})
	out, changed, err := OptimizePNG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatalf("palette conversion should win (raw %d, out %d)", len(raw), len(out))
	}
	if len(out) >= len(raw) {
		t.Errorf("expected smaller: %d -> %d", len(raw), len(out))
	}
	// Pixel identity, including the semi-transparent and fully
	// transparent entries.
	a, b := decodeNRGBA(t, raw), decodeNRGBA(t, out)
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			if a.NRGBAAt(x, y) != b.NRGBAAt(x, y) {
				t.Fatalf("pixel changed at (%d,%d): %v -> %v", x, y,
					a.NRGBAAt(x, y), b.NRGBAAt(x, y))
			}
		}
	}
}

// More than 256 distinct colors: the lossless palette candidate must be
// skipped and the plain re-encode remains.
func TestPaletteConversionSkipsManyColors(t *testing.T) {
	raw := encodeNRGBA(t, 32, 32, func(x, y int) color.NRGBA {
		return color.NRGBA{uint8(x * 8), uint8(y * 8), uint8((x + y) * 4), 0xFF}
	})
	out, _, err := OptimizePNG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, isPaletted, _ := decodeKind(t, out); isPaletted {
		t.Error(">256 colors must not become a palette PNG")
	}
}

func decodeKind(t *testing.T, data []byte) (image.Image, bool, error) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	_, isPaletted := img.(*image.Paletted)
	return img, isPaletted, nil
}

// Quantization is opt-in, deterministic, bounded, and refuses partial
// alpha (semi-transparency must survive untouched).
func TestQuantize(t *testing.T) {
	// A binary-alpha image with many colors: perfect quantization target.
	raw := encodeNRGBA(t, 64, 64, func(x, y int) color.NRGBA {
		a := uint8(255)
		if (x+y)%7 == 0 {
			a = 0
		}
		return color.NRGBA{uint8(x * 4), uint8(y * 4), uint8((x * y) % 256), a}
	})
	opts := PNGOptions{Quantize: true, QuantizeColors: 64}
	out1, changed1, err := OptimizePNGWith(raw, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !changed1 {
		t.Fatal("quantization should win on this image")
	}
	out2, _, err := OptimizePNGWith(raw, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out1, out2) {
		t.Error("quantization is not deterministic")
	}
	img, isPaletted, err := decodeKind(t, out1)
	if err != nil {
		t.Fatal(err)
	}
	if !isPaletted {
		t.Fatal("quantized output must be palette PNG")
	}
	pal := img.(*image.Paletted)
	if len(pal.Palette) > 64 {
		t.Errorf("palette has %d colors, want <= 64", len(pal.Palette))
	}
	if len(out1) >= len(raw) {
		t.Errorf("quantization must be smaller: %d -> %d", len(raw), len(out1))
	}
	// Alpha values survive exactly (binary alpha only).
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			wantA := uint8(255)
			if (x+y)%7 == 0 {
				wantA = 0
			}
			if got := pal.At(x, y).(color.NRGBA).A; got != wantA {
				t.Fatalf("alpha changed at (%d,%d): %d", x, y, got)
			}
		}
	}

	// Without the flag, the same image must NOT be quantized.
	outPlain, _, err := OptimizePNG(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, isPaletted, _ := decodeKind(t, outPlain); isPaletted {
		plain := mustDecode(t, outPlain).(*image.Paletted)
		if len(plain.Palette) > 256 {
			t.Fatal("unreachable")
		}
		// A <=256-color palette here is fine ONLY if it is the lossless
		// conversion; this image has ~4096+ colors, so it must not be
		// paletted at all.
		t.Error("quantization ran without the opt-in flag")
	}

	// Partial alpha: quantization must refuse (lossless palette conversion
	// may still apply, but no color approximation).
	rawPartial := encodeNRGBA(t, 64, 64, func(x, y int) color.NRGBA {
		return color.NRGBA{uint8(x * 4), uint8(y * 4), 0x80, uint8((x * 4) % 256)}
	})
	outQ, _, err := OptimizePNGWith(rawPartial, opts)
	if err != nil {
		t.Fatal(err)
	}
	qImg, isPal, err := decodeKind(t, outQ)
	if err != nil {
		t.Fatal(err)
	}
	if isPal {
		// If paletted, it must be the LOSSLESS conversion — verify pixel
		// identity to be sure the lossy path didn't fire.
		a, b := decodeNRGBA(t, rawPartial), decodeNRGBA(t, outQ)
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				if a.NRGBAAt(x, y) != b.NRGBAAt(x, y) {
					t.Fatalf("partial-alpha image was quantized at (%d,%d)", x, y)
				}
			}
		}
		_ = qImg
	}
}

func mustDecode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// The overrides pass is pixel-verified: accept a genuine lossless win,
// refuse anything that would change a single pixel.
func TestOptimizePNGLosslessVerified(t *testing.T) {
	raw := encodeNRGBA(t, 128, 128, func(x, y int) color.NRGBA {
		return color.NRGBA{uint8(x % 32), uint8(y % 16), 0x40, 0xFF}
	})
	out, changed, err := OptimizePNGLosslessVerified(raw)
	if err != nil {
		t.Fatalf("lossless win must pass verification: %v", err)
	}
	if !changed {
		t.Fatal("expected the palette win")
	}
	a, b := decodeNRGBA(t, raw), decodeNRGBA(t, out)
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			if a.NRGBAAt(x, y) != b.NRGBAAt(x, y) {
				t.Fatalf("verified re-encode changed a pixel at (%d,%d)", x, y)
			}
		}
	}
}

// The full pipeline view: --png-optimize-overrides re-encodes the vanilla
// overrides the default pass refuses to touch, logs them distinctly, and
// the post-transform validator accepts the accounted result.
func TestSquashOverridesOptIn(t *testing.T) {
	p, docs, g := fixture(t, "rich")

	// Deterministic injected re-encoder: accept every input as a "win" by
	// appending a marker we can recognize later. Using the real optimizer
	// on the 16x16 solid-color fixture PNGs often loses keep-if-smaller, so
	// inject to test the accounting; the real optimizer's pixel behavior is
	// proven separately above.
	opts := DefaultOptions()
	opts.PNGOptimizeOverrides = true
	opts.PNG = func(data []byte) ([]byte, bool, error) { return data, false, nil }
	opts.PNGVerify = func(data []byte) ([]byte, bool, error) {
		out := append(append([]byte(nil), data...), []byte("//shrunk")...)
		return out, true, nil
	}
	res, err := Run(p, docs, g, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"textures/blocks/stone.png",      // vanilla path
		"textures/environment/moon.png",  // deny-listed
		"textures/custom/ui_panel.png",   // unknown-schema blocker
		"textures/custom/texset_ref.png", // texture_set blocker
	} {
		if !res.OverridesOptimized[want] {
			t.Errorf("%s should be override-eligible", want)
		}
		if !res.PNGOptimized[want] {
			t.Errorf("%s should be recorded as re-encoded", want)
		}
	}
	if res.OverridesOptimized["textures/custom/mob.png"] {
		t.Error("regular eligible textures must not appear in the override set")
	}
	if res.OverridesOptimized["pack_icon.png"] {
		t.Error("pack_icon is sacred")
	}
	// The default pass fn never wins; overrides fn does: mob.png bytes
	// must be untouched, stone.png bytes must carry the marker.
	if !bytes.Equal(fileAt(res, "textures/blocks/stone.png"),
		append(fileData(t, p, "textures/blocks/stone.png"), []byte("//shrunk")...)) {
		t.Error("override re-encode was not applied to stone.png")
	}
	// The default pass fn never wins; overrides fn does. The eligible
	// texture keeps its exact bytes under its NEW name; stone.png bytes
	// carry the override marker under the SAME name.
	if !bytes.Equal(fileAt(res, "textures/_4.png"), fileData(t, p, "textures/custom/mob.png")) {
		t.Error("default-pass texture bytes changed without a win")
	}
}

// End-to-end: the override pass through the real pipeline must survive the
// post-transform validator (paths unchanged, bytes accounted).
func TestSquashOverridesOptInValidates(t *testing.T) {
	p, docs, g := fixture(t, "rich")
	opts := DefaultOptions()
	opts.PNGOptimizeOverrides = true
	opts.PNGVerify = func(data []byte) ([]byte, bool, error) {
		out := append(append([]byte(nil), data...), []byte("//shrunk")...)
		return out, true, nil
	}
	res, err := Run(p, docs, g, opts)
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
		Renames:            res.Renames,
		Rewrites:           res.Rewrites,
		PNGOptimized:       res.PNGOptimized,
		OverridesOptimized: res.OverridesOptimized,
	})
	for _, f := range r.Errors() {
		t.Errorf("post-transform defect: %s", f.String())
	}
}
