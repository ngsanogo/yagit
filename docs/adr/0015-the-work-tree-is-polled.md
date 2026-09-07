# 0015 — The repository is watched, the work tree is polled

**Status:** completes [0008](0008-file-watching-through-fsnotify.md), which
left half the question open.

## What 0008 settled, and what it did not

0008 decided how to watch — fsnotify — and what to watch: a bounded set of
paths per repository, `.git/HEAD`, `.git/index`, `.git/refs`, `.git/packed-refs`,
and **never the work tree**. Every reason it gave still holds.

What it did not have to answer, because there was no working-directory panel
yet, is how the interface learns that a file was saved in an editor. That
event touches nothing under `.git`. The watch, correct as designed, never
fires.

## The decision

Two mechanisms, each for what it is good at, and both said out loud:

- **The git directory is watched.** Anything git does — a commit, a checkout, a
  fetch, a rebase, a branch created in another terminal — moves a file the
  watch is on and reaches the interface within the 100 ms debounce, over the
  event stream of [0007](0007-one-event-stream.md).

- **The work tree is polled**, by the interface, every two seconds, and only
  while the window has focus. That is the `git status` behind the changes
  panel and, while a file is selected there, the `git diff` drawn beside it.
  Nothing else is polled.

## Why the open diff is on the same interval

Because the status cannot stand in for it. The status names which files differ,
never the content that differs: a file that is already modified and is saved
again produces a byte-identical status entry, since the entry carries codes and
a path and no digest of the work-tree copy. An interface that refreshed the
open diff only when the status changed would keep drawing the file as it was
two saves ago, and the poll would look like it was working.

It matters more here than staleness usually does, because the diff is not only
read: its line numbers are what the user clicks to stage a line. A selection
sent against a diff the file has moved past is refused by the daemon with a
409 — the right refusal, and a poor way to find out.

So the ceiling is two `git` processes every two seconds rather than one, and
only while the changes panel has a file selected and the window has focus.

## Why not watch the work tree after all

Because the cost is not proportional to what it buys. A work tree is an
unbounded tree of directories somebody else's build system writes into:
`node_modules`, `target`, `dist`, `.venv`. Watching it recursively means a
descriptor per directory on macOS and an inotify watch per directory on Linux,
charged against a per-user ceiling shared with every other watcher on the
machine — the editor, the file manager, the dev server already running. Linux
has sized that ceiling from addressable memory since 5.11, anywhere in
`[8192, 1048576]`, so the contributor's laptop that absorbs a work tree says
nothing about the smaller machine that will not. Past the ceiling the watch
fails, and the failure mode is an interface that has silently stopped
refreshing, which is the one thing this project refuses to ship.

`.gitignore` is not a filter that can be applied cheaply here either: it is per
directory, it can be changed while watching, and deciding whether a path is
ignored means asking git — which is the call the watch was supposed to avoid.

## Why two seconds, and why only when focused

`git status --porcelain=v2 -uall` on a warm repository is a few milliseconds:
git stats the index and the paths it lists, and the operating system has all of
it cached. The diff beside it is bounded by one file rather than by the
repository, which is the reason it can afford to ride the same interval.

It is bounded a second time, by the view. Past the two thousand lines the pane
draws, the re-read stops: what it would fetch is a diff of up to the daemon's
ten-megabyte cap, transferred and parsed on the main thread every two seconds,
and none of it past the first two thousand lines reaches the screen. That is
the tab freeze the cap exists to prevent, arriving on a timer instead of on a
click. The drawn lines can then go stale, and staging one of them is refused
with a 409 that names the file and says to read it again — a refusal anybody
can act on, against a tab nobody can use. Two
seconds is below the threshold at which a saved file feels unnoticed, and it
costs a rounding error of a CPU.

Only while focused, and that half matters more than the interval. A laptop with
yagit open in a background tab must not keep a `git` process starting all day
— that is the behaviour that makes a tool something people close. When the
window comes back, TanStack Query refetches on focus anyway, so the first thing
a returning user sees is current.

## What it costs

A file saved while the window has focus appears within two seconds rather than
instantly. That is the honest number.

The other price is the log panel. yagit promises that every git command it
runs appears there verbatim, and the poll keeps that promise loudly: up to
sixty entries a minute, which turns the 500-entry ring over in about eight
minutes. Someone scrolling back for a command from a quarter of an hour ago
will not find it. The alternative is worse — a poll the log panel hides is a
client running commands it does not show, which is the thing this project is
written against.

It also means there are two paths to the same screen being refreshed, which is
exactly the kind of duplication this project usually refuses. The
justification is that they answer different questions: one is "git did
something", the other is "the disk did something", and no single mechanism
answers both without paying for the whole work tree.

## What would change it

A watcher that can follow a work tree cheaply — FSEvents on macOS does this,
and `rjeczalik/notify` wraps it. 0008 declined that dependency on the grounds
that yagit does not watch large trees. If the poll ever proves to be the wrong
trade, that is the record to revisit, and the new one has to answer what a
recursive watch costs per directory on Linux, which FSEvents does not help
with.
