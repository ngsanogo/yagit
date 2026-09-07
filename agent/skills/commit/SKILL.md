---
name: commit
description: Write Conventional Commits for yagit and predict release version with ./do version. Use when creating commits or preparing merges to main.
---

# Commits

## Format

[Conventional Commits](https://www.conventionalcommits.org):

```
<type>(<scope>): <subject>

<body — why, not what>
```

Examples:

```
feat(graph): filter refs drawn in wide histories
fix(api): preserve Hijacker on the ResponseWriter wrapper
docs: agent configuration under agent/
test(e2e): full path through a real browser
```

## Types that move the version

| Type | Effect |
| --- | --- |
| `feat` | minor bump |
| `fix`, `perf` | patch bump |
| `!` or `BREAKING CHANGE:` footer | major (minor while &lt; 1.0) |

Types that move nothing: `docs`, `test`, `ci`, `build`, `chore`, `refactor`.

## Before merging

```sh
./do version
```

Shows the version this branch would release as. The commit message is both the
release note and the version input — write it accordingly.

## Language

Commit messages in English.

## Do not

- Commit unless explicitly asked
- Use `--no-verify` unless explicitly asked
- Amend pushed commits unless explicitly asked
