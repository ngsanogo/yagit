# 0033 — A chosen set of refs is a history, not a filter over one

**Status:** extends [0016](0016-the-graph-draws-what-is-checked-out.md). It
builds the picker 0016 named and deferred, and it takes the same shape 0016
gave the scope rather than a new one beside it.

## The problem

0016 gave the graph two ends of a range. `HEAD` is the current branch and
nothing else; `--all` is every ref, which on git's own repository is 280
columns and therefore no picture at all. Both are right, and the question
people actually arrive with is in the middle of them:

> How far has this topic drifted from main?

Neither end answers it. The current branch alone cannot show the divergence —
the other side of it is exactly what is missing — and every ref draws the
divergence inside a thousand tags' worth of lanes, or past the 24-column
refusal, which is the same as not drawing it.

0016 said so itself, in what it decided against: *a picker naming individual
refs is the fuller version of this same decision, not a second one, and it can
be built on this `Scope` without moving anything*. This is that, and the
sentence held: nothing moved.

## Decision

A third scope, `refs`, whose revisions are named by the client:

```
GET /api/repos/{id}/commits?scope=refs&ref=refs/heads/main&ref=refs/heads/topic
```

Repeated `ref=` rather than one comma-separated value. A ref name may hold a
comma — git forbids a short list of bytes and that is not among them — and a
separator a name can contain is a separator that eventually splits one ref into
two that do not exist.

The same pair travels on the two other routes that read a walk: one commit's
position in it (`GET .../commits/{sha}`), because a row number means nothing
except in the walk it was counted in, and the search (`GET
.../commits/search`), because "in this history" is most of what a search means.

**The chosen set is part of the cache key, not a filter applied after the
walk.** `main` alone and `main` with a topic branch are two different
assignments: a commit's column follows from every commit above it, so the
commits one set leaves out move the ones both sets share. `internal/history`
keys its held assignment by repository, scope and a key built from the refs —
sorted and deduplicated, because the order boxes were ticked in is nobody's
business and the same three refs in another sequence are the same walk.

**The 24-column refusal stays exactly where it is.** Under 0016 it stopped
being the answer to "this repository has too many refs" and became the honest
refusal for a set that is itself too wide. Under this ADR the person can act on
it: the message says fewer references would be narrower, and the control that
makes it so is on the same panel.

**What it costs.** A third held assignment per repository, on repositories
where somebody asked for one, and a set of them per distinct choice. 0012
already says where to bound the held histories if it ever needs bounding, and
nothing here changes that arithmetic — only how many keys can exist.

## The refs come off a query string, which no other revision here does

This is the one thing that genuinely is new, and it is a security decision
rather than a product one.

Everywhere else in `internal/git` a revision is either an object name checked
as hexadecimal or a name the daemon spelled in full itself. `git log` takes its
revisions positionally and has no `--` to hide them behind, so a client-supplied
`--output=/etc/passwd` in that position is an option, not a ref.

So `CheckRefName` refuses on the *shape* of the name rather than on a list of
tricks: no leading dash, no `..`, no `@{`, no control characters, none of the
bytes git itself forbids. Nothing legitimate is turned away — every ref the
interface sends came out of `ForEachRef`, which lists only names git already
accepted — and both walks that take a chosen set run the same check, through one
function, because a check only one of them ran is a check the other route is one
refactor away from not having.

## Two refusals rather than two quiet corrections

- **`scope=refs` with no `ref=`** is refused with 400. An empty walk drawn is
  indistinguishable from an empty repository, so a blank graph would be a
  correct answer to a question nobody asked. The interface never sends it: the
  option only appears once the references have been read, it opens on the branch
  HEAD is on, and the last ticked box cannot be cleared.
- **`ref=` under `scope=head` or `scope=all`** is refused with 400 as well. A
  client that thinks it is narrowing a walk which ignores the parameter would
  get the whole repository's graph back and no indication of it — the same
  failure mode as the unknown scope 0016 refuses, for the same reason.

## What was decided against

- **A range — `main..topic`.** It is a smaller answer to a narrower question.
  Two branches' divergence is the *symmetric* difference plus the commits below
  it, which is what a set of tips walked together already gives; a range shows
  one side and calls it the answer. Ranges are also a syntax to learn, on a
  screen whose whole premise is that the graph is the interface.
- **A multiple-select instead of checkboxes.** The list is as long as the
  repository decides. On a thousand tags the filter box above the list is what
  makes the choice possible at all, and a native `<select multiple>` cannot be
  filtered.
- **Remembering the choice between sessions.** Nothing in yagit is persisted
  between sessions yet, and a stored set of ref names is one that can name a
  branch the repository no longer has. When there is somewhere honest to keep
  it, it goes there with everything else.
- **Reusing the assignment of `--all` and hiding the rows.** This is the
  decision at the top of this file, and it is worth naming as a rejection too:
  the hidden rows are exactly what the lines were drawn through, so the picture
  left behind is not a narrower graph — it is a wrong one.

## Consequences

- `git.ScopeRefs` joins the two scopes of 0016, and `LogScope` takes the chosen
  set as a further argument rather than gaining a twin. One walk function and
  not two: a second entry point is a second place for the check below to be
  missing from.
- `internal/history`'s `Window` and `Locate` take it the same way, and hold it
  in the key of the entry they answer from — `chosenKey` sorts and deduplicates,
  because the order boxes were ticked in is nobody's business.
- The search route (`internal/api/search.go`) reads the same `scope=` and
  `ref=` and answers a flat list: a filtered walk has no assignment to send, so
  the payload's shape says so — commits, no edges, no width, no row numbers.
  Clicking a result asks the commit route where it sits in the walk on screen,
  which is the question the graph can answer.
- `web/src/app/RefPicker.tsx` is the control; `web/src/app/historyScope.ts`
  holds the two pure parts of it — the cache key and what the picker opens on —
  so both can be asserted without a browser.
- The roadmap's "a picker naming individual refs" is no longer a target
  feature that does not exist.
