# The HTTP API

The daemon serves one JSON API on the loopback, under `/api`. This document is
the map: what the routes are, how a client gets in, and what a failure looks
like on the way out.

**It does not list payload fields.** Those are declared once, in
[`web/src/api/types.ts`](../web/src/api/types.ts), and that file is the
contract — a second copy here would be a second definition to keep in step.
Read it beside this page.

## Getting in

Every route below `/api` requires the session token. The daemon never prints
the token. Under `./do` it lives in `.yagit/session-token`, is kept across
restarts, and `./do up --new-token` replaces it. `./do token` prints it once
the stack has answered it — a stack that is down, still starting, or running
on a different token is reported in those words instead of handing you a
secret that opens nothing.

```sh
TOKEN=$(./do token)
BASE=http://127.0.0.1:7420

curl -H "X-Yagit-Token: $TOKEN" $BASE/api/health
```

A browser gets in through a page rather than a URL. A request carrying no
cookie is answered by a form, which posts the token to `POST /api/session` and
comes back with an `HttpOnly` cookie. Nothing puts the token in an address,
where browser history, proxy logs and `Referer` headers would each keep a copy.

`POST /api/session` is the one route reachable without a token — presenting a
credential cannot require already holding one. It is rate limited per client
address, as is every request that arrives without a valid token.

## Routes

### Session and health

| Route | Answers |
| --- | --- |
| `POST /api/session` | Exchanges the token for the session cookie. The only unauthenticated route. |
| `GET /api/health` | What this is and what it runs on: `status`, the daemon's `version`, the count of open `repos`, the `platform` as `GOOS/GOARCH`, the `git` version it drives — or `gitError` when that could not be read — and `unavailable`, the operations this git is too old for, written as sentences. It is the first thing to ask for on a report nobody can reproduce, and it names no path, repository or branch, so it can be pasted into a public issue unread. |

### Repositories

| Route | Answers |
| --- | --- |
| `GET /api/repos/discover` | The git repositories found under a directory, and what the scan refused to look at. |
| `GET /api/repos` | The repositories currently open. |
| `POST /api/repos` | Opens one, by path. Body: `{"path": "…"}`. |
| `POST /api/repos/clone/plan` | What cloning would run. Body: `{"url": "…", "path": "…"}`. Changes nothing. |
| `POST /api/repos/clone` | Clones into `path` under `YAGIT_ROOT`, then opens it. Body: same. Response is NDJSON: `progress` lines, then `done` with the repository or `error`. |
| `POST /api/repos/init/plan` | What making an empty repository would run. Body: `{"path": "…", "branch": ""}` — an empty branch is answered with this machine's `init.defaultBranch`. Changes nothing. |
| `POST /api/repos/init` | Makes an empty repository at `path` under `YAGIT_ROOT` and opens it. Body: same. `201` with the repository. |
| `DELETE /api/repos/{id}` | Closes one, and forgets its held history. |

`discover` takes `dir`, `depth`, `include_worktrees` and `include_submodules`.
Its answer carries `skipped` — counted by reason, so "nothing here" and
"forty-one directories I could not read" are different answers — along with the
depth it used and the ceiling it was capped to.

A path is accepted in exactly three places: the open call, the clone
destination, and the scan root. All three are checked against `YAGIT_ROOT`
after resolving symlinks. Afterwards a client only ever handles the opaque
`id`, so a hostile web page cannot designate a file.

Clone progress rides the request as NDJSON rather than the session event
stream — the destination is not an open repository yet
([ADR 0030](adr/0030-progress-rides-the-request.md)).

