package pipeline

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lettucebecaesar/shulker/internal/transform"
)

// The subpack member of the rich fixture is a complete mini-pack: its own
// atlas referencing its own texture. --squash-subpacks must give it the
// full treatment — minify, rename, rewrite, validate — while the DEFAULT
// keeps every subpack byte identical (asserted by the other suite).
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func TestSquashSubpacksOptIn(t *testing.T) {
	rich := filepath.Join("..", "..", "testdata", "packs", "rich")

	// Baseline: subpack untouched by default.
	base, err := Squash(rich, Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	inSub, ok := base.In.File("subpacks/low_res/textures/terrain_texture.json")
	if !ok {
		t.Fatal("fixture lost its subpack")
	}
	outSub, ok := base.Out.File("subpacks/low_res/textures/terrain_texture.json")
	if !ok || !bytes.Equal(inSub.Data, outSub.Data) {
		t.Fatal("default squash must leave subpack JSON byte-identical")
	}

	// Opt-in: the subpack is squashed as its own pack.
	res, err := Squash(rich, Options{
		Transforms:     transform.DefaultOptions(),
		SquashSubpacks: true,
		Generator:      "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := res.Out.File("subpacks/low_res/textures/terrain_texture.json")
	if !ok {
		t.Fatal("squashed subpack terrain missing")
	}
	if strings.Contains(string(sub.Data), "\n") {
		t.Error("subpack JSON was not minified")
	}
	// The subpack's texture renamed, its atlas reference rewritten —
	// all visible through the prefixed parent log.
	var atlasRewritten bool
	for _, rw := range res.Transform.Rewrites {
		if rw.File == "subpacks/low_res/textures/terrain_texture.json" &&
			rw.From == "textures/custom/item" {
			atlasRewritten = true
			if !strings.HasPrefix(rw.To, "textures/_") {
				t.Errorf("unexpected subpack rewrite target %q", rw.To)
			}
		}
	}
	if !atlasRewritten {
		t.Error("subpack atlas reference was not rewritten")
	}
	var textureRenamed bool
	for _, rn := range res.Transform.Renames {
		if rn.From == "subpacks/low_res/textures/custom/item.png" {
			textureRenamed = true
			if !strings.HasPrefix(rn.To, "subpacks/low_res/textures/_") {
				t.Errorf("unexpected subpack rename target %q", rn.To)
			}
		}
	}
	if !textureRenamed {
		t.Error("subpack texture was not renamed")
	}
	// The subpack manifest stays verbatim even under the opt-in.
	subManifest, ok := res.Out.File("subpacks/low_res/manifest.json")
	if !ok {
		t.Fatal("subpack manifest missing")
	}
	inManifest, _ := res.In.File("subpacks/low_res/manifest.json")
	if !bytes.Equal(subManifest.Data, inManifest.Data) {
		t.Error("subpack manifest was re-serialized")
	}
	// PostTransform accepted the whole thing (it ran inside Squash).
	if res.Post.HasErrors() {
		for _, f := range res.Post.Errors() {
			t.Errorf("post-transform defect: %s", f.String())
		}
	}

	// Determinism with the flag on.
	res2, err := Squash(rich, Options{
		Transforms:     transform.DefaultOptions(),
		SquashSubpacks: true,
		Generator:      "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	var zip1, zip2 bytes.Buffer
	if err := res.Out.WriteZip(&zip1); err != nil {
		t.Fatal(err)
	}
	if err := res2.Out.WriteZip(&zip2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zip1.Bytes(), zip2.Bytes()) {
		t.Error("subpack squash is not byte-deterministic")
	}
}

// A subpack whose own validation would fail must fail the parent squash
// with the subpack named — never a silent bad subpack.
func TestSquashSubpacksPropagatesFailure(t *testing.T) {
	dir := t.TempDir()
	rich := filepath.Join("..", "..", "testdata", "packs", "rich")
	if err := copyTreeTo(rich, dir); err != nil {
		t.Fatal(err)
	}
	// Corrupt the subpack's JSON.
	bad := filepath.Join(dir, "subpacks", "low_res", "textures", "terrain_texture.json")
	if err := writeFile(bad, []byte("{ not json")); err != nil {
		t.Fatal(err)
	}
	_, err := Squash(dir, Options{
		Transforms:     transform.DefaultOptions(),
		SquashSubpacks: true,
		Generator:      "shulker/test",
	})
	if err == nil {
		t.Fatal("corrupt subpack must fail the squash")
	}
	if !strings.Contains(err.Error(), "subpacks/low_res") {
		t.Errorf("error must name the subpack: %v", err)
	}
	// Default (no opt-in): subpack corruption is irrelevant — parent
	// subpacks stay opaque and the squash proceeds.
	_, err = Squash(dir, Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatalf("default squash must not inspect subpacks: %v", err)
	}
}

func copyTreeTo(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
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
		return writeFile(target, data)
	})
}
