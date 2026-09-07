# 0009 — The entry point is Go, not bash

**Status:** reverses `./do` as a large bash script. Applied.

## What was decided before

One entry point, `./do`, in bash. The *one entry point* half was right and is
untouched. The *bash* half had grown into a program and had stopped paying.

## What it cost

**A second prerequisite the README did not name.** The script began with a
bash 4.3 version check. macOS ships bash 3.2 and will not ship another, so on
macOS the prerequisites were mise *and* a newer bash. On Windows — a platform
yagit builds a binary for on every release — there is no bash at all, and no
entry point.

**It generated a cryptographic token by shelling out to node**, in a Go
project:

```bash
YAGIT_TOKEN="$(node -e "process.stdout.write(require('crypto').randomBytes(32).toString('base64url'))")"
```

**Nothing in it was tested.** Not the `.env` reader, whose job is to produce
`YAGIT_ROOT` — the path that is the daemon's security boundary. Not the rule
deriving the listen address from `YAGIT_PUBLIC_HOST`, whose failure mode is
binding `0.0.0.0` when it should have bound the loopback. Untested shell on
the two values that matter most.

## The decision

The logic moves to `cmd/do`, in Go. `./do` becomes a POSIX `sh` shim of about
twenty lines, and `do.cmd` is its counterpart on Windows. Both do the same
three things: install the toolchain with mise, build `cmd/do`, hand over to it.

Compiled and `exec`ed, not `go run`. `go run` stays in the process tree as a
parent that deliberately ignores `SIGINT`, so a signal aimed at the script
would never reach the program and `./do dev` would leave Vite and the daemon
holding both ports. `exec` makes the shim *become* the program: signals and the
exit code pass through exactly as the terminal sent them. Verified — `SIGINT`
now yields exit 130, no surviving processes, both ports released.

## What was gained beyond portability

- The `.env` reader and the listen-address rule have tests, including the case
  where a key is a prefix of another key and the case of the commented-out
  `YAGIT_PUBLIC_HOST` that ships in `.env.example`.
- The token comes from `crypto/rand`.
- The help is printed from the same table that dispatches the commands, so a
  subcommand cannot be documented and missing, or present and undiscoverable —
  and a test asserts it.
- Stopping a process *tree* is stated once per platform, in
  `process_unix.go` and `process_windows.go`, instead of being a comment
  explaining a minus sign.

## What it costs

`./do help` and `./do token` used to be instant and now go through
`mise install` and a cached `go build`: 150 ms warm, against roughly 20 ms.
Every other subcommand already cost seconds.

`./do` is no longer a single file you can read top to bottom. It is a shim plus
a package of seven files — which is how a program of that size is read anyway,
and each of those files has a name saying what is in it.

Two shims exist where there was one script. They are dispatchers, not two
implementations: neither holds a decision, and nothing in either can drift from
the other without failing to start at all.
