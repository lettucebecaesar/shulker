# Hard invariants

These are the rules `shulker` never violates. Every transform is judged against them, and
any uncertainty resolves to "skip the transform and log a warning."

1. **Never corrupt a pack.** On anything unparseable or ambiguous: fail loudly with the
   file path + JSON pointer context. No silent guessing, no best-effort corruption.
2. **Never rename or modify:**
   - `manifest.json` — including UUIDs, versions, and `min_engine_version`. Packs may be
     UUID-linked to behavior packs or other packs. The manifest is stored verbatim.
   - `pack_icon.png`.
   - Any `minecraft:` or `minecon:` namespaced identifier.
   - Client entity `identifier` fields — they are coupled to behavior packs we cannot see.
   - Lang keys.
3. **Vanilla-override protection.** If a file sits at a vanilla path (overriding vanilla
   content the engine resolves directly — e.g. files under `textures/blocks/` referenced
   by vanilla-side JSON that is not inside this pack), it must not be renamed. General
   rule: a file may be renamed only if 100% of its inbound references live inside this
   pack **and** every one of them gets rewritten.
4. **PBR lockstep.** Textures travel in sets — a color map plus conventional companions
   (`_mer`, `_normal`, `_heightmap`, `_mers`). Sets rename, optimize, and report in
   lockstep, or the pack renders wrong with no error.
5. **Molang string literals are sacred in v0.1.** Nothing inside Molang expressions is
   altered. v0.1 builds the tokenizer seam only, and uses it to know what *not* to touch.
6. **Deterministic output.** Same input + config = byte-identical output. Fixed zip
   timestamps, sorted iteration everywhere, no wall-clock or machine-specific data in
   artifacts.
7. **Preserve JSON key order** and tolerate `//` comments, trailing commas, and UTF-8
   BOMs — vanilla and community files routinely contain all three.

## v0.2 rename rules (how invariant 3 is implemented)

A file is rename-eligible only when **all** of the following hold:

- It is a texture, sound file, or a file in an identifier-scanned directory
  (`entity/`, `attachables/`, `animations/`, `animation_controllers/`,
  `render_controllers/`, `particles/`, `models/`), where the engine looks content up by
  identifier rather than by path.
- For path-referenced assets (textures, sounds): the file has at least one pack-internal
  inbound reference, every such reference is in a position we know how to rewrite, and
  **zero** references appear in unknown-schema positions (unknown JSON files referencing
  an asset block that asset's rename — by design).
- Its path does not match the **frozen vanilla path list** (embedded from
  [ZtechNetwork/MCBVanillaResourcePack](https://github.com/ZtechNetwork/MCBVanillaResourcePack),
  a complete mirror of the shipped vanilla resource pack — file paths plus every
  texture/sound reference target extracted by shulker's own graph parsers). Matching is
  exact and extension-insensitive, so a pack's `.tga` shadowing vanilla's `.png` at the
  same reference target is protected too. **Consequence:** genuine overrides like
  `textures/blocks/stone.png` stay put, while custom assets that merely live under
  vanilla directories (`textures/blocks/my_block.png`) rename freely — vanilla cannot
  reference a path it does not have.
- It is not under an engine-hardcoded prefix (`textures/environment/`,
  `textures/colormap/`, `textures/misc/`, `font/`, `texts/`, `ui/`, `subpacks/`) —
  fixed engine lookups no JSON reveals.
- Its whole PBR texture set (if any) passes the same checks.

Identifier-scanned files are renamed *within their directory* with their conventional
suffix preserved (`.geo.json` stays `.geo.json`, etc.), so any directory glob the engine
uses keeps matching — and they are exempt from the vanilla-path check because the engine
resolves them by identifier, never by path.

**Residual risk, stated honestly:** the frozen list is a snapshot. Vanilla adds paths
every release; a pack file at a *new* vanilla path (with pack-internal references) could
in principle be renamed. Staleness in the other direction only over-protects. Regenerate
after major Bedrock releases with `go run ./tools/vanilla-gen` (procedure in the tool
header); the provenance commit is embedded in the artifact and shown by the frozen list
itself.
