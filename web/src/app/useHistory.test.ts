import { describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client';
import type { CommitPage, CommitRow, GraphEdge } from '../api/types';
import {
  commitAt,
  edgesOf,
  failureAt,
  failuresOf,
  pagesCovering,
  windowOf,
  type PageQuery,
} from './useHistory';

/**
 * Paging is arithmetic, and arithmetic that is wrong by one does not fail —
 * it shows the wrong commit under the right row.
 */

function page(first: number, count: number, edges: GraphEdge[] = []): CommitPage {
  return {
    commits: Array.from({ length: count }, (_, offset) => commit(first + offset)),
    edges,
    first,
    page_size: 200,
    total: 1000,
    width: 1,
  };
}

/** A page whose query is doing nothing in particular. */
function query(overrides: Partial<PageQuery> = {}): PageQuery {
  return {
    data: undefined,
    error: null,
    isFetching: false,
    refetch: () => undefined,
    ...overrides,
  };
}

/** What the daemon says when git refuses: message, command, code, stderr. */
const refused = new ApiError(500, 'could not read the history', {
  command: 'git log --max-count=200 --skip=200',
  args: ['log', '--max-count=200', '--skip=200'],
  exit_code: 128,
  stderr: 'fatal: bad object HEAD',
});

function commit(row: number): CommitRow {
  return {
    sha: `sha-${row}`,
    parents: [],
    author: 'Ada Lovelace',
    date: '2026-03-01T10:00:00Z',
    subject: `commit ${row}`,
    refs: [],
    lane: 0,
  };
}

describe('pagesCovering', () => {
  it('asks for the one page a short range falls in', () => {
    expect(pagesCovering(200, 0, 30)).toEqual([0]);
    expect(pagesCovering(200, 199, 199)).toEqual([0]);
  });

  it('asks for both pages a range straddling a boundary falls in', () => {
    expect(pagesCovering(200, 199, 200)).toEqual([0, 1]);
    expect(pagesCovering(200, 150, 450)).toEqual([0, 1, 2]);
  });

  it('asks for nothing before the page size is known', () => {
    // The size comes from the daemon, in the answer to the first page. Until
    // it lands, dividing by it would ask for page Infinity.
    expect(pagesCovering(0, 0, 40)).toEqual([]);
  });

  it('asks for nothing when the range is empty', () => {
    expect(pagesCovering(200, 5, 4)).toEqual([]);
  });
});

describe('commitAt', () => {
  const loaded = [page(200, 200), page(400, 200)];

  it('finds a row in whichever page holds it', () => {
    expect(commitAt(loaded, 200)?.sha).toBe('sha-200');
    expect(commitAt(loaded, 399)?.sha).toBe('sha-399');
    expect(commitAt(loaded, 400)?.sha).toBe('sha-400');
  });

  it('answers nothing for a row whose page has not arrived', () => {
    expect(commitAt(loaded, 0)).toBeUndefined();
    expect(commitAt(loaded, 600)).toBeUndefined();
  });

  it('answers nothing past the end of a short last page', () => {
    // The last page of a history is rarely full, and the row after it is a
    // row the virtualiser asked about before the daemon said how many there
    // were.
    expect(commitAt([page(400, 3)], 403)).toBeUndefined();
    expect(commitAt([page(400, 3)], 402)?.sha).toBe('sha-402');
  });
});

describe('edgesOf', () => {
  const crossing: GraphEdge = { from: 190, from_lane: 0, to: 260, to_lane: 0, lane: 1 };
  const inside: GraphEdge = { from: 10, from_lane: 0, to: 12, to_lane: 0, lane: 0 };

  it('keeps a line that crosses a page boundary exactly once', () => {
    // Both pages carry it, which is what makes each of them drawable alone.
    const edges = edgesOf([page(0, 200, [inside, crossing]), page(200, 200, [crossing])]);

    expect(edges).toHaveLength(2);
    expect(edges.filter((edge) => edge.from === 190)).toHaveLength(1);
  });

  it('keeps two different lines that leave the same row', () => {
    // A merge: two lines out of one dot, differing only by the column they
    // run down. An identity that ignored the column would lose one.
    const merge: GraphEdge[] = [
      { from: 5, from_lane: 0, to: 9, to_lane: 0, lane: 0 },
      { from: 5, from_lane: 0, to: 9, to_lane: 0, lane: 2 },
    ];
    expect(edgesOf([page(0, 200, merge)])).toHaveLength(2);
  });

  it('has nothing to say about no pages', () => {
    expect(edgesOf([])).toEqual([]);
  });
});

describe('failuresOf', () => {
  it('names the rows the page that failed was meant to hold', () => {
    // Page 1 arrived, page 2 did not: rows 400 to 599 are the failed ones.
    const failures = failuresOf([1, 2], [query(), query({ error: refused })], 200);

    expect(failures).toHaveLength(1);
    expect(failureAt(failures, 400)).toBe(failures[0]);
    expect(failureAt(failures, 599)).toBe(failures[0]);
    expect(failureAt(failures, 399)).toBeUndefined();
    expect(failureAt(failures, 600)).toBeUndefined();
  });

  it('carries what git said all the way to the row', () => {
    // The point of the whole thing: the row can show the command, the exit
    // code and the raw stderr, rather than a sentence about a page.
    const failures = failuresOf([1], [query({ error: refused })], 200);
    const error = failureAt(failures, 250)?.error;

    expect(error).toBe(refused);
    expect(error instanceof ApiError ? error.git?.stderr : undefined).toBe(
      'fatal: bad object HEAD',
    );
  });

  it('retries the page the row is on, and no other', () => {
    const pageOne = vi.fn();
    const pageTwo = vi.fn();
    const failures = failuresOf(
      [1, 2],
      [query({ error: refused, refetch: pageOne }), query({ error: refused, refetch: pageTwo })],
      200,
    );

    failureAt(failures, 250)?.retry();

    expect(pageOne).toHaveBeenCalledTimes(1);
    expect(pageTwo).not.toHaveBeenCalled();
  });

  it('says when the page is already being asked for again', () => {
    // What keeps the retry button from looking like it did nothing.
    const failures = failuresOf([1], [query({ error: refused, isFetching: true })], 200);

    expect(failureAt(failures, 200)?.retrying).toBe(true);
  });

  it('has nothing to say about pages that are still on their way', () => {
    expect(failuresOf([0, 1], [query(), query()], 200)).toEqual([]);
    expect(failuresOf([], [], 200)).toEqual([]);
  });
});

/**
 * The window is what the list actually reads, and the defect this fixes lived
 * in the assembly rather than in any of the parts above: every failure was
 * worked out correctly and then dropped on the way out.
 */
describe('windowOf', () => {
  it('answers a failed page with what went wrong, not with silence', () => {
    // Page 1 arrived, page 2 refused. Rows 400 to 599 have no commit, and
    // asking the window for one of them has to reach the error — a window
    // that answers undefined for both is the skeleton that never resolves.
    const history = windowOf(
      [1, 2],
      [query({ data: page(200, 200) }), query({ error: refused })],
      200,
    );

    expect(history.rowAt(250)?.sha).toBe('sha-250');
    expect(history.failureAt(250)).toBeUndefined();

    expect(history.rowAt(450)).toBeUndefined();
    expect(history.failureAt(450)?.error).toBe(refused);
    expect(history.failureAt(450)?.first).toBe(400);
  });

  it('carries the stderr as far as the row, and the way back with it', () => {
    const refetch = vi.fn();
    const history = windowOf([1], [query({ error: refused, refetch })], 200);
    const failure = history.failureAt(200);

    expect(failure?.error instanceof ApiError ? failure.error.git?.stderr : undefined).toBe(
      'fatal: bad object HEAD',
    );

    failure?.retry();
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('keeps the rows a page already had when asking for it again fails', () => {
    // A refetch that failed is no reason to take a history off the screen
    // that is still on it: the commit wins, and the failure waits behind it.
    const history = windowOf([1], [query({ data: page(200, 200), error: refused })], 200);

    expect(history.rowAt(250)?.sha).toBe('sha-250');
    expect(history.laneOf(250)).toBe(0);
  });

  it('has nothing to report while the pages are on their way', () => {
    const history = windowOf([0, 1], [query(), query()], 200);

    expect(history.rowAt(0)).toBeUndefined();
    expect(history.failureAt(0)).toBeUndefined();
    expect(history.edges).toEqual([]);
  });
});
