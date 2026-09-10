# Architecture

yagit is a local daemon plus a web interface: a single Go binary with the
frontend embedded, driving the `git` binary in subprocesses, serving a React
application on the loopback.

This document is the reference for how it is built and why. It describes the
architecture as built. Where a choice had a serious alternative, the reasoning
is in [docs/adr/](adr/README.md) rather than here: what was decided, what
against, and what it costs. Those records are history, not law — five of them
reverse an earlier decision. Phase 12 recorded what the planned phases deferred;
further work is not numbered in [ROADMAP.md](ROADMAP.md).

## Principles

These are not decoration. Each one translates into a rule a reviewer can check,
and each one has been cashed out somewhere in the code.

**Beautiful is better than ugly.** An explicit design system comes before any
screen: type scale, spacing scale, palette, radii, shadows, easing curves. No
magic value inside a component. The commit graph is the main object on screen;
everything else gives way to it.

**Explicit is better than implicit.** yagit never runs a destructive git
command without showing the exact command first. Any operation that rewrites
history or destroys work asks for confirmation and *names what will be lost*. A
log panel shows the git commands actually executed, permanently — a user should
be able to learn git by watching yagit work.

**Simple is better than complex. Complex is better than complicated.** Graph
lane assignment is intrinsically complex, so it gets its own isolated, tested
module. Nothing else is, and nothing else may pretend to be.

**Flat is better than nested.** Maximum UI navigation depth: 2. No dialog inside
a dialog. In code, a flat package structure; needing a third level of nesting
means the split is wrong.

**Sparse is better than dense.** The usual mistake in this category of tool is
density — every pane filled, nothing given room. Every view here has one main
object and breathes around it.

**Readability counts.** No clever one-liners. A long clear name beats a short
cryptic one.

**Errors should never pass silently.** No empty `catch`, no `_ = err`. When a
git command fails, the interface shows the exit code, the raw stderr, and the
exact command — never "Something went wrong". Silencing an error requires a
comment explaining why. `errcheck` runs with `check-blank`, so this is enforced
by machine, not by memory.

**In the face of ambiguity, refuse the temptation to guess.** Conflict state,
detached HEAD, interrupted rebase, diverged upstream: yagit describes the state
and offers named actions. It never chooses on the user's behalf.

**There should be one obvious way to do it.** One action, one path in the UI. No
button duplicating a context-menu entry "for discoverability". The same rule
applies to the tooling: every project command goes through `./do`, and every
value has exactly one definition — tool versions in `mise.toml`, ports in
`cmd/do/main.go`, the repository root in `.env`.

**If the implementation is hard to explain, it's a bad idea.** If a module
cannot be explained in three sentences in its header doc, it gets rewritten.

**Namespaces are one honking great idea.** Clean module boundaries, one
direction of dependency.

## Layers

Read downwards. Every dependency points down the list, and none points back up.

```
web       the React frontend               → api, over HTTP
api       routes, authentication, errors   → history, repo, git, watch
history   the assigned history, held       → graph, repo, git
graph     lane assignment, pure            → git
repo      open repositories, the boundary  → git
watch     the git directories, watched     → fsnotify, nothing of yagit's
edit      work-tree files, read and written → nothing
git       exec and parsing                 → nothing else in the project
session   the token, minted and checked    → nothing; used by both cmd/
protect   owner-only lockdown for secrets  → nothing; used by both cmd/
```

`internal/git` knows nothing about HTTP; `internal/api` never spawns a
subprocess; `internal/graph` knows a SHA and a list of parents and nothing
else.

