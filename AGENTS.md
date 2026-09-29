# AGENTS.md — plugin-punktfunk

Standalone plugin repo for the `punktfunk` check verb (`verb:punktfunk`). The
plugin is a Go module at `candy/plugin-punktfunk/` (module path
`github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk`); the root
`charly.yml` declares `discover: candy` **and** the `punktfunk-skill` `skill:`
entity (the corpus source for `/charly-check:punktfunk`).

Canonical files:

- `candy/plugin-punktfunk/charly.yml` — the `plugin-punktfunk:` candy entity
  (`plugin:` block, `plan:` check) + the `punktfunk-skill` skill entity.
- `candy/plugin-punktfunk/methods.go` — the host + client method surface.
- `candy/plugin-punktfunk/transport.go` / `token.go` / `parse.go` — the
  in-venue transport, token discovery, and response parsing.
- `candy/plugin-punktfunk/schema/punktfunk.cue` — the self-contained schema.
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.
- `README.md` — user overview only; never agent guidance.

## Load these skills first (R0)

- `/charly-internals:plugin` — the plugin authoring reference: the `plugin:`
  block, the unified Provider model, the out-of-process shape, the per-plugin
  CUE-schema contract, placement. Load before touching the provider or schema.
- `/charly-check:punktfunk` — the `punktfunk:` verb this candy serves.
- `/charly-check:check` — the check orchestrator, beds and the R10 sequence.
- `/charly-internals:git-workflow` — before any git/PR action.

## Build / validate / test

- `go build ./...` in `candy/plugin-punktfunk/` — compile the plugin module.
- `go test ./...` in `candy/plugin-punktfunk/` — the plugin's Go tests
  (`cli_test.go`, `plugin_test.go`, `token_test.go`, `transport_test.go`,
  `pin_out_test.go`, `bodyless_write_test.go`).
- `charly box validate` at the repo root — the structural check (the candy +
  `plugin:` block, CUE schema, the skill entity).
- The merge gate is the **org-wide** `charly/pr-validator` (required check
  `validate / validate`, defined in `opencharly/.github`); this repo has **no**
  per-repo candy gate.
- R10 consumer: a punktfunk-bearing bed (pod or VM) whose check composes this
  plugin alongside the `punktfunk` candy.

## Modify this repo

- Edit the `plugin-punktfunk:` candy entity, the Go source, and
  `schema/punktfunk.cue` **together** — the schema is the single source for the
  `params/` struct, so a field change not mirrored in the schema desyncs the
  generated types.
- The `skill:` entity is the corpus source for `/charly-check:punktfunk`; a
  change to the verb surface belongs in BOTH the candy and the skill body.
- The plugin is **out-of-process**; the verb issues its request in-venue over the
  reverse channel (a loopback-bound listener is unreachable host-side).

## Landing

- PR-only. Every change lands through a pull request; the org-required
  `charly/pr-validator` validates the diff and body and arms native auto-merge on
  PASS. Direct pushes to `main` are blocked.
- History lives in `CHANGELOG/` (written by `tag-on-merge` at merge time); the PR
  body IS the changelog.
- The authoritative rulebook is the umbrella `AGENTS.md` in
  `opencharly/opencharly` and `charly/AGENTS.md` in the charly repo. Do not
  restate its rules here.
