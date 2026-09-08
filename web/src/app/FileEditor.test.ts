import { describe, expect, it } from 'vitest';

import { keepWholeFileCommands, sideHasContent } from './FileEditor';

/**
 * What the confirmation in front of "take the whole file" promises.
 *
 * The promise is that the lines above the answer are the lines that run, and
 * they are composed in the browser: there is no `resolve/plan` route to ask
 * the way the discard flow asks `discard/plan`, so the argument lists are
 * mirrored from internal/git/staging.go and rendered by the rule
 * internal/git/commandline.go renders by. This file cannot read the Go and
 * cannot say the two still agree. What it can do is make a change to the copy
 * a change somebody meant to make, which is the whole of what a mirror can be
 * held to from this side.
 *
 * The expectations are written out in full rather than built from the same
 * pieces the code builds from. A test that composed the string the way the
 * subject composes it would pass on the day the subject stops being right.
 */
describe('keepWholeFileCommands', () => {
  it('checks the side out and then stages it, where that side has a file', () => {
    // Two commands and both are needed: the checkout writes the file, and only
    // the add collapses the three index stages that make git call it unmerged.
    expect(keepWholeFileCommands('notes.md', 'ours', true)).toEqual([
      "git checkout --ours -- ':(literal)notes.md'",
      "git add -- ':(literal)notes.md'",
    ]);
  });

  it('names the side that was asked for', () => {
    expect(keepWholeFileCommands('notes.md', 'theirs', true)).toEqual([
      "git checkout --theirs -- ':(literal)notes.md'",
      "git add -- ':(literal)notes.md'",
    ]);
  });

  it('removes the file, and only that, where the side has none', () => {
    // "Deleted by them", taking theirs: there is nothing to check out, and
    // `git rm` both clears the work tree and ends the conflict in one command.
    expect(keepWholeFileCommands('notes.md', 'theirs', false)).toEqual([
      "git rm -- ':(literal)notes.md'",
    ]);
  });

  it('quotes a name holding an apostrophe the way the log panel will', () => {
    // Double quotes, because single-quoting has to break out and back in
    // around every apostrophe — correct, copyable, and nobody's idea of a line
    // they are being asked to approve. Reached only where the name holds none
    // of the four characters a shell still reads inside double quotes.
    //
    // Written as a template literal so that neither quote has to be escaped in
    // the expectation itself: an escape here is a chance to assert the wrong
    // string.
    expect(keepWholeFileCommands("it's.md", 'theirs', false)).toEqual([
      `git rm -- ":(literal)it's.md"`,
    ]);
  });

  it('falls back to single quotes where a shell would read what is inside double ones', () => {
    expect(keepWholeFileCommands('say "hello".md', 'ours', false)).toEqual([
      `git rm -- ':(literal)say "hello".md'`,
    ]);
  });
});

/**
 * Which of the two shapes a conflict gets, read off git's own words.
 *
 * The same seven pairs the daemon's TestHasSideFollowsTheConflictAndNotTheLetterD
 * walks, for the same reason: a rule written as "not deleted" reports our side
 * as having content for `UA`, and the checkout the dialog then promises is the
 * one git refuses with "does not have our version". A dialog that promised a
 * checkout and ran a removal would be a new version of the failure the dialog
 * was added to fix.
 */
describe('sideHasContent', () => {
  it.each<[string, boolean, boolean]>([
    ['both modified', true, true],
    ['both added', true, true],
    ['both deleted', false, false],
    ['added by us', true, false],
    ['added by them', false, true],
    ['deleted by us', false, true],
    ['deleted by them', true, false],
  ])('%s: ours %s, theirs %s', (conflict, ours, theirs) => {
    expect(sideHasContent(conflict, 'ours')).toBe(ours);
    expect(sideHasContent(conflict, 'theirs')).toBe(theirs);
  });

  it('treats a pair git has not invented yet as two sides with content', () => {
    // The answer that fails loudly. `git checkout` on a side that turns out to
    // have no file stops with git's own sentence; `git rm` on a file this code
    // did not understand deletes it.
    expect(sideHasContent('unmerged (XY)', 'ours')).toBe(true);
    expect(sideHasContent('unmerged (XY)', 'theirs')).toBe(true);
  });
});
