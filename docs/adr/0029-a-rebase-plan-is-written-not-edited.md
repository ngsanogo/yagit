# 0029 — A rebase plan is written by the daemon, not edited by a person

**Status:** new. Extends
[0021](0021-a-shown-command-is-not-a-setting.md) — what a command does must not
be left to configuration — to the one git command whose behaviour is not in its
arguments at all. Applied.

## The problem `-i` poses

Every other git command yagit runs is fully described by its argument list.
`git merge --no-ff --no-edit -- refs/heads/x` says what it will do; show that
line and the user knows what they approved.

`git rebase --interactive` does not work that way. Its arguments say almost
nothing. What it actually does is in a **todo list**, and git obtains that list
by writing a default one to a file and running a program over it:

```
GIT_SEQUENCE_EDITOR "$GIT_DIR/rebase-merge/git-rebase-todo"
```

Whatever the file holds when that program exits is what git executes. So there
are two questions, and neither has an obvious answer for a daemon:

1. How does a plan assembled in a browser reach that file, when a daemon has no
   terminal to open an editor in?
2. Which instructions may be offered, given that some of them ask git to open a
   *second* editor over a commit message?

`internal/git/rebase.go` answered both by refusing: *"Interactive rebase is not
offered here; `-i` needs an editor the daemon has no terminal to open."* That
was the right answer while there was no plan to write. It is not the right
answer now, and this record replaces it.

## Decision 1 — yagit writes the todo list, and is its own editor

The plan is assembled on the confirmation, sent to the daemon as object names
and verbs, and written to a temporary file by `internal/git`. `GIT_SEQUENCE_EDITOR`
is then pointed at **this process's own executable**, invoked with a private
flag and the two paths:

```
'<path to yagit>' --write-rebase-todo '<path to the plan>'
```

git appends the todo file and runs the line; the program copies one over the
other and exits. `git.RunAsEditor` is the whole of it — one entry point for
both editors git may run — and it has three callers: `cmd/yagit`, and the
`TestMain` of each test package that drives a rebase. So the thing under test
and the thing that ships are one function, and adding an editor changes one
place rather than three.

Three alternatives were rejected:

- **`cp <plan>` as the editor.** Works on Linux and macOS, and bets on a
  program Windows does not have outside git's own bundled shell. Every platform
  yagit ships for is a gate ([0010](0010-every-shipped-platform-is-a-gate.md)).
- **A script written next to the todo.** A file to make executable on two
  platforms and impossible on the third.
- **Reimplementing the rebase as a series of cherry-picks.** Loses `ORIG_HEAD`,
  loses `--abort`, and turns a stopped rebase into a stopped cherry-pick that
  the banner, the state reader and the resolution screen would all describe
  wrongly.

The daemon's own binary is the one program certain to be present wherever the
daemon runs, needing no PATH lookup and nothing installed alongside.

### What this costs, exactly

git runs an editor setting as a **shell command** — that is what the setting
is, documented and long-standing — and it appends the filename without quoting
anything. So this is the one string in the project that a shell interprets, and
its parts are quoted here rather than assumed to be free of spaces: an
installation path like `/Applications/My Apps/yagit` would otherwise have had
its first word run as a program.

The rule everywhere else is untouched, and the distinction is worth stating
plainly because SECURITY.md makes a claim that has to stay true: **yagit still
never invokes git through a shell.** Every git command is an argument array.
What is new is that git invokes an editor through a shell of its own, over a
string yagit quotes correctly.

## Decision 2 — every instruction commits a message that already exists

The second editor is the harder half. `squash` and `reword` both make git open
`GIT_EDITOR` over a commit message that **does not exist yet** — the two
messages joined, or the old one waiting to be replaced. A daemon cannot show
that message, and `state.go` already refuses to continue a rebase that contains
one:

> This rebase has a squash in it, which writes a commit message. yagit has no
> editor to show you that message in, so finish this one in your terminal.

