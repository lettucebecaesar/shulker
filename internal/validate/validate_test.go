package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/jsonx"
	"github.com/shulker-tools/shulker/internal/pack"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", "packs", name)
}

func loadPack(t *testing.T, dir string) (*pack.Pack, *graph.Graph) {
	t.Helper()
	p, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	docs, parseErrs := graph.ParseAll(p)
	g, err := graph.Build(p, docs, parseErrs)
	if err != nil {
		t.Fatalf("graph %s: %v", dir, err)
	}
	return p, g
}

func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func hasErrorWith(r *Result, substr string) bool {
	for _, f := range r.Errors() {
		if strings.Contains(f.Msg, substr) || strings.Contains(f.Path, substr) {
			return true
		}
	}
	return false
}

func hasWarningWith(r *Result, substr string) bool {
	for _, f := range r.Findings {
		if f.Severity == pack.SeverityWarning &&
			(strings.Contains(f.Msg, substr) || strings.Contains(f.Path, substr)) {
			return true
		}
	}
	return false
}

// The rich fixture is the golden pack: pre-transform validation must be
// error-free on it. Warnings are expected (dangling refs that resolve
// against vanilla, the subpack note).
func TestRichFixtureChecksClean(t *testing.T) {
	p, g := loadPack(t, fixturePath("rich"))
	r := Check(p, g)
	for _, f := range r.Errors() {
		t.Errorf("unexpected error: %s", f.String())
	}
	// Expected warnings are part of the fixture's contract.
	for _, want := range []string{
		"subpacks",                // untouchable subtree note
		"mob.pig.say",             // vanilla sound definition ref
		"controller.render.biped", // vanilla render controller ref
	} {
		if !hasWarningWith(r, want) {
			t.Errorf("expected warning mentioning %q, findings: %+v", want, r.Findings)
		}
	}
	// Vanilla-side texture references (unresolved UI strings) produce no
	// edge and therefore no warning — flagging every vanilla path a pack
	// references would be noise, not signal.
	for _, f := range r.Findings {
		if strings.Contains(f.Msg, "vanilla_thing") {
			t.Errorf("unresolved vanilla texture should not be reported: %s", f.String())
		}
	}
	// The PBR sets must be recognized.
	if len(g.Sets()) != 3 {
		t.Errorf("PBR sets = %d, want 3 (block, pbr, texset_ref)", len(g.Sets()))
	}
}

