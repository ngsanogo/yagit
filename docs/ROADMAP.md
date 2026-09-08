# Roadmap

yagit aims at parity with the established graphical git clients — in features
*and* in visual finish. A screen that looks like an unstyled framework default
has failed.

This page says **what is built**. *Why* it is built that way is in
[ARCHITECTURE.md](ARCHITECTURE.md) and in the records under
[adr/](adr/README.md); a second copy here would be a second copy to keep in
step.

## The graph and the history

- **The commit graph is the main object.** Lanes are assigned in the daemon,
  because a commit's column follows from every commit above it and the browser
  only ever holds a window ([0012](adr/0012-lanes-are-assigned-in-the-daemon.md)).
  Drawn in SVG, for the visible rows only ([0003](adr/0003-the-commit-graph-is-svg.md)).
- **Which refs the picture draws is a choice**: what is checked out (the
  default), every ref, or a set you tick
  ([0016](adr/0016-the-graph-draws-what-is-checked-out.md),
  [0033](adr/0033-a-chosen-set-of-refs-is-a-history.md)). Past 24 columns yagit
  says the history is too wide, with the number, rather than drawing a picture
  that leaves branches out.
- **Search** over message, author, path and content. Every field matches
  literally — a box that passed the query to `git log --grep` would find
  nothing for `fix(api)` and refuse `a(b` with a message about parentheses. The
  answer is a list rather than a filtered graph: lanes drawn over the matches
  alone would show connections the repository does not have.
- **Blame** at a revision, **file history** following renames, and **line
  history** from a blame line number (`git log -L`).

## Working on a repository

- Repositories open in **tabs**. Open, clone and init; clone and init check
  their destination against `YAGIT_ROOT`, and init pins `--initial-branch`
  rather than letting `init.defaultBranch` decide a name the log panel would
  then misreport ([0021](adr/0021-a-shown-command-is-not-a-setting.md)).
- **Staging by file, by hunk and by line**, and discard at the same three
  granularities. Anything finer than a path is a patch handed to `git apply`,
  built once for both directions and fuzzed, because a patch with a miscounted
  range applies cleanly and stages a file nobody asked for.
- **Commit and amend**, with the user's hooks and signing configuration. The
  box starts from the message git would open an editor on, in git's own order:
  `MERGE_MSG` or `SQUASH_MSG`, the commit an amend replaces, then
  `commit.template`. Co-authors are rows of a name and an address, and the
  trailers are added on the way out rather than written into the draft.
- **Conflict resolution in the interface.** Each `<<<<<<<` region is drawn as
  its two sides — git's own labels, the diff3 base where there is one — with
  three buttons between them. Taking a side rewrites the buffer and is
  reversible until the save; taking a side of the whole file goes through
  `git checkout --ours`. Editing by hand is the third way, and the pane refuses
  to stage a file with a marker still in it
  ([0017](adr/0017-yagit-edits-files.md)).

## Moving history

Every destructive step shows the exact `git` command before it runs and names
what will be lost.

- **Checkout** through `git switch --no-guess`, never `git checkout`, which
  restores a *file* of that name where one exists. What is not a local branch
  detaches instead of switching.
- **Branches**: create, rename, delete. A name reaches git where git reads a
  name and never where it reads an option.
- **Merge and rebase** read the two branches first and say which of three
  things will happen, because `merge.ff` is a setting and a button that reads a
  setting cannot say what it does
  ([0022](adr/0022-a-merge-is-checked-before-it-runs.md),
  [0023](adr/0023-a-rebase-is-read-in-both-directions.md)).
- **Interactive rebase**: reorder, `fixup`, `fixup -C`, `edit`, `drop`. yagit
  writes the todo list itself and points git's sequence editor at its own
  binary, because `git rebase -i` takes that list from an editor and a daemon
  has no terminal to open one in
  ([0029](adr/0029-a-rebase-plan-is-written-not-edited.md)).
- **Cherry-pick, revert, reset.** Reset's mode is a choice on the confirmation
  and is always pinned on the command; hard names the commits and the
  uncommitted work it discards. A commit that is not on the branch is refused
  rather than offered.
- **Stash**: push, apply, pop, drop, inspect. A push counts tracked and
  untracked files apart, because `git stash push` leaves the second kind where
  they are while exiting 0. Every write names a stash by both its position and
  the object at it
  ([0028](adr/0028-a-stash-is-a-position-and-a-name.md)).
