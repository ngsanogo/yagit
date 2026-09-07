# 0008 — File watching through fsnotify, and the first Go dependency

**Status:** reverses "inotify works natively, without reservation".

## What was decided before

> **File watching.** inotify works natively, without reservation. Do not build
> a polling fallback speculatively.

The second sentence is right and stays. The first names a Linux system call as
though it were the mechanism, and yagit ships binaries for macOS and Windows,
where it does not exist:

| | mechanism |
| --- | --- |
| Linux | `inotify` |
| macOS, BSD | `kqueue` (or FSEvents for whole trees) |
| Windows | `ReadDirectoryChangesW` |

Three APIs with three failure modes, three descriptor models and three sets of
edge cases around renames, atomic replaces and unmounts. Writing all three is
not a task inside a phase; it is a project, and it is a project someone else
has already finished.

## The decision

`github.com/fsnotify/fsnotify`, and it becomes go.mod's first third-party
dependency.

Still watching a **bounded set of paths per repository** — `.git/HEAD`,
`.git/index`, `.git/refs`, `.git/packed-refs` — and never the working tree.
That rule was right for its own reasons and survives; it also happens to be
what makes the macOS backend viable, since kqueue costs a file descriptor per
watched path and a whole tree would exhaust them.

Still no speculative polling fallback. And still, if a watch cannot be
established, the interface says so loudly.

## Why this dependency, and what the rule is

yagit's Go side has had no dependency at all, which is a good position and not
a goal in itself. The goal is that every dependency be one someone can justify
in a sentence. The rule this sets:

**A Go dependency has to do something the standard library does not, that is
operating-system-specific, that is widely used enough to have had its edge
cases found by other people, and that is small enough to read.**

fsnotify is all four: BSD-3, no transitive dependencies beyond
`golang.org/x/sys`, and the file watcher underneath Kubernetes, Prometheus and
Docker. `go.mod` having one entry also means Dependabot and `govulncheck` start
having something to watch on this side, where until now they watched the
standard library alone.

`SECURITY.md` names a second candidate under the same rule —
`golang.org/x/sys/windows`, to give the token file an explicit ACL rather than
mode bits Windows ignores. Whichever arrives first, the four conditions are how
it is judged.

## What it costs

fsnotify's per-platform semantics are not identical, and the differences are
documented rather than hidden — a rename on Windows can arrive as a
remove-then-create, and a watched file replaced atomically can leave the watch
on the old inode. Both matter, and both are handled the same way: **watch the
directory, not only the file**, and re-stat after every event. yagit re-reads
git's state on any event anyway, so a spurious wake-up costs one `git rev-parse`
and a missed one is the case that must not happen.

The alternative dependency, `rjeczalik/notify`, wraps FSEvents on macOS and so
scales better to large trees. yagit does not watch large trees, so it would be
paying for a property it has designed itself out of needing.
