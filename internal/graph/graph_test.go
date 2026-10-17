package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shulker-tools/shulker/internal/pack"
)

func buildGraph(t *testing.T, files map[string]string) (*Graph, *pack.Pack) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return loadGraph(t, dir)
}

func loadGraph(t *testing.T, dir string) (*Graph, *pack.Pack) {
	t.Helper()
	p, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	docs, parseErrs := ParseAll(p)
	for path, e := range parseErrs {
		t.Fatalf("parse error in %s: %v", path, e)
	}
	g, err := Build(p, docs, parseErrs)
	if err != nil {
		t.Fatal(err)
	}
	return g, p
}

// pngBytes is a minimal valid PNG so classify() sees a real image.
const pngBytes = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\x00\xa2\xf7Hd\x00\x00\x00\x00IEND\xaeB`\x82"

func minimalPack(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"manifest.json": `{"format_version": 2, "header": {"name":"t",
			"uuid": "66c6e9a8-3093-462a-9c36-dbb052165822", "version": [1,0,0],
			"min_engine_version": [1,21,0]},
			"modules": [{"type": "resources", "uuid": "743f6949-53be-44b6-b326-398005028819", "version": [1,0,0]}]}`,
	}
}

func (g *Graph) edgeAt(file, ptr string) *Edge {
	for _, e := range g.Edges {
		if e.File == file && e.Pointer == ptr {
			return e
		}
	}
	return nil
}

func TestAtlasToTextureChain(t *testing.T) {
	files := minimalPack(t)
	files["textures/terrain_texture.json"] = `{
		"resource_pack_name": "test",
		"texture_name": "atlas.terrain",
		"texture_data": {
			"shulker.custom": {"textures": "textures/custom/block"},
			"shulker.multi": {"textures": ["textures/custom/a", "textures/custom/b"]},
			"shulker.tinted": {"textures": [{"path": "textures/custom/c", "overlay_color": "#79c05a"}]},
			"shulker.obj": {"textures": {"path": "textures/custom/d", "tint_color": "#208030"}},
			"shulker.vanilla": {"textures": "textures/blocks/stone"}
		}
	}`
	for _, n := range []string{"block", "a", "b", "c", "d"} {
		files["textures/custom/"+n+".png"] = pngBytes
	}
	files["textures/blocks/stone.png"] = pngBytes

	g, _ := buildGraph(t, files)

	cases := []struct {
		ptr      string
		target   string
		prefixed bool
	}{
		{"/texture_data/shulker.custom/textures", "textures/custom/block.png", true},
		{"/texture_data/shulker.multi/textures/0", "textures/custom/a.png", true},
		{"/texture_data/shulker.multi/textures/1", "textures/custom/b.png", true},
		{"/texture_data/shulker.tinted/textures/0/path", "textures/custom/c.png", true},
		{"/texture_data/shulker.obj/textures/path", "textures/custom/d.png", true},
		{"/texture_data/shulker.vanilla/textures", "textures/blocks/stone.png", true},
	}
	for _, tc := range cases {
		e := g.edgeAt("textures/terrain_texture.json", tc.ptr)
		if e == nil {
			t.Fatalf("missing edge at %s", tc.ptr)
		}
		if e.Target != tc.target {
			t.Errorf("edge %s target = %q, want %q", tc.ptr, e.Target, tc.target)
		}
		if e.Rewriteable != true {
			t.Errorf("edge %s must be rewriteable", tc.ptr)
		}
		if e.Form.Prefixed != tc.prefixed {
			t.Errorf("edge %s form.Prefixed = %v", tc.ptr, e.Form.Prefixed)
		}
	}
	// Atlas provides its short names.
	if _, ok := g.shortNameIndex["shulker.custom"]; !ok {
		t.Error("atlas short name not indexed")
	}
	// Inbound index: texture should see its inbound edge.
	if len(g.Inbound("textures/custom/block.png")) != 1 {
		t.Errorf("inbound edges for block.png = %d, want 1", len(g.Inbound("textures/custom/block.png")))
	}
}

func TestAtlasRelativeUnprefixed(t *testing.T) {
	// Some community packs write atlas paths relative to textures/ without
	// the prefix. Resolution must handle both, and rewrites must preserve
	// the authorial form.
	files := minimalPack(t)
	files["textures/terrain_texture.json"] = `{"texture_data": {"x": {"textures": "custom/rel"}}}`
	files["textures/custom/rel.png"] = pngBytes
	g, _ := buildGraph(t, files)
	e := g.edgeAt("textures/terrain_texture.json", "/texture_data/x/textures")
	if e == nil || e.Target != "textures/custom/rel.png" {
		t.Fatalf("unprefixed atlas ref did not resolve: %+v", e)
	}
	if e.Form.Prefixed {
		t.Error("form must record the unprefixed style")
	}
}

func TestBlocksShortNameAndFlipbook(t *testing.T) {
	files := minimalPack(t)
	files["textures/terrain_texture.json"] = `{"texture_data": {"stone_like": {"textures": "textures/custom/stone"}}}`
	files["textures/flipbook_textures.json"] = `[
		{"flipbook_texture": "textures/custom/flip", "atlas_tile": "flip_strip",
		 "ticks_per_frame": 2, "frames": [0, 1]}]`
	files["blocks.json"] = `{"shulker:anim": {"sound": "stone", "textures": "flip_strip"},
		"shulker:fancy": {"sound": "stone", "textures": {"up": "stone_like", "side": "missing_name"}}}`
	files["textures/custom/stone.png"] = pngBytes
	files["textures/custom/flip.png"] = pngBytes

	g, _ := buildGraph(t, files)

	// Flipbook texture path edge.
	e := g.edgeAt("textures/flipbook_textures.json", "/0/flipbook_texture")
	if e == nil || e.Target != "textures/custom/flip.png" {
		t.Fatalf("flipbook edge wrong: %+v", e)
	}
	// blocks.json short-name edges resolve to the providing registry.
	e = g.edgeAt("blocks.json", "/shulker:fancy/textures/up")
	if e == nil || e.Kind != KindAtlasShortName {
		t.Fatalf("blocks shortname edge wrong: %+v", e)
	}
	if e.Target != "textures/terrain_texture.json" {
		t.Errorf("shortname target = %q, want terrain_texture.json", e.Target)
	}
	if e.Rewriteable {
		t.Error("shortname edges are validation-only in v0.1")
	}
	// Dangling short name must be reported.
	found := false
	for _, w := range g.Warnings {
		if strings.Contains(w.Msg, "missing_name") {
			found = true
		}
	}
	if !found {
		t.Errorf("dangling shortname not reported: %+v", g.Warnings)
	}
}

func TestClientEntityGraph(t *testing.T) {
	files := minimalPack(t)
	files["entity/mob.entity.json"] = `{
		"format_version": "1.10.0",
		"minecraft:client_entity": {"description": {
			"identifier": "shulker:mob",
			"materials": {"default": "entity_alphatest"},
			"textures": {"default": "textures/custom/mob", "sad": "textures/custom/mob_sad"},
			"geometry": {"default": "geometry.shulker_mob"},
			"animations": {"walk": "animation.shulker_mob.walk"},
			"particle_effects": {"burst": "shulker:burst"},
			"render_controllers": ["controller.render.shulker_mob",
				{"controller.render.biped": "query.is_saddled"}],
			"spawn_egg": {"texture": "shulker.item", "base_color": "#123456"},
			"scripts": {"pre_animation": ["v.index = (t.variant == 'temperate') ? 0 : 2;"]}
		}}
	}`
	files["textures/item_texture.json"] = `{"texture_data": {"shulker.item": {"textures": "textures/custom/item"}}}`
	files["textures/custom/mob.png"] = pngBytes
	files["textures/custom/mob_sad.png"] = pngBytes
	files["textures/custom/item.png"] = pngBytes
	files["models/entity/mob.geo.json"] = `{"format_version": "1.12.0",
		"minecraft:geometry": [{"description": {"identifier": "geometry.shulker_mob",
		"texture_width": 64, "texture_height": 64}, "bones": []}]}`
	files["animations/mob.animation.json"] = `{"format_version": "1.8.0",
		"animations": {"animation.shulker_mob.walk": {"loop": true, "animation_length": 1.5}}}`
	files["render_controllers/mob.render_controllers.json"] = `{"format_version": "1.8.0",
		"render_controllers": {"controller.render.shulker_mob": {
			"geometry": "Geometry.default", "materials": [{"*": "Material.default"}],
			"textures": ["Texture.sad"], "arrays": {"textures": {"Array.skins": ["Texture.default", "textures/custom/mob"]}}}}}`
	files["particles/burst.json"] = `{"format_version": "1.10.0",
		"particle_effect": {"description": {"identifier": "shulker:burst",
		"basic_render_parameters": {"material": "particles_alpha",
		"texture": "textures/custom/particles"}}, "components": {}}}`
	files["textures/custom/particles.png"] = pngBytes
	// attachable in the same schema
	files["attachables/hat.json"] = `{"format_version": "1.10.0",
		"minecraft:client_entity": {"description": {"identifier": "shulker:hat",
		"textures": {"default": "textures/custom/mob"}}}}`

	g, _ := buildGraph(t, files)

	// Texture map edges.
	for _, ptr := range []string{
		"/minecraft:client_entity/description/textures/default",
		"/minecraft:client_entity/description/textures/sad",
	} {
		e := g.edgeAt("entity/mob.entity.json", ptr)
		if e == nil || e.Target == "" || !e.Rewriteable {
			t.Fatalf("entity texture edge wrong at %s: %+v", ptr, e)
		}
	}
	// Geometry / animation / particle / render controller identifier edges.
	idCases := []struct {
		ptr    string
		kind   EdgeKind
		target string
	}{
		{"/minecraft:client_entity/description/geometry/default", KindGeometryID, "models/entity/mob.geo.json"},
		{"/minecraft:client_entity/description/animations/walk", KindAnimationID, "animations/mob.animation.json"},
		{"/minecraft:client_entity/description/particle_effects/burst", KindParticleID, "particles/burst.json"},
		{"/minecraft:client_entity/description/render_controllers/0", KindRenderControllerID, "render_controllers/mob.render_controllers.json"},
		// Object form {"controller.render.biped": "query.is_saddled"} — a
		// vanilla-only controller: identifier edge exists, target dangles
		// (resolves against vanilla at runtime), value is sacred molang.
		{"/minecraft:client_entity/description/render_controllers/1", KindRenderControllerID, ""},
		{"/minecraft:client_entity/description/spawn_egg/texture", KindAtlasShortName, "textures/item_texture.json"},
	}
	for _, tc := range idCases {
		e := g.edgeAt("entity/mob.entity.json", tc.ptr)
		if e == nil {
			t.Fatalf("missing edge at %s", tc.ptr)
		}
		if e.Kind != tc.kind {
			t.Errorf("%s kind = %v, want %v", tc.ptr, e.Kind, tc.kind)
		}
		if e.Target != tc.target {
			t.Errorf("%s target = %q, want %q", tc.ptr, e.Target, tc.target)
		}
	}

	// Render controller texture array: "Texture.default" must NOT produce an
	// edge (it is an entity map key); a literal path must.
	if e := g.edgeAt("render_controllers/mob.render_controllers.json",
		"/render_controllers/controller.render.shulker_mob/arrays/textures/Array.skins/0"); e != nil {
		t.Errorf("map-key entry produced an edge: %+v", e)
	}
	e := g.edgeAt("render_controllers/mob.render_controllers.json",
		"/render_controllers/controller.render.shulker_mob/arrays/textures/Array.skins/1")
	if e == nil || e.Target != "textures/custom/mob.png" {
		t.Fatalf("RC literal path edge wrong: %+v", e)
	}

	// Particle texture edge.
	if e := g.edgeAt("particles/burst.json",
		"/particle_effect/description/basic_render_parameters/texture"); e == nil || e.Target != "textures/custom/particles.png" {
		t.Fatalf("particle texture edge wrong: %+v", e)
	}

	// Attachable texture edge.
	if e := g.edgeAt("attachables/hat.json", "/minecraft:client_entity/description/textures/default"); e == nil || e.Target != "textures/custom/mob.png" {
		t.Fatalf("attachable edge wrong: %+v", e)
	}

	// The molang script must never produce an edge (it contains '(' and
	// query.* markers; a sweep would skip it as sacred).
	for _, e := range g.Edges {
		if e.File == "entity/mob.entity.json" && strings.Contains(e.Pointer, "/scripts") {
			t.Errorf("molang leaked into graph: %+v", e)
		}
	}
}

func TestLegacyAndModernGeometry(t *testing.T) {
	files := minimalPack(t)
	files["models/entity/legacy.geo.json"] = `{"format_version": "1.8.0",
		"geometry.legacy_mob": {"texturewidth": 64, "textureheight": 32, "bones": []}}`
	files["models/entity/modern.geo.json"] = `{"format_version": "1.12.0",
		"minecraft:geometry": [{"description": {"identifier": "geometry.modern_mob"}, "bones": []}]}`
	files["entity/a.entity.json"] = `{"minecraft:client_entity": {"description": {
		"identifier": "shulker:a",
		"geometry": {"legacy": "geometry.legacy_mob", "modern": "geometry.modern_mob"}}}}`
	g, _ := buildGraph(t, files)
	if got, ok := g.resolveIdentifier("geometry.legacy_mob", g.geometryIndex); !ok || got != "models/entity/legacy.geo.json" {
		t.Errorf("legacy geometry resolve = %q %v", got, ok)
	}
	if got, ok := g.resolveIdentifier("geometry.modern_mob", g.geometryIndex); !ok || got != "models/entity/modern.geo.json" {
		t.Errorf("modern geometry resolve = %q %v", got, ok)
	}
	e := g.edgeAt("entity/a.entity.json", "/minecraft:client_entity/description/geometry/legacy")
	if e == nil || e.Target != "models/entity/legacy.geo.json" {
		t.Fatalf("legacy geometry edge: %+v", e)
	}
}

func TestSoundGraph(t *testing.T) {
	files := minimalPack(t)
	files["sounds/sound_definitions.json"] = `{"format_version": "1.14.0",
		"sound_definitions": {
			"shulker.pop": {"category": "neutral", "sounds": ["sounds/custom/pop"]},
			"shulker.tuned": {"category": "neutral", "sounds": [{"name": "sounds/custom/pop", "pitch": 1.10}]}
		}}`
	files["sounds.json"] = `{
		"shulker.pop_event": {"category": "neutral", "sounds": [{"name": "shulker.pop", "volume": 1.0}]},
		"shulker.inline": {"sounds": ["sounds/custom/pop"]},
		"shulker.vanilla_event": {"sounds": ["mob.pig.say"]}}`
	files["sounds/custom/pop.ogg"] = "OggS"

	g, _ := buildGraph(t, files)

	e := g.edgeAt("sounds/sound_definitions.json", "/sound_definitions/shulker.pop/sounds/0")
	if e == nil || e.Target != "sounds/custom/pop.ogg" || !e.Rewriteable {
		t.Fatalf("sound def path edge: %+v", e)
	}
	if e.Form.Prefixed != true {
		t.Errorf("vanilla-style sound paths are prefixed; form = %+v", e.Form)
	}
	e = g.edgeAt("sounds/sound_definitions.json", "/sound_definitions/shulker.tuned/sounds/0/name")
	if e == nil || e.Target != "sounds/custom/pop.ogg" {
		t.Fatalf("sound def object-name edge: %+v", e)
	}
	// sounds.json event -> definition name (not the file).
	e = g.edgeAt("sounds.json", "/shulker.pop_event/sounds/0/name")
	if e == nil || e.Kind != KindSoundDefName || e.Target != "sounds/sound_definitions.json" {
		t.Fatalf("event def-name edge: %+v", e)
	}
	// sounds.json inline path (no matching definition name).
	e = g.edgeAt("sounds.json", "/shulker.inline/sounds/0")
	if e == nil || e.Kind != KindSoundPath || e.Target != "sounds/custom/pop.ogg" {
		t.Fatalf("inline sound path edge: %+v", e)
	}
	// Vanilla definition name: dangling warning, def-name kind.
	e = g.edgeAt("sounds.json", "/shulker.vanilla_event/sounds/0")
	if e == nil || e.Kind != KindSoundDefName || e.Target != "" {
		t.Fatalf("vanilla def edge: %+v", e)
	}
}

func TestUIRewriteableAndUnknownBlockers(t *testing.T) {
	files := minimalPack(t)
	files["ui/custom_screen.json"] = `{"namespace": "shulker_ui", "panel": {
		"controls": [{"type": "image", "texture": "textures/custom/ui_panel"},
			{"type": "label", "text": "hello"},
			{"type": "image", "$skinnable": "textures/custom/ui_panel"}]}}`
	// Unknown-schema JSON that references an asset: blocks renames.
	files["data/palette.json"] = `{"background": "textures/custom/ui_panel", "note": "misc"}`
	files["textures/custom/ui_panel.png"] = pngBytes

	g, _ := buildGraph(t, files)

	uiEdges := g.Outbound("ui/custom_screen.json")
	if len(uiEdges) != 2 {
		t.Fatalf("ui edges = %d, want 2 (texture + $var): %+v", len(uiEdges), uiEdges)
	}
	for _, e := range uiEdges {
		if !e.Rewriteable {
			t.Errorf("ui edge must be rewriteable: %+v", e)
		}
	}
	// The unknown file's reference is a blocker.
	blockers := 0
	for _, e := range g.Inbound("textures/custom/ui_panel.png") {
		if e.File == "data/palette.json" {
			blockers++
			if e.Rewriteable {
				t.Error("unknown-schema ref must block")
			}
		}
	}
	if blockers != 1 {
		t.Errorf("blocker edges from data/palette.json = %d, want 1", blockers)
	}
}

func TestTextureSetBlocks(t *testing.T) {
	files := minimalPack(t)
	files["textures/custom/block.png"] = pngBytes
	files["textures/custom/block_mers.tga"] = pngBytes
	files["textures/custom/block.texture_set.json"] = `{"format_version": "1.21.30",
		"minecraft:texture_set": {"color": "block", "metalness_emissive_roughness_subsurface": "block_mers"}}`
	g, _ := buildGraph(t, files)

	set := g.SetOf("textures/custom/block.png")
	if set == nil {
		t.Fatal("PBR set not built (png + tga companion)")
	}
	if set.Companions["_mers"] != "textures/custom/block_mers.tga" {
		t.Errorf("mers companion = %q", set.Companions["_mers"])
	}
	// The texture_set's own reference is a blocker on the color map AND
	// the companion.
	for _, target := range []string{"textures/custom/block.png", "textures/custom/block_mers.tga"} {
		blocked := false
		for _, e := range g.Inbound(target) {
			if !e.Rewriteable {
				blocked = true
			}
		}
		if !blocked {
			t.Errorf("expected a blocker edge on %s", target)
		}
	}
	// The texture_set file itself is never renamed.
	if !NeverRename("textures/custom/block.texture_set.json") {
		t.Error("texture_set files must never be renamed")
	}
}

func TestPBRLockstepSets(t *testing.T) {
	files := minimalPack(t)
	files["textures/terrain_texture.json"] = `{"texture_data": {"pbr": {"textures": "textures/custom/pbr"}}}`
	files["textures/custom/pbr.png"] = pngBytes
	files["textures/custom/pbr_mer.png"] = pngBytes
	files["textures/custom/pbr_normal.png"] = pngBytes
	files["textures/custom/pbr_heightmap.png"] = pngBytes
	// A lone texture that is not a set.
	files["textures/custom/lone.png"] = pngBytes
	g, _ := buildGraph(t, files)

	set := g.SetOf("textures/custom/pbr.png")
	if set == nil {
		t.Fatal("PBR set not built")
	}
	if len(set.Companions) != 3 {
		t.Errorf("companions = %d, want 3", len(set.Companions))
	}
	if g.SetOf("textures/custom/lone.png") != nil {
		t.Error("lone texture must not form a set")
	}
	// Membership is symmetric across the set.
	for _, m := range set.Members() {
		if g.SetOf(m) != set {
			t.Errorf("member %s does not map back to its set", m)
		}
	}
}

func TestCaseInsensitiveResolution(t *testing.T) {
	files := minimalPack(t)
	files["textures/terrain_texture.json"] = `{"texture_data": {"x": {"textures": "Textures/Custom/Foo"}}}`
	files["textures/custom/foo.png"] = pngBytes
	g, _ := buildGraph(t, files)
	e := g.edgeAt("textures/terrain_texture.json", "/texture_data/x/textures")
	if e == nil || e.Target != "textures/custom/foo.png" {
		t.Fatalf("case-insensitive resolve failed: %+v", e)
	}
}

func TestDanglingReported(t *testing.T) {
	files := minimalPack(t)
	files["entity/x.entity.json"] = `{"minecraft:client_entity": {"description": {
		"identifier": "shulker:x", "textures": {"default": "textures/missing/nope"}}}}`
	g, _ := buildGraph(t, files)
	found := false
	for _, w := range g.Warnings {
		if strings.Contains(w.Path, "textures/missing") || strings.Contains(w.Msg, "textures/missing/nope") {
			found = true
		}
	}
	if !found {
		t.Errorf("dangling texture not reported: %+v", g.Warnings)
	}
}

func TestPreservedSuffix(t *testing.T) {
	cases := map[string]string{
		"pig.geo.json":                   ".geo.json",
		"look_at_target.animation.json":  ".animation.json",
		"dog.animation_controllers.json": ".animation_controllers.json",
		"pig.render_controllers.json":    ".render_controllers.json",
		"mob.entity.json":                ".entity.json",
		"burst.json":                     ".json",
		"foo.png":                        ".png",
		"sound.ogg":                      ".ogg",
		"block_mers.tga":                 ".tga",
	}
	for name, want := range cases {
		if got := PreservedSuffix(name); got != want {
			t.Errorf("PreservedSuffix(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDenyList(t *testing.T) {
	for _, p := range []string{
		"textures/blocks/stone.png", "textures/environment/moon.png",
		"textures/particle/particles.png", "textures/colormap/birch.png",
		"textures/misc/missing_texture.png", "font/glyph_20.png",
		"texts/en_US.lang", "ui/hud_screen.json", "subpacks/low/x.png",
	} {
		if !IsDenyListed(p) {
			t.Errorf("%s should be deny-listed", p)
		}
	}
	for _, p := range []string{
		"textures/custom/block.png", "textures/entity/mob.png",
		"sounds/custom/pop.ogg", "entity/mob.entity.json",
		"models/entity/pig.geo.json",
	} {
		if IsDenyListed(p) {
			t.Errorf("%s should NOT be deny-listed", p)
		}
	}
	// Registries are never renamed even though they are not deny-prefixed.
	for _, p := range []string{"blocks.json", "sounds.json",
		"textures/terrain_texture.json", "textures/flipbook_textures.json",
		"sounds/sound_definitions.json", "textures_list.json"} {
		if !NeverRename(p) {
			t.Errorf("%s must never be renamed", p)
		}
	}
	if NeverRename("textures/custom/block.png") {
		t.Error("custom texture must be renamable")
	}
}

func TestTexturesListUpdated(t *testing.T) {
	files := minimalPack(t)
	files["textures_list.json"] = `["textures/custom/keep.png", "textures/missing.png"]`
	files["textures/custom/keep.png"] = pngBytes
	g, _ := buildGraph(t, files)
	e := g.edgeAt("textures_list.json", "/0")
	if e == nil || e.Target != "textures/custom/keep.png" || !e.Rewriteable {
		t.Fatalf("textures_list edge: %+v", e)
	}
	// Missing entries are reported but do not stop the world.
	e = g.edgeAt("textures_list.json", "/1")
	if e == nil || e.Target != "" {
		t.Fatalf("missing textures_list entry should be dangling: %+v", e)
	}
}

func TestRenderPathForm(t *testing.T) {
	// Rewrite keeps the authorial style: prefixed/unprefixed, ext/no-ext.
	cases := []struct {
		form   PathForm
		target string
		want   string
	}{
		{PathForm{Prefixed: true, WithExt: false}, "textures/_0.png", "textures/_0"},
		{PathForm{Prefixed: true, WithExt: true}, "textures/_0.png", "textures/_0.png"},
		{PathForm{Prefixed: false, WithExt: false}, "textures/_0.png", "_0"},
		{PathForm{Prefixed: false, WithExt: true}, "textures/_0.png", "_0.png"},
		{PathForm{Prefixed: true, WithExt: false}, "sounds/_0.ogg", "sounds/_0"},
		{PathForm{Prefixed: false, WithExt: false}, "sounds/_0.ogg", "_0"},
	}
	for _, tc := range cases {
		root := "textures"
		if strings.Contains(tc.target, "sounds/") {
			root = "sounds"
		}
		if got := RenderPathForm(tc.form, root, tc.target); got != tc.want {
			t.Errorf("RenderPathForm(%+v, %s) = %q, want %q", tc.form, tc.target, got, tc.want)
		}
	}
}
