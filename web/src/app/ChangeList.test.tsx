import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import type { FileStatus } from '../api/types';
import { ChangeList, describeRow, renamedFrom, type Selection } from './ChangeList';

/**
 * Where the change list sits in the page's tab order.
 *
 * Five hundred changed files is an ordinary morning after a formatter run, and
 * the only reason this list has a roving tabindex is that reaching the commit
 * box beneath it must not cost a press per row. That promise is kept by the
 * part of the pattern that is easiest to leave out: the row's Stage and its
 * Discard have to leave the tab order with their row. Taking the row alone out
 * of it removes one stop in three and looks exactly like the fix, which is why
 * it is asserted here rather than trusted to a reading.
 *
 * Rendered to markup rather than into a browser, as CommitList's rows are:
 * every attribute under test is in the output of one pure render, and this
 * project has no second test runner beside the end-to-end one.
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

function markup(files: FileStatus[], selected?: Selection): string {
  return renderToStaticMarkup(
    <ChangeList
      title="Unstaged"
      row="unstaged"
      files={files}
      selected={selected}
      onSelect={() => undefined}
      onMove={() => undefined}
      moveLabel="Stage"
      onDiscard={() => undefined}
      busy={false}
    />,
  );
}

function occurrences(html: string, needle: string): number {
  return html.split(needle).length - 1;
}

/** Which row is the list's tab stop, named by the path its button announces. */
function tabbableRow(html: string): string | undefined {
  for (const tag of html.match(/<button[^>]*data-file-row[^>]*>/g) ?? []) {
    if (tag.includes('tabindex="0"')) {
      return /aria-label="([^,"]+)/.exec(tag)?.[1];
    }
  }
  return undefined;
}

describe('the change list in the tab order', () => {
  it('costs three tab stops whatever the row count', () => {
    const html = markup([file('a.txt'), file('b.txt'), file('c.txt')]);

    // Three rows of three controls each, and three stops among the nine — the
    // row the arrows are on, its Stage and its Discard. Six tabbable controls
    // would mean the buttons had been left behind in the tab order.
    expect(occurrences(html, 'tabindex="0"')).toBe(3);
    expect(occurrences(html, 'tabindex="-1"')).toBe(6);
  });

  it('puts that stop on the chosen row, so tabbing back lands where the user was', () => {
    const files = [file('a.txt'), file('b.txt'), file('c.txt')];

    expect(tabbableRow(markup(files))).toBe('a.txt');
    expect(tabbableRow(markup(files, { path: 'b.txt', row: 'unstaged' }))).toBe('b.txt');
  });

  it('keeps the top of the list tabbable when the chosen file is in the other one', () => {
    // A file staged from this list is selected and gone from it. The stop has
    // to fall back to a row that exists, or the list drops out of the tab
    // order entirely and the commit box below becomes unreachable by keyboard.
    const html = markup([file('a.txt')], { path: 'b.txt', row: 'staged' });

    expect(tabbableRow(html)).toBe('a.txt');
  });
});

/**
 * Where a renamed row says it came from.
 *
 * The row draws the arrow, and what follows it has to be the part that
 * changed. A helper rather than a reading of the markup: the two cases differ
 * only in one string, and a test that dug it out of the HTML would be
 * asserting the layout around it as well.
 */
describe('renamedFrom', () => {
  it('names the whole old path when the file moved to another directory', () => {
    // The bug this exists for: the basename alone made the arrow point back at
    // the word it started from, and a move read as a rename to itself.
    expect(renamedFrom('src/gen2.txt', 'pkg/gen2.txt')).toBe('src/gen2.txt');
  });

  it('names the old name alone when the file stayed where it was', () => {
    // The directory is drawn on the row already, a few pixels to the left.
    expect(renamedFrom('src/old.txt', 'src/new.txt')).toBe('old.txt');
  });

  it('treats the repository root as a directory like any other', () => {
    // Two files at the top of the repository are in the same directory, so the
    // name alone is the answer there too.
    expect(renamedFrom('old.txt', 'new.txt')).toBe('old.txt');
    // And a file that moved OUT of a directory has moved, whichever way.
    expect(renamedFrom('src/gen2.txt', 'gen2.txt')).toBe('src/gen2.txt');
  });
});

/**
 * The one part of the row's spoken label this file is the right home for.
 *
 * describeRow's other cases live in workingDirectory.test.ts, beside the rest
 * of this screen's pure helpers; what is asserted here is the half that has to
 * agree with the arrow drawn above — change one and the row says two different
 * things about the same file to two different readers.
 */
describe('describeRow on a renamed file', () => {
  it('says where the file came from, which the arrow says visually', () => {
    const renamed = file('pkg/gen2.txt', {
      kind: 'renamed',
      old_path: 'src/gen2.txt',
      index: 'R',
      work_tree: '.',
      staged: true,
      unstaged: false,
    });

    expect(describeRow(renamed, 'staged')).toBe('pkg/gen2.txt, renamed from src/gen2.txt, staged');
  });
});
