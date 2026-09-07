# 0032 — A deleted branch is remembered, not recalled

**Status:** new. Corrects one sentence of
[0031](0031-undo-reads-the-head-reflog.md), which expected branch deletion to
be another reflog subject to classify. It is not one, and no amount of reading
makes it one.

## The problem

Phase 10's Undo list names branch deletion. 0031 decided that Undo reads the
HEAD reflog and leases by object names, and closed by saying "further undo
kinds (checkout, reset, branch deletion) classify other reflog subjects the
same way". Two of those three do. The third has no subject to classify:

```
$ git branch -D feature
Deleted branch feature (was cbe7d2f).
$ git reflog show HEAD          # unchanged
$ ls .git/logs/refs/heads/      # feature's own reflog is gone with it
main
```

Deleting a branch does not move HEAD, so HEAD's reflog says nothing. The
branch's own reflog — the one place the tip was written down — is deleted along
with the branch. After the command, the commit `feature` pointed at is reachable
from nothing and named by nothing. It is still in the object database until
`git gc` runs, and there is no supported way to ask git which object it was.

The one place the answer survives is git's own stdout: `(was cbe7d2f)`. Reading
that back would mean parsing an English sentence whose wording is git's to
change and whose language is the user's locale to choose — the sort of thing
this codebase refuses everywhere else.

## Decision

**yagit reads the branch tip before it runs the delete, and keeps it in memory
for as long as the repository stays open.** `handleDeleteBranch` resolves
`refs/heads/<name>` first; on success it records the name, that object, and
where HEAD stood at the time. Undo offers `git branch -- <name> <sha>`, shown
before it runs like every other operation.

**The record beats the reflog while HEAD has not moved, and loses the moment it
has.** Undo shows one action, so the two sources have to be ordered, and this is
the ordering rule — no clock on either side. It is exact rather than
approximate: a valid record means nothing has moved HEAD since the deletion, and
every entry the HEAD reflog could offer instead *is* a HEAD movement, so
anything the reflog has to say is necessarily older. Comparing timestamps was
the alternative, and it would have meant trusting a per-entry reflog time that
git records to the second and that the format yagit already parses reports for
the *commit* rather than for the entry.

**A failed read does not stop the delete.** The user asked for a deletion; if
the tip cannot be resolved first, the deletion happens and the undo offer is
what is missing. It is logged, never swallowed.

**In memory, and never on disk.** The record is worth exactly as long as the
repository is open in yagit, which is the same span "the last thing you did"
means to somebody looking at the screen. Written to disk it would survive into a
session where `git gc` has since collected the commits, and offer a restore that
cannot happen.

**Two refusals that only this kind has.** A branch of that name existing again
(`ErrBranchIsBack`) and the object having been collected
(`ErrDeletedWorkIsGone`) are both read before the offer is made and again before
the command runs — the second time because a confirmation can sit open while
somebody else works in a terminal.

## What was decided against

- **Parsing `Deleted branch feature (was cbe7d2f).`** git's wording, git's
  locale, and yagit's only record of an object nobody else can name.
- **`git fsck --lost-found`, or walking the object database.** It finds every
  dangling commit in the repository, not the one this branch held, and it is
  minutes of work on a large repository for a button that must answer at once.
- **Refusing to offer it at all**, on the grounds that the delete confirmation
  already shows `git branch -D` and names what will be lost. Defensible, and it
  was the first shape — but Undo is the feature a graphical client is expected
  to keep, and a client that can undo a commit but not the far more frightening
  operation next to it has the promise backwards.
- **Persisting the record**, or keeping a stack of several deletions. Undo
  offers one action; a second deletion replaces the first, exactly as a second
  commit puts the first out of reach of a single Undo.
- **Extending the record to other unreflogged operations** (branch creation,
  `git stash push`, a remote removed). Each would be its own decision about
  what a reverse means; none of them destroys anything.

## Consequences

- `internal/api` grows one small piece of state — `deletions`, keyed by
  repository id, dropped when the repository is closed.
- `internal/git` grows `BranchTip` and `PreviewUndoBranchDeletion`; neither
  knows the record exists, and the second takes the three facts it needs as
  arguments.
- `UndoPreview` grows a `Branch` field, empty for every reflog kind.
- Undo is no longer only a reflog reading, and 0031's closing sentence is
  narrowed to the two kinds it correctly predicted.
