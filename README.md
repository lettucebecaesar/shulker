# shulker

> **Shrinks, speeds up, and scrambles Bedrock resource packs — squashing is the product, obfuscation falls out of it.**

`shulker` is a production-quality optimizer for Minecraft Bedrock Edition (MCBE) resource
packs. Bedrock servers ship the entire resource pack to every client on join, unencrypted —
so pack size is join time. `shulker` rewrites packs so they download faster, load faster,
and are harder to casually rip, while guaranteeing the pack loads **exactly the same** in
the game.

**Status: v0.1.0 in development.** See [docs/ROADMAP.md](docs/ROADMAP.md) for the plan and
progress.

## Design ethos

Every transform must either **shrink** or **scramble**, and the pack must load exactly the
same afterward. When in doubt, `shulker` skips a transform and logs a warning.
Conservatism beats compression ratio. The brand is "stable."

See the [hard invariants](docs/INVARIANTS.md) — never corrupt a pack, never touch
`manifest.json`, vanilla-override protection, PBR lockstep, Molang literals are sacred,
byte-deterministic output.

## License

MIT — see [LICENSE](LICENSE).
