# 0013 — Agent configuration is tool-agnostic

**Status:** refined by [0037](0037-the-repository-ships-the-open-agent-format-only.md).
Instructions still live under `agent/`. The generated per-tool directories do
not.

## The problem

AI coding agents need project context: commands, conventions, architecture,
gates. Each tool ships its own configuration location — rules directories,
memory files, skills folders — and duplicating instructions across them means
one copy goes stale.

## Decision

All agent instructions live under `agent/` at the repository root. Nothing
tool-specific is written there.

A shim directory per tool is generated from `agent/` by `./do agent sync`.
Each one contains translated rule frontmatter and symlinks to skills. Links at
the repository root point into those shims so a tool discovers configuration
where it looks. `AGENTS.md` is one of those links.

`./do agent check` verifies the shims match `agent/`; it runs as part of
`./do lint`.

## What it costs

- Contributors who change agent instructions must run `./do agent sync` and
  commit the generated shims.
- Rule scopes are declared once in `agent/manifest.yaml` and translated into
  each tool's frontmatter format by the sync command — a small amount of
  machinery to avoid duplicating rule bodies.
- Personal overrides stay outside version control. Tool-specific permission
  files are not in scope yet and would live in the shims only if added later.

## What was decided against

- **Editing tool directories directly.** Two sources of truth; one goes stale.
- **A single root `AGENTS.md` with no structure.** Works for small projects;
  yagit already has layered conventions (Go, web, testing) worth splitting.
- **Tool-specific content in `agent/`.** Defeats the purpose; the canonical
  tree must read the same regardless of which agent loads it.

## References

- [agent/README.md](../../agent/README.md)
- [AGENTS.md open format](https://agents.md/)
