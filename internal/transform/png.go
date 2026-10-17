package transform

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// PNGOptions selects PNG re-encode behavior.
type PNGOptions struct {
	// Quantize enables lossy median-cut quantization (opt-in: it changes
	// pixels). v0.2 guards it to binary-alpha (fully opaque or fully
	// transparent) images only Ã¢â‚¬â€ partial transparency is left alone.
	Quantize bool
	// QuantizeColors is the palette size target for quantization
	// (2..256; 0 means 256).
	QuantizeColors int
}

// OptimizePNG re-encodes a PNG at maximum compression and keeps the result
// only when it is actually smaller (keep-if-smaller: never trade size for
// risk).
//
// Candidates considered:
//   - a straight BestCompression re-encode of the decoded image,
//   - a LOSSLESS conversion to an indexed (palette) PNG when the image uses
//     at most 256 distinct colors Ã¢â‚¬â€ pixel-identical, often dramatically
//     smaller for UI-style art,
//   - with Quantize enabled: a lossy median-cut quantized palette PNG
//     (pixels change Ã¢â‚¬â€ that is what opt-in means).
//
// What changes intentionally on the lossless paths: interlacing is dropped
// (Adam7 costs bytes and the engine does not need it) and ancillary chunks
// Go does not model (iCCP profiles, tEXt metadata, some gAMA/sRGB data)
// disappear Ã¢â‚¬â€ the engine renders pixels, not profiles.
//
// NO quantization by default (v0.1 ethos preserved); lossy is always opt-in.
func OptimizePNG(data []byte) (out []byte, reencoded bool, err error) {
	return OptimizePNGWith(data, PNGOptions{})
}

// OptimizePNGWith is OptimizePNG with explicit options.
func OptimizePNGWith(data []byte, opts PNGOptions) ([]byte, bool, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		// Undecodable PNG: pass through untouched with a warning from the
		// caller. Never corrupt what we cannot read.
		return data, false, err
	}

	best := encodeAtBest(img)
	if best == nil {
		return data, false, nil
	}
	bestChanged := len(best) < len(data)

	// Lossless palette conversion: only for straight-alpha NRGBA images
	// (Go decodes 8-bit RGBA PNGs to *image.NRGBA). Premultiplied RGBA
	// would lose precision converting back to straight-alpha palette
	// entries, so those keep the plain re-encode.
	if nrgba, ok := img.(*image.NRGBA); ok && !nrgba.Bounds().Empty() {
		if pal, ok := toPalettedLossless(nrgba); ok {
			if enc := encodeAtBest(pal); encChanged(enc, best, data) {
				best = enc
				bestChanged = len(best) < len(data)
			}
		}
		if opts.Quantize {
			if pal, ok := quantize(nrgba, clampColors(opts.QuantizeColors)); ok {
				if enc := encodeAtBest(pal); encChanged(enc, best, data) {
					best = enc
					bestChanged = len(best) < len(data)
				}
			}
		}
	}

	if bestChanged {
		return best, true, nil
	}
	return data, false, nil
}

// OptimizePNGLosslessVerified re-encodes without any lossy pass and
// verifies pixel-identity before accepting the result. It backs the
// --png-optimize-overrides opt-in, where the file is a vanilla override:
// the path must stay and the pixels must stay, only the bytes may shrink.
func OptimizePNGLosslessVerified(data []byte) ([]byte, bool, error) {
	out, changed, err := OptimizePNGWith(data, PNGOptions{})
	if err != nil || !changed {
		return out, changed, err
	}
	same, perr := pixelsEqual(data, out)
	if perr != nil || !same {
		// A lossless path producing different pixels is a shulker bug.
		// Refuse the win, keep the original bytes, say why.
		return data, false, fmt.Errorf("re-encode failed pixel verification")
	}
	return out, true, nil
}

func encodeAtBest(img image.Image) []byte {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil // caller falls back to the original bytes
	}
	return buf.Bytes()
}

