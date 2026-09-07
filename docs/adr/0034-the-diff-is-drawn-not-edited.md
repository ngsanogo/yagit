# 0034 — The diff is drawn, not edited

**Status:** reverses [0004](0004-diffs-are-codemirror.md), which chose
CodeMirror 6 with `@codemirror/merge`. No editor component is used, and none is
a dependency.

## What was decided before

0004 replaced Monaco with CodeMirror, and it was right about everything it
argued against Monaco: a theme of hex strings cannot read `oklch()` tokens, and
it fails by turning them red rather than by saying so. That reasoning stands
and is not what this record touches.

What it also decided — and this is the part that never happened — is that
`@codemirror/merge`'s `unifiedMergeView` would be the diff, with its accept and
reject controls in the gutter doing hunk and line staging.

That was written before the daemon had a diff route. The interface was built
afterwards, and it was built without the dependency: `web/package.json` names
no CodeMirror package, `web/src/app/DiffView.tsx` renders the patch out of
plain elements, and `web/src/app/FileEditor.tsx` — the conflict pane
[0017](0017-yagit-edits-files.md) added — is a `<textarea>`. The decision and
the code have disagreed since phase 7. This record makes the code the decision.

## Decision

The diff is a React component over data the daemon already parsed. No editor
component, for diffs or for the conflict pane.

`FileDiff` arrives as hunks of lines, each line carrying its side and its
index. Drawing it is a list; choosing in it is a set of indices.

## Why the merge view was the wrong shape after all

**A patch has an identity; a document does not.** Staging three lines out of a
hunk is not `git add` — git takes whole paths — so it is a patch built in the
browser and applied by the daemon, and it is only correct for the diff it was
built from. Every action carries that diff's fingerprint back, and the daemon
refuses it if the file moved on in between. `unifiedMergeView` models accepting
a change into a *document*, and a document has nothing to send back: there is
no version of this check that the merge view's own controls could have
performed. The gutter controls 0004 wanted are the ones this feature cannot
use.

**The view deliberately stops drawing.** `MAX_DRAWN_LINES` is 2000, counted
across everything on screen at once rather than per file, because a commit's
patch is every file it touched and a lockfile is forty thousand lines nobody
reads. Past it the lines stop and the number is named — the same refusal the
graph makes at 24 columns. An editor holds the whole document by construction;
that is what an editor is for. Capping it means fighting it.

**Selection is the feature, and it is not text selection.** Click a line, or
shift-click to close a range over the *changed* lines only — the context lines
in between are skipped, because they are not stageable. That is a list widget's
behaviour, and it is thirty lines of React over an array. In an editor it is a
selection model fought against the one already there.

**The bundle argument reverses.** 0004 measured CodeMirror at 130 KiB gzipped
against Monaco's 700 and called it five times smaller. Against nothing it is
130 KiB, on the screen that is the reason to open the application at all.

## What it costs

**No syntax highlighting in a diff.** This is the real loss, and it is the
thing an editor component gives away for free. Added and removed lines are
distinguished by colour and by sign, which is what a diff is *for*; language
colouring on top of that is a comfort this does not have and would have had.
Nothing about the current shape prevents adding it later — a highlighter over
each line's text is independent of who draws the list — and it would be a new
record when it happens, because it is a dependency.

**No editing in the diff.** Correct here: the diff pane stages, it does not
write. The one place yagit writes a file is the conflict pane, and 0017 argued
that case on its own terms and reached the same answer this record does.

**Line-level virtualisation is ours.** The 2000-line cap is what stands in for
it. A diff that legitimately wants more than that on screen would need real
virtualisation, and an editor component ships one. That trade was made in
favour of the cap, which says what it is not drawing.

## What this does not reverse

0004's reading of Monaco, and its rule that a component which cannot take a
design token is a component this project cannot theme. That rule is why no
editor is here now.
