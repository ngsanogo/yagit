# 0022 — A merge is checked against the repository before it runs

**Status:** accepted

Extends [0021](0021-a-shown-command-is-not-a-setting.md), which stopped the
user's configuration from deciding what a shown command means. This record is
about the two other ways the same dialog could be wrong: the name in the
command, and the repository underneath it.

## The problem

`merge/plan` reads the two branches, answers the exact command, and the dialog
draws it. Three things could still make the sentence above the button false,
and none of them was the setting 0021 dealt with.

**A name is not a reference.** The plan counted commits with
`git rev-list refs/heads/main...refs/heads/dup` — spelled in full, deliberately
— and then answered the command `git merge --no-ff --no-edit -- dup`. Those are
not the same `dup`. git resolves a short name through the search order in
gitrevisions, which reaches `refs/tags` before `refs/heads`, so a repository
holding both a branch and a tag called `dup` — one release process away from
ordinary — has the plan describe the branch and the merge take the tag. Where
the tag is an ancestor, git prints `Already up to date`, exits 0, and yagit
says "Merged dup into main" over a repository that did not move. Where it is
not, a range nobody was shown gets merged. Reproduced in a scratch repository
before it was fixed.

**A flag cannot hold a sentence up.** The outcome travelled back with the
confirmation and chose the flag that pins it, and that flag was called the
lease: `--ff-only` cannot record a commit, `--no-ff` cannot skip one. But the
dialog for the third outcome says *"main already contains pickup. Merging
changes nothing"*, and `--ff-only` does not refuse to fast-forward. A commit
landing on `pickup` between the plan and the click — another tab, a terminal, a
pull — turned that sentence into a branch moving and a work tree rewritten,
with a toast that then reported "main already had pickup". The lease had a hole
exactly where the sentence promised the most.

**A plan can promise a merge git will refuse.** `rev-list --left-right --count`
reports every commit on each side when two histories have no fork point, which
reads exactly like a wide divergence — so the dialog promised a merge commit
and `git merge` answered `refusing to merge unrelated histories`. A repository
already halfway through a merge or a rebase refused the same way, with
`You have not concluded your merge`.

## What was decided

**A command that acts on a local branch the user named spells it
`refs/heads/…`.** One helper, `git.LocalBranchRef`, and the `--` separator
stays beside it: the two guards cost one argument between them, and neither is
the one that would be missed.

**A merge commit's message is written by the daemon.** Given a full ref git
titles the commit `Merge branch 'refs/heads/dup'`, and `merge.log` decides how
much else goes into it — a setting again, deciding what a commit says. So
`-m "Merge branch 'dup' into main"` is part of the command, which means it is
part of what the confirmation shows. git's own form, minus its special case:
git omits `into main` when the branch merged into is the default one, so the
same operation is described two ways depending on where it happened.

**The plan is read again before the command runs, and the outcome is the
lease.** `merge` re-reads the two branches and refuses with a 409 when what the
merge *is* has changed. The counts are deliberately not part of that lease: a
commit landing anywhere changes "brings 3 commits" without changing what the
command does, and a confirmation that had to be reopened every time anybody
committed would teach people to click through it.

**Both routes refuse what git would refuse, before describing anything.** No
common ancestor is a 409 that says so; an operation already in progress is a
409 that names what to finish first. Both were reachable only as a git failure
after the user had approved a sentence that was false when it was written.

**What the plan does not read is the work tree.** Finding out which local
changes a merge would overwrite means doing the merge. git's refusal names the
files and travels whole, and the boundary is stated in `PreviewMerge`: this
answers what the two *branches* make of each other, and everything it answers
is a promise the run route keeps.

## What was rejected

**Keeping the short name and refusing when it is ambiguous.** It reads well —
"`dup` is both a branch and a tag here" — and it is a worse product: the user
wants to merge the branch, yagit can do it correctly, and refusing teaches
them that yagit is fragile around tags.

**Passing the full ref and leaving the message to git.** One flag less, and
every merge commit in the repository would read `Merge branch 'refs/heads/x'`
forever. The message is not a place to spend the cost of a fix.

**Sending the branch's SHA with the plan and merging that.** It is a tighter
lease — the exact commits the counts were taken from — and it merges a commit
that is no longer the branch. The user clicked a branch.

**Comparing the counts as well as the outcome.** Strictly more honest, and it
turns any commit anywhere into a refused merge. See above.

## What it costs

**One extra `rev-list` per merge**, on top of the one the plan ran. It is the
same single walk, it happens once per click, and it is what makes the sentence
on screen a statement about the repository rather than about a repository two
seconds ago.

**One `merge-base` per plan, and only where the branches have diverged.** The
other two outcomes mean one branch contains the other, so they have met by
definition and the walk is skipped.

**A longer command line.**
`git merge --no-ff --no-edit -m "Merge branch 'x' into main" -- refs/heads/x`
is not a line most people would type. That is the same trade 0021 made and the
same answer: it is longer, and it is complete. `shellQuote` grew a second
quoting style so the message inside it stays a sentence rather than
`'Merge branch '\''x'\'' into main'`.

**A merge can now be refused for a reason that did not exist before**, when a
branch moves while the dialog is open. The refusal names what the merge would
be now, and the row is still there to ask again.

## What would reverse it

git growing a way to ask "what would this command do" without running it, in a
form worth parsing — the same thing that would reopen 0021. The plan's reading
would become git's own, and the lease could be held against git's answer rather
than against a rule about counts.
