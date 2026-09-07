import { useMemo, useState, type ReactNode } from 'react';

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
  // Where the last click landed, so Shift+click has a range to close.
  const [anchor, setAnchor] = useState<number>();

  // The changed lines in the order they are drawn, which is the order a
  // Shift+click range runs through. Derived rather than stored: the diff is
  // refetched on every change, and a stored copy would describe the file as it
  // was before the last stage.
  const changed = useMemo(() => changedLines(diff), [diff]);

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

  const drawn = countDrawnLines(diff);

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

      <div className="min-h-0 flex-1 overflow-auto font-mono text-xs">
        {pointer !== undefined && <PointerNote note={pointer} />}

        {diff.hunks.map((hunk, position) => (
          <HunkView
            // Hunks have no identity of their own; their position in this diff
            // is the only stable thing about them, and the whole list is
            // replaced whenever the diff changes.
            key={position}
            hunk={hunk}
            firstDrawnLine={firstLineOfHunk(diff, position)}
            actions={{ side, busy, selected, onToggle: toggle, onApply: apply }}
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

  return (
    <div className="flex h-9 shrink-0 items-center gap-3 border-b border-line px-3">
      <span className="flex min-w-0 items-baseline gap-2">
        <span className="truncate font-mono text-xs text-ink" title={diff.path}>
          {diff.path}
        </span>
        <span className="shrink-0 text-2xs text-ink-subtle">{SIDE_LABELS[side]}</span>
      </span>

      {count === 0 ? (
        <p className="ml-auto hidden shrink-0 text-2xs text-ink-subtle lg:block">
          Click a changed line to choose it, Shift+click for a range
        </p>
      ) : (
        <span className="ml-auto flex shrink-0 items-center gap-2">
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

function HunkView({ hunk, firstDrawnLine, actions }: HunkViewProps) {
  if (firstDrawnLine >= MAX_DRAWN_LINES) {
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

        {actions !== undefined && <HunkButtons hunk={hunk} actions={actions} />}
      </header>

      <div>
        {hunk.lines.map((line, offset) =>
          firstDrawnLine + offset >= MAX_DRAWN_LINES ? null : (
            <LineView
              key={line.index}
              line={line}
              selected={actions?.selected.has(line.index) ?? false}
              onToggle={actions?.onToggle}
            />
          ),
        )}
      </div>
    </section>
  );
}

function HunkButtons({ hunk, actions }: { hunk: DiffHunk; actions: HunkActions }) {
  const { primary, label, discardable } = actionsFor(actions.side);
  const changedHere = changedIn(hunk);

  return (
    <div
      className={cx(
        'ml-auto flex shrink-0 items-center gap-0.5 opacity-0',
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

const LINE_CLASSES: Record<DiffLine['kind'], string> = {
  added: 'bg-added/10',
  removed: 'bg-deleted/10',
  context: '',
};

const MARKERS: Record<DiffLine['kind'], string> = {
  added: '+',
  removed: '-',
  context: ' ',
};

/**
 * One line of a diff.
 *
 * onToggle is what makes a line choosable, and its absence is what makes a
 * diff read-only — a commit's diff has nothing to stage. The line renders the
 * same either way, which is the point of it being one component.
 */
/**
 * What a screen reader is told this line is.
 *
 * Built from the text the daemon sent, which is already bounded — the label of
 * a minified line used to be the whole line. The count is spoken when there is
 * one, because a name that simply stops is a name that lies about the line.
 */
function accessibleName(line: DiffLine): string {
  const kind = line.kind === 'added' ? 'Added' : 'Removed';
  const rest =
    line.truncated !== undefined && line.truncated > 0
      ? `, and ${line.truncated.toLocaleString()} more characters not shown`
      : '';
  return `${kind} line: ${line.text}${rest}`;
}

function LineView({
  line,
  selected,
  onToggle,
}: {
  line: DiffLine;
  selected: boolean;
  onToggle?: (line: DiffLine, extend: boolean) => void;
}) {
  const changeable = line.kind !== 'context' && onToggle !== undefined;

  const content = (
    <>
      {/* Both line numbers, always, in a fixed-width column. An added line has
          no old number and a removed one has no new number; leaving the space
          empty rather than collapsing it is what keeps the two columns from
          jittering down the file. */}
      <span className="w-10 shrink-0 pr-2 text-right text-2xs text-ink-subtle tabular select-none">
        {line.old_line === 0 ? '' : line.old_line}
      </span>
      <span className="w-10 shrink-0 pr-2 text-right text-2xs text-ink-subtle tabular select-none">
        {line.new_line === 0 ? '' : line.new_line}
      </span>
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

      {/* pre-wrap, not pre: a long line has to wrap rather than push a
          horizontal scrollbar under the whole file, and the leading spaces of
          indented code have to survive. */}
      <span className="min-w-0 flex-1 pr-3 break-all whitespace-pre-wrap text-ink">
        {line.text}
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
      </span>
    </>
  );

  const shared = cx(
    'flex w-full items-start border-l-2 text-left',
    LINE_CLASSES[line.kind],
    selected ? 'border-accent bg-accent-soft/60' : 'border-transparent',
  );

  if (!changeable) {
    return <div className={shared}>{content}</div>;
  }

  return (
    <button
      type="button"
      aria-pressed={selected}
      aria-label={accessibleName(line)}
      onClick={(event) => onToggle(line, event.shiftKey)}
      className={cx(shared, 'cursor-pointer outline-none hover:bg-hover focus-visible:focus-ring')}
    >
      {content}
    </button>
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
 * What the cap left out, or nothing when it was not reached.
 *
 * The comparison lives with the number rather than at each call site, so a
 * view cannot draw a truncated patch and forget to say that it did.
 */
function Truncated({ drawn, children }: { drawn: number; children?: ReactNode }) {
  if (drawn <= MAX_DRAWN_LINES) {
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

  return (
    <>
      {files.map((file, position) => (
        <ReadOnlyDiff
          key={file.path}
          diff={file}
          drawnBefore={starts[position] ?? 0}
          {...(onFileHistory === undefined ? {} : { onHistory: () => onFileHistory(file.path) })}
          {...(onBlame === undefined || file.binary || file.removed
            ? {}
            : { onBlame: () => onBlame(file.path) })}
        />
      ))}
      <Truncated drawn={countPatchLines(files)} />
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
  onHistory,
  onBlame,
}: {
  diff: FileDiff;
  drawnBefore: number;
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
            />
          ))}
        </div>
      )}
    </section>
  );
}

/** Every added and removed line of a hunk: what a selection can name. */
export function changedIn(hunk: DiffHunk): number[] {
  return hunk.lines.filter((line) => line.kind !== 'context').map((line) => line.index);
}

/**
 * The same across a whole diff, in the order the lines are drawn — which is
 * the order a Shift+click range runs through.
 */
export function changedLines(diff: FileDiff): number[] {
  return diff.hunks.flatMap(changedIn);
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
