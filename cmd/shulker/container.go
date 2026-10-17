package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shulker-tools/shulker/internal/graph"
	"github.com/shulker-tools/shulker/internal/pack"
	"github.com/shulker-tools/shulker/internal/pipeline"
	"github.com/shulker-tools/shulker/internal/report"
	"github.com/shulker-tools/shulker/internal/validate"
)

// cmdSquashContainer squashes every resource-pack member of a
// .mcaddon/.mctemplate/.mcworld. Behavior packs and world files pass
// through byte-identically.
func cmdSquashContainer(in, out string, opts pipeline.Options, outMap, reportFile string, quiet bool) {
	if !quiet {
		fmt.Printf("shulker %s — squashing container %s\n", version, in)
	}
	res, err := pipeline.SquashContainer(in, opts)
	if err != nil {
		fail("%v", err)
	}
	if err := res.Container.WriteZipFile(out); err != nil {
		fail("writing %s: %v", out, err)
	}

	// One merged mapping for directory members (paths prefixed with the
	// member root). Nested-zip members are noted: their mapping is
	// available by extracting the member and squashing it directly.
	var renames []graph.Rename
	var rewrites []graph.Rewrite
	skips := map[string]string{}
	pngOptimized := map[string]bool{}
	nested := 0
	for _, m := range res.Members {
		if m.NestedZip {
			nested++
			continue
		}
		prefix := m.Prefix + "/"
		for _, rn := range m.Squash.Transform.Renames {
			renames = append(renames, graph.Rename{From: prefix + rn.From, To: prefix + rn.To})
		}
		for _, rw := range m.Squash.Transform.Rewrites {
			rewrites = append(rewrites, graph.Rewrite{
				File: prefix + rw.File, Pointer: rw.Pointer, From: rw.From, To: rw.To,
			})
		}
		for p, why := range m.Squash.Transform.SkipReasons {
			skips[prefix+p] = why
		}
		for p := range m.Squash.Transform.PNGOptimized {
			pngOptimized[prefix+p] = true
		}
	}
	if outMap == "" {
		outMap = strings.TrimSuffix(out, filepath.Ext(out)) + ".map.json"
	}
	mapping := report.NewMapping("shulker/"+version, renames, rewrites, pngOptimized, skips)
	if err := os.WriteFile(outMap, mapping.Encode(), 0o644); err != nil {
		fail("writing mapping %s: %v", outMap, err)
	}

	if !quiet {
		before, after := res.BytesBefore, res.Container.TotalBytes()
		pct := 0.0
		if before > 0 {
			pct = float64(before-after) * 100 / float64(before)
		}
		fmt.Printf("\n  container: %s -> %s (%.1f%% saved)\n",
			report.HumanBytes(before), report.HumanBytes(after), pct)
		for _, m := range res.Members {
			fmt.Printf("  member %s: renamed %d, rewrote %d, re-encoded %d PNGs\n",
				m.Prefix, len(m.Squash.Transform.Renames),
				len(m.Squash.Transform.Rewrites), len(m.Squash.Transform.PNGOptimized))
		}
		for _, m := range res.Container.BPMembers() {
			fmt.Printf("  member %s: behavior pack — passed through untouched\n", m.Prefix)
		}
		if nested > 0 {
			fmt.Printf("  note: %d nested-zip member(s) squashed in place; the mapping covers directory members (extract and squash a member directly for its own mapping)\n", nested)
		}
		fmt.Printf("\n  output:   %s\n  mapping:  %s\n", out, outMap)
	}
	if reportFile != "" {
		var warnings []string
		for _, m := range res.Members {
			for _, f := range m.Squash.Warnings() {
				warnings = append(warnings, m.Prefix+": "+f.String())
			}
		}
		after := res.Container.TotalBytes()
		sizes := &report.SizeReport{
			FilesBefore: len(res.Container.Entries),
			FilesAfter:  len(res.Container.Entries),
			TotalBefore: res.BytesBefore,
			TotalAfter:  after,
		}
		if err := os.WriteFile(reportFile, sizes.Encode("shulker/"+version,
			len(renames), len(rewrites), len(pngOptimized), warnings), 0o644); err != nil {
			fail("writing report %s: %v", reportFile, err)
		}
		if !quiet {
			fmt.Printf("  report:   %s\n", reportFile)
		}
	}
}

// cmdCheckContainer validates every resource-pack member of a container
// without changing anything. Behavior packs are inventoried but not
// validated (out of v0.2 scope) — stated, not hidden.
func cmdCheckContainer(in string) {
	c, err := pack.LoadContainer(in)
	if err != nil {
		fail("%v", err)
	}
	rp := c.RPMembers()
	bp := c.BPMembers()
	fmt.Printf("shulker %s — checking container %s\n", version, in)
	fmt.Printf("  %d entries, %d resource pack(s), %d behavior pack(s)\n",
		len(c.Entries), len(rp), len(bp))
	anyErr := false
	for _, m := range rp {
		mp, err := memberPack(c, m)
		if err != nil {
			fmt.Printf("  member %s: LOAD ERROR: %v\n", m.Prefix, err)
			anyErr = true
			continue
		}
		docs, parseErrs := graph.ParseAll(mp)
		g, err := graph.Build(mp, docs, parseErrs)
		if err != nil {
			fmt.Printf("  member %s: %v\n", m.Prefix, err)
			anyErr = true
			continue
		}
		r := validate.Check(mp, g)
		status := "passed (with warnings)"
		if r.HasErrors() {
			status = "FAILED"
			anyErr = true
		} else if len(r.Findings) == 0 {
			status = "clean"
		}
		fmt.Printf("  member %s: %d findings — %s\n", m.Prefix, len(r.Findings), status)
		for _, f := range r.Findings {
			fmt.Printf("    - %s\n", f.String())
		}
	}
	for _, m := range bp {
		fmt.Printf("  member %s: behavior pack — not validated by shulker v0.2\n", m.Prefix)
	}
	if len(rp) == 0 {
		fmt.Println("  no resource pack members (world-only container)")
	}
	if anyErr {
		fmt.Println("\n  check FAILED")
		os.Exit(1)
	}
	fmt.Println("\n  check done")
}

// memberPack loads one container member as a pack.
func memberPack(c *pack.Container, m *pack.ContainerMember) (*pack.Pack, error) {
	if m.Kind == pack.MemberFile {
		for _, e := range c.Entries {
			if e.Path == m.Prefix {
				return pack.LoadZipBytes(e.Data, m.Prefix)
			}
		}
		return nil, fmt.Errorf("member %q is missing", m.Prefix)
	}
	files, ok := c.MemberFiles(m)
	if !ok {
		return nil, fmt.Errorf("member %q has no files", m.Prefix)
	}
	return pack.FromFiles(m.Prefix, files)
}
