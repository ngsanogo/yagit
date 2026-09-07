# yagit — agent instructions

## Project

yagit is a graphical Git client: a local Go daemon serves a React web UI on
the loopback. The commit graph is the central screen element. Current status:
**phase 12 complete** — the deferred surface after the planned phases (remote
URL editing; set and unset upstream; NDJSON progress on fetch/pull/push; a
workbench theme toggle; open tabs remembered across reloads; a merge-commit
preference where a fast-forward is possible). Roadmap:
[docs/ROADMAP.md](../docs/ROADMAP.md).

Stack: Go daemon + embedded frontend, React/TypeScript/Vite, TanStack Query,
SVG commit graph assigned in the daemon, one server-sent event stream per
session. Full architecture:
[docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md).

## Commands

Every project action goes through `./do`. Do not run `go test`, `npm run`, or
other toolchain commands directly — CI only verifies `./do` paths.

| Command | Effect |
| --- | --- |
| `./do up [--restart] [--foreground] [--new-token]` | Start stack in background; print URL and token. `--new-token` replaces the token and restarts on it |
| `./do down` | Stop the background stack |
| `./do status` | Report whether the stack is running |
| `./do logs [--no-follow]` | Show output from the background stack |
| `./do dev [--new-token]` | Run stack in foreground with logs in the terminal; stops a background stack first |
| `./do bootstrap [--browsers]` | Install dependencies into `.yagit/` and `web/node_modules/` |
| `./do shell-hook [--write]` | Generate shell aliases for up, down and logs |
| `./do build` | Build frontend and binaries into `dist/` |
| `./do test [go\|web\|e2e\|release\|bench\|fuzz\|soak\|coverage]` | Run tests; no argument runs go, web, and e2e |
| `./do lint` | Static analysis — Go on every platform, frontend, shell (`./do` and `scripts/*.sh`), CI workflows |
| `./do audit` | Dependency vulnerability scan (not a merge gate) |
| `./do fmt` | Reformat code |
| `./do shot [url] [out]` | Screenshot a page via Playwright |
| `./do drive [url]` | Drive the running interface from stdin, in a headless browser |
| `./do token` | Print session token for API calls, once the stack answers it |
| `./do version` | Print release version from Conventional Commits since last tag |
| `./do agent sync\|check` | Regenerate tool shims from `agent/`, or verify they match (CI gate) |

Every command takes `--help` and prints its usage without doing anything else.

Prerequisite: [mise](https://mise.jdx.dev) — installs pinned toolchain from
`mise.toml` / `mise.lock`. The `./do` shim points mise, Go, npm and Playwright
at directories under `.yagit/`, so everything the checkout downloads lives
inside it and `rm -rf` on the clone removes all of it.

## Gates

Before a pull request is ready:

1. `./do lint` must pass
2. `./do test` must pass

`./do audit` is informative only — it queries live vulnerability databases.

## Architecture

Dependencies point one way only:

```
web → api → history → graph, repo → git
```

| Package | Role |
| --- | --- |
| `cmd/do` | Project entry point — every command |
| `cmd/yagit` | Daemon wiring: config, token, lifecycle |
| `internal/git` | Run `git`, parse machine-readable output |
| `internal/repo` | Open repos, security boundary (`YAGIT_ROOT`) |
| `internal/watch` | fsnotify over each repository's git directories |
| `internal/graph` | Lane assignment — pure, tested |
| `internal/history` | Assigned history held per open repo |
| `internal/edit` | Work-tree files, read and written — no git |
| `internal/api` | HTTP routes, auth, JSON errors |
| `internal/session` | The session token: minted and checked in one place, for the daemon and for `./do` |
| `internal/protect` | Owner-only lockdown for secrets — chmod on Unix, an ACL on Windows |
| `web/src/design` | Design tokens — single source of visual values |
| `web/src/app` | Workbench UI |

Ports are defined once in `cmd/do/main.go` (`7420` daemon, `5173` Vite).

## Principles

These are review rules, not decoration:

- **One obvious way.** One command path (`./do`), one definition per value.
- **Errors never pass silently.** No empty `catch`, no `_ = err`. Git failures
  show command, exit code, and raw stderr.
- **Comments explain why, not what.**
- **Explicit over implicit.** Destructive git operations show the exact command
  and name what will be lost.
- **Design tokens only.** No magic CSS values in components — use
  `web/src/design/tokens.css`.
- **ADRs are historical context**, not blocking constraints. See
  [docs/adr/](../docs/adr/README.md). Reversing a decision means a new ADR, not
  permission from the old one.

Detailed conventions: [agent/rules/](rules/).

## Commits and pull requests

Conventional Commits:

```
feat(scope): subject
fix(scope): subject
docs: subject
```

Subject states what changed; body states why. One idea per pull request.

Run `./do version` before merging to see what version the branch produces.

Human-oriented workflow: [CONTRIBUTING.md](../CONTRIBUTING.md).

## Security

- Daemon binds loopback by default (`127.0.0.1:7420`).
- Session token required on every API route. Under `./do` it lives in
  `.yagit/session-token`, outlives the stack, and `./do up --new-token`
  replaces it. Whether a stack is up is asked of the port, never of a file.
- Repositories opened by path once; afterwards addressed by opaque id.
- `git` invoked with argument arrays — never through a shell. The one string a
  shell reads is `GIT_SEQUENCE_EDITOR`, which git defines as a shell command
  and runs itself; yagit quotes its parts and puts nothing client-supplied in
  it ([ADR 0029](../docs/adr/0029-a-rebase-plan-is-written-not-edited.md)).
- See [SECURITY.md](../SECURITY.md).

## Language

English everywhere: code, comments, documentation, commit messages, UI strings.

## Naming other software

Tracked files name no product and quote no source that is not freely reusable
without conditions. Reasoning that came from reading another client stays —
what goes is the name. Write what is true of the field ("established clients
fetch on a timer, and it is why their counts are stale") rather than who does
it.

## Do not

- Run `go test`, `npm run`, or `golangci-lint` directly — use `./do`
- Hard-code ports or tool versions outside their single source files
- Add `_ = err` or empty error handlers without a comment explaining why
- Edit `cursor/` or `claude/` directly — edit `agent/` and run `./do agent sync`
- Treat ADRs as immutable law
- Add comments that restate the code below them
