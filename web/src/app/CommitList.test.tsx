import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../api/client';
import type { CommitRow } from '../api/types';
import { CommitRowView, reportingRows } from './CommitList';
import type { PageFailure } from './useHistory';

/**
 * What a row shows, and what it stays quiet about.
 *
 * A row has three things it can be, and the one nothing else on the screen can
 * say is the failure: the panel's error state belongs to page zero, and every
 * page after it has only its own rows to speak from. So this is asserted here
 * rather than left to the end-to-end suite alone, where one deleted prop would
 * go unnoticed until someone dragged the scrollbar.
 *
 * Rendered to markup rather than into a browser: these rows are plain
 * functions of their props, and markup is the whole of what the assertions
 * need — no DOM, no second test runner beside the end-to-end one.
 */

const refused = new ApiError(500, 'could not read the history', {
  command: 'git log --max-count=200 --skip=200',
  args: ['log', '--max-count=200', '--skip=200'],
  exit_code: 128,
  stderr: 'fatal: bad object HEAD',
});

function failure(overrides: Partial<PageFailure> = {}): PageFailure {
  return {
    first: 400,
    count: 200,
    error: refused,
    retrying: false,
    retry: () => undefined,
    ...overrides,
  };
}

const commit: CommitRow = {
  sha: '0f1e2d3c4b5a69788796a5b4c3d2e1f009182736',
  parents: [],
  author: 'Ada Lovelace',
  date: '2026-03-01T10:00:00Z',
  subject: 'feat: the second lane',
  refs: [],
  lane: 0,
};

function row(props: { commit?: CommitRow; failure?: PageFailure; reports?: boolean } = {}): string {
  return renderToStaticMarkup(
    <CommitRowView
      commit={props.commit}
      failure={props.failure}
      reports={props.reports ?? false}
      indent={undefined}
      selected={false}
      onSelect={() => undefined}
    />,
  );
}

describe('CommitRowView', () => {
  it('says what git said where the commit would have been', () => {
    const markup = row({ failure: failure(), reports: true });

    expect(markup).toContain('could not read the history');
    expect(markup).toContain('fatal: bad object HEAD');
    expect(markup).toContain('exit 128');
    expect(markup).toContain('git log --max-count=200 --skip=200');
    expect(markup).toContain('Retry');
  });

  it('leaves both lines readable when the row is too narrow for them', () => {
    // Both are clipped: the row shares its width with a graph that can take a
    // third of the panel, and a clipped line with no title on it is one the
    // reader cannot finish.
    const markup = row({ failure: failure(), reports: true });

    expect(markup).toContain('title="could not read the history"');
    expect(markup).toContain('title="fatal: bad object HEAD');
  });

  it('shows the commit it still has when asking for its page again failed', () => {
    // A refetch that failed is no reason to take a history off the screen
    // that is still on it.
    const markup = row({ commit, failure: failure(), reports: true });

    expect(markup).toContain('feat: the second lane');
    expect(markup).not.toContain('could not read the history');
  });

  it('holds the space and says nothing while the page is on its way', () => {
    const markup = row();

    expect(markup).toContain('aria-hidden="true"');
    expect(markup).not.toContain('<button');
  });

  it('leaves the way out to the one row that reports the failure', () => {
    // The rest of the page still shows what happened — skeletons under an
    // error are the impression this path exists to correct — but they are not
    // twenty more Retry buttons in the tab order for one action, and not the
    // same sentence twenty times to a screen reader.
    const quiet = row({ failure: failure(), reports: false });

    expect(quiet).toContain('fatal: bad object HEAD');
    expect(quiet).not.toContain('<button');
    expect(quiet).toContain('aria-hidden="true"');
  });
});

describe('reportingRows', () => {
  const earlier = failure({ first: 400 });
  const later = failure({ first: 600 });

  function failureAt(row: number): PageFailure | undefined {
    if (row >= 400 && row < 600) {
      return earlier;
    }
    if (row >= 600 && row < 800) {
      return later;
    }
    return undefined;
  }

  it('reports each failed page once, and each of them somewhere', () => {
    // Two failed pages and a good one on screen at the same time: one way out
    // per page, and none for the page that arrived.
    expect([...reportingRows({ startIndex: 398, endIndex: 601 }, failureAt)]).toEqual([400, 600]);
  });

  it('reports from the first visible row of the page, not from its first row', () => {
    // Dragging the scrollbar lands in the middle of a page, and row 400 is
    // then far above the viewport. A way out that only ever appeared there
    // would be one the reader cannot see.
    expect([...reportingRows({ startIndex: 500, endIndex: 515 }, failureAt)]).toEqual([500]);
  });

  it('has nothing to report when every page arrived', () => {
    expect(reportingRows({ startIndex: 0, endIndex: 20 }, () => undefined).size).toBe(0);
  });

  it('has nothing to report before anything is on screen', () => {
    // The virtualiser has no range until it has measured the panel.
    expect(reportingRows(null, failureAt).size).toBe(0);
  });
});