### History

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/commits` | One page of history, with the part of the graph that page needs. |
| `GET /api/repos/{id}/commits/{sha}` | One commit, and the row it sits on. |
| `GET /api/repos/{id}/files/history` | Commits that touched a path, following renames. Query: `path`, optional `revision` (object name; absent means HEAD). |
| `GET /api/repos/{id}/files/blame` | Who last touched each line of a path at a revision. Same query. |
| `GET /api/repos/{id}/files/line-history` | Commits that changed one line of a path. Query: `path`, `line` (1-based), optional `revision`. |
| `GET /api/repos/{id}/refs` | Branches, remotes and tags, with HEAD beside them. |
| `GET /api/repos/{id}/search` | Commits matching a query. Query: `q`, `in` (`message`\|`author`\|`path`\|`content`), optional `scope` and `ref`. Answers `{commits, truncated, command}` — a list, not a graph: assigning lanes over a filtered set would draw connections the repository does not have. Every field matches literally. |
| `POST /api/repos/{id}/switch` | Moves HEAD. Body: `{"ref": "…", "detach": false}`. |
| `POST /api/repos/{id}/branches` | Makes a local branch. Body: `{"name": "…", "start": "", "switch": true}`. |
| `POST /api/repos/{id}/branches/rename` | Renames one. Body: `{"from": "…", "to": "…"}`. |
| `POST /api/repos/{id}/branches/delete/plan` | Answers the command a delete would run. Changes nothing. |
| `POST /api/repos/{id}/branches/delete` | Deletes one, remembering its tip so Undo can put it back. Body: `{"name": "…", "force": false}`. |
| `POST /api/repos/{id}/tags` | Makes a tag. Body: `{"name": "…", "message": "…", "target": "", "annotated": true}`. `annotated` defaults to true when omitted; lightweight tags need no message. |
| `POST /api/repos/{id}/tags/delete/plan` | Answers the command a tag delete would run. Changes nothing. |
| `POST /api/repos/{id}/tags/delete` | Deletes a local tag. Body: `{"name": "…"}`. |
| `POST /api/repos/{id}/tags/push/plan` | Answers the command a tag push would run. Body: `{"name": "…", "remote": "…"}`. Changes nothing. |
| `POST /api/repos/{id}/tags/push` | Pushes a local tag to a remote under the same name. Same body. |
| `POST /api/repos/{id}/merge/plan` | What merging a branch would do, and the line it would run. Changes nothing. |
| `POST /api/repos/{id}/merge` | Merges one into the branch HEAD is on. Body: `{"branch": "…", "into": "…", "outcome": "up-to-date\|fast-forward\|merge-commit"}`. |
| `POST /api/repos/{id}/rebase/plan` | What rebasing the current branch onto another would do, and the line it would run. Changes nothing. |
| `POST /api/repos/{id}/rebase` | Rebases the current branch onto another. Body: `{"onto": "…", "from": "…", "outcome": "up-to-date\|fast-forward\|rebase"}`. |
| `POST /api/repos/{id}/cherry-pick/plan` | What cherry-picking a commit onto the current branch would do, and the line it would run. Changes nothing. |
| `POST /api/repos/{id}/cherry-pick` | Cherry-picks one commit onto the branch HEAD is on. Body: `{"commit": "…", "into": "…", "outcome": "up-to-date\|fast-forward\|cherry-pick"}`. |
| `POST /api/repos/{id}/revert/plan` | What reverting a commit on the current branch would do, and the line it would run. Changes nothing. |
| `POST /api/repos/{id}/revert` | Reverts one commit on the branch HEAD is on. Body: `{"commit": "…", "into": "…", "outcome": "revert"}`. |
| `POST /api/repos/{id}/reset/plan` | What resetting the current branch to a commit would do, and the line it would run. Body: `{"commit": "…", "mode": "soft\|mixed\|hard"}`. Changes nothing. |
| `POST /api/repos/{id}/reset` | Resets the branch HEAD is on to a commit. Body: `{"commit": "…", "into": "…", "mode": "soft\|mixed\|hard"}`. |
| `GET /api/repos/{id}/undo` | Whether the most recent action can be undone. `{available, offer?}` — offer names kind (`commit`\|`amend`\|`checkout`\|`reset`\|`branch-delete`), subject, head, branch. |
| `POST /api/repos/{id}/undo/plan` | What undoing would run: soft reset, switch/detach back ([ADR 0031](adr/0031-undo-reads-the-head-reflog.md)), or `git branch` putting back one this daemon deleted ([ADR 0032](adr/0032-a-deleted-branch-is-remembered-not-recalled.md)). |
| `POST /api/repos/{id}/undo` | Carries out the plan. Body echoes `kind`, `into`, `head`, `to`, `to_ref`, `branch`, `detach`. |
| `POST /api/repos/{id}/rebase/interactive/plan` | The commits a plan may cover: everything after one commit, oldest first. Body: `{"commit": "…"}`. Changes nothing. |
| `POST /api/repos/{id}/rebase/interactive` | Rewrites them, following a plan. Body: `{"base": "…", "from": "…", "steps": [{"commit": "…", "instruction": "pick\|fixup\|fixup -C\|edit\|drop"}]}`. |

All three history routes — the pages, one commit's position in them, and the
search — take the same pair of parameters for which commits a walk covers:

- `scope=head` — reachable from what is checked out. The default.
- `scope=all` — reachable from every ref.
- `scope=refs` — reachable from the refs named by repeated `ref=`, and from
  nothing else ([ADR 0033](adr/0033-a-chosen-set-of-refs-is-a-history.md)).

`ref=` is repeated rather than one comma-separated value, because a ref name
may hold a comma and a separator a name can contain is one that eventually
splits a reference into two that do not exist:

```
GET /api/repos/{id}/commits?scope=refs&ref=refs/heads/main&ref=refs/heads/topic
```

Two refusals, both 400, and both rather than a quiet correction. `scope=refs`
with no `ref=` is refused because a walk over no reference draws an empty
picture, which is not the same thing as an empty repository. A `ref=` sent
under `scope=head` or `scope=all` is refused because a client that thinks it is
narrowing a walk which ignores the parameter would get the whole repository
back with no indication of it.

The three scopes see different commits, so they are different assignments held
separately — and the chosen set is part of the key rather than a filter over
one walk: a commit's column follows from every commit above it, so the commits
one set leaves out move the ones both sets share.

`commits` takes `page`. The daemon decides how big a page is, and the answer
carries `page_size`, `total` and `first` so a client never holds a second
definition of them. Each page also carries the slice of the graph it needs — a
column per commit, and every edge crossing those rows, ends included — so the
page can be drawn without the rest of the history.

Asking for one commit by SHA answers with the row it sits on. That is what
following a reference into the history needs, and it is the one thing no page
can say. The reference list carries HEAD explicitly, because `for-each-ref`
does not list it. The list reads in version order rather than lexicographic
refname order, which puts v0.10.0 before v0.9.0: branches and remotes ascending
as git printed them, tags newest-first. `tag:` decorations on a commit follow
the same rule, so a row and the sidebar never disagree about which tag comes
first.

`switch` is the only route that moves HEAD, and it answers with the reference
list above — the thing it changed, so a client never draws one frame of the
branch it just left. `detach` is not a variation on one operation: `git switch
main` and `git switch --detach main` leave the repository in two different
places, and only one of them is what checking out main means. Anything that is
not a local branch — a tag, a remote-tracking branch, a raw SHA — has to say
`detach`, because none of them is a place HEAD can sit.

The command is `git switch`, never `git checkout`, and never with git's
`--guess`: a switch to a name that has no local branch would otherwise create
one from a matching remote and set its upstream, which is three things from a
request that named one. What is checked out cannot be created by accident.

Nothing here is confirmed first, because a checkout destroys nothing: git
carries uncommitted work across when it can and refuses the whole switch when
it would overwrite something, naming the files. That refusal is a 422 and it
travels whole.

The four `branches` routes all answer with that same reference list, for the
same reason, and so does `merge`. `start` empty means HEAD, which is left to
git rather than resolved by the daemon, so the line in the log panel is one a
person could have typed.
`switch` chooses between two different commands — `git branch` leaves you where
you are, `git switch --create` moves you onto the new branch — rather than
being a flag on one, because running the two in sequence can leave a branch
made and not stood on when the second half is refused.

**A branch name never reaches git where git reads an option.** `git branch -m
release`, in a repository on main, renames main to release: it creates nothing,
exits 0, and takes away the branch somebody was standing on. Every name goes
after `--`, except the one `git switch --create` takes as its own argument —
where a value consumed by an option cannot be re-read as one, and where a `--`
would instead make the name into the start point. git then refuses the name by
name, which is the true answer to what was asked.

`branches/delete/plan` exists because a destructive confirmation has to show
the exact command, and the browser must not be the one assembling it: the line
the user approves and the line git receives have one definition, on the daemon.
It runs nothing. The discard plan of the working directory is the same idea.

The two `merge` routes join one local branch into the branch HEAD is on, and
the plan is not decoration. **A merge is never left to configuration**
([ADR 0021](adr/0021-a-shown-command-is-not-a-setting.md)). A bare
`git merge` reads `merge.ff`, so the same button is a pointer moving on one
machine, a merge commit on the next and a refusal on the third — while the
dialog shows one line and promises it means one thing. So `merge/plan` reads
the two branches (`git rev-list --left-right --count`) and answers what the
merge *is*: `fast-forward`, `merge-commit`, or `up-to-date`, with the commits
on each side and the exact command for that outcome. `merge_commit: true` on
the plan asks for a merge commit even where a fast-forward is possible — the
preference ADR 0021 left open — and the run accepts that approval over a
fast-forward reading. `merge` then runs
`--ff-only` or `--no-ff --no-edit`, never a bare merge, and answers with the
reference list the branch routes answer with — a merge moves HEAD's branch.

**The branch is named `refs/heads/…`, and the message of a merge commit is
written out** ([ADR 0022](adr/0022-a-merge-is-checked-before-it-runs.md)). A
short name is resolved by git's own search order, which reaches `refs/tags`
before `refs/heads`: in a repository holding both a branch and a tag called
`dup`, `git merge dup` merges the tag while the dialog names the branch. And
given a full ref git would title the commit `Merge branch 'refs/heads/dup'`,
while `merge.log` decides how much else goes in it — so `-m` carries the
message the confirmation showed. `--no-edit` stays beside it because
`GIT_MERGE_AUTOEDIT` can still send git looking for an editor the daemon has no
terminal to open.

**What the plan promised is read again before the command runs.** The flag
holds half of it — `--ff-only` cannot record a commit, `--no-ff` cannot skip
one — but nothing in `git merge --ff-only` refuses to fast-forward a branch the
dialog described as going nowhere. So `merge` reads the two branches once more
and refuses, with a 409, when the outcome has become a different one. The
counts are not part of that lease: a commit landing anywhere changes "brings 3
commits" without changing what the command does, and a confirmation that had to
be reopened every time anybody committed would teach people to click through
it.

`into` is the branch the dialog named, and it is there to be disagreed with —
the same guard `operation` carries. `git merge` acts on wherever HEAD happens
to be, so a checkout in a terminal or a second tab while the confirmation sits
open would otherwise merge into a branch the title never mentioned. A body
naming a branch other than the one HEAD is on now is a 409, and so is naming
none.

Both routes refuse before they describe anything: a repository already in the
middle of a merge, a rebase, a cherry-pick or a revert is a 409 naming the
operation to finish first, and two branches with no common ancestor are a 409
as well — `git merge` refuses unrelated histories, and the counts alone read
exactly like a wide divergence. Merging a branch into itself is a 400, an
outcome the daemon cannot name is a 400, and a conflict is git's own refusal,
whole, at 422 — with the repository left in the state the banner names and the
conflict screen resolves.

What the plan does not read is the work tree. Local changes a merge would
overwrite are found by doing the merge, and git's refusal names the files; the
plan answers what the two *branches* make of each other, and everything it
answers is a promise the run route keeps.

The two `cherry-pick` routes apply one commit onto the branch HEAD is on, and
the plan is not decoration either ([ADR 0021](adr/0021-a-shown-command-is-not-a-setting.md)).
`--ff` turns "HEAD is this commit's parent" into a pointer moving onto that
commit; without it the same arrangement writes a new object with the same tree.
So `cherry-pick/plan` reads the commit and HEAD and answers what the pick
*is*: `fast-forward`, `cherry-pick`, or `up-to-date`, with the full object name,
the subject, and — for the first two — the exact command. `up-to-date` answers
with an empty command: there is no cherry-pick that succeeds as a no-op the way
`git merge --ff-only` does, and inventing one would put a lie on the
confirmation. `cherry-pick` then runs `--ff` or `--no-ff`, always with
`--no-edit`, never a bare cherry-pick.

A merge commit is a 409 naming that a mainline parent would have to be chosen —
the commit panel shows one commit's patch, which for a merge is already the
first-parent reading, and choosing `-m` is a second question this screen does
not ask. A `commit` left empty, or holding something git would read as an
option, is a 400: nothing was read from the repository to decide it. The same
busy-repository and HEAD-lease refusals as merge apply, and a conflict is git's
own refusal at 422 with the banner and conflict screen that already know how to
finish a cherry-pick.

The two `revert` routes take one commit's changes back out of the branch HEAD
is on, and the plan is not decoration either ([ADR 0021](adr/0021-a-shown-command-is-not-a-setting.md)).
A revert always records a new commit under the user's hooks, so `revert/plan`
answers with the full object name, the subject, and the command it would run:
`git revert --no-edit --no-reference -- <sha>` — never a bare revert, which
would open an editor, and never one `revert.reference` can turn into a commit
whose subject is that option's placeholder for an editor. A merge commit is a
409 naming that a mainline parent would have to be chosen; a root commit is a
409 because git would invert it against the empty tree and delete every file
the repository started with, which is not the operation the confirmation
names; a commit that is not reachable from HEAD is a 409 because the
confirmation names what is being taken back out of the branch, and that
sentence is a lie for a commit the branch never held. The same
busy-repository and HEAD-lease refusals as merge apply, and a conflict is
git's own refusal at 422 with the banner and conflict screen that already know
how to finish a revert.

The two `reset` routes move the branch HEAD is on to a selected commit, and
the plan is not decoration either ([ADR 0021](adr/0021-a-shown-command-is-not-a-setting.md)).
Soft, mixed and hard are a **choice** the client makes — the same shape pull's
strategy has — because nothing in the repository decides which of the three
trees move. So `reset/plan` takes `mode`, answers with the full object name,
the subject, how many commits sit past the target, how many TRACKED paths
currently differ — untracked files are left out because a hard reset leaves
them alone, and a count that included them would name a loss that does not
happen — and the command it would run: `git reset --soft|mixed|hard <sha>` —
never a bare reset, whose meaning is mixed only by default, and never with a
`--` before the revision, which git would read as a pathspec ("Cannot do hard
reset with paths"). A commit that is not reachable from HEAD is a 409 because
the confirmation names taking the branch back along its own history, and that
sentence is a lie for a commit the branch never held. The same busy-repository
and HEAD-lease refusals as merge apply. A reset never leaves an in-progress
operation: it either succeeds or git refuses with its own account at 422.

The two `rebase/interactive` routes are the one pair here whose plan is not an
outcome the daemon reads but a **list the client writes**
([ADR 0029](adr/0029-a-rebase-plan-is-written-not-edited.md)). So the plan
route answers with material rather than a prediction: the base it resolved, its
subject, the branch HEAD is on, the commits after it **oldest first** — the
order a todo list is executed in — and the one command that will run whatever
the plan turns out to be. It refuses a range it cannot offer: a merge commit
inside it, nothing after the commit at all, a commit the branch never held, or
more than 250 commits, each a 409.

The run route takes object names and verbs, never a todo list — the daemon
writes that file, from the subjects it read itself, and hands it to git through
`GIT_SEQUENCE_EDITOR`. It reads the range again first and refuses any plan that
is not exactly those commits, each exactly once (409): a commit that landed on
the branch while the dialog was open makes the plan a list about a history that
no longer exists. A verb the daemon does not have, no steps at all, or a
combine on the first commit the plan keeps are 400s — the last of those because
git answers `cannot 'fixup' without a previous commit` *after* starting the
rebase, leaving a repository stopped inside a plan that could never have run.

Five instructions, and one rule chose them: each commits under a message that
already exists. `pick` keeps the commit, `fixup` folds it into the one above
keeping that message, `fixup -C` folds it in and keeps THIS message, `edit`
applies it and stops, `drop` leaves it out. `squash` and `reword` are refused
as unknown words, because both ask git to open an editor over a message nobody
has written — the state `blocked` on the status route already refuses to
continue.

The answer is the reference list, plus `stopped` with `step` and `total` when
git is waiting. That second half is not decoration: a plan holding an `edit`
ends with git stopped in the middle of it **having exited zero**, so nothing in
the command's own answer tells a finished rebase from one waiting at the second
of five commits. A conflict is git's own refusal, whole, at 422, with the
repository left in the state the banner names and the conflict screen resolves.

### Remotes

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/remotes` | The configured remotes (URLs redacted). |
| `POST /api/repos/{id}/remotes` | Adds one. Body: `{"name": "…", "url": "…"}`. |
| `POST /api/repos/{id}/remotes/rename` | Renames one. Body: `{"from": "…", "to": "…"}`. |
| `POST /api/repos/{id}/remotes/set-url/plan` | Answers the command a set-url would run (URL redacted). Changes nothing. |
| `POST /api/repos/{id}/remotes/set-url` | Changes where a remote is fetched from. Body: `{"name": "…", "url": "…"}`. |
| `POST /api/repos/{id}/remotes/remove/plan` | Answers the command a remove would run. Changes nothing. |
| `POST /api/repos/{id}/remotes/remove` | Removes one and its remote-tracking branches. Body: `{"name": "…"}`. |
| `POST /api/repos/{id}/upstream/plan` | What setting a follow would run. Body: `{"branch": "…", "remote": "…", "upstream": "…"}`. Empty branch means HEAD's. |
| `POST /api/repos/{id}/upstream` | Records that a local branch follows a remote-tracking one. Same body. |
| `POST /api/repos/{id}/upstream/unset/plan` | What unsetting a follow would run. Body: `{"branch": "…"}`. |
| `POST /api/repos/{id}/upstream/unset` | Forgets what a local branch follows. Same body. |
| `POST /api/repos/{id}/fetch` | Fetches (with `--prune --progress`). Body: `{"remote": ""}` for every remote. Progress as NDJSON. |
| `POST /api/repos/{id}/pull` | Pulls into the current branch. Body: `{"strategy": "ff-only\|merge\|rebase"}`. Progress as NDJSON. |
| `POST /api/repos/{id}/push/plan` | What pushing would run. Body: `{"remote": "…", "force": false}`. Changes nothing. |
| `POST /api/repos/{id}/push` | Pushes or publishes the current branch. Same body. Progress as NDJSON. |

