# 0019 — The manifest carries what agents may not read

**Status:** completes 0013.

## The problem

An AI coding agent reads the checkout the way a contributor does, and the
checkout holds three kinds of file it has no business opening. `.env` names
`YAGIT_ROOT`, the boundary the daemon refuses to open outside of. `.yagit/`
holds the session token and a stack log tracing every git command the daemon
ran and every repository path it touched. And `web/node_modules`, `dist/` and
`internal/assets/dist` are 276 MB that `./do build` reproduces from source.

Claude Code expresses that as deny rules in a `settings.json`, which it reads
from `.claude/`. In this repository `.claude` is a symlink to `claude/`, a
directory [0013](0013-agent-config-is-tool-agnostic.md) generates and whose
README says not to edit by hand. Writing the file there directly works —
`agent sync` does not clear entries it did not create, and `agent check` does
not look for them — and it makes the rule on the directory false, which is how
the next contributor stops trusting any of it.

0013 left the question open: *"tool-specific permission files are not in scope
yet and would live in the shims only if added later."*

## Decision

The list lives in `agent/manifest.yaml`, as repository-relative paths with a
comment saying why each is denied. `./do agent sync` renders it into
`claude/settings.json`, and `./do agent check` compares that file byte for
byte like every other generated one.

The manifest stays tool-agnostic, exactly as it is for rule scopes: it holds
`dist/**`, and the renderer knows that Claude Code writes that as
`Read(/dist/**)`.

The leading slash is load-bearing. In a project settings file it anchors the
pattern at the working directory; without it the pattern is a gitignore-style
name, and a *deny* rule matches such a name at any depth. `dist/**` alone would
also cover `internal/assets/dist` and every `dist` inside `node_modules` — a
wider rule than the manifest asks for, and not one its reader would predict.

## What it costs

- A third section in a hand-written YAML parser. It reuses the shape `skills:`
  already has, so the cost is one branch and a shared helper for reading an
  entry.
- A manifest with no `deny:` section is now refused. An empty list would render
  a valid settings file that grants everything, and read exactly like one that
  was never meant to deny anything.
- Cursor gets no counterpart. Its settings schema is not something this project
  has established, and an invented one that silently matches nothing is worse
  than none — the shim would claim a protection it does not have.

## What was decided against

- **A hand-written `claude/settings.json`.** Two sources of truth, in the one
  directory whose entire purpose is to have none, and a "do not edit by hand"
  notice that is no longer true.
- **`claude/settings.local.json`.** Already gitignored, so it needs no
  machinery at all — and protects one machine. The paths denied here are
  properties of the repository, not of anyone's checkout.
- **Enforcing it in the sandbox instead.** Deny rules cover Claude's own file
  tools and the file commands it recognises in a shell, not a subprocess that
  opens a path itself. That is the right boundary here: `./do` must keep
  reading `package-lock.json` and writing `dist/`. A rule that stopped it would
  be protecting the wrong thing.

## References

- [0013 — Agent configuration is tool-agnostic](0013-agent-config-is-tool-agnostic.md)
- [agent/README.md](../../agent/README.md)
- [Claude Code permission rules](https://code.claude.com/docs/en/permissions)
