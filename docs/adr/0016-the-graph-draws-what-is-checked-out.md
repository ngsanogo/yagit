# 0016 — The graph draws what is checked out, and every ref is a choice

**Status:** new. It takes the product decision [0012](0012-lanes-are-assigned-in-the-daemon.md)
found and deliberately did not take.

## The problem

The history was always `git log --all`, and 0012 measured what that costs: git's
own repository needs **280 columns** when every ref is drawn, and git's own
`--graph` needs 67 over the identical commit order. Neither is a readable
picture, so the interface refuses to draw one past 24 columns and says so.

The refusal is right. What was wrong is how ordinary it is. A thousand tags and
a hundred topic branches is not an exotic repository — it is any repository with
a few years and a release process behind it — and on every one of them the
centrepiece of the screen was a sentence explaining its own absence. The graph
is the object this application exists to show, and it was missing on exactly the
histories the README uses as its performance claim.

## Decision

The graph is drawn from **what is checked out**: `git log HEAD`, the current
branch, or the commit a detached HEAD sits on. `--all` is still available and is
now a choice, made on screen, beside the history it changes.

`internal/git` names the two as a `Scope`. It travels up through
`internal/history` to `?scope=` on the commits route, and the interface sends it
on every request rather than leaning on the daemon's default — the choice is
visible, so the request says which of the two it is instead of letting an
omission stand for one.

The 24-column bound stays exactly where it was. It is no longer the answer to
"this repository has too many refs", which is now a question the person can
answer; it is the honest refusal for the day the set they chose is itself too
wide.

**What it costs.** Two walks per repository instead of one, and the interface
now has a control it did not have. The default no longer shows a branch that has
not been merged, which is a real loss — it is one click away, and the loss was
already total on any repository whose graph did not fit.

## Which refs, exactly

`HEAD` rather than a list of the current branch and its upstream. It is one
revision, it needs nothing read before it can be built, and it is right in every
state a repository can be in: on a branch it is the branch, detached it is the
commit, and mid-rebase it is where the rebase has got to. A set assembled from
the branch plus its upstream plus its tags would have to decide what to do in
each of those, and every answer would be a guess about what somebody meant.

The empty repository is answered from state, not from git's reply. An unborn
branch has no commit to start from, and `git log` there exits 128 — which is
also what a misspelled revision does, and the message differs between git
versions. So the daemon reads HEAD and the refs first and decides on those: no
HEAD is an empty history under the current branch; no HEAD and no ref is an
empty history under every ref. Nothing is inferred from an exit code, which is
what keeps a genuine failure reaching the user with its command and its stderr
intact.

The same reading fixed a case the ref list alone got wrong: `--all` covers HEAD
as well as `refs/`, so a repository whose every branch has been deleted under a
detached HEAD does have a history, and testing the refs alone drew nothing
there.

## What holds it: a cache per repository *and* per scope

0012 keyed the held assignment by the repository and fingerprinted it with the
refs. Neither is enough now.

The fingerprint gains HEAD. A checkout moves HEAD without moving a single ref,
and both scopes walk from it — one starts there, the other includes it — so the
refs alone would leave the picture on the branch you just left.

The key gains the scope. The two scopes are not two views of one answer: a
commit's column follows from every commit above it, so the commits one scope
leaves out move the ones they share. Holding both is what makes the control a
switch rather than a reload — a person comparing the current branch against
every ref would otherwise pay for the whole walk on every press. It costs the
memory of a second walk, on repositories where someone asked for both, and 0012
already says where to bound that if it ever needs bounding.

## What was decided against

- **A picker naming individual refs.** It is the fuller version of this same
  decision, not a second one, and it can be built on this `Scope` without
  moving anything. Two options is what makes the picture exist again; a list
  with checkboxes beside a thousand tags is a screen of its own.
- **Collapsing lanes leftward, as git's `--graph` does.** It would take 280
  columns down to about 67, which is still not a picture, so it buys nothing
  here — and it costs what makes a page drawable on its own, an edge running
  down one column for its whole length (0012). Still worth revisiting for a
  repository that lands between the figures; it is no longer the only thing
  standing between that repository and its graph.
- **Raising the 24-column bound.** It moves the line between "unreadable" and
  "refused" without putting a readable picture on either side of it.
- **A single button whose label changes.** It has to say what is true and what
  will happen in the same word, and the person reading it cannot tell which. The
  control is a radio group with both options on screen, which is what the choice
  actually is.

## Consequences

- `git.Log` — the whole history, every ref — stays, and is what the tests and
  the graph's checks against real repositories use. Production goes through
  `git.LogScope`, which takes the refs and HEAD the caller already read.
- A scope the daemon does not know is refused with 400 rather than read as the
  default, for the reason a nonsense page number is: answering a history that
  looks entirely correct to a client that asked for a different one hides the
  defect for good.
- `web/src/components/SegmentedControl.tsx` is a new component of the design
  system, on the showcase page with the rest.
- The roadmap's "choosing which refs the graph draws" is no longer a target
  feature that does not exist.
