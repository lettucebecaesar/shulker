// Package graph builds the bidirectional reference graph over a resource
// pack: file ↔ all referencing entries, with file + JSON pointer context on
// every edge. This module powers every later transform — the API is designed
// so "rename X" is impossible to do without moving every edge.
//
// Two classes of edges matter for renaming:
//
//   - Rewriteable path edges from known schema positions (registry files,
//     client entities, particles, render controllers, json-ui, texture
//     lists). The rename transform rewrites exactly these.
//   - Blocker edges: strings in unknown-schema JSON files (or unmodeled
//     positions of known files) that resolve to real pack assets. We do not
//     know how to rewrite them safely, so their targets are locked. This is
//     invariant 3 implemented as a data structure: a file may be renamed
//     only if 100% of its inbound references live inside this pack and every
//     one of them gets rewritten.
package graph

import (
	"fmt"
	"sort"

	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/pack"
)

// EdgeKind says what a reference string means. Kinds that target files
// (TexturePath, SoundPath) drive renaming; identifier/short-name kinds are
// validation-only in v0.1 because identifiers and short names are never
// renamed.
type EdgeKind uint8

const (
	KindTexturePath EdgeKind = iota
	KindSoundPath
	KindAtlasShortName
	KindGeometryID
	KindAnimationID
	KindAnimControllerID
	KindRenderControllerID
	KindParticleID
	KindSoundDefName
	KindMaterialName
)

func (k EdgeKind) String() string {
	switch k {
	case KindTexturePath:
		return "texture-path"
	case KindSoundPath:
		return "sound-path"
	case KindAtlasShortName:
		return "atlas-short-name"
	case KindGeometryID:
		return "geometry"
	case KindAnimationID:
		return "animation"
	case KindAnimControllerID:
		return "animation-controller"
	case KindRenderControllerID:
		return "render-controller"
	case KindParticleID:
		return "particle"
	case KindSoundDefName:
		return "sound-definition"
	case KindMaterialName:
		return "material"
	}
	return "unknown"
}

// PathForm records how a reference string was written so a rewrite can
// reproduce the same authorial style:
//
//   - prefixed:  "textures/custom/foo" or "sounds/mob/pig/say1" (vanilla style
//     for sound_definitions; every atlas and entity texture uses this)
//   - unprefixed: "custom/foo" — atlas-relative (textures/) or sounds-relative
//   - withExt:   the raw string carried its own extension
type PathForm struct {
	Prefixed bool
	WithExt  bool
}

// Edge is one reference from a JSON string node to (ideally) a pack file.
type Edge struct {
	File        string // pack-relative path of the file containing the reference
	Pointer     string // JSON pointer to the string node (RFC 6901)
	Kind        EdgeKind
	Raw         string // original string value, exactly as authored
	Target      string // resolved pack file path, "" when unresolved
	Form        PathForm
	Rewriteable bool // the rename transform knows how to rewrite this position

	node *jsonx.String // the mutable node this edge points at
}

// Node returns the mutable string node (used by the transform apply step).
func (e *Edge) Node() *jsonx.String { return e.node }

// String renders an edge for reports and error messages.
func (e *Edge) String() string {
	status := e.Target
	if status == "" {
		status = "unresolved"
	}
	rw := "rewriteable"
	if !e.Rewriteable {
		rw = "blocking"
	}
	return fmt.Sprintf("%s%s: %s %q -> %s (%s)", e.File, e.Pointer, e.Kind, e.Raw, status, rw)
}

// Provides lists what a file offers to the rest of the pack.
type Provides struct {
	AtlasShortNames     []string
	GeometryIDs         []string
	AnimationIDs        []string
	AnimControllerIDs   []string
	RenderControllerIDs []string
	ParticleIDs         []string
	SoundDefNames       []string
	MaterialNames       []string
}

