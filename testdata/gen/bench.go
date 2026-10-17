package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------- json_heavy

// genJSONHeavy writes a pack whose weight is author-formatted JSON: 4-space
// indentation, comment banners, trailing commas, BOMs, float noise — the
// way real packs actually look on disk.
func genJSONHeavy(dir string) error {
	w := func(rel, content string) error {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, []byte(content), 0o644)
	}

	if err := w("manifest.json", `{
	"format_version": 2,
	"header": {
		"name": "JSON Heavy Benchmark",
		"description": "Deterministic fixture: author-formatted JSON everywhere",
		"uuid": "66c6e9a8-3093-462a-9c36-dbb052165822",
		"version": [1, 0, 0],
		"min_engine_version": [1, 21, 0]
	},
	"modules": [
		{
			"description": "resources",
			"type": "resources",
			"uuid": "743f6949-53be-44b6-b326-398005028819",
			"version": [1, 0, 0]
		}
	]
}
`); err != nil {
		return err
	}
	if err := w("pack_icon.png", ""); err != nil { // replaced below with real bytes
		return err
	}
	if err := writePNG(filepath.Join(dir, "pack_icon.png"), color.RGBA{0x11, 0x22, 0x33, 0xFF}); err != nil {
		return err
	}

	// terrain_texture: 40 short names across all four vanilla value shapes.
	var terrain strings.Builder
	terrain.WriteString("// Terrain atlas — block texture registrations.\n{\n")
	terrain.WriteString("\t\"resource_pack_name\": \"json_heavy\",\n")
	terrain.WriteString("\t\"texture_name\": \"atlas.terrain\",\n")
	terrain.WriteString("\t\"padding\": 8,\n\t\"num_mip_levels\": 4,\n\t\"texture_data\": {\n")
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("bench.block_%02d", i)
		switch i % 4 {
		case 0:
			fmt.Fprintf(&terrain, "\t\t// plain color map\n\t\t%q: {\"textures\": \"textures/custom/b_%02d\"},\n", name, i)
		case 1:
			fmt.Fprintf(&terrain, "\t\t// variant array (grass_top style)\n\t\t%q: {\"textures\": [\"textures/custom/b_%02d\", \"textures/custom/b_%02d\"]},\n",
				name, i, (i+1)%40)
		case 2:
			fmt.Fprintf(&terrain, "\t\t// tinted object (grass_side style)\n\t\t%q: {\"textures\": {\"path\": \"textures/custom/b_%02d\", \"overlay_color\": \"#79c05a\"}},\n", name, i)
		case 3:
			fmt.Fprintf(&terrain, "\t\t// tinted array entry\n\t\t%q: {\"textures\": [{\"path\": \"textures/custom/b_%02d\", \"tint_color\": \"#df6827\"}]},\n", name, i)
		}
	}
	terrain.WriteString("\t},\n}\n")
	if err := w("textures/terrain_texture.json", terrain.String()); err != nil {
		return err
	}

	// blocks.json: every short name bound to a block.
	var blocks strings.Builder
	blocks.WriteString("{\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&blocks, "\t// block %d\n\t\"bench:block_%02d\": {\"sound\": \"stone\", \"textures\": \"bench.block_%02d\"},\n", i, i, i)
	}
	blocks.WriteString("}\n")
	if err := w("blocks.json", blocks.String()); err != nil {
		return err
	}

	// 8 client entities, fully loaded.
	for e := 0; e < 8; e++ {
		var sb strings.Builder
		fmt.Fprintf(&sb, "{\n\t// Client entity for bench:mob_%02d\n\t\"format_version\": \"1.10.0\",\n", e)
		sb.WriteString("\t\"minecraft:client_entity\": {\n\t\t\"description\": {\n")
		fmt.Fprintf(&sb, "\t\t\t\"identifier\": \"bench:mob_%02d\",\n", e)
		sb.WriteString("\t\t\t\"materials\": { \"default\": \"entity_alphatest\" },\n")
		sb.WriteString("\t\t\t\"textures\": {\n")
		for k := 0; k < 4; k++ {
			fmt.Fprintf(&sb, "\t\t\t\t\"variant_%d\": \"textures/custom/b_%02d\",\n", k, (e*4+k)%40)
		}
		sb.WriteString("\t\t\t},\n")
		fmt.Fprintf(&sb, "\t\t\t\"geometry\": { \"default\": \"geometry.mob_%02d\" },\n", e)
		sb.WriteString("\t\t\t\"animations\": {\n")
		for a := 0; a < 6; a++ {
			fmt.Fprintf(&sb, "\t\t\t\t\"anim_%d\": \"animation.mob_%02d.state_%d\",\n", a, e, a)
		}
		sb.WriteString("\t\t\t},\n")
		sb.WriteString("\t\t\t\"scripts\": {\n")
		sb.WriteString("\t\t\t\t\"pre_animation\": [\n")
		for a := 0; a < 4; a++ {
			fmt.Fprintf(&sb, "\t\t\t\t\t\"v.mood_%d = math.clamp(query.health / query.max_health, 0.0, 1.0);\"", a)
			if a < 3 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\t\t\t\t],\n")
		sb.WriteString("\t\t\t\t\"scale\": \"query.is_baby ? 0.550000011920929 : 1.0\"\n")
		sb.WriteString("\t\t\t},\n")
		fmt.Fprintf(&sb, "\t\t\t\"render_controllers\": [ \"controller.render.mob_%02d\" ],\n", e)
		fmt.Fprintf(&sb, "\t\t\t\"particle_effects\": { \"aura_%d\": \"bench:aura_%d\" },\n", e, e)
		sb.WriteString("\t\t\t\"spawn_egg\": { \"texture\": \"bench.spawn_egg\" }\n")
		sb.WriteString("\t\t}\n\t}\n}\n")
		if err := w(fmt.Sprintf("entity/mob_%02d.entity.json", e), sb.String()); err != nil {
			return err
		}
	}

	// 6 animation files with real molang.
	for f := 0; f < 6; f++ {
		var sb strings.Builder
		fmt.Fprintf(&sb, "{\n\t// Animations for mob file %d\n\t\"format_version\": \"1.8.0\",\n\t\"animations\": {\n", f)
		for a := 0; a < 3; a++ {
			fmt.Fprintf(&sb, "\t\t\"animation.mob_%02d.state_%d\": {\n", f, a)
			sb.WriteString("\t\t\t\"loop\": true,\n\t\t\t\"animation_length\": 1.5,\n\t\t\t\"bones\": {\n")
			for b := 0; b < 3; b++ {
				fmt.Fprintf(&sb, "\t\t\t\t\"bone_%d\": {\n\t\t\t\t\t\"rotation\": [\"math.sin(query.anim_time * 30) * %d.5\", 0, 0]\n\t\t\t\t},\n", b, b+1)
			}
			sb.WriteString("\t\t\t},\n\t\t\t\"timeline\": {\n\t\t\t\t\"0.0\": { \"sound_effects\": [ { \"effect\": \"bench.hum\" } ] }\n\t\t\t}\n\t\t},\n")
		}
		sb.WriteString("\t}\n}\n")
		if err := w(fmt.Sprintf("animations/mob_%02d.animation.json", f), sb.String()); err != nil {
			return err
		}
	}

	// 4 json-ui screens, deeply nested and generously commented.
	for u := 0; u < 4; u++ {
		var sb strings.Builder
		fmt.Fprintf(&sb, "// UI screen %d — built by the bench generator.\n{\n\t\"namespace\": \"bench_ui\",\n", u)
		fmt.Fprintf(&sb, "\t\"bench_screen_%d\": {\n\t\t\"type\": \"panel\",\n\t\t\"controls\": [\n", u)
		for c := 0; c < 12; c++ {
			fmt.Fprintf(&sb, "\t\t\t{\n\t\t\t\t// row %d: a labeled icon button\n\t\t\t\t\"type\": \"panel\",\n", c)
			sb.WriteString("\t\t\t\t\"controls\": [\n")
			sb.WriteString("\t\t\t\t\t{\n\t\t\t\t\t\t\"type\": \"image\",\n\t\t\t\t\t\t\"texture\": \"textures/custom/b_00\",\n\t\t\t\t\t\t\"size\": [16, 16],\n\t\t\t\t\t},\n")
			sb.WriteString("\t\t\t\t\t{\n\t\t\t\t\t\t\"type\": \"label\",\n\t\t\t\t\t\t\"text\": \"bench_ui.label\",\n\t\t\t\t\t\t\"color\": \"white\",\n\t\t\t\t\t},\n")
			sb.WriteString("\t\t\t\t],\n\t\t\t},\n")
		}
		sb.WriteString("\t\t],\n\t},\n}\n")
		if err := w(fmt.Sprintf("ui/screen_%02d.json", u), sb.String()); err != nil {
			return err
		}
	}

	// sound_definitions with 32-bit float noise, vanilla style.
	var defs strings.Builder
	defs.WriteString("{\n\t\"format_version\": \"1.14.0\",\n\t\"sound_definitions\": {\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&defs, "\t\t\"bench.hum_%02d\": {\n\t\t\t\"category\": \"ambient\",\n\t\t\t\"sounds\": [\n", i)
		for s := 0; s < 3; s++ {
			fmt.Fprintf(&defs, "\t\t\t\t{ \"name\": \"sounds/custom/hum_%02d_%d\", \"volume\": 0.%d%d%d, \"pitch\": 1.%d0 },\n",
				i, s, (i%4)+5, (s%3)+2, (i+s)%10, (s%3)+1)
		}
		defs.WriteString("\t\t\t],\n\t\t},\n")
	}
	defs.WriteString("\t}\n}\n")
	if err := w("sounds/sound_definitions.json", defs.String()); err != nil {
		return err
	}

	// sounds.json events.
	var events strings.Builder
	events.WriteString("{\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&events, "\t\"bench.event_%02d\": { \"category\": \"ambient\", \"sounds\": [ { \"name\": \"bench.hum_%02d\", \"volume\": 1.0 } ] },\n", i, i)
	}
	events.WriteString("}\n")
	if err := w("sounds.json", events.String()); err != nil {
		return err
	}

	if err := w("textures/item_texture.json", "\xEF\xBB\xBF{\n\t\"texture_data\": { \"bench.spawn_egg\": { \"textures\": \"textures/custom/b_01\" } }\n}\n"); err != nil {
		return err
	}
	if err := w("texts/languages.json", "[\n\t\"en_US\"\n]\n"); err != nil {
		return err
	}
	if err := w("texts/en_US.lang", "## bench\nbench.label=Hello\n"); err != nil {
		return err
	}

	// The textures the registries reference (tiny; they exist so references
	// resolve — this pack measures JSON, not PNG).
	for i := 0; i < 40; i++ {
		if err := writePNG(filepath.Join(dir, fmt.Sprintf("textures/custom/b_%02d.png", i)),
			color.RGBA{uint8(i * 6), uint8(255 - i*5), 0x40, 0xFF}); err != nil {
			return err
		}
	}
	// Audio payload so sound paths resolve.
	ogg := append([]byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00"), make([]byte, 32)...)
	if err := os.MkdirAll(filepath.Join(dir, "sounds/custom"), 0o755); err != nil {
		return err
	}
	for i := 0; i < 30; i++ {
		for s := 0; s < 3; s++ {
			p := filepath.Join(dir, fmt.Sprintf("sounds/custom/hum_%02d_%d.ogg", i, s))
			if err := os.WriteFile(p, ogg, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// genUIPalette writes a pack of UI-style sprites: flat colors, hard edges,
// binary alpha, few distinct colors per sprite — the art style where
// shulker v0.2's lossless palette conversion (RGBA -> indexed PNG) wins
// big. Sprites are BestSpeed-encoded, as community exporters save them.
func genUIPalette(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "textures/custom"), 0o755); err != nil {
		return err
	}
	manifest := `{
	"format_version": 2,
	"header": {
		"name": "UI Palette Benchmark",
		"description": "Deterministic fixture: low-color sprites for palette conversion",
		"uuid": "66c6e9a8-3093-462a-9c36-dbb052165822",
		"version": [1, 0, 0],
		"min_engine_version": [1, 21, 0]
	},
	"modules": [
		{
			"description": "resources",
			"type": "resources",
			"uuid": "743f6949-53be-44b6-b326-398005028819",
			"version": [1, 0, 0]
		}
	]
}
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		return err
	}
	if err := writePNG(filepath.Join(dir, "pack_icon.png"), color.RGBA{0x30, 0x60, 0x90, 0xFF}); err != nil {
		return err
	}

	const n = 16
	var atlas strings.Builder
	atlas.WriteString("{\n\t\"texture_data\": {\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&atlas, "\t\t\"bench.ui_%02d\": {\"textures\": \"textures/custom/ui_%02d\"},\n", i, i)
	}
	atlas.WriteString("\t}\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "textures/terrain_texture.json"), []byte(atlas.String()), 0o644); err != nil {
		return err
	}

	fast := png.Encoder{CompressionLevel: png.BestSpeed}
	for i := 0; i < n; i++ {
		img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
		// A small fixed ramp: 32 flat colors, plus a two-color frame.
		shade := uint8(i * 4)
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				switch {
				case x < 4 || y < 4 || x >= 124 || y >= 124:
					img.SetNRGBA(x, y, color.NRGBA{0x1A, 0x1A, 0x1A, 0xFF})
				case (x/16+y/16)%2 == 0:
					img.SetNRGBA(x, y, color.NRGBA{shade + 0x40, 0x80, 0xC0, 0xFF})
				case (x/16+y/16)%3 == 0:
					img.SetNRGBA(x, y, color.NRGBA{shade, shade, shade, 0xFF})
				default:
					img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 0}) // binary transparency
				}
			}
		}
		var buf bytes.Buffer
		if err := fast.Encode(&buf, img); err != nil {
			return err
		}
		p := filepath.Join(dir, fmt.Sprintf("textures/custom/ui_%02d.png", i))
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- png_heavy

// genPNGHeavy writes a pack whose weight is PNG bytes: 24 BestSpeed-encoded
// RGBA sprites (the way community exporters save), all referenced from the
// terrain atlas so they are provably pack-internal and therefore
// PNG-eligible. This is the honest worst case for the v0.1 no-quantization
// optimizer.
func genPNGHeavy(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "textures/custom"), 0o755); err != nil {
		return err
	}
	manifest := `{
	"format_version": 2,
	"header": {
		"name": "PNG Heavy Benchmark",
		"description": "Deterministic fixture: BestSpeed-encoded sprites",
		"uuid": "66c6e9a8-3093-462a-9c36-dbb052165822",
		"version": [1, 0, 0],
		"min_engine_version": [1, 21, 0]
	},
	"modules": [
		{
			"description": "resources",
			"type": "resources",
			"uuid": "743f6949-53be-44b6-b326-398005028819",
			"version": [1, 0, 0]
		}
	]
}
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		return err
	}
	if err := writePNG(filepath.Join(dir, "pack_icon.png"), color.RGBA{0x20, 0x40, 0x80, 0xFF}); err != nil {
		return err
	}

	var terrain strings.Builder
	terrain.WriteString("{\n\t\"texture_data\": {\n")
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&terrain, "\t\t\"bench.sprite_%02d\": {\"textures\": \"textures/custom/s_%02d\"},\n", i, i)
	}
	terrain.WriteString("\t}\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "textures/terrain_texture.json"), []byte(terrain.String()), 0o644); err != nil {
		return err
	}

	// Sprite art: flat colored fields, discs, rings, and stripes —
	// representative of item/block/game-UI art. Encoded at BestSpeed to
	// model how community tools write PNGs.
	fast := png.Encoder{CompressionLevel: png.BestSpeed}
	for i := 0; i < 24; i++ {
		img := image.NewRGBA(image.Rect(0, 0, 128, 128))
		base := color.RGBA{uint8(20 + i*3), uint8(40 + i*7), uint8(60 + i*5), 0xFF}
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				img.SetRGBA(x, y, base)
			}
		}
		// A disc.
		cx, cy, r := 64.0, 64.0, 40.0-float64(i%8)
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				d := math.Hypot(float64(x)-cx, float64(y)-cy)
				switch {
				case d < r && d > r-8:
					img.SetRGBA(x, y, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF})
				case d <= r-8 && ((x/8+y/8)%2 == 0):
					img.SetRGBA(x, y, color.RGBA{uint8(i * 10), uint8(i * 5), 0x80, 0xFF})
				}
			}
		}
		// Stripes at the bottom.
		for y := 104; y < 128; y++ {
			for x := 0; x < 128; x++ {
				if (x/4+y/4)%3 == 0 {
					img.SetRGBA(x, y, color.RGBA{0x10, 0x20, 0x30, 0xFF})
				}
			}
		}
		var buf bytes.Buffer
		if err := fast.Encode(&buf, img); err != nil {
			return err
		}
		p := filepath.Join(dir, fmt.Sprintf("textures/custom/s_%02d.png", i))
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}
