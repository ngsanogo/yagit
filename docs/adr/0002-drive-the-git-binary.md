# 0002 — Drive the `git` binary, not a library

**Status:** kept.

## The decision

`internal/git` runs the `git` executable in a subprocess, arguments as a slice,
and parses its machine-readable output. No go-git, no libgit2.

## What it is up against

**go-git** is pure Go: no subprocess, no `git` to have installed, and a typed
object model instead of parsing. **libgit2** through cgo is faster still.

Both are serious, and both lose here for the same reason.

## Why the binary wins

**The product promise is the commands.** yagit shows the exact command it ran,
with its exit code and its raw stderr, and a log panel keeps them all — a user
should be able to learn git by watching yagit work. A library has no command
to show. Printing a plausible-looking `git rebase --onto …` beside a call that
did something else through an API is worse than printing nothing: it is a
caption that can drift from the picture, and it would.

**Everything the user already configured comes free.** Their `~/.gitconfig`,
their aliases, their credential helper, their `commit.gpgsign` and signing key,
their hooks, their `core.excludesFile`, their LFS filters, their
`includeIf.gitdir` blocks. A library reimplements a subset of that and diverges
from the git in their terminal on exactly the cases they care about.

**Coverage.** Worktrees, submodules, LFS, partial and shallow clones, `rerere`,
interactive rebase, the reflog. yagit's roadmap ends in that territory, and it
is where a library's support is thinnest.

**cgo.** libgit2 would put `CGO_ENABLED=1` back on, and with it a cross
compiler and a libc dependency on every target. Six static binaries in eight
seconds is a consequence of not doing that.

## What it costs

A process launch per command: five to fifteen milliseconds, which is not
nothing when a screen wants twenty of them. Two answers, in this order:

1. **Ask for more per call.** One `git log --all` for the history, not one per
   branch. `git cat-file --batch` over a long-lived process for blobs, which is
   the documented way to avoid a spawn per object.
2. **Cache in `internal/repo`**, above the boundary, never inside it.

And a parser per format, which is why the format strings are pinned with NUL
separators and why `internal/git` is the package with fuzz targets.
