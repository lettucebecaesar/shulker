package graph

import (
	"github.com/lettucebecaesar/shulker/internal/jsonx"
)

// edgeSeen returns true when a pointer already has an edge (dedup between
// schema extraction and the conservative sweep).
type edgeSeen func(file, ptr string) bool

// textureEdge resolves a texture reference and records the edge. Known-schema
// positions record dangling references too (they are reportable); the sweep
// does not.
func (g *Graph) textureEdge(file, ptr, raw string, rewriteable bool, node *jsonx.String, seen edgeSeen, recordDangling bool) {
	if seen != nil && seen(file, ptr) {
		return
	}
	target, form, ok := g.resolveTexture(raw)
	if !ok && !recordDangling {
		return
	}
	g.addEdge(file, ptr, KindTexturePath, raw, target, form, rewriteable, node)
}

// soundEdge is textureEdge's twin for sound references.
func (g *Graph) soundEdge(file, ptr, raw string, rewriteable bool, node *jsonx.String, seen edgeSeen, recordDangling bool) {
	if seen != nil && seen(file, ptr) {
		return
	}
	target, form, ok := g.resolveSound(raw)
	if !ok && !recordDangling {
		return
	}
	g.addEdge(file, ptr, KindSoundPath, raw, target, form, rewriteable, node)
}

// extractAtlasTextures walks terrain_texture.json / item_texture.json.
//
// Verified vanilla shape (bedrock-samples textures/terrain_texture.json):
//
//	"texture_data": {
//	  "missing":   {"textures": "textures/misc/missing_texture"},
//	  "grass_top": {"textures": ["textures/blocks/grass_top", ...]},
//	  "grass_carried": {"textures": {"path": "textures/blocks/grass_side",
//	                                 "overlay_color": "#79c05a"}},
//	  "grass_side": {"textures": [{"path": "...", "overlay_color": "#df6827"}]}
//	}
//
// All four value shapes appear in vanilla; only the "path"-bearing ones carry
// extra keys (tint colors) which must never be touched.
func (g *Graph) extractAtlasTextures(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	dataV, ok := obj.Get("texture_data")
	if !ok {
		return
	}
	data, ok := jsonx.AsObject(dataV)
	if !ok {
		return
	}
	for i := range data.Members {
		short := &data.Members[i]
		entry, ok := jsonx.AsObject(short.Val)
		if !ok {
			continue
		}
		texV, ok := entry.Get("textures")
		if !ok {
			continue
		}
		ptr := "/texture_data/" + esc(short.Key) + "/textures"
		g.walkTextureValue(file, ptr, texV, seen)
	}
}

// walkTextureValue handles the four atlas value shapes.
func (g *Graph) walkTextureValue(file, ptr string, v jsonx.Value, seen edgeSeen) {
	switch t := v.(type) {
	case *jsonx.String:
		g.textureEdge(file, ptr, t.Str, true, t, seen, true)
	case *jsonx.Array:
		for i, item := range t.Items {
			g.walkTextureValue(file, ptr+"/"+itoa(i), item, seen)
		}
	case *jsonx.Object:
		if p, ok := t.Get("path"); ok {
			if s, isStr := jsonx.AsStringNode(p); isStr {
				g.textureEdge(file, ptr+"/path", s.Str, true, s, seen, true)
			}
		}
	}
}

// extractFlipbook walks textures/flipbook_textures.json.
//
// Verified vanilla shape:
//
//	[{"flipbook_texture": "textures/blocks/fire_0", "atlas_tile": "fire_0",
//	  "atlas_index": 0, "atlas_tile_variant": 0, "ticks_per_frame": 2,
//	  "frames": [16, 17, ...], "replicate": 2, "blend_frames": false}]
//
// flipbook_texture is the path reference; atlas_tile registers a short name
// that blocks.json may reference.
func (g *Graph) extractFlipbook(file string, root jsonx.Value, seen edgeSeen) {
	arr, ok := jsonx.AsArray(root)
	if !ok {
		return
	}
	for i, item := range arr.Items {
		obj, ok := jsonx.AsObject(item)
		if !ok {
			continue
		}
		if v, ok := obj.Get("flipbook_texture"); ok {
			if s, isStr := jsonx.AsStringNode(v); isStr {
				g.textureEdge(file, "/"+itoa(i)+"/flipbook_texture", s.Str, true, s, seen, true)
			}
		}
	}
}

