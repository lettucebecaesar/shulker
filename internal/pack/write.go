package pack

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// fixedTime is the timestamp every zip entry carries: 1980-01-01T00:00:00Z,
// the ZIP format's own epoch. Deterministic output is a hard invariant —
// no wall-clock, no machine fingerprints, byte-identical repacks.
var fixedTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// WriteZip writes the pack as a canonical .mcpack:
//   - entries sorted by path,
//   - every entry timestamp 1980-01-01T00:00:00Z,
//   - no extra fields,
//   - DEFLATE for JSON/text/lang, STORE for already-compressed payloads
//     (png/ogg/jpg — deflating them again is pure CPU waste),
//   - TGA STOREd? No: TGA is typically uncompressed or RLE, so it deflates
//     well; it is the one image exception.
func (p *Pack) WriteZip(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, f := range p.Files {
		h := &zip.FileHeader{
			Name:     f.Path,
			Method:   methodFor(f),
			Modified: fixedTime,
		}
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.Data); err != nil {
			return err
		}
	}
	return zw.Close()
}

// WriteZipFile writes the pack to a zip file on disk.
func (p *Pack) WriteZipFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return p.WriteZip(f)
}

func methodFor(f *File) uint16 {
	if !f.Class.Compressible() {
		// TGA is the exception among binary payloads: it is usually stored
		// raw/RLE and compresses well under DEFLATE.
		if Ext(f.Path) == ".tga" {
			return zip.Deflate
		}
		return zip.Store
	}
	return zip.Deflate
}

// WriteDir materializes the pack as a directory tree (used by the Regolith
// filter mode, which stages packs as plain directories, and by tests).
func (p *Pack) WriteDir(dir string) error {
	for _, f := range p.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, f.Data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", f.Path, err)
		}
	}
	return nil
}

// TotalBytes sums the uncompressed size of every file.
func (p *Pack) TotalBytes() int64 {
	var n int64
	for _, f := range p.Files {
		n += int64(len(f.Data))
	}
	return n
}
