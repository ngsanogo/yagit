import { useQueries, useQuery } from '@tanstack/react-query';

import { api } from '../api/client';
import type { CommitPage, CommitRow, GraphEdge, HistoryScope } from '../api/types';
import { refsKey } from './historyScope';

/**
 * Reading a repository's history, one page at a time.
 *
 * Two hooks rather than one, because two components need different halves of
 * the same data and neither should have to ask for the other's. The panel
 * needs the length of the history to lay a scrollbar over it; the list needs
 * the rows the scrollbar happens to be over. Both read the same query cache,
 * so page zero is fetched once however many of them ask for it.
 *
 * Pages are keyed queries rather than an infinite query, and the reason is the
 * scrollbar: an infinite query reaches page 900 by fetching the 899 before it,
 * which is right for a feed and wrong for a control you can drag to the middle
 * of a million rows (docs/adr/0005).
 *
 * The scope is part of every key, and under `refs` the chosen set with it. Two
 * scopes are two histories — different commits, and different columns for the
 * commits they share — so page 3 of one is not page 3 of the other, and a key
 * that left either out would hand the rows of the walk you just left to the
 * picture you just asked for. The set is keyed by its contents rather than by
 * the order it arrived in; refsKey is where that is decided, and the daemon's
 * own store key does the same thing for the same reason.
 */

/** What the history is, before any particular part of it is read. */
export interface HistoryOverview {
  /** Commits in the whole history. */
  total: number;
  /** Columns the graph needs, over the whole history. */
  width: number;
  /** Rows per page. The daemon decides it and answers with it. */
  pageSize: number;
  isPending: boolean;
  error: Error | null;
}

export function useHistoryOverview(
  repositoryId: string,
  scope: HistoryScope,
  refs: readonly string[] = [],
): HistoryOverview {
  const chosen = refsKey(scope, refs);
  const first = useQuery({
    queryKey: ['commits', repositoryId, scope, chosen, 0],
    queryFn: () => api.commits(repositoryId, 0, scope, refs),
  });

  return {
    total: first.data?.total ?? 0,
    width: first.data?.width ?? 0,
    pageSize: first.data?.page_size ?? 0,
    isPending: first.isPending,
    error: first.error,
  };
}

/** The rows of a range of the history, and the lines crossing them. */
export interface HistoryWindow {
  /** The commit at a row, or undefined while its page is still on its way. */
  rowAt: (row: number) => CommitRow | undefined;
  /** The column of a row's dot, or undefined for the same reason. */
  laneOf: (row: number) => number | undefined;
  /** Every line with something to draw across the range, ends included. */
  edges: GraphEdge[];
  /** What went wrong with the page holding a row, when it failed. */
  failureAt: (row: number) => PageFailure | undefined;
}

/**
 * A page that failed instead of arriving, and the way back to it.
 *
 * The rows are worked out from the page number rather than read off the
 * answer, because a failed page has no answer to read a first row from. Its
 * length is the page size even where the last page of the history would have
 * been short: the rows past the end are rows the virtualiser never asks about.
 */
export interface PageFailure {
  /** The first row the page would have held. */
  first: number;
  /** How many rows it would have held. */
  count: number;
  /** What the daemon said, whole — an ApiError still carries git's stderr. */
  error: Error;
  /** Whether that page is being asked for again right now. */
  retrying: boolean;
  /** Asks for that one page again, and for nothing else. */
  retry: () => void;
}

/**
 * The pages covering the rows [first, last].
 *
 * A range past the end asks for nothing rather than for a page that cannot
 * exist: the history shrinks whenever a branch is deleted, and the virtualiser
 * finds out one render later than the daemon does.
 */
export function useHistoryWindow(
  repositoryId: string,
  pageSize: number,
  first: number,
  last: number,
  scope: HistoryScope,
  refs: readonly string[] = [],
): HistoryWindow {
  const covering = pagesCovering(pageSize, first, last);
  const chosen = refsKey(scope, refs);

  const pages = useQueries({
    queries: covering.map((index) => ({
      queryKey: ['commits', repositoryId, scope, chosen, index],
      queryFn: () => api.commits(repositoryId, index, scope, refs),
    })),
  });

  return windowOf(covering, pages, pageSize);
}