// encChanged reports whether the palette candidate beats the current best
// and the original.
func encChanged(enc, best []byte, data []byte) bool {
	return len(enc) > 0 && (best == nil || len(enc) < len(best)) && len(enc) < len(data)
}

func clampColors(n int) int {
	if n <= 0 || n > 256 {
		return 256
	}
	if n < 2 {
		return 2
	}
	return n
}

// rgbaKey is one straight-alpha pixel color as a comparable byte quad.
type rgbaKey [4]byte

// toPalettedLossless converts an NRGBA image with at most 256 distinct
// colors into an indexed PNG image, pixel-identical by construction:
// palette entries are the exact NRGBA values found in the image.
func toPalettedLossless(img *image.NRGBA) (*image.Paletted, bool) {
	b := img.Bounds()
	if b.Empty() {
		return nil, false
	}
	pix := img.Pix
	stride := img.Stride
	index := make(map[rgbaKey]int, 256)
	palette := make(color.Palette, 0, 256)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := pix[(y-b.Min.Y)*stride:]
		for x := 0; x < b.Dx(); x++ {
			k := rgbaKey{row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]}
			if _, ok := index[k]; !ok {
				if len(palette) >= 256 {
					return nil, false
				}
				index[k] = len(palette)
				palette = append(palette, color.NRGBA{R: k[0], G: k[1], B: k[2], A: k[3]})
			}
		}
	}
	out := image.NewPaletted(image.Rect(0, 0, b.Dx(), b.Dy()), palette)
	for y := 0; y < out.Bounds().Dy(); y++ {
		row := pix[y*stride:]
		for x := 0; x < out.Bounds().Dx(); x++ {
			out.SetColorIndex(x, y, uint8(index[rgbaKey{row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]}]))
		}
	}
	return out, true
}

// quantize implements median-cut color quantization for binary-alpha
// images. Deterministic: unique colors are collected in first-appearance
// order and boxes split on fixed rules, so identical input always yields an
// identical palette. Alpha is quantized too (it only ever holds 0 or 255
// here), which is why partial-alpha images are excluded.
func quantize(img *image.NRGBA, target int) (*image.Paletted, bool) {
	b := img.Bounds()
	if b.Empty() {
		return nil, false
	}
	// Collect unique colors with counts, preserving binary alpha only.
	colors := make([]rgbaKey, 0, 256)
	counts := make(map[rgbaKey]int, 256)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[(y-b.Min.Y)*img.Stride:]
		for x := 0; x < b.Dx(); x++ {
			k := rgbaKey{row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]}
			if k[3] != 0 && k[3] != 255 {
				return nil, false // partial alpha: not in quantization's contract
			}
			if _, seen := counts[k]; !seen {
				colors = append(colors, k)
			}
			counts[k]++
		}
	}
	if len(colors) <= target {
		// Nothing to reduce Ã¢â‚¬â€ the lossless palette path already wins.
		return nil, false
	}

	// Median cut over the unique colors, weighted by pixel counts.
	type box struct {
		colors []rgbaKey
		weight int
	}
	var boxes []box
	totalWeight := 0
	for _, k := range colors {
		totalWeight += counts[k]
	}
	boxes = append(boxes, box{colors: colors, weight: totalWeight})
	for len(boxes) < target {
		// Pick the box with the widest channel range to split.
		bi, bestRange := -1, -1
		for i, bx := range boxes {
			if len(bx.colors) < 2 {
				continue
			}
			if r := rangeOf(bx.colors); r > bestRange {
				bi, bestRange = i, r
			}
		}
		if bi < 0 {
			break
		}
		// Split at the weighted median along the widest channel.
		left := boxes[bi]
		sortBox(left.colors, widestChannel(left.colors))
		mid := len(left.colors) / 2
		acc := 0
		for i, k := range left.colors {
			acc += counts[k]
			if acc >= left.weight/2 && i+1 < len(left.colors) {
				mid = i + 1
				break
			}
		}
		right := box{colors: append([]rgbaKey(nil), left.colors[mid:]...)}
		right.weight = 0
		for _, k := range right.colors {
			right.weight += counts[k]
		}
		left.colors = left.colors[:mid]
		left.weight -= right.weight
		boxes[bi] = left
		boxes = append(boxes, right)
	}

	// Palette entry per box: the count-weighted average of the box.
	palette := make(color.Palette, 0, len(boxes))
	assign := make(map[rgbaKey]int, len(colors))
	for _, bx := range boxes {
		var rs, gs, bs, as, ws uint64
		for _, k := range bx.colors {
			w := uint64(counts[k])
			rs += uint64(k[0]) * w
			gs += uint64(k[1]) * w
			bs += uint64(k[2]) * w
			as += uint64(k[3]) * w
			ws += w
		}
		avg := color.NRGBA{A: 255}
		if ws > 0 {
			avg = color.NRGBA{
				R: uint8(rs / ws), G: uint8(gs / ws), B: uint8(bs / ws), A: uint8(as / ws),
			}
		}
		pi := len(palette)
		palette = append(palette, avg)
		for _, k := range bx.colors {
			assign[k] = pi
		}
	}

	out := image.NewPaletted(image.Rect(0, 0, b.Dx(), b.Dy()), palette)
	for y := 0; y < b.Dy(); y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < b.Dx(); x++ {
			out.SetColorIndex(x, y, uint8(assign[rgbaKey{row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]}]))
		}
	}
	return out, true
}

