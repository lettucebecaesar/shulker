package pipeline

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shulker-tools/shulker/internal/pack"
	"github.com/shulker-tools/shulker/internal/transform"
)

// richEntrySet reads the golden fixture into path->bytes with a prefix.
func richEntrySet(t *testing.T, prefix string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	root := filepath.Join("..", "..", "testdata", "packs", "rich")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[prefix+filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
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
	return buf.Bytes()
}

const bpManifest = `{
	"format_version": 2,
	"header": {
		"name": "Fixture BP",
		"uuid": "3f7f7d1e-ec1e-4a2e-bcd5-69b1b70cf4a6",
		"version": [1, 0, 0],
		"min_engine_version": [1, 21, 0]
	},
	"modules": [
		{
			"type": "data",
			"uuid": "a1944c3a-a34c-45d5-ae42-2d388d7ee7e4",
			"version": [1, 0, 0]
		}
	]
}
`

// TestSquashMCAddon covers the full container flow: the RP member gets the
// real pipeline (with its own post-transform validation), the BP member
// and loose files pass through byte-identically, and the whole container
// squash is deterministic.
func TestSquashMCAddon(t *testing.T) {
	addon := writeZip(t, func() map[string][]byte {
		entries := richEntrySet(t, "MyRP/")
		entries["MyBP/manifest.json"] = []byte(bpManifest)
		entries["MyBP/entities/mob.json"] = []byte(`{"format_version":"1.20.0"}`)
		entries["credits.txt"] = []byte("world file, not ours to touch")
		return entries
	}())
	addonPath := filepath.Join(t.TempDir(), "bundle.mcaddon")
	if err := os.WriteFile(addonPath, addon, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	}
	res, err := SquashContainer(addonPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Members) != 1 || res.Members[0].Prefix != "MyRP" {
		t.Fatalf("expected one RP member, got %+v", res.Members)
	}
	if len(res.Container.BPMembers()) != 1 {
		t.Fatalf("expected the BP to be inventoried, got %d", len(res.Container.BPMembers()))
	}

	// The squashed container re-loads as a container.
	outPath := filepath.Join(t.TempDir(), "bundle.squashed.mcaddon")
	if err := res.Container.WriteZipFile(outPath); err != nil {
		t.Fatal(err)
	}
	reloaded, err := pack.LoadContainer(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.RPMembers()) != 1 || len(reloaded.BPMembers()) != 1 {
		t.Fatalf("reloaded member layout wrong: %+v", reloaded.Members)
	}
	entry := func(path string) []byte {
		for _, e := range reloaded.Entries {
			if e.Path == path {
				return e.Data
			}
		}
		return nil
	}
	// RP member content is transformed (minified) and renamed.
	blocks := entry("MyRP/blocks.json")
	if blocks == nil || strings.Contains(string(blocks), "\n") {
		t.Error("RP member was not squashed")
	}
	if entry("MyRP/entity/_0.entity.json") == nil {
		t.Error("RP member renames missing from container")
	}
	if entry("MyRP/entity/shulker_mob.entity.json") != nil {
		t.Error("old RP member path still present")
	}
	// BP member and world files byte-identical.
	if string(entry("MyBP/manifest.json")) != bpManifest {
		t.Error("BP manifest changed")
	}
	if string(entry("credits.txt")) != "world file, not ours to touch" {
		t.Error("world file changed")
	}

	// Determinism at the container level.
	res2, err := SquashContainer(addonPath, opts)
	if err != nil {
		t.Fatal(err)
	}
	var zip1, zip2 bytes.Buffer
	if err := res.Container.WriteZip(&zip1); err != nil {
		t.Fatal(err)
	}
	if err := res2.Container.WriteZip(&zip2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zip1.Bytes(), zip2.Bytes()) {
		t.Error("container squash is not byte-deterministic")
	}
}

// TestSquashMCTemplate covers the world-template layout: world files plus
// "Resource Packs/<name>" directories.
func TestSquashMCTemplate(t *testing.T) {
	template := writeZip(t, func() map[string][]byte {
		entries := richEntrySet(t, "Resource Packs/MyRP/")
		entries["Behavior Packs/MyBP/manifest.json"] = []byte(bpManifest)
		entries["levelname.txt"] = []byte("Fixture World")
		entries["level.dat"] = []byte("fake world bytes")
		return entries
	}())
	tmplPath := filepath.Join(t.TempDir(), "world.mctemplate")
	if err := os.WriteFile(tmplPath, template, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := SquashContainer(tmplPath, Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Members) != 1 || res.Members[0].Prefix != "Resource Packs/MyRP" {
		t.Fatalf("expected the template RP member, got %+v", res.Members)
	}
	for _, e := range res.Container.Entries {
		switch e.Path {
		case "levelname.txt", "level.dat":
			// World files byte-identical.
		case "Behavior Packs/MyBP/manifest.json":
			if string(e.Data) != bpManifest {
				t.Error("template BP manifest changed")
			}
		}
	}
	found := false
	for _, e := range res.Container.Entries {
		if e.Path == "Resource Packs/MyRP/blocks.json" && !strings.Contains(string(e.Data), "\n") {
			found = true
		}
	}
	if !found {
		t.Error("template RP member was not squashed")
	}
}

// TestSquashNestedZipMember covers packs stored as .mcpack files inside
// the container — squashed in place, mapping intentionally unavailable.
func TestSquashNestedZipMember(t *testing.T) {
	var rpZip bytes.Buffer
	rpPackFiles := richEntrySet(t, "")
	// writeZip needs a Pack-shaped zip; feed it the entries directly.
	zw := zip.NewWriter(&rpZip)
	for name, data := range rpPackFiles {
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

	addon := writeZip(t, map[string][]byte{
		"Packed.mcpack": rpZip.Bytes(),
	})
	addonPath := filepath.Join(t.TempDir(), "nested.mcaddon")
	if err := os.WriteFile(addonPath, addon, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := SquashContainer(addonPath, Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Members) != 1 || !res.Members[0].NestedZip {
		t.Fatalf("expected the nested-zip member, got %+v", res.Members)
	}
	// The member entry is still a zip and now contains the squashed pack.
	var memberData []byte
	for _, e := range res.Container.Entries {
		if e.Path == "Packed.mcpack" {
			memberData = e.Data
		}
	}
	memberPack, err := pack.LoadZipBytes(memberData, "Packed.mcpack")
	if err != nil {
		t.Fatalf("squashed member no longer loads as a pack: %v", err)
	}
	if _, ok := memberPack.File("entity/_0.entity.json"); !ok {
		t.Error("nested member did not get renamed")
	}
	blocks, _ := memberPack.File("blocks.json")
	if strings.Contains(string(blocks.Data), "\n") {
		t.Error("nested member did not get minified")
	}
}

// TestSquashWorldOnlyContainer: a .mcworld has no pack members — the
// squash degrades to a canonical repack without touching content.
func TestSquashWorldOnlyContainer(t *testing.T) {
	world := writeZip(t, map[string][]byte{
		"levelname.txt": []byte("Just A World"),
		"level.dat":     []byte("fake"),
	})
	worldPath := filepath.Join(t.TempDir(), "world.mcworld")
	if err := os.WriteFile(worldPath, world, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := SquashContainer(worldPath, Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Members) != 0 {
		t.Fatalf("world-only container should have no members: %+v", res.Members)
	}
	for _, e := range res.Container.Entries {
		if e.Path == "levelname.txt" && string(e.Data) != "Just A World" {
			t.Error("world content changed")
		}
	}
}
