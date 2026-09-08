import { describe, expect, it } from 'vitest';

import { leafOf, parentDirectoryOf, shortenPath } from './path';

describe('leafOf', () => {
  it('names the last segment', () => {
    expect(leafOf('/repos/yagit')).toBe('yagit');
  });

  it('reads a Windows path, because the daemon may be on one', () => {
    expect(leafOf('C:\\src\\yagit')).toBe('yagit');
  });

  it('ignores a trailing separator, which is punctuation and not a name', () => {
    expect(leafOf('/repos/yagit/')).toBe('yagit');
    expect(leafOf('C:\\src\\yagit\\')).toBe('yagit');
  });

  it('keeps a trailing space, which is part of the name', () => {
    expect(leafOf('/repos/build ')).toBe('build ');
  });

  it('answers with the whole of a path that has no separator', () => {
    expect(leafOf('yagit')).toBe('yagit');
  });

  it('answers with nothing for nothing', () => {
    expect(leafOf('')).toBe('');
  });
});

describe('parentDirectoryOf', () => {
  it('names the directory a path sits in', () => {
    expect(parentDirectoryOf('/repos/yagit')).toBe('/repos');
    expect(parentDirectoryOf('C:\\src\\yagit')).toBe('C:\\src');
  });

  it('looks past a trailing separator rather than answering with the path', () => {
    expect(parentDirectoryOf('/repos/yagit/')).toBe('/repos');
  });

  // There is no parent to name, and inventing `/` or `.` would put a directory
  // on screen nobody asked about.
  it('answers with itself where there is no parent', () => {
    expect(parentDirectoryOf('/yagit')).toBe('/yagit');
    expect(parentDirectoryOf('yagit')).toBe('yagit');
  });
});

describe('shortenPath', () => {
  it('cuts the head and keeps the end that identifies the path', () => {
    expect(shortenPath('/home/me/work/service')).toBe('…/work/service');
  });

  // The case the tab bar was drawing as two identical rows: the leaf agrees,
  // the parent is the whole of the difference, and a CSS truncate ate it.
  it('keeps two checkouts of one project apart', () => {
    expect(shortenPath('/home/me/checkouts-alpha/service')).toBe('…/checkouts-alpha/service');
    expect(shortenPath('/home/me/checkouts-bravo/service')).toBe('…/checkouts-bravo/service');
  });

  it('reads a Windows path, because the daemon may be on one', () => {
    expect(shortenPath('C:\\src\\yagit\\deep\\repo')).toBe('…\\deep\\repo');
  });

  it('answers whole where there is nothing above the two segments kept', () => {
    expect(shortenPath('/repos/yagit')).toBe('/repos/yagit');
    expect(shortenPath('yagit')).toBe('yagit');
    expect(shortenPath('')).toBe('');
  });

  // Dropping `C:` would put `C:\src\repo` and `D:\src\repo` on screen as one
  // string, which is the confusion this function exists to prevent.
  it('refuses to elide a bare root', () => {
    expect(shortenPath('C:\\src\\repo')).toBe('C:\\src\\repo');
  });

  it('ignores a trailing separator and keeps a trailing space', () => {
    expect(shortenPath('/home/me/work/service/')).toBe('…/work/service');
    expect(shortenPath('/home/me/work/build ')).toBe('…/work/build ');
  });
});
