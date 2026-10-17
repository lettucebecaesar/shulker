package pack

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// A container is a zip that carries one or more packs — a .mcaddon (pack
// bundles), a .mctemplate (world template: world files plus
// "Resource Packs/<name>" and "Behavior Packs/<name>" directories), or a
// .mcworld (world only). shulker squashes the RESOURCE pack members and
// leaves behavior packs and world files byte-identical: BP semantics are
// out of v0.2 scope, and world files are not ours to touch.
//
// Members can be directories inside the container ("MyPack/manifest.json")
// or nested zip files ("MyPack.mcpack") — both community layouts ship in
// the wild.

// MemberKind describes how a pack member is stored in the container.
type MemberKind uint8

const (
	// MemberDir is a pack stored as a directory inside the container.
	MemberDir MemberKind = iota
	// MemberFile is a pack stored as a nested zip (.mcpack/.zip) file.
	MemberFile
)

// ContainerMember is one pack found inside a container.
type ContainerMember struct {
	// Prefix is the container-relative path of the pack: "MyPack" for a
	// directory member, "Resource Packs/MyPack" inside world templates,
	// or "MyPack.mcpack" for a nested-zip member.
	Prefix string
	Kind   MemberKind
	// IsBP is true for behavior packs — loaded for inventory, never
	// squashed in v0.2.
	IsBP bool
	// ManifestName is the pack's declared name (reporting only).
	ManifestName string
}

// Container is a loaded pack-container archive.
type Container struct {
	Source   string
	Entries  []*File
	Members  []*ContainerMember
	Warnings []Finding
}

// ContainerExt reports whether path is a container extension shulker can
// open.
func ContainerExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mcaddon", ".mctemplate", ".mcworld":
		return true
	}
	return false
}

// LoadContainer reads a .mcaddon/.mctemplate/.mcworld archive.
func LoadContainer(path string) (*Container, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("%q is not a readable zip archive: %w", path, err)
	}
	return containerFromZip(zr, path)
}

// LoadContainerBytes reads a container from in-memory zip bytes.
func LoadContainerBytes(data []byte, source string) (*Container, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%q is not a readable zip archive: %w", source, err)
	}
	return containerFromZip(zr, source)
}

func containerFromZip(zr *zip.Reader, source string) (*Container, error) {
	c := &Container{Source: source}
	var total int64
	seen := map[string]bool{}
	for _, zf := range zr.File {
		if strings.HasSuffix(zf.Name, "/") {
			continue
		}
		norm, err := NormalizePath(zf.Name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", zf.Name, err)
		}
		if seen[norm] {
			return nil, fmt.Errorf("duplicate zip entry %q", norm)
		}
		seen[norm] = true
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %q: %w", norm, err)
		}
		data, err := io.ReadAll(rc)
		rerr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", norm, err)
		}
		if rerr != nil {
			return nil, fmt.Errorf("closing %q: %w", norm, rerr)
		}
		total += int64(len(data))
		if total > MaxTotalUncompressed {
			return nil, fmt.Errorf("zip expands past the %d byte safety limit", MaxTotalUncompressed)
		}
		c.Entries = append(c.Entries, &File{Path: norm, Data: data, Class: Classify(norm)})
	}

	// Discover pack members: directories containing a manifest.json.
	dirs := map[string]*File{} // dir -> its manifest entry
	for _, e := range c.Entries {
		if path.Base(e.Path) == ManifestPath {
			dir := Parent(e.Path)
			if dir != "" {
				dirs[dir] = e
			}
		}
	}
	// A manifest directory nested inside another manifest directory is a
	// SUBPACK, not a container member (testdata packs ship
	// subpacks/low_res/manifest.json alongside their own): members are
	// only the outermost manifest roots.
	outerDirs := map[string]*File{}
	for dir, manifestEntry := range dirs {
		nested := false
		for other := range dirs {
			if other != dir && strings.HasPrefix(dir, other+"/") {
				nested = true
				break
			}
		}
		if !nested {
			outerDirs[dir] = manifestEntry
		}
	}
	dirs = outerDirs
	// Nested-zip members.
	for _, e := range c.Entries {
		ext := strings.ToLower(path.Ext(e.Path))
		if ext != ".mcpack" && ext != ".zip" {
			continue
		}
		if _, already := dirs[strings.TrimSuffix(e.Path, path.Ext(e.Path))]; already {
			continue
		}
		if memberIsPack(e.Data) {
			c.Members = append(c.Members, &ContainerMember{
				Prefix: e.Path,
				Kind:   MemberFile,
				IsBP:   memberIsBP(e.Data),
			})
		}
	}
	for dir, manifestEntry := range dirs {
		c.Members = append(c.Members, &ContainerMember{
			Prefix:       dir,
			Kind:         MemberDir,
			IsBP:         manifestIsBP(manifestEntry.Data),
			ManifestName: manifestName(manifestEntry.Data),
		})
	}
	sort.Slice(c.Members, func(i, j int) bool { return c.Members[i].Prefix < c.Members[j].Prefix })

	// Loose pack files (no container structure) are not supported: the
	// user should squash the .mcpack directly. Detected above via
	// memberIsPack only for real nested files, so nothing to add.
	sort.Slice(c.Entries, func(i, j int) bool { return c.Entries[i].Path < c.Entries[j].Path })
	if len(c.Entries) == 0 {
		return nil, fmt.Errorf("%s contains no files", source)
	}
	return c, nil
}