| Package | Responsibility |
| --- | --- |
| `cmd/yagit` | Wiring only: configuration, token, lifecycle. |
| `cmd/do` | The project's entry point. Every command `./do` offers ([0009](adr/0009-the-entry-point-is-go.md)). |
| `internal/git` | Runs `git`, parses its machine-readable output. The project's only subprocess boundary. |
| `internal/repo` | Open repositories, and the check that a path is allowed. |
| `internal/graph` | Lane assignment: a pure function from commits to columns and lines. |
| `internal/watch` | Each open repository's git directories, watched: the common one, and a linked worktree's own beside it. Depends on fsnotify and on nothing of yagit's. |
| `internal/history` | Each open repository's assigned history, held between requests. |
| `internal/edit` | Work-tree files, read and written. The only place the daemon opens the user's own files, and it runs no git. |
| `internal/api` | HTTP surface, authentication, error shape. |
| `internal/assets` | The built frontend, embedded through `//go:embed`. |
| `internal/session` | The session token: minted and checked in one place, for the daemon and for `./do` ([0027](adr/0027-the-stack-is-asked-not-a-file.md)). |
| `internal/publicurl` | The address a reverse proxy serves yagit at, checked and turned into the origin a browser presents — one rule for `./do` and the daemon ([0036](adr/0036-a-reverse-proxy-is-a-public-url-not-a-wider-listen.md)). |
| `internal/protect` | Owner-only lockdown for secrets — chmod on Unix, an explicit ACL on Windows ([0035](adr/0035-secrets-get-an-owner-only-acl-on-windows.md)). |
| `web/src/design` | Tokens and the design system showcase. |
| `web/src/components` | Base components. |
| `web/src/app` | The workbench: tabs, the history, the graph, the references. |
| `web/e2e` | Playwright end-to-end tests, and the fixtures they build. |
| `web/scripts` | Frontend tooling: the screenshot runner behind `./do shot`, and the browser driver behind `./do drive`. |

## Stack

Each line links the record that argues it.

- **Backend**: Go. One binary, frontend embedded through `embed.FS`.
- **Frontend**: React, TypeScript, Vite, Tailwind — the utilities generated
  from `tokens.css`, which stays the single source of every visual value
  ([0011](adr/0011-tailwind-generates-the-utilities.md)).
- **Graph**: assigned in Go, drawn in SVG for the visible window only, one
  `<path>` per column. Not canvas: the list is virtualised, so the graph never
  holds more nodes than the rows beside it, and SVG takes
  `var(--color-lane-N)` directly ([0003](adr/0003-the-commit-graph-is-svg.md),
  [0012](adr/0012-lanes-are-assigned-in-the-daemon.md)).
- **Diffs**: no editor component. The daemon parses the patch into hunks and
  lines, and the browser draws that list and lets lines be chosen out of it.
  Not Monaco, which parses theme colours as hex and silently renders anything
  else red — including this project's OKLCH tokens
  ([0004](adr/0004-diffs-are-codemirror.md)); and not CodeMirror either, whose
  merge view accepts changes into a *document* while every action here carries
  the patch's own fingerprint back
  ([0034](adr/0034-the-diff-is-drawn-not-edited.md)).
- **Server state**: TanStack Query, one key per page of commits, invalidated by
  the event stream. TanStack Virtual for the rows
  ([0005](adr/0005-server-state-and-virtualisation.md)).
- **Routing**: none. Two paths, read once from `window.location.pathname`: the
  workbench and the design system showcase. What survives a reload is
  `localStorage`, not a URL ([0006](adr/0006-no-router.md)).
- **Updates**: one server-sent event stream for the whole session, never one per
  repository ([0007](adr/0007-one-event-stream.md)).
- **git**: the `git` binary in a subprocess. No go-git, no libgit2. yagit shows
  the commands it runs, and those commands have to be ones a user can paste into
  their own terminal ([0002](adr/0002-drive-the-git-binary.md)).
- **File watching**: fsnotify, over a bounded set of paths per repository
  ([0008](adr/0008-file-watching-through-fsnotify.md)).

## The git layer

`exec.Command` with arguments as an array, never a string handed to a shell.
Machine-readable formats throughout:

