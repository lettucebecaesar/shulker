// Package pack loads, classifies, and deterministically repacks Minecraft
// Bedrock Edition resource packs (RP). It accepts .mcpack/zip files and
// plain directories, tolerates the industry-standard "single nested
// directory" zip layout, and refuses world templates and addon bundles
// loudly instead of mangling them.
package pack

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxTotalUncompressed bounds the decoded payload of an input zip (zip-bomb
// guard). Vanilla-adjacent packs are hundreds of MB; 2 GiB is a generous
// ceiling that still refuses bombs.
const MaxTotalUncompressed = 2 << 30

// ManifestPath is the engine-fixed location of the manifest. Everything in
// shulker treats this file as read-only data.
const ManifestPath = "manifest.json"

// NotResourcePackError explains why an input was rejected as "not a
// loadable resource pack". It carries a human-facing reason and hint.
type NotResourcePackError struct {
	Reason string
	Hint   string
}

func (e *NotResourcePackError) Error() string {
	msg := "not a resource pack: " + e.Reason
	if e.Hint != "" {
		msg += " (" + e.Hint + ")"
	}
	return msg
}

// File is one in-memory pack member with its normalized slash path.
type File struct {
	Path  string
	Data  []byte
	Class Class
}

// Pack is a loaded resource pack. Files are sorted by path and addressable
// by path; iteration is deterministic.
type Pack struct {
	Source     string // description of the input (path or "archive")
	RootPrefix string // stripped nesting directory, "" when the pack is at the root
	Files      []*File
	Warnings   []Finding

	Manifest *Manifest

	byPath  map[string]*File
	byLower map[string]*File
}

// File returns the pack member at an exact path.
func (p *Pack) File(path string) (*File, bool) {
	f, ok := p.byPath[path]
	return f, ok
}

// Lookup finds a file case-insensitively. MCBE refs are resolved against
// case-sensitive filesystems at runtime, but community packs are full of
// case-mismatched paths that happen to work on Windows. The second result
// is the actual stored path (which may differ in case).
func (p *Pack) Lookup(path string) (*File, string, bool) {
	if f, ok := p.byPath[path]; ok {
		return f, path, true
	}
	lower := strings.ToLower(path)
	// byLower built at assemble time keeps this O(1).
	if f, ok := p.byLower[lower]; ok {
		return f, f.Path, true
	}
	return nil, "", false
}

// Paths returns all file paths in sorted order.
func (p *Pack) Paths() []string {
	out := make([]string, len(p.Files))
	for i, f := range p.Files {
		out[i] = f.Path
	}
	return out
}

// byLower is the case-insensitive index backing Lookup; see Pack.Lookup.

// Load opens a resource pack from a .mcpack/.zip file or a directory.
func Load(path string) (*Pack, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open pack %q: %w", path, err)
	}
	if fi.IsDir() {
		return LoadDir(path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mctemplate", ".mcworld", ".mcaddon":
		return nil, &NotResourcePackError{
			Reason: fmt.Sprintf("%q is a world/addon container, not a resource pack", filepath.Base(path)),
			Hint:   "extract the resource pack (.mcpack or folder with manifest.json) and run shulker on that",
		}
	}
	return LoadZip(path)
}

// LoadDir loads a resource pack from a directory tree.
func LoadDir(dir string) (*Pack, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var files []*File
	var warnings []Finding
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			rel, _ := filepath.Rel(abs, p)
			warnings = append(warnings, Finding{
				Severity: SeverityWarning,
				Path:     filepath.ToSlash(rel),
				Msg:      "skipping non-regular file (symlinks are not supported in packs)",
			})
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return err
		}
		norm, err := NormalizePath(filepath.ToSlash(rel))
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, &File{Path: norm, Data: data, Class: Classify(norm)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %q: %w", dir, err)
	}
	return assemble(files, dir, warnings)
}

// LoadZip loads a resource pack from a .mcpack/.zip archive.
func LoadZip(path string) (*Pack, error) {
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
	return loadFromZip(zr, path)
}

// LoadZipBytes loads a resource pack from in-memory zip bytes (used for
// packs nested inside containers).
func LoadZipBytes(data []byte, source string) (*Pack, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%q is not a readable zip archive: %w", source, err)
	}
	return loadFromZip(zr, source)
}

