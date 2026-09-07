import { describe, expect, it } from 'vitest';

import type { FileDiff, FileStatus, StatusCode } from '../api/types';
import { describeRow, splitPath } from './ChangeList';
import { resolveSelection } from './ChangesView';
import {
  changedLines,
  countDrawnLines,
  countPatchLines,
  firstLineOfFile,
  firstLineOfHunk,
} from './DiffView';
import { isOnDisk, sideOf } from './useWorkingDirectory';

/**
 * The pure parts of the working-directory screen.
 *
 * Each one is a place where being wrong produces no error: a diff read from
 * the wrong side, a selection that follows a file into the wrong list, a hunk
 * offset that truncates the wrong lines. None of them throws; all of them show
 * the user something that is not true.
 */

function file(path: string, overrides: Partial<FileStatus> = {}): FileStatus {
  return {
    path,
    kind: 'ordinary',
    index: '.',
    work_tree: 'M',
    staged: false,
    unstaged: true,
    ...overrides,
  };
}

describe('sideOf', () => {
  it('reads a staged row against HEAD and an unstaged row against the index', () => {
    const both = file('a.txt', { staged: true, unstaged: true, index: 'M' });

    // The same file, two rows, two different questions. An interface that
    // picked one side per FILE would show the wrong half of this one half the
    // time — and this is the state `git add` followed by another edit leaves.
    expect(sideOf(both, 'staged')).toBe('staged');
    expect(sideOf(both, 'unstaged')).toBe('unstaged');
  });

  it('reads a file git has never seen as its whole content', () => {
    // There is no index entry to diff against, so `git diff` answers nothing.
    // Asking for the untracked side is what makes a new file show its lines.
    expect(sideOf(file('new.txt', { kind: 'untracked' }), 'unstaged')).toBe('untracked');
  });
});

describe('resolveSelection', () => {
  const staged = [file('a.txt', { staged: true, unstaged: false })];
  const unstaged = [file('b.txt')];

  it('follows a file into the other list when its row is gone', () => {
    // Staging a file moves it. The click that moved it was a click on a row
    // that no longer exists, and emptying the diff pane at that moment is the
    // interface losing the user's place at the exact moment they were looking.
    const followed = resolveSelection({ path: 'a.txt', row: 'unstaged' }, staged, unstaged);

    expect(followed?.file.path).toBe('a.txt');
    expect(followed?.row).toBe('staged');
  });

  it('prefers the row that was actually chosen when the file is in both', () => {
    const both = [file('c.txt', { staged: true, unstaged: true })];

    expect(resolveSelection({ path: 'c.txt', row: 'staged' }, both, both)?.row).toBe('staged');
    expect(resolveSelection({ path: 'c.txt', row: 'unstaged' }, both, both)?.row).toBe('unstaged');
  });

  it('gives up on a file that is in neither list', () => {
    // Committed, or discarded. There is nothing to show and saying so is the
    // honest answer.
    expect(
      resolveSelection({ path: 'gone.txt', row: 'unstaged' }, staged, unstaged),
    ).toBeUndefined();
  });

  it('has nothing to resolve when nothing was chosen', () => {
    expect(resolveSelection(undefined, staged, unstaged)).toBeUndefined();
  });
});

describe('describeRow', () => {
  it('leads with the path, because that is what tells rows apart', () => {
    expect(describeRow(file('src/parser.go'), 'unstaged')).toBe(
      'src/parser.go, changed, not staged',
    );
    expect(describeRow(file('src/parser.go', { kind: 'untracked' }), 'unstaged')).toBe(
      'src/parser.go, new file, not staged',
    );
  });

  it('says how a path is conflicted instead of which side it is on', () => {
    // An unmerged path is on neither side: there is nothing a commit could
    // record while the merge is unresolved, and "not staged" would suggest
    // staging is the next step.
    const conflicted = file('server.go', { kind: 'unmerged', conflict: 'both modified' });
    expect(describeRow(conflicted, 'unstaged')).toBe('server.go, both modified');
  });
});

describe('splitPath', () => {
  it('separates the name from the directory that places it', () => {
    // Twenty rows under one deep directory are unreadable when every one of
    // them leads with the same forty characters.
    expect(splitPath('internal/git/status.go')).toEqual({
      directory: 'internal/git/',
      name: 'status.go',
    });
    expect(splitPath('README.md')).toEqual({ directory: '', name: 'README.md' });
  });
});

