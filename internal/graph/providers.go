package graph

import (
	"strings"

	"github.com/lettucebecaesar/shulker/internal/jsonx"
)

// Providers pass: index identifiers and short names BEFORE edges resolve
// against them. Every shape here was verified against vanilla files from
// Mojang/bedrock-samples (see file comments).

func (g *Graph) provide(file string, kind EdgeKind, id string, index map[string][]string) {
	if id == "" {
		return
	}
	index[id] = append(index[id], file)
	p := g.providesFor(file)
	switch kind {
	case KindGeometryID:
		p.GeometryIDs = append(p.GeometryIDs, id)
	case KindAnimationID:
		p.AnimationIDs = append(p.AnimationIDs, id)
	case KindAnimControllerID:
		p.AnimControllerIDs = append(p.AnimControllerIDs, id)
	case KindRenderControllerID:
		p.RenderControllerIDs = append(p.RenderControllerIDs, id)
	case KindParticleID:
		p.ParticleIDs = append(p.ParticleIDs, id)
	case KindSoundDefName:
		p.SoundDefNames = append(p.SoundDefNames, id)
	case KindMaterialName:
		p.MaterialNames = append(p.MaterialNames, id)
	case KindAtlasShortName:
		p.AtlasShortNames = append(p.AtlasShortNames, id)
	}
}

func (g *Graph) providesFor(file string) *Provides {
	if p, ok := g.provides[file]; ok {
		return p
	}
	p := &Provides{}
	g.provides[file] = p
	return p
}

// collectGeometry handles BOTH geometry formats, because both still ship in
// the wild:
//
//   - Legacy (current bedrock-samples models/entity/pig.geo.json): the
//     top-level key IS the identifier — {"geometry.pig.v1.8": {...bones...}}
//     (value may be an object or an array of objects).
//   - Modern 1.12+: {"minecraft:geometry": [{"description":
//     {"identifier": "geometry.husk", ...}, "bones": [...]}]}
func (g *Graph) collectGeometry(file string) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	obj, isObj := jsonx.AsObject(root)
	if !isObj {
		return
	}
	if geomsV, has := obj.Get("minecraft:geometry"); has {
		if geoms, isArr := jsonx.AsArray(geomsV); isArr {
			for _, item := range geoms.Items {
				go_, isObj := jsonx.AsObject(item)
				if !isObj {
					continue
				}
				if descV, has := go_.Get("description"); has {
					if desc, isObj := jsonx.AsObject(descV); isObj {
						if idV, has := desc.Get("identifier"); has {
							if id, isStr := jsonx.AsString(idV); isStr {
								g.provide(file, KindGeometryID, id, g.geometryIndex)
							}
						}
					}
				}
			}
		}
		return
	}
	// Legacy: every top-level key starting with "geometry." is one
	// identifier (skip "format_version" and friends).
	for i := range obj.Members {
		key := obj.Members[i].Key
		if strings.HasPrefix(key, "geometry.") {
			g.provide(file, KindGeometryID, key, g.geometryIndex)
		}
	}
}

func (g *Graph) collectAnimationIDs(file string) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	obj, isObj := jsonx.AsObject(root)
	if !isObj {
		return
	}
	if animsV, has := obj.Get("animations"); has {
		if anims, isObj := jsonx.AsObject(animsV); isObj {
			for i := range anims.Members {
				g.provide(file, KindAnimationID, anims.Members[i].Key, g.animationIndex)
			}
		}
	}
}

func (g *Graph) collectAnimControllerIDs(file string) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	obj, isObj := jsonx.AsObject(root)
	if !isObj {
		return
	}
	if acsV, has := obj.Get("animation_controllers"); has {
		if acs, isObj := jsonx.AsObject(acsV); isObj {
			for i := range acs.Members {
				g.provide(file, KindAnimControllerID, acs.Members[i].Key, g.animControllerIndex)
			}
		}
	}
}

func (g *Graph) collectRenderControllerIDs(file string) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	obj, isObj := jsonx.AsObject(root)
	if !isObj {
		return
	}
	if rcsV, has := obj.Get("render_controllers"); has {
		if rcs, isObj := jsonx.AsObject(rcsV); isObj {
			for i := range rcs.Members {
				g.provide(file, KindRenderControllerID, rcs.Members[i].Key, g.renderControllerIndex)
			}
		}
	}
}

func (g *Graph) collectParticleIDs(file string) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	obj, isObj := jsonx.AsObject(root)
	if !isObj {
		return
	}
	peV, has := obj.Get("particle_effect")
	if !has {
		return
	}
	pe, isObj := jsonx.AsObject(peV)
	if !isObj {
		return
	}
	descV, has := pe.Get("description")
	if !has {
		return
	}
	desc, isObj := jsonx.AsObject(descV)
	if !isObj {
		return
	}
	if idV, has := desc.Get("identifier"); has {
		if id, isStr := jsonx.AsString(idV); isStr {
			g.provide(file, KindParticleID, id, g.particleIndex)
		}
	}
}

func (g *Graph) collectSoundDefNames(file string) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	obj, isObj := jsonx.AsObject(root)
	if !isObj {
		return
	}
	if defsV, has := obj.Get("sound_definitions"); has {
		if defs, isObj := jsonx.AsObject(defsV); isObj {
			for i := range defs.Members {
				g.provide(file, KindSoundDefName, defs.Members[i].Key, g.soundDefIndex)
			}
		}
	}
}

// collectAtlasShortNames indexes terrain/item atlas short names and flipbook
// atlas_tile registrations (blocks.json can reference flipbook tiles).
func (g *Graph) collectAtlasShortNames(file string, kind FileKind) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	switch kind {
	case KindTerrainAtlas, KindItemAtlas:
		obj, isObj := jsonx.AsObject(root)
		if !isObj {
			return
		}
		if dataV, has := obj.Get("texture_data"); has {
			if data, isObj := jsonx.AsObject(dataV); isObj {
				for i := range data.Members {
					g.provide(file, KindAtlasShortName, data.Members[i].Key, g.shortNameIndex)
				}
			}
		}
	case KindFlipbook:
		arr, isArr := jsonx.AsArray(root)
		if !isArr {
			return
		}
		for _, item := range arr.Items {
			obj, isObj := jsonx.AsObject(item)
			if !isObj {
				continue
			}
			if tileV, has := obj.Get("atlas_tile"); has {
				if tile, isStr := jsonx.AsString(tileV); isStr {
					g.provide(file, KindAtlasShortName, tile, g.shortNameIndex)
				}
			}
		}
	}
}

// collectMaterialNames parses materials/materials.json defensively — the
// shape has drifted across versions, and a missed material only downgrades
// a validation edge to a "may resolve against vanilla" warning.
func (g *Graph) collectMaterialNames(file string) {
	root, ok := g.Docs[file]
	if !ok {
		return
	}
	obj, isObj := jsonx.AsObject(root)
	if !isObj {
		return
	}
	matsV, has := obj.Get("materials")
	if !has {
		return
	}
	mats, isObj := jsonx.AsObject(matsV)
	if !isObj {
		return
	}
	if listV, has := mats.Get("materials"); has {
		if list, isArr := jsonx.AsArray(listV); isArr {
			for _, item := range list.Items {
				m, isObj := jsonx.AsObject(item)
				if !isObj {
					continue
				}
				if nameV, has := m.Get("name"); has {
					if name, isStr := jsonx.AsString(nameV); isStr {
						g.provide(file, KindMaterialName, name, g.materialIndex)
					}
				}
			}
		}
	}
}
