//go:build ignore

// Command testdata/gen materializes the binary fixture files (PNGs, a TGA,
// an OGG stub) used by the golden fixture packs. Everything it writes is
// deterministic: fixed colors, no timestamps, no randomness. Text fixtures
// are authored by hand in the repository; only bytes that cannot be written
// as reviewable text live here.
//
// Run from the repository root:  go run ./testdata/gen
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	// Locate the repository root by walking up until go.mod is found, so
	// this works no matter where the user invokes it from.
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	pngs := map[string]color.RGBA{
		"testdata/packs/rich/pack_icon.png":                       {0x94, 0x2C, 0xF5, 0xFF},
		"testdata/packs/minimal/pack_icon.png":                    {0x10, 0x20, 0x30, 0xFF},
		"testdata/packs/broken/pack_icon.png":                     {0x30, 0x20, 0x10, 0xFF},
		"testdata/packs/rich/textures/custom/block.png":           {0xC0, 0x39, 0x2B, 0xFF},
		"testdata/packs/rich/textures/custom/block_mer.png":       {0x00, 0x00, 0x00, 0xFF},
		"testdata/packs/rich/textures/custom/block_normal.png":    {0x80, 0x80, 0xFF, 0xFF},
		"testdata/packs/rich/textures/custom/block_heightmap.png": {0x80, 0x80, 0x80, 0xFF},
		"testdata/packs/rich/textures/custom/pbr.png":             {0x44, 0x86, 0x6B, 0xFF},
		"testdata/packs/rich/textures/custom/pbr_mer.png":         {0x40, 0x00, 0x40, 0xFF},
		"testdata/packs/rich/textures/custom/pbr_normal.png":      {0x80, 0x80, 0xFF, 0xFF},
		"testdata/packs/rich/textures/custom/flip.png":            {0x1E, 0x9E, 0x4F, 0xFF},
		"testdata/packs/rich/textures/custom/item.png":            {0xF5, 0xC3, 0x2C, 0xFF},
		"testdata/packs/rich/textures/custom/mob.png":             {0x59, 0x36, 0xC4, 0xFF},
		"testdata/packs/rich/textures/custom/mob_sad.png":         {0x27, 0x4E, 0x8C, 0xFF},
		"testdata/packs/rich/textures/custom/ui_panel.png":        {0xD7, 0xD7, 0xD7, 0xC0},
		"testdata/packs/rich/textures/custom/ui_free.png":         {0xB1, 0xE2, 0xF0, 0xC0},
		"testdata/packs/rich/textures/custom/particles.png":       {0xFF, 0xFF, 0xFF, 0x80},
		"testdata/packs/rich/textures/custom/texset_ref.png":      {0x77, 0xAA, 0xBB, 0xFF},
		"testdata/packs/rich/textures/custom/texset_ref_mer.png":  {0x20, 0x40, 0x60, 0xFF},
		"testdata/packs/rich/textures/blocks/stone.png":           {0x7F, 0x7F, 0x7F, 0xFF},
		"testdata/packs/rich/textures/environment/moon.png":       {0xE8, 0xE8, 0xD0, 0xFF},
		"testdata/packs/rich/font/glyph_20.png":                   {0x11, 0x11, 0x11, 0xFF},
		"testdata/packs/rich/subpacks/low_res/textures/item.png":  {0x99, 0x88, 0x77, 0xFF},
	}
	for path, c := range pngs {
		if err := writePNG(filepath.Join(root, path), c); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	// PBR set 2's MERS companion is .tga, mirroring current vanilla where
	// color maps are .png and PBR companions ship as .tga.
	if err := writeTGA(filepath.Join(root, "testdata/packs/rich/textures/custom/pbr_mers.tga"), color.RGBA{0x60, 0x00, 0x60, 0xFF}); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// OGG stub: enough header for tools to recognize the type; content is
	// irrelevant because v0.1 passes audio through byte-for-byte.
	ogg := append([]byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00"), make([]byte, 64)...)
	ogg = append([]byte(nil), ogg...)
	for _, path := range []string{
		"testdata/packs/rich/sounds/custom/pop.ogg",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(root, path), ogg, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	// item_texture.json with a UTF-8 BOM: community packs ship these, the
	// engine tolerates them, and minify must strip the BOM.
	itemTexture := "\xEF\xBB\xBF" + `{
	"resource_pack_name": "shulker_fixture",
	"texture_name": "atlas.item",
	"texture_data": {
		"shulker.item": {
			"textures": "textures/custom/item",
		},
	}
}
`
	if err := os.WriteFile(filepath.Join(root, "testdata/packs/rich/textures/item_texture.json"), []byte(itemTexture), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println("fixture binaries written")
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

// writePNG writes a 16x16 solid RGBA PNG via the stdlib encoder.
func writePNG(path string, c color.RGBA) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = []byte{c.R, c.G, c.B, c.A}[i%4]
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// writeTGA writes an uncompressed 16x16 32-bit TGA.
func writeTGA(path string, c color.RGBA) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	// TGA header: no ID, no color map, type 2 (uncompressed truecolor),
	// 32-bit, top-down (descriptor 0x20).
	buf.Write([]byte{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		16, 0, 16, 0, 32, 0x20})
	for range 16 * 16 {
		buf.Write([]byte{c.B, c.G, c.R, c.A})
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
