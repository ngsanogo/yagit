# Agent configuration

Everything an AI coding agent needs to work in this repository lives here.
Tool-specific directories at the repository root — `cursor/` and `claude/` — are
**generated shims** that delegate here. Do not edit them by hand; run
`./do agent sync` after changing anything under `agent/`.

## Layout

```
agent/
  AGENTS.md         entry point (also linked as AGENTS.md at the repo root)
  manifest.yaml     rule scopes, skill list, denied paths — drives ./do agent sync
  rules/            modular instructions, one concern per file
  skills/           workflow skills (SKILL.md per directory)
```

## Contributing

1. Edit files under `agent/` only.
2. Run `./do agent sync` to regenerate `cursor/` and `claude/`.
3. Run `./do agent check` — CI runs this in the lint gate.

Rules are plain Markdown without tool-specific frontmatter. The sync command
translates scopes into each tool's format.

Skills follow the [Agent Skills](https://agentskills.io) layout: a directory
with a `SKILL.md` file and optional supporting files.

## Denied paths

The `deny:` section of the manifest lists what no agent reads: this machine's
`.env` and `.yagit/`, and the build output `./do build` reproduces. Paths are
repository-relative and carry a comment saying why — never anchored with a
leading slash, which sync refuses: `/dist/**` would render as an absolute path
from the filesystem root and deny nothing. Sync renders each entry into
`claude/settings.json` as an anchored `Read()` rule; Cursor has no counterpart
because its settings schema is not one this project has established. See
[ADR 0019](../docs/adr/0019-the-manifest-carries-what-agents-may-not-read.md).

## Personal overrides

Local-only overrides belong outside version control:

- `CLAUDE.local.md` at the repository root, and `cursor/rules/local.mdc`
  beside it — one document in the two formats the two tools read
- `claude/settings.local.json` or `cursor/settings.local.json`

These paths are listed in `.gitignore`.
