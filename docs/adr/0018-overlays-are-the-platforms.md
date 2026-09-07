# 0018 — Overlays are the platform's, not a library's

**Status:** accepted

## The problem

A row of references has more actions than fit beside it, so the rest go behind
a menu. That menu has to open over a list that scrolls inside its own panel,
and the first `overflow: auto` above it will clip anything drawn in that flow.
It also has to close on Escape, close when the pointer goes anywhere else,
survive a second menu opening, give focus back where it came from, and stack
above whatever else is on screen without anyone maintaining a table of z-index
values.

That is the point where a React project usually installs Radix, Headless UI, or
Floating UI, and it is worth asking whether it has to.

## What was decided

Overlays are built on the browser's own top layer, and nothing else.

- Modals are `<dialog>` with `showModal()` — the focus trap, Escape, the
  inertness of the page behind, and the backdrop are the element's.
- Menus are `popover="auto"` — the top layer, Escape, light dismiss, the
  stacking, and the focus restoration are the attribute's.
- Tooltips are neither. They are CSS, because they never need to escape
  anything and a popover for a hover bubble is a mechanism looking for a use.

What the platform does not hand over is **placement**: the top layer has no
idea where the button was. So `Menu` measures — once, in one layout effect,
before the browser paints — and writes a `top` and a `left`. Below the trigger
where there is room, above it where there is not, pulled back from the edges of
the window, right edges aligned with the row's actions.

## What it was decided against

**Floating UI, or Radix on top of it.** It is the better positioning engine,
and it is not close: collision detection against arbitrary scroll containers,
anchor tracking, arrows, virtual elements. yagit needs one of those behaviours
— flip when it does not fit — and gets it in six lines. The rest of what the
dependency is for is behaviour this decision refuses on purpose: see below.

**A portal into `document.body` with a z-index.** The oldest answer, and the
one that produces a number nobody can change safely. The top layer has no
number: last opened is on top, and a modal is above a popover because the
specification says so.

**Reimplementing light dismiss.** A `mousedown` listener on the document, a
`contains` check, an Escape handler, a stack of what is open — every project
has this file and every one of them has a bug in it. `popover="auto"` is that
file, written by the people who also decide what a click is.

**`popovertarget` on the trigger.** The attribute makes the browser the
invoker: it toggles the popover itself and skips its own light dismiss for the
button, which is exactly the trap described below and would delete the code
that avoids it. It also shows the popover during the click, before any script
can measure it — so the first frame draws the menu wherever an unpositioned
popover lands, and it jumps into place afterwards. A menu that appears in the
wrong place is worse than a ref, so the show stays in script and the trigger
does the toggling by hand. This is the one place the platform is not taken
whole, and it is the first thing to revisit when CSS anchor positioning makes
the measuring unnecessary.

## What it costs

**Placement is ours, and so are its edges.** The menu flips above the trigger
and clamps to the window, and that is all. There is no shifting along the
cross axis and no collision detection against an inner scroller.

**A menu does not follow its row.** It is placed once. A scroll that moves the
trigger closes it, rather than leaving it pointing at a row that has moved —
which is honest, and is also what a real positioning engine would have made
unnecessary. The measurement is compared, not the scroll event: bringing a row
into view is itself a scroll, and its event arrives after the menu it opened.

**The light-dismiss order has to be known.** The browser dismisses an auto
popover on `pointerup`, before the `click` that follows it. A trigger that
toggled on React state would therefore reopen the menu it had just closed. The
component records what was true at `pointerdown`, and an end-to-end test pins
it, because nothing about it is guessable from the code that fails.

**A menu is short, and says so by scrolling when it is not.** A menu long
enough to need scrolling is a list, and a list belongs in a panel where it can
be searched. That is a rule about what to put in a menu, and it cannot be a
rule about what the window will be: a short screen turns three items into more
than fits. So the placement caps the menu at the room its side of the trigger
has and lets it scroll — because the top layer is not reachable by scrolling
the page, and an item painted past the bottom of the window would otherwise be
unreachable by every means at once.

**A floor under the browsers.** The popover API has been Baseline across the
engines since 2024. yagit is a local application whose user opens it in the
browser they already have, so this is a real constraint and not a hypothetical
one — it is the same floor `<dialog>` already put there.

## What would reverse it

A control whose placement is genuinely hard: a combobox that has to stay glued
to a field inside a scrolling panel, or a date picker that must shift rather
than flip. One of those is a reason to add a positioning engine — for that
control, and not as the way every overlay is built.

The other direction is likelier. CSS anchor positioning replaces the measuring
with two declarations and gives the tracking away for free. The placement lives
in one function so that the day it is available in every browser yagit runs
in, deleting it is a small change rather than an argument.
