# 0023 — A rebase is read in both directions, and says what it takes away

**Status:** accepted

Extends [0021](0021-a-shown-command-is-not-a-setting.md) and
[0022](0022-a-merge-is-checked-before-it-runs.md). 0021 anticipated this one by
name — "the next operation — rebase, cherry-pick, revert — meets the same
question" — and rebase met it, plus three that merge never had to answer.

## The problem

The first rebase was written from merge's shape and read the two branches with
one number: `git rev-list --count refs/heads/onto..refs/heads/from`, the
commits on the branch that the upstream does not have. Non-zero was a replay;
zero was "nothing to do". Both readings were wrong, and they were wrong in
opposite directions. Each was reproduced in a scratch repository before it was
fixed.

**A branch AHEAD of the upstream was promised a rewrite git refuses to
perform.** `main` at one commit, `feature` three past it. The count is 2, so
the dialog said *"feature would replay 2 commits onto main"*. `git rebase`
answers `Current branch feature is up to date.`, writes nothing, and exits 0 —
because it decides this by merge base, not by counting: the upstream is an
ancestor of HEAD, so there is nowhere to move to. The toast then reported a
rebase over a repository that had not moved. This is the ordinary case — a
feature branch whose base has not moved since — so it was the most common
click in the feature.

**A branch BEHIND the upstream was called a no-op while git rewrote the work
tree.** `feature` at one commit, `main` three past it. The count is 0, so the
dialog said *"feature already sits on main. Rebasing changes nothing"* — and
the button under that sentence ran the command shown, git answered
`Successfully rebased and updated refs/heads/feature`, the branch moved to
main's tip and every file under it changed. The re-read of
[0022](0022-a-merge-is-checked-before-it-runs.md) could not catch either one:
the plan and the re-read compute the same wrong outcome from the same wrong
rule.

**The count was not what git replays.** `--no-rebase-merges` discards the merge
commits inside the range rather than recreating them, so a branch of four
commits including one merge was described as "4 commits replayed" while git
wrote three — and nobody was told that a merge commit was about to disappear
for good.

**Nothing on the confirmation said a rebase destroys anything.** It ran through
`ConfirmDialog` with no `destructive` and no `losing`, so the operation that
rewrites every commit on a branch got the same non-destructive dialog a merge
gets. A user rebasing a branch they had already pushed was told only that hooks
run and a conflict is possible.

**`rebase.updateRefs` moved branches nobody named.** With the setting on, a
plain `git rebase` force-updates every other branch pointing into the replayed
range. It is a good feature and a good workflow; performing it silently under a
dialog that named one branch is exactly the failure 0021 exists to stop, and
none of the three flags the command already carried touched it.

## What was decided

**The two branches are read in both directions, by merge's rule.**
`git rev-list --left-right --count refs/heads/from...refs/heads/onto`, one
walk, both numbers — the same command `merge/plan` runs, in the same order:
nothing arriving means nothing happens, nothing of our own means the branch
simply moves, both means the operation proper. So a rebase has three outcomes
and not two, and they are merge's three: `up-to-date`, `fast-forward`,
`rebase`. "The upstream is an ancestor of the branch" and "the upstream holds
no commit the branch lacks" are the same sentence, which is why one rule
answers what git decides with a merge base.

**What the confirmation counts is what leaves the branch.** Two numbers:
`rewriting`, the ordinary commits the branch holds past the fork, and
`flattening`, the merge commits among them — one more `rev-list` for the
second, and subtraction for the first, so they cannot disagree about the total.
Every one of them stops being what the branch points at, whether git writes it
again under a new hash or drops it as a patch already upstream. It is
deliberately not a prediction of git's replay loop: git also drops commits that
come out empty against the new base, which nothing can know without doing the
rebase, so an exact count is not available before the command runs and the
number that is available errs towards warning about more rather than less.

**The replay is destructive, and the dialog says what it takes.** `destructive`
and a `losing` list — the commits the branch points at now, and the merge
commits a straight-line replay does not recreate — and a red menu item beside
the delete. The other two outcomes get neither: an up-to-date rebase writes
nothing and a fast-forward moves a pointer, and a red panel over an operation
that loses nothing is how people learn to read past the one that does.

