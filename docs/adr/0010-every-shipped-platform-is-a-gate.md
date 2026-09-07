# 0010 — Every platform we ship a binary for runs a gate

**Status:** new. Applied.

## The problem

`./do build` produced six binaries — Linux, macOS and Windows, amd64 and arm64
— and CI ran every gate on `ubuntu-latest` and nothing else. Two thirds of what
the build produces had never been executed by anyone.

That is not a theoretical gap. It hid a real defect: `commandEnvironment()`
built git's environment from an allowlist of `PATH` and `HOME`. On Windows that
is not a leaner environment, it is one where Winsock cannot load its provider
catalogue without `SystemRoot` — so every network command fails at name
resolution, with an error blaming the network — where a credential helper
shipped as a `.cmd` is invisible without `PATHEXT`, and where the user's
configuration lives behind `USERPROFILE` or `HOMEDRIVE` + `HOMEPATH` rather
than `HOME`.

A git client is a desktop application. Windows and macOS are not exotic targets
here; they are most of the audience.

## The decision

**A platform yagit ships a binary for runs a gate in CI, or yagit stops
shipping a binary for it.**

Concretely: a `platform` job runs the Go gate on `windows-latest` and
`macos-latest`, each through the entry point its users actually type — `./do`
on macOS, `do.cmd` under PowerShell on Windows. A `do.cmd` exercised only
through Git Bash would not be tested at all, and Git Bash is not what a Windows
contributor has open.

`fail-fast` is off. One platform failing is a result about that platform;
cancelling the other throws away the answer to "is this everywhere, or just
there".

## Scope, and why it stops there

The Go gate only. The frontend is platform-independent, and the end-to-end
tests would triple the job to re-check a browser rather than an operating
system. arm64 has no hosted runner and stays cross-compiled — worth saying out
loud, because the rule above is then satisfied only for the amd64 half of
macOS and Windows.

## What the gate catches

Platform bugs that reading the code does not surface:

- **The daemon took the repository hostage.** `internal/repo` held the `.git`
  directory open so that a recycled inode number could not pass for the same
  directory. Go opens without `FILE_SHARE_DELETE`, so on Windows that handle
  makes the operating system refuse to delete or even rename the project — a
  git client that stops you moving your own working copy. `pin_unix.go` and
  `pin_windows.go` now do opposite things for opposite reasons, each saying so
  in its own file.
- **The identity check did not work there at all.** `os.SameFile` on Windows
  compares a volume serial and an NTFS file reference, and `os.Stat` reads
  neither — it keeps the path and resolves it lazily, inside the comparison. A
  `FileInfo` taken before a directory was replaced therefore described the
  replacement, compared against itself, and reported it unchanged. The check
  that exists to refuse a swapped repository was answering "same" every time.
- A test asserting that `/` is an absolute path, which `filepath.IsAbs` says it
  is not on Windows.
- A test about a directory name ending in a space, which Win32 cannot
  represent.

Token-file privacy on Windows is a separate decision:
[0035](0035-secrets-get-an-owner-only-acl-on-windows.md).

## What it costs

Two more jobs, about a minute each, and the discipline of keeping them green.
The alternative is trusting the compiler alone for two of the three platforms
a release binary targets.