// Graph is the assembled reference graph. All iteration is deterministic:
// edges are stored in sorted (file, pointer) order.
type Graph struct {
	Pack  *pack.Pack
	Docs  map[string]jsonx.Value // parsed JSON per path (nil for non-JSON)
	Edges []*Edge

	byTarget    map[string][]*Edge   // resolved target file -> inbound edges
	bySource    map[string][]*Edge   // source file -> outbound edges
	provides    map[string]*Provides // file -> what it provides
	byFile      map[string]*FileFacts
	setByMember map[string]*TextureSet // every member path -> its PBR set
	sets        []*TextureSet          // canonical (sorted by color path)

	// Provider indexes: identifier/short-name -> providing files (sorted).
	geometryIndex         map[string][]string
	animationIndex        map[string][]string
	animControllerIndex   map[string][]string
	renderControllerIndex map[string][]string
	particleIndex         map[string][]string
	soundDefIndex         map[string][]string
	materialIndex         map[string][]string
	shortNameIndex        map[string][]string

	Warnings []pack.Finding
}

// FileFacts caches per-file decisions for the planners.
type FileFacts struct {
	Kind     FileKind
	Provides *Provides
}

// Inbound returns every edge targeting the given pack file.
func (g *Graph) Inbound(target string) []*Edge { return g.byTarget[target] }

// Outbound returns every edge originating in the given pack file.
func (g *Graph) Outbound(file string) []*Edge { return g.bySource[file] }

// FileProvides returns what a file provides (nil when nothing).
func (g *Graph) FileProvides(file string) *Provides { return g.provides[file] }

// SetOf returns the PBR texture set a file belongs to (nil when it is a
// lone texture).
func (g *Graph) SetOf(path string) *TextureSet { return g.setByMember[path] }

// Sets returns every PBR set, sorted by color-map path.
func (g *Graph) Sets() []*TextureSet { return g.sets }