Add, rename, set-url and remove answer with the remotes list. Set and unset
upstream answer with the reference list. Fetch, pull and push stream progress
as NDJSON (ADR 0030) — progress lines, then either the references they moved
or the failure. Validation refusals before git starts are still ordinary JSON
errors. URLs on the list are redacted
at the parse: a token in a remote URL is a password, and this list is drawn on
a screen ([ADR 0020](adr/0020-the-network-is-gits-and-so-are-the-credentials.md)).
Names reach git after `--`, for the same reason branch names do.

### Worktrees

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/worktrees` | Every checkout of the repository, the main one first, with `current` marking the one this tab is on. |
| `POST /api/repos/{id}/worktrees/plan` | What making another checkout would run. Body: `{"path": "…", "ref": "…", "new_branch": "", "detach": false}`. Changes nothing. |
| `POST /api/repos/{id}/worktrees` | Makes another checkout under `YAGIT_ROOT`. Same body. |
| `POST /api/repos/{id}/worktrees/remove/plan` | What removing one would run. Body: `{"path": "…", "force": false}`. Refuses the main working tree. |
| `POST /api/repos/{id}/worktrees/remove` | Removes a linked checkout. Same body. |
| `POST /api/repos/{id}/worktrees/prune` | Forgets the checkouts whose directories are gone. Locked ones are left alone. |

Every route that writes answers with the list as it stands afterwards, the way
the branch routes answer with the reference list.

A new checkout is **not** opened as a tab. Every worktree of one repository
shares a git directory, and that directory is the registry's identity for a
repository — so opening one would answer with the tab already open on the main
tree rather than with a second one.

The destination goes through the same check as a clone and an init: a path
under `YAGIT_ROOT` whose parent exists and which does not. The list is read
with `git worktree list --porcelain -z`; `--porcelain` alone prints paths
unquoted, so a directory holding a newline would split one worktree into two.
That is what raises this one feature's floor to git 2.36, where `-z` first
existed.

### Large files

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/lfs` | Whether git-lfs is on this machine, and which patterns this repository routes through it. `{installed, version, patterns}`. |
| `POST /api/repos/{id}/lfs/track/plan` | What tracking a pattern would run. Body: `{"pattern": "*.psd"}`. Changes nothing. |
| `POST /api/repos/{id}/lfs/track` | Routes a pattern through LFS, by writing `.gitattributes`. Same body. |
| `POST /api/repos/{id}/lfs/untrack/plan` | What untracking one would run. Same body. Changes nothing. |
| `POST /api/repos/{id}/lfs/untrack` | Takes a pattern back out of `.gitattributes`. Same body. |

