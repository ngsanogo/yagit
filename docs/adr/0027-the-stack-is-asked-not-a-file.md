# 0027 — Whether a stack is up is asked of the port, not of a file

**Status:** new. Reverses the second half of
[0026](0026-the-session-token-outlives-the-run.md) — the daemon's own token
file — and keeps the first. Applied.

## What was decided before

0026 moved the session token into `.yagit/session-token`, written by `./do`
and reused across stacks, and kept the daemon writing `.yagit/token` at start
and removing it at stop. The two were argued to answer two questions: the
first "what is the secret", the second "is a daemon up, and here it is". `./do
token`, `./do shot`, `./do drive`, the readiness probe and the end-to-end
suite all read the second file, and three error messages sent a reader to
`./do up` when it was absent.

## What went wrong

The second file answered its question wrongly in both directions.

**Present when no daemon is.** The daemon removes the file in a deferred call
that never runs on `kill -9`, an out-of-memory kill or a power cut — and air's
two-second kill delay reaches the daemon before its five-second grace period
does on an ordinary stop with a request in flight. Afterwards `./do status`
said "not running", because it probes the port, while `./do token` printed a
secret for nothing and `./do shot` launched a browser at nothing, because they
trusted the file. Before 0026 the stale secret opened nothing; after it, the
stale file held the checkout's persistent token, which opens the next daemon.

**Absent when a daemon is.** Every start began by deleting the file, on the
grounds that a copy left by a killed stack must not pass for a live one. That
deletion did not check for a live one. A `./do dev` typed beside a healthy
background stack — the card `./do up` prints suggests exactly that — or a
`./do test e2e` run during an air rebuild unlinked the running daemon's file.
The daemon writes it once and never again, so from then on `./do token`
refused, `./do status` said "starting", and the next `./do up` waited two
minutes and stopped a healthy stack.

**Read by the wrong stack.** The end-to-end suite read the file to
authenticate, so its throwaway stack ran on the checkout's token. A workbench
tab left open on that token reconnects on its own — the event stream retries
— and every click in it landed in the fixtures the specs were asserting on.

## The decision

There is one token file, `.yagit/session-token`, and `./do` alone writes it.
The daemon is given no `YAGIT_TOKEN_FILE` under `./do`; its `-token-file`
flag stays for a released binary run by hand, whose token would otherwise be
known to nobody.

Whether a stack is running is put to the port. `probeStack` first asks
`/api/health` with no token — a yagit daemon refuses that; a listener that
answers is a stranger, and the stored token is never sent to it — then
presents the stored token to `/api/health` and to `/`, and reports one of five
answers: nobody answers; the daemon answers and the frontend does not yet; a
daemon answers and refuses the token; something answers that is not a yagit
daemon; the stack answers. Every command that used to ask the file asks that
instead, and says which answer it got.

- `./do token`, `./do shot` and `./do drive` hand out the token once the
  stack has answered it. "Not running", "starting", "refuses the token in
  `.yagit/session-token`" and "not a yagit daemon" are four sentences, not
  one.
- `./do up` restarts a stack that refuses the stored token: the file is the
  checkout's token, and a daemon that disagrees with it was started before
  the file was deleted or edited. A stranger on the port is reported, never
  killed — that half of `./do down` is unchanged.
- `./do test e2e` mints a throwaway token for the stack it starts, and passes
  the token of whichever stack it drives through the environment. The suite
  reads nothing under `.yagit/`.
- `./do dev` stops a background stack before starting, since the two cannot
  share the ports, and says so.

The token's shape is defined once, in `internal/session`, for the daemon and
for `./do`. 0026 left a duplicate constant; this removes it.

## What was decided against

- **Keeping the daemon's file and checking it against the probe.** Two
  sources that can disagree need a rule for the disagreement, and every rule
  is a special case of "trust the probe". The file added nothing the probe
  did not already know.
- **Reading the token file's mtime, or a pid inside it.** Still a file: still
  survives a `kill -9`.
- **Refusing `./do dev` beside a background stack instead of stopping it.**
  The card offers `./do dev` as a way to watch the same stack. A refusal
  would send the reader to `./do down` and back; stopping it, with a line
  saying so, is what the reader meant.

## What it costs

`./do token` makes an HTTP request before printing — three, on the loopback,
in milliseconds (a challenge with no token, then the two halves of the stack).
It refuses when the stack is down, which is the behaviour docs/API.md promised
and the file never delivered.

The end-to-end suite can no longer be pointed at a running stack by hand
without `YAGIT_TOKEN` in its environment. It never could be pointed at one
correctly — `./do test e2e` is the way in, and it always was.

A daemon that answers with a token other than the stored one is restarted by
a plain `./do up`. That is a change of behaviour on a state that used to hang
for two minutes; it is printed, and it is what `--new-token` does on purpose.

The challenge catches a listener that answers health without a credential. It
does not catch one that refuses unauthenticated requests and then accepts any
token — that listener still sees the secret on the authenticated probe, which
is the residual shared-host risk documented in SECURITY.md.

## References

- `probeStack` and `runningStackToken` in `cmd/do/stack.go`
- `settleRunningStack` in `cmd/do/up.go`
- `internal/session`
- [SECURITY.md](../../SECURITY.md), "Design consequences"
