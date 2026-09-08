import { useVirtualizer } from '@tanstack/react-virtual';
import {
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type KeyboardEvent,
  type Ref,
  type RefObject,
} from 'react';

import type { CommitRow, HistoryScope } from '../api/types';
import { Avatar, AVATAR_SIZE } from '../components/Avatar';
import { RefBadge } from '../components/Badge';
import { Button } from '../components/Button';
import { cx } from '../lib/cx';
import { gitFailureLine } from '../lib/errorDisplay';
import { formatExactTime, formatRelativeTime, shortenSha } from '../lib/format';
import { CommitGraph } from './CommitGraph';
import { columnsWithin, graphFits, graphGutter, graphWidth, ROW_HEIGHT } from './geometry';
import { useHistoryWindow, type PageFailure } from './useHistory';

/**
 * The history: the graph, and one row per commit beside it.
 *
 * Only the visible rows exist in the DOM, and only their pages are fetched. A
 * repository with a hundred thousand commits is not unusual — git's own has
 * well over that — and rendering them all is not slow, it is a tab that never
 * opens. The virtualizer is headless: it returns offsets and indices and
 * renders nothing itself, so every element below is yagit's own and the
 * design system is untouched.
 *
 * The graph is drawn after the rows on purpose. A selected row paints its
 * background across the whole width, the graph column included, and a picture
 * drawn first would disappear underneath it.
 *
 * The list is ONE stop in the page's tab order and the arrows move within it,
 * the shape Tabs and SegmentedControl already use. A row per Tab press is what
 * it was: four hundred presses to walk past the screen this application is
 * for, and about fifty rows reachable before the browser's own wrap took focus
 * back to the top. See tabbableRow and rowForKey below.
 */
interface CommitListProps {
  repositoryId: string;
  /** Commits in the whole history: the length the scrollbar measures. */
  total: number;
  /** Columns the graph needs, over the whole history. */
  columns: number;
  /** Rows per page, as the daemon reported it. */
  pageSize: number;
  /** Which refs this history was walked from; the pages are asked for by it. */
  scope: HistoryScope;
  /** The chosen references, under `scope=refs` and empty otherwise. */
  refs?: readonly string[];
  selected?: string;
  onSelect: (sha: string) => void;
  ref?: Ref<CommitListHandle>;
}

/**
 * Taking the list somewhere, which is an event and not a state.
 *
 * Imperative on purpose: a prop holding the row to show could not tell a
 * second click on the same reference from the first, and coming back to a tip
 * you have scrolled away from is exactly what that second click is for.
 */
export interface CommitListHandle {
  scrollToRow: (row: number) => void;
}