// memberIsPack reports whether the zip bytes look like a loadable pack
// (manifest present — including the classic single nested directory).
func memberIsPack(data []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, zf := range zr.File {
		norm, err := NormalizePath(zf.Name)
		if err != nil {
			continue
		}
		if norm == ManifestPath || strings.HasSuffix(norm, "/"+ManifestPath) ||
			strings.EqualFold(norm, ManifestPath) {
			return true
		}
	}
	return false
}

func memberIsBP(data []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, zf := range zr.File {
		norm, err := NormalizePath(zf.Name)
		if err != nil {
			continue
		}
		if norm == ManifestPath || strings.HasSuffix(norm, "/"+ManifestPath) {
			rc, err := zf.Open()
			if err != nil {
				return false
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return false
			}
			return manifestIsBP(data)
		}
	}
	return false
}

func manifestIsBP(data []byte) bool {
	m, findings := ParseManifest(data)
	if m == nil || HasErrors(findings) {
		return false
	}
	for _, mod := range m.Modules {
		switch mod.Type {
		case "resources":
			return false
		case "data", "world_template", "script", "javascript":
			return true
		}
	}
	return false
}

func manifestName(data []byte) string {
	m, _ := ParseManifest(data)
	if m == nil {
		return ""
	}
	return m.Name
}

// RPMembers returns the resource-pack members in sorted order.
func (c *Container) RPMembers() []*ContainerMember {
	var out []*ContainerMember
	for _, m := range c.Members {
		if !m.IsBP {
			out = append(out, m)
		}
	}
	return out
}

// BPMembers returns the behavior-pack members in sorted order.
func (c *Container) BPMembers() []*ContainerMember {
	var out []*ContainerMember
	for _, m := range c.Members {
		if m.IsBP {
			out = append(out, m)
		}
	}
	return out
}

// MemberFiles returns the container entries of a directory member, with
// the member prefix stripped so they form a loadable pack root. File
// members return (nil, false) — read them via LoadContainerBytes.
func (c *Container) MemberFiles(m *ContainerMember) ([]*File, bool) {
	if m.Kind != MemberDir {
		return nil, false
	}
	prefix := m.Prefix + "/"
	var out []*File
	for _, e := range c.Entries {
		if strings.HasPrefix(e.Path, prefix) {
			rel := strings.TrimPrefix(e.Path, prefix)
			out = append(out, &File{Path: rel, Data: e.Data, Class: Classify(rel)})
		}
	}
	return out, true
}

// SetMemberFiles replaces a directory member's entries with a transformed
// file set (paths member-relative); the caller's paths are prefixed back.
func (c *Container) SetMemberFiles(m *ContainerMember, files []*File) {
	prefix := m.Prefix + "/"
	kept := make([]*File, 0, len(c.Entries))
	for _, e := range c.Entries {
		if !strings.HasPrefix(e.Path, prefix) {
			kept = append(kept, e)
		}
	}
	for _, f := range files {
		rel := f.Path
		kept = append(kept, &File{
			Path:  prefix + rel,
			Data:  f.Data,
			Class: f.Class,
		})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Path < kept[j].Path })
	c.Entries = kept
}

// SetMemberZip replaces a nested-zip member with new zip bytes.
func (c *Container) SetMemberZip(m *ContainerMember, zipData []byte) {
	for _, e := range c.Entries {
		if e.Path == m.Prefix {
			e.Data = zipData
			return
		}
	}
}

// TotalBytes sums the uncompressed entry sizes.
func (c *Container) TotalBytes() int64 {
	var n int64
	for _, e := range c.Entries {
		n += int64(len(e.Data))
	}
	return n
}

// WriteZipFile writes the container as a canonical zip (sorted entries,
// fixed timestamps, DEFLATE for text/JSON, STORE for compressed payloads).
func (c *Container) WriteZipFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zerr := c.WriteZip(f)
	if cerr := f.Close(); zerr == nil {
		zerr = cerr
	}
	return zerr
}

// WriteZip writes the container to w.
func (c *Container) WriteZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, e := range c.Entries {
		h := &zip.FileHeader{
			Name:     e.Path,
			Method:   methodFor(e),
			Modified: fixedTime,
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(e.Data); err != nil {
			return err
		}
	}
	return zw.Close()
}
