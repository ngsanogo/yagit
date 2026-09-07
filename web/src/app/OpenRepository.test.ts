import { describe, expect, it } from 'vitest';

import { ApiError } from '../api/client';
import { failureOnForm, failureOnRow, failurePlacement, rowIsOpening } from './OpenRepository';

/**
 * One mutation serves both ways into a repository, and what went wrong before
 * was never a missing error — it was an error drawn beside a control nobody
 * had used. The rule is therefore about placement, and placement is arithmetic
 * on the request: it either came from the list or it came from the field.
 */

const refused = new ApiError(400, '"/home/you/gone" is not a git repository', {
  command: 'git rev-parse --path-format=absolute --git-common-dir --is-bare-repository',
  args: ['rev-parse', '--path-format=absolute', '--git-common-dir', '--is-bare-repository'],
  exit_code: 128,
  stderr: 'fatal: not a git repository (or any of the parent directories): .git',
});

describe('failurePlacement', () => {
  it('places nothing while nothing has failed', () => {
    expect(failurePlacement(null, undefined)).toEqual({ on: 'nothing' });

    // A successful open leaves its variables behind. Reading them as a failure
    // would put a stale error on a row that has just opened correctly.
    expect(failurePlacement(null, { path: '/home/you/project', from: 'list' })).toEqual({
      on: 'nothing',
    });
  });

  it('reports a refused row on that row', () => {
    expect(failurePlacement(refused, { path: '/home/you/gone', from: 'list' })).toEqual({
      on: 'row',
      path: '/home/you/gone',
      failure: refused,
    });
  });

  it('reports a refused path under the field it was typed in', () => {
    expect(failurePlacement(refused, { path: '/home/you/gone', from: 'form' })).toEqual({
      on: 'form',
      failure: refused,
    });
  });

  it('decides by the way in, not by the path', () => {
    // Both ways in can name the same repository, and the scan list is not the
    // tiebreaker. Working the origin out afterwards by looking the failed path
    // up in that list would send this pair to the same place — the row — which
    // is the original bug with the two controls swapped.
    const listed = '/home/you/listed';

    expect(failurePlacement(refused, { path: listed, from: 'list' }).on).toBe('row');
    expect(failurePlacement(refused, { path: listed, from: 'form' }).on).toBe('form');
  });

  it('carries the git failure whole, wherever it lands', () => {
    // The row has to be able to show the command, the exit code and the raw
    // stderr — the same detail the form shows, not a shortened version.
    for (const from of ['list', 'form'] as const) {
      const placement = failurePlacement(refused, { path: '/home/you/gone', from });
      if (placement.on === 'nothing') {
        throw new Error(`a failure from the ${from} was placed nowhere`);
      }

      expect(placement.failure).toBeInstanceOf(ApiError);
      expect((placement.failure as ApiError).git).toEqual(refused.git);
    }
  });
});

/**
 * Placement says where a failure belongs. The selectors below say what each
 * control is handed, and they are what the screen renders, so they are where a
 * mistake shows: invert the comparison in failureOnRow and every refusal is
 * painted on every row except the one that was clicked — with nothing else in
 * the project able to notice.
 */
describe('failureOnRow', () => {
  const placement = failurePlacement(refused, { path: '/home/you/gone', from: 'list' });

  it('hands the failure to the row that asked', () => {
    expect(failureOnRow(placement, '/home/you/gone')).toBe(refused);
  });

  it('hands nothing to any other row', () => {
    expect(failureOnRow(placement, '/home/you/project')).toBeNull();
  });

  it('hands nothing to a row when the path was typed', () => {
    expect(
      failureOnRow(
        failurePlacement(refused, { path: '/home/you/gone', from: 'form' }),
        '/home/you/gone',
      ),
    ).toBeNull();
  });
});

describe('failureOnForm', () => {
  it('hands the failure to the field it was typed in', () => {
    expect(failureOnForm(failurePlacement(refused, { path: '/home/you/gone', from: 'form' }))).toBe(
      refused,
    );
  });

  it('leaves the field clean when the list asked', () => {
    // An empty box must not explain a click somewhere else.
    expect(
      failureOnForm(failurePlacement(refused, { path: '/home/you/gone', from: 'list' })),
    ).toBeNull();
  });

  it('leaves the field clean while nothing has failed', () => {
    expect(failureOnForm(failurePlacement(null, undefined))).toBeNull();
  });
});

describe('rowIsOpening', () => {
  it('spins the row that asked', () => {
    expect(rowIsOpening(true, { path: '/home/you/listed', from: 'list' }, '/home/you/listed')).toBe(
      true,
    );
  });

  it('leaves every other row alone', () => {
    expect(rowIsOpening(true, { path: '/home/you/listed', from: 'list' }, '/home/you/other')).toBe(
      false,
    );
  });

  it('leaves the row alone when its path was typed into the field', () => {
    // A path can be in the list and in the box at once. Matching on the path
    // alone disables and spins a row nobody clicked, then re-enables it with
    // no message while the explanation appears somewhere else entirely.
    expect(rowIsOpening(true, { path: '/home/you/listed', from: 'form' }, '/home/you/listed')).toBe(
      false,
    );
  });

  it('spins nothing when nothing is in flight', () => {
    // Variables outlive the request, so pending is what says there is one.
    expect(
      rowIsOpening(false, { path: '/home/you/listed', from: 'list' }, '/home/you/listed'),
    ).toBe(false);
    expect(rowIsOpening(true, undefined, '/home/you/listed')).toBe(false);
  });
});
