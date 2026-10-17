package pipeline

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lettucebecaesar/shulker/internal/transform"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "packs", name)
}

func TestSquashPipelineRich(t *testing.T) {
	res, err := Squash(fixture(t, "rich"), Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Post.HasErrors() {
		for _, f := range res.Post.Errors() {
			t.Errorf("post-transform defect: %s", f.String())
		}
	}
	if len(res.Out.Files) != len(res.In.Files) {
		t.Errorf("file count changed: %d -> %d", len(res.In.Files), len(res.Out.Files))
	}
	// The squash must actually save bytes on the fixture (comments,
	// whitespace, BOM, float noise, long paths).
	if res.Out.TotalBytes() >= res.In.TotalBytes() {
		t.Errorf("squash did not shrink the fixture: %d -> %d",
			res.In.TotalBytes(), res.Out.TotalBytes())
	}
}

func TestSquashPipelineRefusesBrokenPack(t *testing.T) {
	_, err := Squash(fixture(t, "broken"), Options{
		Transforms: transform.DefaultOptions(),
		Generator:  "shulker/test",
	})
	if err == nil {
		t.Fatal("broken pack must be refused")
	}
	if !strings.Contains(err.Error(), "refuses to work around") {
		t.Errorf("error should explain the refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "textures/broken.json") {
		t.Errorf("error should name the broken file: %v", err)
	}
}

// The cache must be provably transparent: cached and fresh squashes are
// byte-identical (determinism makes this a theorem, the test makes it a
// fact).
func TestSquashCacheTransparent(t *testing.T) {
	dir := t.TempDir()
	run := func(cache bool) []byte {
		res, err := Squash(fixture(t, "rich"), Options{
			Transforms: transform.DefaultOptions(),
			UseCache:   cache,
			CacheDir:   dir,
			Generator:  "shulker/test",
		})
		if err != nil {
			t.Fatal(err)
		}
		var zip bytes.Buffer
		if err := res.Out.WriteZip(&zip); err != nil {
			t.Fatal(err)
		}
		return zip.Bytes()
	}
	fresh := run(false)
	cachedMiss := run(true) // populates the cache
	cachedHit := run(true)  // must reuse it
	if !bytes.Equal(fresh, cachedMiss) {
		t.Error("first (cache-miss) run differs from no-cache run")
	}
	if !bytes.Equal(cachedMiss, cachedHit) {
		t.Error("cached run differs from fresh run")
	}
}
