# shulker — v0.1 scope

The v0.1 rename policy is deliberately conservative:

- Rename **actual file paths** (textures, sounds, geometry, animations, particles, UI
  textures) — and nothing else. Short names, identifiers, bone names, Molang, lang keys
  are all untouched.
- A file is renamed only if it passes the vanilla-override check and every inbound
  reference is pack-internal and gets rewritten.

## In scope (v0.1)

| Milestone | Scope | Status |
|-----------|-------|--------|
| M1 Pack I/O | `.mcpack`/zip/directory loading, manifest validation, deterministic canonical repack | in progress |
| M2 Reference graph | Parsers + bidirectional graph over every RP file type | pending |
| M4 Validator | Post-transform safety net, golden fixtures, check-only mode | pending |
| M3 Transforms | JSON minify, path renaming with Sourcemap-style mapping, PNG optimize, audio passthrough | pending |
| M5 CLI + Regolith + cache | `shulker squash/check/map`, size report, content-addressed cache, Regolith filter mode | pending |

## Out of scope (v0.1 — seams only, no builds)

- Molang variable/identifier renaming (tokenizer seam only; Molang string literals are
  sacred in v0.1).
- Texture atlas packing + UV rewrite.
- Audio re-encoding (engine requires Vorbis OGG; there is no good pure-Go encoder —
  strict passthrough with a stub hook for optional ffmpeg shell-out).
- Watermarking, behavior packs, marketplace-encryption, zip-structure exploit tricks.

## v0.2+ roadmap (written, not built)

- Bundle a frozen vanilla path deny-list (generated from `Mojang/bedrock-samples` at
  build time) so vanilla-override protection gets even tighter without changing behavior
  for custom assets.
- PNG quantization (pngquant-style) behind a `--png-quantize` flag with keep-if-smaller
  guards.
- Molang minification behind a real tokenizer.
- Texture atlas packing with UV rewrite (the big one — needs geometry + UI cooperation).
- Audio: optional ffmpeg shell-out hook behind `--audio-ffmpeg` (never required).
- `.mcaddon` / `.mctemplate` passthrough and behavior pack support.
- Parallel zip streaming and pack-size-aware memory mode for 1 GB+ packs.