// extractTextureSet walks *.texture_set.json (Vibrant Visuals PBR).
//
// Verified vanilla shape (textures/blocks/acacia_shelf.texture_set.json):
//
//	{"format_version": "1.21.30",
//	 "minecraft:texture_set": {
//	   "color": "acacia_shelf",
//	   "metalness_emissive_roughness_subsurface": "acacia_shelf_mers"}}
//
// Values are SIBLING names — bare file names resolved relative to the
// texture_set file's own directory (they can also be hardcoded numeric
// values, which we ignore). Because the pairing between iron_block.png and
// iron_block.texture_set.json is name+directory based, v0.1 treats these as
// BLOCKERS: a texture referenced by a texture_set file is never renamed.
func (g *Graph) extractTextureSet(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	setV, ok := obj.Get("minecraft:texture_set")
	if !ok {
		return
	}
	set, ok := jsonx.AsObject(setV)
	if !ok {
		return
	}
	dir := parentDir(file)
	for i := range set.Members {
		m := &set.Members[i]
		s, isStr := jsonx.AsStringNode(m.Val)
		if !isStr {
			continue // hardcoded PBR values (numbers/objects)
		}
		ptr := "/minecraft:texture_set/" + esc(m.Key)
		if seen != nil && seen(file, ptr) {
			continue
		}
		if target, ok := g.resolveSibling(dir, s.Str); ok {
			g.addEdge(file, ptr, KindTexturePath, s.Str, target, PathForm{}, false, s)
		} else {
			g.addEdge(file, ptr, KindTexturePath, s.Str, "", PathForm{}, false, s)
		}
	}
}

// extractBlocksShortNames walks blocks.json. Values are terrain atlas SHORT
// NAMES (never file paths, never rewritten in v0.1 — short names are
// sacred), so these edges exist for validation only.
//
// Verified vanilla shape: {"acacia_leaves": {"carried_textures":
// "acacia_leaves_carried", "sound": "grass", "textures": "acacia_leaves",
// "isotropic": {"down": true, "up": true}, "ambient_occlusion_exponent":
// 0.80}} — with "textures" as either a string or an object of facet names.
func (g *Graph) extractBlocksShortNames(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	for bi := range obj.Members {
		block := &obj.Members[bi]
		entry, ok := jsonx.AsObject(block.Val)
		if !ok {
			continue
		}
		for _, field := range []string{"textures", "carried_textures"} {
			v, ok := entry.Get(field)
			if !ok {
				continue
			}
			g.walkShortNameValue(file, "/"+esc(block.Key)+"/"+field, v, seen)
		}
	}
}

func (g *Graph) walkShortNameValue(file, ptr string, v jsonx.Value, seen edgeSeen) {
	switch t := v.(type) {
	case *jsonx.String:
		if seen != nil && seen(file, ptr) {
			return
		}
		target, _ := g.resolveShortName(t.Str)
		g.addEdge(file, ptr, KindAtlasShortName, t.Str, target, PathForm{}, false, t)
	case *jsonx.Object:
		for i := range t.Members {
			m := &t.Members[i]
			if s, isStr := jsonx.AsStringNode(m.Val); isStr {
				g.walkShortNameValue(file, ptr+"/"+esc(m.Key), s, seen)
			}
		}
	}
}

