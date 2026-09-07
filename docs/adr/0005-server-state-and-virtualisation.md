# 0005 — Server state is TanStack Query, keyed by page

**Status:** new. Nothing had been decided; phase 3 needs an answer.

## The problem

The main screen is a list of commits that can be a million rows long, with a
scrollbar you can drag to the middle of it, over data that changes underneath
whenever the user commits from their terminal.

Three questions, and they are not the same question:

1. What renders only the visible rows?
2. What holds the data those rows read?
3. What tells that cache it is stale?

## The decision

1. **Rows: `@tanstack/react-virtual`.** Headless — it computes offsets and
   returns indices, and renders nothing. Every element remains yagit's own, so
   the design system is untouched. The alternative, `react-window`, brings its
   own markup and its own inline styles, which is the one thing a project with
   a strict token rule cannot accept.

2. **Data: `@tanstack/react-query`, one query key per page.**
   `['commits', repoID, pageIndex]`, pages of 200, and the visible pages
   mounted through `useQueries`.

3. **Invalidation: the event stream** (see [0007](0007-one-event-stream.md))
   calls `queryClient.invalidateQueries({ queryKey: ['commits', repoID] })`.
   Only mounted pages refetch; the rest are marked stale and cost nothing until
   they are looked at again.

## Why not `useInfiniteQuery`

It is the obvious tool and it is the wrong one. An infinite query holds an
ordered list of pages and grows it at either end: to reach page 900 it must
fetch pages 1 through 899. That is correct for a feed you scroll and wrong for
a scrollbar you drag. Dropping the scrollbar to keep the abstraction would mean
losing the one control that makes a long history navigable.

Page-keyed queries give random access for free, because each page is an
ordinary independent query. Nothing clever is involved; the trick is only to
notice that "infinite" is a different shape from "windowed".

## Why a library at all, in a project with no Go dependencies

What is needed is a keyed cache with request de-duplication, garbage collection
of pages nobody is looking at, subscription from components, and correct
behaviour when two things ask for page 12 at once. Hand-written, that is a few
hundred lines whose bugs are all of the intermittent kind. It is also 13 KiB
gzipped, and it is the piece of this stack the most people have already
debugged.

The rule from [0008](0008-file-watching-through-fsnotify.md) applies on this
side too: a dependency earns its place by doing something that is genuinely
hard to get right, not something that is merely tedious.

## What it costs

Two libraries from the same family, which is a family to be careful with — it
is large, and most of it is not wanted here. Only these two are taken.

Page size is a guess until it is measured. 200 rows is roughly three screens,
so a fast scroll crosses a page boundary about once a second, and a page of
commit metadata is a few tens of kilobytes over a loopback socket. It is a
constant in one place, and the number to revisit first if scrolling stutters.
