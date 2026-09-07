# 0020 — The network is git's, and so are the credentials

**Status:** new. It is the decision phase 8 turns on, and it was written the day
fetch, pull and push arrived.

## The problem

Every operation before this one ran against a repository on this disk and
answered in milliseconds. Fetching, pulling and pushing do not: they wait on
somebody else's server, they need permission to get there, and they are the
first thing yagit does that can fail for a reason that is nobody's fault.

That raises three questions at once, and the tempting answer to each is the
wrong one:

1. **Who holds the credentials?** A client that asks for a password has to keep
   it, and it has no better place to put it than the keyring git is already
   using.
2. **How long may a command take?** The Runner bounds every git command at
   thirty seconds, which is right for `git status` and absurd for a clone.
3. **What does "push" mean when the branch and the remote have both moved?**
   git's answer is a family of options, one of which quietly loses other
   people's work.

## Decision

**yagit authenticates nothing.** git already has a credential subsystem, the
user's ssh agent, their `~/.gitconfig`, their `insteadOf` rewrites and their
proxy. All of it is reached by passing the environment through — by name, one
variable at a time, with the reason it is there — and none of it is reached by
yagit asking for a secret it would then have to store.

`GIT_TERMINAL_PROMPT=0` stays, and it is what makes this honest rather than
lucky: a command with no credentials fails saying so, in git's own words, in
the same second. It does not hang on a prompt nobody can see. `GIT_ASKPASS` and
`SSH_ASKPASS` stay off the inherited list for the same reason — they name a
program git runs to ask a question, and a daemon with no terminal has nowhere
to ask. They arrive, if ever, with an interface that can do the asking.

What was added to the list for these three commands, each with its own line in
`internal/git/environment.go`:

- `HTTP_PROXY` and its three companions, in both spellings. Behind a corporate
  proxy they are frequently the only thing set, and without them a fetch does
  not fail — it hangs until the deadline, blaming a network that works.
- `GIT_SSH_COMMAND` and `GIT_SSH`, which is how a person pins the ssh binary,
  the key or the jump host git should use.
- `XDG_CONFIG_HOME`, where a `~/.config/git/config` lives on the machines that
  keep it there — identity, signing key, credential helper, all of it.
- `DBUS_SESSION_BUS_ADDRESS` on the platforms that have one, which is how
  `git-credential-libsecret` reaches the desktop keyring. Without it the helper
  answers nothing, git falls back to asking, the prompt is refused, and the
  push fails on a machine whose credentials are stored and working.

**A network command carries its own deadline.** `Command.Timeout` overrides the
Runner's, and exactly one value is ever passed to it: `networkTimeout`, ten
minutes. A per-call duration would be a second definition of one policy, and
the copy that gets forgotten is always the one on the command that needed it.
The deadline stays rather than being removed, because the alternative is not
"no limit" but "until the process is killed": a connection that dies without
closing is a case TCP itself can take hours to notice.

The browser has the same number written a second time — `NETWORK_TIMEOUT_MS`,
ten minutes plus the room the ordinary one leaves — because it cannot read a Go
constant, and because the timeout that should fire is the daemon's. The
daemon's can answer with what git said; the browser's can only say that nothing
came back.

**Pushing goes where the branch follows, and the refspec is written out.**
`git push origin main` looks like the request and is not: it sends the branch to
`refs/heads/main` on the other side, so a branch called `main` that follows
`origin/trunk` would quietly create a second branch on the server and push to
that one forever after. The daemon reads `%(upstream:remotename)` and
`%(upstream:remoteref)` — git's own splitting, because a remote may be called
`origin/mirror` and a branch may be called `feat/lanes`, and the joined string
has several readings — and runs `main:refs/heads/trunk`.

A branch that follows nothing is a different operation and gets a different
word: it is **published**, which needs a remote chosen by somebody and records
that choice with `--set-upstream`.

**A force push is `--force-with-lease --force-if-includes`, and never
`--force`.** Two flags rather than one, and the second is not decoration. The
lease is held against the remote-tracking ref, which a fetch moves without
anybody reading what arrived — so a person who fetches, then forces, holds a
lease over somebody else's commit. `--force-if-includes` requires that whatever
the remote had is actually in this branch's history, which is the thing the
user believes when they force.

**A pull says which of the three it is.** git decides between a merge and a
rebase from `pull.rebase`, and warns when it is unset, because the right answer
depends on the repository and on the team. A button cannot read a setting and
still say what it does, so `--ff-only`, `--no-rebase` and `--rebase` are three
operations named on screen, and the flag is passed every time — including the
one that matches git's default.

The default is the fast-forward. It is the only one that can neither write a
commit nobody asked for nor stop halfway on a conflict in the middle of
somebody's afternoon; when the two have genuinely diverged git says so and
stops, and that refusal is the moment the other two become a question worth
putting.

## What was decided against

- **A login form, and a credential store of yagit's own.** It would duplicate
  the keyring on every platform, and it would be the only place in this
  application where a secret is held by something other than the tool that
  needs it. The daemon runs as the user; git run by that daemon can already
  reach everything git run by that user can.
- **Inheriting the whole environment.** It is the one-line version of the list
  above and it hands git several dozen `GIT_*` switches nobody chose, plus the
  agents that make an unauthenticated command succeed by accident. The
  allowlist stays an allowlist ([0002](0002-drive-the-git-binary.md) is the
  decision it serves).
- **Removing the deadline for network commands.** See above: what replaces it
  is not patience but a request nobody ever gets back.
- **Streaming git's progress on the session event stream.** Deferred here to
  cloning; decided in [0030](0030-progress-rides-the-request.md) as NDJSON on
  the request that started the command, not on `/api/events`.
- **Fetching on a timer.** Some clients do, and it is why their counts are
  usually right. It is also a network request made on the user's behalf, on
  their bandwidth, against a server that may count it — and it is what makes
  `--force-with-lease` alone unsafe, since the lease moves without anybody
  looking. Fetch is a button here, and `--force-if-includes` is what makes that
  choice free.
- **Offering the remaining `git remote` verbs now.** Adding, renaming and
  removing a remote is configuration editing, it is rare, and it has no
  destructive edge that a client makes safer. The list is read and shown; the
  three commands that use it are the phase.

## Consequences

- `--force-if-includes` needs **git 2.30** (January 2021). It is the only
  version floor yagit has, it applies to one operation, and a git that predates
  it refuses the push by name rather than doing something else.
- Remote URLs are **redacted where they are parsed**, not where they are drawn:
  `https://ada:ghp_…@example.test/x.git` is a working remote and a common one,
  and this list goes into a JSON route and onto a screen. Nothing in
  `internal/git` returns a URL that can be handed back to git, which is what
  makes the redaction impossible for a later caller to forget. A userinfo with
  a colon keeps its user name and loses the password; one without a colon is
  removed whole, because nothing in the string says whether it is a name or a
  token.
- The interface gained a component: `web/src/components/Select.tsx`, native,
  for the choice whose length the user's repository decides. Two or three
  options that fit on screen are still a `SegmentedControl`, and a handful of
  actions are still a `Menu`.
- The counts on the Fetch/Pull/Push bar are read from `git status`, which is
  polled ([0015](0015-the-work-tree-is-polled.md)), so they say what was true at
  the last fetch. That is why the Pull button stays live with nothing behind it:
  `git pull` fetches before it integrates, and pressing it is how somebody who
  has not fetched finds out.