Two facts about two different things, which is why one route answers both.
Whether git-lfs is installed is a property of the MACHINE — one `git lfs
version`, the same answer for every repository on it. Which patterns go through
the filter is a property of a file in the work tree, read from the top-level
`.gitattributes` and parsed for `filter=lfs`; `diff=lfs` and `merge=lfs` travel
with it and neither decides where the bytes live.

Nothing here transfers anything, and that is not an omission. LFS installs
itself into git as a filter and git runs it, so every fetch, pull, push and
checkout yagit already drives carries LFS content already.

Recognising a pointer needs no git-lfs at all, and every file diff the daemon
sends is checked for one: `lfs.old` and `lfs.new` carry `{oid, size}` where a
side is a pointer rather than the file. Both halves are optional for the reason
a diff has an `added` and a `removed` flag — a file newly tracked by LFS has a
pointer on the new side and its content on the old, and one being untracked is
the reverse.

The panel that lists patterns is drawn only where a repository already routes
something through LFS. Tracking the first pattern is offered from the
repository chrome whether that panel is there or not — the routes themselves
are unconditional, and a repository that tracks nothing can still be given its
first pattern by a client that asks.

A bare repository is refused with 409: `git lfs track` writes a file into a
work tree, and one that has none has nowhere to put it. A machine without
git-lfs is refused the same way, naming the program rather than passing on
git's "'lfs' is not a git command".

