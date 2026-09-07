import { describe, expect, it } from 'vitest';

import type { FileStatus, StatusCode } from '../api/types';
import { commonDirectory, suggestCommitMessage } from './commitMessage';

/**
 * The suggestion is a guess, and a guess is worth having only while it is
 * right about the easy cases. These are the easy cases.
 */

/** A staged file, named by the codes git would report for it. */
function staged(path: string, index: StatusCode, extra: Partial<FileStatus> = {}): FileStatus {
  return {
    path,
    kind: 'ordinary',
    index,
    work_tree: '.',
    staged: true,
    unstaged: false,
    ...extra,
  };
}

describe('suggestCommitMessage', () => {
  it('says nothing when nothing is staged', () => {
    // The caller acts on this: a placeholder reading "Update 0 files" would be
    // the interface talking to itself.
    expect(suggestCommitMessage([])).toBe('');
  });

  it('names the one file and what happened to it', () => {
    expect(suggestCommitMessage([staged('src/parser.ts', 'A')])).toBe('Add src/parser.ts');
    expect(suggestCommitMessage([staged('src/parser.ts', 'M')])).toBe('Update src/parser.ts');
    expect(suggestCommitMessage([staged('src/parser.ts', 'D')])).toBe('Delete src/parser.ts');
  });

  it('calls a type change what it is', () => {
    // A file that became a symlink is neither added nor updated, and the
    // difference is exactly what a reader of the log needs.
    expect(suggestCommitMessage([staged('bin/node', 'T')])).toBe('Change the type of bin/node');
  });

  it('names both halves of a rename', () => {
    const renamed = staged('src/parser.ts', 'R', { kind: 'renamed', old_path: 'src/reader.ts' });
    expect(suggestCommitMessage([renamed])).toBe('Rename src/reader.ts to src/parser.ts');
  });

  it('calls a rename that kept its name a move', () => {
    // "Rename a/x.ts to b/x.ts" sends the reader looking for a new name that
    // is not there.
    const moved = staged('lib/x.ts', 'R', { kind: 'renamed', old_path: 'src/x.ts' });
    expect(suggestCommitMessage([moved])).toBe('Move src/x.ts to lib/x.ts');
  });

  it('names both halves of a copy', () => {
    const copied = staged('src/b.ts', 'C', { kind: 'copied', old_path: 'src/a.ts' });
    expect(suggestCommitMessage([copied])).toBe('Copy src/a.ts to src/b.ts');
  });

  it('reads the index and not the work tree', () => {
    // "AM" is a file added to the index and edited again afterwards. The
    // commit records the addition; the later edit is not in it.
    const addedThenEdited = staged('src/parser.ts', 'A', { work_tree: 'M', unstaged: true });
    expect(suggestCommitMessage([addedThenEdited])).toBe('Add src/parser.ts');
  });

  it('counts the files and names the directory they share', () => {
    expect(
      suggestCommitMessage([
        staged('internal/git/diff.go', 'M'),
        staged('internal/git/status.go', 'M'),
        staged('internal/git/log.go', 'M'),
      ]),
    ).toBe('Update 3 files in internal/git');
  });

  it('leaves the directory unsaid when it is the repository root', () => {
    // Every path is in the root, so naming it adds a word and no information.
    expect(suggestCommitMessage([staged('README.md', 'M'), staged('src/a.ts', 'M')])).toBe(
      'Update 2 files',
    );
  });

  it('keeps a shared verb across several files', () => {
    expect(suggestCommitMessage([staged('docs/a.md', 'A'), staged('docs/b.md', 'A')])).toBe(
      'Add 2 files in docs',
    );
  });

  it('falls back to the neutral verb when the files disagree', () => {
    // "Add 2 files" about a commit that deleted one of them would be worse
    // than saying less.
    expect(suggestCommitMessage([staged('docs/a.md', 'A'), staged('docs/b.md', 'D')])).toBe(
      'Update 2 files in docs',
    );
  });
});

describe('commonDirectory', () => {
  it('is empty for a path at the root', () => {
    expect(commonDirectory(['README.md'])).toBe('');
  });

  it('is the directory of a single nested path', () => {
    expect(commonDirectory(['internal/git/diff.go'])).toBe('internal/git');
  });

  it('stops at the deepest shared directory', () => {
    expect(commonDirectory(['internal/git/diff.go', 'internal/api/file.go'])).toBe('internal');
  });

  it('shares whole components, never a common string prefix', () => {
    // "src/ap" is five characters two paths agree on and no directory anybody
    // has.
    expect(commonDirectory(['src/apple.ts', 'src/apricot.ts'])).toBe('src');
  });

  it('is empty when one path sits at the root', () => {
    expect(commonDirectory(['internal/git/diff.go', 'README.md'])).toBe('');
  });

  it('is empty for no paths at all', () => {
    expect(commonDirectory([])).toBe('');
  });
});
