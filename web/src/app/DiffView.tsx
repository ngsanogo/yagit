import { useQuery } from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from 'react';

import { api } from '../api/client';

import type { DiffHunk, DiffLine, DiffSide, FileDiff } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import {
  CloseIcon,
  FileIcon,
  MinusIcon,
  PlusIcon,
  SearchIcon,
  TrashIcon,
} from '../components/Icons';
import { Kbd } from '../components/Kbd';
import { cx } from '../lib/cx';
import { errorDescription } from '../lib/errorDisplay';
import { commandModifier } from '../lib/platform';
import {
  findInDiff,
  findInPatch,
  findSpans,
  matchesByLine,
  stepMatch,
  type DiffFindMatch,
} from './diffFind';
import { LineView } from './DiffLineView';
import {
  changedIn,
  changedLines,
  countDrawnLines,
  countPatchLines,
  firstLineOfFile,
  firstLineOfHunk,
  gutterWidth,
} from './diffLines';
import { markedSpans, type LineSpan } from './diffSpans';
import { Capped, MAX_DRAWN_LINES, Truncated } from './drawnLines';
import {
  blobURL,
  imageSidesForRevision,
  imageSidesForWorkingDiff,
  isImageDiff,
  type ImageSide,
} from './imageDiff';
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

export type DiffAction = 'stage' | 'unstage' | 'discard';

interface DiffViewProps {
  diff: FileDiff;
  side: DiffSide;
  /** Open repository: needed to load image bytes for a binary image diff. */
  repositoryId: string;
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
  /**
   * How many unchanged lines surround each hunk, and how to widen or narrow
   * it. Absent on a read-only surface (a commit's patch keeps the default).
   */
  context?: number;
  onMoreContext?: () => void;
  onLessContext?: () => void;
}

/** How each side reads in the header. git's own words for git's own states. */
const SIDE_LABELS: Record<DiffSide, string> = {
  staged: 'staged',
  unstaged: 'not staged',
  untracked: 'not tracked',
};

/** The glyph on the action that moves lines the way a side moves them. */
function actionGlyph(action: DiffAction) {
  switch (action) {
    case 'stage':
      return <PlusIcon />;
    case 'unstage':
      return <MinusIcon />;
    case 'discard':
      return <TrashIcon />;
  }
}

