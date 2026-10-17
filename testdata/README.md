# Benchmarks & golden fixtures

## testdata/gen

Regenerates every binary fixture and the benchmark packs deterministically
(fixed colors, no timestamps, no randomness):

```
go run testdata/gen/main.go testdata/gen/bench.go
```

Text fixtures are hand-authored in the repository; the generator only writes
bytes that cannot be reviewable text (PNGs, TGA, OGG stubs, the BOM'd
item_texture.json) plus the full benchmark packs.

## tools/vanilla-gen

Freezes the vanilla path universe into `internal/vanilla/vanilla_paths.gz`
from a blob-filtered, JSON-sparse checkout of
[MCBVanillaResourcePack](https://github.com/ZtechNetwork/MCBVanillaResourcePack)
(exact procedure in the tool's doc comment). Regenerate after major Bedrock
releases; staleness only over-protects.

## testdata/packs

- `rich/` — the golden pack. Covers every file type shulker models: two
  suffix-PBR sets (one with a mixed `.png`+`.tga` extension set), a
  `*.texture_set.json` blocker case, flipbook, json-ui (with blocked + free +
  vanilla texture refs), blocks.json → terrain short-name chains (including a
  vanilla `stone` override and a custom-under-vanilla-directory texture),
  legacy AND modern geometry, attachable, animation with Molang + keyframed
  effects, render controllers with both map-key and literal-path texture
  arrays, sounds (prefixed paths, float noise, vanilla def references),
  textures_list.json, an unknown-schema file that blocks a texture on
  purpose, deny-listed vanilla overrides, a subpack that is itself a
  complete mini-pack, lang with comments, and JSON with
  comments/trailing commas/BOM.
- `broken/` — seeded defects the validator must flag: unparseable JSON, a
  dangling texture reference, a dangling geometry identifier, a
  languages.json entry with no `.lang` file.
- `minimal/` — smallest valid pack.

## testdata/benchmarks

- `json_heavy/` — author-formatted JSON everywhere (comments, 4-space
  indentation, trailing commas, BOM, float noise). Squashes **29.3%**.
- `png_heavy/` — 24 BestSpeed-encoded 128×128 sprites referenced from the
  terrain atlas. Squashes **23.0%** with no quantization.
- `ui_palette/` — 16 low-color UI-style sprites (binary alpha, few distinct
  colors) referenced from the terrain atlas. Squashes **63.3%** via the
  v0.2 lossless palette conversion, pixel-identical.

`TestBenchmarkFixtureReductions` (internal/pipeline/bench_test.go) prints the
tables and asserts ≥25% / ≥10% / ≥25% on every CI run, so the README numbers
cannot silently rot.
