package graph

import (
	"strings"

	"github.com/lettucebecaesar/shulker/internal/jsonx"
)

// extractClientEntity walks entity/*.entity.json and attachables/*.json.
//
// Verified vanilla shape (entity/pig.entity.json):
//
//	{"format_version": "1.10.0",
//	 "minecraft:client_entity": {"description": {
//	   "identifier": "minecraft:pig",
//	   "materials":   {"default": "pig_v3"},
//	   "textures":    {"default": "textures/entity/pig/pig_v3", ...},
//	   "geometry":    {"default": "geometry.pig.v3"},
//	   "animations":  {"walk": "animation.quadruped.walk", ...},
//	   "scripts":     {...molang, SACRED...},
//	   "render_controllers": ["controller.render.saddle", ...],
//	   "spawn_egg":   {"texture": "spawn_egg_pig"}}}}
//
// The identifier field is never touched (coupled to behavior packs we cannot
// see). Texture map values are the path references we rewrite. Molang in
// "scripts" never resolves as a path, so it is naturally protected.
func (g *Graph) extractClientEntity(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	var desc *jsonx.Object
	descPrefix := ""
	if ceV, ok := obj.Get("minecraft:client_entity"); ok {
		if ce, isObj := jsonx.AsObject(ceV); isObj {
			if dV, ok := ce.Get("description"); ok {
				desc, _ = jsonx.AsObject(dV)
				descPrefix = "/minecraft:client_entity/description"
			}
		}
	}
	if desc == nil {
		// Tolerate files that put "description" at the root (some attachable
		// generators do).
		if dV, ok := obj.Get("description"); ok {
			desc, _ = jsonx.AsObject(dV)
			descPrefix = "/description"
		}
	}
	if desc == nil {
		return
	}

	for i := range desc.Members {
		m := &desc.Members[i]
		ptr := descPrefix + "/" + esc(m.Key)
		switch m.Key {
		case "textures":
			if o, ok := jsonx.AsObject(m.Val); ok {
				for j := range o.Members {
					tex := &o.Members[j]
					if s, isStr := jsonx.AsStringNode(tex.Val); isStr {
						g.textureEdge(file, ptr+"/"+esc(tex.Key), s.Str, true, s, seen, true)
					}
				}
			}
		case "geometry":
			g.mapOfIDs(file, ptr, m.Val, KindGeometryID, g.geometryIndex, seen)
		case "animations":
			g.mapOfIDs(file, ptr, m.Val, KindAnimationID, g.animationIndex, seen)
		case "particle_effects":
			g.mapOfIDs(file, ptr, m.Val, KindParticleID, g.particleIndex, seen)
		case "materials":
			g.mapOfIDs(file, ptr, m.Val, KindMaterialName, g.materialIndex, seen)
		case "render_controllers":
			if arr, ok := jsonx.AsArray(m.Val); ok {
				for ai, item := range arr.Items {
					aptr := ptr + "/" + itoa(ai)
					if seen != nil && seen(file, aptr) {
						continue
					}
					switch t := item.(type) {
					case *jsonx.String:
						target, _ := g.resolveIdentifier(t.Str, g.renderControllerIndex)
						g.addEdge(file, aptr, KindRenderControllerID, t.Str, target, PathForm{}, false, t)
					case *jsonx.Object:
						// {"controller.render.biped": "query.molang"} — the
						// KEY is the identifier; the value is molang and
						// therefore sacred (and never renamed anyway).
						for _, mm := range t.Members {
							target, _ := g.resolveIdentifier(mm.Key, g.renderControllerIndex)
							g.addEdge(file, aptr, KindRenderControllerID, mm.Key, target, PathForm{}, false, nil)
						}
					}
				}
			}
		case "spawn_egg":
			if o, ok := jsonx.AsObject(m.Val); ok {
				if v, ok := o.Get("texture"); ok {
					if s, isStr := jsonx.AsStringNode(v); isStr {
						tptr := ptr + "/texture"
						if seen == nil || !seen(file, tptr) {
							target, _ := g.resolveShortName(s.Str)
							g.addEdge(file, tptr, KindAtlasShortName, s.Str, target, PathForm{}, false, s)
						}
					}
				}
			}
		}
	}
}

// mapOfIDs records identifier-valued map entries (geometry/animations/
// particle_effects/materials).
func (g *Graph) mapOfIDs(file, ptr string, v jsonx.Value, kind EdgeKind, index map[string][]string, seen edgeSeen) {
	o, ok := jsonx.AsObject(v)
	if !ok {
		return
	}
	for i := range o.Members {
		m := &o.Members[i]
		mptr := ptr + "/" + esc(m.Key)
		if seen != nil && seen(file, mptr) {
			continue
		}
		s, isStr := jsonx.AsStringNode(m.Val)
		if !isStr {
			continue
		}
		// Molang-valued entries (conditional geometry etc.) are sacred;
		// identifier strings are not expressions.
		target, _ := g.resolveIdentifier(s.Str, index)
		g.addEdge(file, mptr, kind, s.Str, target, PathForm{}, false, s)
	}
}