export function DiffView({
  diff,
  side,
  repositoryId,
  onApply,
  busy,
  context,
  onMoreContext,
  onLessContext,
}: DiffViewProps) {
  const [selected, setSelected] = useState<ReadonlySet<number>>(new Set());
  // Where the last click landed, so Shift+click has a range to close. It is
  // also where the pane's single tab stop sits: the line the keyboard last
  // touched is the line it should come back to, and a roving tab stop needs
  // exactly one line to be the one — see the arrow keys below.
  const [anchor, setAnchor] = useState<number>();
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState('');
  const [findAt, setFindAt] = useState(0);
  const findInput = useRef<HTMLInputElement>(null);
  const scroller = useRef<HTMLDivElement>(null);

  // The changed lines in the order they are drawn, which is the order a
  // Shift+click range runs through. Derived rather than stored: the diff is
  // refetched on every change, and a stored copy would describe the file as it
  // was before the last stage.
  const changed = useMemo(() => changedLines(diff), [diff]);
  const findMatches = useMemo(() => findInDiff(diff, findQuery), [diff, findQuery]);
  const activeFind =
    findMatches.length === 0 ? undefined : findMatches[findAt % findMatches.length];

  // The same lines by index, because the arrow keys move between line NUMBERS
  // and toggling wants the line itself. Built once per diff rather than
  // searched per keystroke: a rewritten lockfile is two thousand rows, and a
  // linear scan of them on every press is a key that feels stuck.
  //
  // It stops where the drawing stops, which is the rule the rest of this file
  // keeps: a regenerated lockfile arrives as one hunk of sixty thousand lines,
  // and the pane's whole defence against it is that it does no work per line
  // it does not draw. Nothing past the cap can be reached anyway — `changed`
  // is the only source of the indices looked up here, and it is bounded the
  // same way — so the entries beyond it were a map of lines nobody can select.
  const lineAt = useMemo(() => {
    const byIndex = new Map<number, DiffLine>();
    let drawn = 0;
    for (const hunk of diff.hunks) {
      for (const line of hunk.lines) {
        if (drawn >= MAX_DRAWN_LINES) {
          return byIndex;
        }
        drawn += 1;
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
    setFindAt(0);
  }

  // Clamp during render rather than in an effect: when the query shrinks the
  // match list, an effect that wrote findAt would cascade a second paint for
  // a number we already know.
  const findIndex = findMatches.length === 0 ? 0 : findAt % findMatches.length;
  if (findIndex !== findAt && findMatches.length > 0) {
    setFindAt(findIndex);
  }

  useEffect(() => {
    if (activeFind === undefined || scroller.current === null) {
      return;
    }
    const target = scroller.current.querySelector(`[data-diff-line="${activeFind.line}"]`);
    if (target instanceof HTMLElement) {
      target.scrollIntoView({ block: 'nearest' });
    }
  }, [activeFind]);

  if (diff.binary) {
    if (isImageDiff(diff)) {
      return (
        <ImageDiffPreview
          sides={imageSidesForWorkingDiff(diff, side)}
          repositoryId={repositoryId}
          note="git shows no lines for this file. Stage or discard it whole from the list."
        />
      );
    }
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
  const modifier = commandModifier();

  function openFind() {
    setFindOpen(true);
    queueMicrotask(() => findInput.current?.select());
  }

  function onFindShortcut(event: KeyboardEvent<HTMLDivElement>) {
    if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 'f') {
      return;
    }
    event.preventDefault();
    openFind();
  }

  return (
    <div className="flex h-full min-h-0 flex-col" onKeyDown={onFindShortcut}>
      <DiffHeader
        diff={diff}
        side={side}
        count={selected.size}
        busy={busy}
        context={context}
        onMoreContext={onMoreContext}
        onLessContext={onLessContext}
        findOpen={findOpen}
        onFind={openFind}
        findHint={modifier.label}
        onApply={(action) => apply(action, [...selected])}
        onClear={() => {
          setSelected(new Set());
          setAnchor(undefined);
        }}
      />

      {findOpen && (
        <DiffFindBar
          query={findQuery}
          count={findMatches.length}
          at={findAt}
          inputRef={findInput}
          onQuery={(next) => {
            setFindQuery(next);
            setFindAt(0);
          }}
          onStep={(delta) => setFindAt((at) => stepMatch(findMatches.length, at, delta))}
          onClose={() => {
            setFindOpen(false);
            setFindQuery('');
            setFindAt(0);
          }}
        />
      )}

      {/* Absolute fill rather than a bare overflow-auto flex child. A tall
          file's hunks inside `overflow-auto` still contributed their layout
          height to the document — the page grew a scrollbar through empty
          canvas while this pane stayed a strip. Pinning the scroller to the
          slot keeps the content's height inside the Diff panel. The same
          repair is on a commit's patch in CommitDetails. */}
      <div className="relative min-h-0 flex-1">
        <div
          ref={scroller}
          className="absolute inset-0 overflow-auto font-mono text-xs"
          onKeyDown={moveByKey}
        >
          <Capped lines={drawn}>Hunk buttons act on the lines drawn, not on the rest.</Capped>

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
              findMatches={findMatches}
              activeFind={activeFind}
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

          <Truncated lines={drawn} subject="patch">
            Staging the whole file still works — it is <code className="text-ink">git add</code>,
            and needs no patch.
          </Truncated>
        </div>
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
  context,
  onMoreContext,
  onLessContext,
  findOpen,
  findHint,
  onFind,
  onApply,
  onClear,
}: {
  diff: FileDiff;
  side: DiffSide;
  count: number;
  busy: boolean;
  context?: number;
  onMoreContext?: () => void;
  onLessContext?: () => void;
  findOpen: boolean;
  findHint: string;
  onFind: () => void;
  onApply: (action: DiffAction) => void;
  onClear: () => void;
}) {
  const { primary, label, discardable } = actionsFor(side);
  const cameFrom = diff.old_path !== undefined && diff.old_path !== '' ? diff.old_path : undefined;
  // Drawn wherever a context is given, with the button at an end disabled:
  // the pair keeps its place in the header as the width moves.
  const canWiden = context !== undefined;

  return (
    <div className="@container flex h-10 shrink-0 items-center gap-3 border-b border-line bg-sunken/40 px-3">
      <span className="flex min-w-0 items-center gap-2">
        <FileIcon className="shrink-0 text-ink-subtle" />
        <span className="truncate font-mono text-xs font-medium text-ink" title={diff.path}>
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
        <Badge className="shrink-0">{SIDE_LABELS[side]}</Badge>
      </span>

      {canWiden && (
        <span className="flex shrink-0 items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            disabled={busy || onLessContext === undefined}
            onClick={onLessContext}
          >
            Less context
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={busy || onMoreContext === undefined}
            onClick={onMoreContext}
          >
            More context
          </Button>
          {context !== undefined && (
            <span
              className="hidden text-2xs text-ink-subtle tabular @xl:inline"
              title="Unchanged lines around each change"
            >
              ±{context}
            </span>
          )}
        </span>
      )}

      <span className="ml-auto flex shrink-0 items-center gap-2">
        {!findOpen && (
          <Button
            size="sm"
            variant="ghost"
            onClick={onFind}
            aria-keyshortcuts={`${findHint}+F`}
            title={`Find in this diff (${findHint}+F)`}
          >
            <SearchIcon />
            Find
          </Button>
        )}
        {count === 0 ? (
          <p className="hidden shrink-0 items-center gap-1.5 text-2xs text-ink-subtle @2xl:flex">
            Click a line to choose it, <Kbd>Shift</Kbd>
            <span>+ click for a range, or {label.toLowerCase()} a hunk from its header</span>
          </p>
        ) : (
          <span className="flex shrink-0 items-center gap-2">
            {/* The sentence above says Shift+click, and the moment a first line
              is chosen it is replaced by these buttons — which is exactly when
              the reader first has an anchor to extend from. The short form
              survives into that state. */}
            <span className="hidden text-2xs text-ink-subtle @3xl:inline">
              Shift+click to extend
            </span>
            <Badge tone="accent">{count} selected</Badge>
            {/* Secondary, not primary. The screen already has its one primary
              button — Commit — and that is the action it is ultimately
              waiting for; staging is a step on the way. The accent badge
              beside this is what draws the eye to the selection. */}
            <Button
              size="sm"
              variant="secondary"
              leading={actionGlyph(primary)}
              disabled={busy}
              onClick={() => onApply(primary)}
            >
              {label} {count === 1 ? 'line' : 'lines'}
            </Button>
            {discardable && (
              <Button
                size="sm"
                variant="danger"
                leading={<TrashIcon />}
                disabled={busy}
                onClick={() => onApply('discard')}
              >
                Discard
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={onClear}>
              Clear
            </Button>
          </span>
        )}
      </span>
    </div>
  );
}

/**
 * Find bar for the open patch.
 *
 * Lives under the header rather than inside it so the path and the Stage
 * buttons keep a fixed height: a find field that grew the header would shove
 * every line down by its own height on open.
 */
function DiffFindBar({
  query,
  count,
  at,
  inputRef,
  onQuery,
  onStep,
  onClose,
}: {
  query: string;
  count: number;
  at: number;
  inputRef: RefObject<HTMLInputElement | null>;
  onQuery: (query: string) => void;
  onStep: (delta: 1 | -1) => void;
  onClose: () => void;
}) {
  const label =
    query.trim() === '' ? 'Type to find' : count === 0 ? 'No matches' : `${at + 1} of ${count}`;

  return (
    <div className="flex h-9 shrink-0 items-center gap-2 border-b border-line bg-sunken px-3">
      <SearchIcon className="shrink-0 text-ink-subtle" />
      <input
        ref={inputRef}
        type="search"
        value={query}
        onChange={(event) => onQuery(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault();
            onClose();
            return;
          }
          if (event.key === 'Enter') {
            event.preventDefault();
            onStep(event.shiftKey ? -1 : 1);
          }
        }}
        placeholder="Find in this diff"
        aria-label="Find in this diff"
        className={cx(
          'h-7 min-w-0 flex-1 rounded-md border border-line-strong bg-canvas px-2 text-xs text-ink',
          'outline-none placeholder:text-ink-subtle focus-visible:focus-ring',
        )}
      />
      <span className="shrink-0 text-2xs text-ink-subtle tabular" aria-live="polite">
        {label}
      </span>
      <Button size="sm" variant="ghost" disabled={count === 0} onClick={() => onStep(-1)}>
        Previous
      </Button>
      <Button size="sm" variant="ghost" disabled={count === 0} onClick={() => onStep(1)}>
        Next
      </Button>
      <Button size="sm" variant="ghost" onClick={onClose} aria-label="Close find">
        <CloseIcon />
      </Button>
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
  /** Find hits for the open query; empty when find is closed. */
  findMatches?: readonly DiffFindMatch[];
  activeFind?: DiffFindMatch;
}

function HunkView({
  hunk,
  firstDrawnLine,
  gutter,
  actions,
  findMatches = [],
  activeFind,
}: HunkViewProps) {
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
  // Grouped once per hunk, then looked up per line: filtering every match for
  // every line is lines × matches, and a common letter makes that thousands.
  const findByLine = useMemo(() => matchesByLine(findMatches), [findMatches]);

  if (drawable <= 0) {
    return null;
  }

  return (
    <section className="group/hunk">
      <header className="sticky top-0 flex items-center gap-2 border-y border-line bg-sunken px-3 py-1">
        <span className="text-2xs text-info">
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
        {hunk.lines.map((line, offset) => {
          if (offset >= drawable) {
            return null;
          }
          const onLine = findByLine.get(line.index) ?? [];
          const activeOnLine =
            activeFind !== undefined && activeFind.line === line.index ? activeFind : undefined;
          // Find paint replaces the intra-line edit wash for that line: layering
          // both made the current match unreadable on an added/removed tint.
          const find = findSpans(
            line.text,
            onLine.map((match) => ({ start: match.start, length: match.length })),
            activeOnLine === undefined
              ? undefined
              : { start: activeOnLine.start, length: activeOnLine.length },
          );
          return (
            <LineView
              key={line.index}
              line={line}
              gutter={gutter}
              spans={find === undefined ? marked.get(line.index) : undefined}
              findSpans={find}
              selected={actions?.selected.has(line.index) ?? false}
              tabStop={actions?.tabStop === line.index}
              onToggle={actions?.onToggle}
            />
          );
        })}
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
          leading={<TrashIcon />}
          disabled={actions.busy || changedHere.length === 0}
          onClick={() => actions.onApply('discard', changedHere)}
        >
          Discard hunk
        </Button>
      )}
      <Button
        size="sm"
        variant="ghost"
        leading={actionGlyph(primary)}
        disabled={actions.busy || changedHere.length === 0}
        onClick={() => actions.onApply(primary, changedHere)}
      >
        {label} hunk
      </Button>
    </div>
  );
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
 * Before/after (or one side) of an image FileDiff.
 *
 * Drawn as <img> against the blob route, never as inline data: the session
 * cookie already authorises same-origin GETs, and a five-mebibyte PNG does
 * not belong in the React tree as a base64 string.
 */
function ImageDiffPreview({
  sides,
  repositoryId,
  note,
}: {
  sides: readonly ImageSide[];
  repositoryId: string;
  note: string;
}) {
  if (sides.length === 0) {
    return <Notice title="Binary file" description={note} />;
  }

  return (
    <div className="flex flex-col gap-3 px-3 py-3">
      <p className="text-2xs text-ink-subtle">{note}</p>
      <div
        className={cx(
          'grid gap-3',
          sides.length > 1 ? 'grid-cols-1 sm:grid-cols-2' : 'grid-cols-1',
        )}
      >
        {sides.map((side) => (
          <figure
            key={`${side.at}:${side.path}`}
            className="flex min-w-0 flex-col gap-1.5 rounded-md border border-line bg-sunken p-2"
          >
            <figcaption className="truncate font-mono text-2xs text-ink-subtle" title={side.path}>
              {side.label}
              <span className="text-ink-muted"> · {side.path}</span>
            </figcaption>
            <PreviewImage url={blobURL(repositoryId, side)} alt={`${side.label}: ${side.path}`} />
          </figure>
        ))}
      </div>
    </div>
  );
}

/**
 * One side of an image preview, or the reason it could not be drawn.
 *
 * An <img> that fails says nothing but "failed", so the same URL is asked
 * again once, through the API client, for the daemon's sentence — a file
 * over the preview's cap, a path that is gone — and that is what replaces
 * the picture.
 */
function PreviewImage({ url, alt }: { url: string; alt: string }) {
  const [failed, setFailed] = useState(false);
  const why = useQuery({
    queryKey: ['blob-failure', url],
    queryFn: () => api.blobFailure(url),
    enabled: failed,
    retry: false,
  });

  if (failed) {
    return (
      <p className="py-6 text-center text-xs text-ink-muted">
        {why.isError ? errorDescription(why.error) : 'Reading why this side did not load…'}
      </p>
    );
  }
  return (
    <img
      src={url}
      alt={alt}
      onError={() => setFailed(true)}
      className="max-h-80 w-full object-contain"
    />
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
  repositoryId,
  revision,
  onFileHistory,
  onBlame,
}: {
  files: readonly FileDiff[];
  repositoryId: string;
  /** Object the new side of each file comes from — a commit or stash SHA. */
  revision: string;
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
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState('');
  const [findAt, setFindAt] = useState(0);
  const findInput = useRef<HTMLInputElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const findMatches = useMemo(() => findInPatch(files, findQuery), [files, findQuery]);
  const findIndex = findMatches.length === 0 ? 0 : findAt % findMatches.length;
  if (findIndex !== findAt && findMatches.length > 0) {
    setFindAt(findIndex);
  }
  const activeFind = findMatches.length === 0 ? undefined : findMatches[findIndex];
  const modifier = commandModifier();

  useEffect(() => {
    if (activeFind === undefined || scroller.current === null) {
      return;
    }
    const section = scroller.current.querySelector(
      `[data-diff-path="${CSS.escape(activeFind.path)}"]`,
    );
    const target =
      section?.querySelector(`[data-diff-line="${activeFind.line}"]`) ??
      scroller.current.querySelector(`[data-diff-line="${activeFind.line}"]`);
    if (target instanceof HTMLElement) {
      target.scrollIntoView({ block: 'nearest' });
    }
  }, [activeFind]);

  function openFind() {
    setFindOpen(true);
    queueMicrotask(() => findInput.current?.select());
  }

  return (
    <div
      className="flex min-h-0 flex-col"
      onKeyDown={(event) => {
        if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 'f') {
          return;
        }
        event.preventDefault();
        openFind();
      }}
    >
      {/* One strip, in either state: the button that opens the bar, or the
          bar itself in its place — never an emptied strip above the bar. */}
      {findOpen ? (
        <DiffFindBar
          query={findQuery}
          count={findMatches.length}
          at={findAt}
          inputRef={findInput}
          onQuery={(next) => {
            setFindQuery(next);
            setFindAt(0);
          }}
          onStep={(delta) => setFindAt((at) => stepMatch(findMatches.length, at, delta))}
          onClose={() => {
            setFindOpen(false);
            setFindQuery('');
            setFindAt(0);
          }}
        />
      ) : (
        <div className="flex h-9 shrink-0 items-center justify-end gap-2 border-b border-line bg-sunken/40 px-3">
          <Button
            size="sm"
            variant="ghost"
            onClick={openFind}
            title={`Find in this patch (${modifier.label}+F)`}
          >
            <SearchIcon />
            Find
          </Button>
        </div>
      )}

      <div ref={scroller} className="min-h-0 flex-1 overflow-auto">
        <Capped lines={drawn} />

        {files.map((file, position) => (
          <ReadOnlyDiff
            key={file.path}
            diff={file}
            repositoryId={repositoryId}
            revision={revision}
            drawnBefore={starts[position] ?? 0}
            gutter={gutter}
            findMatches={findMatches.filter((match) => match.path === file.path)}
            activeFind={
              activeFind !== undefined && activeFind.path === file.path ? activeFind : undefined
            }
            {...(onFileHistory === undefined ? {} : { onHistory: () => onFileHistory(file.path) })}
            {...(onBlame === undefined || file.binary || file.removed
              ? {}
              : { onBlame: () => onBlame(file.path) })}
          />
        ))}

        <Truncated lines={drawn} subject="patch" />
      </div>
    </div>
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
  repositoryId,
  revision,
  drawnBefore,
  gutter,
  findMatches = [],
  activeFind,
  onHistory,
  onBlame,
}: {
  diff: FileDiff;
  repositoryId: string;
  revision: string;
  drawnBefore: number;
  gutter: string;
  findMatches?: readonly DiffFindMatch[];
  activeFind?: DiffFindMatch;
  onHistory?: () => void;
  onBlame?: () => void;
}) {
  const pointer = pointerNote(diff);

  return (
    <section className="border-b border-line last:border-b-0" data-diff-path={diff.path}>
      <header className="flex items-center gap-2 border-b border-line bg-sunken px-3 py-1.5">
        <FileIcon className="shrink-0 text-ink-subtle" />
        <span
          className="min-w-0 flex-1 truncate font-mono text-xs font-medium text-ink"
          title={diff.path}
        >
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
        isImageDiff(diff) ? (
          <ImageDiffPreview
            sides={imageSidesForRevision(diff, revision)}
            repositoryId={repositoryId}
            note="git shows no content for this file line by line."
          />
        ) : (
          <p className="px-3 py-2 text-2xs text-ink-subtle">
            git shows no content for this file, so there is nothing to read line by line.
          </p>
        )
      ) : (
        <div className="font-mono text-xs">
          {pointer !== undefined && <PointerNote note={pointer} />}

          {diff.hunks.map((hunk, position) => (
            <HunkView
              key={position}
              hunk={hunk}
              firstDrawnLine={drawnBefore + firstLineOfHunk(diff, position)}
              gutter={gutter}
              findMatches={findMatches}
              activeFind={activeFind}
            />
          ))}
        </div>
      )}
    </section>
  );
}
