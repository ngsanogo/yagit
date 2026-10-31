# Development

How to run yagit from a checkout. End users who want a published binary should
use the one-line installer in the [README](../README.md).

## Requirements

[mise](https://mise.jdx.dev), and nothing else. It installs Go, Node, pnpm,
air, golangci-lint, shellcheck, actionlint, zizmor and govulncheck at the
versions pinned in [`mise.toml`](../mise.toml) (checksums in
[`mise.lock`](../mise.lock)), and runs the project's tasks.

```sh
curl https://mise.run | sh          # macOS, Linux
winget install jdx.mise             # Windows
```

And `git` itself, which mise does not install and should not: yagit drives the
one already on your machine, with your configuration, your hooks and your
credential helpers. Any
reasonably current version will do, and a handful of flags set the bar.

The floor is **2.31**, from March 2021. Opening a repository at all reads
`git rev-parse --path-format=absolute`, and that option is 2.31's. Under it
nothing works, and what git says is that the option is unknown — which reads
as though the repository were at fault rather than the git. `GET /api/health`
names the version it found and every operation that version is too old for,
so the answer is one request away rather than one confusing failure away.

Above the floor, two operations set their own bar. Everything else works on
2.31; a force push's `--force-if-includes` is 2.30's and so is already in.

**Rebase wants 2.38**, from October 2022, for `--no-update-refs` — without it
`rebase.updateRefs` force-moves branches the confirmation never named. An older git
refuses the flag by name rather than ignoring it, so that one operation fails
loudly and nothing else does.

**Worktrees want 2.36**, from March 2022, for `git worktree list --porcelain
-z`. This is the one place a missing flag is worked around rather than allowed
to fail: the worktree panel is drawn beside the references of every repository,
so refusing there would be a red panel on every screen rather than one
operation saying no. A git without `-z` is read without it, and the cost is
exactly what `-z` buys — a worktree whose path holds a newline is read cut off
at it, and no output from that git can say otherwise.

## Getting started

```sh
git clone https://github.com/ngsanogo/yagit
cd yagit
./do up        # start the stack in the background; prints the URL and the token
```

`./do <task>` is a shortcut for `mise run <task>` (`do.cmd` on Windows), and
`./do` alone lists the tasks. The stack is the daemon, rebuilt by air on every
saved Go file, and Vite; the daemon proxies everything outside `/api` to Vite,
so the browser only talks to `http://127.0.0.1:7420`.

The session token belongs to the checkout (`.yagit/session-token`), so
restarting the stack leaves an open tab logged in. `./do token --new` replaces
it and logs every browser out.

`.env` is optional; [`.env.example`](../.env.example) documents it:
`YAGIT_ROOT` (the directory under which yagit may open repositories, your home
directory by default) and the two ways to browse from another machine — a
reverse proxy (`YAGIT_PUBLIC_URL`), or the machine's name with
`YAGIT_PUBLIC_HOST` and `YAGIT_LISTEN_ALL=1`. The daemon refuses a combination
of the two.

## Tasks

| Task | Effect |
| --- | --- |
| `up` / `down` / `status` / `logs` | The stack in the background (Unix). On Windows, use `dev`. |
| `dev` | The stack in the foreground. |
| `bootstrap` / `bootstrap:browsers` | Install the frontend dependencies / the Chromium the e2e tests drive. |
| `build` | Build the frontend, then the binaries for every platform into `dist/`. |
| `test` | Go, frontend and end-to-end tests. |
| `test:go`, `test:web`, `test:e2e` | One of them. `test:e2e` reuses a running stack, or starts one for the run. |
| `test:release` | Playwright against the binary `build` made for this machine. |
| `test:coverage`, `test:bench` | Coverage of both suites; Go benchmarks. |
| `lint` | Go (for every shipped platform), the frontend, the shell scripts, the workflows. |
| `audit` | Go (standard library included) and frontend dependencies against the vulnerability databases. |
| `fmt` | Reformat the code. |
| `shot [url] [file]`, `drive [url]` | Screenshot a page / drive the running interface from stdin, headless. |
| `token` | Print the session token, to query the API with curl. |
| `version` | The version the next release would carry, from the Conventional Commits. |

`lint` and `test` are the gates before a pull request; `audit` is informative,
since its answer changes without the code changing.

## The interface (dev)

Open the URL `./do up` prints. The workbench shows the repositories you have
opened as tabs, the history of the active one in the middle with its graph
drawn beside it, its branches, remotes and tags on the right. Above the history,
beside the branch you are on, are the three commands that involve somebody else
— fetch, pull and push — each carrying the count that says why you would press
it.

Only the rows on screen exist in the DOM, and only their page of history is
fetched — two hundred commits at a time, whichever two hundred the scrollbar is
over. So a repository with a hundred thousand commits opens as fast as one with
ten: git's own takes about three quarters of a second the first time, and
twelve milliseconds a page afterwards.

The graph is assigned in the daemon, because a commit's column follows from
every commit above it and the browser only ever holds a window. Some histories
are too wide to draw at all — git's own needs 280 columns with every ref drawn
— and yagit says so, with the number, rather than drawing a narrower picture
that leaves branches out.

Every visual value comes from `web/src/design/tokens.css`, and the design
system page at `/design` shows all of it: every token, every component, in
every state.

## Principles

- **Third-party code is pinned by content**: `mise.lock`, `web/pnpm-lock.yaml`,
  and every GitHub Action by commit digest.
- **The daemon listens on the loopback unless told otherwise**, and the session
  token guards every route. The full model is in [SECURITY.md](../SECURITY.md).
- **When git fails, everything is shown**: the exact command, the exit code, the
  raw stderr.

## Release install scripts

`scripts/install.sh` and `scripts/install.ps1` download a GitHub Release
binary for the machine, verify `SHA256SUMS`, and install a launcher named
`yagit` that defaults `-root` to the home directory. They are what the README
points end users at. Pin a tag with `YAGIT_VERSION=<tag>`.

`scripts/uninstall.sh` and `scripts/uninstall.ps1` reverse that install: the
launcher and the binary go, an empty `YAGIT_HOME` goes with them, and the
session token is named rather than deleted — removing a secret without saying
so would be the kind of quiet that looks like a bug the next time somebody
starts `yagit` and finds a fresh token.
