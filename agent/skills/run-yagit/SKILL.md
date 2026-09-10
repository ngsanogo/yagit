---
name: run-yagit
description: Start, drive and screenshot the running yagit app — bring up the daemon and Vite with ./do up, click through the workbench in a headless Chromium with ./do drive, read the API, then build, test and lint. Use when asked to run yagit, open a repository in it, screenshot a view, or check a change in the real interface rather than in the tests.
---

# Running yagit

yagit is a Go daemon that serves a React interface on `127.0.0.1:7420`. There
is no window to open: an agent starts the stack with `./do up`, then drives
the page with **`./do drive`** — a REPL over the frontend's own Playwright that
reads one command per line from stdin and prints the answer, so it works from a
pipe and from tmux alike.

All paths below are relative to the project root. Every project action goes
through `./do`; nothing here calls `go`, `npm`, `node` or `playwright` by hand.

## Prerequisites

[mise](https://mise.jdx.dev) and nothing else — `./do` installs Go, Node, air
and the rest at the versions pinned in `mise.toml`, and `npm ci` for the
frontend, on first run. `./do drive` and `./do shot` install Chromium into
`.yagit/browsers` the first time they need it — everything the checkout
downloads stays inside it. No `apt-get` was needed on this container.

`.env` holds `YAGIT_ROOT`, the directory under which the daemon may open
repositories. `./do` writes it on first run, defaulting to `$HOME`. A path
outside it is refused — that is a security boundary, not a setting to widen
casually.

## Start the stack

```bash
./do up      # returns once the stack answers, printing the URL and the token
./do status  # running, starting, or not running
./do logs    # follow its output
./do down    # stop it
```

`./do up` waits for the stack itself and returns the terminal, which is the
whole reason an agent uses it: no backgrounding, no readiness poll to write,
no pid to keep. It does not answer until both halves do — the daemon *and*
Vite behind it — so the next command in a `&&` chain can assume a working
interface.

A stack that fails to start says so instead of timing out: `./do up` watches
the supervisor, and prints the tail of `.yagit/dev.log` with the reason.

## Drive it (agent path)

Pipe a script in. Each reply is followed by a `ready` line — one line in, one
`ready` out, blank lines and `#` comments included:

```bash
./do drive <<'EOF'
open
repo open /path/to/some-repo
nav /
click 'role=tab[name=/yagit/]'
wait 'role=heading[name=/History —/]'
text 'role=heading[name=/History —/]'
shot workbench
errors
quit
EOF
```

`./do drive` exits non-zero if any command in the script failed, so a shell
that chains steps on `&&` stops where the interface did.

Screenshots land in `.yagit/shots/NN-<name>.png` — inside the gitignored
runtime directory, so they never dirty the working tree, and the number
continues across runs so a before is never overwritten by its after. The driver
prints the full path of each one; **look at the file**, a 401 page and a
workbench are the same size on disk.

| command | what it does |
| --- | --- |
| `open` | Exchange the token for the session cookie, load `/`, wait for the workbench |
| `nav <path>` | Go to a path (reloads, which is how the tab bar picks up a repository opened through the API) |
| `click <selector>` | Click the first match |
| `fill <selector> <value>` | Type into a box, re-filling until the value sticks |
| `press <key> [selector]` | Send a key |
| `wait <selector>` | Block until the first match is visible |
| `text <selector>` | Print the first match's text |
| `count <selector>` | How many match |
| `shot [name]` | Full-page screenshot into `.yagit/shots/` |
| `api <METHOD> <path> [json]` | Call the API with the token header |
| `repo open\|close\|list [path\|id]` | Open a repository by path, close one by id, list the open ones |
| `errors` | Console and page errors collected so far |
| `eval <js>` | Evaluate in the page |
| `quit` | Close the browser and leave |

**How a line is read.** The first few words of a line are tokens; whatever
follows them is the rest of the line, verbatim. Selectors and keys are tokens
and can be quoted. Values are not tokens and are never touched: the value of a
`fill`, the JSON body of an `api`, the path of a `repo open`, the JavaScript of
an `eval` all reach their destination exactly as written, quotes and spaces and
colons included.

```bash
api POST /api/repos {"path": "/home/ada/My Repo"}
eval document.title + " (built)"
fill 'role=textbox[name="Commit message"]' feat: a line from the driver
```

Selectors are Playwright's, plus `label=` for this interface's labelled fields
(`label=Repository path`). Wrap any selector containing spaces in **single**
quotes — a role selector carries its own double quotes:
`click 'role=button[name="Open repository"]'`.

For step-by-step work, run the same REPL under tmux and poll for the `ready`
marker rather than sleeping:

```bash
tmux new-session -d -s yagit -x 200 -y 50
tmux send-keys -t yagit './do drive' Enter
timeout 60 bash -c 'until tmux capture-pane -t yagit -p | grep -q "driver ready"; do sleep 0.2; done'
tmux send-keys -t yagit 'open' Enter
timeout 60 bash -c 'until tmux capture-pane -t yagit -p | grep -q "^ready"; do sleep 0.2; done'
tmux capture-pane -t yagit -p | grep -v '^$' | tail -5
```

`capture-pane` pads its output to the height of the pane, so a bare `tail`
returns blank lines and looks like a driver that answered nothing.

### A worked flow: stage and commit through the interface

Build a throwaway repository under `.yagit/` (gitignored, and inside
`YAGIT_ROOT`) rather than committing into the checkout you are working in:

```bash
D=$PWD/.yagit/driver-demo
rm -rf "$D" && mkdir -p "$D"

# Every git in the fixture, and only the fixture: no global config, so this
# machine's identity and commit signing cannot reach it — and no `export`,
# so a later `git commit` in the real checkout still has both.
fixture_git() {
  env GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null \
    git -C "$D" -c user.name=Ada -c user.email=ada@example.com "$@"
}

fixture_git init -q -b main
printf 'one\ntwo\nthree\n' > "$D/notes.txt"
fixture_git add -A
fixture_git commit -qm "chore: seed"
printf 'one\ntwo\nthree\nfour\n' > "$D/notes.txt"
```

```bash
./do drive <<EOF
open
repo open $D
nav /
click 'role=tab[name=/driver-demo/]'
click 'role=radio[name=/Changes/]'
click 'role=button[name="Stage notes.txt"]'
wait 'role=heading[name=/1 staged, 0 unstaged/]'
fill 'role=textbox[name="Commit message"]' feat: a line from the driver
click 'role=button[name=/^Commit 1 to main\$/]'
wait 'text=Nothing to commit'
shot committed
errors
quit
EOF
fixture_git log --oneline -1   # feat: a line from the driver
```

Note the unquoted heredoc so `$D` expands, and `\$` in the commit button's
regex. Check the result in git, not only on screen: the interface can show a
committed state that git disagrees with, and only one of the two is the product.

### One-shot screenshot

When a picture is all that is wanted, `./do shot` needs no driver:

```bash
./do shot http://127.0.0.1:7420/ /tmp/workbench.png
```

It authenticates through the deprecated `?token=` query, which is all a
capture with nobody to fill the token form can do.

### The API from the shell

```bash
T=$(./do token)
curl -s -H "X-Yagit-Token: $T" http://127.0.0.1:7420/api/repos
curl -s -X POST -H "X-Yagit-Token: $T" -H 'Content-Type: application/json' \
  -d '{"path":"/path/to/some-repo"}' http://127.0.0.1:7420/api/repos
```

Routes are listed in [docs/API.md](../../../docs/API.md).

## Build, and run what was built

```bash
./do build          # frontend, then six cross-compiled binaries into dist/
```

`dist/` holds one binary per platform; take the one for this machine — the
example below is `linux-arm64`. The built binary serves the interface from
inside itself — no Vite, no `./do`. It is the only way to see what a release
actually renders, and `./do drive` points at it by url:

```bash
T=$(./do token)
YAGIT_TOKEN=$T ./dist/yagit-linux-arm64 -root "$HOME" -addr 127.0.0.1:7421 \
  > /tmp/yagit-release.log 2>&1 &
REL=$!
timeout 60 sh -c "until curl -sf -o /dev/null \
  -H 'X-Yagit-Token: $T' http://127.0.0.1:7421/api/health; do sleep 0.5; done"

./do drive http://127.0.0.1:7421 <<'EOF'
open
shot release
errors
quit
EOF

kill -INT $REL
```

A port of its own, and the running stack's token handed to it through the
environment — the same token, so the cookie a browser already holds opens both
daemons. It needs no `-token-file`: nothing under `./do` reads one. `./do
token` answers only while the stack is up, which the recipe needs anyway.

For the gate rather than a look, `./do test release` does all of the above by
itself — a free port, a scratch root, a token minted for the run — and runs
`web/e2e/release.spec.ts` against it. The recipe above is for driving the
release binary by hand, which is the only way to *see* what it renders.

## Run (human path)

`./do dev` runs the same stack in the foreground and stays there until Ctrl-C,
which is the shape to want when every line of output matters. On a headless
machine `./do up` is the one to use — there is no browser here to open it in
either way.

## Test and lint

```bash
./do test        # go, web and e2e — the merge gate
./do test go     # one target: go, web, e2e, bench, fuzz, soak, coverage
./do lint
```

`./do test e2e` starts its own stack, or reuses the `./do up` already running.
`./do test release` needs a `./do build` first; it is the only gate that starts
what a release actually ships.

## Gotchas

- **A mutating API call authenticated by the cookie alone is refused**:
  `403 origin rejected for a mutating request authenticated by cookie`. The
  daemon only skips the origin check for an explicit credential, so POST and
  DELETE need the `X-Yagit-Token` header even when the session cookie is
  already set. The driver sends it on every call.
- **Vite's own port is not the app.** `127.0.0.1:7421` answers `/api/repos`
  with `200` and the SPA's `index.html` — there is no proxy that way round; the
  daemon proxies *to* Vite. A script pointed at 7421 gets HTML where it expects
  JSON and no error to explain it. Always 7420.
- **The card may print a URL this machine cannot open.** When `.env` sets
  `YAGIT_PUBLIC_URL`, `./do up` prints the reverse proxy's address, which is
  for the person's browser. Scripts, `curl`, `./do shot` and `./do drive` on
  this machine keep using `http://127.0.0.1:7420`, where the daemon still
  listens.
- **Reading right after a click reads the previous state.** Views refetch on
  the server-sent event stream, so `text 'role=heading[name=/Changes —/]'`
  straight after `click 'Stage …'` still says `0 staged`. `wait` on the value
  you expect first — it retries; `text` does not.
- **`fill` on a box a query seeds appends instead of replacing.** The field
  settles from a request that lands after the dialog opens (the commit box from
  the message an amend would replace, `Scan in` from the last scanned root),
  and a value written mid-render collapses `fill`'s selection. Commit b36021b
  is this failing one run in fifteen. The driver's `fill` re-fills and reads the
  value back; if it reports `but it holds …`, wait for the box to settle first.
- **`pgrep -f 'do dev'` matches the agent's own shell.** The command line that
  launched the stack is itself a process containing that string, so the pattern
  returns two pids and `kill` refuses both. Capture `$!` at launch instead.
- **The workbench opens on whichever repository the daemon lists first**, not
  the one just opened. Click the tab by name: `click 'role=tab[name=/name/]'`.
- **A repository opened through the API does not appear until the page
  reloads.** `repo open <path>` then `nav /`.
- **The counts leave the heading when the tree is clean.** It reads
  `Changes — 1 staged, 0 unstaged` while something differs and plain `Changes`
  once nothing does, so a wait for `/0 staged, 0 unstaged/` after a successful
  commit times out. Wait for `text=Nothing to commit`.
- **Development and the release binary do not serve the same CSS**, so a visual
  change confirmed against `./do dev` is not yet confirmed. The daemon sends
  `default-src 'self'`, and an asset the bundler inlined as a `data:` URI is
  refused against it — in the built binary only, silently, in a console nobody
  has open. That is how a missing font was once found. It cannot happen now:
  `assetsInlineLimit` is 0 in `web/vite.config.ts` and `./do build` fails on a
  stylesheet that carries one. Still read `errors` against `./dist/yagit-*`
  before believing a visual change is fine — the policy has other directives.

## Troubleshooting

- **`error: the stack is not running. Run ./do up`** from `./do token`,
  `./do drive` or `./do shot`: nothing answers on the port. `./do up` waits for
  the stack itself. **`the stack is starting`** means a supervisor is alive and
  not answering yet; `./do logs` follows it.
- **`a daemon answers and refuses the token in .yagit/session-token`**: the
  stack was started on a token the file no longer holds — the file was deleted
  or edited underneath it. `./do up` restarts the stack on the stored token.
- **Every route answers `401`**: the token is missing from the request. Header
  `X-Yagit-Token`, or `POST /api/session` to trade it for the cookie — which
  is what the driver's `open` does.
- **`"/etc": path outside the allowed root ("/home/you")`**: the path is not
  under `YAGIT_ROOT`. Use a repository inside it, or change `.env` deliberately.
- **`InvalidSelectorError: … unexpected symbol "\"`**: double quotes were
  escaped inside a double-quoted selector. Use single quotes around the whole
  selector.
- **`error: usage: click <selector>`**: the line supplied no selector. The
  driver refuses to guess rather than clicking something arbitrary.
- **A command failed but the REPL kept going**: that is deliberate. The next
  line — `shot`, or `errors` — is usually what diagnoses it. The exit status
  still records the failure.