Offering `squash` from yagit's own dialog and then refusing to continue the
rebase it started would be the interface arguing with itself. So the
instruction set was chosen by the rule rather than around it:

| Offered | git verb | The message it commits under |
| --- | --- | --- |
| Keep | `pick` | the commit's own |
| Combine into the one above | `fixup` | the one above's |
| Combine, keep this message | `fixup -C` | this commit's |
| Stop to amend | `edit` | the commit's own, then whatever the user amends it to |
| Drop | `drop` | nothing is committed |

`squash` is not offered. Its question — "what should the combined message say?"
— is answered instead by the two that have an answer: whose message survives.
`fixup -C` is what makes that a real choice rather than a restriction; the case
of the flag is its whole meaning, since `-c` opens an editor and `-C` does not.

What enforces the rule is not a second check but **an editor that refuses**.
`GIT_EDITOR` is pointed at this same executable under `--refuse-message-editor`,
a flag whose whole behaviour is to print why and exit non-zero. A plan that
ever reached git with a step needing a message fails at once, with yagit's own
sentence in git's stderr, so the invariant holds even if `CheckPlan` is one day
wrong.

### Why not simply leave `GIT_EDITOR` unset

That was the first answer, and it rested on git's *"Terminal is dumb, but
EDITOR unset"* — which is what git does on Linux and macOS. It is not what git
does on Windows. There git falls back to an editor from its own bundled
environment, which with no terminal to draw in **waits**: the test written to
prove this invariant hung for the full ten minutes on the Windows job and was
killed, and a daemon would have done the same, then been killed halfway through
a sequence.

A hang is strictly worse than a failure — it holds the request open, it leaves
a half-replayed rebase behind, and it reports a timeout about a plan that was
refused. So the invariant is defended by a program yagit ships rather than by
a platform's behaviour, which is only ever defended on the platforms that
behave. `false` would have been shorter and would have reintroduced the same
class of bet: `environment.go` already declines to name `true` for exactly this
reason — it is looked up through git's own shell, which is a different program
on each of the three platforms yagit ships for.

## Two things git does that are answered rather than passed on

**A combine with nothing above it is refused before the command runs.** git
answers `cannot 'fixup' without a previous commit` — *after* it has started the
rebase, leaving a repository stopped inside a plan that could never have run. A
refusal beforehand costs a sentence; a refusal afterwards costs an abort.

**A stop at an `edit` exits zero.** git reports success when it stops, because
stopping is what it was told to do, so nothing in the exit code tells a
finished rebase from one waiting at the second of five commits. The route
therefore reads `ReadState` after the command and says which happened.
Reporting "rewrote 4 commits" over a repository sitting halfway through a
rewrite would be the worst thing this interface could say: the state is real,
the banner would be missing, and the next operation would be refused for a
reason nothing on screen had mentioned.

## The lease is stronger here than anywhere else

[0022](0022-a-merge-is-checked-before-it-runs.md) has the run route read the
plan again and refuse an operation that has become a different one. Merge and
rebase compare an **outcome** — one word — which a stale plan can match while
meaning something else.

An interactive rebase compares the plan against the commits themselves: the run
route reads the range again and refuses anything that is not exactly those
commits, each exactly once. A commit that landed on the branch while the dialog
was open, or one that left it, is refused by name. That check is also what
makes the request safe at all — without it a plan could name a commit from
outside the range and replay foreign history under a dialog that never
mentioned it.

## What was not decided

- `reword` remains unavailable. Changing a message is `edit` plus the commit
  box, which is the amend path that already exists and already shows the
  message before it is committed. A dedicated verb would need the daemon to
  carry messages into git through an editor it cannot show.
- Merge commits inside the range are refused rather than flattened. A plan is a
  list of lines and a merge commit is not one; git would drop it without a
  word.
- A plan covers at most 250 commits. A list nobody can read is a list that gets
  approved unread, which is the judgement [0016](0016-the-graph-draws-what-is-checked-out.md)
  already made about how wide a graph may be drawn.
