package pack

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validManifest mirrors vanilla's manifest shape (format_version 2,
// header with uuid/version/min_engine_version, one resources module).
const validManifest = `{
	"format_version": 2,
	"header": {
		"name": "Test Pack",
		"description": "fixture",
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
}`

// tinyPNG is a real 1x1 PNG so the pack is genuinely loadable by the engine.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	// 1x1 RGBA PNG, hand-assembled and valid.
	return []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
		0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
		0x0d, 0x00, 0xa2, 0xf7, 0x48, 0x64,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
}

func writePackDir(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writePackZip(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "test.mcpack")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func samplePackFiles(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		"manifest.json":                 []byte(validManifest),
		"pack_icon.png":                 tinyPNG(t),
		"textures/terrain_texture.json": []byte(`{"texture_data":{"a":{"textures":"textures/b"}}}`),
		"textures/b.png":                tinyPNG(t),
		"textures/c.tga":                []byte("raw tga bytes"),
		"sounds/random/click.ogg":       []byte("OggS-fake-bytes"),
		"texts/en_US.lang":              []byte("item.test.name=Test\n"),
		"texts/languages.json":          []byte(`["en_US"]`),
		"entity/mob.entity.json":        []byte(`{"format_version":"1.10.0","minecraft:client_entity":{"description":{"identifier":"test:mob"}}}`),
		"models/entity/mob.geo.json":    []byte(`{"format_version":"1.12.0","minecraft:geometry":[{"description":{"identifier":"geometry.mob","texture_width":64,"texture_height":64,"bones":[]}}]}`),
		"subpacks/low/manifest.json":    []byte(validManifest),
		"ui/custom.json":                []byte(`{"namespace":"test"}`),
		"README.txt":                    []byte("hello"),
	}
}

func TestLoadDir(t *testing.T) {
	p, err := LoadDir(writePackDir(t, samplePackFiles(t)))
	if err != nil {
		t.Fatal(err)
	}
	if p.RootPrefix != "" {
		t.Errorf("RootPrefix = %q, want empty", p.RootPrefix)
	}
	// Deterministic ordering.
	paths := p.Paths()
	for i := 1; i < len(paths); i++ {
		if paths[i-1] >= paths[i] {
			t.Errorf("paths not strictly sorted: %q then %q", paths[i-1], paths[i])
		}
	}
	want := []string{
		"README.txt", "entity/mob.entity.json", "manifest.json",
		"models/entity/mob.geo.json", "pack_icon.png",
		"sounds/random/click.ogg", "subpacks/low/manifest.json",
		"texts/en_US.lang", "texts/languages.json",
		"textures/b.png", "textures/c.tga", "textures/terrain_texture.json",
		"ui/custom.json",
	}
	if len(paths) != len(want) {
		t.Fatalf("file count = %d, want %d (%v)", len(paths), len(want), paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
	// Classes.
	for path, class := range map[string]Class{
		"manifest.json":                 ClassManifest,
		"pack_icon.png":                 ClassIcon,
		"textures/b.png":                ClassTexture,
		"textures/terrain_texture.json": ClassJSONFixed,
		"textures/c.tga":                ClassTexture,
		"sounds/random/click.ogg":       ClassSound,
		"texts/en_US.lang":              ClassLang,
		"subpacks/low/manifest.json":    ClassSubpack,
		"entity/mob.entity.json":        ClassJSON,
		"README.txt":                    ClassText,
	} {
		f, ok := p.File(path)
		if !ok {
			t.Fatalf("missing file %s", path)
		}
		if f.Class != class {
			t.Errorf("class(%s) = %v, want %v", path, f.Class, class)
		}
	}
	// Subpack warning present.
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w.Msg, "subpacks") && w.Severity == SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Errorf("expected subpacks warning, got %+v", p.Warnings)
	}
}