**`--merge` and `--no-update-refs` join the flags that pin what configuration
would decide**, beside `--no-autosquash`, `--no-autostash` and
`--no-rebase-merges`. `--merge` is the one 0021 did not anticipate, because a
merge has no equivalent: `rebase.backend` chooses between two programs, and the
older `apply` backend rewinds and re-applies patches, resolves conflicts
differently, and does not implement `--update-refs` at all — so a repository
setting it would get a different operation and a silently ineffective flag from
the same command line. `rebase.forkPoint` is deliberately not in the list and
is the one place a setting is left alone: it can only turn fork-point off, and
naming an upstream on the command line — which every rebase here does — already
does that. Pinning it would be a flag that reads as load-bearing and is not.
`TestRebaseIgnoresForkPointWhereAnUpstreamIsNamed` is what keeps that true.

**`--no-ff` is the lease, on the replay only.** git offers a rebase no
`--ff-only`, so the argument list can hold up one of the three outcomes and not
the other two: without `--no-ff`, a commit landing on the upstream between the
re-read and the exec turns an approved replay into "Current branch is up to
date" and nothing at all. Passing it on the other two would be worse than
passing nothing — on an up-to-date rebase `--no-ff` means "rebase forced",
which rewrites every commit on the branch under a new hash while the sentence
said nothing would happen. What holds those two up is the re-read alone, and
the gap is named in `RebaseArgs` and on the route rather than papered over.

**Unrelated histories are not refused.** 0022 said "both routes refuse what git
would refuse", and this is what that rule answers here: `git merge` stops at
`refusing to merge unrelated histories` and `git rebase` replays the branch onto
the other root without a word. Refusing would be yagit inventing a rule git
does not have, and the plan says something louder than a refusal anyway — every
commit the branch has ever held, about to be written again somewhere it has
never been.

**The four questions merge and rebase both ask live in one file.**
`internal/api/branchop.go`: is HEAD on a branch, is the repository free to start
anything, is the branch named a different one, is the plan still true. They had
been written twice, in prose differing only by the word for what was about to
happen — two sentinel errors character for character identical, and one arm of
`statusForOperationError` growing by three per operation. Cherry-pick and revert
are the next two, and they ask the same four.

## What was rejected

**Keeping two outcomes and special-casing the ahead branch.** It fixes the
symptom and leaves the model wrong: the fast-forward is a real thing git does,
with a real sentence to say about it ("nothing is replayed, and every file
under the branch changes"), and folding it into either neighbour loses that
sentence.

**Counting with `--cherry-pick`, git's own recipe for the replay set.**
`git rev-list --right-only --cherry-pick --no-merges onto...from` is what the
sequencer builds, and it is wrong in the direction that matters: two initially
empty commits are patch-equivalent to each other, so a branch holding one is
counted as replaying nothing while git replays it and says so. Under-warning is
the one error this number must not make.

**Refusing the up-to-date rebase without running git.** The command is on the
confirmation and the button under it must run that command; showing a line and
not running it is the same lie by a shorter route. A plain `git rebase` there
prints `Current branch is up to date` and writes nothing, which is what the
sentence promised.

**Passing the upstream's SHA instead of its name, as a tighter lease.** Rejected
for the reason 0022 rejected it for merge: it rebases onto a commit that is no
longer the branch, and the user clicked a branch.

**Offering interactive rebase here.** `-i` needs an editor the daemon has no
terminal to open. It is phase 9, and it is a different screen rather than a
flag on this one.

## What it costs

**A git floor of 2.38 for one operation.** `--no-update-refs` landed in git
2.38 (October 2022); older versions refuse it by name. That is the second place
in yagit that asks for more than the 2.30 floor, after `--force-if-includes`,
and both are recorded in README.md. The alternative was leaving one setting able
to force-update branches the confirmation never mentioned.

**One extra `rev-list` per plan, and only on a replay.** The other two outcomes
write no commit, so there is nothing to split and the walk is skipped — the
same shape as 0022's merge-base, which is spent only where it can change the
answer.

**A longer command line.**
`git rebase --merge --no-autosquash --no-autostash --no-rebase-merges
--no-update-refs --no-ff -- refs/heads/main` is not a line anybody would type.
Same trade as 0021 and the same answer: it is longer, and it is complete.

**A count that can be larger than what git replays.** Named above, and it is
the deliberate direction. The `losing` list is written to be true either way:
the commits it names stop being what the branch points at whether git rewrites
them or drops them.

## What would reverse it

git growing `--ff-only` for rebase, or a way to ask what a rebase would do
without doing it. The first would close the lease's two open outcomes; the
second would replace the count with git's own answer and make the "warns about
more than happens" cost disappear. It is the same thing that would reopen 0021
and 0022, and it would reopen all three at once.