export function CommitList({
  repositoryId,
  total,
  columns,
  pageSize,
  scope,
  refs = [],
  selected,
  onSelect,
  ref,
}: CommitListProps) {
  const scrollElement = useRef<HTMLDivElement>(null);
  const listWidth = useListWidth(scrollElement);

  // The row the list hands focus to when Tab reaches it, and the row an arrow
  // key counts from. One piece of state for both, because both questions have
  // the same answer: the last row the reader put focus on.
  const [wantedRow, setWantedRow] = useState<number>();
  /*
   * The row a key press is on its way to, held until the virtualiser has
   * rendered it. See the effect below.
   *
   * A ref and not state, though it is written on a key press and read after a
   * render. Nothing drawn depends on it — the tab stop is chosen from
   * wantedRow — so holding it in state would buy a second render for every
   * press of ArrowDown on the screen this application is read on, and would
   * put a setState in the body of an effect, which is the cascade the
   * compiler's lint rules exist to catch.
   */
  const chasing = useRef<number | undefined>(undefined);

  // The React Compiler's lint rule objects that useVirtualizer returns
  // functions it cannot memoize safely. That is true, it is inherent to the
  // library ADR 0005 chose on its merits, and the compiler is not enabled in
  // this project — vite.config.ts runs plugin-react without it. Silenced here
  // rather than left to warn on every run, because a warning that always
  // appears is one people stop reading. If the compiler is ever turned on,
  // this is the line to come back to.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: total,
    getScrollElement: () => scrollElement.current,
    estimateSize: () => ROW_HEIGHT,
    // Rows rendered above and below the viewport. Enough that a flick of the
    // wheel lands on painted rows rather than on blanks, few enough that the
    // DOM stays small.
    overscan: 12,
  });

  const rows = virtualizer.getVirtualItems();
  const firstRow = rows[0]?.index ?? 0;
  const lastRow = rows[rows.length - 1]?.index ?? 0;

  const history = useHistoryWindow(repositoryId, pageSize, firstRow, lastRow, scope, refs);

  useImperativeHandle(
    ref,
    () => ({
      // Centred rather than brought to the top edge: a commit at the very top
      // of the viewport has no history above it on screen, and the rows above
      // a commit are half of what you came to look at.
      scrollToRow: (row: number) => virtualizer.scrollToIndex(row, { align: 'center' }),
    }),
    [virtualizer],
  );

  /*
   * Focus lands on a row after the virtualiser has rendered it, which is not
   * the tick the key was pressed on: scrollToIndex moves the scroll offset,
   * and a row two hundred places down the history does not exist in the DOM
   * until the window has been recomputed around it — nor, if its page is still
   * on its way, until the page arrives. So the row is remembered and claimed
   * on whichever render brings it in.
   *
   * After every render rather than off a dependency list, the shape Tabs uses
   * for the same reason: the row can arrive because the window moved, because
   * a page landed, or because a retry finally answered, and a list of those
   * causes is a list that will one day be missing the fourth. The bail-out at
   * the top makes the repeat free, and it is the case on every render but the
   * handful after a key press.
   */
  useEffect(() => {
    const chasedRow = chasing.current;
    if (chasedRow === undefined) {
      return;
    }

    const list = scrollElement.current;
    const active = document.activeElement;
    // Give up if the reader moved on while a page was in flight: focus taken
    // out of the list, or put on another row of it. Pulling it back to a row
    // they have stopped looking at is worse than not moving it at all. Focus
    // on <body> is not "moved on" — it is where the browser leaves it when the
    // row that had it was unmounted by the very scroll this is chasing.
    const abandoned =
      wantedRow !== chasedRow ||
      (list !== null && active !== document.body && !list.contains(active));
    if (abandoned) {
      chasing.current = undefined;
      return;
    }

    // Not there yet: the scroll has not landed, or the row landed as a
    // skeleton, which is not a button and carries no data-row.
    const row = list?.querySelector<HTMLElement>(`[data-row="${chasedRow}"]`);
    if (row === null || row === undefined) {
      return;
    }

    // preventScroll: scrollToIndex has already put the row where the key asked
    // for it, and the browser's own scroll-into-view would fight that.
    row.focus({ preventScroll: true });
    chasing.current = undefined;
  });

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    // Which row the key is on is read from the DOM rather than from state,
    // because not everything in this container is a row: the Retry button on a
    // failed page lives here too, and only the row buttons carry data-row. A
    // key pressed on anything else is not ours to take.
    const from = rowOf(event.target);
    if (from === undefined) {
      return;
    }

    const visible = virtualizer.range;
    // A page is a viewport less one row, so the row you were on stays on
    // screen and you can see what you jumped over.
    const page = visible === null ? 1 : Math.max(1, visible.endIndex - visible.startIndex);
    const target = rowForKey(event.key, from, total, page);
    if (target === undefined) {
      return;
    }

    event.preventDefault();
    // Focus moves, and nothing is selected. The commit pane is keyed by sha,
    // so selecting per keystroke would remount it and ask the daemon for a
    // commit on every press of ArrowDown. Enter and Space select, which a
    // button already does for nothing.
    virtualizer.scrollToIndex(target, { align: 'auto' });
    chasing.current = target;
    setWantedRow(target);
  };

  // A history with hundreds of branches open at once is wider than any column
  // beside a list of subjects can be. Then the rows take their ordinary
  // padding back and the reason is said out loud, rather than a narrower
  // picture being drawn that leaves branches out of it.
  const drawn = graphFits(columns);
  // And a graph that fits the bound can still not fit the window. The gutter
  // is bounded by the list rather than by the column count alone; where that
  // bites, the picture is clipped at the gutter's edge and the rows say so.
  const gutter = drawn ? graphGutter(columns, listWidth) : undefined;
  // How many columns the reader is actually being shown, when that is fewer
  // than the history has, and undefined when the whole picture fits.
  const shown =
    gutter !== undefined && gutter < graphWidth(columns) ? columnsWithin(gutter) : undefined;

  // `range` is the rows on screen, which getVirtualItems above has just worked
  // out. The items themselves are that range plus the overscan, and a row
  // twelve rows above the viewport is no place for the only way out of a
  // failed page.
  const reporting = reportingRows(virtualizer.range, history.failureAt);

  const selectedRow = rows.find((row) => history.rowAt(row.index)?.sha === selected)?.index;
  const loaded = (row: number) => history.rowAt(row) !== undefined;
  const tabbable = tabbableRow(wantedRow ?? selectedRow, firstRow, lastRow, loaded);

  return (
    <div className="flex h-full flex-col">
      {!drawn && <TooWide columns={columns} scope={scope} />}
      {shown !== undefined && <GraphClipped shown={shown} columns={columns} />}

      {/* The scroll container carries the list role: it is the element a
          keyboard and a test both reach for, and the sized box inside it
          exists only to give the scrollbar something to measure. */}
      <div
        ref={scrollElement}
        className="min-h-0 flex-1 overflow-auto"
        role="list"
        aria-label="Commits"
        onKeyDown={handleKeyDown}
      >
        <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
          {rows.map((row) => (
            <div
              key={row.index}
              role="listitem"
              // The rendered window is not the list's length, and without
              // these two the window is what a screen reader is told: "list,
              // 26 items" and "item 3 of 26" for a history the panel heading
              // says is four hundred long, with both numbers moving under the
              // reader as it scrolls. They are the only channel that carries
              // the history's size — the graph beside the rows is aria-hidden
              // (docs/adr/0003) on the grounds that the rows say everything
              // it draws, and this is part of paying that bill.
              aria-setsize={total}
              aria-posinset={row.index + 1}
              className="absolute inset-x-0 top-0"
              style={{ height: row.size, transform: `translateY(${row.start}px)` }}
            >
              <CommitRowView
                row={row.index}
                commit={history.rowAt(row.index)}
                failure={history.failureAt(row.index)}
                reports={reporting.has(row.index)}
                indent={gutter}
                tabbable={row.index === tabbable}
                selected={history.rowAt(row.index)?.sha === selected}
                onSelect={onSelect}
                onFocusRow={setWantedRow}
              />
            </div>
          ))}

          {gutter !== undefined && rows.length > 0 && (
            <CommitGraph
              first={firstRow}
              count={rows.length}
              width={gutter}
              total={total}
              edges={history.edges}
              laneOf={history.laneOf}
            />
          )}
        </div>
      </div>
    </div>
  );
}

