# 0011 — Tailwind generates the utilities from `tokens.css`

**Status:** kept.

## The tension

Tailwind's reputation is for rapid ad-hoc styling and arbitrary values —
`p-[13px]`, `text-[#1a1a1a]` — and this project forbids precisely that:

> No component hardcodes a colour, a spacing, a radius, a shadow or a duration.

A design system with fifteen base components and a strict token rule looks like
a case for CSS Modules or vanilla-extract, and the argument deserved to be made
rather than waved away.

## Why Tailwind stays

Because of *how* it is wired here. `tokens.css` opens with:

```css
@theme static {
  --color-canvas: oklch(17.5% 0.012 200);
  --spacing: 0.25rem;
  --text-sm: 0.875rem;
```

Tailwind 4 generates its utilities **from** that block. `bg-canvas` exists
because `--color-canvas` does; `p-3` is three times `--spacing` and cannot be
anything else. The utility surface is not a parallel vocabulary sitting beside
the tokens — it is a projection of them. Delete a token and the class that used
it stops existing, loudly, at build time.

That inverts the usual objection. Here Tailwind is not the escape hatch from
the design system, it is the mechanism that enforces it: to write a value that
is not a token you have to reach for `style={{}}`, which is visible in review
in a way `padding: 13px` inside a stylesheet is not.

CSS Modules would give scoping and nothing else — the token rule would go back
to being a convention people remember, and reviewers would be the only check.

## Why `static` rather than plain `@theme`

Without the keyword, Tailwind emits only the variables it has seen a matching
utility class for. yagit reads some tokens another way: `Avatar` tints itself
with `var(--color-lane-N)` through an inline style, chosen by a hash of the
author's name, so no class mentions those variables anywhere in the source. They
would be absent, and their colours would resolve to nothing — a silent visual
failure with no error attached.

[0003](0003-the-commit-graph-is-svg.md) removed the other reason for `static`:
the canvas used to read lane colours back out of the document at runtime. The
inline-style readers remain, so `static` remains.

## What `static` does not reach: the `--shadow-*` namespace

A note on the boundary of the guarantee above, not a change to it.

`static` is a promise about **emission**: the variable is in the stylesheet
whether or not a class mentions it. It says nothing about whether the utility
bearing that token's name goes on to **read** the variable — and one namespace
does not. Tailwind resolves `--shadow-*` at build time and inlines the value
into the rule, so that a later `shadow-<colour>` can substitute a colour of its
own:

```css
.shadow-dialog {
  --tw-shadow: 0 24px 64px oklch(0% 0 0 / 0.55);
}
```

The dark literal is baked in there. A `:root[data-theme='light']` block
redefining `--shadow-dialog` therefore sets a custom property that nothing on
the page ever reads, and no form of `@theme` fixes it: the failure is not a
missing variable. The light theme wore the dark theme's shadows for a release
because of it — a 45%-black drop shadow drawn for a near-black page, which on
white is a grey smear under every menu, toast, tooltip and dialog.

The three shadows now sit outside `@theme`, beside the durations, and reach an
element through an `@utility` that dereferences them at paint time. That is the
cost paragraph below, paid deliberately: outside `@theme` there is no
build-time failure to catch a deleted token, and the utility paints nothing
instead. `web/src/design/tokens.test.ts` is what replaces the build error — it
asserts the shadows stay out of `@theme`, and that each one reaches an element
through a `var()` rather than a literal.

Nothing above changes. Tailwind still projects the utility surface from the
tokens, and `static` is still what keeps the lane and avatar colours — read
through inline styles, named by no class anywhere — in the stylesheet at all.

## What it costs

Every token ships whether or not anything uses it. The whole stylesheet is
35 KiB raw, 9 KiB gzipped, which is the price of never debugging a colour that
is missing because nobody wrote its class name.

Tailwind's own utilities also remain reachable in full — `flex`, `grid`,
`gap-*` and the rest — and nothing stops someone writing `p-[13px]` on a bad
day. That is what review is for, and it is one grep to find.