- **Undo**, on the HEAD reflog, leased by object names
  ([0031](adr/0031-undo-reads-the-head-reflog.md)): the tip commit or amend, a
  checkout, a reset. Undoing a branch deletion is the one kind the reflog
  cannot answer, so the tip is read before the delete and remembered while the
  repository is open
  ([0032](adr/0032-a-deleted-branch-is-remembered-not-recalled.md)).

## Talking to a remote

yagit authenticates nothing and never asks for a password: the network is
git's, and so are the credentials
([0020](adr/0020-the-network-is-gits-and-so-are-the-credentials.md)).

- **Fetch, pull and push.** Fetching always prunes. Pulling is three named
  operations rather than one — fast-forward, merge, rebase. Pushing writes the
  refspec out (`main:refs/heads/trunk`), because a branch pushed to its own
  name would create a second branch on the server for one line of work.
- **Forcing** is `--force-with-lease --force-if-includes`, never a bare
  `--force`.
- **Remotes**: add, rename, remove, edit URL. Setting and unsetting a branch's
  upstream sits beside the branch list.
- **Tags**: annotated or lightweight, local delete, push to a chosen remote.
- **Progress** on clone, fetch, pull and push rides the request that started
  it, as NDJSON ([0030](adr/0030-progress-rides-the-request.md)).

## Beside the repository

- **Worktrees**: which exist, which branch each holds, and making and unmaking
  them through the command shown first.
- **Submodules**: listed, added, updated, synced and removed. The panel appears
  only where there is one to show.
- **LFS**: the patterns a repository routes through the filter, tracking
  through the command shown first, and a committed pointer named wherever a
  diff would otherwise show three unreadable lines of metadata.
- **Signatures** are read and never written. The commit box says a commit is
  about to be signed before the button is pressed, and a commit's pane carries
  git's own verdict on the signature it has.

## What is deliberately not built

- **`squash` and `reword` in an interactive rebase.** Both ask git to open an
  editor over a message nobody has written, which is the state the daemon
  already refuses to continue. `fixup` and `fixup -C` are the same question
  with an answer.
- **Force-updating a tag on a remote.**
- **LFS transfers.** LFS installs itself into git as a filter and git runs it,
  so every transfer yagit already drives carries LFS content. yagit never
  installs git-lfs, and says once, plainly, when a repository needs it.
- **`protocol.file.allow`.** git refuses a submodule clone over the file
  transport by default, and turning a security control off on every user's
  behalf is not a client's decision.
- **Undoing a discard.** It never moved a ref, so the reflog has nothing to
  say about it.

## Phases

Work proceeded in phases; each ended with something runnable and testable.

| | Phase | Status |
| --- | --- | --- |
| 0 | Skeleton: `mise.toml`, `./do`, a Go hello-world reachable from a browser. | ✅ |
| 1 | Go daemon, token authentication, `git log` parsing, JSON endpoints. | ✅ |
| 2 | Design system — tokens, palette, typography, base components — before any product screen. | ✅ |
| 3 | Virtualised commit list. | ✅ |
| 4 | Lane assignment and SVG rendering, checked against three hundred generated histories, eleven million fuzzed, and git's own repository. | ✅ |
| 5 | `git status`, file watching, the working-directory panel, staging by file, hunk and line, and the event stream. | ✅ |
| 6 | Commit and amend, checkout, branch create/rename/delete, merge, rebase. | ✅ |
| 7 | A commit's message and its patch; file history, blame, line history. | ✅ |
| 8 | Remotes, fetch, pull, push, clone, remote management. | ✅ |
| 9 | Interactive rebase, stash, cherry-pick, revert, reset. | ✅ |
| 10 | Undo, on the reflog. | ✅ |
| 11 | Conflicts, worktrees, submodules, LFS, signature verdicts. | ✅ |
| 12 | What the phases deferred: remote URL editing, upstream set and unset, NDJSON progress on fetch/pull/push, the theme toggle and the light theme, tabs and scope remembered across reloads, and the merge-commit preference where a fast-forward is possible. | ✅ |

Further work is not numbered. Unit tests stay mandatory on parsing and on lane
assignment — the two places where bugs are silent — and both carry a fuzz
target as well.
