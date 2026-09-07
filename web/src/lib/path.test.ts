import { describe, expect, it } from 'vitest';

import { leafOf, parentDirectoryOf } from './path';

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
