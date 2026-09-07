# 0021 — A command the interface shows is a command configuration cannot change

**Status:** accepted

## The problem

yagit's central promise is that the exact git command is on screen before a
destructive operation runs, and in the log panel after every one. `git merge --
pickup` in a confirmation dialog looks like it keeps that promise. It does not.

`git merge` with no flag asks `merge.ff`. On a machine where it is unset the
branch pointer moves and nothing is written. On a machine where somebody set
`merge.ff = false` — a common house style, because it keeps feature branches
visible in the graph — the same click records a merge commit: it runs the
user's `pre-commit` and `commit-msg` hooks, signs the commit if `commit.gpgsign`
is on, and can sit waiting on a pinentry the daemon cannot show. On a machine
with `merge.ff = only` the same click refuses outright.

One dialog, one line of text, three operations. The line was true about the
argument list and false about everything a person actually cares about.

`git pull` had already been met with this and answered: `pullRequest.Strategy`
is required, and `PullArgs` appends `--ff-only`, `--no-rebase` or `--rebase`
every time, because "a button cannot read a setting and still say what it
does". Merge was written afterwards and did not carry the answer over. That is
what makes this worth recording rather than fixing quietly: the next operation —
rebase, cherry-pick, revert — meets the same question, and "there is a rule" is
worth more than three routes that each remembered.

## What was decided

**No command the daemon runs may have its meaning decided by the user's
configuration.** Where git resolves a question by reading a setting, yagit
resolves it first and passes the flag that pins the answer.

Two shapes follow from that, and which one applies is decided by where the
answer legitimately comes from.

**When it is a choice, it travels with the request.** Whether a pull rebases is
a matter of taste and of the team; nothing in the repository decides it. So the
three are three named operations, the client says which, and the daemon passes
the matching flag.

**When it is a fact, the daemon reads it and the client is told.** Whether
merging one local branch into another is a fast-forward is not an opinion: it
is where the two branches stand. So `merge/plan` reads them —
`git rev-list --left-right --count refs/heads/a...refs/heads/b`, one walk, both
numbers — and answers what the merge *is*: `fast-forward`, `merge-commit` or
`up-to-date`, with the counts and the exact command for that outcome. The
confirmation shows the command and says the same thing in words, because "3
commits arrive and nothing is committed" is not something an argument list
says. The outcome goes back with the request and chooses the flag:
`--ff-only`, or `--no-ff --no-edit`.

**The flag is also the lease.** This is the part that pays for itself twice.
A fast-forward that stopped being one while the dialog was open — somebody
committed in a terminal — is refused by git rather than quietly recorded as a
merge commit nobody was shown; and a merge commit stays a merge commit even
where a fast-forward became possible. The command on screen cannot become a
different operation between being read and being run, which is the same guard
`--force-with-lease` gives a push and `operation` gives an abort.

`--no-edit` belongs to the same rule. `git merge` opens an editor on the message
it prepared whenever it believes somebody is watching; the daemon has no
terminal, so what it believes is an implementation detail of tty detection.
Saying `--no-edit` makes the outcome a property of the command rather than of
the environment it was run in.

## What it was decided against

**Reading `merge.ff` in the daemon and showing the command it implies.** This
keeps the user's configuration authoritative, which is superficially the
respectful answer. It loses the lease — the setting can change, and so can the
relationship between the branches — and it makes the daemon a second
interpreter of a file git already interprets. Two readings of one setting is
one bug.

**Putting the choice in the dialog: a "create a merge commit anyway" box.**
This is a real workflow (`--no-ff` on every feature branch). It was rejected
for the first shipping of merge because it makes the model two-dimensional —
the fact, plus a choice over it — for a question most people answer once in
their life through `merge.ff`. Phase 12 opened the door: the plan round trip
re-asks when the box flips, exactly as the publish dialog re-asks when the
remote changes, and the command is still answered rather than assembled.

**Leaving `merge` alone because "git's default is fine".** git's default *is*
fine. It is not a default when the user has changed it, and the dialog is not
allowed to be wrong on those machines only — those are the machines whose
owners most know what a merge commit costs.

## What it costs

**One extra `git rev-list` per confirmation.** It walks the symmetric
difference of two branches, which is bounded by how far they have diverged, and
it happens once, when a dialog opens.

**Two commands where git has one.** `git merge --ff-only -- x` and
`git merge --no-ff --no-edit -- x` both appear in the log panel, and neither is
the line a user would have typed themselves. That is the trade: the line is
longer and it is complete.

**The preference costs a second plan round trip when the box flips.** That is
the same cost the publish dialog already pays when the remote changes, and it
is why the command on screen never describes a preference other than the one
selected.

## What would reverse it

A version of yagit that stops showing commands — that decides the exact line
is developer trivia and puts "Merge" over an operation it describes in prose
only. Every argument here rests on the promise that the line is exact; drop the
promise and the setting can go back to deciding.

More plausibly: git grows a way to ask "what would this command do" without
running it, in a form worth parsing. Then the reading here becomes git's own
rather than a rule about counts, and this record needs writing again.