// The broken fixture seeds every defect class the validator must flag.
func TestBrokenFixtureFlagsDefects(t *testing.T) {
	p, g := loadPack(t, fixturePath("broken"))
	r := Check(p, g)
	if !r.HasErrors() {
		t.Fatal("broken fixture must produce errors")
	}
	for _, want := range []string{
		"textures/broken.json",    // unparseable JSON: loud, with path
		"textures/missing/nope",   // dangling texture ref, with pointer
		"geometry.does_not_exist", // dangling geometry id
		"xx_XX",                   // languages.json without .lang file
	} {
		found := false
		for _, f := range r.Findings {
			if strings.Contains(f.Path, want) || strings.Contains(f.Msg, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a finding mentioning %q, got %+v", want, r.Findings)
		}
	}
}

func TestPostTransformIdentity(t *testing.T) {
	p, g := loadPack(t, fixturePath("rich"))
	r := PostTransform(p, g, p, g, &TransformLog{})
	for _, f := range r.Errors() {
		t.Errorf("identity transform must be clean: %s", f.String())
	}
}

// manualMiniSquash renames a texture and rewrites every inbound reference
// on disk, producing a legitimately transformed pack. skipFile simulates a
// transform bug that misses one rewrite.
func manualMiniSquash(t *testing.T, skipFile string) (*pack.Pack, *graph.Graph, *pack.Pack, *graph.Graph, []graph.Rename, []graph.Rewrite) {
	t.Helper()
	in, inG := loadPack(t, fixturePath("rich"))
	dir := copyDir(t, fixturePath("rich"))

	const from = "textures/custom/mob.png"
	const to = "textures/_0.png"
	if err := os.Rename(filepath.Join(dir, filepath.FromSlash(from)), filepath.Join(dir, filepath.ToSlash(to))); err != nil {
		t.Fatal(err)
	}

	var renames = []graph.Rename{{From: from, To: to}}
	var rewrites []graph.Rewrite
	for _, e := range inG.Inbound(from) {
		if e.File == skipFile {
			continue // simulate a transform bug: miss this rewrite
		}
		newRaw := graph.RenderPathForm(e.Form, "textures", to)
		target := filepath.Join(dir, filepath.FromSlash(e.File))
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonx.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		node, ok := jsonx.GetByPointer(doc, e.Pointer)
		if !ok {
			t.Fatalf("pointer %s missing in %s", e.Pointer, e.File)
		}
		s, ok := jsonx.AsStringNode(node)
		if !ok {
			t.Fatalf("pointer %s in %s is not a string", e.Pointer, e.File)
		}
		if s.Str != e.Raw {
			t.Fatalf("pointer %s content %q != edge raw %q", e.Pointer, s.Str, e.Raw)
		}
		s.Set(newRaw)
		if err := os.WriteFile(target, jsonx.Encode(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		rewrites = append(rewrites, graph.Rewrite{File: e.File, Pointer: e.Pointer, From: e.Raw, To: newRaw})
	}
	out, outG := loadPack(t, dir)
	return in, inG, out, outG, renames, rewrites
}

func TestPostTransformAcceptsCorrectRename(t *testing.T) {
	in, inG, out, outG, renames, rewrites := manualMiniSquash(t, "")
	r := PostTransform(in, inG, out, outG, &TransformLog{Renames: renames, Rewrites: rewrites})
	for _, f := range r.Errors() {
		t.Errorf("correct rename rejected: %s", f.String())
	}
	if len(renames) != 1 || len(rewrites) != 5 {
		t.Errorf("expected 5 rewrites (entity, attachable, RC, terrain, textures_list), got %d", len(rewrites))
	}
}

func TestPostTransformCatchesMissingRewrite(t *testing.T) {
	in, inG, out, outG, renames, rewrites := manualMiniSquash(t, "textures/terrain_texture.json")
	r := PostTransform(in, inG, out, outG, &TransformLog{Renames: renames, Rewrites: rewrites})
	if !r.HasErrors() {
		t.Fatal("missing rewrite must be caught")
	}
	if !hasErrorWith(r, "no longer resolves") && !hasErrorWith(r, "vanished") {
		t.Errorf("expected resolution failure, got: %+v", r.Errors())
	}
}

func TestPostTransformCatchesUnloggedChange(t *testing.T) {
	p, g := loadPack(t, fixturePath("rich"))

	// Same pack, but someone edited a string without logging it.
	dir := copyDir(t, fixturePath("rich"))
	target := filepath.Join(dir, filepath.FromSlash("entity/shulker_mob.entity.json"))
	data, _ := os.ReadFile(target)
	doc, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	node, ok := jsonx.GetByPointer(doc, "/minecraft:client_entity/description/textures/default")
	if !ok {
		t.Fatalf("pointer missing in entity fixture")
	}
	node.(*jsonx.String).Set("textures/custom/mob_sad")
	if err := os.WriteFile(target, jsonx.Encode(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	out, outG := loadPack(t, dir)
	r := PostTransform(p, g, out, outG, &TransformLog{})
	if !hasErrorWith(r, "without a logged rewrite") {
		t.Errorf("unlogged string change must be caught, got: %+v", r.Errors())
	}
	if !hasErrorWith(r, "string changed") {
		t.Errorf("structural diff must flag it too, got: %+v", r.Errors())
	}
}

func TestPostTransformCatchesManifestEdit(t *testing.T) {
	p, g := loadPack(t, fixturePath("rich"))
	dir := copyDir(t, fixturePath("rich"))
	// A still-valid manifest whose bytes differ: the pack loads, but the
	// manifest must be stored verbatim and any byte change is a defect.
	data, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	tampered := strings.Replace(string(data), "Shulker Rich Fixture", "Tampered", 1)
	if tampered == string(data) {
		t.Fatal("test setup: fixture name not found to tamper with")
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	out, outG := loadPack(t, dir)
	r := PostTransform(p, g, out, outG, &TransformLog{})
	if !hasErrorWith(r, "manifest.json changed") {
		t.Errorf("manifest mutation must be caught, got: %+v", r.Errors())
	}
}

func TestPostTransformCatchesDroppedFile(t *testing.T) {
	p, g := loadPack(t, fixturePath("rich"))
	dir := copyDir(t, fixturePath("rich"))
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash("sounds/custom/pop.ogg"))); err != nil {
		t.Fatal(err)
	}
	out, outG := loadPack(t, dir)
	r := PostTransform(p, g, out, outG, &TransformLog{})
	if !hasErrorWith(r, "missing this file") {
		t.Errorf("dropped file must be caught, got: %+v", r.Errors())
	}
}

func TestPostTransformCatchesPBRLockstepViolation(t *testing.T) {
	in, inG := loadPack(t, fixturePath("rich"))
	dir := copyDir(t, fixturePath("rich"))

	// Rename ONLY the color map of the block PBR set.
	const from = "textures/custom/block.png"
	const to = "textures/_7.png"
	if err := os.Rename(filepath.Join(dir, filepath.FromSlash(from)), filepath.Join(dir, filepath.ToSlash(to))); err != nil {
		t.Fatal(err)
	}

	out, outG := loadPack(t, dir)
	r := PostTransform(in, inG, out, outG, &TransformLog{
		Renames: []graph.Rename{{From: from, To: to}},
	})
	if !hasErrorWith(r, "left behind") && !hasErrorWith(r, "lockstep") {
		t.Errorf("PBR lockstep violation must be caught, got: %+v", r.Errors())
	}
}

func TestPostTransformCatchesDenyListReencode(t *testing.T) {
	in, inG := loadPack(t, fixturePath("rich"))
	r := PostTransform(in, inG, in, inG, &TransformLog{
		PNGOptimized: map[string]bool{"textures/blocks/stone.png": true},
	})
	if !hasErrorWith(r, "vanilla-override texture was re-encoded") {
		t.Errorf("deny-listed re-encode must be caught, got: %+v", r.Errors())
	}
}

func TestMinimalPackChecksClean(t *testing.T) {
	p, g := loadPack(t, fixturePath("minimal"))
	r := Check(p, g)
	for _, f := range r.Errors() {
		t.Errorf("unexpected error: %s", f.String())
	}
	if len(p.Files) != 4 {
		t.Errorf("minimal pack file count = %d", len(p.Files))
	}
}
