import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../api/client';
import type { CommitRow } from '../api/types';
import { CommitRowView, reportingRows, rowForKey, tabbableRow } from './CommitList';
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

function row(
  props: { commit?: CommitRow; failure?: PageFailure; reports?: boolean; tabbable?: boolean } = {},
): string {
  return renderToStaticMarkup(
    <CommitRowView
      row={7}
      commit={props.commit}
      failure={props.failure}
      reports={props.reports ?? false}
      indent={undefined}
      tabbable={props.tabbable ?? false}
      selected={false}
      onSelect={() => undefined}
      onFocusRow={() => undefined}
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

  it('names its author once, and not in front of its subject', () => {
    // The initials are visible text, so they join the row's accessible name.
    // Before the chip was marked decorative every row in the history was
    // announced as "AL feat: the second lane" — two letters that mean nothing,
    // ahead of the most-read string on the screen, encoding the fact the
    // author column says in full a moment later.
    const markup = row({ commit });

    expect(markup).toContain('aria-hidden="true"');
    expect(markup.indexOf('feat: the second lane')).toBeLessThan(markup.indexOf('Ada Lovelace<'));
  });

  it('offers the tooltip of a date something the date does not already say', () => {
    // Past a week the visible text is a day and nothing more, and the tooltip
    // used to hand back that same day. A working day's worth of commits shares
    // a date, and the list's order is topological rather than chronological.
    const markup = row({ commit });

    expect(markup).toMatch(/title="[^"]*10:00[^"]*"/);
    expect(markup).toContain('1 Mar 2026');
  });

  it('is the one stop in the tab order only when the list says so', () => {
    expect(row({ commit, tabbable: true })).toContain('tabindex="0"');
    expect(row({ commit })).toContain('tabindex="-1"');
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

describe('tabbableRow', () => {
  const anything = () => true;

  it('makes the list one stop rather than one per commit', () => {
    // The whole point: a rendered window of forty rows leaves exactly one of
    // them in the page's tab order, and Tab walks past the history in a press.
    const rendered = [40, 41, 42, 43, 44];
    const stops = rendered.filter((row) => row === tabbableRow(42, 40, 44, anything));

    expect(stops).toEqual([42]);
  });

  it('keeps the stop inside the rendered window when the reader scrolled away', () => {
    // The row focus was last on is two thousand rows above the window now.
    // Leaving the stop there would leave the list with none at all, and Tab
    // would step over the history entirely.
    expect(tabbableRow(3, 400, 440, anything)).toBe(400);
    expect(tabbableRow(9000, 400, 440, anything)).toBe(440);
  });

  it('falls to the first rendered row when nothing has been focused or selected', () => {
    expect(tabbableRow(undefined, 12, 30, anything)).toBe(12);
  });

  it('skips the rows that are not buttons yet', () => {
    // A skeleton and a failed row are aria-hidden divs. A stop on one is no
    // stop, so the search runs down from the wanted row and then back up.
    const loaded = (row: number) => row === 5 || row === 20;

    expect(tabbableRow(10, 0, 40, loaded)).toBe(20);
    expect(tabbableRow(30, 0, 40, loaded)).toBe(20);
  });

  it('has no stop to offer while the whole window is still on its way', () => {
    // Honest rather than convenient: there is nothing on screen to put focus
    // on, and the rows arrive within a page fetch.
    expect(tabbableRow(4, 0, 40, () => false)).toBeUndefined();
  });

  it('has nothing to say before the virtualiser has measured the panel', () => {
    expect(tabbableRow(undefined, 0, -1, anything)).toBeUndefined();
  });
});

describe('rowForKey', () => {
  const total = 402;
  const page = 20;

  it('steps one row at a time, and a screenful at a time', () => {
    expect(rowForKey('ArrowDown', 10, total, page)).toBe(11);
    expect(rowForKey('ArrowUp', 10, total, page)).toBe(9);
    expect(rowForKey('PageDown', 10, total, page)).toBe(30);
    expect(rowForKey('PageUp', 100, total, page)).toBe(80);
  });

  it('reaches both ends of a history no amount of scrolling was going to', () => {
    expect(rowForKey('Home', 300, total, page)).toBe(0);
    expect(rowForKey('End', 3, total, page)).toBe(total - 1);
  });

  it('clamps at both ends rather than wrapping', () => {
    // The tab bar and the segmented control wrap, and are right to: they hold
    // three things. A history holds a hundred thousand, and ArrowDown on the
    // root commit landing back on the tip is losing your place with no way of
    // knowing it happened.
    expect(rowForKey('ArrowUp', 0, total, page)).toBe(0);
    expect(rowForKey('ArrowDown', total - 1, total, page)).toBe(total - 1);
    expect(rowForKey('PageUp', 3, total, page)).toBe(0);
    expect(rowForKey('PageDown', total - 2, total, page)).toBe(total - 1);
  });

  it('leaves every other key to the page', () => {
    // Typing, Escape, the browser's own bindings: a list that swallowed them
    // would be a trap of a different kind.
    for (const key of ['Enter', ' ', 'ArrowLeft', 'ArrowRight', 'Tab', 'Escape', 'j']) {
      expect(rowForKey(key, 10, total, page)).toBeUndefined();
    }
  });

  it('answers nothing for the names every object already has', () => {
    // Not reachable from a KeyboardEvent, and the point is that it should not
    // depend on that. Looked up in an object literal these six inherit from
    // Object.prototype and answer with a function, which the undefined check
    // lets through and Math.min turns into NaN.
    for (const key of [
      'constructor',
      'toString',
      'valueOf',
      'hasOwnProperty',
      '__proto__',
      'isPrototypeOf',
    ]) {
      expect(rowForKey(key, 10, total, page)).toBeUndefined();
    }
  });

  it('never leaves the history, even on a repository with one commit', () => {
    expect(rowForKey('End', 0, 1, page)).toBe(0);
    expect(rowForKey('PageDown', 0, 1, page)).toBe(0);
  });
});