- History:
  `git log HEAD --topo-order --decorate=short --pretty=format:'%H%x00%P%x00%an%x00%aI%x00%s%x00%D%x00%x0a' --`
  `HEAD` is the revision the interface asks for first — what is checked out —
  and `--all` takes its place when every ref is chosen instead
  ([0016](adr/0016-the-graph-draws-what-is-checked-out.md)).
  The NUL separators and the NUL-then-newline terminator are not optional:
  without them, multi-line commit messages break the parser. NUL is the byte
  git refuses in every field — 0x01, which this format used at first, it does
  not, and a subject reading `Revert "add \x01 handling"` split one commit
  into two.
- Working directory:
  `git status --porcelain=v2 --branch --untracked-files=all -z`
  `--untracked-files=all` because git otherwise collapses an untracked
  directory into one `sub/` entry, and a panel offering to stage `sub/` can
  neither show what is inside it nor leave a file out. A rename spends TWO
  NUL-terminated records — the entry, then the path it came from — and reading
  the first without the second shifts every field after it: every following
  file would carry the wrong path, silently.
- Diffs, on three sides: `git diff --cached` for HEAD against the index, `git
  diff` for the index against the work tree, and `git diff --no-index --
  /dev/null <path>` for a file git has never seen. The last one exits 1 when
  the files differ, which is the case it exists for, so 1 is listed as a
  success code rather than checked for afterwards.
- One commit:
  `git log -1 --pretty=format:'%H%x00%P%x00%an%x00%aI%x00%cn%x00%cI%x00%D%x00%B'`
  and then `git show --format= --patch`. Two reads rather than one, because
  `git show` prints the message and the patch together and telling where one
  ends means either trusting a message not to look like a diff — it can — or
  choosing a separator a message could contain. A merge takes
  `-m --first-parent`: it has one answer per parent, git prints none of them
  by default, and the payload says which one is being shown.
- Refs:
  `git for-each-ref --sort=version:refname --format='%(refname)%00%(objectname)%00%(*objectname)%00%(upstream)%00%(upstream:track)'`
  `--sort=version:refname` so release tags and version-like branch names are
  in version order rather than lexicographic (which puts v0.10.0 before
  v0.9.0). Tags are then sorted newest-first in process — the same
  `version:refname` compare that reorders `tag:` decorations from `%D` — so
  the sidebar and the badges on a row agree. A descending sort on the whole
  namespace would reverse ordinary branches too. `%(*objectname)` is not
  optional either: for an annotated tag `%(objectname)` is the tag object, not
  the commit it points at, and the badge would attach to an object absent from
  the graph.
- Rewriting a range from a plan:
  `git rebase --interactive --no-autosquash --no-autostash --no-rebase-merges --no-update-refs -- <base>`
  The four pins are the plain rebase's four, for the same reasons, and
  `--no-autosquash` matters more here: with the setting on git rewrites the
  todo list it was given, promoting any commit whose subject begins `fixup!`
  into an instruction the user never wrote. `--no-ff` is deliberately absent —
  there it makes "every commit is written again" true, here it would rewrite
  the commits the plan left alone, so git fast-forwards over the untouched
  bottom of the plan and those commits keep their hashes.

  The todo list is the input that is not in the argument list. git obtains it
  by running `GIT_SEQUENCE_EDITOR` over a file it has written, so yagit writes
  the list itself and points that variable at its own executable with a private
  flag — the one program certain to exist wherever the daemon runs
  ([0029](adr/0029-a-rebase-plan-is-written-not-edited.md)). `GIT_EDITOR` is
  pointed at the same executable under a flag whose whole behaviour is to
  refuse and exit non-zero, so a step that asked for a commit message fails at
  once instead of committing one nobody read. Leaving it unset was the first
  answer and is not portable: git says "Terminal is dumb, but EDITOR unset" on
  Linux and macOS, and on Windows opens an editor from its own bundled
  environment that waits for a terminal it has not got.
