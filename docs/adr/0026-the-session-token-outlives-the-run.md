# 0026 — The session token outlives the run that minted it

**Status:** new. Extends the reasoning already in the code twice over. Applied.
The second token file this record keeps — the daemon's own, whose presence
answered "is a daemon running?" — was removed by
[0027](0027-the-stack-is-asked-not-a-file.md), which says why. The token, its
file, and `--new-token` are unchanged.

## What was decided before

The token was moved out of the daemon and up into `./do`, and both halves said
why in a comment rather than in a record:

> The session token is minted HERE, once, rather than by the daemon on every
> start: air restarts it on every saved Go file, and a fresh token per rebuild
> would log the browser out in the middle of writing code. **The session is
> this `./do` run, not the process.**

That fixed the loud half of the problem. The quiet half survived it. Every
`./do up` minted a token of its own, so `./do down` followed by `./do up` — the
ordinary shape of a working day — invalidated the cookie in a tab that had
never closed. The interface answered with a 401, reloaded onto the door page,
and the first thing the terminal printed was a secret to paste again.

That is the same fault at the next level up, and the same sentence refutes it.

## The decision

The token belongs to the checkout. `./do` mints one into
`.yagit/session-token` the first time it needs one and reuses it afterwards,
for every stack it ever starts. Restarting no longer ends the session.

Rotation becomes something typed: `./do up --new-token`, which replaces the
file, implies `--restart` because a daemon already up is holding the secret
that was just replaced, and says on the way past that every browser holding
the old cookie has to paste again.

## Two token files, and why that is not two definitions of one value

`.yagit/token` stays exactly as it was: written by the daemon at startup,
removed when it stops. It looks like a duplicate of the new file and is not,
because its **presence** is load-bearing. It is how `./do token`, `./do shot`,
`./do drive`, `stackResponds` and the end-to-end suite answer "is a daemon
running?", and the three error messages that send a reader to `./do up` are
built on its absence. Making the persistent file serve both roles would mean
`./do token` printing a secret for a daemon that is not there.

So the two files answer two questions:

| File | Written by | Lives | Answers |
| --- | --- | --- | --- |
| `.yagit/session-token` | `./do` | until rotated | what the secret is |
| `.yagit/token` | the daemon | one stack | a daemon is up, and here it is |

## What was decided against

- **`YAGIT_TOKEN` in `.env`.** The obvious place, and the daemon has honoured
  that variable all along, so it was three lines of work. It was refused for
  what it does to `.env` rather than for what it does to the token. `.env`
  is documented in its own example file and asserted in `project_test.go` to be
  a description of this machine and *not a secret* — it is the file people
  paste into an issue when their setup will not start. Worse, `.env.example` is
  tracked: a value there is a default credential shared by everyone who ever
  clones, and the daemon's floor of 22 characters would wave through
  `changemechangemechangeme`. [ADR
  0014](0014-a-breaking-configuration-change-announces-itself.md) had already
  turned down a `./do doctor` that would write a security-relevant setting into
  `.env` on someone's behalf; this is that shape. The file now says so itself:
  `./do` reads three keys out of `.env` and warns about every other `YAGIT_`
  assignment, so a `YAGIT_TOKEN` written there is named on the way past rather
  than sitting there looking like it works.
- **Expiry.** A token renewed after thirty days bounds the damage of a leak
  without anyone having to act. It also reintroduces the day it stops working
  for no reason the person can see, which is the entire complaint this record
  answers, merely made rarer and therefore more confusing.
- **Reusing `.yagit/token` for both jobs**, above.
- **Storing it outside the checkout**, in `~/.config` or the like. `.yagit/`
  is deliberately the one place a checkout puts what it downloads and what it
  generates, so `rm -rf` on the clone takes all of it ([ADR
  0024](0024-the-checkout-owns-its-toolchain.md)). A secret in the home
  directory outlives the clone it was for.

## What it costs

**A token that leaked once stays valid.** Before, a leak expired at the next
`./do down`; now it expires when somebody types `--new-token`. This is the real
price, it is recorded in SECURITY.md beside the others rather than only here,
and it is why rotation got a flag and a printed sentence instead of being left
to `rm`.

On Windows, mode bits alone do not lock the file down — access is governed by
the ACL — so every write of a secret goes through `protect.OwnerOnly`
([ADR 0035](0035-secrets-get-an-owner-only-acl-on-windows.md)). The exposure
now lasts as long as the checkout rather than as long as one stack.

**A file that a person can corrupt.** `./do` reads a token it did not mint in
this run, so it validates it: 32 bytes of base64url, which is what `mintToken`
produces. Anything else is replaced, with a warning naming the file. Passing a
truncated token through would fail the daemon's own check and blame
`YAGIT_TOKEN`, a variable the reader never set.

The check measures against `mintToken` rather than against the daemon's floor
of 22 characters on purpose: `cmd/do` cannot import `cmd/yagit`, and a second
copy of that constant is a second place for it to drift.

## References

- `sessionToken` and `usableToken` in `cmd/do/project.go`
- `sessionToken` in `cmd/yagit/main.go`, which still generates one when no
  environment provides it — a released binary has no `./do` and no such file
- [SECURITY.md](../../SECURITY.md), "What yagit is, in security terms"
