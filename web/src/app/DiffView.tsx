import { useMemo, useState, type KeyboardEvent, type ReactNode } from 'react';

import type { DiffHunk, DiffLine, DiffSide, FileDiff } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { cx } from '../lib/cx';
import { pointerNote, type PointerNote as PointerNoteText } from './lfsDiff';

/**
 * A file's diff, with the lines that can be chosen.
 *
 * Choosing is what this screen is for. `git add` takes whole paths, so
 * anything finer is a patch — and a patch is only correct for the diff it was
 * built from, which is why every action carries the diff's own fingerprint
 * back to the daemon. If the file moved on in between, the request is refused
 * rather than applied to whatever is there now.
 *
 * Two ways to choose, and they are the same mechanism: a hunk's button names
 * every changed line in that hunk, a click on a line names that one. There is
 * no third path and no separate "hunk staging" anywhere in the daemon.
 */

/**
 * How many lines are drawn before the view says it is not drawing the rest.
 *
 * A generated file, a lockfile, a vendored dependency: diffs of tens of
 * thousands of lines exist and nobody reads them line by line. Past this the
 * lines stop and the number is named, the way the graph names the width it
 * will not draw — the whole-file actions still work, because they are `git
 * add` and need no patch at all.
 *
 * Counted across everything drawn at once, not per file: a commit's patch is
 * every file it touched, and a cap granted to each of five hundred files is no
 * cap at all.
 */
export const MAX_DRAWN_LINES = 2000;

export type DiffAction = 'stage' | 'unstage' | 'discard';

interface DiffViewProps {
  diff: FileDiff;
  side: DiffSide;
  /**
   * Runs an action over a set of line indices of this diff, and answers
   * whether it went.
   *
   * A discard is proposed to the user before it runs, and a selection thrown
   * away at the moment the question is asked is one the user has to rebuild
   * after saying no — to the one dialog that exists to let them say it.
   */
  onApply: (action: DiffAction, indices: number[]) => boolean;
  busy: boolean;
}

/** How each side reads in the header. git's own words for git's own states. */
const SIDE_LABELS: Record<DiffSide, string> = {
  staged: 'staged',
  unstaged: 'not staged',
  untracked: 'not tracked',
};

export function DiffView({ diff, side, onApply, busy }: DiffViewProps) {
  const [selected, setSelected] = useState<ReadonlySet<number>>(new Set());
  // Where the last click landed, so Shift+click has a range to close. It is
  // also where the pane's single tab stop sits: the line the keyboard last
  // touched is the line it should come back to, and a roving tab stop needs
  // exactly one line to be the one — see the arrow keys below.
  const [anchor, setAnchor] = useState<number>();

  // The changed lines in the order they are drawn, which is the order a
  // Shift+click range runs through. Derived rather than stored: the diff is
  // refetched on every change, and a stored copy would describe the file as it
  // was before the last stage.
  const changed = useMemo(() => changedLines(diff), [diff]);

  // The same lines by index, because the arrow keys move between line NUMBERS
  // and toggling wants the line itself. Built once per diff rather than
  // searched per keystroke: a rewritten lockfile is two thousand rows, and a
  // linear scan of them on every press is a key that feels stuck.
  const lineAt = useMemo(() => {
    const byIndex = new Map<number, DiffLine>();
    for (const hunk of diff.hunks) {
      for (const line of hunk.lines) {
        if (line.kind !== 'context') {
          byIndex.set(line.index, line);
        }
      }
    }
    return byIndex;
  }, [diff]);

  // The selection is dropped whenever the diff changes, because an index means
  // something different in a different diff. Keying on the fingerprint rather
  // than on the path is what makes that exact: the same file re-read after a
  // stage is a new diff, and holding the old selection would carry a choice
  // made about lines that are no longer there.
  const [seenDiff, setSeenDiff] = useState(diff.id);
  if (seenDiff !== diff.id) {
    setSeenDiff(diff.id);
    setSelected(new Set());
    setAnchor(undefined);
  }

  if (diff.binary) {
    return (
      <Notice
        title="Binary file"
        description="git shows no content for this file, so there are no lines to choose between. Stage or discard it whole from the list."
      />
    );
  }

  // Before the hunks rather than instead of them. A pointer diff IS readable —
  // three lines of metadata — and reading it is exactly the trap: it says an
  // OID changed where somebody expected to see their file change. The lines
  // stay, because staging one of them is still what tracking a file under LFS
  // records; the sentence above them says what they stand for.
  const pointer = pointerNote(diff);

  if (diff.hunks.length === 0) {
    return <ModeOrEmpty diff={diff} />;
  }

  function toggle(line: DiffLine, extend: boolean) {
    setSelected((current) => {
      const next = new Set(current);

      if (extend && anchor !== undefined) {
        // A range, in drawn order. Shift+click always ADDS: taking lines away
        // with it would make an accidental Shift undo a careful selection.
        const from = changed.indexOf(anchor);
        const to = changed.indexOf(line.index);
        if (from >= 0 && to >= 0) {
          for (const index of changed.slice(Math.min(from, to), Math.max(from, to) + 1)) {
            next.add(index);
          }
          return next;
        }
      }

      if (next.has(line.index)) {
        next.delete(line.index);
      } else {
        next.add(line.index);
      }
      return next;
    });
    setAnchor(line.index);
  }

  function apply(action: DiffAction, indices: number[]) {
    if (!onApply(action, indices)) {
      return;
    }
    setSelected(new Set());
    setAnchor(undefined);
  }

  /**
   * ArrowUp and ArrowDown between CHANGED lines, Shift with them to extend.
   *
   * Every changed line is a button, and a 600-line rewrite is 600 of them: as
   * plain tab stops they put the next hunk's header several hundred presses
   * away, which is a keyboard path that exists and cannot be walked. So the
   * pane keeps one tab stop — the anchor, the line the keyboard last touched —
   * and the arrows move between the rest, which is the same roving pattern the
   * design system's tablist and menu already use.
   *
   * Context lines are skipped rather than stepped over, because they are not
   * choosable: `changed` is the sequence a Shift+click range runs through, and
   * the arrows walk exactly that so the two ways of building a selection agree
   * about what is next.
   */
  function moveByKey(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') {
      return;
    }

    // instanceof rather than a cast: the key may have been pressed on a hunk
    // button, where there is no line to move from and the pane should scroll
    // the way it always does.
    const from =
      event.target instanceof HTMLElement ? event.target.closest('[data-diff-line]') : null;
    if (!(from instanceof HTMLElement) || from.dataset.diffLine === undefined) {
      return;
    }

    const at = changed.indexOf(Number(from.dataset.diffLine));
    const next = at < 0 ? undefined : changed[at + (event.key === 'ArrowDown' ? 1 : -1)];
    if (next === undefined) {
      return;
    }

    const target = event.currentTarget.querySelector(`[data-diff-line="${next}"]`);
    if (!(target instanceof HTMLElement)) {
      return;
    }

    // Or the pane scrolls a row under the focus it has just moved, and the two
    // movements land the reader somewhere neither of them meant.
    event.preventDefault();
    target.focus();

    const line = lineAt.get(next);
    if (event.shiftKey && line !== undefined) {
      toggle(line, true);
      return;
    }
    setAnchor(next);
  }

  const drawn = countDrawnLines(diff);
  const gutter = gutterWidth([diff]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <DiffHeader
        diff={diff}
        side={side}
        count={selected.size}
        busy={busy}
        onApply={(action) => apply(action, [...selected])}
        onClear={() => {
          setSelected(new Set());
          setAnchor(undefined);
        }}
      />

      <div className="min-h-0 flex-1 overflow-auto font-mono text-xs" onKeyDown={moveByKey}>
        <Capped drawn={drawn}>Hunk buttons act on the lines drawn, not on the rest.</Capped>

        {pointer !== undefined && <PointerNote note={pointer} />}

        {diff.hunks.map((hunk, position) => (
          <HunkView
            // Hunks have no identity of their own; their position in this diff
            // is the only stable thing about them, and the whole list is
            // replaced whenever the diff changes.
            key={position}
            hunk={hunk}
            firstDrawnLine={firstLineOfHunk(diff, position)}
            gutter={gutter}
            actions={{
              side,
              busy,
              selected,
              tabStop: anchor ?? changed[0],
              onToggle: toggle,
              onApply: apply,
            }}
          />
        ))}

        <Truncated drawn={drawn}>
          Staging the whole file still works — it is <code className="text-ink">git add</code>, and
          needs no patch.
        </Truncated>
      </div>
    </div>
  );
}

