// Package cache is shulker's content-addressed transform cache. Re-squashing
// unchanged assets is instant because the expensive pass (PNG re-encode) is
// keyed by content hash + transform version.
//
// Determinism makes the cache PROVABLY safe: same input bytes + same
// transform definition = same output bytes, so a cached result is by
// definition byte-identical to a fresh one. Cache failures (unwritable dir,
// full disk) are degraded to cache misses — a cache can never fail a squash.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Cache stores transform outputs under a content-addressed layout:
// <dir>/<key[:2]>/<key[2:]>. The sharding keeps directories listable.
type Cache struct {
	dir     string
	enabled bool
}

// DefaultDir returns ~/.cache/shulker, per the documented contract.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "shulker"), nil
}

// New creates a cache rooted at dir. dir == "" uses DefaultDir.
func New(dir string, enabled bool) (*Cache, error) {
	if !enabled {
		return &Cache{enabled: false}, nil
	}
	if dir == "" {
		var err error
		dir, err = DefaultDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Unwritable cache location: degrade to disabled, never fail.
		return &Cache{enabled: false}, nil
	}
	return &Cache{dir: dir, enabled: true}, nil
}

// Key derives the cache key from the transform identity and input content.
func Key(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// HashBytes returns the hex sha256 of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Get fetches a cached payload. ok is false on any miss or error; changed
// reproduces the transform's keep-if-smaller decision byte-for-byte.
func (c *Cache) Get(key string) (data []byte, changed bool, ok bool) {
	if !c.enabled {
		return nil, false, false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil || len(data) == 0 {
		return nil, false, false
	}
	return data[1:], data[0] == 1, true
}

// Put stores a payload (flagged with changed) atomically. Errors are
// swallowed: cache writes must never break a squash.
func (c *Cache) Put(key string, changed bool, data []byte) {
	if !c.enabled {
		return
	}
	target := c.path(key)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return
	}
	payload := make([]byte, 0, len(data)+1)
	if changed {
		payload = append(payload, 1)
	} else {
		payload = append(payload, 0)
	}
	payload = append(payload, data...)
	tmp, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmp.Name(), target)
}

func (c *Cache) path(key string) string {
	if len(key) < 2 {
		return filepath.Join(c.dir, "xx", key)
	}
	return filepath.Join(c.dir, key[:2], key[2:])
}

// PNGKey is the cache key for the PNG transform. profile pins the exact
// transform definition (pass + options); change it whenever the re-encode
// behavior changes, so cached entries can never describe a different
// transform than the code would run.
func PNGKey(profile string, data []byte) string {
	return Key("png-optimize", profile, HashBytes(data))
}