- Checking out: `git switch --no-guess -- <branch>`, or `git switch --detach --
  <commit-ish>` for everything that is not a local branch. `git checkout` is
  not used and the difference is not cosmetic: `git checkout main` switches
  branch unless a file called `main` exists, where it silently restores that
  file over the user's work instead — same command, same exit code, a different
  operation. `--no-guess` refuses the other silent one, where a name matching a
  remote-tracking branch creates a local branch and sets its upstream from a
  request that named neither.

- Merging: `git merge --ff-only -- refs/heads/<branch>`, or
  `git merge --no-ff --no-edit -m "Merge branch 'x' into main" --
  refs/heads/x` where a commit is recorded. Never a bare `git merge`, which
  reads `merge.ff`; never a short name, which git resolves through `refs/tags`
  before `refs/heads`; never git's own message, which is composed from the name
  as it was given and from `merge.log`. All three are one rule —
  [0021](adr/0021-a-shown-command-is-not-a-setting.md) and
  [0022](adr/0022-a-merge-is-checked-before-it-runs.md) — and the last one is
  why `shellQuote` has a second quoting style: a message with `'x'` inside it
  has to stay a sentence on the confirmation that shows it.

Updates are pushed to the frontend over SSE, debounced at 100 ms, ignoring
`.git/index.lock` — which appears and vanishes around every write, yagit's own
included.

A command that writes outlives the browser that asked for it. Every request
context is cancelled the moment a client disconnects, and a reload during a
merge — the most likely moment for one, because the button has been spinning
while git runs a hook — would otherwise SIGKILL git halfway and leave a
half-written merge and an `index.lock` behind. The write routes run under
`context.WithoutCancel`, so the deadline is the command's own timeout and
nothing else; the reads keep the request's context, because nobody is left to
want them.

Every execution — success or failure — is handed to an observer. That is what
feeds the command log panel, and it is why the panel cannot drift out of sync
with what actually ran.

## Lane assignment

An isolated, pure, tested module — `internal/graph`. In the daemon rather than
in the browser, and that is forced: a commit's column follows from every commit
above it, and the browser only ever holds a window
([0012](adr/0012-lanes-are-assigned-in-the-daemon.md)).

Commits are walked in topological order against an array of active columns,
each holding the SHA it is waiting for:

- A commit's column is the leftmost one waiting for it, otherwise the leftmost
  free one.
- Then free **every other** column that was waiting for that same SHA. This is
  branch convergence. Omitting it leaves phantom columns open across hundreds
  of rows, and it is the bug that makes most implementations unreadable.
- The first parent continues the commit's column, which is what keeps a branch
  on one colour for its whole length. Each additional parent takes a column
  already waiting for it if one exists, otherwise the leftmost free one.
- A root commit frees its column.

The answer is a column per commit and one **edge** per (commit, parent) pair:
from a dot, down a single column, to another dot. Not the segments crossing
each row, which is the same picture written per row and is quadratic — a
history with a few hundred concurrent branches wrote 5.5 GB of them for a
hundred thousand commits, against 3.3 MB of edges.

A window of that answer is not a scan of it. The edges are ordered by the row
each line leaves, so the lines starting inside a window are two binary searches
away — but a line entering the window from above and leaving below it starts
nowhere near either bound, and no ordering by one end will find it. Assignment
records those once, for the top of every 256 rows, and a window is the two
searches plus that list. On git's own history the last page costs 2.4 µs
against the 57 µs of reading every edge.

Links are drawn as Bézier curves, in SVG, for the visible window only
([0003](adr/0003-the-commit-graph-is-svg.md)). The per-column palette is stable
over time: a branch does not change colour as you scroll.

Some histories cannot be drawn at all. git's own needs 280 columns with every
ref drawn — git's own `--graph` needs 67 over the identical order — and no
picture that wide is readable. Which refs are drawn is therefore a choice on
screen, defaulting to what is checked out
([0016](adr/0016-the-graph-draws-what-is-checked-out.md)); past a bound the
interface still says so, with the number, rather than drawing one that leaves
branches out.

