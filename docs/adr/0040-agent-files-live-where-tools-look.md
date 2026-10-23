# 0040 — Agent files live where tools look

**Status:** reverses the link half of
[0037](0037-the-repository-ships-the-open-agent-format-only.md).

## The problem

0037 was right to drop the generated per-tool directories. What it left was
two names for one tree: the files under `agent/`, and the links `AGENTS.md`
and `.agents/skills` that tools open. Keeping the second name honest took a
command (`./do agent sync` and `check`), a copy fallback for a Windows
checkout that cannot create a symlink, a CI job that exists only to run that
fallback, and a rule that says not to edit the file a tool just opened.

A link that has to be explained, checked and rebuilt is a second copy wearing
the first's name.

## Decision

The file a tool opens is the file.

- `AGENTS.md` at the repository root is the entry point.
- Rules are `.agents/rules/`. Skills are `.agents/skills/`.
- Nothing links, nothing copies, and `./do` has no `agent` command.

## What it costs

- A tool that looks somewhere else still needs a person to point it at these
  files. That pointer stays out of the repository, as 0037 already decided.
- There is no check that a published name matches a hidden source. There is
  nothing to match.

## What was decided against

- **Keeping `agent/` and the links.** The links are the part that had to be
  explained, and the command existed only to maintain them.
- **Folding the rules into `AGENTS.md`.** [0013](0013-agent-config-is-tool-agnostic.md)
  was right that Go, the web UI and the tests are different concerns. They
  stay separate files, next to the skills.

## References

- [0037 — The repository ships the open agent format only](0037-the-repository-ships-the-open-agent-format-only.md)
- [0013 — Agent configuration is tool-agnostic](0013-agent-config-is-tool-agnostic.md)
- [AGENTS.md](../../AGENTS.md)