### Submodules

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/submodules` | What this repository pins, in index order. |
| `POST /api/repos/{id}/submodules/plan` | What adding one would run, with the URL redacted. Body: `{"url": "…", "path": "…"}`. Changes nothing. |
| `POST /api/repos/{id}/submodules` | Pins another repository and clones it. Same body. |
| `POST /api/repos/{id}/submodules/update` | Checks out what this repository records — `--init --recursive`. Body: `{"path": ""}` for all of them. |
| `POST /api/repos/{id}/submodules/sync` | Copies the URLs from `.gitmodules` into this repository's config. Same body. |
| `POST /api/repos/{id}/submodules/remove/plan` | BOTH lines a removal runs. Body: `{"path": "…", "force": false}`. Changes nothing. |
| `POST /api/repos/{id}/submodules/remove` | Unpins one. Same body. |

The list is built from `git ls-files --stage -z`, filtered to mode `160000`,
and not from `git submodule status`: that command's format is written for a
terminal, where a path holding a space is already ambiguous. `.gitmodules`
supplies the URL and `git config --get submodule.<name>.url` says whether
`init` has run — its exit 1 is the answer "nobody has" rather than a failure.

Where each checkout stands is read with
`git rev-parse --show-superproject-working-tree HEAD` **inside** it. A plain
`rev-parse HEAD` would not do: a submodule nobody has fetched is an empty
directory inside the superproject's work tree, so git's upward search finds the
superproject and answers with its HEAD — a commit from another repository,
reported as the submodule's.

Removing runs two commands because git has no single one: a `deinit` takes the
checkout away and a `git rm` takes the gitlink and the `.gitmodules` entry.
Neither touches `.git/modules/<name>`, where the objects stay; the confirmation
says so. Adding and removing both leave a **staged** change, which is a commit
the user still has to make.

yagit never sets `protocol.file.allow`. git refuses a submodule clone over the
file transport by default (CVE-2022-39253), and turning that off for every user
is not a client's decision.

### The stash

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/stashes` | The stack, newest first. |
| `GET /api/repos/{id}/stashes/{index}` | One stash and the patch it holds. |
| `POST /api/repos/{id}/stash/push/plan` | What stashing the work tree would save. Body: `{"untracked": false}`. Changes nothing. |
| `POST /api/repos/{id}/stash/push` | Sets the work tree aside. Body: `{"message": "…", "untracked": false}`. |
| `POST /api/repos/{id}/stash/apply/plan` | What putting one stash back would run. Body: `{"index": 0, "sha": "…", "mode": "apply\|pop"}`. Changes nothing. |
| `POST /api/repos/{id}/stash/apply` | Puts one back, keeping it or removing it. Same body. |
| `POST /api/repos/{id}/stash/drop/plan` | What dropping one would run, and how much goes with it. Body: `{"index": 0, "sha": "…"}`. Changes nothing. |
| `POST /api/repos/{id}/stash/drop` | Throws one away. Same body. |

