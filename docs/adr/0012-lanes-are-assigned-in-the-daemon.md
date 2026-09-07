# 0012 — Lanes are assigned in the daemon, and the history is held there

**Status:** new. Phase 4 needs an answer, and [0003](0003-the-commit-graph-is-svg.md)
deliberately did not give one: it decided how the graph is *drawn* and said
that lane assignment "does not know what draws its output".

## The problem

Three questions, and the first one decides the other two.

1. Where does a commit's column get worked out?
2. What shape does the answer travel in?
3. What holds it between two requests?

## Where: in Go

A commit's column is not a property of the commit. It follows from every commit
above it — which lanes are open when the walk reaches it, and which of them were
waiting for it. Compute it over a window and it is not an approximation of the
right answer, it is a different picture: a branch whose tip is off the top of
the screen has no column at all.

The browser only ever holds a window. [0005](0005-server-state-and-virtualisation.md)
made sure of that, and it is the whole reason a repository with a million
commits opens at all. So the browser structurally cannot do this.

That settles it, and two further things fall out in the same direction.
Assigning a million commits takes about 320 ms in Go, once, off the thread that
paints; the same walk in JavaScript happens on the one thread that does, and it
happens before the first row can be drawn. And
the histories the assignment has to be right about — git's own, Linux's — are
reachable from a Go test and not from a component test.

**What it costs.** The graph is now part of the API's contract rather than a
detail of the interface: changing how lanes are assigned changes what the
daemon sends. That is the honest place for it. The picture is what the two
sides have to agree on.

## What shape: edges, not segments per row

The first implementation wrote, for each row, the segments crossing it. It is
the obvious shape — it is what a renderer draws — and it is quadratic. A
generated history with a few hundred concurrent branches produced **5.5 GB** of
segments for a hundred thousand commits, because every open branch writes a
segment on every row it spans.

One edge per (commit, parent) pair, written once however far it runs, is
**3.3 MB** for the same history and 40 MB for a million commits. It is also the
simpler thing to say out loud: a line from a commit's dot, down one column, to
one of its parents' dots.

The renderer asks for the edges crossing a range of rows. That was a scan of
the whole list to begin with — half a millisecond over a million commits — and
is now two binary searches plus a list of the edges crossing the top of every
256 rows, which is the part no ordering by one end can find. An edge carries
the columns of both of its ends, so a page can be drawn without looking up rows
it does not hold.

**What it costs.** The renderer does slightly more arithmetic — it works out
where an edge enters and leaves the window rather than being handed segments.
That arithmetic is `web/src/app/geometry.ts`, a handful of pure functions with
tests, and the alternative was a payload that cannot be sent.

## What holds it: a cache in the daemon, keyed by the refs

Paging the transport is not paging the computation. `git log --all` has to walk
the whole history whatever page is asked for, so a page request that re-read it
would make every scroll cost what the first load cost.

`internal/history` holds each open repository's commits and their assignment,
and reassigns them when the refs move. The refs are a *complete* fingerprint,
not a heuristic: a commit's name is a hash of its content, so nothing reachable
from a ref can change without the ref moving. `git for-each-ref` costs
milliseconds and runs on every request; the walk behind it runs when the answer
would differ.

**What it costs.** Memory, proportional to the history: about 250 MB of commits
per million, plus 40 MB of graph. A repository is forgotten when its tab is
closed, which is what keeps that bounded by what the user is actually looking
at. If it ever needs to be bounded further, the place to do it is here, with a
number the interface can show — not by quietly truncating the history.

## Why not compute lanes in TypeScript anyway

It was worth asking, because it would keep the API smaller. Two answers, and
the first is enough: the browser has a window, and the answer needs the whole
history. The second is that it would put the project's second silent-bug
surface — the first is parsing — in the layer with the weakest tests, next to
the layer that already has a fuzz target and an invariant checker.

## What this turned up: some graphs cannot be drawn

Checking the invariants against git's own repository — 85 469 commits, the
acceptance this phase was written against — answered a question nobody had
asked. That history needs **280 columns** when every ref is drawn. It is not an
artefact of this assignment: git's own `--graph`, over the identical commit
order, needs 67. A thousand tags and a hundred topic branches in flight are
genuinely that many lines at once.

No readable picture exists at either figure. So the interface draws the graph
up to a bound and, past it, says so: the number, and that every commit is still
listed. Drawing a narrower picture would mean leaving branches out of it
without saying which, which is the one thing this project does not do.

What actually fixes it is choosing which refs the graph draws. That is a
product decision this phase does not take, and the message names the reason
rather than apologising for it.

The assignment is also about three times wider than git's for the same history,
because a column here is fixed for the life of the line that occupies it —
git's collapses lanes leftward as they free up. That is the price of an edge
being one column, which is what makes a page drawable on its own. It is worth
revisiting only if a repository ever lands between the two figures and the
bound is what keeps its graph off the screen.

## Consequences

- `internal/graph` is pure and knows nothing about git beyond a SHA and a list
  of parents. Its tests do not assert columns; they assert that the picture is
  a faithful drawing of the history, over three hundred generated topologies
  and a fuzz target.
- The commits route is paged, which [0005](0005-server-state-and-virtualisation.md)
  had decided and phase 3 had not done. The daemon owns the page size and
  reports it, so there is one definition of it rather than two that must agree.
- `web/src/design/tokens.ts` exports `laneColor(index)` in place of the
  `LANE_COLOR_VARIABLES` array 0003 expected to keep. Every caller indexes the
  palette by data — a column, a hash of a name — and an index into an array is
  a value that may not be there. A function always answers a colour, and the
  variable name is still written in exactly one place.
