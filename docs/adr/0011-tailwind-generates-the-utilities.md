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

## What it costs

Every token ships whether or not anything uses it. The whole stylesheet is
35 KiB raw, 9 KiB gzipped, which is the price of never debugging a colour that
is missing because nobody wrote its class name.

Tailwind's own utilities also remain reachable in full — `flex`, `grid`,
`gap-*` and the rest — and nothing stops someone writing `p-[13px]` on a bad
day. That is what review is for, and it is one grep to find.
