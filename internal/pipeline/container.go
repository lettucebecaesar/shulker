package pipeline

import (
	"bytes"
	"fmt"

	"github.com/shulker-tools/shulker/internal/pack"
)

// ContainerResult is a completed container squash: the transformed
// container plus per-member squash results.
type ContainerResult struct {
	Container *pack.Container
	Members   []*MemberResult
	// BytesBefore is the container's uncompressed size before squashing
	// (the container is squashed in place, so it is captured up front).
	BytesBefore int64
}

// MemberResult is one squashed resource-pack member.
type MemberResult struct {
	// Prefix is the container-relative member root.
	Prefix string
	// NestedZip is true when the member is a .mcpack/.zip file inside
	// the container (mappings are not emitted for those; extract and
	// squash directly if you need one).
	NestedZip bool
	// Squash is the member's pipeline result (validated inside).
	Squash *Result
}

// SquashContainer squashes every resource-pack member of a
// .mcaddon/.mctemplate/.mcworld container in place. Behavior packs and
// world files are copied through byte-identically — BP semantics are out
// of v0.2 scope, and world files are not ours to touch. Each member runs
// the full load → transform → prove pipeline with its own post-transform
// validation, so a member is only written back if its squash is proven.
func SquashContainer(in string, opts Options) (*ContainerResult, error) {
	c, err := pack.LoadContainer(in)
	if err != nil {
		return nil, fmt.Errorf("loading container: %w", err)
	}
	return SquashLoadedContainer(c, opts)
}

// SquashLoadedContainer runs the member squashes over an already-loaded
// container.
func SquashLoadedContainer(c *pack.Container, opts Options) (*ContainerResult, error) {
	res := &ContainerResult{Container: c}
	res.BytesBefore = c.TotalBytes()
	rp := c.RPMembers()
	if len(rp) == 0 {
		// A .mcworld or an addon with only behavior packs: the container
		// still gets the canonical repack, and that is all.
		return res, nil
	}
	for _, m := range rp {
		var memberPack *pack.Pack
		var err error
		switch m.Kind {
		case pack.MemberDir:
			files, ok := c.MemberFiles(m)
			if !ok || len(files) == 0 {
				return nil, fmt.Errorf("member %q has no files", m.Prefix)
			}
			memberPack, err = pack.FromFiles(m.Prefix, files)
		case pack.MemberFile:
			var zipData []byte
			for _, e := range c.Entries {
				if e.Path == m.Prefix {
					zipData = e.Data
					break
				}
			}
			if zipData == nil {
				return nil, fmt.Errorf("member %q is missing from the container", m.Prefix)
			}
			memberPack, err = pack.LoadZipBytes(zipData, m.Prefix)
		}
		if err != nil {
			return nil, fmt.Errorf("member %q: %w", m.Prefix, err)
		}
		sres, err := SquashPack(memberPack, opts)
		if err != nil {
			return nil, fmt.Errorf("member %q: %w", m.Prefix, err)
		}
		switch m.Kind {
		case pack.MemberDir:
			c.SetMemberFiles(m, sres.Out.Files)
		case pack.MemberFile:
			var buf bytes.Buffer
			if err := sres.Out.WriteZip(&buf); err != nil {
				return nil, fmt.Errorf("member %q: %w", m.Prefix, err)
			}
			c.SetMemberZip(m, buf.Bytes())
		}
		res.Members = append(res.Members, &MemberResult{
			Prefix:    m.Prefix,
			NestedZip: m.Kind == pack.MemberFile,
			Squash:    sres,
		})
	}
	return res, nil
}