## Staging part of a file

`git add` takes whole paths. Anything finer has to be expressed as a patch and
handed to `git apply`, and building that patch is the one place in this project
where a bug produces no error at all: a patch with a miscounted range applies
cleanly and stages a file nobody asked for.

`internal/git/patch.go` is that builder, and it is pure, fuzzed, and written
once for both directions. The direction is which side of the diff the patch
will be applied against, and it decides how an UNSELECTED line is written:

|  | applied forward, onto the old side | applied reversed, onto the new side |
| --- | --- | --- |
| unselected addition | dropped — not in the base, must not be in the result | becomes context — in the base, and staying |
| unselected removal | becomes context — in the base, and staying | dropped — already gone, and staying gone |

Reversing a forward patch is **not** the reverse patch. The context lines
differ, `git apply` matches on context, and the mistake is silent whenever a
hunk holds one change. `StageLines`, `UnstageLines` and `DiscardLines` each
pair a direction with a `git apply` flag once, so the two cannot disagree.

A selection covering every changed line is not a patch at all: it is `git add`
or `git restore` on the path, because a patch cannot carry a mode change, a
binary file or a deletion.

The client sends back the **fingerprint** of the diff its selection was made
against. The daemon re-reads the diff before building anything, and refuses
with 409 if it has moved. Without that check a patch built from stale line
numbers can still apply — somewhere.

## Resolving a conflict without leaving

A merge that stops leaves the work-tree file holding both versions between
markers, and the repository holding three index stages for that path. Every
other screen in yagit can describe such a file and none of them could fix it,
which is where a git client stops being one.

Three things had to exist for that, and only the third is about editing.

**The repository has to say what it is doing.** `git status --porcelain` does
not: it reports files and a branch, and says nothing about the merge that
produced them. git records the operation as files in the work tree's own git
directory — `MERGE_HEAD`, `rebase-merge/msgnum` — and `git status` in a
terminal reads exactly those to print "You are currently merging". So
`internal/git/state.go` reads the same ones, in git's own order: an interactive
rebase stopped on a conflict has a rebase directory AND `CHERRY_PICK_HEAD`,
because replaying a commit is how a rebase moves.

**An unmerged path has no diff.** `git diff` answers one with git's combined
format — `diff --cc`, hunks headed `@@@`, two columns of markers per line —
because a conflicted path has three versions and not two. There is no patch to
build from it and no such thing as staging half a conflict. The parser refuses
it by name; before that it failed on the `@@@`, and the interface showed a
message about a range over a file whose real problem was an unfinished merge.

**The file has to be editable.** `internal/edit` is the only place the daemon
opens the user's own files, and it runs no git at all — deleting seven
characters is not a git operation, and driving `git` for it would mean
inventing a command git does not have. What it does have is the part that is
actually hard:

- The work tree is opened as an `os.Root` and no absolute path is ever built,
  so the kernel refuses a component that leaves it — including a symlink
  swapped in after a check, which is the one thing a check cannot cover. What
  the package decides for itself is `.git`: under it, or named at any depth,
  directory or the one-line pointer file a linked worktree uses.
- A save carries the fingerprint of the content it started from, and is refused
  if the file moved. The same guard as a line selection, and the moment it
  matters most is a merge, where a `git checkout --theirs` in another window is
  exactly what a save would silently undo.
- Line endings are recorded and put back. A text box normalises every newline
  it is given to LF, so a CRLF file round-tripped through one returns with
  every line changed: a diff touching the whole file for a one-line fix.
- The write is a temporary file and a rename, so an interrupted save leaves the
  old content rather than half the new.

The choice between the two sides is made in the browser, over the markers, and
rewrites a buffer. Taking a side of a WHOLE file is the other thing and goes
through git — `git checkout --ours`, then `git add`, because the first writes
the file and only the second collapses the index stages.