func loadFromZip(zr *zip.Reader, source string) (*Pack, error) {
	var files []*File
	var warnings []Finding
	var total int64
	seen := map[string]bool{}
	for _, zf := range zr.File {
		if strings.HasSuffix(zf.Name, "/") {
			continue // directory entries carry no content
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
		files = append(files, &File{Path: norm, Data: data, Class: Classify(norm)})
	}
	return assemble(files, source, warnings)
}

// assemble applies the pack-level invariants: template detection, nested
// single-directory stripping, manifest discovery + validation, deterministic
// ordering.
func assemble(files []*File, source string, warnings []Finding) (*Pack, error) {
	// --- world template / addon detection ---
	// .mctemplate/.mcworld: world + packs. Never silently squashed.
	for _, f := range files {
		if f.Path == "level.dat" || f.Path == "levelname.txt" || HasPrefixDir(f.Path, "db/") {
			return nil, &NotResourcePackError{
				Reason: "input contains a world (level.dat/levelname.txt/db/)",
				Hint:   "extract the resource pack and run shulker on that",
			}
		}
		if HasPrefixDir(f.Path, "Resource Packs/") || HasPrefixDir(f.Path, "Behavior Packs/") {
			return nil, &NotResourcePackError{
				Reason: "input is a world template / addon bundle (contains 'Resource Packs/' or 'Behavior Packs/')",
				Hint:   "extract the resource pack and run shulker on that",
			}
		}
	}

	// --- manifest discovery, with nested-single-dir stripping ---
	manifest, prefix, err := findManifest(files)
	if err != nil {
		return nil, err
	}
	if prefix != "" {
		stripped := files[:0]
		for _, f := range files {
			f.Path = strings.TrimPrefix(f.Path, prefix)
			f.Class = Classify(f.Path)
			stripped = append(stripped, f)
		}
		files = stripped
		manifest = strings.TrimPrefix(manifest, prefix)
		warnings = append(warnings, Finding{
			Severity: SeverityInfo,
			Path:     prefix,
			Msg:      fmt.Sprintf("stripped nested pack directory %q", prefix),
		})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	p := &Pack{Source: source, RootPrefix: prefix, Files: files, Warnings: warnings}
	p.byPath = make(map[string]*File, len(files))
	p.byLower = make(map[string]*File, len(files))
	for _, f := range files {
		if _, dup := p.byPath[f.Path]; dup {
			return nil, fmt.Errorf("duplicate pack path %q (case-variant duplicate entries)", f.Path)
		}
		p.byPath[f.Path] = f
		p.byLower[strings.ToLower(f.Path)] = f
	}

	// Manifest presence + validity is a hard requirement: shulker must know
	// it is squashing a pack the engine can mount.
	mf, ok := p.byPath[manifest]
	if !ok {
		return nil, &NotResourcePackError{
			Reason: "no manifest.json found",
			Hint:   "a resource pack must contain manifest.json at its root",
		}
	}
	if manifest != ManifestPath {
		p.Warnings = append(p.Warnings, Finding{
			Severity: SeverityWarning,
			Path:     manifest,
			Msg:      "manifest has non-canonical casing; the engine requires lowercase \"manifest.json\"",
		})
	}
	m, mFindings := ParseManifest(mf.Data)
	p.Manifest = m
	p.Warnings = append(p.Warnings, mFindings...)
	if HasErrors(mFindings) {
		return nil, fmt.Errorf("manifest.json is invalid: %s", joinFindings(mFindings))
	}

	// Soft observations.
	if _, ok := p.byPath["pack_icon.png"]; !ok {
		p.Warnings = append(p.Warnings, Finding{
			Severity: SeverityInfo,
			Path:     "pack_icon.png",
			Msg:      "missing pack icon (optional, but stores show a placeholder)",
		})
	}
	if p.hasPrefix("subpacks/") {
		p.Warnings = append(p.Warnings, Finding{
			Severity: SeverityWarning,
			Path:     "subpacks/",
			Msg: "subpacks detected; they are left completely untouched by default — " +
				"pass --squash-subpacks to squash each subpack with the same rigor",
		})
	}
	return p, nil
}

// FromFiles assembles a transformed file set into a Pack: classification,
// ordering, indexing, and manifest validation run exactly as they do for
// Load — the pipeline's output goes through the same rigor as its input.
// Discovery (template detection, nested-prefix stripping) is intentionally
// absent: transforms start from an already-loaded pack.
func FromFiles(source string, files []*File) (*Pack, error) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	p := &Pack{Source: source, Files: files}
	p.byPath = make(map[string]*File, len(files))
	p.byLower = make(map[string]*File, len(files))
	for _, f := range files {
		f.Class = Classify(f.Path)
		if _, dup := p.byPath[f.Path]; dup {
			return nil, fmt.Errorf("duplicate pack path %q in transformed file set", f.Path)
		}
		p.byPath[f.Path] = f
		p.byLower[strings.ToLower(f.Path)] = f
	}
	mf, ok := p.byPath[ManifestPath]
	if !ok {
		return nil, &NotResourcePackError{
			Reason: "transformed file set has no manifest.json",
			Hint:   "the transform must never drop or rename the manifest",
		}
	}
	m, mFindings := ParseManifest(mf.Data)
	p.Manifest = m
	p.Warnings = append(p.Warnings, mFindings...)
	if HasErrors(mFindings) {
		return nil, fmt.Errorf("transformed manifest.json is invalid: %s", joinFindings(mFindings))
	}
	return p, nil
}

// findManifest locates manifest.json at the root, or under exactly one
// nested directory (the classic community-zip layout:
// MyPack/manifest.json + MyPack/**).
func findManifest(files []*File) (manifestPath, prefix string, err error) {
	// Exact and case-variant matches at the root.
	for _, f := range files {
		if f.Path == ManifestPath {
			return ManifestPath, "", nil
		}
		if strings.EqualFold(f.Path, ManifestPath) {
			return f.Path, "", nil
		}
	}
	if len(files) == 0 {
		return "", "", &NotResourcePackError{Reason: "input contains no files"}
	}
	// Collect the top-level shape: directory names with a trailing slash,
	// plus a "" marker for stray root-level files.
	roots := map[string]bool{}
	hasRootFiles := false
	for _, f := range files {
		if i := strings.IndexByte(f.Path, '/'); i >= 0 {
			roots[f.Path[:i+1]] = true
		} else {
			hasRootFiles = true
		}
	}
	// The one classic tolerated layout: everything under a single directory
	// whose manifest sits one level down.
	if len(roots) == 1 && !hasRootFiles {
		var only string
		for r := range roots {
			only = r
		}
		for _, f := range files {
			if f.Path == only+ManifestPath {
				return f.Path, only, nil
			}
			if strings.EqualFold(f.Path, only+ManifestPath) {
				return f.Path, only, nil
			}
		}
		return "", "", &NotResourcePackError{
			Reason: fmt.Sprintf("single top-level directory %q contains no manifest.json", strings.TrimSuffix(only, "/")),
			Hint:   "point shulker at the directory that holds manifest.json",
		}
	}
	// Multiple roots (or stray root files) and no root manifest: looks like
	// an .mcaddon someone renamed, or a broken pack. Refuse with a message
	// that actually helps.
	var tops []string
	for r := range roots {
		tops = append(tops, strings.TrimSuffix(r, "/"))
	}
	sort.Strings(tops)
	shown := tops
	if len(shown) > 5 {
		shown = shown[:5]
	}
	return "", "", &NotResourcePackError{
		Reason: "no manifest.json at the root and no single nested pack directory (top level: " + strings.Join(shown, ", ") + ")",
		Hint:   "this looks like a bundle of packs; extract the one you want and squash that",
	}
}

func (p *Pack) hasPrefix(dir string) bool {
	for _, f := range p.Files {
		if HasPrefixDir(f.Path, dir) {
			return true
		}
	}
	return false
}

func joinFindings(fs []Finding) string {
	var parts []string
	for _, f := range fs {
		if f.Severity == SeverityError {
			parts = append(parts, f.Msg)
		}
	}
	return strings.Join(parts, "; ")
}