describe('the diff, measured', () => {
  const diff: FileDiff = {
    id: 'fingerprint',
    path: 'a.txt',
    binary: false,
    added: false,
    removed: false,
    hunks: [
      {
        old_start: 1,
        old_lines: 2,
        new_start: 1,
        new_lines: 2,
        heading: '',
        lines: [
          { kind: 'context', text: 'one', index: 0, old_line: 1, new_line: 1, no_newline: false },
          { kind: 'removed', text: 'two', index: 1, old_line: 2, new_line: 0, no_newline: false },
          { kind: 'added', text: 'TWO', index: 2, old_line: 0, new_line: 2, no_newline: false },
        ],
      },
      {
        old_start: 10,
        old_lines: 1,
        new_start: 10,
        new_lines: 2,
        heading: 'func Example()',
        lines: [
          { kind: 'context', text: 'ten', index: 3, old_line: 10, new_line: 10, no_newline: false },
          { kind: 'added', text: 'new', index: 4, old_line: 0, new_line: 11, no_newline: false },
        ],
      },
    ],
  };

  it('counts every body line across every hunk', () => {
    expect(countDrawnLines(diff)).toBe(5);
  });

  it('places each hunk where it starts in the drawn diff', () => {
    // The offset decides which lines fall past the display cap. Wrong by one
    // and the cut lands in the middle of a hunk that said it would fit.
    expect(firstLineOfHunk(diff, 0)).toBe(0);
    expect(firstLineOfHunk(diff, 1)).toBe(3);
  });

  it('offers only the changed lines to a selection', () => {
    // Context is not a change. Including it would let a click on an unchanged
    // line be sent to the daemon as part of a patch.
    expect(changedLines(diff)).toEqual([1, 2, 4]);
  });

  it('spends one cap across every file of a patch', () => {
    // A commit is drawn as all of its files at once. A cap granted to each
    // file separately bounds nothing: five hundred files under a two-thousand
    // line cap is a million lines, and the tab drawing them stops answering.
    const second: FileDiff = { ...diff, path: 'b.txt' };

    expect(firstLineOfFile([diff, second])).toEqual([0, 5]);
    expect(countPatchLines([diff, second])).toBe(10);
    expect(firstLineOfFile([])).toEqual([]);
  });
});

/**
 * Whether there is a file to open at all.
 *
 * A deletion sits in the list like every other change, which is exactly why
 * this has to be asked: offering to edit a file somebody deleted produces a
 * 404 in answer to a button that should not have been there.
 */
describe('isOnDisk', () => {
  it('is true for an ordinary modification', () => {
    expect(isOnDisk(file('a.txt', { index: '.', work_tree: 'M' }))).toBe(true);
  });

  it('is true for an untracked file', () => {
    expect(isOnDisk(file('a.txt', { kind: 'untracked', index: '.', work_tree: '.' }))).toBe(true);
  });

  it('is false for a deletion nobody has staged', () => {
    expect(isOnDisk(file('a.txt', { index: '.', work_tree: 'D' }))).toBe(false);
  });

  it('is false for a staged deletion the work tree agrees with', () => {
    expect(isOnDisk(file('a.txt', { index: 'D', work_tree: '.' }))).toBe(false);
  });

  it('is true for a file deleted and then written again', () => {
    // git reports "DM": the index has the deletion, the work tree has a file.
    // There is something to open, and it is the thing the user just wrote.
    expect(isOnDisk(file('a.txt', { index: 'D', work_tree: 'M' }))).toBe(true);
  });

  // The codes of an unmerged entry are us and them, not index and work tree,
  // so the rule above does not apply to one at all. Checked against real git:
  // it leaves a file in the work tree for every pair but "both deleted" — the
  // merged text with markers when both sides changed it, and the surviving
  // side's version when only one did.
  it.each<[StatusCode, StatusCode, string]>([
    ['U', 'U', 'both modified'],
    ['A', 'A', 'both added'],
    ['A', 'U', 'added by us'],
    ['U', 'A', 'added by them'],
    ['D', 'U', 'deleted by us, so their version is in the work tree'],
    ['U', 'D', 'deleted by them, so our version is in the work tree'],
  ])('is true for %s%s: %s', (index, work_tree) => {
    expect(isOnDisk(file('a.txt', { kind: 'unmerged', index, work_tree }))).toBe(true);
  });

  it('is false when both sides deleted it', () => {
    // "DD" is the one conflict with nothing on disk, so it is the one row
    // that must not open an editor: the read would 404.
    expect(isOnDisk(file('a.txt', { kind: 'unmerged', index: 'D', work_tree: 'D' }))).toBe(false);
  });
});
