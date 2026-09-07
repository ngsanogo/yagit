---
name: pull-request
description: Prepare yagit pull requests with correct gates, scope, and description. Use when opening or reviewing PRs.
---

# Pull requests

## Scope

One idea per pull request. A refactor and a feature in the same diff cannot be
reviewed — only trusted.

## Gates (required)

```sh
./do lint
./do test
```

Both must pass. `./do audit` is optional — not a merge gate.

## Title and body

- Title: what changed (Conventional Commits style)
- Body: why, and what would have gone wrong otherwise

## Checklist

- [ ] `./do lint` passes
- [ ] `./do test` passes
- [ ] If `agent/` changed: `./do agent sync` and `./do agent check`
- [ ] One logical change
- [ ] English throughout
- [ ] No secrets (.env, tokens) in the diff

## Review criteria

From [docs/ARCHITECTURE.md](../../../docs/ARCHITECTURE.md):

1. Comments explain why, not what
2. Errors never pass silently
3. One obvious way — no duplicate UI paths or config values
4. Dependencies point one way (web → api → … → git)

## Releases

Merging to `main` triggers release when commits warrant a version bump. Nobody
tags manually. Release notes come from merged PR titles and bodies.
