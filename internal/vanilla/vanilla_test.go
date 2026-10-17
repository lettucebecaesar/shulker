package vanilla

import (
	"strings"
	"testing"
)

func TestFrozenListSanity(t *testing.T) {
	// Spot checks against MCBVanillaResourcePack@61bd205b0921 (a strict
	// superset of bedrock-samples: it also ships font/, ui/, atmospherics/,
	// color_grading/, lighting/… which bedrock-samples omits).
	for _, p := range []string{
		"textures/blocks/stone.png",
		"textures/entity/pig/pig_v3.png",
		"textures/misc/missing_texture.png",
		"textures/particle/particles.png",
		"sounds/mob/pig/say1.ogg",
		"entity/pig.entity.json",
		"models/entity/pig.geo.json",
		"ui/hud_screen.json",
		"font/minecraft-ten.ttf",
		"atmospherics/atmospherics.json",
	} {
		if !IsVanillaPath(p) {
			t.Errorf("vanilla path missing from frozen list: %s", p)
		}
	}
	// Custom paths must NOT be frozen.
	for _, p := range []string{
		"textures/blocks/shulker_custom_block.png",
		"textures/custom/mob.png",
		"entity/my_thing.entity.json",
	} {
		if IsVanillaPath(p) {
			t.Errorf("custom path frozen: %s", p)
		}
	}
	// Extension-insensitive override detection.
	if !IsVanillaTarget("sounds/mob/pig/say1.wav") {
		t.Error("cross-extension override (say1.wav over say1.ogg) must match")
	}
	if !IsVanillaTarget("textures/blocks/stone.tga") {
		t.Error("cross-extension override (stone.tga over stone.png) must match")
	}
	if IsVanillaTarget("textures/blocks/shulker_custom_block.png") {
		t.Error("custom path must not match extension-insensitively")
	}
}

func TestProvenanceAndCount(t *testing.T) {
	if !strings.HasPrefix(Provenance(), "# MCBVanillaResourcePack@") {
		t.Errorf("provenance = %q", Provenance())
	}
	if Count() < 55000 {
		t.Errorf("suspiciously small frozen list: %d", Count())
	}
}
