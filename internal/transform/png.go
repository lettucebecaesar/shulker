package transform

import (
	"bytes"
	"image/png"
)

// OptimizePNG re-encodes a PNG at maximum compression and keeps the result
// only when it is actually smaller (keep-if-smaller: never trade size for
// risk). Alpha, palettes, and bit depths survive because image/png decodes
// into the corresponding Go image types and re-encodes the same type.
//
// What changes intentionally:
//   - interlacing is dropped (Adam7 costs bytes and the engine does not
//     need it),
//   - ancillary chunks Go does not model (iCCP profiles, tEXt metadata, some
//     gAMA/sRGB data) disappear — the engine renders pixels, not profiles.
//
// What must never change: the pixels. A decoder bug would be caught by the
// validator, not by vibes — but v0.1 only re-encodes textures whose
// references are provably pack-internal, so the blast radius of a bad
// re-encode is exactly the set of files we fully control.
//
// NO quantization in v0.1 (roadmap: v0.2, behind a flag).
func OptimizePNG(data []byte) (out []byte, reencoded bool, err error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		// Undecodable PNG: pass through untouched with a warning from the
		// caller. Never corrupt what we cannot read.
		return data, false, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return data, false, err
	}
	if buf.Len() < len(data) {
		return buf.Bytes(), true, nil
	}
	return data, false, nil
}
