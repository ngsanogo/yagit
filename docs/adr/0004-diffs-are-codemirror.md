# 0004 — Diffs are CodeMirror 6

**Status:** reversed by [0034](0034-the-diff-is-drawn-not-edited.md), which
chose no editor component at all. What follows is why Monaco was dropped, and
that half still holds; the CodeMirror half was never built.

It reverses the earlier decision to use Monaco's `DiffEditor`.

## What was decided before

> **Diffs**: Monaco DiffEditor.

Monaco is VS Code's editor. It is excellent, it is what people reach for, and
it is the wrong shape for this project on three counts — one of which
contradicts the project's first rule outright.

## 1. Monaco cannot read the design tokens, and fails silently when asked

A Monaco theme is a JavaScript object of colour strings, and this is how it
parses them:

```js
// monaco-editor/esm/vs/base/common/color.js
static fromHex(hex) {
    return Color.Format.CSS.parseHex(hex) || Color.red;
}
```

Hex, and nothing else. Not `var(--color-surface)`, not
`oklch(21.5% 0.013 200)` — yagit's palette is OKLCH precisely so that lanes at
equal lightness are equally salient, and that notation goes in as `null` and
comes out **red**. No exception, no warning. Two rules of this project fall at
once: *no component hardcodes a colour*, and *errors should never pass
silently*.

Living with it means a second palette, hand-converted to hex, in TypeScript,
maintained beside `tokens.css` and diverging from it the first time a grey is
nudged. That is the exact duplication the token file exists to prevent, and it
would be introduced for the one screen where colour carries meaning — added
lines against removed ones.

CodeMirror is themed with CSS. `.cm-changedLine { background: var(--color-added-bg) }`
is the whole mechanism, tokens included, theme switching included, with nothing
to keep in sync.

## 2. Hunk and line staging is what CodeMirror's diff view already is

Phase 5 stages by file, then by hunk, then by line. That means interactive
controls in the gutter of a diff, and gutters are CodeMirror's core vocabulary:
`gutter`, `GutterMarker`, `Decoration`, all first-class and all documented.
`@codemirror/merge` ships `unifiedMergeView` with accept and reject controls
per chunk — the staging interaction, already built.

Monaco's diff editor is built to *show* a comparison. Its gutter is its own,
and putting a widget in it means working against the widget system rather than
with it.

## 3. The size, measured

Bundled with esbuild, minified, no languages, nothing else:

| | raw | gzipped |
| --- | --- | --- |
| Monaco: `editor.api` + the diff editor contribution | 2 582 KiB | 663 KiB |
| CodeMirror 6: `basicSetup` + `@codemirror/merge` + one language | 387 KiB | 130 KiB |

yagit's entire frontend, when this was measured, was 219 KiB raw and 68 KiB
gzipped. Monaco is not a dependency of the application; it is ten times the
application. It also brings web workers, which the single-origin model then has
to serve out of the embedded assets — one more mechanism for one more reason.

CodeMirror is five times smaller before tree-shaking has been asked to do
anything, and it is modular by design: the languages yagit highlights are
chosen, not inherited.

## The decision

`@codemirror/merge` for diffs, `@codemirror/view` and `@codemirror/state`
underneath, one language package per syntax worth highlighting, themed entirely
from `tokens.css`. Phase 7 is where it lands; phase 5's staging gutter is built
on the same primitives.

## What it costs

Monaco does more out of the box: minimap, code lens, IntelliSense, a command
palette. yagit wants none of them. It is showing a diff and letting you stage
part of it, not editing a project.

Monaco is also the more familiar name, and "we use what VS Code uses" is an
easier sentence than this page. That is not an argument, it is a shortcut.
