# 0014 — A breaking configuration change announces itself

**Status:** new.

## The problem

`.env` describes a machine and is never committed, so a rule that narrows what
`.env` may hold breaks checkouts nobody can see. The case in hand: a
non-loopback `YAGIT_PUBLIC_HOST` requires `YAGIT_LISTEN_ALL=1`. The refusal is
right — widening the listen address should be something a person agreed to, not
a side effect of naming a host — and it stops `./do` dead for anyone whose
`.env` already names one.

A release note does not reach them. There is no `CHANGELOG.md`, on purpose: a
release's notes are generated from the pull requests merged since the previous
tag, and a title about sessions or TLS does not read as "this will break your
`.env`". A hand-written changelog would be no better, and it is worth saying
why, because it is the obvious answer. Nobody reads the release notes of a tool
that already works for them. They find out from a terminal, not from a
document.

## The decision

A change that stops accepting a configuration somebody already has announces
itself in three places, ordered here by how likely each is to reach them.

**1. The program refuses, and the refusal says what to do.** This is the only
one of the three that fires at the moment the person is blocked, so it carries
the weight. It names the variable that unblocks them, `YAGIT_LISTEN_ALL`, and
it offers the other way out as well: `comment YAGIT_PUBLIC_HOST out` of
`.env`. Those two phrases are what the test named below pins.

The sentences around them are not reproduced here. They live in
`listenAddress` in `cmd/do/project.go`, they are laid out by `indent` in
`cmd/do/main.go`, and a document that copies the rendering out is a copy
nothing checks — which is the failure this record exists to describe, not one
to commit while describing it.

Naming both ways out is the half worth pinning. A refusal whose only advice is
"listen on all interfaces" would push people into the widening the refusal
exists to prevent — including the ones who set that hostname on a machine they
now browse locally.

**2. The commit carries `!`, or a `BREAKING CHANGE:` footer.** `./do version`
already reads both: below 1.0 they move the minor rather than the major, which
is semver's own rule. A version number that moves is what tells somebody who
reads nothing else that this release is not the previous one with a bug removed.

**3. The pull request is labelled `breaking-change`.** `.github/release.yml`
sorts that label into its own section, above the rest, so a reader who does
open the notes meets it first instead of fifth.

## What was decided against

- **A `CHANGELOG.md`.** It would be a second copy of the pull requests, it is
  the copy nothing checks, and — the point above — it is read by people who are
  not the ones about to be broken.
- **A `./do doctor` that repairs `.env` for you.** `.env` describes your
  machine and is never committed. A command that edits a security-relevant
  setting there on your behalf has the shape of the problem the listen-address
  rule removed: a listen address widened by something other than a decision.
- **A deprecation window that keeps the old behaviour working.** The old
  behaviour widened the listen address without being asked. A grace period is a
  period during which that carries on happening.

## What it costs

Two of the three are applied by hand, and nothing checks either. The label
especially: Dependabot applies `dependencies` to its own pull requests, which is
the only reason that split will still be true in a year, and nobody applies
`breaking-change` to theirs unprompted. A label left off produces exactly the
silence this record is about.

`breaking-change` also has to exist on the repository before anyone can apply
it, and a commit cannot create it — `.github/release.yml` names a label that a
maintainer creates once, by hand, and until then the section is a heading no
pull request can ever land under. The command is in
[CONTRIBUTING.md](../../CONTRIBUTING.md), "Repository settings", so there is
one place to keep it right.

That is why the refusal is first and why it is the one under test —
`TestDaemonEnvironmentRefusesARemoteHostWithoutAcknowledgment` in
`cmd/do/project_test.go` pins that it names the variable that fixes it and that
it offers the loopback too. The sentence a person is actually going to read is
the one worth a test.

## References

- [CONTRIBUTING.md](../../CONTRIBUTING.md), "Releases"
- [`.github/release.yml`](../../.github/release.yml)
- [SECURITY.md](../../SECURITY.md), for the listen-address rule itself