## The assigned history, held

Paging the transport is not paging the computation: `git log` walks the whole
history whatever page is asked for. `internal/history` holds each open
repository's commits and their assignment — one per repository and per chosen
set of refs, since the two sets see different commits and so are two
assignments — and reassigns them when the refs or HEAD move.

The refs and HEAD are a complete fingerprint, not a heuristic — a commit's name
is a hash of its content, so nothing reachable from either can change without
one of them moving, and a checkout moves HEAD alone. `git for-each-ref` and
`git rev-parse` run on every request and cost milliseconds; the walk behind
them runs when the answer would differ.

That same read is what `GET /api/repos/{id}/refs` answers with. The store hands
back the list it read to decide whether it was still current, which is how
`internal/api` gets to run no git command of its own.

A repository is forgotten when it is closed. Without that, a tab shut after
browsing a large history holds every commit of it for the life of the daemon.

## Security model

The threat model, the guarantees and the known limitations are in
[SECURITY.md](../SECURITY.md). In short:

- Loopback by default; widening the listen address is a deliberate act, and a
  reverse proxy in front reaches the browser without it.
- A 256-bit session token required on every route, exchanged once for an
  HttpOnly cookie.
- Repositories opened by path once, addressed by opaque id afterwards.
- Origin checking on cookie-authenticated mutating requests.
- git never invoked through a shell. The one string git itself runs through one
  — the sequence editor of an interactive rebase — is quoted by yagit and
  holds nothing a client supplied
  ([0029](adr/0029-a-rebase-plan-is-written-not-edited.md)).
- The one route that writes a user's file works through an `os.Root` on the
  work tree, and refuses `.git` at any depth.

## One origin, in development and in production

The browser only ever talks to the daemon, on one origin.

In production the daemon serves the embedded frontend. In development it proxies
everything outside `/api` to Vite, which listens on the loopback and is never
addressed directly. The browser sees no difference, so the token-for-cookie
exchange follows the same path on both sides.

This matters because an authentication model that differs between development
and production is a model nobody ever really tests.

Vite's hot-reload socket is part of the same origin. Its client is told no port
and opens the WebSocket on the page's own, and the daemon passes the upgrade on
to Vite with everything else outside `/api` — so it works whether the browser
came straight to the daemon or through a reverse proxy serving yagit under a
name of its own. A reverse proxy is one more hop in front of that origin, not a
second origin: the browser still talks to one address, which is the proxy's,
and `YAGIT_PUBLIC_URL` is how the daemon learns what that address is
([0036](adr/0036-a-reverse-proxy-is-a-public-url-not-a-wider-listen.md)).

## Agent configuration

AI coding agent instructions live in [`agent/`](../agent/). Tool-specific
directories (`cursor/`, `claude/`) are generated from there by
`./do agent sync`. See [`agent/README.md`](../agent/README.md).

## Toolchain

Everything runs natively. `mise.toml` pins Go, Node, air, golangci-lint,
shellcheck, actionlint and zizmor; `mise` is the only prerequisite — on every
platform, which it was not while `./do` needed a bash newer than the one macOS
ships — and `./do` is the only entry point. It is a short `sh` shim that points
the tools at `.yagit/` and hands over to `cmd/do`, with `do.cmd` beside it for
Windows ([0009](adr/0009-the-entry-point-is-go.md)).

Every platform yagit ships a binary for runs a gate in CI
([0010](adr/0010-every-shipped-platform-is-a-gate.md)).

Three points deserve care, because each replaced an earlier constraint that no
longer holds:

