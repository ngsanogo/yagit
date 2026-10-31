# yagit — agent instructions

## Project

yagit is a graphical Git client: a local Go daemon serves a React web UI on
the loopback. The commit graph is the central screen element. Current status:
**phase 12 complete** — the deferred surface after the planned phases (remote
URL editing; set and unset upstream; NDJSON progress on fetch/pull/push; a
workbench theme toggle; open tabs remembered across reloads; a merge-commit
preference where a fast-forward is possible). Roadmap:
[docs/ROADMAP.md](docs/ROADMAP.md).

Stack: Go daemon + embedded frontend, React/TypeScript/Vite, TanStack Query,
SVG commit graph assigned in the daemon, one server-sent event stream per
session. Full architecture:
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Commands

Every project action is a mise task: `./do <task>` (or `mise run <task>`;
`do.cmd` on Windows). `./do` alone lists them; the full table is in
[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md#tasks).

| Command | Effect |
| --- | --- |
| `./do up` / `down` / `status` / `logs` | The stack (daemon + Vite) in the background; `up` prints the URL and token |
| `./do dev` | The stack in the foreground |
| `./do build` | Frontend, then binaries for every platform into `dist/` |
| `./do test [go\|web\|e2e\|release\|coverage\|bench]` | Tests; no argument runs go, web and e2e |
| `./do lint` | Go on every platform, frontend, shell, CI workflows |
| `./do audit` | Dependency vulnerability scan (not a merge gate) |
| `./do fmt` | Reformat code |
| `./do shot [url] [out]` / `./do drive [url]` | Screenshot / drive the running interface, headless |
| `./do token [--new]` | Print (or replace) the session token |
| `./do version` | Next release version from the Conventional Commits |

Prerequisite: [mise](https://mise.jdx.dev), which installs the toolchain pinned
in `mise.toml` / `mise.lock`.

## Gates

Before a pull request is ready:

1. `./do lint` must pass
2. `./do test` must pass

`./do audit` is informative only — it queries live vulnerability databases.

## Paths an agent does not read

The `.ignore` file at the repository root lists them, in the same syntax as
`.gitignore`. It covers secrets, this machine's session, build output, and
generated files that are tracked but not worth reading. Respect it even when
a search tool does not.

## Architecture

Dependencies point one way only:

```
web → api → history → graph, repo → git
```

| Package | Role |
| --- | --- |
| `cmd/yagit` | Daemon wiring: config, token, lifecycle |
| `internal/git` | Run `git`, parse machine-readable output |
| `internal/repo` | Open repos, security boundary (`YAGIT_ROOT`) |
| `internal/watch` | fsnotify over each repository's git directories |
| `internal/graph` | Lane assignment — pure, tested |
| `internal/history` | Assigned history held per open repo |
| `internal/edit` | Work-tree files, read and written — no git |
| `internal/api` | HTTP routes, auth, JSON errors |
| `internal/session` | The session token: minted and checked in one place |
| `internal/publicurl` | A reverse proxy's address (`YAGIT_PUBLIC_URL`), checked and read as the origin a browser presents |
| `internal/protect` | Owner-only lockdown for secrets — chmod on Unix, an ACL on Windows |
| `web/src/design` | Design tokens — single source of visual values |
| `web/src/app` | Workbench UI |

Ports: `7420` for the daemon, `7421` for Vite (`mise.toml`'s `dev:*` tasks and `web/playwright.config.ts`).

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
- **Explain why in place.** A choice that is not obvious from the code gets a
  comment beside it, not a separate record.

Always read [.agents/rules/core.md](.agents/rules/core.md). Read the others when the
change touches that part of the tree:

| When the change touches | Read |
| --- | --- |
| Go (`cmd/`, `internal/`, `*.go`) | [.agents/rules/go.md](.agents/rules/go.md) |
| the web UI (`web/`) | [.agents/rules/web.md](.agents/rules/web.md) |
| tests | [.agents/rules/testing.md](.agents/rules/testing.md) |

Workflows for one task — committing, opening a pull request, running the app —
live in [.agents/skills/](.agents/skills/).

## Commits and pull requests

Conventional Commits:

```
feat(scope): subject
fix(scope): subject
docs: subject
```

Subject states what changed; body states why. One idea per pull request.

Run `./do version` before merging to see what version the branch produces.

Human-oriented workflow: [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

- Daemon binds loopback by default (`127.0.0.1:7420`). Behind a reverse proxy
  it stays there: `YAGIT_PUBLIC_URL` in `.env` names the proxy's address, and
  is refused beside the settings that widen the listen address.
- Session token required on every API route. Under `./do` it lives in
  `.yagit/session-token`, outlives the stack, and `./do token --new`
  replaces it. Whether a stack is up is asked of the port, never of a file.
- Repositories opened by path once; afterwards addressed by opaque id.
- `git` invoked with argument arrays — never through a shell. The one string a
  shell reads is `GIT_SEQUENCE_EDITOR`, which git defines as a shell command
  and runs itself; yagit quotes its parts and puts nothing client-supplied in
  it.
- See [SECURITY.md](SECURITY.md).

## Language

English everywhere: code, comments, documentation, commit messages, UI strings.

## Naming other software

Tracked files name no product and quote no source that is not freely reusable
without conditions. Reasoning that came from reading another client stays —
what goes is the name. Write what is true of the field ("established clients
fetch on a timer, and it is why their counts are stale") rather than who does
it.

## Do not

- Hard-code ports or tool versions outside their single source files
- Add `_ = err` or empty error handlers without a comment explaining why
- Add comments that restate the code below them
