package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPutGetRoundTrip(t *testing.T) {
	c, err := New(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Get(Key("missing")); ok {
		t.Error("empty cache must miss")
	}
	c.Put(PNGKey([]byte("fakepng")), true, []byte("optimized"))
	data, changed, ok := c.Get(PNGKey([]byte("fakepng")))
	if !ok || !changed || !bytes.Equal(data, []byte("optimized")) {
		t.Errorf("Get = %q %v %v", data, changed, ok)
	}
	// Different content, different key.
	if _, _, ok := c.Get(PNGKey([]byte("otherpng"))); ok {
		t.Error("wrong content must miss")
	}
}

func TestDisabledNeverPersists(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	c.Put(Key("x"), true, []byte("data"))
	if _, _, ok := c.Get(Key("x")); ok {
		t.Error("disabled cache must always miss")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("disabled cache wrote %d entries", len(entries))
	}
}

func TestUnwritableDirDegrades(t *testing.T) {
	// A path that cannot be a directory degrades to a disabled cache,
	// never an error — cache failures must not fail squashes.
	bad := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(bad, true)
	if err != nil {
		t.Fatalf("New with unwritable dir: %v", err)
	}
	c.Put(Key("x"), true, []byte("data"))
	if _, _, ok := c.Get(Key("x")); ok {
		t.Error("degraded cache must miss")
	}
}

func TestKeyIsStableAndDistinct(t *testing.T) {
	k1 := Key("png", "a")
	k2 := Key("png", "a")
	if k1 != k2 {
		t.Error("key not stable")
	}
	if Key("png", "a") == Key("png", "b") {
		t.Error("different inputs produced the same key")
	}
}