1. **The listen address.** The built binary binds `127.0.0.1`: the safe default.
   `./do` widens the listen address only when both a non-loopback
   `YAGIT_PUBLIC_HOST` and `YAGIT_LISTEN_ALL=1` are set — a headless
   development machine browsed from somewhere else, and a person who agreed to
   it. `YAGIT_PUBLIC_URL` is the other way to be browsed from somewhere else,
   through a reverse proxy on the same machine, and it keeps the daemon on the
   loopback; beside either widening setting it is refused. The rule lives in
   one place, `listenAddress` in `cmd/do/project.go`, and is commented there.
   What protects the daemon afterwards is the token, never the address. This is
   exactly the kind of thing someone later "fixes" by mistake.

2. **File watching.** Through fsnotify, the one third-party Go dependency: inotify is Linux's mechanism, macOS uses kqueue and Windows
   `ReadDirectoryChangesW`, and writing all three is a project rather than a
   phase ([0008](adr/0008-file-watching-through-fsnotify.md)). Do not build a
   polling fallback speculatively: that is a second mechanism to maintain for a
   problem that no longer exists. Watch a bounded set of paths per repository —
   `.git/HEAD`, `.git/index`, `.git/refs`, `.git/packed-refs` — rather than the
   whole tree, and if watching cannot be established, say so loudly. An
   interface that silently stops refreshing is the worst of both worlds.

   Directories, never files: git replaces HEAD, the index and packed-refs by
   writing a temporary file and renaming it over the old one, and a watch on
   the file follows the inode that was replaced. The refs tree is followed as
   it grows — `git branch feat/x` creates a directory, and every branch under
   it would otherwise be invisible.

   That leaves one thing the watch cannot see: a file saved in an editor,
   which touches nothing under `.git`. The interface polls for that, while its
   window has focus: `git status` every two seconds, and the diff of the file
   open beside it on the same interval — the status reports which files differ
   and never what changed inside one, so a second save of an already-modified
   file moves the diff and leaves the status identical. The diff's poll stops
   past the two thousand lines the pane draws, where a re-read costs up to the
   daemon's whole diff cap and changes nothing on screen. That half is
   [0015](adr/0015-the-work-tree-is-polled.md).

3. **Credentials.** The daemon runs as the user, with their `~/.gitconfig`,
   their SSH agent and their GPG agent already present. There is nothing to
   mount or forward. The real work is elsewhere: `commandEnvironment()` in
   `internal/git/environment.go` builds git's environment from an allowlist rather
   than inheriting it. It pins several settings and passes through `PATH`,
   `HOME`, and `GIT_CONFIG_GLOBAL` / `GIT_CONFIG_SYSTEM` when those are already
   set. Names are added one at a time, each with the operation that made it
   necessary — never by inheriting the whole environment.

   `GNUPGHOME` and `SSH_AUTH_SOCK` arrived with commits, in phase 6: a user
   with `commit.gpgsign` set has decided that an unsigned commit is not
   acceptable, and a daemon that drops the variable their agent lives behind
   makes that decision for them. Two tests hold the line in both directions —
   one fails if any other agent variable appears, the other fails if either of
   these two is ever dropped. `GIT_ASKPASS` and `SSH_ASKPASS` stay out and are
   a different question: they name a program git runs to ask for a passphrase,
   and a daemon with no terminal has nowhere to ask.

   Phase 8 added the rest of the list for the same reason, one name at a time:
   the four proxy variables in both spellings, `GIT_SSH_COMMAND` and `GIT_SSH`,
   `XDG_CONFIG_HOME`, and `DBUS_SESSION_BUS_ADDRESS` where the keyring
   credential helper lives. Each of them breaks a fetch on a machine that sets
   it, and none of them breaks it in a way that looks like an environment
   problem — a dropped proxy hangs until the deadline and blames a network that
   works. yagit still authenticates nothing and never asks for a password:
   `GIT_TERMINAL_PROMPT=0` is what turns a missing credential into an error in
   the same second rather than a request that hangs. See
   [0020](adr/0020-the-network-is-gits-and-so-are-the-credentials.md), which
   also has the reasoning behind the ten-minute deadline those three commands
   carry in place of the Runner's thirty seconds.
