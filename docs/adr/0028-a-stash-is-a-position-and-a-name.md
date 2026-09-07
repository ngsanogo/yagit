# 0028 — A stash is named by a position and checked by an object name

**Status:** new. Extends
[0022](0022-a-merge-is-checked-before-it-runs.md) — the plan is checked against
the repository before the command runs — to the one operation whose plan cannot
be checked the way every other one is. Applied.

## What was decided before

0021 established that a command the interface shows must be a command
configuration cannot change, and 0022 that the plan behind it is read again
before it runs. 0023 extended that to a rebase. The mechanism has been the same
in every case since: **the plan resolves a revision to a full object name, and
the run sends that name back.** `agreesOnCommit` compares it against what the
daemon resolves now, and a mismatch is a 409.

That works because the object name *is* the argument. Cherry-picking
`a2801ba` sends `a2801ba` to git. If the name still resolves to the same
object, the command the user approved is the command that runs, and if it does
not, nothing runs.

## What a stash breaks

A stash has no name of its own.

It is a commit held in a reflog under `refs/stash`, and it is addressed as
`stash@{0}`, `stash@{1}`, `stash@{2}` — a **position in a stack**. Position 0
is whatever is most recent. So the address of a given stash changes whenever
another is pushed on top of it, or one below it is dropped out of the middle.

The obvious fix is to send the object name instead, and git refuses it:

```
$ git stash drop 4d1b104
error: '4d1b104' is not a stash reference
```

Which is correct on git's part. Dropping is an edit of a reflog, not of the
object database — there is no object to delete, only an entry to remove — so
the entry is what has to be named.

That leaves the gap this record exists for. A confirmation reading

> Drop “the older one”?
> `git stash drop 'stash@{1}'`

is drawn against a stack at one moment and pressed at another. In between, a
terminal in the next window runs `git stash push`. Every entry moves down by
one. The command on the dialog is still perfectly valid, still runs, still
exits 0 — and throws away work the person looking at the screen never saw.

Nothing in 0022's mechanism catches it, because there is no name to compare.

## The decision

**Every write names a stash twice: by position, and by object name.**

The plan resolves the position to the object at it and answers with both. The
run sends both back. `git.StashTarget` reads the list again and refuses, with a
409, when that position no longer holds that object:

```
this stash is no longer at that position: stash@{1} now holds 49836a9,
not c767a21 — read the list again
```

The position is what reaches git, because it is the only thing git takes. The
object name never reaches git at all: its whole job is to be compared. It is a
lease on a number, and it is checked against a fresh `git stash list` rather
than against anything the client kept.

Refusing a request that names no object is part of it, for the reason
`agreesOnBranch` refuses an empty branch: a client that names none is a client
that did not look.

**Reading is deliberately not held to this.** `GET /stashes/{index}` takes a
position and nothing else. Nothing is at stake if the stack shifted between the
click and the answer — the worst outcome is looking at a different stash — and
the answer carries the stash it actually read, so the panel titles itself from
that rather than from the row that was clicked. Demanding the pair there would
turn an ordinary race into a 409 for somebody who only wanted to look.

## What it is decided against

**Addressing stashes by object name throughout, with drop as a special case.**
`git stash apply` and `git stash show` both accept a raw object name, so two of
the four operations could have skipped the position entirely. Rejected because
it would leave one operation guarded and three unguarded against the same
hazard, and because it makes the interface's addressing depend on which
subcommand happens to be lenient — the sort of rule nobody can hold in their
head while adding the fifth operation.

**Refusing to renumber: keeping a client-side identity for each stash.** There
is no such identity to keep. A stash's object name is not unique — the same
tree stashed twice in the same second with the same message produces the same
commit — and nothing about a reflog entry is stable across a `git stash drop`
run anywhere else.

**Locking, or a daemon-held handle.** yagit drives the `git` binary against a
repository the user is also using from a terminal (ADR 0002). Any handle it
minted would be a claim about a repository it does not own. The lease above
makes no such claim: it re-reads, and it is right whether the stack was moved
by this daemon, another yagit, or somebody typing.

## What it costs

Two round trips where one would do. The plan reads the list to resolve the
position; the run reads it again to check it. Both are `git stash list`, which
is a reflog walk over a handful of entries, and the alternative is a
confirmation that can act on the wrong work.

A refusal people will occasionally see for a reason that is not their fault:
they opened a dialog, something else touched the stack, and the button they
pressed answered "read the list again". That is the correct outcome and it is
still an interruption. It is made as small as it can be — the list on screen is
written from every operation's own answer, so it is never stale for longer than
one response — but a repository two programs are editing can always move
between a question and its answer, and saying so beats acting on a stale
number.

## What follows from it

The stash list is not decoration. Because the position is identity, a list that
is stale for even one render is a list whose rows carry the wrong number, so
every write answers with the whole stack and the client writes it into the
cache rather than asking again. The open stash's own patch is keyed by position
too, and is therefore dropped by every one of those operations rather than
written — a cached answer under a renumbered row is exactly the confusion this
record is about.