/**
 * The width of the list, in pixels, kept current as the window is resized.
 *
 * Zero until something has measured it — the first render, and every render in
 * a runner with no layout — which graphGutter reads as "no bound known" rather
 * than as "no room".
 *
 * A ResizeObserver and not a listener on the window: this panel's width also
 * changes when the reference sidebar or the commit pane beside it does, and a
 * window that was never resized would leave the gutter measured against a
 * layout that is no longer on screen.
 */
function useListWidth(element: RefObject<HTMLDivElement | null>): number {
  const [width, setWidth] = useState(0);

  useEffect(() => {
    const node = element.current;
    if (node === null) {
      return;
    }

    const observer = new ResizeObserver((entries) => {
      const measured = entries[0]?.contentRect.width;
      if (measured !== undefined) {
        setWidth(measured);
      }
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [element]);

  return width;
}

/**
 * The row a key press is on, read off the element it happened on.
 *
 * Undefined for anything that is not a commit row — the Retry button of a
 * failed page is inside this container too, and arrowing away from it would
 * take focus off the one control that page has.
 */
function rowOf(target: EventTarget): number | undefined {
  if (!(target instanceof HTMLElement)) {
    return undefined;
  }
  const attribute = target.closest('[data-row]')?.getAttribute('data-row');
  if (attribute === null || attribute === undefined) {
    return undefined;
  }
  const row = Number(attribute);
  return Number.isInteger(row) ? row : undefined;
}

/**
 * The row that carries the list's one stop in the page's tab order.
 *
 * Exactly one, and always a row that is actually rendered. Leaving every row
 * tabbable costs one press of Tab per commit to walk past the central screen
 * of the application; pinning the stop to the selected row instead would lose
 * it the moment the virtualiser scrolled that row out of the DOM, and Tab
 * would then step over the history entirely.
 *
 * Skeleton and failed rows are skipped: they are aria-hidden and they are not
 * buttons, so a stop on one is no stop at all. The search runs down from the
 * wanted row and then back up, so the entry point stays as near as it can to
 * where the reader left it. A window that is all skeletons has no stop, which
 * is honest — there is nothing there yet to put focus on, and the rows arrive
 * within a page fetch.
 *
 * Exported because it is arithmetic with one visible failure mode: get it
 * wrong and the history is either unreachable by keyboard or costs four
 * hundred presses to leave, neither of which throws.
 */
export function tabbableRow(
  wanted: number | undefined,
  first: number,
  last: number,
  loaded: (row: number) => boolean,
): number | undefined {
  if (last < first) {
    return undefined;
  }

  const start = Math.min(Math.max(wanted ?? first, first), last);
  for (let row = start; row <= last; row += 1) {
    if (loaded(row)) {
      return row;
    }
  }
  for (let row = start - 1; row >= first; row -= 1) {
    if (loaded(row)) {
      return row;
    }
  }
  return undefined;
}

/**
 * Where a key takes the focus from the row it was pressed on, or undefined for
 * a key the list has no answer for.
 *
 * ADR 0001 already names the arrow keys among the bindings left to yagit after
 * the browser has taken its own, so this is that decision applied rather than
 * a new one.
 *
 * Both ends clamp, where the tab bar and the segmented control wrap. Those
 * hold three things and a dead end at the edge is what people report as "the
 * arrows stopped working". A history holds a hundred thousand: ArrowDown on
 * the root commit landing back on the tip is not a wrap, it is losing your
 * place with no way of knowing it happened.
 */
export function rowForKey(
  key: string,
  from: number,
  total: number,
  page: number,
): number | undefined {
  const target = {
    ArrowUp: from - 1,
    ArrowDown: from + 1,
    PageUp: from - page,
    PageDown: from + page,
    Home: 0,
    End: total - 1,
  }[key];
  if (target === undefined) {
    return undefined;
  }
  return Math.min(Math.max(target, 0), Math.max(total - 1, 0));
}

/**
 * Said once, above the rows, when the graph is too wide to draw.
 *
 * It names the number rather than apologising, and says what is still true:
 * every commit is in the list. A picture that quietly left branches out would
 * be the worse answer, and a silent absence the worst of the three.
 *
 * This is now the refusal for a set of refs somebody chose, not for every ref
 * in the repository, so it points at the narrower choice when that is the one
 * not being drawn. Under the default scope there is no narrower choice to
 * point at, and the sentence used to stop there — a reader on the one walk
 * that cannot be narrowed was told the central feature was off and left
 * looking for the setting that would bring it back. Saying that no such
 * setting exists is the end of the thought.
 */
function TooWide({ columns, scope }: { columns: number; scope: HistoryScope }) {
  return (
    <p className="shrink-0 border-b border-line px-4 py-2 text-xs text-ink-muted">
      The graph is not drawn: this history is {columns} columns wide at its widest, and no picture
      that wide is readable beside the rows it belongs to. Every commit is still listed.
      {scope === 'all' &&
        ' Drawing the current branch alone is narrower, and so is picking the references you are comparing.'}
      {scope === 'refs' && ' Fewer references would be narrower.'}
      {scope === 'head' &&
        ' This is already the narrowest walk there is, and no choice of references would draw fewer columns.'}
    </p>
  );
}

/**
 * Said above the rows when the window, rather than the history, is what the
 * picture does not fit.
 *
 * The bound in geometry.ts is written in columns and cannot see how wide the
 * window got, so a graph inside it kept every pixel it asked for while the
 * subjects beside it went to nothing. The gutter is bounded by the list now,
 * and this is the other half of that: the same refusal to draw part of a
 * picture without saying which part.
 */
function GraphClipped({ shown, columns }: { shown: number; columns: number }) {
  return (
    <p className="shrink-0 border-b border-line px-4 py-2 text-xs text-ink-muted">
      The graph is drawn as far as {shown} of its {columns} columns: the rest would leave no room
      for the subjects it sits beside. A wider window draws them.
    </p>
  );
}

/**
 * The rows that report their page's failure out loud: the first row of each
 * failed page that is actually on screen.
 *
 * A page is two hundred rows and the window renders about forty of them, so a
 * failed page whose every row carried its own Retry would put forty controls
 * for one action into the tab order, and read one sentence forty times to a
 * screen reader. The rest of the page still shows what happened — leaving
 * those rows as skeletons is the history-stops-here impression this whole
 * path exists to correct — but they show it the way PendingRow holds space:
 * visible, and hidden from assistive technology.
 *
 * The rows counted are the visible ones, not the rendered ones. The
 * virtualiser renders a dozen either side of the viewport, and dragging the
 * scrollbar onto a failed page lands on the middle of it: the way out has to
 * be on a row the reader is looking at.
 *
 * Exported because it is the arithmetic of "one action, one path": a mistake
 * here is not a crash, it is a way out that is missing or repeated.
 */
export function reportingRows(
  visible: { startIndex: number; endIndex: number } | null,
  failureAt: (row: number) => PageFailure | undefined,
): Set<number> {
  const reporting = new Set<number>();
  if (visible === null) {
    return reporting;
  }

  const reported = new Set<number>();
  for (let row = visible.startIndex; row <= visible.endIndex; row += 1) {
    const failure = failureAt(row);
    if (failure === undefined || reported.has(failure.first)) {
      continue;
    }
    reported.add(failure.first);
    reporting.add(row);
  }
  return reporting;
}

/*
 * The columns a row is laid out in, and the reason they are these.
 *
 * The subject takes what is left over: it is the only column whose useful
 * length has no bound, and the only one a history is read for. The other three
 * are fixed, which is the whole point of them — "when did this land" and "who
 * has been working here" are answered by running an eye straight down a
 * column, and a metadata line that flowed after a subject of any length moved
 * the date's left edge by up to 152 pixels between two adjacent rows. The
 * tabular figures on that date were paid for and aligned nothing: tabular
 * figures only line up digits that already start at the same x.
 *
 * Their order is who, then when, then which. The two facts about people are
 * read on every pass and sit nearest the subject they belong to; the sha ends
 * the row against the panel's edge, where it is easy to find when you want to
 * take it to a terminal and easy to ignore the rest of the time. It is also
 * the one column whose width never varies, so it makes a clean right edge.
 *
 * Everything truncates and nothing wraps. The row height is fixed
 * (geometry.ts), so a second line does not make a row taller — it covers the
 * subject.
 *
 * The subject's basis is what decides which column yields first once the
 * window is too narrow to hold them all, and it is deliberately larger than
 * the space the subject is guaranteed. Flexbox shares a shortfall in
 * proportion to each item's basis, so a column that asks for less keeps less:
 * the subject asked for 128 pixels against the author's 160, started behind
 * and stayed behind, and around a nine hundred pixel window the author came
 * out the wider of the two — on the screen the subject is the whole reason
 * for. Asking for 256, about the length of a conventional-commit subject,
 * costs a wide window nothing (the subject is the only column that grows, so
 * it takes whatever is left over either way) and makes the author the column
 * that gives ground.
 *
 * They are written here rather than repeated in three components because the
 * skeleton row has to reproduce them to the pixel: a column missing from it is
 * width the subject placeholder grows by and hands back the instant the page
 * arrives, which is the sideways twitch the fixed row height exists to
 * prevent.
 */
const SUBJECT_COLUMN = 'min-w-0 grow shrink basis-64';
const AUTHOR_COLUMN = 'w-40 min-w-0';
const DATE_COLUMN = 'w-24 shrink-0';
const SHA_COLUMN = 'w-14 shrink-0';

interface CommitRowViewProps {
  /** Where this row sits in the history: the arrow keys count in these. */
  row: number;
  /** Undefined while the page holding this row is still on its way. */
  commit: CommitRow | undefined;
  /** What went wrong with that page, when it failed instead of arriving. */
  failure: PageFailure | undefined;
  /** Whether this is the row that reports that failure and offers the way out. */
  reports: boolean;
  /** Room to leave for the graph, or undefined when none is drawn. */
  indent: number | undefined;
  /**
   * Whether this row is the list's one stop in the page's tab order.
   *
   * Decided by the list and passed in, not worked out here: which row it is
   * depends on the window, on what has loaded and on where the reader left
   * focus, none of which a row can see.
   */
  tabbable: boolean;
  selected: boolean;
  onSelect: (sha: string) => void;
  /** Told which row focus landed on, so the arrows count from where it is. */
  onFocusRow: (row: number) => void;
}

export function CommitRowView({
  row,
  commit,
  failure,
  reports,
  indent,
  tabbable,
  selected,
  onSelect,
  onFocusRow,
}: CommitRowViewProps) {
  // The commit comes first: a page that still has rows in the cache keeps
  // showing them, because a refetch that fails is no reason to take a history
  // off the screen that is still on it.
  if (commit === undefined) {
    return failure === undefined ? (
      <PendingRow indent={indent} />
    ) : (
      <FailedRow indent={indent} failure={failure} reports={reports} />
    );
  }

  const committedAt = new Date(commit.date);

  return (
    <button
      type="button"
      data-row={row}
      tabIndex={tabbable ? 0 : -1}
      onClick={() => onSelect(commit.sha)}
      onFocus={() => onFocusRow(row)}
      aria-current={selected}
      style={indent === undefined ? undefined : { paddingLeft: indent }}
      className={cx(
        'flex h-full w-full items-center gap-3 border-b border-line px-4 text-left outline-none',
        'transition-colors transition-instant',
        selected ? 'bg-selected' : 'hover:bg-hover',
        'focus-visible:focus-ring',
      )}
    >
      <span className={cx('flex items-center gap-2', SUBJECT_COLUMN)}>
        <span className="truncate text-sm text-ink">{commit.subject}</span>
        {commit.refs.map((ref) => (
          <RefBadge key={ref} {...decorationBadge(ref)} />
        ))}
        {/* The graph draws a merge as two lines leaving one dot, and the
            graph is aria-hidden. This is the same fact in the channel a
            screen reader can reach — and the only one left when the picture
            is refused or clipped. */}
        {commit.parents.length > 1 && (
          <span className="shrink-0 text-2xs text-ink-subtle">merge</span>
        )}
      </span>

      {/* The chip travels with the name rather than leading the row. It is one
          fact drawn twice — a colour you learn to recognise, and the name it
          stands for — and split across the two ends of a row it is read as
          two. Beside the name it is what makes the author column scannable at
          a glance, and the subject then starts where the eye lands: right
          after the graph. */}
      <span
        className={cx('flex items-center gap-2 text-2xs text-ink-subtle', AUTHOR_COLUMN)}
        // The name in full, one hover away, for the same reason the failed row
        // titles both of its lines: a column narrow enough to scan down is
        // narrow enough to cut "Grace Brewster Murray Hopper" in half, and the
        // chip beside it is twenty-four pixels of tooltip target.
        title={commit.author}
      >
        <Avatar name={commit.author} decorative />
        <span className="truncate">{commit.author}</span>
      </span>

      {/* The exact instant as a title: past a week the visible text is a day
          and nothing more, and a working day's worth of commits shares it. */}
      <span
        className={cx('truncate text-2xs text-ink-subtle tabular', DATE_COLUMN)}
        title={formatExactTime(committedAt)}
      >
        {/* `now` is passed in rather than read inside: the same list rendered
            twice in one frame must not disagree with itself about the time,
            and a pure function of two dates is testable. */}
        {formatRelativeTime(committedAt, new Date())}
      </span>

      <span className={cx('truncate font-mono text-2xs text-ink-subtle', SHA_COLUMN)}>
        {shortenSha(commit.sha)}
      </span>
    </button>
  );
}

/**
 * A row whose page has not arrived yet.
 *
 * It holds the row's height and says nothing else. Dragging the scrollbar into
 * the middle of a long history lands on rows nobody has fetched, and leaving
 * the space blank would read as a history that stops there.
 *
 * It holds the row's COLUMNS too, empty ones included. The bars are placed
 * inside the same tracks the loaded row uses rather than being sized by eye,
 * because anything else is a layout that changes as each page lands.
 */
function PendingRow({ indent }: { indent: number | undefined }) {
  return (
    <div
      aria-hidden="true"
      style={indent === undefined ? undefined : { paddingLeft: indent }}
      className="flex h-full w-full items-center gap-3 border-b border-line px-4"
    >
      <span className={SUBJECT_COLUMN}>
        <span className="block h-2 w-64 max-w-full rounded-sm bg-hover" />
      </span>

      <span className={cx('flex items-center gap-2', AUTHOR_COLUMN)}>
        <span
          className="shrink-0 rounded-full bg-hover"
          style={{ width: AVATAR_SIZE, height: AVATAR_SIZE }}
        />
        <span className="h-2 w-20 min-w-0 rounded-sm bg-hover" />
      </span>

      {/* Empty, and still exactly as wide as the columns they stand in for.
          Two more bars per row would make the wait busier than the history. */}
      <span className={DATE_COLUMN} />
      <span className={SHA_COLUMN} />
    </div>
  );
}

/**
 * A row whose page failed instead of arriving.
 *
 * Every row of the failed page says so. Saying it on one row and leaving the
 * rest as skeletons would be the same history-stops-here impression this row
 * exists to correct. Only one of them says it out loud, though — see
 * reportingRows.
 *
 * The detail is what the daemon actually said — git's stderr, the exit code,
 * the command. It sits beside the message rather than under it, because the
 * row is one line high now; both carry their own text as a title, because
 * either can be wider than a row that shares its width with the graph.
 */
function FailedRow({
  indent,
  failure,
  reports,
}: {
  indent: number | undefined;
  failure: PageFailure;
  reports: boolean;
}) {
  const detail = gitFailureLine(failure.error);

  return (
    <div
      aria-hidden={!reports}
      style={indent === undefined ? undefined : { paddingLeft: indent }}
      className="flex h-full w-full items-center gap-3 border-b border-line px-4"
    >
      <span className="min-w-0 shrink truncate text-sm text-danger" title={failure.error.message}>
        {failure.error.message}
      </span>
      {detail !== undefined && (
        <span
          className="min-w-0 grow shrink truncate font-mono text-2xs text-ink-subtle"
          title={detail}
        >
          {detail}
        </span>
      )}

      {/* One way out, on one row of the page, and it asks for that page alone:
          reloading the application would throw away every other page already
          on screen. */}
      {reports && (
        <Button className="shrink-0" size="sm" loading={failure.retrying} onClick={failure.retry}>
          Retry
        </Button>
      )}
    </div>
  );
}

/**
 * Turns one entry of git's decoration into a badge.
 *
 * Exported because the commit details pane paints the same badges from the
 * same strings, and two functions reading `HEAD -> main` would eventually
 * disagree about what it means.
 *
 * `%D` writes them as `HEAD -> main`, `origin/main`, `tag: v1.0`. The prefixes
 * are git's own words, and stripping them here rather than in the parser keeps
 * the API answering exactly what git said — the interface is where a
 * presentation decision belongs.
 */
export function decorationBadge(decoration: string): {
  kind: 'head' | 'branch' | 'remote' | 'tag';
  name: string;
  current?: boolean;
} {
  if (decoration.startsWith('HEAD -> ')) {
    return { kind: 'head', name: decoration.slice('HEAD -> '.length), current: true };
  }
  if (decoration === 'HEAD') {
    return { kind: 'head', name: 'HEAD', current: true };
  }
  if (decoration.startsWith('tag: ')) {
    return { kind: 'tag', name: decoration.slice('tag: '.length) };
  }
  if (decoration.includes('/')) {
    return { kind: 'remote', name: decoration };
  }
  return { kind: 'branch', name: decoration };
}