// extractSoundDefinitionPaths walks sounds/sound_definitions.json.
//
// Verified vanilla shape: paths carry the "sounds/" prefix and no extension;
// entries are plain strings or objects with a "name" member:
//
//	"ambient.cave": {"category": "ambient",
//	                 "sounds": ["sounds/cave/cave1", ...],
//	                  "subtitle": "subtitles.ambient.sound"}
//	"armor.equip_wolf": {"sounds": [{"name": "sounds/armor/equip_wolf1",
//	                                  "pitch": 1.10}]}
func (g *Graph) extractSoundDefinitionPaths(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	defsV, ok := obj.Get("sound_definitions")
	if !ok {
		return
	}
	defs, ok := jsonx.AsObject(defsV)
	if !ok {
		return
	}
	for i := range defs.Members {
		def := &defs.Members[i]
		entry, ok := jsonx.AsObject(def.Val)
		if !ok {
			continue
		}
		soundsV, ok := entry.Get("sounds")
		if !ok {
			continue
		}
		sounds, ok := jsonx.AsArray(soundsV)
		if !ok {
			continue
		}
		base := "/sound_definitions/" + esc(def.Key) + "/sounds"
		for si, item := range sounds.Items {
			switch t := item.(type) {
			case *jsonx.String:
				g.soundEdge(file, base+"/"+itoa(si), t.Str, true, t, seen, true)
			case *jsonx.Object:
				if nv, ok := t.Get("name"); ok {
					if s, isStr := jsonx.AsStringNode(nv); isStr {
						g.soundEdge(file, base+"/"+itoa(si)+"/name", s.Str, true, s, seen, true)
					}
				}
			}
		}
	}
}

// extractSoundEvents walks root sounds.json. Entries reference definition
// NAMES from sound_definitions.json (the engine's indirection layer);
// older packs inline file paths instead. Definition names win when both
// interpretations are possible — that matches engine lookup order.
func (g *Graph) extractSoundEvents(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	for ei := range obj.Members {
		event := &obj.Members[ei]
		entry, ok := jsonx.AsObject(event.Val)
		if !ok {
			continue
		}
		soundsV, ok := entry.Get("sounds")
		if !ok {
			continue
		}
		sounds, ok := jsonx.AsArray(soundsV)
		if !ok {
			continue
		}
		base := "/" + esc(event.Key) + "/sounds"
		for si, item := range sounds.Items {
			ptr := base + "/" + itoa(si)
			if seen != nil && seen(file, ptr) {
				continue
			}
			switch t := item.(type) {
			case *jsonx.String:
				g.soundEventRef(file, ptr, t.Str, t)
			case *jsonx.Object:
				if nv, ok := t.Get("name"); ok {
					if s, isStr := jsonx.AsStringNode(nv); isStr {
						nptr := ptr + "/name"
						if seen != nil && seen(file, nptr) {
							continue
						}
						g.soundEventRef(file, nptr, s.Str, s)
					}
				}
			}
		}
	}
}

func (g *Graph) soundEventRef(file, ptr, raw string, node *jsonx.String) {
	if target, ok := g.resolveIdentifier(raw, g.soundDefIndex); ok {
		g.addEdge(file, ptr, KindSoundDefName, raw, target, PathForm{}, false, node)
		return
	}
	// Not a known definition: some packs inline file paths here.
	if target, form, ok := g.resolveSound(raw); ok {
		g.addEdge(file, ptr, KindSoundPath, raw, target, form, true, node)
		return
	}
	// Neither: likely a vanilla definition name. Record for validation.
	g.addEdge(file, ptr, KindSoundDefName, raw, "", PathForm{}, false, node)
}

// extractTexturesList walks the marketplace textures_list.json inventory.
// Entries are full paths, usually WITH extensions. The list must be updated
// on rename or the pack stops validating against its own inventory.
func (g *Graph) extractTexturesList(file string, root jsonx.Value, seen edgeSeen) {
	arr, ok := jsonx.AsArray(root)
	if !ok {
		return
	}
	for i, item := range arr.Items {
		s, isStr := jsonx.AsStringNode(item)
		if !isStr {
			continue
		}
		g.textureEdge(file, "/"+itoa(i), s.Str, true, s, seen, true)
	}
}