Every route that writes answers with the stack as it stands afterwards, the way
the branch routes answer with the reference list: the operation changed the
list, so the list is what comes back.

**A stash is named twice, and both names are read again**
([ADR 0028](adr/0028-a-stash-is-a-position-and-a-name.md)). Everywhere else in
this API a plan resolves a revision to an object name and the run sends that
name straight to git, so the name *is* the agreement. A stash has no such name
to send: it is addressed as `stash@{0}`, `stash@{1}` — a **position** in a
stack — and `git stash drop` refuses an object name outright. So the position
is what runs, and the position is the part that moves: anything pushed or
dropped renumbers every entry below it. Both routes therefore take `index` and
`sha` together, read the list again, and answer 409 when that position no
longer holds that object. A request naming no `sha` is refused the same way,
for the reason a missing `into` is: a client that names none is a client that
did not look.

**Two of git's own answers here succeed while doing nothing, and neither is
passed on.** `git stash push` with nothing tracked and changed prints "No local
changes to save" and exits 0, leaving every untracked file exactly where it
was — so `stash/push` reads the work tree itself and answers 409, naming
`--include-untracked` when that is the flag that would have worked. The guard
is on the command and not on the plan, deliberately: `stash/push/plan`
*describes* that work tree, counting tracked and untracked paths apart, because
the dialog it draws is the only place the flag can be turned on and a plan that
refused would mean the dialog never opens. And `git stash show` on a stash
whose only content is untracked files prints nothing and exits 0, because they
hang off a third parent it does not read without being asked — so that parent
is read explicitly, and no stash can be inspected into an empty screen.