/**
 * What the actions are called on each side.
 *
 * Named here once. "Stage" on the staged list would be a button that undoes
 * itself, and the mapping from a row to the operations it offers is exactly
 * the sort of thing that drifts when it is written out three times.
 */
function actionsFor(side: DiffSide): { primary: DiffAction; label: string; discardable: boolean } {
  if (side === 'staged') {
    return { primary: 'unstage', label: 'Unstage', discardable: false };
  }
  return { primary: 'stage', label: 'Stage', discardable: true };
}

/**
 * The path, which side is being shown, and whatever can be done to the
 * selection.
 *
 * The path lives here rather than in the panel's title for a reason that is
 * not cosmetic: a panel title is set in capitals, and a path is
 * case-sensitive. `PARSER.GO` is not a file, and a screen that prints it is
 * teaching the reader something false about their own repository.
 *
 * The bar holds its height whether or not anything is selected. A control
 * strip that appears and disappears shifts every line under it by its own
 * height, and the line the user was about to click moves out from under the
 * pointer.
 *
 * It is its own container, and the hint below is gated on THAT rather than on
 * the window. The bar is as wide as the Diff panel, which is the window minus
 * the file list beside it and can be half of it; a viewport breakpoint here
 * measures a box this text has never been inside, and answers about a width
 * the reader does not have.
 */
