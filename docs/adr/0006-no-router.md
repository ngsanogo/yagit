# 0006 — No router library

**Status:** kept. What it decided against is still absent; what it expected to
build in place of a router was never needed, and this record says what is there
instead.

## The decision

No React Router, no TanStack Router, and no routing module of yagit's own.
`web/src/App.tsx` reads `window.location.pathname` once:

```
/         the workbench
/design   the design system showcase
```

The showcase is deliberately unlinked from the workbench — it is the reference,
and the end-to-end suite's way into every component state, not a product
screen.

## Why

The architecture caps navigation depth at two, and the workbench is one screen
of panels rather than a tree of pages: repositories are tabs, a commit is a
selection, a diff is a pane. A router's value is in what that shape does not
have — nested layouts, route-level data loading, code splitting per branch,
type-safe search parameter serialisation. Adopting one would be adopting an
abstraction whose whole benefit sits in the part of the problem yagit does not
have.

Being served in a browser does make the URL tempting: a reload landing where
you were, a window restored after a crash. That half is real, and it is
answered without the URL. `web/src/app/sessionStore.ts` writes the open
repository paths, the active one, the theme and the history scope to
`localStorage`, and the workbench re-opens them on load. Paths rather than the
daemon's opaque ids, because an id dies with the process that minted it.

## What it costs

**Nothing inside the workbench can be linked to.** A repository, a commit, a
diff cannot be sent to somebody as a URL, and the back button does not move
within the application because the application never navigates.

**Reverse this when any of these becomes true**, and add a record saying so:

- a view is worth linking to from outside the application;
- the back button is expected to move inside the workbench;
- what has to survive a reload outgrows a handful of `localStorage` keys.

Until then a router would be configuration for a thing that has two addresses.
