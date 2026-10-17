# shulker roadmap

## v0.1 — shipped scope

| Milestone | Scope | Status |
|-----------|-------|--------|
| M1 Pack I/O | `.mcpack`/zip/directory loading, nested single-dir zips, manifest validation (verbatim storage), deterministic canonical repack, world-template/addon rejection | done |
| M2 Reference graph | Parsers + bidirectional graph over every RP file type; rewriteable vs blocker edges; PBR set lockstep; Molang protection seam | done |
| M4 Validator | Pre-flight check mode + post-transform proof (structural diff with rewrite allowlist, inventory accounting, reference continuity, PBR lockstep, deny-list immutability); golden fixtures with seeded defects | done |
| M3 Transforms | JSON minify (comments/BOM/trailing-comma stripping, f32-provable float normalization), conservative path renaming with Sourcemap-style mapping, PNG re-encode keep-if-smaller, audio passthrough | done |
| M5 CLI + Regolith + cache | `squash`/`check`/`map --reverse`/`regolith`, size report + JSON report, content-addressed PNG cache, verified `regolith run` integration | done |

Measured on the committed fixtures (enforced by
`TestBenchmarkFixtureReductions`): json_heavy **29.3%**, png_heavy **23.0%**.

## v0.2 candidates (written, not built)

- **Frozen vanilla path list**: generate a deny-list of actual vanilla paths
  from `Mojang/bedrock-samples` at build time, so vanilla-override protection
  becomes exhaustive instead of conservative. The graph API
  (`graph.IsDenyListed`) is already the single switch point.
- **PNG quantization** (pngquant-style) behind `--png-quantize`, guarded by
  keep-if-smaller and full-pixel-fidelity tests. Biggest remaining PNG win.
- **PNG optimization for override textures in place** (path stays, bytes
  re-encode): semantically safe, blocked in v0.1 by the "vanilla-override
  files stay byte-identical" acceptance contract; needs an explicit opt-in.
- **Molang tooling** on the existing tokenizer seam: dead-expression removal,
  then `variable.*` renaming with cross-file scope analysis. The seam
  (`internal/molang`) and the graph's sacred-string policy are ready.
- **Audio**: optional ffmpeg shell-out behind `--audio-ffmpeg` (never
  required, never default). The transform hook point exists.
- **Texture atlas packing + UV rewrite**: the big one. Needs geometry bone
  UV extraction + json-ui cooperation; tracked here so the design constraint
  (graph-driven, validator-proven) is not rediscovered the hard way.
- **JSON transform caching** (the PNG pass is cached today; JSON minify is
  cheap enough that it was not worth the complexity).
- **`.mcaddon` / `.mctemplate` passthrough**: extract member packs, squash
  each, repack the container.
- **Behavior packs**: the graph model extends naturally, but BP-side
  semantics (events, molang in BP entities) need their own fixture suite.
- **Subpacks**: recursive squash of `subpacks/` members.
- **Streaming pack mode** for 1 GB+ packs (current pipeline is in-memory;
  vanilla-adjacent packs are hundreds of MB).

## Non-goals

- Marketplace-style encryption or DRM theater: assets always end up on the
  client. See the README FAQ ("Is this DRM?").
- Zip-structure exploit tricks (fragile against engine updates, deliberately
  avoided).