function DiffHeader({
  diff,
  side,
  count,
  busy,
  onApply,
  onClear,
}: {
  diff: FileDiff;
  side: DiffSide;
  count: number;
  busy: boolean;
  onApply: (action: DiffAction) => void;
  onClear: () => void;
}) {
  const { primary, label, discardable } = actionsFor(side);
  const cameFrom = diff.old_path !== undefined && diff.old_path !== '' ? diff.old_path : undefined;

  return (
    <div className="@container flex h-9 shrink-0 items-center gap-3 border-b border-line px-3">
      <span className="flex min-w-0 items-baseline gap-2">
        <span className="truncate font-mono text-xs text-ink" title={diff.path}>
          {diff.path}
        </span>
        {/* Where a rename came from, in the words a commit's patch already
            uses for the same fact. Truncating rather than shrink-0, which is
            the mistake the hint beside it used to make: a string that holds
            its full width wins that width from the one thing in this bar that
            names what is about to be staged. Both paths give ground together
            instead, and the title carries whichever of them is cut. */}
        {cameFrom !== undefined && (
          <span
            className="min-w-0 truncate font-mono text-2xs text-ink-subtle"
            title={`Renamed from ${cameFrom}`}
          >
            ← {cameFrom}
          </span>
        )}
        <span className="shrink-0 text-2xs text-ink-subtle">{SIDE_LABELS[side]}</span>
      </span>

      {count === 0 ? (
        <p className="ml-auto hidden shrink-0 text-2xs text-ink-subtle @2xl:block">
          Click a line to choose it, Shift+click for a range, or {label.toLowerCase()} a whole hunk
          from its header
        </p>
      ) : (
        <span className="ml-auto flex shrink-0 items-center gap-2">
          {/* The sentence above says Shift+click, and the moment a first line
              is chosen it is replaced by these buttons — which is exactly when
              the reader first has an anchor to extend from. The short form
              survives into that state. */}
          <span className="hidden text-2xs text-ink-subtle @3xl:inline">Shift+click to extend</span>
          <Badge tone="accent">{count} selected</Badge>
          {/* Secondary, not primary. The screen already has its one primary
              button — Commit — and that is the action it is ultimately
              waiting for; staging is a step on the way. The accent badge
              beside this is what draws the eye to the selection. */}
          <Button size="sm" variant="secondary" disabled={busy} onClick={() => onApply(primary)}>
            {label} {count === 1 ? 'line' : 'lines'}
          </Button>
          {discardable && (
            <Button size="sm" variant="danger" disabled={busy} onClick={() => onApply('discard')}>
              Discard
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onClear}>
            Clear
          </Button>
        </span>
      )}
    </div>
  );
}

/** What can be done to a hunk, and to the lines inside it. */
interface HunkActions {
  side: DiffSide;
  busy: boolean;
  selected: ReadonlySet<number>;
  /** The one changed line that is in the tab order; see moveByKey. */
  tabStop: number | undefined;
  onToggle: (line: DiffLine, extend: boolean) => void;
  onApply: (action: DiffAction, indices: number[]) => void;
}

interface HunkViewProps {
  hunk: DiffHunk;
  /**
   * Where this hunk starts in everything being drawn, so the lines past the
   * cap can be left out without every hunk having to count for itself. A
   * commit's patch is several files, and each one starts where the last one
   * ended.
   */
  firstDrawnLine: number;
  /** The width both line-number columns are drawn at; see gutterWidth. */
  gutter: string;
  /**
   * Absent in a commit's diff.
   *
   * A commit is history: there is nothing in it to stage, and the same absence
   * is what makes its lines unclickable. One renderer either way — the gutters,
   * the markers, the wrapping and the cap are the same code — so the two views
   * cannot drift into drawing the same patch two different ways.
   */
  actions?: HunkActions;
}

function HunkView({ hunk, firstDrawnLine, gutter, actions }: HunkViewProps) {
  // How many of this hunk's lines the cap still has room for — nought or fewer
  // for a hunk that is entirely past it. A commit's patch is every hunk of
  // every file it touched, so most of them are never drawn at all, and the one
  // the cap lands in the middle of is drawn in part.
  //
  // The word diff is asked the same number rather than being handed the whole
  // hunk. A regenerated lockfile arrives as one hunk of sixty thousand lines,
  // and tokenising all of them to mark the two thousand on screen is precisely
  // the freeze the cap was put there to prevent.
  const drawable = MAX_DRAWN_LINES - firstDrawnLine;
  const marked = useMemo(
    () => (drawable > 0 ? markedSpans(hunk, drawable) : new Map<number, LineSpan[]>()),
    [drawable, hunk],
  );

  if (drawable <= 0) {
    return null;
  }

  return (
    <section className="group/hunk">
      <header className="sticky top-0 flex items-center gap-2 bg-sunken px-3 py-1">
        <span className="text-2xs text-ink-subtle">
          @@ -{hunk.old_start},{hunk.old_lines} +{hunk.new_start},{hunk.new_lines} @@
        </span>
        {hunk.heading !== '' && (
          <span className="truncate text-2xs text-ink-muted">{hunk.heading}</span>
        )}

        {actions !== undefined && (
          <HunkButtons hunk={hunk} firstDrawnLine={firstDrawnLine} actions={actions} />
        )}
      </header>

      <div>
        {hunk.lines.map((line, offset) =>
          offset >= drawable ? null : (
            <LineView
              key={line.index}
              line={line}
              gutter={gutter}
              spans={marked.get(line.index)}
              selected={actions?.selected.has(line.index) ?? false}
              tabStop={actions?.tabStop === line.index}
              onToggle={actions?.onToggle}
            />
          ),
        )}
      </div>
    </section>
  );
}

/**
 * Staging a whole hunk, from the hunk.
 *
 * Visible at rest rather than only on hover. Staging by hunk is the coarse
 * grain of this screen's one feature and the grain most people reach for
 * first; drawn at zero opacity it is a feature nothing on screen mentions, and
 * a user who never happens to sweep the pointer across a hunk header concludes
 * the pane stages whole files and single lines and nothing between. Quiet
 * until the hunk is under the pointer or holds the focus, which is the same
 * bargain the row actions in the change list and the reference sidebar strike.
 *
 * Quiet, and no quieter than 80%: a ghost button is --color-ink-muted, and
 * against the header it stands on that reads 4.99:1 in the light theme at this
 * opacity and 3.89 at 70%. Zero opacity was exempt from the floor because
 * nothing invisible has to be read — the moment these are drawn at all, they
 * are text, and the AA floor is the whole of what decides how faint they go.
 */
function HunkButtons({
  hunk,
  firstDrawnLine,
  actions,
}: {
  hunk: DiffHunk;
  firstDrawnLine: number;
  actions: HunkActions;
}) {
  const { primary, label, discardable } = actionsFor(actions.side);
  const changedHere = changedIn(hunk, firstDrawnLine);

  return (
    <div
      className={cx(
        'ml-auto flex shrink-0 items-center gap-0.5 opacity-80',
        'transition-opacity transition-instant',
        'group-hover/hunk:opacity-100 group-focus-within/hunk:opacity-100',
      )}
    >
      {discardable && (
        <Button
          size="sm"
          variant="ghost"
          disabled={actions.busy || changedHere.length === 0}
          onClick={() => actions.onApply('discard', changedHere)}
        >
          Discard hunk
        </Button>
      )}
      <Button
        size="sm"
        variant="ghost"
        disabled={actions.busy || changedHere.length === 0}
        onClick={() => actions.onApply(primary, changedHere)}
      >
        {label} hunk
      </Button>
    </div>
  );
}

/**
 * The two tints a line's kind paints: the whole line, and the run inside it
 * that actually moved.
 *
 * Kept as one table because they are one decision. The pale wash says which
 * side of the diff the line is on; the stronger one says which characters of
 * it are the change, and a colour picked for the second without the first
 * beside it is how the two stop being read as the same colour.
 */
const KIND_TINTS: Record<DiffLine['kind'], { whole: string; changed: string }> = {
  added: { whole: 'bg-added/10', changed: 'bg-added/25' },
  removed: { whole: 'bg-deleted/10', changed: 'bg-deleted/25' },
  context: { whole: '', changed: '' },
};

const MARKERS: Record<DiffLine['kind'], string> = {
  added: '+',
  removed: '-',
  context: ' ',
};

/**
 * What a screen reader is told this line is.
 *
 * Built from the text the daemon sent, which is already bounded — the label of
 * a minified line used to be the whole line. The count is spoken when there is
 * one, because a name that simply stops is a name that lies about the line.
 *
 * The whitespace note is the same fact the glyphs draw. A line whose only
 * change is an indent reads identically to the line above it in this name, and
 * a reader who cannot see the dots has nothing else to go on.
 */
function accessibleName(line: DiffLine, spans: readonly LineSpan[] | undefined): string {
  const kind = line.kind === 'added' ? 'Added' : 'Removed';
  const rest =
    line.truncated !== undefined && line.truncated > 0
      ? `, and ${line.truncated.toLocaleString()} more characters not shown`
      : '';
  return `${kind} line: ${line.text}${rest}${whitespaceNote(spans) ?? ''}`;
}

/** The one sentence the glyphs are drawing, for a reader who cannot see them. */
function whitespaceNote(spans: readonly LineSpan[] | undefined): string | undefined {
  if (spans === undefined) {
    return undefined;
  }
  if (spans.some((span) => span.glyphs && span.changed)) {
    return ', where what changed is the whitespace';
  }
  return spans.some((span) => span.glyphs) ? ', with trailing whitespace' : undefined;
}

/**
 * One line of a diff.
 *
 * onToggle is what makes a line choosable, and its absence is what makes a
 * diff read-only — a commit's diff has nothing to stage. The line renders the
 * same either way, which is the point of it being one component.
 *
 * Two elements carry backgrounds and they own different things: the row owns
 * the STATE — hovered, chosen — and the span inside it owns the KIND. They
 * used to be one element with two background utilities on it, which is not a
 * choice at all but a question put to the cascade: `bg-accent-soft/60` sorts
 * before `bg-added/10`, so a chosen added line painted as an ordinary added
 * one and the only evidence of the choice was a 2px stripe at the left edge.
 * Layered, a chosen line is an accent wash with the kind's colour still over
 * it, and a hovered one keeps its green or its red instead of losing it to an
 * opaque highlight.
 *
 * The two number columns sit OUTSIDE that span deliberately. Every wash the
 * row can wear lightens what is under it, and the numbers are the faintest ink
 * in the pane: measured against --color-ink-subtle, a hovered added line's
 * numbers come to 4.34:1 with the kind tint over them and 5.29:1 without,
 * which is the AA floor on the wrong side of a state a pointer produces by
 * accident. Outside the tint they read against the row alone, and the coloured
 * band starts where the diff's own +/- marker does.
 */
function LineView({
  line,
  gutter,
  spans,
  selected,
  tabStop,
  onToggle,
}: {
  line: DiffLine;
  gutter: string;
  spans: readonly LineSpan[] | undefined;
  selected: boolean;
  tabStop: boolean;
  onToggle?: (line: DiffLine, extend: boolean) => void;
}) {
  const changeable = line.kind !== 'context' && onToggle !== undefined;
  const note = whitespaceNote(spans);

  const body =
    spans === undefined
      ? line.text
      : spans.map((span, at) => <Span key={at} span={span} tint={KIND_TINTS[line.kind].changed} />);

  const number = cx(
    gutter,
    'shrink-0 pr-2 text-right text-2xs text-ink-subtle tabular select-none',
  );

  const content = (
    <>
      {/* Both line numbers, always, in a fixed-width column. An added line has
          no old number and a removed one has no new number; leaving the space
          empty rather than collapsing it is what keeps the two columns from
          jittering down the file. */}
      <span className={number}>{line.old_line === 0 ? '' : line.old_line}</span>
      <span className={number}>{line.new_line === 0 ? '' : line.new_line}</span>

      <span className={cx('flex min-w-0 flex-1 items-start', KIND_TINTS[line.kind].whole)}>
        <span
          aria-hidden="true"
          className={cx(
            'w-4 shrink-0 text-center select-none',
            line.kind === 'added' && 'text-added',
            line.kind === 'removed' && 'text-deleted',
            line.kind === 'context' && 'text-ink-subtle',
          )}
        >
          {MARKERS[line.kind]}
        </span>

        {/* A read-only row has no aria-label to carry the +/- through, and the
            glyph beside it is aria-hidden precisely so it is not read as
            punctuation: without this, an addition, a deletion and a context
            line are the same text to a screen reader, which is the state a
            patch is least readable in. select-none so a copy of the patch is
            still the patch. */}
        {!changeable && line.kind !== 'context' && (
          <span className="sr-only select-none">
            {line.kind === 'added' ? 'Added line: ' : 'Removed line: '}
          </span>
        )}

        {/* pre-wrap, not pre: a long line has to wrap rather than push a
            horizontal scrollbar under the whole file, and the leading spaces of
            indented code have to survive. wrap-anywhere, not break-all: both
            keep a minified line from forcing that scrollbar, but break-all
            takes the break at whatever character the edge falls on even when a
            space sat three characters earlier, which cuts every wrapped line of
            prose, Markdown and comments mid-word. */}
        <span className="min-w-0 flex-1 pr-3 wrap-anywhere whitespace-pre-wrap text-ink">
          {body}
          {line.truncated !== undefined && line.truncated > 0 && (
            /* Said, not silently done. A line that stops with no sign of it
               reads as the file having ended there. */
            <span className="ml-2 text-2xs text-ink-subtle">
              (+{line.truncated.toLocaleString()} more characters, not shown)
            </span>
          )}
          {line.no_newline && (
            <span className="ml-2 text-2xs text-ink-subtle">(no newline at end of file)</span>
          )}
          {!changeable && note !== undefined && <span className="sr-only select-none">{note}</span>}
        </span>
      </span>
    </>
  );

  const shared = cx(
    'flex w-full items-start border-l-2 text-left',
    selected ? 'border-accent bg-accent-soft/60' : 'border-transparent',
  );

  if (!changeable) {
    return <div className={shared}>{content}</div>;
  }

  return (
    <button
      type="button"
      // The pane's roving tab stop. Every other changed line stays reachable
      // by arrow key rather than by Tab; see moveByKey.
      tabIndex={tabStop ? 0 : -1}
      data-diff-line={line.index}
      aria-pressed={selected}
      aria-label={accessibleName(line, spans)}
      onClick={(event) => {
        // A press, a drag and a release inside one line is a text selection,
        // and the browser reports it as a click on the element both ends
        // landed in. Without this, copying an identifier or an error string
        // out of a diff silently arms a staging selection — and the header
        // answers that by swapping the hint for a Stage button and a red
        // Discard one. Enter from the keyboard arrives with nothing selected,
        // so the keyboard path is untouched.
        if ((window.getSelection()?.toString() ?? '') !== '') {
          return;
        }
        onToggle(line, event.shiftKey);
      }}
      className={cx(
        shared,
        'cursor-pointer outline-none focus-visible:focus-ring',
        // Not on a chosen line: --color-hover is opaque, so hovering one would
        // paint over the accent wash and take away the only full-width mark
        // the choice has. The other row lists in the workbench draw the same
        // bargain the same way.
        !selected && 'hover:bg-hover',
      )}
    >
      {content}
    </button>
  );
}

/**
 * One run of a line: the part that moved, the whitespace that cannot be seen,
 * or ordinary text.
 *
 * The glyphs are drawn INSTEAD of the whitespace and the whitespace is kept
 * beside them at zero width, which is the only arrangement that satisfies both
 * halves of the problem. Replacing the characters outright would put a middle
 * dot into every copy taken out of the pane, and pasting `····fmt.Println` into
 * an editor is a worse day than not seeing the indent change; leaving them
 * alone and tinting the run instead cannot distinguish a tab from the four
 * spaces that replaced it, which is the commonest whitespace change there is.
 * So the visible run is select-none and aria-hidden, and the twin beside it —
 * clipped to no width, whitespace-pre so the copy keeps every character — is
 * what a selection actually picks up. The line numbers in this same component
 * have relied on select-none for a clean copy since the pane was written.
 *
 * A tab keeps its own character after the arrow rather than being replaced by
 * a fixed number of spaces: the arrow occupies one column and the real tab
 * then advances to the next tab stop, so the text after it lands exactly where
 * it lands on every other line. An arrow alone would be one column wide where
 * the tab was four, and the file would step in and out down the page.
 *
 * align-bottom on the twin is not decoration. An inline-block whose overflow
 * is not visible takes its baseline from its bottom margin edge rather than
 * from the line inside it, so the twin — a whole line-height tall, standing on
 * the row's baseline — grows the line box above it and the row comes out
 * taller than every row around it. Aligning it to the bottom of the line
 * instead makes it fit inside the height the row already had. A pane whose
 * rows change height wherever an indent changed is the jitter the fixed gutter
 * exists to prevent, arrived at from the other direction.
 */
function Span({ span, tint }: { span: LineSpan; tint: string }) {
  const marked = span.changed ? tint : undefined;

  if (!span.glyphs) {
    return marked === undefined ? <>{span.text}</> : <span className={marked}>{span.text}</span>;
  }

  return (
    <span className={marked}>
      <span aria-hidden="true" className="select-none">
        {glyphsFor(span.text)}
      </span>
      <span className="inline-block w-0 overflow-hidden align-bottom whitespace-pre">
        {span.text}
      </span>
    </span>
  );
}

/** A middle dot per space, an arrow per tab — and the tab itself, for width. */
function glyphsFor(text: string): string {
  return [...text].map((character) => (character === '\t' ? '→\t' : '·')).join('');
}

/**
 * A diff with no hunk is not always an empty diff.
 *
 * An empty file's creation, an empty file's deletion, a mode change, a rename:
 * each is a real change with no line in it, and saying "no changes" about one
 * would be telling the user their change does not exist.
 */
function ModeOrEmpty({ diff }: { diff: FileDiff }) {
  // git writes no hunk for a zero-byte file: `touch docs/.keep` produces a
  // header and nothing else. The flags are the only record that the file is
  // new or gone, and without them the screen tells the user their new file is
  // not a change.
  if (diff.added) {
    return (
      <Notice
        title="New file, empty"
        description="git has the file but there is nothing in it, so there are no lines to choose between. Stage it whole from the list."
      />
    );
  }

  if (diff.removed) {
    return (
      <Notice
        title="Deleted"
        description="The file was empty, so there are no lines to choose between. Stage or discard the deletion whole from the list."
      />
    );
  }

  if (
    diff.old_mode !== undefined &&
    diff.new_mode !== undefined &&
    diff.old_mode !== diff.new_mode
  ) {
    return (
      <Notice
        title="Mode changed"
        description={`The file's mode went from ${diff.old_mode} to ${diff.new_mode}. There is no content change, so there is nothing to choose line by line.`}
      />
    );
  }

  if (diff.old_path !== undefined && diff.old_path !== '') {
    return (
      <Notice
        title="Renamed"
        description={`This file came from ${diff.old_path}. A rename has no lines to choose between, so stage or unstage it whole.`}
      />
    );
  }

  return (
    <Notice
      title="Nothing on this side"
      description="git reports no change here. It may all be on the other side, or the file may have been put back the way it was."
    />
  );
}

/**
 * What a diff of an LFS pointer stands for, above the pointer itself.
 *
 * Inside the scrolling body rather than in the header, because it belongs to
 * the lines under it: a note pinned above a list of files would be a sentence
 * about whichever one the reader happened to be looking at.
 */
function PointerNote({ note }: { note: PointerNoteText }) {
  return (
    <p className="border-b border-line bg-sunken px-3 py-2 font-sans text-2xs text-ink-muted">
      <span className="text-ink">{note.title}.</span> {note.detail}
    </p>
  );
}

function Notice({ title, description }: { title: string; description: string }) {
  return <EmptyState title={title} description={description} className="py-10" />;
}

/**
 * Whether the cap bit.
 *
 * One comparison, so no view can draw a capped patch and fail to say so — and
 * so the two notices that say it cannot disagree about when to appear.
 */
function overCap(drawn: number): boolean {
  return drawn > MAX_DRAWN_LINES;
}

/**
 * That the patch is cut, said where the reader begins.
 *
 * The paragraph at the foot of the pane is the honest full version and it is
 * two thousand lines away: on a rewritten lockfile it sits fifty screens down,
 * which is a sentence only somebody who already knows the patch is cut will
 * ever reach. A reader staging from the top otherwise has no way to learn that
 * the file continues.
 */
function Capped({ drawn, children }: { drawn: number; children?: ReactNode }) {
  if (!overCap(drawn)) {
    return null;
  }

  return (
    <p className="border-b border-line bg-sunken px-3 py-2 font-sans text-2xs text-ink-muted">
      <span className="text-ink">
        Showing the first {MAX_DRAWN_LINES.toLocaleString('en-GB')} lines
      </span>{' '}
      of {drawn.toLocaleString('en-GB')}. {children}
    </p>
  );
}

/**
 * What the cap left out, or nothing when it was not reached.
 *
 * The comparison lives with the number rather than at each call site, so a
 * view cannot draw a truncated patch and forget to say that it did.
 */
function Truncated({ drawn, children }: { drawn: number; children?: ReactNode }) {
  if (!overCap(drawn)) {
    return null;
  }

  return (
    <p className="border-t border-line px-3 py-2 text-2xs text-ink-muted">
      {drawn.toLocaleString('en-GB')} lines in this patch; the first{' '}
      {MAX_DRAWN_LINES.toLocaleString('en-GB')} are shown. {children}
    </p>
  );
}

/**
 * A commit's patch: every file it touched, drawn once and capped once.
 *
 * The cap is the patch's rather than each file's, which is the only version
 * of it that bounds anything — a commit that regenerates a lockfile is one
 * file of sixty thousand lines, and a vendor drop is five hundred files of a
 * hundred. Both freeze a tab that draws every line it is given.
 */
export function ReadOnlyPatch({
  files,
  onFileHistory,
  onBlame,
}: {
  files: readonly FileDiff[];
  /** Opens the history of a path, starting at the commit being shown. */
  onFileHistory?: (path: string) => void;
  /** Opens the blame of a path at the commit being shown. */
  onBlame?: (path: string) => void;
}) {
  const starts = firstLineOfFile(files);
  const drawn = countPatchLines(files);
  // One width for the whole patch rather than one per file: the files are
  // drawn into a single scroll region, and a gutter that resized at each
  // header would step the whole page sideways as the reader went down it.
  const gutter = gutterWidth(files);

  return (
    <>
      <Capped drawn={drawn} />

      {files.map((file, position) => (
        <ReadOnlyDiff
          key={file.path}
          diff={file}
          drawnBefore={starts[position] ?? 0}
          gutter={gutter}
          {...(onFileHistory === undefined ? {} : { onHistory: () => onFileHistory(file.path) })}
          {...(onBlame === undefined || file.binary || file.removed
            ? {}
            : { onBlame: () => onBlame(file.path) })}
        />
      ))}

      <Truncated drawn={drawn} />
    </>
  );
}

/**
 * A file's diff with nothing to click.
 *
 * A commit is history: there is nothing in it to stage, and a screen that
 * offered would be offering something the daemon has no route for. What is
 * shared with the staging view is everything below the interaction — the same
 * hunks, the same lines, the same cap — so the two cannot drift into rendering
 * the same patch two different ways.
 *
 * `drawnBefore` is how many lines the files above this one already spent of
 * the cap. A commit's patch is drawn all at once, so the budget is the
 * commit's, not each file's.
 */
function ReadOnlyDiff({
  diff,
  drawnBefore,
  gutter,
  onHistory,
  onBlame,
}: {
  diff: FileDiff;
  drawnBefore: number;
  gutter: string;
  onHistory?: () => void;
  onBlame?: () => void;
}) {
  const pointer = pointerNote(diff);

  return (
    <section className="border-b border-line last:border-b-0">
      <header className="flex items-baseline gap-2 bg-sunken px-3 py-1.5">
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-ink" title={diff.path}>
          {diff.path}
        </span>
        {diff.old_path !== undefined && diff.old_path !== '' && (
          <span className="shrink-0 font-mono text-2xs text-ink-subtle">← {diff.old_path}</span>
        )}
        {diff.added && <span className="shrink-0 text-2xs text-added">new file</span>}
        {diff.removed && <span className="shrink-0 text-2xs text-deleted">deleted</span>}
        {diff.binary && <span className="shrink-0 text-2xs text-ink-subtle">binary</span>}
        {onHistory !== undefined && (
          <Button size="sm" variant="ghost" onClick={onHistory}>
            History
          </Button>
        )}
        {onBlame !== undefined && (
          <Button size="sm" variant="ghost" onClick={onBlame}>
            Blame
          </Button>
        )}
      </header>

      {diff.binary ? (
        <p className="px-3 py-2 text-2xs text-ink-subtle">
          git shows no content for this file, so there is nothing to read line by line.
        </p>
      ) : (
        <div className="font-mono text-xs">
          {pointer !== undefined && <PointerNote note={pointer} />}

          {diff.hunks.map((hunk, position) => (
            <HunkView
              key={position}
              hunk={hunk}
              firstDrawnLine={drawnBefore + firstLineOfHunk(diff, position)}
              gutter={gutter}
            />
          ))}
        </div>
      )}
    </section>
  );
}

/**
 * Every added and removed line of a hunk that the cap let through: what a
 * selection can name.
 *
 * Bounded rather than complete, and the discard is why. A 3,200-line patch
 * draws its first 2,000 and the hunk header above them keeps a working
 * "Discard hunk" — unbounded, that button destroys 1,200 lines the pane
 * deliberately refused to show, and the only thing between it and silent loss
 * is a confirmation naming a count nobody can check. `firstDrawnLine` is where
 * this hunk starts in everything being drawn, which is what makes the answer
 * the same one the renderer arrived at.
 */
export function changedIn(hunk: DiffHunk, firstDrawnLine: number): number[] {
  const indices: number[] = [];
  hunk.lines.forEach((line, offset) => {
    if (line.kind !== 'context' && firstDrawnLine + offset < MAX_DRAWN_LINES) {
      indices.push(line.index);
    }
  });
  return indices;
}

/**
 * The same across a whole diff, in the order the lines are drawn — which is
 * the order a Shift+click range runs through, and the order the arrow keys
 * walk.
 */
export function changedLines(diff: FileDiff): number[] {
  const indices: number[] = [];
  let firstDrawnLine = 0;
  for (const hunk of diff.hunks) {
    indices.push(...changedIn(hunk, firstDrawnLine));
    firstDrawnLine += hunk.lines.length;
  }
  return indices;
}

/** How many body lines the whole diff holds. */
export function countDrawnLines(diff: FileDiff): number {
  return diff.hunks.reduce((total, hunk) => total + hunk.lines.length, 0);
}

/** Where a hunk starts, counted in drawn lines from the top of the diff. */
export function firstLineOfHunk(diff: FileDiff, position: number): number {
  let total = 0;
  for (let index = 0; index < position; index += 1) {
    total += diff.hunks[index]?.lines.length ?? 0;
  }
  return total;
}

/**
 * Where each file of a patch starts, counted in drawn lines from its top.
 *
 * A commit's patch is every file it touched, drawn into one scroll region. The
 * cap is spent across them in order, so the files past it cost their header
 * and nothing more.
 */
export function firstLineOfFile(files: readonly FileDiff[]): number[] {
  const starts: number[] = [];
  let total = 0;
  for (const file of files) {
    starts.push(total);
    total += countDrawnLines(file);
  }
  return starts;
}

/** How many body lines a whole patch holds. */
export function countPatchLines(files: readonly FileDiff[]): number {
  return files.reduce((total, file) => total + countDrawnLines(file), 0);
}

/**
 * How wide both line-number columns are drawn, for the whole of what is drawn
 * at once.
 *
 * One width for both columns and for every line, chosen from the largest
 * number any of them will show. The fixed gutter is what keeps the +/- marker
 * from stepping sideways down a file, and at the single width it used to have
 * a six-digit number ran 2px into the column beside it — real, if only in a
 * file of a hundred thousand lines. Sized in steps off the spacing scale
 * rather than in `ch` through an inline style: a width invented in a component
 * is the thing the token rule exists to stop, and three steps cover every file
 * anybody has.
 */
export function gutterWidth(files: readonly FileDiff[]): string {
  let widest = 0;
  for (const file of files) {
    for (const hunk of file.hunks) {
      widest = Math.max(widest, hunk.old_start + hunk.old_lines, hunk.new_start + hunk.new_lines);
    }
  }

  const digits = String(widest).length;
  if (digits <= 4) {
    return 'w-10';
  }
  return digits <= 6 ? 'w-14' : 'w-16';
}

/**
 * One run of a line, and what is true of it.
 *
 * A line is a list of these or nothing at all — nothing being the ordinary
 * case, where the text is drawn as it arrived and no span wraps it.
 */
export interface LineSpan {
  text: string;
  /** Part of what differs from the line this one is drawn against. */
  changed: boolean;
  /** Whitespace with nothing to show for itself: drawn as glyphs. */
  glyphs: boolean;
}

/**
 * What changed INSIDE each changed line of a hunk, where that can be said.
 *
 * A one-character change is otherwise two fully tinted lines and no mark on
 * the character: the reader compares two long strings by eye, which is the
 * work this screen exists to spare them. Comparing the two lines is only
 * meaningful if they are versions of each other, and nothing on the wire says
 * they are — `DiffLine` carries kind, text and numbers, and git's own output
 * has no intra-line information in it at all. So the pairing is inferred, and
 * inferred narrowly: a run of removed lines followed by a run of added ones,
 * of the SAME length, is taken as line-for-line replacement. Three removed and
 * five added is a rewrite whose lines do not correspond, and a confidently
 * drawn highlight over the wrong pair is worse than the flat tint, which at
 * least only claims the line changed.
 *
 * A word-level diff in the daemon is the larger version of this and would
 * change the wire type. This is the frontend half of it: pure, cheap, and
 * wrong about nothing it does not first check.
 *
 * `drawable` is how many of the hunk's lines the cap left room for, and the
 * rest are not read at all: nothing past it is on screen to be marked, and the
 * pane's whole defence against a sixty-thousand-line hunk is that it does no
 * work per line it does not draw. A pair the boundary cuts in half stops being
 * a pair — the two runs are no longer the same length — so the last lines
 * before the drawing stops keep the flat tint, which is the same answer this
 * function gives anywhere else it cannot see both sides.
 */
export function markedSpans(hunk: DiffHunk, drawable: number): Map<number, LineSpan[]> {
  const marked = new Map<number, LineSpan[]>();
  // Clamped rather than trusted. `drawable` is a subtraction at the call site,
  // and a negative one handed to slice counts back from the END of the hunk —
  // which would mark the lines the cap threw away and none of the ones on
  // screen, silently and only on the largest patches in a repository.
  const room = Math.min(Math.max(drawable, 0), hunk.lines.length);
  const lines = room === hunk.lines.length ? hunk.lines : hunk.lines.slice(0, room);

  const record = (line: DiffLine, against: string | undefined) => {
    const spans = spansAgainst(line.text, against);
    if (spans !== undefined) {
      marked.set(line.index, spans);
    }
  };

  let at = 0;
  while (at < lines.length) {
    if (lines[at]?.kind === 'context') {
      at += 1;
      continue;
    }

    const removed = runOf(lines, at, 'removed');
    const added = runOf(lines, at + removed.length, 'added');
    if (removed.length === 0 && added.length === 0) {
      at += 1;
      continue;
    }

    const paired = removed.length === added.length;
    removed.forEach((line, index) => record(line, paired ? added[index]?.text : undefined));
    added.forEach((line, index) => record(line, paired ? removed[index]?.text : undefined));
    at += removed.length + added.length;
  }

  return marked;
}

/** A run of consecutive lines of one kind, starting where it is told to. */
function runOf(lines: readonly DiffLine[], from: number, kind: DiffLine['kind']): DiffLine[] {
  const run: DiffLine[] = [];
  for (let at = from; at < lines.length; at += 1) {
    const line = lines[at];
    if (line === undefined || line.kind !== kind) {
      break;
    }
    run.push(line);
  }
  return run;
}

/**
 * One line split into what stayed, what moved, and the whitespace nobody can
 * see — or nothing, when there is neither.
 *
 * `against` is the line this one replaced, where there is one. Without it only
 * trailing whitespace can be marked, because everything else is a comparison.
 *
 * Which whitespace is drawn is a decision and not an omission. Always-on
 * markers turn the indent of every added line in a Go or Python file into a
 * field of dots, so the ones that matter are read as texture and skipped;
 * behind a toggle they are off on the render that mattered, because nobody
 * turns on a control to see something they do not yet know is there. So they
 * are drawn exactly where they carry information: whitespace that DIFFERS from
 * the line this one replaced, and trailing whitespace, which is invisible by
 * construction and never deliberate. An ordinary indent is not news; an indent
 * that changed is the whole of the news.
 *
 * Only spaces and tabs count as whitespace here. A carriage return is the
 * third candidate and it is deliberately left out: a file with CRLF endings
 * carries one on every line, and marking them would put a glyph at the end of
 * every changed line in every repository written on Windows to say nothing at
 * all.
 */
export function spansAgainst(text: string, against: string | undefined): LineSpan[] | undefined {
  const mine = split(text);
  const theirs = against === undefined ? undefined : split(against);

  const leadChanged = theirs !== undefined && mine.lead !== theirs.lead;
  const trailChanged = theirs !== undefined && mine.trail !== theirs.trail;

  const spans: LineSpan[] = [];
  if (mine.lead !== '') {
    spans.push({ text: mine.lead, changed: leadChanged, glyphs: leadChanged });
  }
  spans.push(...bodySpans(mine.body, theirs?.body));
  if (mine.trail !== '') {
    spans.push({ text: mine.trail, changed: trailChanged, glyphs: true });
  }

  // Nothing to say about the line: it is drawn as the plain text it always
  // was, which is also what keeps the ordinary line to a single text node.
  return spans.some((span) => span.changed || span.glyphs) ? spans : undefined;
}

/**
 * The body of a line against the body of the line it replaced.
 *
 * Tokens rather than characters, and the difference is what makes the result
 * readable: `colour` against `color` shares the letters either side of the
 * `u`, so a character-level answer highlights one letter in the middle of a
 * word and leaves the reader to work out which word it was in. A token-level
 * answer highlights `colour` and `color`, which is the word that changed.
 *
 * Two lines that share no token at all get no highlight. They are not versions
 * of each other in any way this can see — `two` against `TWO` shares nothing —
 * and a highlight over the whole line only restates the tint already under it.
 */
function bodySpans(body: string, against: string | undefined): LineSpan[] {
  if (body === '') {
    return [];
  }

  const flat = [{ text: body, changed: false, glyphs: false }];
  if (against === undefined || against === '') {
    return flat;
  }

  const mine = tokens(body);
  const theirs = tokens(against);

  let head = 0;
  while (head < mine.length && head < theirs.length && mine[head] === theirs[head]) {
    head += 1;
  }

  let tail = 0;
  while (
    tail < mine.length - head &&
    tail < theirs.length - head &&
    mine[mine.length - 1 - tail] === theirs[theirs.length - 1 - tail]
  ) {
    tail += 1;
  }

  if (head === 0 && tail === 0) {
    return flat;
  }

  const middle = mine.slice(head, mine.length - tail).join('');
  if (middle === '') {
    // Everything this line holds is shared: the change is on the other side of
    // the pair, which will mark it. Saying so twice would mark a line for
    // holding what did not change.
    return flat;
  }

  const before = mine.slice(0, head).join('');
  const after = mine.slice(mine.length - tail).join('');

  const spans: LineSpan[] = [];
  if (before !== '') {
    spans.push({ text: before, changed: false, glyphs: false });
  }
  spans.push({ text: middle, changed: true, glyphs: blank(middle) });
  if (after !== '') {
    spans.push({ text: after, changed: false, glyphs: false });
  }
  return spans;
}

/**
 * A line's leading whitespace, its body, and its trailing whitespace.
 *
 * A line that is nothing but whitespace is all trailing: there is no body for
 * it to lead, and trailing whitespace is the run that gets drawn whether or
 * not there is a line to compare it with.
 *
 * Scanned from both ends rather than matched with `/[ \t]+$/`, which is the
 * obvious way to write it and is quadratic on the input this pane is given.
 * That pattern consumes a run of whitespace, fails the anchor, hands one
 * character back, fails again — and starts over from the next position in the
 * run. The daemon caps a line at two thousand runes, so a line that is two
 * thousand spaces and a character costs two million steps, times every line of
 * a file made of them. A scan is linear and says the same thing.
 */
function split(text: string): { lead: string; body: string; trail: string } {
  let end = text.length;
  while (end > 0 && isBlank(text[end - 1])) {
    end -= 1;
  }

  let start = 0;
  while (start < end && isBlank(text[start])) {
    start += 1;
  }

  return { lead: text.slice(0, start), body: text.slice(start, end), trail: text.slice(end) };
}

/**
 * The whitespace this pane draws, one character at a time.
 *
 * Spaces and tabs and nothing else, in one place, because `split` and `blank`
 * asking the question two different ways is how a carriage return ends up
 * dotted on one side of the pane and not the other.
 */
function isBlank(character: string | undefined): boolean {
  return character === ' ' || character === '\t';
}

/**
 * A line cut into words, whitespace and everything else.
 *
 * Unicode letters and digits, not `\w`: an identifier in a language people
 * write in is `préférence` or `店舗名`, and a class that stops at ASCII would
 * cut both into a run of single characters and mark the whole word as moved.
 */
function tokens(text: string): string[] {
  return text.match(/[\p{L}\p{N}_]+|[ \t]+|[^\p{L}\p{N}_ \t]+/gu) ?? [];
}

/** Whitespace and nothing else — a run that would be drawn as blank. */
function blank(text: string): boolean {
  for (const character of text) {
    if (!isBlank(character)) {
      return false;
    }
  }
  return text !== '';
}