// Build assembles the graph over a parsed pack. docs maps each JSON file's
// parsed root; parseErrs maps files that failed to parse (reported as
// error-severity findings — callers gate on them before transforming).
func Build(p *pack.Pack, docs map[string]jsonx.Value, parseErrs map[string]error) (*Graph, error) {
	g := &Graph{
		Pack:                  p,
		Docs:                  docs,
		byTarget:              make(map[string][]*Edge),
		bySource:              make(map[string][]*Edge),
		provides:              make(map[string]*Provides),
		byFile:                make(map[string]*FileFacts),
		setByMember:           make(map[string]*TextureSet),
		geometryIndex:         make(map[string][]string),
		animationIndex:        make(map[string][]string),
		animControllerIndex:   make(map[string][]string),
		renderControllerIndex: make(map[string][]string),
		particleIndex:         make(map[string][]string),
		soundDefIndex:         make(map[string][]string),
		materialIndex:         make(map[string][]string),
		shortNameIndex:        make(map[string][]string),
	}

	// Pass 0: surface parse failures loudly (never silently best-effort).
	var paths []string
	for path := range parseErrs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		g.Warnings = append(g.Warnings, pack.Finding{
			Severity: pack.SeverityError,
			Path:     path,
			Msg:      "unparseable JSON: " + parseErrs[path].Error(),
		})
	}

	// Pass 1: classify files and collect providers (identifiers, short
	// names). Providers must exist before reference resolution.
	for _, f := range p.Files {
		kind := KindOf(f.Path)
		g.byFile[f.Path] = &FileFacts{Kind: kind}
		switch kind {
		case KindGeometryFile:
			g.collectGeometry(f.Path)
		case KindAnimationFile:
			g.collectAnimationIDs(f.Path)
		case KindAnimControllerFile:
			g.collectAnimControllerIDs(f.Path)
		case KindRenderControllerFile:
			g.collectRenderControllerIDs(f.Path)
		case KindParticleFile:
			g.collectParticleIDs(f.Path)
		case KindSoundDefinitions:
			g.collectSoundDefNames(f.Path)
		case KindTerrainAtlas, KindItemAtlas, KindFlipbook:
			g.collectAtlasShortNames(f.Path, kind)
		case KindMaterials:
			g.collectMaterialNames(f.Path)
		}
	}

	// Pass 2: extract reference edges, known-schema positions first.
	seen := map[string]map[string]bool{} // file -> pointer -> already edged
	edgeAt := func(file, ptr string) bool {
		if seen[file] == nil {
			seen[file] = map[string]bool{}
		}
		if seen[file][ptr] {
			return true
		}
		seen[file][ptr] = true
		return false
	}
	for _, f := range p.Files {
		kind := g.byFile[f.Path].Kind
		root, ok := docs[f.Path]
		if !ok {
			continue // not JSON / not parsed (errors already surfaced)
		}
		switch kind {
		case KindTerrainAtlas, KindItemAtlas:
			g.extractAtlasTextures(f.Path, root, edgeAt)
		case KindFlipbook:
			g.extractFlipbook(f.Path, root, edgeAt)
		case KindTextureSetFile:
			g.extractTextureSet(f.Path, root, edgeAt)
		case KindBlocksFile:
			g.extractBlocksShortNames(f.Path, root, edgeAt)
		case KindSoundDefinitions:
			g.extractSoundDefinitionPaths(f.Path, root, edgeAt)
		case KindSoundsEvents:
			g.extractSoundEvents(f.Path, root, edgeAt)
		case KindEntityFile, KindAttachableFile:
			g.extractClientEntity(f.Path, root, edgeAt)
		case KindRenderControllerFile:
			g.extractRenderControllerTextures(f.Path, root, edgeAt)
		case KindParticleFile:
			g.extractParticle(f.Path, root, edgeAt)
		case KindAnimationFile:
			g.extractAnimationRefs(f.Path, root, edgeAt)
		case KindTexturesList:
			g.extractTexturesList(f.Path, root, edgeAt)
		}
	}

	// Pass 3: the conservative sweep. Every string in every JSON file that
	// resolves to a real pack texture or sound file and is not already an
	// extracted edge becomes a BLOCKER for that target. Known files get
	// swept too: a position the schema extraction does not model is exactly
	// as suspicious as an unknown file. Molang expressions are sacred and
	// never swept.
	for _, f := range p.Files {
		root, ok := docs[f.Path]
		if !ok {
			continue
		}
		switch {
		case f.Path == "manifest.json" || f.Class == pack.ClassSubpack:
			// The manifest is stored verbatim; subpacks are untouchable.
			// Neither participates in the reference graph.
			continue
		}
		g.sweep(f.Path, root, edgeAt)
	}

	// Deterministic edge order: (file, pointer, raw).
	sort.Slice(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Pointer != b.Pointer {
			return a.Pointer < b.Pointer
		}
		return a.Raw < b.Raw
	})
	for _, e := range g.Edges {
		if e.Target != "" {
			g.byTarget[e.Target] = append(g.byTarget[e.Target], e)
		}
		g.bySource[e.File] = append(g.bySource[e.File], e)
	}

	// Pass 4: PBR texture sets — suffix-convention companions travel in
	// lockstep with their color map, or renders silently break.
	g.buildSets()

	// Pass 5: report dangling references with exact locations. Unresolved
	// does not mean broken — identifiers and paths may legitimately resolve
	// against the vanilla packs — so these are warnings, not errors.
	for _, e := range g.Edges {
		if e.Target == "" {
			g.Warnings = append(g.Warnings, pack.Finding{
				Severity: pack.SeverityWarning,
				Path:     e.File + e.Pointer,
				Msg:      fmt.Sprintf("%s %q does not resolve inside this pack (may resolve against vanilla)", e.Kind, e.Raw),
			})
		}
	}
	return g, nil
}

// addEdge records an edge if its node is not already claimed at this pointer.
func (g *Graph) addEdge(file, ptr string, kind EdgeKind, raw, target string, form PathForm, rewriteable bool, node *jsonx.String) {
	g.Edges = append(g.Edges, &Edge{
		File: file, Pointer: ptr, Kind: kind, Raw: raw, Target: target,
		Form: form, Rewriteable: rewriteable, node: node,
	})
}
