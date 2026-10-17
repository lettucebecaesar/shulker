# shulker roadmap

## v0.1 — shipped

| Milestone | Scope | Status |
|-----------|-------|--------|
| M1 Pack I/O | `.mcpack`/zip/directory loading, nested single-dir zips, manifest validation (verbatim storage), deterministic canonical repack, world-template/addon rejection | done |
| M2 Reference graph | Parsers + bidirectional graph over every RP file type; rewriteable vs blocker edges; PBR set lockstep; Molang protection seam | done |
| M4 Validator | Pre-flight check mode + post-transform proof (structural diff with rewrite allowlist, inventory accounting, reference continuity, PBR lockstep, deny-list immutability); golden fixtures with seeded defects | done |
| M3 Transforms | JSON minify (comments/BOM/trailing-comma stripping, f32-provable float normalization), conservative path renaming with Sourcemap-style mapping, PNG re-encode keep-if-smaller, audio passthrough | done |
| M5 CLI + Regolith + cache | `squash`/`check`/`map --reverse`/`regolith`, size report + JSON report, content-addressed PNG cache, verified `regolith run` integration | done |

## v0.2 — shipped

| Feature | Scope | Status |
|---------|-------|--------|
| Frozen vanilla path list | Exhaustive vanilla-override protection: exact + extension-insensitive matching against the embedded path universe (files + reference targets) from [MCBVanillaResourcePack](https://github.com/ZtechNetwork/MCBVanillaResourcePack); regenerable with `tools/vanilla-gen`; unblocks custom assets under vanilla directories | done |
| Lossless palette conversion | RGBA→indexed PNG for ≤256-color images, pixel-identical (63.3% on the `ui_palette` fixture) | done |
| `--png-quantize` | Opt-in lossy median-cut quantization (deterministic, binary-alpha only, alpha preserved) | done |
| `--png-optimize-overrides` | Opt-in pixel-verified lossless re-encode of vanilla overrides (paths stay, pixels stay) | done |
| `--audio-ffmpeg` | Opt-in Vorbis re-encode of pack-internal OGG through ffmpeg, keep-if-smaller, graceful absence | done |
| Containers | `.mcaddon`/`.mctemplate`/`.mcworld`: RP members (directory or nested-zip) each get the full pipeline; BP + world files byte-identical; one merged member-prefixed mapping | done |
| `--squash-subpacks` | Recursive subpack squash, each subpack its own full pipeline + validation, logs merged into the parent's | done |

Measured on the committed fixtures (enforced by
`TestBenchmarkFixtureReductions`): json_heavy **29.3%**, png_heavy
**23.0%**, ui_palette **63.3%**.

## v0.3+ candidates (written, not built)

- **PNG optimization for override textures with quantization** (the
  `--png-optimize-overrides` pass is lossless-only today; combining it with
  `--png-quantize` needs explicit doc warnings and a pixel-verified
  fallback).
- **Molang tooling** on the existing tokenizer seam: dead-expression
  removal, then `variable.*` renaming with cross-file scope analysis. The
  seam (`internal/molang`) and the graph's sacred-string policy are ready.
- **Texture atlas packing + UV rewrite**: the big one. Needs geometry bone
  UV extraction + json-ui cooperation; tracked here so the design
  constraint (graph-driven, validator-proven) is not rediscovered the
  hard way.
- **JSON transform caching** (the PNG and audio passes are cached today;
  JSON minify is cheap enough that it was not worth the complexity).
- **Behavior packs**: the graph model extends naturally, but BP-side
  semantics (events, Molang in BP entities) need their own fixture suite.
- **Streaming pack mode** for 1 GB+ packs (current pipeline is in-memory;
  vanilla-adjacent packs are hundreds of MB).

## Non-goals

- Marketplace-style encryption or DRM theater: assets always end up on the
  client. See the README FAQ ("Is this DRM?").
- Zip-structure exploit tricks (fragile against engine updates, deliberately
  avoided).