**What a stash holds is read with `git diff` and `git show`, never with `git
stash show`.** The wrapper re-parses its arguments and hands the remainder on,
and the `--src-prefix=a/` / `--dst-prefix=b/` this project pins do not survive
that on every git: the header comes back as `diff --git butesnotes.md`, and the
diff parser refuses it. Dropping the prefixes is not an option either — pinning
them is what stops `diff.mnemonicPrefix` and `diff.noprefix` from breaking every
diff in the interface on a user's machine. So a stash commit's parents are used
directly: `stash^1` against the stash for the tracked half, and `stash^3` — a
parentless commit whose tree is the untracked files — for the rest. A commit
with neither two nor three parents is not a stash and is refused, so an
ordinary object name cannot be drawn as one.

A message holding a newline is a 400: git stores the whole thing on the commit
and flattens it into the reflog, so the list would come back saying something
nobody typed. A message that starts with a dash is not refused — it reaches git
as `--message=<value>`, one argument, which cannot be read as an option.

The plan for `apply` carries how many paths the stash holds and how many
tracked paths currently differ. The second is not a refusal: putting a stash
back onto work in progress is ordinary and git merges the two. It is the
difference between an apply that lands and one that stops on conflict markers,
which is what the confirmation says. `apply` and `pop` are two git subcommands
rather than a flag, and the mode is pinned on the command — a client asking for
one and getting the other would have a stash removed nobody agreed to remove.

A conflict is git's own refusal at 422, whole. It is worth knowing where git
puts the words: "CONFLICT", and "The stash entry is kept in case you need it
again", go to **stdout**, and only "Recorded preimage" goes to stderr — a
failure carrying stderr alone would say nothing anybody could act on, which is
what borrowing stdout into a failed command's account is for. What a stash
conflict does *not* leave is an operation: git writes no `MERGE_HEAD` for one,
so there is nothing to abort or continue and the banner stays away. It is
finished in the changes view, by editing the file and staging it.

Every write refuses a bare repository (409) and a repository already in the
middle of a merge, a rebase, a cherry-pick or a revert (409, naming the
operation to finish first) — git refuses that one too, with "notes.md: needs
merge", which says nothing about the merge in the way. What these routes do
**not** require is a branch: `git stash` on a detached HEAD works and records
`(no branch)`, so demanding one would refuse a real operation for an unrelated
reason. The stack's entries carry an empty `branch` in that case rather than
git's own phrase, which is not a name anything can be done with.

Reading is treated differently from writing on purpose. `stashes/{index}` takes
a position and no object name, because nothing is at stake if the stack shifted
between the click and the answer — and the answer carries the stash it actually
read, so the panel titles itself from that rather than from the row that was
clicked. A position past the end is a 404: there is no such thing to look at.