/**
 * The window a set of page results makes.
 *
 * Everything above this line belongs to the query library; everything below it
 * is yagit's — which page a row is in, and whether that page arrived, failed
 * or is still on its way. Split out so the answers can be asserted without a
 * browser to run the queries in: the hook is the only part left that a test
 * cannot reach, and it now decides nothing.
 */
export function windowOf(
  covering: readonly number[],
  pages: readonly PageQuery[],
  pageSize: number,
): HistoryWindow {
  const loaded = pages
    .map((page) => page.data)
    .filter((page): page is CommitPage => page !== undefined);

  const failed = failuresOf(covering, pages, pageSize);

  return {
    rowAt: (row) => commitAt(loaded, row),
    laneOf: (row) => commitAt(loaded, row)?.lane,
    edges: edgesOf(loaded),
    failureAt: (row) => failureAt(failed, row),
  };
}

/**
 * The part of a page's query the rows depend on.
 *
 * Written out rather than taken from the query library: it is the whole of
 * what a row needs, and a test can build one.
 */
export interface PageQuery {
  data: CommitPage | undefined;
  error: Error | null;
  isFetching: boolean;
  refetch: () => unknown;
}

/**
 * The pages of the window that failed.
 *
 * Results come back in the order the queries were asked for, which is what
 * pairs each of them with the page number at the same position. Exported for
 * the same reason as the arithmetic below: a page number that is out by one
 * retries a page nobody was waiting for and leaves the failed one on screen.
 */
export function failuresOf(
  covering: readonly number[],
  pages: readonly PageQuery[],
  pageSize: number,
): PageFailure[] {
  const failures: PageFailure[] = [];

  covering.forEach((index, position) => {
    const query = pages[position];
    if (query === undefined || query.error === null) {
      return;
    }

    failures.push({
      first: index * pageSize,
      count: pageSize,
      error: query.error,
      retrying: query.isFetching,
      retry: () => {
        // Nothing to await: a refetch settles into the query's own state, so a
        // second failure comes back through `error` and redraws these rows.
        void query.refetch();
      },
    });
  });

  return failures;
}

/**
 * What went wrong with the page holding a row, out of whichever failed page
 * holds it.
 *
 * Shaped like commitAt, and for the same reason: a page that knows where it
 * starts and how long it is cannot be misplaced by an arithmetic mistake.
 */
export function failureAt(failures: readonly PageFailure[], row: number): PageFailure | undefined {
  for (const failure of failures) {
    const offset = row - failure.first;
    if (offset >= 0 && offset < failure.count) {
      return failure;
    }
  }
  return undefined;
}

/**
 * The page numbers a range of rows falls in.
 *
 * Exported because it is arithmetic, and arithmetic that is wrong by one shows
 * the wrong commits rather than failing.
 */
export function pagesCovering(pageSize: number, first: number, last: number): number[] {
  if (pageSize <= 0 || last < first) {
    return [];
  }
  const from = Math.floor(Math.max(first, 0) / pageSize);
  const to = Math.floor(Math.max(last, 0) / pageSize);
  return Array.from({ length: to - from + 1 }, (_, offset) => from + offset);
}

/**
 * The commit at a row, out of whichever loaded page holds it.
 *
 * The page is found by asking each one whether the row is inside it, rather
 * than by dividing the row by the page size. There are never more than a
 * handful loaded, and a page that knows where it starts and how long it is
 * cannot be misplaced by an arithmetic mistake.
 */
export function commitAt(pages: CommitPage[], row: number): CommitRow | undefined {
  for (const page of pages) {
    const offset = row - page.first;
    if (offset >= 0 && offset < page.commits.length) {
      return page.commits[offset];
    }
  }
  return undefined;
}

/**
 * Every line across the loaded pages, each one once.
 *
 * A line crossing a page boundary is sent with both of the pages it crosses,
 * which is what makes each of them drawable on its own. Drawing it twice would
 * be invisible; the duplicate is dropped anyway, because the number of lines
 * on screen is what the batching by column is measured against.
 */
export function edgesOf(pages: CommitPage[]): GraphEdge[] {
  const edges: GraphEdge[] = [];
  const seen = new Set<string>();

  for (const page of pages) {
    for (const edge of page.edges) {
      const identity = `${edge.from}:${edge.to}:${edge.lane}`;
      if (!seen.has(identity)) {
        seen.add(identity);
        edges.push(edge);
      }
    }
  }
  return edges;
}
