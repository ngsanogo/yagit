# Development

How to run yagit from a checkout. End users who want a published binary should
use the one-line installer in the [README](../README.md) once a release exists.

## Requirements

[mise](https://mise.jdx.dev), and nothing else. It installs Go, Node, air,
golangci-lint, shellcheck, actionlint and zizmor at the versions pinned in
[`mise.toml`](../mise.toml) — and at the checksums pinned in
[`mise.lock`](../mise.lock) — in user space, without sudo and without touching
`/usr/local`.

```sh
curl https://mise.run | sh          # macOS, Linux
winget install jdx.mise             # Windows
```

`./do` is a POSIX `sh` shim over a Go program, so it needs no bash at all; on
Windows, `do.cmd` beside it does the same job.

And `git` itself, which mise does not install and should not: yagit drives the
one already on your machine, with your configuration, your hooks and your
credential helpers ([ADR 0002](adr/0002-drive-the-git-binary.md)). Any
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
`rebase.updateRefs` force-moves branches the confirmation never named
([ADR 0023](adr/0023-a-rebase-is-read-in-both-directions.md)). An older git
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
./do up
```

On first run `./do` creates a `.env`, installs the toolchain and the frontend
dependencies, starts the stack in the background, and prints what to open:

```
  yagit   http://127.0.0.1:7420/
  Token    …   (paste on first visit)

  ./do down    stop
  ./do logs    follow output
  ./do dev     run in the foreground
```

The token belongs to the checkout rather than to the run that printed it, so
stopping and starting the stack leaves an open tab logged in; a browser asks
for it again only when its own session cookie is gone. `./do up --new-token`
replaces it, and logs every browser out.

Everything the checkout needs at runtime lives under `.yagit/` in the
repository — the toolchain mise installs, Go's build and module caches, npm's
cache, Playwright's browsers, the session token, the logs. Nothing is written
to your home directory, and removing the clone removes all of it.

`./do up` installs what it needs on first run. `./do bootstrap --browsers`
adds Chromium, which only screenshots and end-to-end tests need. `./do
shell-hook` prints aliases for up, down and logs; `--write` appends the one
line to your `~/.bashrc` or `~/.zshrc`. Every command takes `--help`.

`.env` holds one required value, `YAGIT_ROOT`: the directory under which
yagit is allowed to open repositories. It defaults to your home directory.
[`.env.example`](../.env.example) documents every other setting, including how
to develop on one machine and browse from another.

### Browsing from another machine

There are two ways, and `.env` takes one of them.

**Through a reverse proxy on the development machine** — the recommended one,
and the shape of a VM that serves each of its apps under a host name of its
own:

```sh
YAGIT_PUBLIC_URL=https://yagit.my-dev-box.local
```

The daemon stays on `127.0.0.1:7420`, where the proxy connects; point the proxy
there, let WebSocket upgrades through, leave responses unbuffered — the event
stream never ends — and have it send `X-Forwarded-Proto`. `./do up` prints the
public URL, the daemon accepts writes from a page served at it, the session
cookie is `Secure` because the browser's side is HTTPS, and hot reload rides
the proxy like every other request. A host name of its own matters: browsers
keep cookies per host rather than per port, so applications sharing one name on
different ports log each other out.

**Straight to the daemon**, by setting `YAGIT_PUBLIC_HOST` to the machine's
name and `YAGIT_LISTEN_ALL=1`, which widens the listen address to `0.0.0.0`.

Setting `YAGIT_PUBLIC_URL` beside either of those is refused, with the lines to
comment out for each way. `./do up --restart` picks up a changed `.env`.

The ports are `7420` for the daemon and `7421` for Vite, both on the loopback
and both defined in `cmd/do/main.go`. Vite is never addressed directly — the
daemon proxies to it — and it sits beside the daemon's port rather than on its
own default, which every other Vite project on the machine would also want.

## Commands

Everything goes through `./do`. There is no second path: no Makefile, no
`npm run` to type by hand, no container to build. When two paths lead to the
same place, one of them goes stale and nobody notices.

| Command | Effect |
| --- | --- |
| `./do up [--restart] [--foreground] [--new-token]` | Start the stack in the background. Print the URL and token, then return. `--new-token` replaces the token and restarts the stack on it. |
| `./do down` | Stop the background stack. |
| `./do status` | Report whether the stack is running. |
| `./do logs [--no-follow]` | Show output from the background stack. |
| `./do dev [--new-token]` | Run the stack in the foreground, with logs in the terminal. A background stack is stopped first: the two cannot share the ports. |
| `./do bootstrap [--browsers]` | Install dependencies into `.yagit/` and `web/node_modules/`. |
| `./do shell-hook [--write]` | Generate `yagit-up`, `yagit-down`, `yagit-logs` aliases. |
| `./do build` | Build the frontend and the binaries into `dist/`. |
| `./do test [go\|web\|e2e\|release\|bench\|fuzz\|soak\|coverage]` | Run tests. With no argument, go, web and e2e. `release` starts the built binary; `bench` times the graph; `soak` repeats the end-to-end suite on a busy machine; `coverage` prints a baseline. |
| `./do lint` | Static analysis of Go — for every platform yagit ships to, not only this one — the frontend, the shell — the `./do` shim and the release installers under `scripts/` — and the CI workflows. |
| `./do audit` | Check every dependency, the Go standard library included, against the vulnerability databases. |
| `./do fmt` | Reformat the code. |
| `./do shot [url] [out]` | Screenshot a page into a PNG, through Playwright. |
| `./do drive [url]` | Drive the running interface from stdin: one command per line, in a headless browser. |
| `./do token` | Print the session token, to query the API with curl — once the stack answers it. |
| `./do version` | Print the version this checkout would be released as, if anything warrants one. |
| `./do agent sync\|check` | Regenerate the tool shims under `claude/` and `cursor/` from `agent/`, or verify they match. |

`./do test e2e` starts the daemon and Vite for you, or reuses them when
`./do up` is already running in another terminal. No test requires you to have
remembered to start anything first.

`./do lint` and `./do test` are the gates before a pull request. `./do audit`
is informative only — it queries live vulnerability databases.

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
every commit above it and the browser only ever holds a window
([ADR 0012](adr/0012-lanes-are-assigned-in-the-daemon.md)). Some histories
are too wide to draw at all — git's own needs 280 columns with every ref drawn
— and yagit says so, with the number, rather than drawing a narrower picture
that leaves branches out.

Every visual value comes from `web/src/design/tokens.css`, and the design
system page at `/design` shows all of it: every token, every component, in
every state.

## Why it is built this way

Every choice had a serious alternative. What was decided, what against, and
what it costs is in [docs/adr/](adr/README.md).

- **One entry point, and one definition per value.** `./do` is the only way to
  develop, build, test and run yagit. Tool versions live in `mise.toml`, the
  ports in `cmd/do/main.go`, the visual values in `tokens.css`. A value written
  in three files is a value that will drift.
- **Everything third-party is pinned by content, not by name.** `mise.lock`
  holds a SHA256 per tool per platform, `web/package-lock.json` does the same
  for the frontend, and every GitHub Action is referenced by commit digest. A
  version tag is a mutable pointer someone else controls; a hash is not.
- **The daemon listens on the loopback unless told otherwise**, and what
  protects it afterwards is the session token required on every route — never
  the address. A repository is opened by path once and addressed by an opaque
  id from then on. The full model is in [SECURITY.md](../SECURITY.md).
- **When git fails, everything is shown.** The exact command, the exit code,
  the raw stderr. Never "Something went wrong" — and the rule holds for the
  project's own tooling too.

## Release install scripts

`scripts/install.sh` and `scripts/install.ps1` download a GitHub Release
binary for the machine, verify `SHA256SUMS`, and install a launcher named
`yagit` that defaults `-root` to the home directory. They are what the README
points end users at once a release exists. Pin a tag with
`YAGIT_VERSION=<tag>`.

`scripts/uninstall.sh` and `scripts/uninstall.ps1` reverse that install: the
launcher and the binary go, an empty `YAGIT_HOME` goes with them, and the
session token is named rather than deleted — removing a secret without saying
so would be the kind of quiet that looks like a bug the next time somebody
starts `yagit` and finds a fresh token.
