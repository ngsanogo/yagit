# 0017 — yagit edits files, and that is not a git operation

**Status:** accepted

## The problem

A merge that does not resolve on its own leaves the work-tree file holding both
versions between markers. Every screen yagit had could describe that file —
name it, colour it, call it "both modified" — and none of them could change it.
The way out was: open another editor, delete seven characters, come back.

That is the point at which a git client stops being one. It is also the moment
a user is least equipped to leave: mid-merge, with a repository in a state they
did not choose, being asked to remember which side was theirs.

The same gap is a smaller nuisance a hundred times a day. A typo in a line you
are about to commit is a round trip through another application for one
character.

## What was decided

The daemon reads and writes work-tree files, in a package of its own,
`internal/edit`, which runs **no git command at all**.

Editing a file is not a git operation and this refuses to pretend it is. git
never sees the file until somebody stages it; there is no plumbing command for
"write these bytes"; and driving `git` for it would mean inventing one. So this
is the one place in the daemon that opens the user's own files directly, and
the package doc says so rather than leaving a reader to wonder which rule got
bent.

Two consequences follow, and both are deliberate.

**Saving does not stage.** Disk and index are different places on every other
screen in this interface — that is what the two lists in the changes panel
are — and an editor that quietly staged would be deciding which of the two the
user meant. Resolving a conflict is therefore three acts: choose, save, mark
resolved. The pane says which one is still missing.

**The scope is a quick fix, and it says so.** No syntax highlighting, no
completion, a two-megabyte cap, a plain `<textarea>`. That element is the one
editing surface every browser already gets right — undo, selection, input
methods, screen readers — and a hand-rolled one would have to win all of that
back before it was level. Past the cap the answer is a real editor, and the
message says so instead of apologising.

## What it was decided against

**Sending the user to their `$EDITOR`.** Several clients do this, and it is
honest about being a desktop application with a shell next door. It also means
the one flow that most needs to be finishable inside the application cannot be.

**A code editor component.** [ADR 0004](0004-diffs-are-codemirror.md) commits
to CodeMirror 6 for diffs, and CodeMirror would give inline decorations over an
editable buffer — the shape VS Code's conflict resolution has. That remains the
right answer for diffs and it may become the right answer here.

> Later: it was neither. 0004's CodeMirror half was never built, and
> [0034](0034-the-diff-is-drawn-not-edited.md) reverses it — the diff is drawn
> from parsed hunks, with no editor. The paragraph below is what actually
> decided this pane, and it did not depend on 0004 being true. It is not what
this pane needed to exist: the buffer, the marker parsing, the fingerprint
check and the three-act flow are all independent of which widget draws the
text, and none of them was going to get easier by adding a dependency first.

**Staging the resolution automatically.** See above.

## What it costs

The daemon can now write to files under `YAGIT_ROOT`, which it could not
before. That is a real widening of what a compromised page could do, and it is
answered where it can be enforced rather than by convention:

- **The work tree is opened as an `os.Root`, and no absolute path is ever
  built.** Every read and every write is a method on that root, so the kernel
  walks the components and refuses one that leaves the directory. A check
  followed by an open can be beaten by swapping a symlink in between the two;
  there is no "in between" here.
- Anything under a git directory is refused outright, and so is a path with a
  `.git` component at any depth, whether it is a directory or the one-line
  FILE a linked worktree and a submodule use. `.git/config` names programs git
  runs (`core.pager`, `core.fsmonitor`) and the pointer file names where git
  reads its state from; a page that can write either can run anything, on the
  next command yagit issues. That refusal is made on where the path leads and
  not on how it is spelled, because a repository is a directory anybody can
  commit a symlink into.
- A save carries the fingerprint of the content it started from and is refused
  if the file moved, so a stale buffer cannot silently overwrite a checkout.
- Non-text files are refused rather than mangled: bytes that are not UTF-8
  cannot survive JSON, and would be saved back as replacement characters.

The write is a temporary file and a rename, so an interrupted save leaves the
old content rather than half the new one.

## What would reverse it

A version of yagit that shells out to the user's editor for everything, and
gives up on finishing a merge in the interface. That is a different product.

More plausibly: the pane grows until it is an editor, badly. The line held here
is the two-megabyte cap and the absence of a language mode. When the answer to
"why can't I do X in this box" stops being "because your editor does it better",
this decision needs writing again.
