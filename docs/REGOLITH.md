# Regolith filter integration

`shulker` runs as a Regolith **exe filter**. This document records the
verified contract, the live test evidence, and the exact project layout —
no guessing, per the project rules.

## The contract (verified against Regolith 1.8.0 source)

From `Bedrock-OSS/regolith` `regolith/filter_exe.go` and `regolith/utils.go`:

1. **Invocation**: `<FILTER_DIR>/<exe> [settingsJSON ...staticArgs]`. The
   settings JSON blob is passed **only when the profile filter entry has
   non-empty `settings`**; with empty settings the binary is invoked with
   **zero arguments**.
2. **Environment**: `FILTER_DIR` (the filter's location) and `ROOT_DIR`
   (the project root) are always exported into the filter's environment.
3. **Working directory**: the Regolith working dir (`.regolith/tmp` by
   default; the `tmp_dir` user config can move it). The compiled resource
   pack is staged at **`./RP`** (behavior pack at `./BP`, filter data at
   `./data`).
4. **Stdout/stderr** are relayed into the Regolith log, labeled with the
   filter name: `[shulker] ...`.
5. The `exe` path is `filepath.Join(FILTER_DIR, exe)`; for filters defined
   directly in `config.json`, `FILTER_DIR` resolves to the project root in
   practice — verified live (see below), so `"exe": "shulker.exe"` with the
   binary in the project root works.
6. `config.json` (1.8 schema) requires: `name`, `author`, `packs`
   (`behaviorPack`/`resourcePack`), and a `regolith` object containing
   `formatVersion` (semver, ≤ the Regolith version), `dataPath`, optional
   `filterDefinitions`, and `profiles`. Note `"regolith": "1.0.0"` (the old
   string form) and a missing `dataPath` are hard config errors in 1.8.
7. Every profile needs an `export` section; `{"target": "none"}` skips
   export (useful for CI-style verification).

## How shulker honors it

- `shulker regolith` (and auto-detection) squashes `./RP` **in place**:
  output files are written back, renamed-away paths are deleted, and the rest
  of the Regolith chain (further filters, export) sees the optimized pack.
- Invocation detection: the `regolith` marker argument, the
  `FILTER_DIR`+`ROOT_DIR` environment pair (the reliable zero-argument
  signal), or a leading JSON object in argv.
- Settings JSON maps onto squash flags: `no-rename`,
  `no-rename-textures`, `no-rename-sounds`, `no-rename-identdirs`,
  `no-minify`, `no-png`, `no-cache`, `workers`, plus the v0.2 opt-ins:
  `png-optimize-overrides`, `png-quantize`, `png-quantize-colors`,
  `audio-ffmpeg`, `audio-quality`, `squash-subpacks`.

## Project layout (tested)

```
my-project/
├── config.json
├── shulker.exe          <- from `go build -o shulker.exe ./cmd/shulker`
├── RP/                  <- your resource pack source (manifest.json at root)
├── BP/                  <- behavior pack (untouched by shulker)
└── data/
```

```jsonc
// config.json
{
  "name": "my-project",
  "author": "you",
  "packs": { "behaviorPack": "BP", "resourcePack": "RP" },
  "regolith": {
    "formatVersion": "1.2.0",
    "dataPath": "data",
    "filterDefinitions": {
      "shulker": { "runWith": "exe", "exe": "shulker.exe" }
    },
    "profiles": {
      "default": {
        "filters": [ { "filter": "shulker", "settings": {} } ],
        "export": { "target": "none" }
      }
    }
  }
}
```

Then: `regolith run default`.

## Live verification log (Regolith 1.8.0, Windows x64)

Run on 2026-10-16 against this repository's `rich` fixture staged as `RP/`:

- `regolith run default` → exit 0, `Successfully ran the "default" profile`.
- Filter output relayed with `[shulker]` labels, size table intact:
  `TOTAL 13.7 KiB → 10.1 KiB (26.3%)`, `renamed 23 files, rewrote 30
  references`.
- `.regolith/tmp/RP/entity/_0.entity.json` present,
  `entity/shulker_mob.entity.json` gone (in-place rename worked through the
  full Regolith pipeline).
- Blocked textures (`texset_ref*`, `ui_panel.png`) correctly untouched in the
  staged pack.
- With `"settings": {"no-rename": true}`: `renamed 0 files, rewrote 0
  references`, original file names retained — the settings blob path is
  honored.
- With `"settings": {}` Regolith invokes the binary with **no arguments**;
  shulker detects the filter context via `FILTER_DIR`/`ROOT_DIR`.

## Testing without Regolith

`cmd/shulker/cli_test.go` reproduces the invocation shapes Regolith uses
(zero-arg + env, settings blob + marker) so the contract is covered by CI
even where Regolith is not installed.