// extractRenderControllerTextures walks render_controllers/*.json texture
// arrays. Values are EITHER entity texture-map keys ("Texture.default" —
// rendered by molang) or literal texture paths. Map keys never resolve as
// files, so only genuine paths produce edges; we must not emit dangling
// warnings for "Texture.default" style entries.
//
// Verified vanilla shape (pig.render_controllers.json):
//
//	{"render_controllers": {"controller.render.pig": {
//	   "arrays": {"textures": {"Array.skins": ["Texture.default", "Texture.saddled"]}},
//	   "geometry": "Geometry.default",
//	   "materials": [{"*": "Material.default"}],
//	   "textures": ["Array.skins[query.is_saddled]"]}}}
func (g *Graph) extractRenderControllerTextures(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	rcsV, ok := obj.Get("render_controllers")
	if !ok {
		return
	}
	rcs, ok := jsonx.AsObject(rcsV)
	if !ok {
		return
	}
	for ri := range rcs.Members {
		rc := &rcs.Members[ri]
		body, ok := jsonx.AsObject(rc.Val)
		if !ok {
			continue
		}
		arraysV, ok := body.Get("arrays")
		if !ok {
			continue
		}
		arrays, ok := jsonx.AsObject(arraysV)
		if !ok {
			continue
		}
		texturesV, ok := arrays.Get("textures")
		if !ok {
			continue
		}
		textures, ok := jsonx.AsObject(texturesV)
		if !ok {
			continue
		}
		for ai := range textures.Members {
			arr := &textures.Members[ai]
			items, ok := jsonx.AsArray(arr.Val)
			if !ok {
				continue
			}
			base := "/render_controllers/" + esc(rc.Key) + "/arrays/textures/" + esc(arr.Key)
			for ii, item := range items.Items {
				s, isStr := jsonx.AsStringNode(item)
				if !isStr {
					continue
				}
				g.textureEdge(file, base+"/"+itoa(ii), s.Str, true, s, seen, false)
			}
		}
	}
}

// extractParticle walks particles/*.json.
//
// Verified vanilla shape (particles/campfire_smoke.json):
//
//	{"particle_effect": {"description": {
//	   "identifier": "minecraft:campfire_smoke_particle",
//	   "basic_render_parameters": {"material": "particles_alpha",
//	                               "texture": "textures/particle/campfire_smoke"}}}}
func (g *Graph) extractParticle(file string, root jsonx.Value, seen edgeSeen) {
	obj, ok := jsonx.AsObject(root)
	if !ok {
		return
	}
	peV, ok := obj.Get("particle_effect")
	if !ok {
		return
	}
	pe, ok := jsonx.AsObject(peV)
	if !ok {
		return
	}
	descV, ok := pe.Get("description")
	if !ok {
		return
	}
	desc, ok := jsonx.AsObject(descV)
	if !ok {
		return
	}
	brpV, ok := desc.Get("basic_render_parameters")
	if !ok {
		return
	}
	brp, ok := jsonx.AsObject(brpV)
	if !ok {
		return
	}
	for i := range brp.Members {
		m := &brp.Members[i]
		s, isStr := jsonx.AsStringNode(m.Val)
		if !isStr {
			continue
		}
		ptr := "/particle_effect/description/basic_render_parameters/" + esc(m.Key)
		switch m.Key {
		case "texture":
			g.textureEdge(file, ptr, s.Str, true, s, seen, true)
		case "material":
			if seen != nil && seen(file, ptr) {
				continue
			}
			target, _ := g.resolveIdentifier(s.Str, g.materialIndex)
			g.addEdge(file, ptr, KindMaterialName, s.Str, target, PathForm{}, false, s)
		}
	}
}

// extractAnimationRefs walks animations/*.json for keyframed sound and
// particle references:
//
//	"timeline": {"0.0": {"sound_effects": [{"effect": "random.pop"}],
//	                      "particle_effects": [{"effect": "minecraft:burst"}]}}
//
// These are identifiers/definition names — validation-only in v0.1.
func (g *Graph) extractAnimationRefs(file string, root jsonx.Value, seen edgeSeen) {
	// The walk callback never returns an error (it only records edges).
	_ = jsonx.Walk(root, func(ptr string, v jsonx.Value) error {
		o, ok := jsonx.AsObject(v)
		if !ok {
			return nil
		}
		for _, key := range []string{"sound_effects", "particle_effects"} {
			effV, has := o.Get(key)
			if !has {
				continue
			}
			g.collectEffectRefs(file, ptr+"/"+esc(key), effV, seen)
		}
		return nil
	})
}

func (g *Graph) collectEffectRefs(file, ptr string, v jsonx.Value, seen edgeSeen) {
	switch t := v.(type) {
	case *jsonx.Array:
		for i, item := range t.Items {
			g.collectEffectRefs(file, ptr+"/"+itoa(i), item, seen)
		}
	case *jsonx.Object:
		if effV, ok := t.Get("effect"); ok {
			if s, isStr := jsonx.AsStringNode(effV); isStr {
				eptr := ptr + "/effect"
				if seen != nil && seen(file, eptr) {
					return
				}
				kind := KindSoundDefName
				index := g.soundDefIndex
				if strings.Contains(ptr, "particle_effects") {
					kind = KindParticleID
					index = g.particleIndex
				}
				target, _ := g.resolveIdentifier(s.Str, index)
				g.addEdge(file, eptr, kind, s.Str, target, PathForm{}, false, s)
			}
		}
		for i := range t.Members {
			if t.Members[i].Key == "effect" {
				continue
			}
			// Time-keyed objects: {"0.0": {...}}
			if _, isStr := jsonx.AsStringNode(t.Members[i].Val); isStr {
				continue
			}
			g.collectEffectRefs(file, ptr+"/"+esc(t.Members[i].Key), t.Members[i].Val, seen)
		}
	}
}
