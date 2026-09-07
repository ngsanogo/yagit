# 0031 — Undo reads the HEAD reflog, and leases by object names

**Status:** new. Extends
[0022](0022-a-merge-is-checked-before-it-runs.md) and
[0028](0028-a-stash-is-a-position-and-a-name.md) to the operation that
reverses something already recorded. Applied to the first undoable action
(last commit); more kinds follow the same shape.

## The problem

Undo is a product promise a graphical git client is expected to keep: a stack
of recent actions, and a preview of what reversing the top one would do.
yagit's roadmap names the feature and says it is built on the reflog.

Two readings of that sentence fight each other:

1. **The UI list is the HEAD reflog.** Every checkout, commit, reset and
   rebase that touched HEAD appears, including ones a terminal ran. Positions
   (`HEAD@{n}`) slide whenever anything else moves HEAD — the same trap
   [0028](0028-a-stash-is-a-position-and-a-name.md) solved for the stash.
2. **The UI list is yagit's own journal** of commands it ran. That matches
   "explicit stack", but invents a second history beside git's, goes silent
   when somebody undoes in a terminal, and still needs the reflog (or the
   object database) to reverse anything that moved a ref.

A third trap sits in the roadmap's capability list: **discard**. Discard never
moves a ref, so the reflog cannot restore it. Treating discard as a reflog undo
would either lie or invent a recovery mechanism the rest of Undo does not
use.

## Decision

**Undo offers the reverse of the most recent HEAD-reflog entry yagit knows
how to reverse.** The entry is identified by the object names at `HEAD@{0}`
and `HEAD@{1}`, never by the index alone. The plan answers with both SHAs;
the run sends both back and refuses with a 409 when HEAD is no longer that
tip, or the previous tip is no longer what the plan read.

The command on screen is the real git that runs — for undoing a commit,
always `git reset --soft <previous>` ([0021](0021-a-shown-command-is-not-a-setting.md)).
Soft is the promise: the commit leaves the branch, and its tree stays in the
index so the work is not discarded. Mixed or hard would be a different
product sentence, and a choice the first slice does not offer.

**Discard is not an Undo action.** The roadmap list that named it is corrected
here: discard stays the operation the confirmation already calls irreversible.
Recovering discarded work later would be a different feature (for example
stashing before discard), not a reflog walk.

**An "explicit stack" in the interface is a later reading of several
classified reflog entries**, each still leased by object names. Shipped so
far: the tip was made by `commit` / `commit (amend)`, by `checkout: moving
from …`, or by `reset: moving to …`. The initial commit (`commit (initial)`)
is refused — deleting HEAD is a different command and a different
confirmation.

**Reversing a reset is a soft reset too, whatever mode the original used.**
git records "reset: moving to <what was typed>" and nothing about which of the
three trees moved, so mirroring the mode would mean guessing — and guessing
`--hard` would overwrite whatever the work tree holds at the moment Undo is
pressed. Soft moves the ref and touches nothing else, which is the only
reverse that cannot take away more than the reset did; what it therefore
cannot do, bring back files a `--hard` discarded, the confirmation says out
loud. A reset whose reverse target is where HEAD already sits is refused
rather than offered: `git stash push` resets to HEAD on its way past, and a
discard typed as `git reset --hard HEAD` leaves the same line, so the guard
is also what keeps discard out of Undo when somebody spells it that way.

## What was decided against

- **A yagit-only journal as the source of truth.** It would miss terminal
  work and invent a history git already keeps.
- **Addressing undo as `HEAD@{1}` alone.** Positions move; object names do
  not. Same reason as 0028.
- **Undoing discard through the reflog.** The reflog has nothing to say.
- **A bare `git reset` or a mixed default.** Soft is pinned so the line on
  screen cannot mean something else on a machine that changed `reset.mode`
  (0021).
- **Hard-reset as undo.** That would discard the work the commit held, which
  is the opposite of what "undo the commit" means in every other client that
  offers it.

## Consequences

- `internal/git` grows a HEAD reflog reader and an undo-commit preview that
  classifies the tip entry.
- The HTTP surface is plan then execute, with `head` and `to` echoed and
  checked, beside the branch name `into` every other branch operation uses.
- Further undo kinds classify other reflog subjects the same way; each gets
  its own pinned command and its own lease fields. Checkout and reset did.
  Branch deletion did not, and could not: it writes no HEAD-reflog entry and
  deletes the branch's own reflog with the branch, so the tip has to be read
  before the command rather than recalled after it
  ([0032](0032-a-deleted-branch-is-remembered-not-recalled.md)).
- The roadmap's Undo bullet no longer names discard.
