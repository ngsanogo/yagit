# 0037 — The repository ships the open agent format only

**Status:** reverses the generated-directory half of
[0013](0013-agent-config-is-tool-agnostic.md) and reverses
[0019](0019-the-manifest-carries-what-agents-may-not-read.md).

## The problem

0013 put instructions in `agent/` so they would not be copied per tool, then
generated a directory per tool so each one would find them. 0019 rendered the
list of paths an agent must not read into one tool's settings file, because
that was the only place a denial was enforced.

The repository is public. A generated directory that exists only so one product
discovers configuration is an advertisement for that product, and a personal
override beside it is how a checkout's own notes leak into a commit. Anyone
can read `agent/` and `AGENTS.md`. A second tree that translates the same words
into one product's frontmatter does not make the instructions more true, and
it names the product in every file the generator writes.

## Decision

The published contract is the open one.

- Instructions, rules and skills stay under `agent/`. Nothing there is written
  for a particular tool.
- `AGENTS.md` at the repository root is a link to `agent/AGENTS.md`.
- `.agents/skills` is a link to `agent/skills`, the directory an agent looks in
  for skills.
- `./do agent sync` refreshes those two links. `./do agent check` verifies
  them. Where a checkout cannot create a symlink, sync writes a copy and check
  compares content, as before.
- Paths an agent should not read are `.ignore`, in gitignore syntax, which an
  indexer already understands. `.gitignore` continues to decide what git
  stores. The two overlap on purpose: `.ignore` also names tracked generated
  files.
- Personal notes for one checkout match `*.local.md`, which `.gitignore` and
  `.ignore` both list. They are not a way to configure the project.

Which rule applies to which files is a table in `AGENTS.md`. An agent that can
read the repository can read the table. Path-scoped frontmatter existed only
to feed the generated directories, so it left with them.

## What it costs

- A tool that does not read `AGENTS.md` or `.agents/skills` does not pick the
  instructions up by itself. A person using that tool can point it at those
  files from their own checkout. That pointer is not part of this repository.
- On a checkout that copied `.agents/skills` instead of linking it, a new or
  edited skill is invisible until `./do agent sync`. `./do agent check` fails
  until then. A symlink does not have this lag.
- Nothing in the repository denies a read the way a tool's own permission file
  can. `.ignore` is a request to indexers and a sentence in `AGENTS.md`. A
  tool that ignores both can still open `.env`. `.env` is gitignored and never
  committed; the session token lives under `.yagit/`, which is too.

## What was decided against

- **Keeping the generated directories and simply not mentioning them.** The
  directories are the advertisement. A public tree that contains them is still
  arranged around two products.
- **A Makefile, or any second command runner, beside `./do`.** The command an
  agent runs is already `./do`, documented in `AGENTS.md`, and it is the only
  path CI verifies. A second entry point is a second way to drift.
- **Flattening every rule into one `AGENTS.md`.** 0013 was right that the
  conventions are layered. The file points at the layers; it does not swallow
  them.
- **Generating `.ignore` from a manifest.** The file is short, the comments
  are the reason each path is there, and a generator would be a second source
  wearing the first's clothes. `.gitignore` is maintained the same way.

## References

- [0013 — Agent configuration is tool-agnostic](0013-agent-config-is-tool-agnostic.md)
- [0019 — The manifest carries what agents may not read](0019-the-manifest-carries-what-agents-may-not-read.md)
- [agent/README.md](../../agent/README.md)
- [AGENTS.md open format](https://agents.md/)
- [Agent Skills](https://agentskills.io/specification)