### The working directory

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/status` | What differs, on the side it differs on, plus branch, upstream, ahead and behind. |
| `GET /api/repos/{id}/diff` | One file's diff. Takes `path` and `side=staged\|unstaged\|untracked`. |
| `POST /api/repos/{id}/stage` | Stages whole files, or chosen lines. |
| `POST /api/repos/{id}/unstage` | The reverse. |
| `POST /api/repos/{id}/discard` | Destroys working-directory changes. |
| `POST /api/repos/{id}/discard/plan` | The exact git commands a discard would run, having run none of them. |
| `POST /api/repos/{id}/commit` | Commits what is staged. Body: `{"message": "…", "amend": false}`. |
| `GET /api/repos/{id}/prepared-message` | The message git would open an editor on. Takes `amend=true\|false`. |
| `POST /api/repos/{id}/operation/plan` | The command finishing or calling off the operation in progress would run. |
| `POST /api/repos/{id}/operation` | Runs it. Body: `{"action": "abort\|continue\|skip", "operation": "rebase"}`. |

The three staging routes take `{"paths": [...]}` for whole files, or
`{"paths": [...], "lines": {...}}` to stage part of one. Line selections are
turned into a patch fed to `git apply`; the builder is pure and fuzzed
([ARCHITECTURE.md](ARCHITECTURE.md#staging-part-of-a-file)).

`discard/plan` exists because discarding is the one operation that destroys
work no commit holds, and the interface cannot know the command: the split
between `git restore` and `git clean` follows a status read on the server, at
the moment the question is asked. So the plan is composed where that status
lives, and a dialog never invents a line git will not receive.

The status is read again when the discard itself arrives, so a file that became
tracked in between is still handled by the right command. The plan is what the
user is shown, never what the operation trusts.

`status` carries a `state` object naming the operation the work tree is in the
middle of — `merge`, `rebase`, `cherry-pick`, `revert`, `bisect`, `am` — with a
rebase's branch and its progress beside it. `git status --porcelain` reports
none of that, so the daemon reads the same marker files git itself reads.

The two `operation` routes finish, or call off, what `state` reports. The client
never says WHAT to abort — only which of the three things to do — and the
daemon builds the command from its own reading. `operation` in the body is what
the interface was looking at, and it is there to be disagreed with: the status
is polled every two seconds, so a rebase can end and a merge begin while an
Abort sits on screen, and running it then would destroy a merge nobody saw
after a dialog that promised `git rebase --abort`. A body naming an operation
other than the one in progress is a 409, and so is naming none: an empty string
is not a wildcard. An action an operation does not take — `git merge --skip`
does not exist — is a 400.

`state` also carries `actions`, the instructions that operation takes, and
`blocked`, why one of them is refused HERE. The one case today is a rebase with
a `squash`, `reword` or `fixup -c` in its plan: continuing commits under a
message git prepares in an editor, and a daemon has no terminal to show one in.
That message is the replayed commit's own for a `pick`, which is why continuing
is offered at all — and a new one for those three, which is why it is not. The
reason is a sentence, because the button that is drawn disabled and the 409 the
route answers use the same string.

The plan half checks none of that. It is asked the instant before a dialog
opens, its answer describes the repository as it is now, and refusing to
describe it would leave the dialog with nothing to show.

Both answer with the working directory rather than the references, which is the
opposite of the branch routes. The banner is drawn from the status and has to
stop saying "Rebasing" in the same frame the button comes back; the references
move too, and the event stream reports that.

`prepared-message` answers with `{"text": "…", "source": "…", "signing": false}`,
where the source is `merge`, `squash`, `head`, `template`, or empty when git
prepared nothing. It is what `git commit` would put in the editor and nothing
else: MERGE_MSG after a merge that stopped, the replaced commit's message under
`amend=true`, and the file `commit.template` names when no operation prepared
anything — last, because that is the order `git commit` itself reads them in.
Comment lines are removed by `git stripspace --strip-comments`, which honours
`core.commentChar` — yagit commits with `--cleanup=whitespace`, so a comment
left in would be committed verbatim.

`signing` is `commit.gpgsign` read, never written: whose key a commit carries
stays the person's own decision, made in git's configuration
([ADR 0021](adr/0021-a-shown-command-is-not-a-setting.md)). It rides on this
route because it answers the same question the message does — what the next
commit is going to be — and because signing is the step that fails once
everything else has succeeded, which a box that never mentioned it turns into
"the button did not work".

Co-authors are not here either. `Co-authored-by:` is a trailer convention
rather than a git feature — git stores the message verbatim — so the lines are
composed in the browser and arrive as part of the message, like every other
word in it.

Nothing here suggests anything. What the interface proposes from the staged file
list — "Add src/parser.ts" — is computed in the browser and never reaches this
API: it is a guess, it stays a placeholder until somebody accepts it, and a
guess nobody read is not a commit message.

### Editing a file

| Route | Answers |
| --- | --- |
| `GET /api/repos/{id}/file` | One work-tree file as text. Takes `path`. |
| `PUT /api/repos/{id}/file` | Writes it back. Body: `{"path": "…", "text": "…", "base": "…"}`. |
| `POST /api/repos/{id}/resolve` | Takes one side of a conflict whole. Body: `{"paths": [...], "side": "ours\|theirs"}`. |

The file on disk, not a blob from the index or a commit: it is the one a merge
left conflict markers in, and the only one saving can put back. Editing is not
a git operation and these two do not pretend otherwise — no command runs, and a
save does not stage.

`base` is the `fingerprint` the read answered with, and it is required. A save
is only correct for the content it started from, so a file that moved
underneath — a checkout in another window, the user's own editor — is answered
with 409 rather than overwritten. The same guard as a line selection's diff id.

Refused, always: a path that resolves outside the work tree or inside the git
directory (403 — `.git/config` names programs git runs), a file that is not
text or not a regular file (409), one over two megabytes (413).

`resolve` runs two commands, `git checkout --ours` then `git add`, because the
first writes the file and only the second collapses the index stages that make
git call it unmerged. Which of the two commands each path needs is read off
git's own status codes: "deleted by us" kept ours is `git rm`, since our side of
that conflict is the file not existing.

A conflicted path's `diff` is a 409. `git diff` answers an unmerged path with a
combined diff — three versions wide, headed `@@@` — which no patch can be built
from and which the parser refuses by name rather than failing on the syntax.

### The stream, and the log

| Route | Answers |
| --- | --- |
| `GET /api/events` | Server-sent events. One stream per session, never one per repository. |
| `GET /api/log` | The git commands the daemon has actually run. |

The stream sends one event kind, `repository`, when a repository's git
directories change. A client refetches what it is showing rather than being
told what changed ([ADR 0007](adr/0007-one-event-stream.md)).

`/api/log` is what backs the log panel: every execution with its command, exit
code, duration and stderr. A user should be able to learn git by watching
yagit work.

## When git fails

The response carries the exact command, its arguments, the exit code and the
raw stderr. Never "Something went wrong".

```json
{"error":{"message":"…","git":{"command":"git rev-parse --show-toplevel",
  "args":["rev-parse","--show-toplevel"],"exit_code":128,
  "stderr":"fatal: not a git repository …"}}}
```

`git` is absent when the failure was not a git failure — a bad parameter, an
unknown id — and the `message` stands alone. An unknown route under `/api`
answers JSON too, rather than the frontend's index page: a client that mistypes
a path should get an error it can parse.