func TestZipRoundTrip(t *testing.T) {
	files := samplePackFiles(t)
	zipPath := writePackZip(t, files)
	p, err := Load(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// Content preserved exactly.
	for name, want := range files {
		f, ok := p.File(name)
		if !ok {
			t.Fatalf("zip pack missing %s", name)
		}
		if !bytes.Equal(f.Data, want) {
			t.Errorf("%s content changed through zip round trip", name)
		}
	}
}

func TestRepackDeterministic(t *testing.T) {
	files := samplePackFiles(t)
	p, err := Load(writePackDir(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := p.WriteZip(&a); err != nil {
		t.Fatal(err)
	}
	if err := p.WriteZip(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("WriteZip is not byte-deterministic")
	}

	// Repack idempotency: repack(repack(x)) == repack(x).
	p2, err := Load(writePackZip(t, withZipBytes(t, a.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	var c bytes.Buffer
	if err := p2.WriteZip(&c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), c.Bytes()) {
		t.Error("canonical repack is not idempotent")
	}
}

func withZipBytes(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := readAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		rc.Close()
		out[zf.Name] = data
	}
	return out
}

func readAll(rc interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := rc.Read(tmp)
		buf.Write(tmp[:n])
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}

func TestNestedSingleDirZip(t *testing.T) {
	files := map[string][]byte{}
	for name, data := range samplePackFiles(t) {
		files["MyPack/"+name] = data
	}
	p, err := Load(writePackZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if p.RootPrefix != "MyPack/" {
		t.Errorf("RootPrefix = %q, want MyPack/", p.RootPrefix)
	}
	if _, ok := p.File("manifest.json"); !ok {
		t.Error("nested manifest not mapped to pack root")
	}
	if _, ok := p.File("textures/b.png"); !ok {
		t.Error("nested texture not mapped to pack root")
	}
}

func TestNestedSingleDirOnDisk(t *testing.T) {
	root := t.TempDir()
	inner := writePackDir(t, samplePackFiles(t))
	if err := os.Rename(inner, filepath.Join(root, "MyPack")); err != nil {
		t.Fatal(err)
	}
	p, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.RootPrefix != "MyPack/" {
		t.Errorf("RootPrefix = %q, want MyPack/", p.RootPrefix)
	}
}

func TestRejectsWorldTemplate(t *testing.T) {
	files := samplePackFiles(t)
	files["level.dat"] = []byte("world data")
	files["levelname.txt"] = []byte("My World")
	_, err := Load(writePackZip(t, files))
	var nrp *NotResourcePackError
	if err == nil || !asNotResourcePack(err, &nrp) {
		t.Fatalf("expected NotResourcePackError, got %v", err)
	}
	if !strings.Contains(nrp.Reason, "world") {
		t.Errorf("reason = %q", nrp.Reason)
	}
}

func asNotResourcePack(err error, target **NotResourcePackError) bool {
	nrp, ok := err.(*NotResourcePackError)
	if ok {
		*target = nrp
	}
	return ok
}

func TestRejectsMCTemplateExtension(t *testing.T) {
	path := writePackZip(t, samplePackFiles(t))
	renamed := strings.TrimSuffix(path, ".mcpack") + ".mctemplate"
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(renamed); err == nil {
		t.Fatal("expected rejection of .mctemplate")
	} else if !strings.Contains(err.Error(), "world/addon container") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectsAddonBundle(t *testing.T) {
	files := map[string][]byte{
		"PackA/manifest.json": []byte(validManifest),
		"PackB/manifest.json": []byte(validManifest),
		"PackA/extra.txt":     []byte("x"),
		"PackB/extra.txt":     []byte("x"),
	}
	_, err := Load(writePackZip(t, files))
	if err == nil {
		t.Fatal("expected addon-bundle rejection")
	}
	if !strings.Contains(err.Error(), "bundle") {
		t.Errorf("error = %v", err)
	}
}

func TestRejectsMissingManifest(t *testing.T) {
	files := samplePackFiles(t)
	delete(files, "manifest.json")
	_, err := Load(writePackZip(t, files))
	if err == nil {
		t.Fatal("expected missing-manifest rejection")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error = %v", err)
	}
}

func TestRejectsBadPaths(t *testing.T) {
	files := samplePackFiles(t)
	files["../evil.txt"] = []byte("nope")
	if _, err := Load(writePackZip(t, files)); err == nil {
		t.Fatal("expected bad-path rejection")
	}
}

func TestRejectsDuplicateEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"manifest.json", "manifest.json"} {
		w, _ := zw.Create(name)
		w.Write([]byte(validManifest))
	}
	zw.Close()
	path := filepath.Join(t.TempDir(), "dup.mcpack")
	os.WriteFile(path, buf.Bytes(), 0o644)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	p, err := Load(writePackDir(t, samplePackFiles(t)))
	if err != nil {
		t.Fatal(err)
	}
	f, actual, ok := p.Lookup("TEXTURES/B.PNG")
	if !ok {
		t.Fatal("case-insensitive lookup failed")
	}
	if actual != "textures/b.png" {
		t.Errorf("actual path = %q", actual)
	}
	if f.Path != "textures/b.png" {
		t.Errorf("returned file path = %q", f.Path)
	}
}

func TestMethodSelection(t *testing.T) {
	p, err := Load(writePackDir(t, samplePackFiles(t)))
	if err != nil {
		t.Fatal(err)
	}
	for path, wantMethod := range map[string]uint16{
		"manifest.json":           zip.Deflate,
		"textures/b.png":          zip.Store,
		"textures/c.tga":          zip.Deflate, // TGA is the exception
		"sounds/random/click.ogg": zip.Store,
		"texts/en_US.lang":        zip.Deflate,
		"entity/mob.entity.json":  zip.Deflate,
	} {
		f, ok := p.File(path)
		if !ok {
			t.Fatalf("missing %s", path)
		}
		if got := methodFor(f); got != wantMethod {
			t.Errorf("method(%s) = %d, want %d", path, got, wantMethod)
		}
	}
}

func TestManifestVerbatimRoundTrip(t *testing.T) {
	// The manifest must survive repack byte-identically — never re-serialized.
	p, err := Load(writePackDir(t, samplePackFiles(t)))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := p.File("manifest.json")
	if !bytes.Equal(f.Data, []byte(validManifest)) {
		t.Error("manifest bytes changed")
	}
}

func TestWriteDir(t *testing.T) {
	p, err := Load(writePackDir(t, samplePackFiles(t)))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := p.WriteDir(dir); err != nil {
		t.Fatal(err)
	}
	p2, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Files) != len(p.Files) {
		t.Fatalf("file count %d != %d", len(p2.Files), len(p.Files))
	}
	for i := range p.Files {
		a, b := p.Files[i], p2.Files[i]
		if a.Path != b.Path || !bytes.Equal(a.Data, b.Data) {
			t.Errorf("file %q changed through dir round trip", a.Path)
		}
	}
}