func rangeOf(colors []rgbaKey) int {
	var mn, mx [4]byte
	if len(colors) > 0 {
		mn, mx = colors[0], colors[0]
	}
	for _, k := range colors {
		for c := 0; c < 4; c++ {
			if k[c] < mn[c] {
				mn[c] = k[c]
			}
			if k[c] > mx[c] {
				mx[c] = k[c]
			}
		}
	}
	best := 0
	for c := 0; c < 4; c++ {
		if int(mx[c])-int(mn[c]) > best {
			best = int(mx[c]) - int(mn[c])
		}
	}
	return best
}

func widestChannel(colors []rgbaKey) int {
	var mn, mx [4]byte
	if len(colors) > 0 {
		mn, mx = colors[0], colors[0]
	}
	for _, k := range colors {
		for c := 0; c < 4; c++ {
			if k[c] < mn[c] {
				mn[c] = k[c]
			}
			if k[c] > mx[c] {
				mx[c] = k[c]
			}
		}
	}
	best, bestRange := 0, -1
	for c := 0; c < 4; c++ {
		if r := int(mx[c]) - int(mn[c]); r > bestRange {
			best, bestRange = c, r
		}
	}
	return best
}

// sortBox orders colors in place along one channel (insertion sort Ã¢â‚¬â€ the
// per-box color counts are small and this keeps the algorithm free of
// sort.Slice's non-deterministic tie-breaking).
func sortBox(colors []rgbaKey, channel int) {
	for i := 1; i < len(colors); i++ {
		for j := i; j > 0 && colors[j][channel] < colors[j-1][channel]; j-- {
			colors[j], colors[j-1] = colors[j-1], colors[j]
		}
	}
}

// pixelsEqual decodes both PNGs and compares every pixel exactly.
func pixelsEqual(a, b []byte) (bool, error) {
	ia, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		return false, err
	}
	ib, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return false, err
	}
	if ia.Bounds() != ib.Bounds() {
		return false, nil
	}
	for y := ia.Bounds().Min.Y; y < ia.Bounds().Max.Y; y++ {
		for x := ia.Bounds().Min.X; x < ia.Bounds().Max.X; x++ {
			// color.Color comparison via NRGBA normalization: both decoders
			// yield straight-alpha representations for the color types we
			// produce.
			ca := color.NRGBAModel.Convert(ia.At(x, y)).(color.NRGBA)
			cb := color.NRGBAModel.Convert(ib.At(x, y)).(color.NRGBA)
			if ca != cb {
				return false, nil
			}
		}
	}
	return true, nil
}
