import { useVirtualizer } from '@tanstack/react-virtual';
import { useImperativeHandle, useRef, type Ref } from 'react';

import type { CommitRow, HistoryScope } from '../api/types';
import { Avatar } from '../components/Avatar';
import { RefBadge } from '../components/Badge';
import { Button } from '../components/Button';
import { cx } from '../lib/cx';
import { gitFailureLine } from '../lib/errorDisplay';
import { formatAbsoluteTime, formatRelativeTime, shortenSha } from '../lib/format';
import { CommitGraph } from './CommitGraph';
import { graphFits, graphWidth, ROW_HEIGHT } from './geometry';
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

  // A history with hundreds of branches open at once is wider than any column
  // beside a list of subjects can be. Then the rows take their ordinary
  // padding back and the reason is said out loud, rather than a narrower
  // picture being drawn that leaves branches out of it.
  const drawn = graphFits(columns);
  const indent = drawn ? graphWidth(columns) : undefined;

  // `range` is the rows on screen, which getVirtualItems above has just worked
  // out. The items themselves are that range plus the overscan, and a row
  // twelve rows above the viewport is no place for the only way out of a
  // failed page.
  const reporting = reportingRows(virtualizer.range, history.failureAt);

  return (
    <div className="flex h-full flex-col">
      {!drawn && <TooWide columns={columns} scope={scope} />}

      {/* The scroll container carries the list role: it is the element a
          keyboard and a test both reach for, and the sized box inside it
          exists only to give the scrollbar something to measure. */}
      <div
        ref={scrollElement}
        className="min-h-0 flex-1 overflow-auto"
        role="list"
        aria-label="Commits"
      >
        <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
          {rows.map((row) => (
            <div
              key={row.index}
              role="listitem"
              className="absolute inset-x-0 top-0"
              style={{ height: row.size, transform: `translateY(${row.start}px)` }}
            >
              <CommitRowView
                commit={history.rowAt(row.index)}
                failure={history.failureAt(row.index)}
                reports={reporting.has(row.index)}
                indent={indent}
                selected={history.rowAt(row.index)?.sha === selected}
                onSelect={onSelect}
              />
            </div>
          ))}

          {drawn && rows.length > 0 && (
            <CommitGraph
              first={firstRow}
              count={rows.length}
              columns={columns}
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
 * Said once, above the rows, when the graph is too wide to draw.
 *
 * It names the number rather than apologising, and says what is still true:
 * every commit is in the list. A picture that quietly left branches out would
 * be the worse answer, and a silent absence the worst of the three.
 *
 * This is now the refusal for a set of refs somebody chose, not for every ref
 * in the repository, so it points at the narrower choice when that is the one
 * not being drawn.
 */
function TooWide({ columns, scope }: { columns: number; scope: HistoryScope }) {
  return (
    <p className="shrink-0 border-b border-line px-4 py-2 text-xs text-ink-muted">
      The graph is not drawn: this history is {columns} columns wide at its widest, and no picture
      that wide is readable beside the rows it belongs to. Every commit is still listed.
      {scope === 'all' &&
        ' Drawing the current branch alone is narrower, and so is picking the references you are comparing.'}
      {scope === 'refs' && ' Fewer references would be narrower.'}
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

interface CommitRowViewProps {
  /** Undefined while the page holding this row is still on its way. */
  commit: CommitRow | undefined;
  /** What went wrong with that page, when it failed instead of arriving. */
  failure: PageFailure | undefined;
  /** Whether this is the row that reports that failure and offers the way out. */
  reports: boolean;
  /** Room to leave for the graph, or undefined when none is drawn. */
  indent: number | undefined;
  selected: boolean;
  onSelect: (sha: string) => void;
}

export function CommitRowView({
  commit,
  failure,
  reports,
  indent,
  selected,
  onSelect,
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
      onClick={() => onSelect(commit.sha)}
      aria-current={selected}
      style={indent === undefined ? undefined : { paddingLeft: indent }}
      className={cx(
        'flex h-full w-full items-center gap-3 border-b border-line px-4 text-left outline-none',
        'transition-colors transition-instant',
        selected ? 'bg-selected' : 'hover:bg-hover',
        'focus-visible:focus-ring',
      )}
    >
      <Avatar name={commit.author} />

      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm text-ink">{commit.subject}</span>
          {commit.refs.map((ref) => (
            <RefBadge key={ref} {...decorationBadge(ref)} />
          ))}
        </span>
        {/* nowrap throughout: the graph takes width from this line, and a
            date that wraps onto a second one grows a row whose height is
            fixed — the subject above it is what gets covered. */}
        <span className="flex items-center gap-2 whitespace-nowrap text-2xs text-ink-subtle">
          <span className="font-mono">{shortenSha(commit.sha)}</span>
          <span className="truncate">{commit.author}</span>
          {/* `now` is passed in rather than read inside: the same list rendered
              twice in one frame must not disagree with itself about the time,
              and a pure function of two dates is testable. */}
          <span className="tabular" title={formatAbsoluteTime(committedAt)}>
            {formatRelativeTime(committedAt, new Date())}
          </span>
          {/* The graph draws a merge as two lines leaving one dot, and the
              graph is aria-hidden. This is the same fact in the channel a
              screen reader can reach. */}
          {commit.parents.length > 1 && <span>merge</span>}
        </span>
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
 */
function PendingRow({ indent }: { indent: number | undefined }) {
  return (
    <div
      aria-hidden="true"
      style={indent === undefined ? undefined : { paddingLeft: indent }}
      className="flex h-full w-full items-center gap-3 border-b border-line px-4"
    >
      <span className="size-6 shrink-0 rounded-full bg-hover" />
      <span className="h-2 w-64 max-w-[40%] rounded-sm bg-hover" />
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
 * the command — flattened onto one line by the fixed row height. Both lines
 * carry their own text as a title, because either can be wider than a row
 * that shares its width with the graph.
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
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm text-danger" title={failure.error.message}>
          {failure.error.message}
        </span>
        {detail !== undefined && (
          <span className="truncate font-mono text-2xs text-ink-subtle" title={detail}>
            {detail}
          </span>
        )}
      </span>

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
